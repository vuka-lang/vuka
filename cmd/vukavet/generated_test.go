package main

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/packages"
)

// TestGeneratedLint runs analyzers gopls runs over the Go the transpiler
// writes for golden cases: what they report shows on the user's .vuka lines.
func TestGeneratedLint(t *testing.T) {
	golden := filepath.Join("..", "..", "transpile", "testdata", "golden")
	for _, name := range []string{"infer_args", "match_default"} {
		src, err := os.ReadFile(filepath.Join(golden, name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if got := lint(t, map[string]string{name + ".go": string(src)}); len(got) > 0 {
			t.Errorf("%s: generated Go has diagnostics:\n%s", name, strings.Join(got, "\n"))
		}
	}
}

// TestGeneratedLintReports shows the analyzers report what the transpiler
// used to write.
func TestGeneratedLintReports(t *testing.T) {
	got := lint(t, map[string]string{"old.go": `package main

import (
	"errors"

	vuka "github.com/vuka-lang/vuka/v2"
)

func attach() (int64, error) { return 0, errors.New("x") }

func old() vuka.Option[int64] {
	n, err := attach()
	if err != nil {
		return vuka.None[int64]()
	}
	return vuka.Some[int64](n)
}

func match() string {
	switch n, err := attach(); {
	case err == nil:
		_ = n
		return "ok"
	case err != nil:
		return "err"
	default:
		panic("vuka: no case matched")
	}
}

func main() { old(); match() }
`})
	want := []string{"old.go:16:9: unnecessary type arguments", "old.go:24:11: tautological condition: non-nil != nil"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("got %q, want %q", got[i], w)
		}
	}
}

func lint(t *testing.T, files map[string]string) []string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = "module lint\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\nreplace github.com/vuka-lang/vuka/v2 => " + root + "\n"
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: dir, Env: append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("the package doesn't load")
	}
	g, err := checker.Analyze([]*analysis.Analyzer{nilness.Analyzer, inferTypeArgs}, pkgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for act := range g.All() {
		if !act.IsRoot {
			continue
		}
		for _, d := range act.Diagnostics {
			out = append(out, act.Package.Fset.Position(d.Pos).String()+": "+d.Message)
		}
	}
	slices.Sort(out)
	return out
}

// inferTypeArgs is gopls's infertypeargs check: explicit type arguments of
// a call that Go infers alike without them.
var inferTypeArgs = &analysis.Analyzer{
	Name: "infertypeargs",
	Doc:  "type arguments Go would infer",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var fun ast.Expr
				switch ix := call.Fun.(type) {
				case *ast.IndexExpr:
					fun = ix.X
				case *ast.IndexListExpr:
					fun = ix.X
				default:
					return true
				}
				id, _ := fun.(*ast.Ident)
				if sel, ok := fun.(*ast.SelectorExpr); ok {
					id = sel.Sel
				}
				inst, ok := pass.TypesInfo.Instances[id]
				if id == nil || !ok {
					return true
				}
				bare := &ast.CallExpr{Fun: fun, Lparen: call.Lparen, Args: call.Args, Ellipsis: call.Ellipsis, Rparen: call.Rparen}
				info := &types.Info{Instances: map[*ast.Ident]types.Instance{}}
				if types.CheckExpr(pass.Fset, pass.Pkg, call.Pos(), bare, info) != nil {
					return true
				}
				if got, ok := info.Instances[id]; ok && types.Identical(got.Type, inst.Type) {
					pass.Reportf(call.Fun.Pos(), "unnecessary type arguments")
				}
				return true
			})
		}
		return nil, nil
	},
}
