package transpile

import (
	"go/ast"
	"go/types"
	"strconv"
)

// A component's handler prop is typed: ui.Action (any form an on…
// attribute takes), ui.JSChain (browser commands) or ui.ActionOf[T] (a
// method or closure taking a T). The event sugar follows the prop's type,
// not the attribute's spelling, so
//
//	<Button on={Remove(o.ID)} />       →  Button{On: ui.JS.Push(p.Remove, o.ID)}
//	<Button on={open = !open} />       →  Button{On: <the browser commands>}
//	<Table onSelect={Select} />        →  Table{OnSelect: p.Select}, Select checked to take a T
//
// and inside the component an ActionOf[T] field's call in an on… attribute
// is the push of what it holds, given the value:
//
//	<tr onClick={OnSelect(o)}>         →  <tr onClick={ui.JS.Push(c.OnSelect, o)}>
//
// A func prop given a call of a method (E0192), a value an ActionOf[T]
// can't hold (E0193) and an action called where no event fires, or with
// values that don't fit (E0194), are errors.

// actionType is how t holds a handler: "Action", "JSChain" or "ActionOf"
// (with its payload's type), or "" when it is none of the target's.
func actionType(f *fileState, t types.Type) (kind string, payload types.Type) {
	if t == nil || f.target == nil {
		return "", nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || !declaredBy(f.target, t) {
		return "", nil
	}
	switch name := n.Obj().Name(); name {
	case "Action", "JSChain":
		if n.TypeArgs().Len() == 0 {
			return name, nil
		}
	case "ActionOf":
		if n.TypeArgs().Len() == 1 {
			return name, n.TypeArgs().At(0)
		}
	}
	return "", nil
}

// takesEvents reports whether a prop of type t takes what an element's
// on… attribute takes, whatever the attribute's spelling.
func takesEvents(f *fileState, t types.Type) bool {
	k, _ := actionType(f, t)
	return k == "Action" || k == "JSChain"
}

// actionDisplay is an action type as the errors name it: ui.ActionOf[Order].
func (e *engine) actionDisplay(f *fileState, t types.Type) string {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return e.display(t)
	}
	s := f.target.q + n.Obj().Name()
	if n.TypeArgs().Len() == 1 {
		s += "[" + e.display(n.TypeArgs().At(0)) + "]"
	}
	return s
}

// actionField is call's field when it calls a field of an action type —
// Action, ActionOf[T] — with that field's type.
func (e *engine) actionField(f *fileState, call *ast.CallExpr) (*types.Var, types.Type) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, nil
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return nil, nil
	}
	v, _ := s.Obj().(*types.Var)
	if v == nil {
		return nil, nil
	}
	if k, _ := actionType(f, v.Type()); k == "Action" || k == "ActionOf" {
		return v, v.Type()
	}
	return nil, nil
}

