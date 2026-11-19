package load

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const webImport = `import "github.com/vuka-lang/web/v2"` + "\n"

// restartCase is a module of one package, app: its Go sources by file name.
type restartCase struct {
	name         string
	files        map[string]string
	keys         []string // restart-only
	live         []string
	handled      []string
	unclassified int
}

func runRestart(t *testing.T, c restartCase) RestartReport {
	t.Helper()
	var srcs []restartSrc
	for name, src := range c.files {
		srcs = append(srcs, restartSrc{"example.com/app", name, []byte(src)})
	}
	return analyzeRestart(srcs)
}

func TestRestartKeysClassification(t *testing.T) {
	cases := []restartCase{
		{
			name: "package variable initialiser",
			files: map[string]string{"a.go": "package main\n" + webImport + `
var dsn = web.Env[string]("PKG_DSN")
func main() {}
`},
			keys: []string{"PKG_DSN"},
		},
		{
			name: "init",
			files: map[string]string{"a.go": "package main\n" + webImport + `
var port int
func init() { port = web.Env("INIT_PORT", 8080) }
`},
			keys: []string{"INIT_PORT"},
		},
		{
			name: "main, and what main calls",
			files: map[string]string{"a.go": "package main\n" + webImport + `
func main() {
	_ = web.MustEnv[string]("MAIN_KEY")
	boot()
}
func boot() { _ = web.Env("BOOT_KEY", "") }
func unused() { _ = web.Env("UNUSED_KEY", "") }
`},
			keys: []string{"BOOT_KEY", "MAIN_KEY"},
			live: []string{"UNUSED_KEY"},
		},
		{
			name: "providers and constructors the app builds once",
			files: map[string]string{"a.go": "package main\n" + webImport + `
type DB struct{}
func OpenDB() *DB { _ = web.Env("DB_URL", ""); return nil }
type Cache struct{}
func NewCache() *Cache { _ = web.Env("CACHE_URL", ""); return nil }
type Mail struct{}
func NewMail() *Mail { _ = web.Env("MAIL_HOST", ""); return nil }
type Pool struct{}
func (p *Pool) Start() error { _ = web.Env("POOL_SIZE", 4); return nil }
func main() {
	app := web.New(web.Provide(OpenDB), web.OnStart(func() error { _ = web.Env("ONSTART_KEY", ""); return nil }))
	app.Service(NewCache)
	app.Provide(func() *Mail { return NewMail() })
}
`},
			keys: []string{"CACHE_URL", "DB_URL", "MAIL_HOST", "ONSTART_KEY", "POOL_SIZE"},
		},
		{
			name: "handlers, registered or not, are live",
			files: map[string]string{"a.go": "package main\n" + webImport + `
func List() string { return web.Env("HANDLER_KEY", "") }
func Show() string { return web.Env("SHOW_KEY", "") }
func main() {
	app := web.New()
	app.Get("/", List)
	app.Get("/show", func() string { return Show() + web.Env("LIT_KEY", "") })
	app.Cron("tick", "@every 1m", func() { _ = web.Env("CRON_KEY", "") })
}
`},
			live: []string{"CRON_KEY", "HANDLER_KEY", "LIT_KEY", "SHOW_KEY"},
		},
		{
			name: "a live page's Render, event, Update and Mount",
			files: map[string]string{"a.go": "package main\n" + webImport + `
type Page struct{}
func (p *Page) Mount() error { _ = web.Env("MOUNT_KEY", ""); return nil }
func (p *Page) Render() string { return web.Env("RENDER_KEY", "") }
func (p *Page) Save() error { _ = web.Env("EVENT_KEY", ""); return nil }
func (p *Page) Update() error { _ = web.Env("UPDATE_KEY", ""); return nil }
func main() { web.New().Live("/", &Page{}) }
`},
			live: []string{"EVENT_KEY", "MOUNT_KEY", "RENDER_KEY", "UPDATE_KEY"},
		},
		{
			name: "a helper called from start and from a request path",
			files: map[string]string{"a.go": "package main\n" + webImport + `
func limit() int { return web.Env("SHARED_LIMIT", 10) }
func handler() int { return limit() }
func main() {
	_ = limit()
	web.New().Get("/", handler)
}
`},
			keys: []string{"SHARED_LIMIT"},
		},
		{
			name: "a key also used with Hot or OnEnvChange is the app's to handle",
			files: map[string]string{"a.go": "package main\n" + webImport + `
var rate = web.Hot[int]("HOT_KEY", 100)
var burst = web.Env("HOT_KEY", 1)
var size = web.Env("WATCH_KEY", 1)
var plain = web.Env("PLAIN_KEY", 1)
func init() { web.OnEnvChange("WATCH_KEY", func(old, new string) {}) }
`},
			keys:    []string{"PLAIN_KEY"},
			handled: []string{"HOT_KEY", "WATCH_KEY"},
		},
		{
			name: "web.Require checks when the app is built",
			files: map[string]string{"a.go": "package main\n" + webImport + `
func opts() []web.Option { return []web.Option{web.Require("REQ_A", "REQ_B")} }
`},
			keys: []string{"REQ_A", "REQ_B"},
		},
		{
			name: "keys written as constants and concatenations",
			files: map[string]string{"a.go": "package main\n" + webImport + `
const prefix = "APP_"
const dsnKey = prefix + "DSN"
var a = web.Env("APP_"+"PORT", 1)
var b = web.Env(dsnKey, "")
`},
			keys: []string{"APP_DSN", "APP_PORT"},
		},
		{
			name: "a key that isn't a constant is skipped and counted",
			files: map[string]string{"a.go": "package main\n" + webImport + `
var name = "X"
func get(k string) string { return web.Env(k, "") }
var a = web.Env(name, "")
var b = web.Env("Y_"+name, "")
var c = web.Env("KNOWN", "")
func main() { _ = get("ARG") }
`},
			keys:         []string{"KNOWN"},
			unclassified: 3,
		},
		{
			name: "a closure called on the spot or kept in a variable runs at start",
			files: map[string]string{"a.go": "package main\n" + webImport + `
func main() {
	func() { _ = web.Env("IMMEDIATE_KEY", "") }()
	load := func() { _ = web.Env("LOCAL_KEY", "") }
	load()
	later := func() { _ = web.Env("NEVER_CALLED_KEY", "") }
	_ = later
}
`},
			keys: []string{"IMMEDIATE_KEY", "LOCAL_KEY"},
			live: []string{"NEVER_CALLED_KEY"},
		},
		{
			name: "a name that isn't web's is not a read",
			files: map[string]string{"a.go": `package main
import "os"
type env struct{}
func (env) Env(k string) string { return "" }
var e env
var a = e.Env("NOT_WEB")
var b = os.Getenv("OS_KEY")
`},
		},
		{
			name: "a renamed import of web and a later major version",
			files: map[string]string{"a.go": `package main
import w "github.com/vuka-lang/web/v3"
var a = w.Env("RENAMED_KEY", "")
`},
			keys: []string{"RENAMED_KEY"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := runRestart(t, c)
			check := func(what string, got, want []string) {
				t.Helper()
				if !slices.Equal(got, want) {
					t.Errorf("%s = %v, want %v", what, got, want)
				}
			}
			check("restart-only", rep.Keys, c.keys)
			check("live", rep.Live, c.live)
			check("handled", rep.Handled, c.handled)
			if len(rep.Unclassified) != c.unclassified {
				t.Errorf("%d unclassified, want %d: %v", len(rep.Unclassified), c.unclassified, rep.Unclassified)
			}
		})
	}
}

