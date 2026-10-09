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
		if comps, ok := vuka.TemplComponents(f); ok {
			comps["Show"].(func(string) templ.Component)(d.Name).Render(context.Background(), os.Stdout)
			fmt.Println(len(comps))
		}
	}
}

@View("views/pet.html")
func pet() {}

@View("pet.templ")
func page() {}

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

const filesWant = `main.pet example.com/site:views/pet.html "<h1>pet</h1>\n" <nil>
main.page example.com/site:pet.templ "package main\n\ntempl Show(name string) {\n\t<p>{ name }</p>\n}\n\ntempl list() {\n\t<ul></ul>\n}\n" <nil>
<p>main.page</p>2
`

// TestFiles: vuka.File literals are embedded, with go run's overlay, in the
// build module, and in place; a .templ file's components are registered.
func TestFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"main.vuka":      filesVuka,
		"pet.templ":      petTempl,
		"views/pet.html": "<h1>pet</h1>\n",
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
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
// inside the module) and a .templ file of another package are errors at the
// literal.
func TestFileErrors(t *testing.T) {
	root := templModule(t, map[string]string{
		"app/main.vuka": `package main

func View(f vuka.File) func(*vuka.Decl) { return func(*vuka.Decl) {} }

@View("missing.html")
func a() {}

@View("../shared.html")
func b() {}

@View("ui/card.templ")
func c() {}

func main() {}
`,
		"shared.html":      "x",
		"app/ui/card.templ": "package ui\n\ntempl Card() {\n\t<p></p>\n}\n",
	})
	pkgs, err := Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Transpile(pkgs, t.TempDir(), Options{})
	var list transpile.ErrorList
	if !errors.As(err, &list) || len(list) != 3 {
		t.Fatalf("want three errors, got %v", err)
	}
	for i, want := range []string{"5:7: no file \"missing.html\" in the package's directory", "8:7: the vuka.File \"../shared.html\" is outside", "11:7: the .templ file \"ui/card.templ\" must be in this package"} {
		if !strings.Contains(list[i].Error(), want) {
			t.Errorf("error %d: %v, want %s", i, list[i], want)
		}
	}
}