// actionPush is the lowering of an event's call of an ActionOf[T] field:
// the push of the handler it holds, given the value. ok reports whether
// the event calls an action field; a call that doesn't fit — an Action's,
// or one passing other than one value — is E0194, with a nil push.
func (e *engine) actionPush(f *fileState, a *jsxAttr) (*stateBind, bool) {
	x := e.attrExpr(f, a)
	if x == nil {
		return nil, false
	}
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	v, t := e.actionField(f, call)
	if v == nil {
		return nil, false
	}
	kind, payload := actionType(f, t)
	gen := func(s string) jsxPiece { return jsxPiece{text: s, keep: span{-1, -1}} }
	keep := func(x ast.Node) jsxPiece { return jsxPiece{keep: span{f.orig(x.Pos()), f.orig(x.End())}} }
	fail := func(n ast.Node, format string, args ...any) (*stateBind, bool) {
		e.errs.add("E0194", f.at(f.orig(n.Pos())), format, args...).
			help("%s", e.actionCallHelp(f, v, t, a.name))
		return nil, true
	}
	if call.Ellipsis.IsValid() {
		return fail(call, "%s={%s(…)} spreads its values: %s is %s, which takes one value", a.name, v.Name(), v.Name(), e.actionDisplay(f, t))
	}
	if kind == "Action" {
		return fail(call, "%s={%s()} calls %s, which is %s: it is what the event does, and is given as it is", a.name, v.Name(), v.Name(), e.actionDisplay(f, t))
	}
	if len(call.Args) != 1 {
		return fail(call, "%s is %s, which takes one value, and %s={%s(…)} passes %d", v.Name(), e.actionDisplay(f, t), a.name, v.Name(), len(call.Args))
	}
	arg := call.Args[0]
	pieces := []jsxPiece{gen(f.target.q + "JS.Push("), keep(call.Fun), gen(", ")}
	if read, attr, ok := elementRead(e, arg); ok {
		switch {
		case read == "Checked" && !isBasic(payload, types.IsBoolean) && !isBasic(payload, types.IsString):
			return fail(arg, "%s is the element's checked, a bool, and %s takes %s", srcText(f, arg), v.Name(), e.display(payload))
		case read != "Checked" && !scalarParam(payload):
			return fail(arg, "%s is read from the element, a string, and %s takes %s", srcText(f, arg), v.Name(), e.display(payload))
		}
		if attr != "" {
			read += "(" + strconv.Quote(attr) + ")"
		} else {
			read += "()"
		}
		pieces = append(pieces, gen(f.target.q+"This."+read), gen(")"))
		return &stateBind{start: a.expr.start, end: a.expr.end, pieces: pieces}, true
	}
	if mentionsThis(e, arg) {
		return nil, false // checkEventCall reports the read
	}
	pieces = append(pieces, keep(arg), gen(")"))
	return &stateBind{start: a.expr.start, end: a.expr.end, pieces: pieces}, true
}

// actionCallHelp is how an action field is used: forwarded, or called in
// an on… attribute with its value.
func (e *engine) actionCallHelp(f *fileState, v *types.Var, t types.Type, attr string) string {
	if attr == "" || !isEvent(attr) {
		attr = "onClick"
	}
	if k, payload := actionType(f, t); k == "ActionOf" {
		return "call it in an on… attribute with the " + e.display(payload) + " it takes, " + attr + "={" + v.Name() + "(…)}, or forward it: " + attr + "={" + v.Name() + "}"
	}
	return "forward it to an element's on… attribute: " + attr + "={" + v.Name() + "}"
}

func srcText(f *fileState, n ast.Node) string {
	return string(f.src[f.orig(n.Pos()):f.orig(n.End())])
}

// checkActions reports, once everything is lowered, a value an ActionOf[T]
// prop can't hold (E0193), a func prop given a call of a method, or a func
// field called in an event, while the markup renders (E0192), and a call of
// an action field left outside an event (E0194).
func (e *engine) checkActions() {
	for _, f := range e.vuka {
		if f.target == nil || f.ast == nil {
			continue
		}
		events := map[*ast.CallExpr]*jsxAttr{}
		for _, t := range f.jsx {
			for _, c := range t.comps {
				if !c.resolved || c.dead {
					continue
				}
				eachArg(c.args, func(x *compArg) {
					if x.attr != nil && x.typ != nil {
						e.checkActionArg(f, c, x.attr, x.typ)
					}
				})
			}
			walkNodes([]jsxNode{t.root}, func(n jsxNode) {
				el, ok := n.(*jsxElem)
				if !ok {
					return
				}
				for _, a := range el.attrs {
					if a.kind != 'e' || a.stmts || a.push != nil || !isEvent(a.name) {
						continue
					}
					if call, ok := ast.Unparen(e.attrExpr(f, a)).(*ast.CallExpr); ok {
						events[call] = a
						if el.comp == nil {
							e.checkFuncFieldCall(f, a, call)
						}
					}
				}
			})
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			e.checkActionPush(f, call)
			v, t := e.actionField(f, call)
			if v == nil {
				return true
			}
			where := "where no event fires"
			name := ""
			if a := events[call]; a != nil {
				where, name = "in "+a.name+", which a component takes as a value", a.name
			}
			e.errs.add("E0194", f.at(f.orig(call.Pos())), "%s(…) calls %s %s: %s is %s, which runs when an event fires", v.Name(), v.Name(), where, v.Name(), e.actionDisplay(f, t)).
				help("%s", e.actionCallHelp(f, v, t, name))
			return true
		})
	}
}

