package transpile

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// A call of an exported method of the markup's receiver in an on…
// attribute is a push of that method, made when the event fires:
//
//	onChange={Choose(this.value)}     →  onChange={ui.JS.Push(p.Choose, ui.This.Value())}
//	onClick={Remove(o.ID)}            →  onClick={ui.JS.Push(p.Remove, o.ID)}
//	onClick={Reset(ctx, 0)}           →  onClick={ui.JS.Push(p.Reset, 0)}
//
// this.value, this.checked and this.<attr> (camelCase for a dashed name:
// this.dataId is data-id) are the element's reads, made in the browser.
// The method never runs while the markup renders. The same holds on a
// component's tag, for an on… attribute it passes on to the element it
// spreads its attributes on, or a prop that takes browser commands
// (ui.JSChain, any).
//
// A call of anything else keeps its meaning: an unexported method, a
// function, or, with no element read, a method returning browser commands
// or a func (a handler built while rendering), or one passed its
// dependencies itself.

// eventCallAttr reports whether a's value is an event's: an element's on…
// attribute, or one a component's tag passes on to its element or gives a
// prop taking browser commands.
func (e *engine) eventCallAttr(f *fileState, el *jsxElem, a *jsxAttr) bool {
	if a.kind != 'e' || a.stmts || !isEvent(a.name) {
		return false
	}
	c := el.comp
	if c == nil {
		return a.event
	}
	if !c.resolved || c.dead {
		return false
	}
	if a.event {
		return true
	}
	var typ types.Type
	eachArg(c.args, func(x *compArg) {
		if x.attr == a {
			typ = x.typ
		}
	})
	if typ == nil {
		return false
	}
	if _, fn := typ.Underlying().(*types.Signature); fn {
		return false
	}
	return f.target.pkg != nil && e.takesCommands(typ, f.target.pkg)
}

// lowerEventCalls lowers each event's call of a method of the markup's
// receiver in t, its components resolved, to a push.
func (e *engine) lowerEventCalls(f *fileState, t *jsxTree) {
	if f.html == nil || !f.target.has("RegisterPush") || !f.target.has("This") {
		return
	}
	walkNodes([]jsxNode{t.root}, func(n jsxNode) {
		el, ok := n.(*jsxElem)
		if !ok {
			return
		}
		for _, a := range el.attrs {
			if a.push != nil || !e.eventCallAttr(f, el, a) {
				continue
			}
			if p := e.eventPush(f, a); p != nil {
				a.push = p
				f.dropEdits(a.expr, p.pieces)
			}
		}
	})
}

// eventPush is the push an event's call lowers to, or nil when it is no
// call of a method of the markup's receiver, or keeps its meaning.
func (e *engine) eventPush(f *fileState, a *jsxAttr) *stateBind {
	call, m := e.eventMethodCall(f, a)
	if m == nil {
		return nil
	}
	sig := m.Type().(*types.Signature)
	args := e.pushedArgs(sig, call)
	reads := 0
	for _, x := range args {
		if _, _, ok := elementRead(e, x); ok {
			reads++
		} else if mentionsThis(e, x) {
			return nil // a read the method can't be sent: checkEventCall reports it
		}
	}
	if reads == 0 && (e.buildsHandler(f, sig) || e.passesDeps(sig, call)) {
		return nil
	}
	gen := func(s string) jsxPiece { return jsxPiece{text: s, keep: span{-1, -1}} }
	keep := func(x ast.Node) jsxPiece { return jsxPiece{keep: span{f.orig(x.Pos()), f.orig(x.End())}} }
	q := f.target.q
	pieces := []jsxPiece{gen(q + "JS.Push("), keep(call.Fun)}
	for _, x := range args {
		pieces = append(pieces, gen(", "))
		if read, attr, ok := elementRead(e, x); ok {
			if attr != "" {
				read += "(" + strconv.Quote(attr) + ")"
			} else {
				read += "()"
			}
			pieces = append(pieces, gen(q+"This."+read))
			continue
		}
		pieces = append(pieces, keep(x))
	}
	pieces = append(pieces, gen(")"))
	return &stateBind{start: a.expr.start, end: a.expr.end, pieces: pieces}
}

// pushedArgs are the arguments of a call a push sends: all but a leading
// context, which is the event's.
func (e *engine) pushedArgs(sig *types.Signature, call *ast.CallExpr) []ast.Expr {
	ps := sig.Params()
	if ps.Len() > 0 && isContext(ps.At(0).Type()) && len(call.Args) > 0 && isContext(e.info.TypeOf(call.Args[0])) {
		return call.Args[1:]
	}
	return call.Args
}

