package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// restartApp is an app of Go, .vuka and .vuka.html files reading the
// environment in every kind of place; a stub of web records what the
// generated registration gives it. The comments say what each read is.
var restartApp = map[string]string{
	"web/go.mod": "module github.com/vuka-lang/web/v2\n\ngo 1.25.0\n",
	"web/web.go": `// Package web is a stub of the calls the analysis reads.
package web

import (
	"slices"
	"sync"
)

type Option func()
type App struct{}

func New(opts ...Option) *App                                 { return &App{} }
func (a *App) Get(path string, fn any) *App                   { return a }
func (a *App) Cron(name, spec string, fn any) *App            { return a }
func Provide(ctors ...any) Option                             { return nil }
func Require(keys ...string) Option                           { return nil }
func Env[T any](key string, def ...T) T                       { var z T; return z }
func MustEnv[T any](key string) T                             { var z T; return z }
func OnEnvChange(key string, fn func(old, new string)) func() { return func() {} }

type HotValue[T any] struct{}

func Hot[T any](key string, def ...T) *HotValue[T] { return &HotValue[T]{} }
func (h *HotValue[T]) Get() T                      { var z T; return z }

var (
	mu   sync.Mutex
	only []string
)

func RestartOnlyEnv(keys ...string) { mu.Lock(); only = append(only, keys...); mu.Unlock() }

func NeedsRestart(key string) bool {
	mu.Lock()
	defer mu.Unlock()
	return slices.Contains(only, key)
}
`,
	"app/main.go": `package main

import (
	"fmt"
	"strings"

	"github.com/vuka-lang/web/v2"

	"example.com/app/jobs"
	"example.com/app/parts"
	"example.com/app/store"
)

// start: a package variable
var listen = web.Env("LISTEN_ADDR", ":8080")

func main() {
	// start: main, a provider, web.Require
	app := web.New(web.Provide(store.Open), web.Require("REQ_KEY"))
	// live: a handler, a cron's function
	app.Get("/", index).Get("/stats", store.Stats).Cron("tick", "@every 1m", jobs.Tick)
	_ = parts.Counter{}
	_ = lookup("DYNAMIC")
	var asked []string
	for _, k := range []string{"LISTEN_ADDR", "INIT_KEY", "DB_URL", "SHARED_KEY", "REQ_KEY", "BANNER_KEY", "HOT_KEY",
		"STATS_KEY", "JOB_KEY", "RENDER_KEY", "MOUNT_KEY", "EVENT_KEY", "INDEX_KEY", "DYNAMIC"} {
		if web.NeedsRestart(k) {
			asked = append(asked, k)
		}
	}
	fmt.Println("restart:" + strings.Join(asked, ","))
}

// live: a handler
func index() string { _ = web.Env("INDEX_KEY", ""); return helper() }
`,
	"app/shared.go": `package main

import "github.com/vuka-lang/web/v2"

// start from init, live from index: flagged
func helper() string { return web.Env("SHARED_KEY", "") }

// start: init
func init() { _ = web.Env("INIT_KEY", ""); _ = helper() }

// a key the program handles itself
var rate = web.Hot[int]("HOT_KEY", 1)
var rateAtStart = web.Env("HOT_KEY", 1)

// a key that isn't a constant
func lookup(name string) string { return web.Env[string](name) }
`,
	"app/jobs/jobs.go": `package jobs

import "github.com/vuka-lang/web/v2"

func Tick() { _ = web.Env("JOB_KEY", "") }
`,
	"app/store/store.vuka": `package store

import "github.com/vuka-lang/web/v2"

type DB struct{}

func Open() *DB {
	_ = web.Env("DB_URL", "")
	return &DB{}
}

func Stats() string { return web.Env("STATS_KEY", "") }
`,
	"app/parts/counter.vuka.html": `<script lang="go">
package parts

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

var banner = web.Env("BANNER_KEY", "")

type Counter struct {
	ui.Live
	n int
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error {
	c.n = len(web.Env("MOUNT_KEY", ""))
	return nil
}

func (c *Counter) Inc() { c.n += len(web.Env("EVENT_KEY", "")) }
</script>

<button onClick={Inc}>{web.Env("RENDER_KEY", "") + banner}</button>
`,
}

// TestRestartKeysRegistered: the generated registration reaches a built
// program, with the keys the analysis found at start and none of the live
// ones, in Go, .vuka and .vuka.html files; the vet warning counts the
// reads whose key it couldn't read.
func TestRestartKeysRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gomod, gosum := testModule(t, "example.com/app")
	gomod = strings.Replace(gomod, "require (", "require (\n\tgithub.com/vuka-lang/web/v2 "+familyVersion(t, "web"), 1) + "\nreplace github.com/vuka-lang/web/v2 => ../web\n"
	writeFile(t, filepath.Join(root, "app", "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "app", "go.sum"), gosum)
	for name, src := range restartApp {
		writeFile(t, filepath.Join(root, name), src)
	}
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
	t.Chdir(filepath.Join(root, "app"))

	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	want := "restart:LISTEN_ADDR,INIT_KEY,DB_URL,SHARED_KEY,REQ_KEY,BANNER_KEY"
	if got := strings.TrimSpace(out.String()); got != want {
		t.Errorf("the program reports\n%s\nwant\n%s", got, want)
	}

	out.Reset()
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, &out, &errOut); err != nil {
		t.Fatalf("vuka vet: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	if s := errOut.String(); !strings.Contains(s, "W0405") || !strings.Contains(s, "1 web.Env read has a key that isn't a string constant") {
		t.Errorf("vuka vet:\n%s", s)
	}
}
