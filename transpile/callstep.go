package transpile

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"
)

// callComponent checks a Call step given a component, ui.JS.Call(components.
// EditProduct{ID: p.ID, OnSave: Reload}), and is the expression registering
// its type, components.EditProduct{}, and the Action props among the
// literal's fields: a Call opens only the components a Call in the markup
// names (ui.RegisterCall), and sends its props, so a prop holding markup
// (a ui.Node, a slot, Children) or a func that isn't a ui.Action is an
// error (E0191). ok is false for a step that isn't one: a function's name.
func (e *engine) callComponent(f *fileState, call *ast.CallExpr) (reg string, actions []ast.Expr, ok bool) {
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	arg := call.Args[0]
	t := e.info.TypeOf(arg)
	if t == nil || isInvalid(t) || isBasic(t, types.IsString) {
		return "", nil, false
	}
	fail := func(n ast.Node, format string, a ...any) (string, []ast.Expr, bool) {
		e.errs.add("E0191", f.at(f.orig(n.Pos())), format, a...).
			help("open a component with its props as values: ui.JS.Call(components.EditProduct{ID: p.ID, OnSave: Reload})")
		return "", nil, true
	}
	for _, a := range call.Args[1:] {
		if at := e.info.TypeOf(a); at != nil && !isTargetNamed(f, at, "CallOption") && !isTargetNamed(f, at, "Size") {
			return fail(a, "%s follows the component %s: a Call of a component takes its props in the component's value, and CallOptions (or a Size) after it", text(a), text(arg))
		}
	}
	base := t
	if p, isPtr := t.(*types.Pointer); isPtr {
		base = p.Elem()
	}
	named, _ := types.Unalias(base).(*types.Named)
	if named == nil || named.Obj().Pkg() == nil {
		return fail(arg, "ui.JS.Call takes a function's name or a component's value, and %s is %s", text(arg), e.display(t))
	}
	if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
		return fail(arg, "ui.JS.Call takes a component's value, and %s is %s, which isn't a struct", text(arg), e.display(t))
	}
	if named.TypeParams().Len() > 0 || named.TypeArgs().Len() > 0 {
		return fail(arg, "%s is a generic component, which a Call can't open: open a concrete one", text(arg))
	}
	if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, nil, "Render"); obj == nil {
		return fail(arg, "%s isn't a component: it renders no markup", e.display(named))
	}
	if lit, isLit := ast.Unparen(stripAddr(arg)).(*ast.CompositeLit); isLit {
		for _, el := range lit.Elts {
			kv, isKV := el.(*ast.KeyValueExpr)
			key, isID := kvKey(kv, isKV)
			if !isID {
				continue
			}
			fv, _ := e.info.Uses[key].(*types.Var)
			if fv == nil {
				continue
			}
			ft := fv.Type()
			switch {
			case isTargetNamed(f, ft, "Action") || isTargetNamed(f, ft, "JSChain"):
				if e.funcArg(kv.Value) {
					actions = append(actions, kv.Value)
				}
			case e.holdsMarkup(f, ft, 0):
				return fail(kv, "%s's %s is markup (%s): a Call sends its component's props, and markup can't be sent; pass what it is made of, and let the component render it", e.display(named), key.Name, e.display(ft))
			default:
				if _, isFunc := ft.Underlying().(*types.Signature); isFunc {
					return fail(kv, "%s's %s is a func, which a Call can't send: declare it ui.Action, which takes a method of the page or a closure", e.display(named), key.Name)
				}
				switch ft.Underlying().(type) {
				case *types.Chan:
					return fail(kv, "%s's %s is a channel, which a Call can't send", e.display(named), key.Name)
				}
			}
		}
	}
	typeText, ok := e.typeExpr(f, arg, named)
	if !ok {
		return fail(arg, "%s's package isn't imported by name in this file: import it to open %s", e.display(named), e.display(named))
	}
	return typeText + "{}", actions, true
}

func stripAddr(x ast.Expr) ast.Expr {
	if u, ok := ast.Unparen(x).(*ast.UnaryExpr); ok && u.Op.String() == "&" {
		return u.X
	}
	return x
}

func kvKey(kv *ast.KeyValueExpr, ok bool) (*ast.Ident, bool) {
	if !ok {
		return nil, false
	}
	id, ok := kv.Key.(*ast.Ident)
	return id, ok
}

