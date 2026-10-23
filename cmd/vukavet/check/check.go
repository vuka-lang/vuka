// Package check is vukavet's analyzers: the checks vuka vet and the
// libraries' boot make of a vuka app, run on its Go source — routes and
// handlers, live pages, ui.Template holes and orm Schema methods. Each
// diagnostic carries the code the same mistake has at boot or in vuka vet
// (docs/errors), as its Category, its URL and the start of its message.
package check

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzers are vukavet's checks.
var Analyzers = []*analysis.Analyzer{Routes, Live, Templates, Schema, Context, URLState}

const (
	vukaPath  = "github.com/vuka-lang/vuka/v2"
	webPath   = "github.com/vuka-lang/web/v2"
	uiPath    = "github.com/vuka-lang/ui/v2"
	ormPath   = "github.com/vuka-lang/orm/v2"
	templPath = "github.com/a-h/templ"
	docsURL   = "https://vuka-lang.github.io/errors/"
)

// report is a diagnostic with a code at node, its help on a line of its own.
func report(pass *analysis.Pass, at analysis.Range, code, help, format string, args ...any) {
	msg := code + ": " + fmt.Sprintf(format, args...)
	if help != "" {
		msg += "\n\thelp: " + help
	}
	pass.Report(analysis.Diagnostic{Pos: at.Pos(), End: at.End(), Category: code, URL: docsURL + code, Message: msg})
}

// span is a range for a position with no node.
type span struct{ pos, end token.Pos }

func (s span) Pos() token.Pos { return s.pos }
func (s span) End() token.Pos { return s.end }

// at is obj's place when it is declared in the package analysed, else
// fallback: a diagnostic stays in the package's files.
func at(pass *analysis.Pass, obj types.Object, fallback analysis.Range) analysis.Range {
	if obj != nil && obj.Pkg() == pass.Pkg && obj.Pos().IsValid() {
		return span{obj.Pos(), obj.Pos() + token.Pos(len(obj.Name()))}
	}
	return fallback
}

// named is t, through one pointer, when it is the type pkg.name.
func named(t types.Type, pkg, name string) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

// is reports whether t is exactly pkg.name, no pointer.
func is(t types.Type, pkg, name string) bool {
	_, ptr := t.(*types.Pointer)
	return !ptr && named(t, pkg, name)
}

// method is the method a call calls when it is a method of pkg's type
// (or a pointer to it) named recv: its name, and its receiver expression.
func method(info *types.Info, call *ast.CallExpr, pkg, recv string) (string, ast.Expr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	s, ok := info.Selections[sel]
	if !ok || s.Kind() != types.MethodVal || !named(s.Recv(), pkg, recv) {
		return "", nil
	}
	return sel.Sel.Name, sel.X
}

// funcOf is the package-level function pkg.name a call calls, "" when it
// calls another.
func funcOf(info *types.Info, call *ast.CallExpr, pkg string) string {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.IndexExpr:
		return funcOf(info, &ast.CallExpr{Fun: f.X}, pkg)
	default:
		return ""
	}
	fn, ok := info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkg || fn.Type().(*types.Signature).Recv() != nil {
		return ""
	}
	return fn.Name()
}

// str is e's value when it is a constant string.
func str(info *types.Info, e ast.Expr) (string, bool) {
	if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), true
	}
	return "", false
}

// lookup is the package path's object name among pkg's imports, direct or
// not.
func lookup(pkg *types.Package, path, name string) types.Object {
	seen := map[*types.Package]bool{}
	var find func(*types.Package) types.Object
	find = func(p *types.Package) types.Object {
		if seen[p] {
			return nil
		}
		seen[p] = true
		if p.Path() == path {
			return p.Scope().Lookup(name)
		}
		for _, q := range p.Imports() {
			if o := find(q); o != nil {
				return o
			}
		}
		return nil
	}
	return find(pkg)
}

// iface is the interface pkg.name, nil when the package isn't imported.
func iface(pass *analysis.Pass, path, name string) *types.Interface {
	if o := lookup(pass.Pkg, path, name); o != nil {
		if i, ok := o.Type().Underlying().(*types.Interface); ok {
			return i
		}
	}
	return nil
}

func implements(t types.Type, i *types.Interface) bool {
	return i != nil && types.Implements(t, i)
}

var (
	errorType  = types.Universe.Lookup("error").Type()
	errorIface = errorType.Underlying().(*types.Interface)
)

func isContext(t types.Type) bool { return is(t, "context", "Context") }

// lowerFirst is a Go name as the browser's live object names it.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// typeString is t as the package analysed writes it: other packages by
// their names.
func typeString(pass *analysis.Pass, t types.Type) string {
	return types.TypeString(t, qualifier(pass))
}

func qualifier(pass *analysis.Pass) types.Qualifier {
	return func(p *types.Package) string {
		if p == pass.Pkg {
			return ""
		}
		return p.Name()
	}
}