// checkActionArg checks a component's attribute setting an ActionOf[T]
// prop, or a func prop.
func (e *engine) checkActionArg(f *fileState, c *jsxComp, a *jsxAttr, typ types.Type) {
	typ = e.propType(f, a, typ)
	kind, payload := actionType(f, typ)
	if kind == "" {
		if _, fn := typ.Underlying().(*types.Signature); fn && a.kind == 'e' && !a.stmts {
			e.checkFuncPropCall(f, c, a, typ)
		}
		return
	}
	if kind != "ActionOf" {
		return
	}
	field := exportName(a.name)
	want := e.actionDisplay(f, typ)
	fail := func(off int, format string, args ...any) *Error {
		return e.errs.add("E0193", f.at(off), format, args...).
			help("pass a method taking %s, %s={Choose}, or a closure, %s={func(v %s) { … }}", article(e.display(payload)), a.name, a.name, e.display(payload))
	}
	switch {
	case a.kind == 's':
		fail(a.off, "<%s>'s %s is %s, a handler taking %s, and %q is text", c.el.tag, field, want, article(e.display(payload)), a.val)
		return
	case a.kind == 'b':
		fail(a.off, "<%s>'s %s is %s, a handler taking %s, and the attribute has no value", c.el.tag, field, want, article(e.display(payload)))
		return
	case a.kind != 'e' || a.stmts:
		return // statements: lowerAttrWrites reports them
	}
	x := e.attrExpr(f, a)
	if x == nil {
		return
	}
	if call, m := e.eventMethodCall(f, a); m != nil {
		fail(f.orig(call.Pos()), "<%s>'s %s is %s: %s calls %s while the page renders, and %s takes the handler, which the component gives %s", c.el.tag, field, want, srcText(f, call), m.Name(), field, article(e.display(payload))).
			help("pass a method taking %s, not a call of one: %s={%s}", article(e.display(payload)), a.name, m.Name())
		return
	}
	t := e.info.TypeOf(x)
	if t == nil || isInvalid(t) {
		return
	}
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return
	}
	if types.Identical(t, typ) {
		return // an ActionOf[T] passed on
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		fail(f.orig(x.Pos()), "<%s>'s %s is %s, a handler taking %s, and %s is %s", c.el.tag, field, want, article(e.display(payload)), srcText(f, x), e.display(t))
		return
	}
	if msg := e.payloadProblem(sig, payload); msg != "" {
		fail(f.orig(x.Pos()), "<%s>'s %s is %s, and %s %s", c.el.tag, field, want, srcText(f, x), msg)
	}
}

// payloadProblem is what is wrong with a handler an ActionOf[T] holds: it
// takes one value, a T, besides a context.Context and its dependencies.
func (e *engine) payloadProblem(sig *types.Signature, payload types.Type) string {
	if sig.Variadic() {
		return "is variadic"
	}
	var vals []*types.Var
	ps := sig.Params()
	for i := 0; i < ps.Len(); i++ {
		t := ps.At(i).Type()
		if isContext(t) && i == 0 || isPointerOrInterface(t) && !types.AssignableTo(payload, t) {
			continue
		}
		vals = append(vals, ps.At(i))
	}
	switch {
	case len(vals) == 0:
		return "takes no " + e.display(payload)
	case len(vals) > 1:
		return "takes " + strconv.Itoa(len(vals)) + " values, and the component gives one, " + article(e.display(payload))
	case !types.AssignableTo(payload, vals[0].Type()):
		return "takes " + article(e.display(vals[0].Type())) + ", not the " + e.display(payload) + " the component gives"
	}
	return ""
}

// checkFuncPropCall reports a func prop given a call of a method of the
// page in an attribute: the method runs while the page renders.
func (e *engine) checkFuncPropCall(f *fileState, c *jsxComp, a *jsxAttr, typ types.Type) {
	call, m := e.eventMethodCall(f, a)
	if m == nil || e.buildsHandler(f, m.Type().(*types.Signature)) {
		return
	}
	e.errs.add("E0192", f.at(f.orig(call.Pos())), "<%s>'s %s is a func, %s: %s calls %s while the page renders, not when the event fires", c.el.tag, exportName(a.name), e.display(typ), srcText(f, call), m.Name()).
		help("declare the prop as %s, which takes the call as a push, or pass the method itself: %s={%s}", e.suggestAction(f, typ), a.name, m.Name())
}

