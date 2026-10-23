package main

import (
	"go/ast"
	"go/types"
)

// requestReads are what a layout reads the request through: the request
// itself, its URL and its query.
var requestReads = map[[2]string]bool{
	{webPath, "Request"}: true, {webPath, "PageQuery"}: true,
	{"net/http", "Request"}: true, {"net/url", "URL"}: true, {"net/url", "Values"}: true,
}

// pageReads are what a layout reads of the page below it: its title and
// breadcrumbs. Its markup reads them afresh for each page, its Load only
// for the page it loads for.
var pageReads = map[[2]string]bool{
	{UIPath, "PageTitle"}: true, {UIPath, "PageCrumbs"}: true,
}

// readsRequest is what of the request a folder's layout reads — web.Request,
// an *http.Request, a URL or a query, in its methods or the functions and
// methods of its package they call; ui.PageTitle or ui.PageCrumbs in its
// Load or what Load calls — "" when it reads none. Such a layout loads
// again on every navigation (web.ReloadLayout); another persists while the
// pages below it change, its Load seeing only its path parameters.
func (c *routeChecker) readsRequest(obj *types.TypeName, d *routeDir) string {
	syn, ok := c.r.syntax[d.importPath]
	if !ok {
		return ""
	}
	decls := map[types.Object]*ast.FuncDecl{}
	var roots []*ast.FuncDecl
	for _, f := range syn.files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if o := obj.Pkg().Scope().Lookup(fd.Name.Name); o != nil && fd.Recv == nil {
				decls[o] = fd
			}
			if fd.Recv != nil && len(fd.Recv.List) == 1 && recvNamed(fd.Recv.List[0].Type, obj.Name()) {
				roots = append(roots, fd)
			}
		}
	}
	// methods of the package's types, by their objects
	for _, f := range syn.files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Body != nil {
				if o := syn.uses[fd.Name]; o != nil {
					decls[o] = fd
				}
			}
		}
	}
	reads := func(roots []*ast.FuncDecl, set map[[2]string]bool) string {
		seen := map[*ast.FuncDecl]bool{}
		var found string
		var visit func(fd *ast.FuncDecl)
		visit = func(fd *ast.FuncDecl) {
			if seen[fd] || found != "" {
				return
			}
			seen[fd] = true
			ast.Inspect(fd, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || found != "" {
					return found == ""
				}
				o := syn.uses[id]
				if o == nil || o.Pkg() == nil {
					return true
				}
				if set[[2]string{o.Pkg().Path(), o.Name()}] {
					switch o.(type) {
					case *types.TypeName, *types.Func:
						found = o.Pkg().Name() + "." + o.Name()
					}
					return false
				}
				if o.Pkg() == obj.Pkg() {
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
	if found := reads(roots, requestReads); found != "" {
		return found
	}
	var load []*ast.FuncDecl
	for _, fd := range roots {
		if fd.Name.Name == "Load" {
			load = append(load, fd)
		}
	}
	if found := reads(load, pageReads); found != "" {
		return found + " in Load"
	}
	return ""
}

// recvNamed reports whether a receiver's type is name or *name.
func recvNamed(e ast.Expr, name string) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if ix, ok := e.(*ast.IndexExpr); ok {
		e = ix.X
	}
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
