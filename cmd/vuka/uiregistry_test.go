package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// copyRegistry copies the registry built in to a directory.
func copyRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := fs.WalkDir(registryFS, "registry", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := registryFS.ReadFile(p)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(dir, strings.TrimPrefix(p, "registry/")), string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// serveRegistry serves dir over HTTPS at /<ref>/registry/ and makes it vuka's
// own registry at ref; it counts the requests.
func serveRegistry(t *testing.T, dir, ref string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	files := http.StripPrefix("/"+ref+"/registry", http.FileServer(http.Dir(dir)))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/"+ref+"/registry/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	client, def, refFn := registryHTTP, defaultRegistry, registryRef
	t.Cleanup(func() { registryHTTP, defaultRegistry, registryRef = client, def, refFn })
	registryHTTP = srv.Client()
	defaultRegistry = srv.URL + "/%s/registry"
	registryRef = func() string { return ref }
	return srv, &n
}

// writeIndex writes a registry's index into dir, each file's digest that
// of the file in dir.
func writeIndex(t *testing.T, dir string, r *registry) {
	t.Helper()
	sum := func(f *registryFile) {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path))); err == nil && f.SHA256 == "" {
			f.SHA256 = sha256Hex(b)
		}
	}
	for i := range r.Items {
		for j := range r.Items[i].Files {
			sum(&r.Items[i].Files[j])
		}
	}
	for k, s := range r.Styles {
		sum(&s)
		r.Styles[k] = s
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "index.json"), string(b))
}

// smallRegistry is a registry in a directory: a, using b, using c, and
// the style and cn of vuka's.
func smallRegistry(t *testing.T) (string, *registry) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(dir, "kit", n+".go"), "package design\n\nconst "+strings.ToUpper(n)+" = 1\n")
	}
	cn, _ := registryFS.ReadFile("registry/ui/cn.go")
	writeFile(t, filepath.Join(dir, "kit/cn.go"), string(cn))
	css, _ := registryFS.ReadFile("registry/styles/default.css")
	writeFile(t, filepath.Join(dir, "styles/default.css"), string(css))
	r := &registry{Schema: 1, Name: "acme", Package: "design", Styles: map[string]registryFile{"default": {Path: "styles/default.css"}},
		Items: []registryItem{
			{Name: "cn", Title: "cn", Description: "the merge", Files: []registryFile{{Path: "kit/cn.go"}}},
			{Name: "a", Title: "A", Description: "a", Tier: 1, Files: []registryFile{{Path: "kit/a.go"}}, Deps: []string{"b"}},
			{Name: "b", Title: "B", Description: "b", Tier: 1, Files: []registryFile{{Path: "kit/b.go"}}, Deps: []string{"c", "cn"}},
			{Name: "c", Title: "C", Description: "c", Tier: 1, Files: []registryFile{{Path: "kit/c.go"}}, Requires: map[string]string{"ui": "v2.10.0"}},
		}}
	writeIndex(t, dir, r)
	return dir, r
}

func TestUIRegistryFetch(t *testing.T) {
	reg := copyRegistry(t)
	srv, hits := serveRegistry(t, reg, "v2.99.0")
	dir := uiApp(t)
	out := mustUI(t, dir, "init")
	if want := "registry: " + srv.URL + "/v2.99.0/registry\nwrote styles/app.css: the design tokens\nwrote components/ui/cn.go\n"; !strings.HasPrefix(out, want) {
		t.Errorf("init from the registry said:\n%s", out)
	}
	out = mustUI(t, dir, "add", "dialog")
	if want := "registry: " + srv.URL + "/v2.99.0/registry\nwrote components/ui/button.vuka.html\nwrote components/ui/dialog.vuka.html\n"; !strings.HasPrefix(out, want) {
		t.Errorf("add from the registry said:\n%s", out)
	}
	want, _ := registryFS.ReadFile("registry/ui/dialog.vuka.html")
	if got := read(t, filepath.Join(dir, "components/ui/dialog.vuka.html")); got != string(want) {
		t.Errorf("the dialog isn't the registry's")
	}

	// --offline reads the copy built in.
	before := hits.Load()
	out = mustUI(t, dir, "add", "--offline", "badge")
	if hits.Load() != before || strings.Contains(out, "registry") || !strings.HasPrefix(out, "wrote components/ui/badge.vuka.html\n") {
		t.Errorf("add --offline (%d requests) said:\n%s", hits.Load()-before, out)
	}

	// A file whose digest isn't the index's is refused, not replaced by
	// the copy built in.
	writeFile(t, filepath.Join(reg, "ui/card.vuka.html"), "<div>changed</div>\n")
	_, err := runUI(t, dir, "add", "card")
	if err == nil || !strings.Contains(err.Error(), "vuka ui: "+srv.URL+"/v2.99.0/registry/ui/card.vuka.html: its sha256 is ") || exists(dir, "components/ui/card.vuka.html") {
		t.Errorf("a file changed after its index: %v", err)
	}

	// A registry vuka can't fetch: the copy built in, said so.
	os.Remove(filepath.Join(reg, "ui/alert.vuka.html"))
	out = mustUI(t, dir, "add", "alert")
	if want := "couldn't fetch the registry (GET " + srv.URL + "/v2.99.0/registry/ui/alert.vuka.html: 404 Not Found): used the copy built into vuka\nwrote components/ui/alert.vuka.html\n"; !strings.HasPrefix(out, want) {
		t.Errorf("add with a file missing said:\n%s", out)
	}
	registryRef = func() string { return "v2.98.0" }
	out = mustUI(t, dir, "add", "label")
	if want := "couldn't fetch the registry (GET " + srv.URL + "/v2.98.0/registry/index.json: 404 Not Found): used the copy built into vuka\nwrote components/ui/label.vuka.html\n"; !strings.HasPrefix(out, want) {
		t.Errorf("add with no registry at the version said:\n%s", out)
	}
	// A build of a checkout uses its own copy.
	registryRef = func() string { return "" }
	before = hits.Load()
	if out := mustUI(t, dir, "add", "input"); hits.Load() != before || strings.Contains(out, "registry") {
		t.Errorf("a build of a checkout fetched: %s", out)
	}
}

