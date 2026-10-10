package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// templModule writes a module requiring templ, the local vuka and the local
// ui (testModule) with files, and returns its directory.
func templModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "site")
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/site")
	for name, src := range files {
		writeFile(t, filepath.Join(root, name), src)
	}
	r, _ := filepath.EvalSymlinks(root)
	return r
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
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	gens, overlay, err := load.Transpile(pkgs, t.TempDir(), load.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var tg *load.Generated
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
	if _, err := load.Mirror(root, out, gens, false); err != nil {
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
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].ImportPath != "example.com/site/ui" || len(pkgs[0].Templ) != 1 {
		t.Fatalf("packages: %+v", pkgs)
	}
	_, overlay, err := load.Transpile(pkgs, t.TempDir(), load.Options{})
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
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("packages: %+v", pkgs)
	}
	_, _, err = load.Transpile(pkgs, t.TempDir(), load.Options{})
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

	"github.com/vuka-lang/ui"
)

func Note(text string, children ui.Node) ui.Node { return <p>{text}: {children}</p> }

func main() {
	page := <Card title="Hi"><Note text="note">body</Note></Card>
	page.Render(context.Background(), os.Stdout)
}
`,
	})
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, overlay, err := load.Transpile(pkgs, t.TempDir(), load.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := goRun(t, root, overlay, "."), `<div class="card"><h2>Hi</h2><p>note: body</p></div>`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

const filesVuka = `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/a-h/templ"
	"github.com/vuka-lang/ui/templx"
)

func View(f vuka.File) func(*vuka.Decl) {
	return func(d *vuka.Decl) {
		b, err := f.Bytes()
		fmt.Printf("%s %s %q %v\n", d.Name, f, b, err)
		comps, _ := templx.Components(f)
		for _, c := range comps {
			fmt.Println(c.Name, c.Params)
			switch fn := c.Func.(type) {
			case func(string) templ.Component:
				fn(d.Name).Render(context.Background(), os.Stdout)
				fmt.Println()
			case func(string, int) templ.Component:
				fn(d.Name, 2).Render(context.Background(), os.Stdout)
				fmt.Println()
			}
		}
	}
}

@View("views/pet.html")
func pet() {}

@View("pet.templ")
func page() {}

@View("views/cards/card.templ")
func card() {}

func main() {}
`

const petTempl = `package main

templ Show(name string) {
	<p>{ name }</p>
}

templ list() {
	<ul></ul>
}
`

const cardTempl = `package cards

templ Card(title string, n int) {
	<b>{ title }</b>@stars(n)
}

templ stars(n int) {
	<i>{ n }</i>
}
`

const filesWant = `main.pet example.com/site:views/pet.html "<h1>pet</h1>\n" <nil>
main.page example.com/site:pet.templ "package main\n\ntempl Show(name string) {\n\t<p>{ name }</p>\n}\n\ntempl list() {\n\t<ul></ul>\n}\n" <nil>
Show [name]
<p>main.page</p>
list []
main.card example.com/site/views/cards:card.templ "package cards\n\ntempl Card(title string, n int) {\n\t<b>{ title }</b>@stars(n)\n}\n\ntempl stars(n int) {\n\t<i>{ n }</i>\n}\n" <nil>
Card [title n]
<b>main.card</b><i>2</i>
`

// TestFiles: vuka.File literals are embedded, with go run's overlay, in the
// build module, and in place; a .templ file's components are registered, with
// their parameters' names, also for a .templ file in a subdirectory's package.
func TestFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"main.vuka":              filesVuka,
		"pet.templ":              petTempl,
		"views/pet.html":         "<h1>pet</h1>\n",
		"views/cards/card.templ": cardTempl,
	})
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if one, err := load.Discover(root, "example.com/site", root, false, nil); err != nil || len(one) != 2 || one[1].ImportPath != "example.com/site/views/cards" {
		t.Fatalf("Discover of one directory doesn't bring the .templ package it names: %v", err)
	}
	gens, overlay, err := load.Transpile(pkgs, t.TempDir(), load.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var refs []transpile.FileRef
	for _, g := range gens {
		if filepath.Base(g.Source) == "main.vuka" {
			refs = g.Files
		}
	}
	type want struct{ lit, path, comps string }
	wants := []want{
		{`"views/pet.html"`, "views/pet.html", "[]"},
		{`"pet.templ"`, "pet.templ", "[{Show [name]} {list []}]"},
		{`"views/cards/card.templ"`, "views/cards/card.templ", "[{Card [title n]}]"},
	}
	if len(refs) != len(wants) {
		t.Fatalf("Files: %+v", refs)
	}
	for i, w := range wants {
		r, off := refs[i], strings.Index(filesVuka, w.lit)
		if r.Off != off || r.End != off+len(w.lit) || r.Path != filepath.Join(root, filepath.FromSlash(w.path)) || fmt.Sprint(r.Components) != w.comps {
			t.Errorf("Files[%d] = %+v, want %s at %d, %s", i, r, w.lit, off, w.comps)
		}
	}
	if got := goRun(t, root, overlay, "."); got != filesWant {
		t.Fatalf("go run with the overlay:\n%s\nwant:\n%s", got, filesWant)
	}

	out := filepath.Join(root, "build")
	if _, err := load.Mirror(root, out, gens, false); err != nil {
		t.Fatal(err)
	}
	if got := plainRun(t, out); got != filesWant {
		t.Fatalf("go run in the build module:\n%s", got)
	}

	for _, g := range gens {
		if err := os.WriteFile(g.Target, g.Src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := plainRun(t, root); got != filesWant {
		t.Fatalf("go run in place:\n%s", got)
	}
}

func plainRun(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("go", "run", ".")
	c.Dir = dir
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}

// TestFileErrors: a missing file, one outside the package's directory (even
// inside the module), a .templ file in a subdirectory that is another module or
// whose package clause isn't its directory's, and one beside the package with
// another package clause are errors at the literal.
func TestFileErrors(t *testing.T) {
	root := templModule(t, map[string]string{
		"app/main.vuka": `package main

func View(f vuka.File) func(*vuka.Decl) { return func(*vuka.Decl) {} }

@View("missing.html")
func a() {}

@View("../shared.html")
func b() {}

@View("ext/card.templ")
func c() {}

@View("mixed/card.templ")
func d() {}

@View("other.templ")
func e() {}

func main() {}
`,
		"shared.html":          "x",
		"app/ext/go.mod":       "module example.com/ext\n",
		"app/ext/card.templ":   "package ext\n\ntempl Card() {\n\t<p></p>\n}\n",
		"app/mixed/card.templ": "package cards\n\ntempl Card() {\n\t<p></p>\n}\n",
		"app/mixed/mixed.go":   "package mixed\n",
		"app/other.templ":      "package other\n\ntempl Card() {\n\t<p></p>\n}\n",
	})
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = load.Transpile(pkgs, t.TempDir(), load.Options{})
	var list transpile.ErrorList
	if !errors.As(err, &list) || len(list) != 5 {
		t.Fatalf("want five errors, got %v", err)
	}
	for i, want := range []string{
		"5:7: no file \"missing.html\" in the package's directory",
		"8:7: the vuka.File \"../shared.html\" is outside",
		"11:7: the .templ file \"ext/card.templ\": " + filepath.Join(root, "app/ext") + " is in another module",
		"14:7: the .templ file \"mixed/card.templ\": it is package cards; the directory's Go files are package mixed",
		"17:7: the .templ file \"other.templ\" is not in this package",
	} {
		if !strings.Contains(list[i].Error(), want) {
			t.Errorf("error %d: %v, want %s", i, list[i], want)
		}
	}
}
