package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// A package importing one that doesn't compile sees its types as invalid:
// what it then can't do with them (a field of an embedded type of the
// broken package's it can't find, a GraphQL result it can't describe) is
// the broken package's error, said there. Such an error is a knock-on: it
// is dropped, and the import that brought the invalid type in reported in
// its place, which the build's report counts with the files that fail only
// because of their imports. An error is a knock-on only when the package
// has an import that didn't load and the type at fault is invalid through
// it; any other error stays.

// brokenPaths are the import paths of the package's imports that didn't
// load this round.
func (e *engine) brokenPaths() map[string]bool {
	var out map[string]bool
	for _, te := range e.typeErrs {
		rest, ok := strings.CutPrefix(te.Msg, "could not import ")
		if !ok {
			continue
		}
		path, _, _ := strings.Cut(rest, " ")
		if out == nil {
			out = map[string]bool{}
		}
		out[path] = true
	}
	return out
}

// invalidThrough reports whether t is, or is made of, an invalid type, in
// a package with an import that didn't load (broken): a knock-on of it.
func invalidThrough(t types.Type, broken map[string]bool) bool {
	if len(broken) == 0 || t == nil {
		return false
	}
	seen := map[types.Type]bool{}
	var walk func(t types.Type) bool
	walk = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Basic:
			return t.Kind() == types.Invalid
		case *types.Alias:
			return walk(types.Unalias(t))
		case *types.Named:
			if p := t.Obj().Pkg(); p != nil && broken[p.Path()] {
				return true
			}
			return walk(t.Underlying())
		case *types.Pointer:
			return walk(t.Elem())
		case *types.Slice:
			return walk(t.Elem())
		case *types.Array:
			return walk(t.Elem())
		case *types.Chan:
			return walk(t.Elem())
		case *types.Map:
			return walk(t.Key()) || walk(t.Elem())
		case *types.Struct:
			for i := range t.NumFields() {
				if walk(t.Field(i).Type()) {
					return true
				}
			}
		case *types.Tuple:
			for i := range t.Len() {
				if walk(t.At(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			return walk(t.Params()) || walk(t.Results())
		case *types.Interface:
			for i := range t.NumEmbeddeds() {
				if walk(t.EmbeddedType(i)) {
					return true
				}
			}
			for i := range t.NumExplicitMethods() {
				if walk(t.ExplicitMethod(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
}

// knockOnErr reports whether the type error te is a knock-on of an import
// that didn't load: about an expression whose type is invalid through it.
func (e *engine) knockOnErr(te types.Error, broken map[string]bool) bool {
	if len(broken) == 0 || strings.HasPrefix(te.Msg, "could not import ") || e.info == nil {
		return false
	}
	if strings.Contains(te.Msg, "invalid type") {
		return true
	}
	f := e.fileOf(te.Pos)
	if f == nil || f.ast == nil {
		return false
	}
	var path []ast.Node
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || te.Pos < n.Pos() || te.Pos > n.End() {
			return false
		}
		path = append(path, n)
		return true
	})
	typeOf := func(x ast.Expr) types.Type {
		if tv, ok := e.info.Types[x]; ok {
			return tv.Type
		}
		if id, ok := x.(*ast.Ident); ok {
			if o := e.info.Uses[id]; o != nil {
				return o.Type()
			}
		}
		return nil
	}
	// The expressions at the error, innermost first, up to its statement.
	for i := len(path) - 1; i >= 0; i-- {
		var xs []ast.Expr
		switch n := path[i].(type) {
		case ast.Stmt, ast.Decl, *ast.Field:
			return false
		case *ast.SelectorExpr:
			xs = []ast.Expr{n.X}
		case *ast.CallExpr:
			xs = append([]ast.Expr{n.Fun}, n.Args...)
		case *ast.CompositeLit:
			xs = []ast.Expr{n.Type}
		case *ast.IndexExpr:
			xs = []ast.Expr{n.X}
		case *ast.StarExpr:
			xs = []ast.Expr{n.X}
		case *ast.Ident:
			xs = []ast.Expr{n}
		}
		for _, x := range xs {
			if x != nil && invalidThrough(typeOf(x), broken) {
				return true
			}
		}
	}
	return false
}

// foldKnockOns drops the knock-on errors, reporting for each import that
// didn't load, in their place, the import.
func (e *engine) foldKnockOns() {
	kept := (*e.errs)[:0]
	dropped := false
	for _, err := range *e.errs {
		if err.knockOn {
			dropped = true
			continue
		}
		kept = append(kept, err)
	}
	*e.errs = kept
	if !dropped {
		return
	}
	have := map[token.Position]bool{}
	for _, err := range *e.errs {
		if err.Code == "E0005" {
			have[err.Pos] = true
		}
	}
	for _, te := range e.typeErrs {
		if !strings.HasPrefix(te.Msg, "could not import ") {
			continue
		}
		if pos := e.typeErrPos(te); !have[pos] {
			have[pos] = true
			e.errs.add("E0005", pos, "%s", importFailure(te.Msg))
		}
	}
}