// holdsMarkup reports whether a prop of type t holds markup: the target's
// Node, a func returning one (a slot taking values), a component's value,
// or a slice, array, map or pointer of them, or a struct with such a field.
func (e *engine) holdsMarkup(f *fileState, t types.Type, depth int) bool {
	if depth > 4 || isTargetNamed(f, t, "Action") || isTargetNamed(f, t, "JSChain") {
		return false
	}
	node, _ := e.targetObj(f.target, "Node").(*types.TypeName)
	if node != nil {
		if types.Identical(t, node.Type()) {
			return true
		}
		if it, ok := node.Type().Underlying().(*types.Interface); ok && it.NumMethods() > 0 {
			if _, isIface := t.Underlying().(*types.Interface); !isIface && (types.Implements(t, it) || types.Implements(types.NewPointer(t), it)) {
				return true
			}
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Signature:
		for i := range u.Results().Len() {
			if e.holdsMarkup(f, u.Results().At(i).Type(), depth+1) {
				return true
			}
		}
	case *types.Slice:
		return e.holdsMarkup(f, u.Elem(), depth+1)
	case *types.Array:
		return e.holdsMarkup(f, u.Elem(), depth+1)
	case *types.Map:
		return e.holdsMarkup(f, u.Elem(), depth+1)
	case *types.Pointer:
		return e.holdsMarkup(f, u.Elem(), depth+1)
	case *types.Struct:
		for i := range u.NumFields() {
			if fl := u.Field(i); fl.Exported() && e.holdsMarkup(f, fl.Type(), depth+1) {
				return true
			}
		}
	}
	return false
}

// typeExpr is the expression naming named in f: what the Call's composite
// literal names it by, else its name (this package's) or its package's
// import name and its name.
func (e *engine) typeExpr(f *fileState, arg ast.Expr, named *types.Named) (string, bool) {
	if lit, ok := ast.Unparen(stripAddr(arg)).(*ast.CompositeLit); ok && lit.Type != nil {
		return string(f.src[f.orig(lit.Type.Pos()):f.orig(lit.Type.End())]), true
	}
	pkg := named.Obj().Pkg()
	if e.pkg != nil && pkg == e.pkg {
		return named.Obj().Name(), true
	}
	for _, im := range f.ast.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if path != pkg.Path() {
			continue
		}
		name := pkg.Name()
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "_" || name == "." {
			return "", name == "."
		}
		return name + "." + named.Obj().Name(), true
	}
	return "", false
}

// closedStep checks a Closed step: it follows a component's Call, the last
// Push or Call before it in its chain (E0191).
func (e *engine) closedStep(f *fileState, closed *ast.CallExpr) {
	at := f.at(f.orig(closed.Fun.(*ast.SelectorExpr).Sel.Pos()))
	x := closed.Fun.(*ast.SelectorExpr).X
	for {
		call, ok := ast.Unparen(x).(*ast.CallExpr)
		if !ok {
			return
		}
		name, _, ok := e.chainStep(f, call)
		switch {
		case !ok:
			return
		case name == "Call":
			if len(call.Args) > 0 && isBasic(e.info.TypeOf(call.Args[0]), types.IsString) {
				e.errs.add("E0191", at, "Closed follows a Call of a script's function, %s: it runs once the component a Call opened closes", strings.TrimSpace(string(f.src[f.orig(call.Args[0].Pos()):f.orig(call.Args[0].End())]))).
					help("give the Call a component, or drop Closed")
			}
			return
		case name == "Push":
			e.errs.add("E0191", at, "Closed follows a Push: it runs once the component a Call opened closes").
				help("put it after a Call of a component: ui.JS.Call(components.Details{ID: id}).Closed(…)")
			return
		case pushHooks[name] || name == "Timeout":
			x = call.Fun.(*ast.SelectorExpr).X
		default:
			return
		}
	}
}

// typedSuccess checks a Success (or OnSuccess) given a method taking one
// value, after a Push whose handler returns a result: the method gets that
// result (ui.Result), so its parameter takes the result's type — a scalar
// or a struct (E0168). It reports false when it reported an error.
func (e *engine) typedSuccess(f *fileState, hook *ast.CallExpr) bool {
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	sig, _ := e.info.TypeOf(hook.Args[0]).Underlying().(*types.Signature)
	if sig == nil {
		return true
	}
	var params []*types.Var
	for i := range sig.Params().Len() {
		t := sig.Params().At(i).Type()
		if (i == 0 && isContext(t)) || isPointerOrInterface(t) {
			continue
		}
		params = append(params, sig.Params().At(i))
	}
	if len(params) != 1 {
		return true
	}
	push := e.pushBefore(f, hook)
	if push == nil || len(push.Args) == 0 {
		return true
	}
	if name, _, _ := e.chainStep(f, push); name != "Push" {
		return true
	}
	hs, _ := e.info.TypeOf(push.Args[0]).Underlying().(*types.Signature)
	if hs == nil {
		return true
	}
	res := hs.Results()
	if res.Len() == 0 || res.Len() == 1 && types.Identical(res.At(0).Type(), types.Universe.Lookup("error").Type()) {
		return true // no result: the method takes the element's value, as a push of it does
	}
	rt, pt := res.At(0).Type(), params[0].Type()
	fail := func(format string, a ...any) bool {
		e.errs.add("E0168", f.at(f.orig(hook.Args[0].Pos())), format, a...).
			help("give the method one parameter of the push's result type, or pass a method taking none")
		return false
	}
	switch kind := e.resultKind(f, rt); kind {
	case "scalar", "struct":
	default:
		return fail("%s takes a value, and the push before it returns %s (%s): a Success's method gets a scalar or a struct the push returns", text(hook.Args[0]), e.display(rt), kind)
	}
	if !types.AssignableTo(rt, pt) && !(scalarParam(rt) && scalarParam(pt)) {
		return fail("%s takes %s, and the push before it returns %s: a Success's method gets the push's result", text(hook.Args[0]), e.display(pt), e.display(rt))
	}
	return true
}
