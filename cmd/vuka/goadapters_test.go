package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGoCallAdapters: a package written in Go gets an adapter for each
// signature its vuka.Wrap calls wrap — a function, a method expression, a
// function literal — once each, as a .vuka file's call decorators do; a
// package with .vuka files is left to the compiler.
func TestGoCallAdapters(t *testing.T) {
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25.0\n",
		"main.go": `package main

import (
	"context"

	"github.com/vuka-lang/vuka"
)

func logged(c *vuka.Call) { c.Next() }

type Store struct{}

func (s *Store) save(ctx context.Context, n int) error { return nil }

func add(a, b int) int { return a + b }

var (
	Add   = vuka.Wrap(add, vuka.FuncInfo{Name: "main.add"}, logged)
	Save  = vuka.Wrap((*Store).save, vuka.FuncInfo{Name: "Store.save", Method: true}, logged)
	Hi    = vuka.Wrap(func(name string) string { return "hi " + name }, vuka.FuncInfo{Name: "hi"}, logged)
	Again = vuka.Wrap(add, vuka.FuncInfo{Name: "again"}, logged)
)

func main() {}
`,
		"mixed/a.go":   "package mixed\n\nimport \"github.com/vuka-lang/vuka\"\n\nvar F = vuka.Wrap(func() {}, vuka.FuncInfo{}, nil)\n",
		"mixed/b.vuka": "package mixed\n",
	})
	gens := goCallAdapters(root)
	if len(gens) != 1 || gens[0].Target != filepath.Join(root, "vuka_calls_gen.go") {
		t.Fatalf("generated %v, want the root package's vuka_calls_gen.go", gens)
	}
	src := string(gens[0].Src)
	for _, want := range []string{
		"vuka.RegisterAdapter(func(next func(int, int) int, call vuka.Caller) func(int, int) int {",
		"vuka.RegisterAdapter(func(next func(*Store, context.Context, int) error, call vuka.Caller) func(*Store, context.Context, int) error {",
		"vuka.RegisterAdapter(func(next func(string) string, call vuka.Caller) func(string) string {",
		"import (\n\t\"context\"\n\n\t\"github.com/vuka-lang/vuka\"\n)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("lacks %s:\n%s", want, src)
		}
	}
	if n := strings.Count(src, "RegisterAdapter("); n != 3 {
		t.Errorf("%d adapters, want 3:\n%s", n, src)
	}
}
