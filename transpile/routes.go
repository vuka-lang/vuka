package transpile

import (
	"bytes"
	"go/ast"
	"go/types"
	"strconv"
)

// route("users.edit", user.ID) links to a folder route by name, in markup
// and in a .vuka file's Go: it is web.RouteURL(ctx, "users.edit", user.ID),
// routePath web.RoutePath, and query{"page": 2} a web.RouteQuery. The ctx
// is the one in scope, the render's in markup. A package declaring route,
// routePath or query has its own.

// routeBuiltins are the names route lowers, and what they are in web.
var routeBuiltins = map[string]string{"route": "RouteURL", "routePath": "RoutePath", "query": "RouteQuery"}

// mayRoute reports whether src may call route or routePath, or write a
// query literal.
func mayRoute(src []byte) bool {
	return bytes.Contains(src, []byte("route(")) || bytes.Contains(src, []byte("routePath(")) || bytes.Contains(src, []byte("query{"))
}

// lowerRoutes writes f's route, routePath and query builtins as web's. It
// reports whether it changed the source.
func (e *engine) lowerRoutes(f *fileState) bool {
	if e.info == nil || !f.mayRoute {
		return false
	}
	changed := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || f.off(n.Pos()) >= f.body {
			return n == nil
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			id, ok := n.Fun.(*ast.Ident)
			if !ok || (id.Name != "route" && id.Name != "routePath") || !e.builtinRoute(id) {
				return true
			}
			off := f.orig(id.Pos())
			if f.done[off] {
				return true
			}
			f.done[off] = true
			f.add(off, off+len(id.Name), f.webName()+"."+routeBuiltins[id.Name])
			if name := e.ctxAt(n); name != "" {
				text := name
				if len(n.Args) > 0 {
					text += ", "
				}
				f.insert(f.orig(n.Lparen)+1, text, -2)
			} else if f.html == nil || !e.inMarkup(f, n) {
				err := e.errs.add("E0162", f.at(off), "%s needs a context.Context, and none is in scope", id.Name)
				err.help("take a ctx context.Context in the function calling it")
				err.Len = len(id.Name)
			}
			changed = true
		case *ast.CompositeLit:
			id, ok := n.Type.(*ast.Ident)
			if !ok || id.Name != "query" || !e.builtinRoute(id) {
				return true
			}
			off := f.orig(id.Pos())
			if f.done[off] {
				return true
			}
			f.done[off] = true
			f.add(off, off+len(id.Name), f.webName()+".RouteQuery")
			changed = true
		}
		return true
	})
	if changed {
		e.progress = true
	}
	return changed
}

// builtinRoute reports whether id is the builtin: nothing it names is
// declared.
func (e *engine) builtinRoute(id *ast.Ident) bool {
	if e.info.Uses[id] != nil || e.info.Defs[id] != nil {
		return false
	}
	return e.pkg == nil || e.pkg.Scope().Lookup(id.Name) == nil
}

// ctxAt is the name of the context.Context variable ctx in scope at c, ""
// when there is none.
func (e *engine) ctxAt(c *ast.CallExpr) string {
	if e.pkg == nil {
		return ""
	}
	s := e.pkg.Scope().Innermost(c.Pos())
	if s == nil {
		return ""
	}
	_, obj := s.LookupParent("ctx", c.Pos())
	v, ok := obj.(*types.Var)
	if !ok {
		return ""
	}
	if n, ok := types.Unalias(v.Type()).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context" {
		return "ctx"
	}
	return ""
}

// inMarkup reports whether c is a call of f's markup.
func (e *engine) inMarkup(f *fileState, c *ast.CallExpr) bool {
	for _, m := range e.markupCalls(f) {
		if m == c {
			return true
		}
	}
	return false
}

// webName is what f calls package web, importing it on first use.
func (f *fileState) webName() string {
	for _, imp := range f.ast.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == WebPath {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return "web"
		}
	}
	return f.importAs(WebPath, "__web")
}
