package transpile

import (
	"go/ast"
	"go/types"
)

// Markup coerces the values it takes — a hole, an attribute, a prop, a
// for's range, a condition — from the fallible and optional types to the
// value they hold, by this table:
//
//	(T, error) from a call    ui.Must(call)          an error fails the render (unwrap.go)
//	vuka.Result[T]            ui.Must(r.Get())       Err fails the render
//
// A value keeps its type where it is used as it is: the receiver of a
// method or field (r.IsErr(), r.UnwrapOr(x)), an argument whose parameter
// takes it (fmt.Sprint(r)), a prop whose field does (a Result, any, a type
// parameter), and a name a template's statement binds to it (r := Find(),
// then if r.IsErr()) — its reads coerce.

// coercion is a row of the table: the runtime's generic type it takes, and
// the text it wraps a value of it in, given the target's qualifier.
type coercion struct {
	kind, name     string
	prefix, suffix func(q string) string
}

var coercions = []coercion{
	{kind: "result", name: "Result",
		prefix: func(q string) string { return q + "Must(" },
		suffix: func(string) string { return ".Get())" }},
}

// coercionOf is the row taking t, with its value type.
func coercionOf(t types.Type) (*coercion, types.Type) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != RuntimePath || n.TypeArgs().Len() != 1 {
		return nil, nil
	}
	for i := range coercions {
		if coercions[i].name == n.Obj().Name() {
			return &coercions[i], n.TypeArgs().At(0)
		}
	}
	return nil, nil
}

// coerceIn coerces the values in x the table takes: x itself unless the
// prop it sets takes it as it is, and those inside it that aren't used as
// they are.
func (e *engine) coerceIn(f *fileState, x markupValue, changed *bool) {
	if f.target == nil || !f.target.has("Must") || x.bind {
		return
	}
	var visit func(n ast.Expr, keep bool)
	visit = func(n ast.Expr, keep bool) {
		if n == nil {
			return
		}
		if tv, ok := e.info.Types[n]; ok && !tv.IsType() && !keep {
			if c, _ := coercionOf(tv.Type); c != nil {
				if e.coerce(f, n, c, tv.Type) {
					*changed = true
				}
				return
			}
		}
		switch n := n.(type) {
		case *ast.ParenExpr:
			visit(n.X, keep)
		case *ast.SelectorExpr:
			visit(n.X, true)
		case *ast.CallExpr:
			visit(n.Fun, true)
			var sig *types.Signature
			if tv, ok := e.info.Types[n.Fun]; ok && !tv.IsType() && !tv.IsBuiltin() {
				sig, _ = types.Unalias(tv.Type).Underlying().(*types.Signature)
			}
			for i, a := range n.Args {
				visit(a, sig != nil && e.paramTakes(sig, i, a))
			}
		case *ast.IndexExpr:
			visit(n.X, false)
			visit(n.Index, false)
		case *ast.StarExpr:
			visit(n.X, false)
		case *ast.UnaryExpr:
			visit(n.X, false)
		case *ast.BinaryExpr:
			visit(n.X, false)
			visit(n.Y, false)
		case *ast.CompositeLit:
			for _, el := range n.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					el = kv.Value
				}
				visit(el, true)
			}
		}
	}
	visit(x.expr, x.comp != nil && e.propTakes(f, x))
}

// paramTakes reports whether the i'th argument a of a call to sig is taken
// as it is: its parameter's type is one it is assignable to.
func (e *engine) paramTakes(sig *types.Signature, i int, a ast.Expr) bool {
	p := sig.Params()
	if p.Len() == 0 {
		return false
	}
	var t types.Type
	switch {
	case sig.Variadic() && i >= p.Len()-1:
		t = p.At(p.Len() - 1).Type().(*types.Slice).Elem()
	case i < p.Len():
		t = p.At(i).Type()
	default:
		return false
	}
	tv, ok := e.info.Types[a]
	return ok && takesAsIs(t, tv.Type)
}

// takesAsIs reports whether a declared type t takes a value of v as it is:
// a type parameter, the same generic type, or one v is assignable to.
func takesAsIs(t, v types.Type) bool {
	switch u := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		if c, _ := coercionOf(v); c != nil && u.Obj().Pkg() != nil && u.Obj().Pkg().Path() == RuntimePath && u.Obj().Name() == c.name {
			return true
		}
	}
	return types.AssignableTo(v, t)
}

// propTakes reports whether the prop x sets takes its value as it is.
func (e *engine) propTakes(f *fileState, x markupValue) bool {
	tv, ok := e.info.Types[x.expr]
	if !ok {
		return false
	}
	if c, _ := coercionOf(tv.Type); c == nil {
		return false
	}
	expr := e.compExpr(f, x.comp)
	if expr == nil {
		return false
	}
	var names []string
	var typs []types.Type
	switch obj := e.info.Uses[compIdent(expr)].(type) {
	case *types.TypeName:
		st, ok := obj.Type().Underlying().(*types.Struct)
		if !ok {
			return false
		}
		for i := 0; i < st.NumFields(); i++ {
			names, typs = append(names, st.Field(i).Name()), append(typs, st.Field(i).Type())
		}
	case *types.Func:
		sig := obj.Signature()
		if st, _ := propsStruct(sig, nil); st != nil {
			for i := 0; i < st.NumFields(); i++ {
				names, typs = append(names, st.Field(i).Name()), append(typs, st.Field(i).Type())
			}
		} else {
			for i := 0; i < sig.Params().Len(); i++ {
				names, typs = append(names, sig.Params().At(i).Name()), append(typs, sig.Params().At(i).Type())
			}
		}
	default:
		return false
	}
	i := bindName(names, x.attr)
	return i >= 0 && takesAsIs(typs[i], tv.Type)
}

// coerce wraps x, of type t, as c says, once.
func (e *engine) coerce(f *fileState, x ast.Expr, c *coercion, t types.Type) bool {
	start, end := f.orig(x.Pos()), f.orig(x.End())
	if f.unwrapped[span{start, end}] || end <= start || end > len(f.src) {
		return false
	}
	f.unwrapped[span{start, end}] = true
	_, val := coercionOf(t)
	lparen := end
	if call, ok := x.(*ast.CallExpr); ok {
		lparen = f.orig(call.Lparen)
	}
	f.unwraps = append(f.unwraps, Unwrap{Off: start, Lparen: lparen, End: end, Type: e.display(val), Kind: c.kind})
	pre, suf := c.prefix(f.target.q), c.suffix(f.target.q)
	switch x.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.ParenExpr, *ast.CompositeLit:
	default:
		pre, suf = pre+"(", ")"+suf
	}
	f.insert(start, pre, -1)
	if last := f.src[end-1]; last == suf[len(suf)-1] {
		// Inside the span, so a copy of it has it: the call's own ) is the
		// suffix's last byte.
		f.insert(end-1, string(last)+suf[:len(suf)-1], -1)
	} else {
		f.fixed = append(f.fixed, edit{start: end, end: end, text: suf, prio: -2, tail: true})
	}
	return true
}
