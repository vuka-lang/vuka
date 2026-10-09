package load

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// templModule writes a module requiring the runtime (from this repo) and
// templ at the version vuka builds with, its go.sum taken from this repo's.
func templModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	root := filepath.Join(t.TempDir(), "site")
	write(t, filepath.Join(root, "go.mod"), "module example.com/site\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/vuka v0.0.0\n\tgithub.com/a-h/templ "+templVersion(t, repo)+"\n)\n\nreplace github.com/vuka-lang/vuka => "+repo+"\n")
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "go.sum"), string(sum))
	for name, src := range files {
		write(t, filepath.Join(root, name), src)
	}
	return realPath(root)
}

func templVersion(t *testing.T, repo string) string {
	mod, _ := os.ReadFile(filepath.Join(repo, "go.mod"))
	for _, line := range strings.Split(string(mod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == TemplPath {
			return f[1]
		}
	}
	t.Fatal("go.mod doesn't require templ")
	return ""
}

func goRun(t *testing.T, root, overlay string, args ...string) string {
	t.Helper()
	c := exec.Command("go", append([]string{"run", "-overlay=" + overlay}, args...)...)
	c.Dir = root
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}

const helloTempl = `package main

templ Hello(name string) {
	<p class="greeting">Hello, { name }!</p>
}
`

// TestTempl: a .vuka file calls a component of a .templ file beside it, with
// no templ generate; a stale *_templ.go on disk is ignored.
func TestTempl(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"hello.templ":    helloTempl,
		"hello_templ.go": "package main\n\nthis is stale and doesn't parse\n",
		"main.vuka": `package main

import (
	"context"
	"os"
)

func render(name string) Result[int] {
	Hello(name).Render(context.Background(), os.Stdout)?
	return Ok(1)
}

func main() {
	render(shout("vuka")).Unwrap()
	os.Stdout.WriteString("\n")
}
`,
		"util.go": "package main\n\nimport \"strings\"\n\nfunc shout(s string) string { return strings.ToUpper(s) }\n",
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	gens, overlay, err := Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var tg *Generated
	for i, g := range gens {
		if strings.HasSuffix(g.Source, ".templ") {
			tg = &gens[i]
		}
	}
	if tg == nil || tg.Target != filepath.Join(root, "hello_templ.go") || tg.Map != nil {
		t.Fatalf("no generated Go for hello.templ: %+v", gens)
	}
	if got, want := goRun(t, root, overlay, "."), `<p class="greeting">Hello, VUKA!</p>`+"\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// The build module holds the generated Go, which plain go builds.
	out := filepath.Join(root, "build")
	if _, err := Mirror(root, out, gens, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "hello_templ.go"))
	if err != nil || !strings.Contains(string(got), "func Hello(name string) templ.Component") {
		t.Fatalf("mirror's hello_templ.go: %v\n%s", err, got)
	}
}

// TestTemplOnly: a package of .templ (and .go) files alone builds through
// vuka, imported by another package.
func TestTemplOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"ui/card.templ": `package ui

templ Card(title string) {
	<div class="card">
		<h2>{ title }</h2>
		{ children... }
	</div>
}
`,
		"main.go": `package main

import (
	"context"
	"os"

	"github.com/a-h/templ"

	"example.com/site/ui"
)

func main() {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>body</p>"))
	ui.Card("Hi").Render(ctx, os.Stdout)
}
`,
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].ImportPath != "example.com/site/ui" || len(pkgs[0].Templ) != 1 {
		t.Fatalf("packages: %+v", pkgs)
	}
	_, overlay, err := Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := goRun(t, root, overlay, "."), `<div class="card"><h2>Hi</h2><p>body</p></div>`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestTemplError: templ's parse errors come out positioned in the .templ.
func TestTemplError(t *testing.T) {
	root := templModule(t, map[string]string{
		"broken.templ": "package main\n\ntempl Broken() {\n\t<div>\n\t\t<span>open\n\t</div>\n}\n",
		"main.vuka":    "package main\n\nfunc main() { Broken() }\n",
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("packages: %+v", pkgs)
	}
	_, _, err = Transpile(pkgs, t.TempDir(), Options{})
	var list transpile.ErrorList
	if !errors.As(err, &list) || len(list) != 1 {
		t.Fatalf("want one positioned error, got %v", err)
	}
	e := list[0]
	if e.Pos.Filename != filepath.Join(root, "broken.templ") || e.Pos.Line < 3 || e.Pos.Column < 1 {
		t.Fatalf("error at %v: %s", e.Pos, e.Msg)
	}
	t.Log(e)
}

// TestTemplChildren: JSX children reach a .templ component's { children... }
// through templ's context.
func TestTemplChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"card.templ": `package main

templ Card(title string) {
	<div class="card"><h2>{ title }</h2>{ children... }</div>
}
`,
		"main.vuka": `package main

import (
	"context"
	"os"
)

func Note(text string, children vuka.Node) vuka.Node { return <p>{text}: {children}</p> }

func main() {
	page := <Card title="Hi"><Note text="note">body</Note></Card>
	page.Render(context.Background(), os.Stdout)
}
`,
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, overlay, err := Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := goRun(t, root, overlay, "."), `<div class="card"><h2>Hi</h2><p>note: body</p></div>`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
