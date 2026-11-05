package load

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSessionKeepUnnamed: with KeepUnnamed, main isn't transpiled again
// when lib gains an exported function or basic constant no word of main
// makes, and is when lib's change can reach it: a function it calls, a name
// one of its words ends, a type, a name the transpiler looks for. Each run
// matches Transpile from scratch, so a kept output is never stale.
func TestSessionKeepUnnamed(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := sessionModule(t)
	s, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	s.KeepUnnamed = true
	first, _ := sessionRun(t, s, root)
	lib := filepath.Join(root, "lib", "lib.vuka")
	for _, c := range []struct {
		name, old, new string
		transpiled     int
	}{
		{"a new exported function", "func helper()", "func Extra(n int) string { return \"\" }\n\nfunc helper()", 1},
		{"its signature changed", "func Extra(n int) string", "func Extra(n, m int) string", 1},
		{"a new exported constant", "func helper()", "const Limit = 3\n\nfunc helper()", 1},
		{"its value changed", "Limit = 3", "Limit = 4", 1},
		{"a function main calls", "func Count() (int, error) { return dep.N(), nil }", "func Count() Result[int] { return Ok(dep.N()) }", 2},
		{"a function a word of main ends", "func helper()", "func FmtRun() {}\n\nfunc helper()", 2},
		{"a function named as the transpiler looks for", "func helper()", "func Part() {}\n\nfunc helper()", 2},
		{"a new type", "func helper()", "type Shape struct{ N int }\n\nfunc helper()", 2},
		{"a typed constant", "func helper()", "const Small Shape2 = 1\n\ntype Shape2 int\n\nfunc helper()", 2},
		{"a new variable", "func helper()", "var Global = 1\n\nfunc helper()", 2},
	} {
		edit(t, lib, c.old, c.new)
		main, _ := sessionRun(t, s, root)
		if s.ran.fellBack || s.ran.transpiled != c.transpiled {
			t.Errorf("%s: %+v, want %d transpiled", c.name, s.ran, c.transpiled)
		}
		if c.transpiled == 1 && main == "" {
			t.Errorf("%s: no main", c.name)
		}
	}
	if first == "" {
		t.Fatal("no main")
	}

	// Without it (vuka build), lib's API is main's: any exported change
	// transpiles main again.
	s.KeepUnnamed = false
	sessionRun(t, s, root)
	edit(t, lib, "func helper()", "func Extra2() {}\n\nfunc helper()")
	if sessionRun(t, s, root); s.ran.transpiled != 2 {
		t.Errorf("without KeepUnnamed, a new exported function: %+v", s.ran)
	}
}

func TestNamedBy(t *testing.T) {
	words := map[string]bool{"user": true, "find": true, "do": true, "count": true, "id": true, "values": true}
	for name, want := range map[string]bool{
		"Find":         true,  // the word
		"UserFind":     true,  // Type.Find, a static
		"BaseDo":       true,  // Post.Do, Post embedding Base
		"NewUser":      true,  // User.New
		"User_find":    true,  // an old static
		"ByID":         true,  // ends with a word at its edge
		"Recount":      false, // count, but not at an edge
		"Users":        false,
		"Finder":       false,
		"Helper":       false,
		"BenchUnused1": false,
		"x_1":          false,
	} {
		if got := namedBy(words, name); got != want {
			t.Errorf("namedBy(%s) = %v, want %v", name, got, want)
		}
	}
}

// TestProbedNames lists every scope lookup in the transpiler. A function or
// basic constant of an imported package that the transpiler can find by a
// name the source doesn't write or end must be probed (session.go), or
// KeepUnnamed would keep a package whose output it changes. A new lookup
// fails this test until its name is reviewed: add it to probed when it is
// such a name, then to the list here.
func TestProbedNames(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../../transpile", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		for name, f := range p.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Lookup" || strings.HasPrefix(types.ExprString(sel.X), "types.Universe") {
					return true
				}
				arg := call.Args[0]
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if !probed(strings.Trim(lit.Value, `"`)) {
						t.Errorf("%s: Lookup(%s) isn't probed", filepath.Base(name), lit.Value)
					}
					return true
				}
				if b, ok := arg.(*ast.BinaryExpr); ok {
					if lit, ok := b.X.(*ast.BasicLit); ok && !slices.Contains(probedPrefixes, strings.Trim(lit.Value, `"`)) {
						t.Errorf("%s: Lookup(%s) isn't probed", filepath.Base(name), types.ExprString(arg))
					}
					if lit, ok := b.Y.(*ast.BasicLit); ok && !slices.Contains(probedSuffixes, strings.Trim(lit.Value, `"`)) {
						t.Errorf("%s: Lookup(%s) isn't probed", filepath.Base(name), types.ExprString(arg))
					}
				}
				got = append(got, filepath.Base(name)+" "+types.ExprString(arg))
				return true
			})
		}
	}
	slices.Sort(got)
	got = slices.Compact(got)
	if want := strings.Split(strings.TrimSpace(reviewedLookups), "\n"); !slices.Equal(got, want) {
		var b strings.Builder
		for _, g := range got {
			b.WriteString(g + "\n")
		}
		t.Errorf("the transpiler's scope lookups changed; review the new ones (see the comment), then make reviewedLookups:\n%s", b.String())
	}
}

// reviewedLookups are the transpiler's scope lookups TestProbedNames has
// seen reviewed: by a name the source writes or ends (an identifier, a
// selector, staticGoName's), one asserted to a kind KeepUnnamed never keeps
// (a type, a variable), a local scope's, or a probed one.
const reviewedLookups = `
bindings.go f.html.recvType
bindings.go id.Name
contextvars.go p.Func
contextvet.go f.html.recvType
decorate.go "Register" + fn.Name()
decorate.go d.name
decorate.go name
decorate.go s.(*ast.TypeSpec).Name.Name
decorate.go sel
decorate.go strings.TrimPrefix(d.rtype, "*")
decorate.go x.Sel.Name
engine.go model
enum.go name + "Values"
enum.go parseName(name)
enum.go s
fields.go kind
fields.go n.Obj().Name() + "Columns"
fields.go n.Obj().Name() + suffix
graphql.go name
graphql.go op.name
graphqlloader.go l.fetch
htmlfile.go h.recvType
jsxcomp.go id.Name
jsxcomp.go n
match.go id.Name
match.go x.Sel.Name
parts.go f.html.recvType
parts.go h.recvType
parts.go sel
push.go name
schema.go n
schema.go sel
schema.go sel.Sel.Name
slots.go h.recvType
static.go member
static.go oldStaticGoName(tn.Name(), member)
static.go p.typ
static.go staticGoName(s.typeName, s.name)
static.go staticGoName(tn.Name(), member)
static.go x.Sel.Name
streams.go f.html.shape.Type
target.go name
targets.go a.Name
targets.go sel
tidy.go n
tracked.go f.html.shape.Type
tracked.go g.html.shape.Type
tryexpr.go n
`
