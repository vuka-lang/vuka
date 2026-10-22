package check

import (
	"cmp"
	"go/ast"
	"go/types"
	"net/http"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// registration is a call registering with web: web.Routes.Get(…),
// app.Live(…), app.Service(…).
type registration struct {
	at     span   // from the method's name: a chain's call is on its own line
	verb   string // Handle, Get, Post, Put, Patch, Delete, Live, Controller, Service
	method string // the HTTP method of a route
	path   string // its path, a controller's prefix; "" when not a constant
	fn     ast.Expr
	opts   []ast.Expr
	global bool         // on web.Routes: every app's
	app    types.Object // the App it is on, when the call names one
}

var verbs = map[string]string{"Get": http.MethodGet, "Post": http.MethodPost, "Put": http.MethodPut,
	"Patch": http.MethodPatch, "Delete": http.MethodDelete, "Handle": "", "Live": http.MethodGet, "Controller": "", "Service": ""}

// registrations are the package's calls registering with web, in source
// order.
func registrations(pass *analysis.Pass) []registration {
	var out []registration
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			r, ok := registrationOf(pass.TypesInfo, call)
			if ok {
				out = append(out, r)
			}
			return true
		})
	}
	slices.SortFunc(out, func(a, b registration) int { return cmp.Compare(a.at.pos, b.at.pos) })
	return out
}

func registrationOf(info *types.Info, call *ast.CallExpr) (registration, bool) {
	var r registration
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		r.at = span{sel.Sel.Pos(), call.End()}
	}
	var recv ast.Expr
	if r.verb, recv = method(info, call, webPath, "Registry"); r.verb != "" {
		r.global = true
	} else if r.verb, recv = method(info, call, webPath, "App"); r.verb == "" {
		return r, false
	}
	m, ok := verbs[r.verb]
	if !ok || call.Ellipsis.IsValid() {
		return r, false
	}
	r.method = m
	args := call.Args
	if r.verb == "Handle" {
		if len(args) < 3 {
			return r, false
		}
		r.method, _ = str(info, args[0])
		args = args[1:]
	}
	if r.verb == "Service" {
		if len(args) != 1 {
			return r, false
		}
		r.fn = args[0]
		return r, true
	}
	if len(args) < 2 {
		return r, false
	}
	r.path, _ = str(info, args[0])
	r.fn, r.opts = args[1], args[2:]
	if !r.global {
		r.app = rootVar(info, recv)
	}
	return r, true
}

// rootVar is the variable a chain of App calls starts from: app in
// app.Get(…).Get(…).
func rootVar(info *types.Info, e ast.Expr) types.Object {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			if v, ok := info.Uses[x].(*types.Var); ok {
				return v
			}
			return nil
		case *ast.CallExpr:
			sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr)
			if !ok {
				return nil
			}
			e = sel.X
		default:
			return nil
		}
	}
}

// island reports whether the options have ui.Island(…): the route serves
// the struct its function builds, backed by its island.
func (r registration) island(info *types.Info) bool {
	for _, o := range r.opts {
		if c, ok := ast.Unparen(o).(*ast.CallExpr); ok && funcOf(info, c, uiPath) == "Island" {
			return true
		}
	}
	return false
}

// ctor is the struct a constructor's signature builds — func(…) *T or
// func(…) (*T, error) — and why not when it doesn't.
func ctor(sig *types.Signature, q types.Qualifier) (*types.Named, string) {
	res := sig.Results()
	if n := res.Len(); n == 0 || n > 2 || n == 2 && !types.Identical(res.At(1).Type(), errorType) {
		return nil, "a constructor returns *T or (*T, error), not " + types.TypeString(sig, q)
	}
	if p, ok := res.At(0).Type().(*types.Pointer); ok {
		if n, ok := types.Unalias(p.Elem()).(*types.Named); ok {
			if _, ok := n.Underlying().(*types.Struct); ok {
				return n, ""
			}
		}
	}
	return nil, "a constructor returns a pointer to a struct, not " + types.TypeString(res.At(0).Type(), q)
}

// pageType is the struct a route serves as a page: the one its function
// builds, with ui.Island among its options, or a live page's.
func (r registration) pageType(info *types.Info) *types.Named {
	t := info.TypeOf(r.fn)
	if t == nil {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types.Signature:
		if r.verb != "Live" && !r.island(info) {
			return nil
		}
		n, _ := ctor(u, nil)
		return n
	case *types.Pointer:
		if r.verb == "Live" {
			if n, ok := types.Unalias(u.Elem()).(*types.Named); ok {
				if _, ok := n.Underlying().(*types.Struct); ok {
					return n
				}
			}
		}
	}
	return nil
}

func joinPath(prefix, path string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	switch {
	case prefix == "":
		return path
	case path == "/" || path == "":
		return prefix
	case !strings.HasPrefix(path, "/"):
		return prefix + "/" + path
	}
	return prefix + path
}
