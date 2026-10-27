package check

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Parts checks the deferred parts of pages written in Go: a method
// registered with a timing attribute (vuka.RegisterFunc((*Index).Stats,
// ui.Defer(""))) is a page's or component's (E0341), takes a context and
// returns (T, error) (E0347), has one timing attribute (E0345), is a
// stateless page's (E0346), and is named only by its own type's ui.Part
// (E0349) — never called from Go (E0343) — which some ui.Part does
// (E0342). What the transpiler checks of .vuka source, for Go.
var Parts = &analysis.Analyzer{
	Name: "parts",
	Doc:  "check that the methods deferred with ui.Defer are named by their own type's ui.Part only",
	URL:  docsURL,
	Run:  runParts,
}

// deferred is a method registered with a timing attribute.
type deferred struct {
	fn    *types.Func
	recv  *types.TypeName
	reg   *ast.CallExpr // its registration
	named bool          // a ui.Part of its type names it
}

func runParts(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	byFn := map[*types.Func]*deferred{}
	var order []*deferred
	allowed := map[*ast.Ident]bool{} // the method names a registration or a part may hold
	inspect := func(fn func(n ast.Node, stack []ast.Node)) {
		for _, f := range pass.Files {
			var stack []ast.Node
			ast.Inspect(f, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				fn(n, stack)
				stack = append(stack, n)
				return true
			})
		}
	}
	// The registrations.
	inspect(func(n ast.Node, _ []ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || funcOf(info, call, vukaPath) != "RegisterFunc" || len(call.Args) < 2 {
			return
		}
		var defers []ast.Expr
		for _, a := range call.Args[1:] {
			if lit, ok := ast.Unparen(a).(*ast.CompositeLit); ok && call.Ellipsis.IsValid() { // generated: []any{…}...
				for _, el := range lit.Elts {
					if isDefer(info, el) {
						defers = append(defers, el)
					}
				}
				continue
			}
			if isDefer(info, a) {
				defers = append(defers, a)
			}
		}
		if len(defers) == 0 {
			return
		}
		id, fn := methodRef(info, call.Args[0])
		if fn == nil {
			if f, ok := info.Uses[id].(*types.Func); ok && id != nil {
				report(pass, call.Args[0], "E0341", "make it a method of the page whose markup shows it, or drop ui.Defer",
					"ui.Defer goes on a page's or component's method: %s is a function", f.Name())
			}
			return
		}
		allowed[id] = true
		sig := fn.Type().(*types.Signature)
		recv := typeNameOf(sig.Recv().Type())
		name := recv.Name() + "." + fn.Name()
		switch {
		case len(defers) > 1:
			report(pass, defers[1], "E0345", "keep one", "%s has ui.Defer twice: one timing attribute says when it runs", name)
		case !renders(recv.Type()):
			report(pass, call.Args[0], "E0341", "put it on a method of the page whose markup shows it, or drop ui.Defer",
				"ui.Defer goes on a page's or component's method: %s is a method of %s, which renders nothing", fn.Name(), recv.Name())
		case embedsLiveType(recv.Type()):
			report(pass, call.Args[0], "E0346", "drop ui.Defer and load "+fn.Name()+" in the page's mount hook into a field it renders",
				"ui.Defer is for stateless pages; %s is live: a live page loads in its mount hook and pushes with Update", recv.Name())
		case sig.Params().Len() != 1 || !named(sig.Params().At(0).Type(), "context", "Context") || sig.Results().Len() != 2 || !isError(sig.Results().At(1).Type()):
			report(pass, call.Args[0], "E0347", "func (p *"+recv.Name()+") "+fn.Name()+"(ctx context.Context) (T, error)",
				"ui.Defer: %s is %s; a deferred method takes a context.Context and returns (T, error)", name, types.TypeString(sig, types.RelativeTo(pass.Pkg)))
		default:
			if byFn[fn] == nil {
				d := &deferred{fn: fn, recv: recv, reg: call}
				byFn[fn] = d
				order = append(order, d)
			}
		}
	})
	if len(byFn) == 0 {
		return nil, nil
	}
	// The parts naming them.
	inspect(func(n ast.Node, stack []ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || funcOf(info, call, uiPath) != "Part" || len(call.Args) == 0 {
			return
		}
		id, fn := methodRef(info, call.Args[0])
		d := byFn[fn]
		if d == nil {
			return
		}
		allowed[id] = true
		var in *types.TypeName
		for i := len(stack) - 1; i >= 0; i-- {
			if fd, ok := stack[i].(*ast.FuncDecl); ok {
				if fd.Recv != nil {
					in = recvTypeName(info, fd)
				}
				break
			}
		}
		if in != d.recv {
			where := "a function"
			if in != nil {
				where = in.Name() + "'s method"
			}
			report(pass, call.Args[0], "E0349", "render "+d.recv.Name()+"'s part in "+d.recv.Name()+"'s own Render, or give "+orName(in, "this")+" a deferred method of its own",
				"ui.Part names %s.%s in %s: a part is its own page's or component's", d.recv.Name(), d.fn.Name(), where)
			return
		}
		d.named = true
	})
	// Every other use is Go needing the result now.
	for id, o := range info.Uses {
		fn, ok := o.(*types.Func)
		if !ok || byFn[fn] == nil || allowed[id] {
			continue
		}
		d := byFn[fn]
		pos := pass.Fset.Position(id.Pos())
		report(pass, id, "E0343", "drop ui.Defer to call it here, or render what reads it in a ui.Part",
			"ui.Defer: %s is called from Go (%s:%d), where its result is needed now; name it in a ui.Part only", d.fn.Name(), baseName(pos.Filename), pos.Line)
	}
	for _, d := range order {
		if !d.named {
			report(pass, d.reg.Args[0], "E0342", "render it with ui.Part(p."+d.fn.Name()+", fallback, func(v T) ui.Node { … }), or drop ui.Defer",
				"ui.Defer: no ui.Part of %s names %s, so there is no part to defer", d.recv.Name(), d.fn.Name())
		}
	}
	return nil, nil
}

