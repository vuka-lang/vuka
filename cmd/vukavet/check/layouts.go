package check

import (
	"go/ast"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/analysis"
)

// requestReads are what a layout reads the request through: the request
// itself, its URL and its query.
var requestReads = map[[2]string]bool{
	{webPath, "Request"}: true, {webPath, "PageQuery"}: true,
	{"net/http", "Request"}: true, {"net/url", "URL"}: true, {"net/url", "Values"}: true,
}

// checkLayouts reports a folder's layout — the struct of a
// _layout.vuka.html — reading the request (W0703): web.Request, an
// *http.Request, a URL or a query, in its methods or the functions and
// methods of the package they call. vuka registers such a layout with
// web.ReloadLayout, and it loads and renders again on every navigation,
// where another persists; a menu marking the current page reads the
// address in the browser instead (ui.Link's Active).
func checkLayouts(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if name := filepath.Base(pass.Fset.File(f.Pos()).Name()); name != "private_layout_html.go" {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				obj, _ := pass.TypesInfo.Defs[ts.Name].(*types.TypeName)
				if obj == nil || !hasChildren(obj) {
					continue
				}
				if what := readsRequest(pass, obj); what != "" {
					report(pass, ts.Name, "W0703", "mark the current link in the browser: <ui.Link href=\"/orders\" active=\"prefix\">; read only the layout's path parameters, its fields",
						"%s.%s, a folder's layout, reads %s: it loads and renders again on every navigation, where a layout reading only its path parameters stays as it is", obj.Pkg().Name(), obj.Name(), what)
				}
			}
		}
	}
}

// hasChildren reports whether obj is a struct with a field Children ui.Node.
func hasChildren(obj *types.TypeName) bool {
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Name() == "Children" && !f.Embedded() && isNode(f.Type()) {
			return true
		}
	}
	return false
}

// isNode reports whether t is ui.Node, an alias or not.
func isNode(t types.Type) bool {
	if a, ok := t.(*types.Alias); ok && a.Obj().Pkg() != nil && a.Obj().Pkg().Path() == uiPath && a.Obj().Name() == "Node" {
		return true
	}
	return named(t, uiPath, "Node")
}

// readsRequest is what of the request obj's methods read, following the
// functions and methods of its package they call; "" when nothing.
func readsRequest(pass *analysis.Pass, obj *types.TypeName) string {
	decls := map[types.Object]*ast.FuncDecl{}
	var roots []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			o := pass.TypesInfo.Defs[fd.Name]
			if o != nil {
				decls[o] = fd
			}
			if fn, ok := o.(*types.Func); ok {
				if recv := fn.Type().(*types.Signature).Recv(); recv != nil && named(recv.Type(), obj.Pkg().Path(), obj.Name()) {
					roots = append(roots, fd)
				}
			}
		}
	}
	seen := map[*ast.FuncDecl]bool{}
	found := ""
	var visit func(fd *ast.FuncDecl)
	visit = func(fd *ast.FuncDecl) {
		if seen[fd] || found != "" {
			return
		}
		seen[fd] = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || found != "" {
				return found == ""
			}
			o := pass.TypesInfo.Uses[id]
			if o == nil || o.Pkg() == nil {
				return true
			}
			if requestReads[[2]string{o.Pkg().Path(), o.Name()}] {
				switch o.(type) {
				case *types.TypeName, *types.Func:
					found = o.Pkg().Name() + "." + o.Name()
				}
				return true
			}
			if o.Pkg() == pass.Pkg {
				if callee, ok := decls[o]; ok {
					visit(callee)
				}
			}
			return true
		})
	}
	for _, fd := range roots {
		visit(fd)
	}
	return found
}