// checkFuncFieldCall reports an element's event calling a func field:
// the field's func runs while the markup renders.
func (e *engine) checkFuncFieldCall(f *fileState, a *jsxAttr, call *ast.CallExpr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return
	}
	sig, ok := s.Obj().Type().Underlying().(*types.Signature)
	if !ok || e.buildsHandler(f, sig) {
		return
	}
	name := s.Obj().Name()
	e.errs.add("E0192", f.at(f.orig(call.Pos())), "%s is a func, %s: %s={%s(…)} calls it while the markup renders, not when the event fires", name, e.display(s.Obj().Type()), a.name, name).
		help("declare %s as %s: in an on… attribute, %s(…) is then the push of what it holds", name, e.suggestAction(f, s.Obj().Type()), name)
}

// suggestAction is the action type a func prop's handler fits:
// ui.ActionOf[T] for one value, else ui.Action.
func (e *engine) suggestAction(f *fileState, t types.Type) string {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		return f.target.q + "Action"
	}
	var vals []types.Type
	for i := 0; i < sig.Params().Len(); i++ {
		pt := sig.Params().At(i).Type()
		if isContext(pt) || isPointerOrInterface(pt) {
			continue
		}
		vals = append(vals, pt)
	}
	if len(vals) == 1 && f.target.has("ActionOf") {
		return f.target.q + "ActionOf[" + e.display(vals[0]) + "]"
	}
	return f.target.q + "Action"
}

// propType is the type of the field a component's attribute sets, as this
// round's types have it: the field its value is keyed by, else typ.
func (e *engine) propType(f *fileState, a *jsxAttr, typ types.Type) types.Type {
	if a.kind != 'e' {
		return typ
	}
	x := e.attrExpr(f, a)
	if x == nil {
		return typ
	}
	kv, ok := f.parents[x].(*ast.KeyValueExpr)
	if !ok || kv.Value != x {
		return typ
	}
	if id, ok := kv.Key.(*ast.Ident); ok {
		if v, ok := e.info.Uses[id].(*types.Var); ok {
			return v.Type()
		}
	}
	return typ
}

// actionMethod is the method value a struct literal's field of an action
// type is set to, p.Select, or nil.
func (e *engine) actionMethod(f *fileState, kv *ast.KeyValueExpr) ast.Expr {
	id, ok := kv.Key.(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := e.info.Uses[id].(*types.Var)
	if !ok || !v.IsField() {
		return nil
	}
	if k, _ := actionType(f, v.Type()); k != "Action" && k != "ActionOf" {
		return nil
	}
	sel, ok := ast.Unparen(kv.Value).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if s := e.info.Selections[sel]; s == nil || s.Kind() != types.MethodVal {
		return nil
	}
	return sel
}

// checkActionPush checks the value a lowered call of an ActionOf[T] field
// pushes, ui.JS.Push(c.OnSelect, o): its type is known once it is lowered.
func (e *engine) checkActionPush(f *fileState, call *ast.CallExpr) {
	if name, _, ok := e.chainStep(f, call); !ok || name != "Push" || len(call.Args) != 2 {
		return
	}
	sel, ok := ast.Unparen(call.Args[0]).(*ast.SelectorExpr)
	if !ok {
		return
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return
	}
	kind, payload := actionType(f, s.Obj().Type())
	at := e.info.TypeOf(call.Args[1])
	if kind != "ActionOf" || at == nil || isInvalid(at) || types.AssignableTo(at, payload) {
		return
	}
	if isTargetNamed(f, at, "ValueCondition") || isTargetNamed(f, at, "Condition") {
		return // an element's read: checked as it is lowered
	}
	v := s.Obj().(*types.Var)
	e.errs.add("E0194", f.at(f.orig(call.Args[1].Pos())), "%s is %s, and %s takes %s: it is %s", srcText(f, call.Args[1]), e.display(at), v.Name(), article(e.display(payload)), e.actionDisplay(f, s.Obj().Type())).
		help("%s", e.actionCallHelp(f, v, s.Obj().Type(), ""))
}