// passesDeps reports whether a call passes an argument for one of the
// method's pointer or interface parameters, the dependencies a push leaves
// to the app: the call is a handler as written.
func (e *engine) passesDeps(sig *types.Signature, call *ast.CallExpr) bool {
	ps := sig.Params()
	at := 0
	if ps.Len() > 0 && isContext(ps.At(0).Type()) {
		at = 1
	}
	for i := range e.pushedArgs(sig, call) {
		if at+i < ps.Len() && isPointerOrInterface(ps.At(at+i).Type()) {
			return true
		}
	}
	return false
}

// eventMethodCall is an event's call when it calls an exported method of
// the markup's receiver, with that method.
func (e *engine) eventMethodCall(f *fileState, a *jsxAttr) (*ast.CallExpr, *types.Func) {
	x := e.attrExpr(f, a)
	if x == nil {
		return nil, nil
	}
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		return nil, nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, nil
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !sel.Sel.IsExported() {
		return nil, nil
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || !e.isReceiver(f, call, id) {
		return nil, nil
	}
	return call, s.Obj().(*types.Func)
}

// isReceiver reports whether id, in n's function, names that function's
// receiver: the markup's.
func (e *engine) isReceiver(f *fileState, n ast.Node, id *ast.Ident) bool {
	obj := e.info.Uses[id]
	if obj == nil {
		return false
	}
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		fd, ok := p.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
			return false
		}
		return e.info.Defs[fd.Recv.List[0].Names[0]] == obj
	}
	return false
}

// buildsHandler reports whether a method returns a handler built while
// rendering rather than a result a push puts in place: a func, or browser
// commands.
func (e *engine) buildsHandler(f *fileState, sig *types.Signature) bool {
	if sig.Results().Len() != 1 {
		return false
	}
	t := sig.Results().At(0).Type()
	if _, fn := t.Underlying().(*types.Signature); fn {
		return true
	}
	if isNamed(t, templPath, "ComponentScript") || isTargetNamed(f, t, "Commands") || isTargetNamed(f, t, "CommandsOn") {
		return true
	}
	chain, _ := e.targetObj(f.target, "JSChain").(*types.TypeName)
	if chain == nil {
		return false
	}
	it, ok := chain.Type().Underlying().(*types.Interface)
	return ok && (types.Implements(t, it) || types.Implements(types.NewPointer(t), it))
}

// elementRead is x when it reads the element an event is on: this.value
// (Value), this.checked (Checked), this.<attr> (Attr, with the attribute's
// name).
func elementRead(e *engine, x ast.Expr) (read, attr string, ok bool) {
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok || !isThis(e, sel.X) {
		return "", "", false
	}
	switch name := sel.Sel.Name; {
	case name == "value":
		return "Value", "", true
	case name == "checked":
		return "Checked", "", true
	case misreads(name) != "":
		return "", "", false
	default:
		return "Attr", thisAttr(name), true
	}
}

// misreads is value or checked when name is a letter or two away from it.
func misreads(name string) string {
	if s := closestName(name, []string{"value", "checked"}); s != name {
		return s
	}
	return ""
}

// mentionsThis reports whether x reads this anywhere.
func mentionsThis(e *engine, x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if id, ok := n.(ast.Expr); ok && isThis(e, id) {
			found = true
		}
		return !found
	})
	return found
}

// isThis reports whether x is this, the element an event is on: a name the
// markup's Go doesn't declare.
func isThis(e *engine, x ast.Expr) bool {
	id, ok := ast.Unparen(x).(*ast.Ident)
	return ok && id.Name == "this" && e.info.Uses[id] == nil && e.info.Defs[id] == nil
}