func TestRefOf(t *testing.T) {
	for v, want := range map[string]string{
		"v2.11.0":                                     "v2.11.0",
		"v2.11.0-rc.1":                                "v2.11.0-rc.1",
		"v2.10.1-0.20261029073925-8ba597307921":       "8ba597307921",
		"v2.0.0-20261029073925-8ba597307921":          "8ba597307921",
		"v2.11.0-rc.1.0.20261029073925-8ba597307921":  "8ba597307921",
		"v2.10.1-0.20261029073925-8ba597307921+dirty": "",
		"(devel)": "",
		"":        "",
	} {
		if got := refOf(v); got != want {
			t.Errorf("refOf(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestUIRegistryOther(t *testing.T) {
	reg, _ := smallRegistry(t)
	dir := uiApp(t)
	mustUI(t, dir, "init", "--registry", reg)
	rel, _ := filepath.Rel(dir, reg)
	if toml := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(toml, "registry = \""+filepath.ToSlash(rel)+"\"\n") {
		t.Errorf("init --registry didn't record it:\n%s", toml)
	}
	if out := mustUI(t, dir, "list"); !strings.Contains(out, "a        browser  a\n") || strings.Contains(out, "button") {
		t.Errorf("list from [ui] registry:\n%s", out)
	}

	// The items a uses, each before what uses it.
	out := mustUI(t, dir, "add", "a")
	if want := "registry: " + reg + "\nwrote components/ui/c.go\nwrote components/ui/b.go\nwrote components/ui/a.go\n"; !strings.HasPrefix(out, want) {
		t.Errorf("add a said:\n%s", out)
	}
	if out := mustUI(t, dir, "add", "a"); out != "registry: "+reg+"\ncomponents/ui/a.go is up to date\n" {
		t.Errorf("add a again said:\n%s", out)
	}

	// The diff: a changed file, the rest the same.
	writeFile(t, filepath.Join(dir, "components/ui/b.go"), "package design\n\nconst B = 2\n")
	out = mustUI(t, dir, "diff")
	want := "registry: " + reg + "\n--- components/ui/b.go\n+++ components/ui/b.go (vuka ui add)\n@@ -1,3 +1,3 @@\n package design\n \n-const B = 2\n+const B = 1\n\nvuka ui add -f b takes the registry's; cn, a, c: the same as the registry's\n"
	if out != want {
		t.Errorf("diff said:\n%s\nwant:\n%s", out, want)
	}
	if out := mustUI(t, dir, "diff", "c", "A"); out != "registry: "+reg+"\nc, a: the same as the registry's\n" {
		t.Errorf("diff c A said:\n%s", out)
	}
	os.Remove(filepath.Join(dir, "components/ui/c.go"))
	if out := mustUI(t, dir, "diff", "c"); out != "registry: "+reg+"\nc: the app hasn't components/ui/c.go\nvuka ui add -f c takes the registry's\n" {
		t.Errorf("diff c without c.go said:\n%s", out)
	}
	if _, err := runUI(t, dir, "diff", "d"); err == nil || !strings.Contains(err.Error(), `vuka ui diff: no component "d"`) {
		t.Errorf("diff d: %v", err)
	}
	if out := mustUI(t, dir, "add", "-f", "b"); !strings.Contains(out, "wrote components/ui/b.go\n") || read(t, filepath.Join(dir, "components/ui/b.go")) != "package design\n\nconst B = 1\n" {
		t.Errorf("add -f b:\n%s", out)
	}

	// --offline reads vuka's own.
	if out := mustUI(t, dir, "list", "--offline"); !strings.Contains(out, "button") {
		t.Errorf("list --offline:\n%s", out)
	}
	for args, msg := range map[string]string{
		"list --offline --registry " + reg:     "--offline reads the registry built into vuka; it takes no --registry",
		"list --registry http://example.com/r": "the registry http://example.com/r: an https:// URL or a directory",
		"list --registry nowhere":              "the registry nowhere: no such directory",
		"diff --dry-run":                       "vuka ui diff: no flag --dry-run",
	} {
		if _, err := runUI(t, dir, strings.Fields(args)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("vuka ui %s: %v, want %q", args, err, msg)
		}
	}
}

func TestUIRegistryRefuses(t *testing.T) {
	dir := uiApp(t)
	mustUI(t, dir, "init")
	try := func(edit func(r *registry), args ...string) error {
		t.Helper()
		reg, r := smallRegistry(t)
		edit(r)
		writeIndex(t, reg, r)
		_, err := runUI(t, dir, append([]string{"add", "--registry", reg}, args...)...)
		return err
	}
	for name, c := range map[string]struct {
		edit func(r *registry)
		msg  string
	}{
		"escape": {func(r *registry) {
			r.Items[1].Files[0] = registryFile{Path: "kit/../../evil.go", SHA256: strings.Repeat("0", 64)}
		}, `item a: file "kit/../../evil.go": a path dir/name, inside the components directory`},
		"up": {func(r *registry) {
			r.Items[1].Files[0] = registryFile{Path: "../evil.go", SHA256: strings.Repeat("0", 64)}
		}, `file "../evil.go": a path dir/name`},
		"absolute": {func(r *registry) {
			r.Items[1].Files[0] = registryFile{Path: "/etc/evil.go", SHA256: strings.Repeat("0", 64)}
		}, `file "/etc/evil.go": a path dir/name`},
		"top": {func(r *registry) {
			r.Items[1].Files[0] = registryFile{Path: "evil.go", SHA256: strings.Repeat("0", 64)}
		}, `file "evil.go": a path dir/name`},
		"digest":   {func(r *registry) { r.Items[1].Files[0].SHA256 = "abc" }, "file kit/a.go: its sha256, 64 hex digits"},
		"schema":   {func(r *registry) { r.Schema = 2 }, "schema 2, newer than this vuka reads (1): vuka upgrade"},
		"noschema": {func(r *registry) { r.Schema = 0 }, `no "schema"`},
		"library":  {func(r *registry) { r.Items[3].Requires = map[string]string{"orm": "v1.0.0"} }, "item c requires orm: the libraries are vuka, ui and web"},
		"cycle":    {func(r *registry) { r.Items[3].Deps = []string{"a"} }, "the registry's items use each other in a circle: a → b → c → a"},
		"missing":  {func(r *registry) { r.Items[3].Deps = []string{"z"} }, "the registry's c uses z, which it hasn't"},
		"newer": {func(r *registry) {
			r.Items[1].Requires = map[string]string{"ui": "v2.12.0", "web": "v2.11.0"}
			r.Items[2].Requires = map[string]string{"ui": "v2.11.0"}
		}, "vuka ui add: b, a need github.com/vuka-lang/ui/v2 v2.12.0 or newer; the app requires v2.10.0; a needs github.com/vuka-lang/web/v2 v2.11.0 or newer; the app doesn't require it. Upgrade first, then add again:\n\n\tgo get github.com/vuka-lang/ui/v2@v2.12.0 github.com/vuka-lang/web/v2@v2.11.0"},
	} {
		if err := try(c.edit, "a"); err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: %v, want %q", name, err, c.msg)
		}
	}
	if exists(dir, "components/ui/a.go") || exists(dir, "components/ui/c.go") {
		t.Errorf("a refused add wrote files")
	}

	// A module the app replaces is taken as it is; one it requires at a
	// newer version is enough.
	writeFile(t, filepath.Join(dir, "go.mod"), uiGoMod+"\nreplace github.com/vuka-lang/ui/v2 => ../ui\n")
	if err := try(func(r *registry) { r.Items[3].Requires = map[string]string{"ui": "v2.12.0"} }, "a"); err != nil {
		t.Errorf("with ui replaced: %v", err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), strings.Replace(uiGoMod, "v2.10.0", "v2.12.1-0.20261101000000-abcdefabcdef", 1))
	if err := try(func(r *registry) { r.Items[3].Requires = map[string]string{"ui": "v2.12.0"} }, "-f", "a"); err != nil {
		t.Errorf("with a newer ui: %v", err)
	}
}
