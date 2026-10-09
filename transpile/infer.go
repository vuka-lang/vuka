package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
)

// infer gives Err, None, Ok and Some the type argument Go can't infer, taken
// from where the value goes: `return Err(e)` in a function returning
// Result[User] becomes `return vuka.Err[User](e)`, and `var o Option[int] =
// None` becomes `vuka.None[int]()`.
func (e *engine) infer(f *fileState) {
	delete(e.pending, f)
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !e.isRuntimeFunc(sel) {
			return true
		}
		if ix, ok := f.parents[sel].(*ast.IndexExpr); ok && ix.X == sel {
			return true // instantiated by hand
		}
		off := f.orig(sel.Sel.End())
		if f.done[off] {
			return true
		}
		name := sel.Sel.Name
		var expr ast.Expr = sel
		call, isCall := f.parents[sel].(*ast.CallExpr)
		if isCall && call.Fun == sel {
			expr = call
		} else if name != "None" {
			return true
		}
		want := e.context(f, expr)
		kind, arg := runtimeType(want)
		ctor := map[string]string{"Ok": "Result", "Err": "Result", "Some": "Option", "None": "Option"}[name]
		if kind != ctor {
			if tv := e.info.Types[expr]; name == "Err" || name == "None" || isInvalid(tv.Type) {
				if e.pending[f] == nil {
					e.pending[f] = map[int]string{}
				}
				e.pending[f][f.orig(sel.Pos())] = name
			}
			return true
		}
		if tv := e.info.Types[expr]; !isInvalid(tv.Type) && types.Identical(tv.Type, want) {
			return true
		}
		s, msg := e.typeText(f, arg)
		if msg != "" {
			e.errs.add(f.nodePos(sel.Pos()), "%s", msg)
			return true
		}
		text := "[" + s + "]"
		if !isCall || call.Fun != sel {
			text += "()"
		}
		f.done[off] = true
		delete(e.pending[f], f.orig(sel.Pos()))
		f.insert(off, text, 1)
		e.progress = true
		return true
	})
}

func (e *engine) isRuntimeFunc(sel *ast.SelectorExpr) bool {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := e.info.Uses[x].(*types.PkgName)
	if !ok || pn.Imported().Path() != RuntimePath {
		return false
	}
	switch sel.Sel.Name {
	case "Ok", "Err", "Some", "None":
		return true
	}
	return false
}

// context is the type a value written at n is expected to have, or nil.
func (e *engine) context(f *fileState, n ast.Expr) types.Type {
	for {
		p, ok := f.parents[n].(*ast.ParenExpr)
		if !ok {
			break
		}
		n = p
	}
	typeOf := func(x ast.Expr) types.Type {
		if tv, ok := e.info.Types[x]; ok && !isInvalid(tv.Type) {
			return tv.Type
		}
		return nil
	}
	switch p := f.parents[n].(type) {
	case *ast.ReturnStmt:
		sig, _ := e.enclosingFunc(f, p)
		if sig == nil || sig.Results().Len() != len(p.Results) {
			return nil
		}
		for i, r := range p.Results {
			if r == n {
				return sig.Results().At(i).Type()
			}
		}
	case *ast.AssignStmt:
		if p.Tok != token.ASSIGN || len(p.Lhs) != len(p.Rhs) {
			return nil
		}
		for i, r := range p.Rhs {
			if r == n {
				return typeOf(p.Lhs[i])
			}
		}
	case *ast.ValueSpec:
		if p.Type != nil {
			return typeOf(p.Type)
		}
	case *ast.CallExpr:
		sig, ok := typeOf(p.Fun).(*types.Signature)
		if !ok {
			return nil
		}
		for i, a := range p.Args {
			if a != n {
				continue
			}
			params := sig.Params()
			switch {
			case sig.Variadic() && i >= params.Len()-1 && !p.Ellipsis.IsValid():
				return params.At(params.Len() - 1).Type().(*types.Slice).Elem()
			case i < params.Len():
				return params.At(i).Type()
			}
		}
	case *ast.KeyValueExpr:
		if p.Value != n {
			return nil
		}
		lit, ok := f.parents[p].(*ast.CompositeLit)
		if !ok {
			return nil
		}
		switch u := underlying(typeOf(lit)).(type) {
		case *types.Struct:
			if key, ok := p.Key.(*ast.Ident); ok {
				for i := 0; i < u.NumFields(); i++ {
					if u.Field(i).Name() == key.Name {
						return u.Field(i).Type()
					}
				}
			}
		case *types.Map:
			return u.Elem()
		case *types.Slice:
			return u.Elem()
		}
	case *ast.CompositeLit:
		switch u := underlying(typeOf(p)).(type) {
		case *types.Slice:
			return u.Elem()
		case *types.Array:
			return u.Elem()
		}
	}
	return nil
}

func underlying(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return t.Underlying()
}
