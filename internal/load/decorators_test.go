package load

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecoratorsAcrossPackages uses another package's composed decorator,
// which bundles that package's unexported decorator, and its attribute types,
// whose targets are read from the type alone.
func TestDecoratorsAcrossPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	repo, _ := filepath.Abs("../..")
	root := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => "+repo+"\n")
	write(t, filepath.Join(root, "api", "api.vuka"), `package api

import "fmt"

var Routes = map[string]*vuka.Decl{}

func Get(path string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { Routes[path] = d }
}

decorator timed(c) {
	c.Next()
	fmt.Println("timed", c.Name)
}

@vuka.Targets(vuka.OnParam)
type Path string

decorator Route(path string) = @Get(path) @timed

decorator Timed = @timed
`)
	main := `package main

import (
	"fmt"

	"example.com/app/api"
)

@api.Route("/pets/{id}")
func show(@api.Path("id") id string) string { return "pet " + id }

@api.Timed
func ping() string { return "pong" }

func main() {
	d := api.Routes["/pets/{id}"]
	var p api.Path
	d.Params[0].Attr(&p)
	fmt.Println(d.Name, p, show("7"), ping())
}
`
	write(t, filepath.Join(root, "main.vuka"), main)
	run := func() (string, error) {
		pkgs, err := Discover(root, "example.com/app", root, true, nil)
		if err != nil {
			t.Fatal(err)
		}

		_, overlay, err := Transpile(pkgs, t.TempDir(), Options{})
		if err != nil {
			return "", err
		}
		c := exec.Command("go", "run", "-overlay="+overlay, ".")
		c.Dir = root
		c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("go run: %v\n%s", err, out)
		}
		return string(out), nil
	}
	out, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if want := "timed main.show\ntimed main.ping\nmain.show id pet 7 pong\n"; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}

	write(t, filepath.Join(root, "main.vuka"), main+"\n@api.Path(\"x\")\nfunc misplaced() {}\n")
	if _, err := run(); err == nil || !strings.Contains(err.Error(), "@api.Path can't go on a function; api.Path is for parameters") {
		t.Fatalf("misplaced attribute: got %v", err)
	}
}
