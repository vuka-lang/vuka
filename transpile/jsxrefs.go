package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// checkRefs checks a JSX target's refs, when it declares Ref: an element's
// ref="name" names it for code in the same function, which finds it with
// Ref("name") — each Ref with a literal name in a function with JSX must
// name a ref of that function's elements. A function with a computed
// ref={…} isn't checked, nor one without JSX.
func (e *engine) checkRefs() {
	for _, f := range e.vuka {
		if f.target == nil || f.ast == nil {
			continue
		}
		ref, _ := e.targetObj(f.target, "Ref").(*types.Func)
		if ref == nil {
			continue
		}
		type fn struct {
			decl     *ast.FuncDecl
			start    int
			end      int
			jsx      bool
			computed bool
			names    []string
		}
		var fns []*fn
		for _, d := range f.ast.Decls {
			if d, ok := d.(*ast.FuncDecl); ok && d.Body != nil {
				fns = append(fns, &fn{decl: d, start: f.orig(d.Pos()), end: f.orig(d.End())})
			}
		}
		for _, t := range f.jsx {
			i := slices.IndexFunc(fns, func(x *fn) bool { return x.start <= t.start && t.start < x.end })
			if i < 0 {
				continue
			}
			x := fns[i]
			x.jsx = true
			walkJSX(t.root, func(el *jsxElem) {
				for _, a := range el.attrs {
					switch {
					case a.name != "ref" || el.comp != nil:
					case a.kind == 's':
						x.names = append(x.names, a.val)
					default:
						x.computed = true
					}
				}
			})
		}
		for _, x := range fns {
			if !x.jsx || x.computed {
				continue
			}
			ast.Inspect(x.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 || e.callee(call.Fun) != types.Object(ref) {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil || slices.Contains(x.names, name) {
					return true
				}
				msg := "no ref=" + strconv.Quote(name) + " in " + funcDesc(x.decl)
				switch s := closestName(name, x.names); {
				case s != "":
					msg += "; did you mean " + strconv.Quote(s) + "?"
				case len(x.names) == 0:
					msg += `: give an element ref="` + name + `"`
				default:
					msg += "; its refs are " + strings.Join(quoteAll(x.names), ", ")
				}
				e.errs.add(f.at(f.orig(lit.Pos())), "%s", msg)
				return true
			})
		}
	}
}

// callee is the object a call's function names: an identifier or a
// qualified one.
func (e *engine) callee(fun ast.Expr) types.Object {
	switch x := fun.(type) {
	case *ast.Ident:
		return e.info.Uses[x]
	case *ast.SelectorExpr:
		return e.info.Uses[x.Sel]
	}
	return nil
}

// walkJSX calls f for each element of a JSX tree, components' tags included.
func walkJSX(n jsxNode, f func(*jsxElem)) {
	switch n := n.(type) {
	case *jsxElem:
		if n == nil {
			return
		}
		f(n)
		for _, k := range n.kids {
			walkJSX(k, f)
		}
	case *jsxBlock:
		for _, body := range append(slices.Clip(n.bodies), n.cases...) {
			for _, k := range body {
				walkJSX(k, f)
			}
		}
	}
}

// funcDesc names a function for a message: Counter's render for a Render
// method, Counter.Close for another method, Page for a function.
func funcDesc(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	recv := types.ExprString(t)
	if d.Name.Name == "Render" {
		return recv + "'s render"
	}
	return recv + "." + d.Name.Name
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strconv.Quote(s)
	}
	return out
}