// isDefer reports whether x is a ui.Defer value.
func isDefer(info *types.Info, x ast.Expr) bool {
	tv, ok := info.Types[x]
	return ok && is(tv.Type, uiPath, "Defer")
}

// methodRef is the method a method expression ((*T).M, T.M) or a method
// value (p.M) names, and its name's identifier; for anything else, the
// identifier a plain function's name has, if any.
func methodRef(info *types.Info, x ast.Expr) (*ast.Ident, *types.Func) {
	switch x := ast.Unparen(x).(type) {
	case *ast.SelectorExpr:
		if s := info.Selections[x]; s != nil && (s.Kind() == types.MethodExpr || s.Kind() == types.MethodVal) {
			fn, _ := s.Obj().(*types.Func)
			return x.Sel, fn
		}
		return x.Sel, nil
	case *ast.Ident:
		return x, nil
	}
	return nil, nil
}

func typeNameOf(t types.Type) *types.TypeName {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Obj()
	}
	return nil
}

// renders reports whether t (or *t) is a ui.Node — Render(ctx, w) — or a
// view (View() or Render() returning one).
func renders(t types.Type) bool {
	for _, name := range []string{"Render", "View"} {
		if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, name); o != nil {
			if _, ok := o.(*types.Func); ok {
				return true
			}
		}
	}
	return false
}

// embedsLiveType reports whether t embeds ui.Live.
func embedsLiveType(t types.Type) bool {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range st.NumFields() {
		if f := st.Field(i); f.Embedded() && named(f.Type(), uiPath, "Live") {
			return true
		}
	}
	return false
}

func isError(t types.Type) bool { return types.Identical(t, types.Universe.Lookup("error").Type()) }

func orName(tn *types.TypeName, def string) string {
	if tn != nil {
		return tn.Name()
	}
	return def
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