// thisAttr is the attribute this.<name> reads: dataId is data-id.
func thisAttr(name string) string {
	var b strings.Builder
	for i, r := range name {
		if 'A' <= r && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// checkEventCalls reports, once everything is lowered, what is left of
// the events' calls: an element read outside a push (E0170) and a call
// of a method the receiver doesn't have (E0172).
func (e *engine) checkEventCalls() {
	for _, f := range e.vuka {
		if f.html == nil {
			continue
		}
		for _, t := range f.jsx {
			walkNodes([]jsxNode{t.root}, func(n jsxNode) {
				el, ok := n.(*jsxElem)
				if !ok {
					return
				}
				for _, a := range el.attrs {
					if a.push != nil || a.kind != 'e' || a.stmts || !isEvent(a.name) {
						continue
					}
					e.checkEventCall(f, a)
				}
			})
		}
	}
}

func (e *engine) checkEventCall(f *fileState, a *jsxAttr) {
	x := e.attrExpr(f, a)
	if x == nil {
		return
	}
	recv := e.markupReceiver(f, x)
	if call, ok := ast.Unparen(x).(*ast.CallExpr); ok && recv != nil {
		if name, at, ok := e.unknownMethod(f, call); ok {
			methods := exportedMethods(recv)
			err := e.errs.add("E0172", f.at(at), "%s isn't a method of %s: an event calls one of its exported methods", name, e.display(recv.Obj().Type()))
			switch s := closestName(name, methods); {
			case s != "":
				err.help("did you mean %s?", s)
			case len(methods) > 0:
				err.help("its methods are %s", strings.Join(methods, ", "))
			default:
				err.help("declare func (%s *%s) %s(…) error", f.html.recv, recv.Obj().Name(), name)
			}
			return
		}
	}
	reported := false
	ast.Inspect(x, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !isThis(e, sel.X) {
			return true
		}
		read := "this." + sel.Sel.Name
		if s := misreads(sel.Sel.Name); s != "" {
			e.errs.add("E0170", f.at(f.orig(sel.Sel.Pos())), "%s reads the attribute %s; did you mean this.%s?", read, thisAttr(sel.Sel.Name), s).
				help("this.value is the element's value, this.checked whether it is checked, this.<attr> an attribute's text")
			reported = true
			return false
		}
		owner, method := "the page's", "Save"
		if recv != nil {
			owner = recv.Obj().Name() + "'s"
			if ms := exportedMethods(recv); len(ms) > 0 {
				method = ms[0]
			}
		}
		e.errs.add("E0170", f.at(f.orig(sel.Pos())), "%s reads the element %s is on, in the browser: it is an argument of a call to one of %s exported methods, which the event pushes", read, a.name, owner).
			help("call the method with it, %s={%s(%s)}, or push another handler: %sJS.Push(handler, %sThis.Value())", a.name, method, read, f.target.q, f.target.q)
		reported = true
		return false
	})
	if reported {
		return
	}
	if call, m := e.eventMethodCall(f, a); m != nil && isHandlerCall(e.info.Types[call]) && recv != nil && !e.statefulType(f, recv) {
		sig := m.Type().(*types.Signature)
		if !e.passesDeps(sig, call) {
			e.errs.add("E0169", f.at(a.off), "%s needs a live connection because %s={…} calls %s, a handler only a live instance runs", recv.Obj().Name(), a.name, m.Name()).
				help("embed ui.Live in %s", recv.Obj().Name())
			return
		}
		e.errs.add("E0169", f.at(a.off), "%s needs a live connection because %s={…} calls %s with its dependencies, a handler only a live instance runs", recv.Obj().Name(), a.name, m.Name()).
			help("embed ui.Live in %s, or leave the dependencies out, %s={%s(…)}, for a push the app injects them into", recv.Obj().Name(), a.name, m.Name())
	}
}

// unknownMethod is the name a call names as its function when the
// markup's receiver has no such method and nothing else declares it.
func (e *engine) unknownMethod(f *fileState, call *ast.CallExpr) (string, int, bool) {
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		if e.info.Uses[fn] == nil && fn.IsExported() {
			return fn.Name, f.orig(fn.Pos()), true
		}
	case *ast.SelectorExpr:
		if id, ok := ast.Unparen(fn.X).(*ast.Ident); ok && e.isReceiver(f, call, id) && e.info.Selections[fn] == nil {
			return fn.Sel.Name, f.orig(fn.Sel.Pos()), true
		}
	}
	return "", 0, false
}

// markupReceiver is the type of the receiver of the function x is in, when
// it has one: the markup's.
func (e *engine) markupReceiver(f *fileState, x ast.Node) *types.Named {
	for p := f.parents[x]; p != nil; p = f.parents[p] {
		fd, ok := p.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
			return nil
		}
		obj := e.info.Defs[fd.Recv.List[0].Names[0]]
		if obj == nil {
			return nil
		}
		t := obj.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		n, _ := types.Unalias(t).(*types.Named)
		return n
	}
	return nil
}

// exportedMethods are the exported methods of n and its pointer, its own
// and promoted, sorted.
func exportedMethods(n *types.Named) []string {
	ms := types.NewMethodSet(types.NewPointer(n))
	var out []string
	for i := range ms.Len() {
		if m := ms.At(i).Obj(); m.Exported() && m.Pkg() != nil && m.Pkg() == n.Obj().Pkg() {
			out = append(out, m.Name())
		}
	}
	sort.Strings(out)
	return out
}
