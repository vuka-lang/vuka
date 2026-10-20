package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEject ejects the examples: with -rm each .vuka file becomes plain Go
// that go build and go vet take with no vuka command, and the programs
// print what they printed built by vuka.
func TestEject(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	examples := []string{"users", "shapes"}
	for _, ex := range examples {
		src, err := os.ReadFile(filepath.Join("../../examples", ex, "main.vuka"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, ex, "main.vuka"), string(src))
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	bin := t.TempDir()
	want := map[string][]byte{}
	for _, ex := range examples {
		var stderr bytes.Buffer
		if err := runGo("build", []string{"-o", filepath.Join(bin, ex), "./" + ex}, io.Discard, &stderr); err != nil {
			t.Fatalf("vuka build %s: %v\n%s", ex, err, stderr.Bytes())
		}
		out, err := exec.Command(filepath.Join(bin, ex)).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", ex, err, out)
		}
		want[ex] = out
	}

	// One file, without -rm: its generated Go beside it, the source kept.
	var out bytes.Buffer
	if err := eject([]string{"users/main.vuka"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("users/main_vuka.go"); err != nil || !strings.Contains(out.String(), "wrote users/main_vuka.go") {
		t.Fatalf("vuka eject users/main.vuka: %v\n%s", err, out.String())
	}
	if _, err := os.Stat("shapes/main_vuka.go"); err == nil {
		t.Error("ejecting users/main.vuka wrote shapes' Go too")
	}

	out.Reset()
	if err := eject([]string{"-rm", "./..."}, &out); err != nil {
		t.Fatal(err)
	}
	for _, ex := range examples {
		if !strings.Contains(out.String(), "ejected "+ex+"/main.vuka: "+ex+"/main.go") {
			t.Errorf("vuka eject -rm doesn't report %s:\n%s", ex, out.String())
		}
		if _, err := os.Stat(filepath.Join(ex, "main.vuka")); err == nil {
			t.Errorf("%s/main.vuka is still there", ex)
		}
		if _, err := os.Stat(filepath.Join(ex, "main_vuka.go")); err == nil {
			t.Errorf("%s/main_vuka.go is still there", ex)
		}
		src, err := os.ReadFile(filepath.Join(ex, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"//line ", "/*line ", "DO NOT EDIT", root} {
			if bytes.Contains(src, []byte(bad)) {
				t.Errorf("%s/main.go has %q:\n%s", ex, bad, src)
			}
		}
	}
	for _, args := range [][]string{{"build", "-o", bin, "./..."}, {"vet", "./..."}} {
		c := exec.Command("go", args...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}
	for _, ex := range examples {
		got, err := exec.Command(filepath.Join(bin, ex)).CombinedOutput()
		if err != nil || !bytes.Equal(got, want[ex]) {
			t.Errorf("%s built with go build: %v\n%s\nwant:\n%s", ex, err, got, want[ex])
		}
	}
}

// An ejected file has its type's constructor and schema method after the
// type; its init functions stay where the source wrote them.
func TestEjectedArrange(t *testing.T) {
	src := `package main

// Board is a page.
type Board struct{ notes *Notes }

// Open opens a note.
func (b *Board) Open(id int) {}

func init() { register("a") }

func (b *Board) LiveSchema(l *Schema) { l.Mount(b.mount) }

func NewBoard(notes *Notes) *Board { return &Board{notes: notes} }

func init() {
	register("b")
}
`
	want := `package main

// Board is a page.
type Board struct{ notes *Notes }

func NewBoard(notes *Notes) *Board { return &Board{notes: notes} }

func (b *Board) LiveSchema(l *Schema) { l.Mount(b.mount) }

// Open opens a note.
func (b *Board) Open(id int) {}

func init() { register("a") }

func init() {
	register("b")
}
`
	if got := string(ejected([]byte(src))); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// vuka gen's Go keeps its line directives: the moved functions take
	// theirs, and the code after each seam resumes the source's lines.
	gen := `package main

//line board.vuka:3
// Board is a page.
type Board struct{ notes *Notes }

// Open opens a note.
func (b *Board) Open(id int) {}

//line board.vuka:20
func (b *Board) LiveSchema(l *Schema) { l.Mount(b.mount) }

func last() {}
`
	wantGen := `package main

//line board.vuka:3
// Board is a page.
type Board struct{ notes *Notes }

//line board.vuka:20
func (b *Board) LiveSchema(l *Schema) { l.Mount(b.mount) }

//line board.vuka:6
// Open opens a note.
func (b *Board) Open(id int) {}

//line board.vuka:22
func last() {}
`
	if got := string(arrange([]byte(gen))); got != wantGen {
		t.Errorf("gen: got:\n%s\nwant:\n%s", got, wantGen)
	}
}

// Ejecting a package vuka linked into main implicitly imports it there:
// plain go build links only what main imports.
func TestEjectLinksDeclarers(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	writeFile(t, filepath.Join(root, "cli/cli.go"), `package cli

import (
	"sort"

	"github.com/vuka-lang/vuka"
)

var commands = map[string]*vuka.Decl{}

func Command(name string) func(*vuka.Decl) { return func(d *vuka.Decl) { commands[name] = d } }

func Names() []string {
	var out []string
	for n := range commands {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
`)
	writeFile(t, filepath.Join(root, "greet/greet.vuka"), `package greet

import "example.com/app/cli"

@cli.Command("greet")
func greet() string { return "hi" }
`)
	writeFile(t, filepath.Join(root, "main.vuka"), `package main

import (
	"fmt"

	"example.com/app/cli"
)

func main() { fmt.Println(cli.Names()) }
`)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	var out bytes.Buffer
	if err := eject([]string{"-rm", "./greet"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := eject([]string{"-rm", "."}, &out); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile("main.go")
	if !bytes.Contains(src, []byte(`_ "example.com/app/greet"`)) {
		t.Fatalf("main.go doesn't import the ejected package:\n%s\n%s", src, out.String())
	}
	got, err := exec.Command("go", "run", ".").CombinedOutput()
	if err != nil || string(got) != "[greet]\n" {
		t.Errorf("go run .: %v\n%s", err, got)
	}
}
