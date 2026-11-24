package main

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
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

// funcDecls are the package's functions and methods with bodies, by their
// objects.
func (s *factsSource) funcDecls() map[types.Object]*ast.FuncDecl {
	scope := s.ty.Pkg.Scope()
	decls := map[types.Object]*ast.FuncDecl{}
	for _, f := range s.ty.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fd.Recv == nil {
				if o := scope.Lookup(fd.Name.Name); o != nil {
					decls[o] = fd
				}
			} else if o := s.ty.Uses[fd.Name]; o != nil {
				decls[o] = fd
			}
		}
	}
	return decls
}

// methodsOf are the methods of the package's type name, with bodies.
func (s *factsSource) methodsOf(name string) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, f := range s.ty.Files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil && fd.Recv != nil && len(fd.Recv.List) == 1 && recvNamed(fd.Recv.List[0].Type, name) {
				out = append(out, fd)
			}
		}
	}
	return out
}

// reads is what of set the functions roots read, in them or the functions
// and methods of the package they call, "" when none; via is the exported
// functions of other page packages they call when they read none.
func (s *factsSource) reads(decls map[types.Object]*ast.FuncDecl, roots []*ast.FuncDecl, set map[[2]string]bool) (found string, via []string) {
	seen := map[*ast.FuncDecl]bool{}
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
			o := s.ty.Uses[id]
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
			if o.Pkg() == s.ty.Pkg {
				if callee, ok := decls[o]; ok {
					visit(callee)
				}
			} else if fn, ok := o.(*types.Func); ok && fn.Exported() && fn.Type().(*types.Signature).Recv() == nil && s.t.inPages(o.Pkg().Path()) {
				via = append(via, o.Pkg().Path()+"."+o.Name())
			}
			return true
		})
	}
	for _, fd := range roots {
		visit(fd)
	}
	if found != "" {
		return found, nil
	}
	slices.Sort(via)
	return "", slices.Compact(via)
}

// readsRequest is what of the request a folder's layout, roots its
// methods, reads — web.Request, an *http.Request, a URL or a query, in its
// methods or the functions and methods of its package they call; ui.PageTitle
// or ui.PageCrumbs in its Load or what Load calls — "" when it reads none,
// and the other page packages' functions it calls, which may. Such a layout
// loads again on every navigation (web.ReloadLayout); another persists
// while the pages below it change, its Load seeing only its path parameters.
func (s *factsSource) readsRequest(roots []*ast.FuncDecl, withLoad bool) readFact {
	decls := s.funcDecls()
	found, via := s.reads(decls, roots, requestReads)
	if found != "" || !withLoad {
		return readFact{Reads: found, Via: via}
	}
	var load []*ast.FuncDecl
	for _, fd := range roots {
		if fd.Name.Name == "Load" {
			load = append(load, fd)
		}
	}
	if f, _ := s.reads(decls, load, pageReads); f != "" {
		return readFact{Reads: f + " in Load"}
	}
	return readFact{Via: via}
}

// readers are the package's exported functions as they read the request.
func (s *factsSource) readers() map[string]readFact {
	decls := s.funcDecls()
	out := map[string]readFact{}
	for obj, fd := range decls {
		if fd.Recv != nil || !obj.Exported() {
			continue
		}
		if r := s.readsRequestFunc(decls, fd); r.Reads != "" || len(r.Via) > 0 {
			out[obj.Name()] = r
		}
	}
	return out
}

func (s *factsSource) readsRequestFunc(decls map[types.Object]*ast.FuncDecl, fd *ast.FuncDecl) readFact {
	found, via := s.reads(decls, []*ast.FuncDecl{fd}, requestReads)
	return readFact{Reads: found, Via: via}
}

// inPages reports whether path is a package of the tree's pages.
func (t *routeTree) inPages(path string) bool {
	return path == t.pagesPath || strings.HasPrefix(path, t.pagesPath+"/")
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
