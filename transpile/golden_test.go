package transpile_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden transpiles each testdata/golden/*.vuka and compares the Go it
// produces with name.golden, or its errors with name.err. A case with a
// name.out file is also compiled and run, and its output compared.
func TestGolden(t *testing.T) {
	cases, _ := filepath.Glob("testdata/golden/*.vuka")
	if len(cases) == 0 {
		t.Fatal("no golden cases")
	}
	for _, path := range cases {
		name := strings.TrimSuffix(filepath.Base(path), ".vuka")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(path)
			res, err := transpile.Package([]transpile.File{{Name: name + ".vuka", Src: src}},
				transpile.Options{Importer: load.NewImporter(dir, runtimeOverlay(t))})
			if err != nil {
				if !strings.HasPrefix(name, "err_") {
					t.Fatalf("only err_ cases may fail:\n%v", err)
				}
				compare(t, filepath.Join(dir, name+".err"), []byte(err.Error()+"\n"))
				return
			}
			if strings.HasPrefix(name, "err_") {
				t.Fatal("an err_ case transpiled without errors")
			}
			out := res.Files[0].Src
			compare(t, filepath.Join(dir, name+".golden"), out)

			want, err := os.ReadFile(filepath.Join(dir, name+".out"))
			if err != nil {
				return
			}
			if got := run(t, out); got != string(want) {
				t.Errorf("output:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

var overlay struct {
	once sync.Once
	path string
	err  error
}

// runtimeOverlay stands in for the JSX runtime while this checkout lacks it:
// an overlay adding its contract (bodies panic) to the runtime package, and
// renaming the Attr[T] helper whose name the contract's Attr type takes. With
// the runtime present it is "".
func runtimeOverlay(t *testing.T) string {
	overlay.once.Do(func() {
		root, _ := filepath.Abs("..")
		if _, err := os.Stat(filepath.Join(root, "node.go")); err == nil {
			return
		}
		dir, err := os.MkdirTemp("", "vuka-golden")
		if err != nil {
			overlay.err = err
			return
		}
		call, err := os.ReadFile(filepath.Join(root, "call.go"))
		if err != nil {
			overlay.err = err
			return
		}
		files := map[string]string{
			"call.go":          strings.Replace(string(call), "func Attr[T any](c *Call)", "func AttrOf[T any](c *Call)", 1),
			"node.go":          jsxRuntimeStub,
			"templx/templx.go": templxStub,
		}
		replace := map[string]string{}
		for name, text := range files {
			p := filepath.Join(dir, strings.ReplaceAll(name, "/", "_"))
			if overlay.err = os.WriteFile(p, []byte(text), 0o644); overlay.err != nil {
				return
			}
			replace[filepath.Join(root, name)] = p
		}
		js, _ := json.Marshal(map[string]any{"Replace": replace})
		overlay.path = filepath.Join(dir, "overlay.json")
		overlay.err = os.WriteFile(overlay.path, js, 0o644)
	})
	if overlay.err != nil {
		t.Fatal(overlay.err)
	}
	return overlay.path
}

const jsxRuntimeStub = `package vuka

import (
	"context"
	"io"
)

type Node interface {
	Render(ctx context.Context, w io.Writer) error
}

type Attr struct {
	Name  string
	Value any
}

func El(tag string, attrs []Attr, children ...Node) Node { panic("stub") }
func Text(v any) Node                                    { panic("stub") }
func Child(v any) Node                                   { panic("stub") }
func Fragment(children ...Node) Node                     { panic("stub") }
func Nodes(build func(add func(Node))) Node              { panic("stub") }
func Try(n Node, err error) Node                         { panic("stub") }
`

const templxStub = `package templx

import "github.com/vuka-lang/vuka"

func WithChildren(c vuka.Node, children vuka.Node) vuka.Node { panic("stub") }
`

func compare(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)\ngot:\n%s", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func run(t *testing.T, src []byte) string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles and runs the program")
	}
	dir := t.TempDir()
	root, _ := filepath.Abs("..")
	mod := "module golden\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => " + root + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}
