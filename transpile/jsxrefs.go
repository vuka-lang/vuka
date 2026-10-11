package transpile

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// checkRefs checks a JSX target's refs, when it declares Ref: an element's
// ref="name" names it for code in the same function, which finds it with
// Ref("name") — each literal name of a Ref call (it takes one or more) in a
// function with JSX must name a ref of that function's elements. A function
// with a computed ref={…} isn't checked, nor one without JSX.
//
// When the target declares GetAttr too, a GetAttr("attr", values…) whose
// attribute the function's JSX writes literally on its elements is checked
// the same way: each literal value must be one of those it has. A computed
// value of that attribute turns the check off for it, and values that
// aren't string literals (matchers, variables) aren't checked.
func (e *engine) checkRefs() {
	for _, f := range e.vuka {
		if f.target == nil || f.ast == nil {
			continue
		}
		ref, _ := e.targetObj(f.target, "Ref").(*types.Func)
		getAttr, _ := e.targetObj(f.target, "GetAttr").(*types.Func)
		if ref == nil && getAttr == nil {
			continue
		}
		type fn struct {
			decl     *ast.FuncDecl
			start    int
			end      int
			jsx      bool
			computed bool
			names    []string
			attrs    map[string][]string // literal values of each attribute written
			dynamic  map[string]bool     // attributes some element computes
		}
		var fns []*fn
		for _, d := range f.ast.Decls {
			if d, ok := d.(*ast.FuncDecl); ok && d.Body != nil {
				fns = append(fns, &fn{decl: d, start: f.orig(d.Pos()), end: f.orig(d.End()), attrs: map[string][]string{}, dynamic: map[string]bool{}})
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
				if el.comp != nil {
					return
				}
				for _, a := range el.attrs {
					name := a.name
					if name == "className" {
						name = "class"
					}
					switch {
					case a.event:
					case a.kind == 's' || a.kind == 'b':
						x.attrs[name] = append(x.attrs[name], a.val)
					default:
						x.dynamic[name] = true
					}
					switch {
					case a.name != "ref":
					case a.kind == 's':
						x.names = append(x.names, a.val)
					default:
						x.computed = true
					}
				}
			})
		}
		for _, x := range fns {
			if !x.jsx {
				continue
			}
			ast.Inspect(x.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch obj := e.callee(call.Fun); {
				case obj == nil:
				case ref != nil && obj == types.Object(ref) && !x.computed:
					for _, arg := range call.Args {
						name, ok := stringLit(arg)
						if !ok || slices.Contains(x.names, name) {
							continue
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
						e.errs.add(f.at(f.orig(arg.Pos())), "%s", msg)
					}
				case getAttr != nil && obj == types.Object(getAttr) && len(call.Args) > 1:
					attr, ok := stringLit(call.Args[0])
					have, written := x.attrs[attr]
					if !ok || !written || x.dynamic[attr] || call.Ellipsis.IsValid() {
						return true
					}
					for _, arg := range call.Args[1:] {
						v, ok := stringLit(arg)
						if !ok || slices.Contains(have, v) {
							continue
						}
						msg := fmt.Sprintf("%s(%s, %s): no element in %s has %s=%s", types.ExprString(call.Fun), strconv.Quote(attr), strconv.Quote(v), funcDesc(x.decl), attr, strconv.Quote(v))
						if s := closestName(v, have); s != "" {
							msg += "; did you mean " + strconv.Quote(s) + "?"
						} else {
							msg += "; its values are " + strings.Join(quoteAll(slices.Compact(slices.Sorted(slices.Values(have)))), ", ")
						}
						e.errs.add(f.at(f.orig(arg.Pos())), "%s", msg)
					}
				}
				return true
			})
		}
	}
}

// stringLit is the value of a string literal expression.
func stringLit(x ast.Expr) (string, bool) {
	lit, ok := x.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
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
