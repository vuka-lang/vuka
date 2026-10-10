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

const filesVuka = `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/a-h/templ"
)

func View(f vuka.File) func(*vuka.Decl) {
	return func(d *vuka.Decl) {
		b, err := f.Bytes()
		fmt.Printf("%s %s %q %v\n", d.Name, f, b, err)
		comps, _ := vuka.TemplComponents(f)
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
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if one, err := Discover(root, "example.com/site", root, false, nil); err != nil || len(one) != 2 || one[1].ImportPath != "example.com/site/views/cards" {
		t.Fatalf("Discover of one directory doesn't bring the .templ package it names: %v", err)
	}
	gens, overlay, err := Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := goRun(t, root, overlay, "."); got != filesWant {
		t.Fatalf("go run with the overlay:\n%s\nwant:\n%s", got, filesWant)
	}

	out := filepath.Join(root, "build")
	if _, err := Mirror(root, out, gens, false); err != nil {
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
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Transpile(pkgs, t.TempDir(), Options{})
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