func TestRestartKeysAcrossPackages(t *testing.T) {
	rep := analyzeRestart([]restartSrc{
		{"example.com/app", "main.go", []byte(`package main
import (
	"example.com/app/store"
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)
func main() {
	web.New(web.Provide(store.Open)).Get("/", pages.Index)
	_ = store.Opts
}
`)},
		{"example.com/app/store", "store.go", []byte(`package store
import "github.com/vuka-lang/web/v2"
const URLKey = "STORE_URL"
func Open() int { return web.Env(URLKey, 0) }
var Opts = web.Env("STORE_OPTS", "")
func Reload() string { return web.Env("STORE_RELOAD", "") }
`)},
		{"example.com/app/pages", "pages.go", []byte(`package pages
import (
	"example.com/app/store"
	"github.com/vuka-lang/web/v2"
)
func Index() string { _ = store.Reload(); return web.Env(store.URLKey + "_X", "") }
`)},
	})
	if want := []string{"STORE_OPTS", "STORE_URL"}; !slices.Equal(rep.Keys, want) {
		t.Errorf("restart-only = %v, want %v", rep.Keys, want)
	}
	if want := []string{"STORE_RELOAD", "STORE_URL_X"}; !slices.Equal(rep.Live, want) {
		t.Errorf("live = %v, want %v", rep.Live, want)
	}
}

func TestRestartSrcFile(t *testing.T) {
	got := string(restartSrcFile(RestartReport{Keys: []string{"A", "B"}, Web: "github.com/vuka-lang/web/v2"}))
	for _, want := range []string{"Code generated by vuka. DO NOT EDIT.", "package main", `import vukaweb "github.com/vuka-lang/web/v2"`, "vukaweb.RestartOnlyEnv(", "\t\t\"A\",\n\t\t\"B\",\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if empty := string(restartSrcFile(RestartReport{})); strings.Contains(empty, "import") || !strings.Contains(empty, "package main") {
		t.Errorf("an empty file:\n%s", empty)
	}
}

func TestRestartAnalysisIsCached(t *testing.T) {
	a := []restartSrc{{"example.com/app", "a.go", []byte("package main\n" + webImport + "var x = web.Env(\"ONE\", 1)\n")}}
	b := []restartSrc{{"example.com/app", "a.go", []byte("package main\n" + webImport + "var x = web.Env(\"TWO\", 1)\n")}}
	for i, tc := range []struct {
		srcs []restartSrc
		want string
	}{{a, "ONE"}, {a, "ONE"}, {b, "TWO"}, {a, "ONE"}} {
		if got := analyzeRestartCached(tc.srcs).Keys; !slices.Equal(got, []string{tc.want}) {
			t.Errorf("run %d: %v, want %s", i, got, tc.want)
		}
	}
}

// An app built with a web that has no RestartOnlyEnv gets nothing
// registered: the generated file would not compile.
func TestRestartKeysNeedAWebThatRegisters(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	for _, tc := range []struct {
		name, web string
		want      bool
	}{
		{"with", "package web\n\nfunc RestartOnlyEnv(keys ...string) {}\n", true},
		{"without", "package web\n\nfunc Env(key string) string { return \"\" }\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			write(t, filepath.Join(dir, "web", "go.mod"), "module github.com/vuka-lang/web/v2\n\ngo 1.23\n")
			write(t, filepath.Join(dir, "web", "web.go"), tc.web)
			root := filepath.Join(dir, "app")
			write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire github.com/vuka-lang/web/v2 v2.0.0\n\nreplace github.com/vuka-lang/web/v2 => ../web\n")
			write(t, filepath.Join(root, "main.go"), "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nvar _ = web.Env\n\nfunc main() {}\n")
			if got := webRegistersRestart(root, "github.com/vuka-lang/web/v2"); got != tc.want {
				t.Errorf("webRegistersRestart = %v, want %v", got, tc.want)
			}
		})
	}
}
