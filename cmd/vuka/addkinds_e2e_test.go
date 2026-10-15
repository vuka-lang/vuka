package main

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vuka add's kinds on an app vuka new made: the app still tests, builds
// and serves; an entity's migration is the one makemigrations writes, and
// it applies on SQLite as the app starts.
func TestAddKindsEndToEnd(t *testing.T) {
	e := newE2E(t)
	off := []string{"VUKA_TAILWIND=off", "VUKA_FRONTEND=off"}
	dir := e.new("blog", "--auth=no")
	e.run(dir, off, "vuka", "add", "module", "posts")
	e.run(dir, off, "vuka", "add", "service", "posts/Mailer")
	e.run(dir, off, "vuka", "add", "component", "StatCard")
	e.run(dir, off, "vuka", "add", "page", "/reports/{id}")
	e.run(dir, off, "vuka", "add", "page", "/stats", "--live", "--module", "posts")
	out := e.run(dir, off, "vuka", "add", "entity", "posts/Comment", "body:text", "post:ref", "email:string:unique",
		"score:int:index", "approved_at:time?", "price:decimal", "active:bool", "meta:json", "due:date")
	if !strings.Contains(out, "migration written") || !exists(dir, "migrations", "0002_comments.go") {
		t.Fatalf("no migration:\n%s", out)
	}
	e.run(dir, off, "vuka", "add", "entity", "posts/Tag", "name:string:index", "comment:ref?")
	if out := e.run(dir, off, "go", "tool", "orm", "makemigrations", "--check"); !strings.Contains(out, "No changes") {
		t.Errorf("makemigrations --check: %s", out)
	}
	// a page using the component
	os.WriteFile(filepath.Join(dir, "pages", "cards.vuka"), []byte(`package pages

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"

	"example.com/blog/components"
)

@web.Get("/cards")
func Cards() ui.Node { return <components.StatCard Title="Posts"><p>3</p></components.StatCard> }
`), 0o644)
	e.run(dir, off, "vuka", "test", "./...")
	bin := filepath.Join(dir, "blog")
	e.run(dir, off, "vuka", "build", "-o", bin, ".")
	base, stop := serve(t, dir, bin)
	for path, want := range map[string]string{"/reports/7": "Reports", "/stats": "Clicked 0 times", "/cards": "Posts", "/posts": "Posts"} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(b), want) {
			t.Errorf("GET %s: %d\n%s", path, res.StatusCode, b)
		}
	}
	stop()
	if out := e.run(dir, nil, "go", "tool", "orm", "showmigrations"); !strings.Contains(out, "[X] 0002_comments") || !strings.Contains(out, "[X] 0003_tags") {
		t.Errorf("showmigrations: %s", out)
	}
}

// Islands: a component imported into a page type-checks against its props,
// and a page backed by a component compiles, live and stateless.
func TestAddIslandsEndToEnd(t *testing.T) {
	e := newE2E(t)
	off := []string{"VUKA_TAILWIND=off", "VUKA_FRONTEND=off"}
	dir := e.new("isl", "--islands=yes", "--frontend=none", "--auth=no", "--db=none")
	e.run(dir, off, "vuka", "add", "island", "Chart", "--no-install")
	e.run(dir, off, "vuka", "add", "island", "Clock", "--react", "--no-install")
	e.run(dir, off, "vuka", "add", "page", "/reports", "--island", "Reports", "--live", "--no-install")
	e.run(dir, off, "vuka", "add", "page", "/board", "--island", "Board", "--no-install")
	os.WriteFile(filepath.Join(dir, "pages", "charts.vuka"), []byte(`package pages

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"

	Chart "../web/islands/Chart.vue"
	Clock "../web/islands/Clock.tsx"
)

@web.Get("/charts")
func Charts() ui.Node {
	return <main><Chart title="Sales" count={3} client:load /><Clock title="Now" client:load /></main>
}
`), 0o644)
	e.run(dir, off, "vuka", "vet", "./...")
	// a prop the component doesn't declare is a compile error
	os.WriteFile(filepath.Join(dir, "pages", "charts.vuka"), []byte(strings.Replace(read(t, filepath.Join(dir, "pages", "charts.vuka")), `count={3}`, `colour="red"`, 1)), 0o644)
	c := e.cmd(dir, off, "vuka", "vet", "./...")
	if out, err := c.CombinedOutput(); err == nil || !strings.Contains(string(out), "colour") {
		t.Errorf("an unknown prop compiled: %v\n%s", err, out)
	}
}

// cmd is a command as run runs it, not yet started.
func (e *e2e) cmd(dir string, env []string, name string, args ...string) *exec.Cmd {
	if name == "vuka" {
		name = e.vuka
	}
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), append([]string{"GOWORK=off", "PATH=" + filepath.Dir(e.vuka) + string(os.PathListSeparator) + os.Getenv("PATH")}, env...)...)
	return c
}
