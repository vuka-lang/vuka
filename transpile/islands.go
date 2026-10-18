package transpile

import (
	"go/ast"
	"go/constant"
	"go/types"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/diag"
)

// UI islands (Phase 1). A <ui.Island> wrapper is lowered to a ui.MountIsland
// call: the body component tag becomes the props struct literal, its client:*
// directive the hydration strategy, its children the slot, and a nested
// <ui.Template fallback> the fallback. See docs/design/islands.md.
//
//	<ui.Island>
//	  <ui.Template fallback><Skeleton/></ui.Template>
//	  <Counter client:visible count={5}><small>clicks</small></Counter>
//	</ui.Island>
//	// →
//	ui.MountIsland(Counter{Count: 5}, ui.Visible(),
//	    ui.Slot(<small>clicks</small>), ui.Fallback(<Skeleton/>))

// targetFunc is obj's name when it is a function the file's JSX target package
// declares (Island, Template), else "". It tells a <ui.Island>/<ui.Template>
// wrapper tag from an ordinary component.
func (e *engine) targetFunc(tg *jsxTarget, obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok || tg == nil || fn.Pkg() == nil || fn.Pkg().Path() != tg.path {
		return ""
	}
	return fn.Name()
}

// isIslandEmit reports whether name is an on<Emit> handler attribute: "on"
// followed by an uppercase letter (the JSX event convention — onRowClick,
// onDelete). This tells an emit binding from an ordinary prop (online, on).
func isIslandEmit(name string) bool {
	return len(name) > 2 && name[0] == 'o' && name[1] == 'n' && name[2] >= 'A' && name[2] <= 'Z'
}

// emitName is an on<Emit> attribute name lowered to its emit name: the leading
// "on" dropped and the first letter lower-cased (onRowClick → rowClick).
func emitName(attr string) string {
	s := attr[2:]
	return strings.ToLower(s[:1]) + s[1:]
}

// resolveIsland decides a <ui.Island> wrapper: it partitions the wrapper's
// children into <ui.Template> slots and the one island-body component tag,
// binds the body's attributes to the body type's fields, and records the
// strategy, slot and fallback for the MountIsland call the writer emits. The
// body and template tags are marked consumed so they are not emitted on their
// own.
func (e *engine) resolveIsland(f *fileState, c *jsxComp, fail func(diag.Code, int, string, ...any)) {
	el := c.el
	node := e.nodeType(c.target)
	if node == nil {
		c.fail = "the runtime has no Node"
		return
	}

	var templates []*jsxElem
	var body *jsxElem
	for _, k := range el.kids {
		ce, ok := k.(*jsxElem)
		if !ok {
			if t, ok := k.(*jsxText); ok && strings.TrimSpace(t.val) == "" {
				continue
			}
			fail("E0306", el.start, "<%s> may contain only <ui.Template> tags and one island component", el.tag)
			return
		}
		name := ""
		if ce.comp != nil {
			name = e.targetFunc(c.target, e.info.Uses[compIdent(e.compExpr(f, ce.comp))])
		}
		switch {
		case name == "Template":
			templates = append(templates, ce)
		case name == "Island":
			fail("E0306", ce.start, "<%s> can't nest inside another <%s>", ce.tag, el.tag)
			return
		case body != nil:
			fail("E0306", ce.start, "<%s> takes one island component as its body; it already has <%s>", el.tag, body.tag)
			return
		default:
			body = ce
		}
	}
	if body == nil {
		fail("E0306", el.start, "<%s> needs one island component as its body", el.tag)
		return
	}
	if body.comp == nil {
		fail("E0306", body.start, "<%s>'s body must be an island component (a type with @ui.Island), not <%s>", el.tag, body.tag)
		return
	}

	if body.comp == c {
		fail("E0001", body.start, "internal: island body is its wrapper")
		return
	}
	if !e.bindIsland(f, c, body, fail) {
		return
	}
	for _, tm := range templates {
		named := ""
		for _, a := range tm.attrs {
			if a.name == "fallback" && a.kind == 'b' {
				named = "fallback"
				continue
			}
			fail("E0306", a.off, "<ui.Template> supports only the boolean fallback slot in this version, not %s", a.name)
			return
		}
		if named != "fallback" {
			fail("E0306", tm.start, "<ui.Template> needs a slot name; this version supports fallback")
			return
		}
		if c.hasFallback {
			fail("E0306", tm.start, "<%s> has more than one fallback template", el.tag)
			return
		}
		c.fallbackKids, c.hasFallback = tm.kids, true
		tm.comp.consumed, tm.comp.resolved = true, true
	}

	body.comp.consumed, body.comp.resolved = true, true
	e.finishIsland(f, c)
}

// finishIsland marks an island tag decided.
func (e *engine) finishIsland(f *fileState, c *jsxComp) {
	// A stable per-placement id (file + the wrapper's source offset): identical
	// across renders, so the host's data-vk-s doesn't churn in the live-view diff.
	c.islandSite = f.name + "#" + strconv.Itoa(c.el.start)
	c.island, c.resolved = true, true
	e.progress = true
}

// island writes a <ui.Island> wrapper as a ui.MountIsland call: the props
// literal, the strategy option, then Slot and Fallback. The pieces that carry
// source spans — the body's prop expressions, the strategy value, and the slot
// and fallback markup — are emitted in document order; when the fixed argument
// order (props, strategy, slot, fallback) doesn't match that, a closure takes
// them as parameters so every kept span stays monotonic.
func (w *jsxWriter) island(c *jsxComp) {
	q := w.q

	// A span-bearing value, with its source offset and a writer for the
	// source it keeps. name is set in closure mode.
	type piece struct {
		off   int
		typ   string
		name  string
		write func()
	}
	attrPiece := make([]*piece, len(c.args))
	for i, a := range c.args {
		if a.attr != nil && a.attr.kind == 'e' {
			ex := a.attr.expr
			attrPiece[i] = &piece{off: ex.start, typ: c.islandArgTyp[i], write: func() { w.keep(ex) }}
		}
	}
	var stratPiece *piece
	if c.strat != nil && c.strat.kind == 'e' {
		ex := c.strat.expr
		stratPiece = &piece{off: ex.start, typ: "string", write: func() { w.keep(ex) }}
	}
	var slotPiece *piece
	if c.hasSlot && nodesKeep(c.slotKids) {
		kids := c.slotKids
		slotPiece = &piece{off: nodesStart(kids), typ: q + "Node", write: func() { w.frame(kids) }}
	}
	var fbPiece *piece
	if c.hasFallback && nodesKeep(c.fallbackKids) {
		kids := c.fallbackKids
		fbPiece = &piece{off: nodesStart(kids), typ: q + "Node", write: func() { w.frame(kids) }}
	}
	propPiece := func(ps []islandProp, typs []string) []*piece {
		out := make([]*piece, len(ps))
		for i, p := range ps {
			if p.attr != nil && p.attr.kind == 'e' {
				a := p.attr
				out[i] = &piece{off: a.expr.start, typ: typs[i], write: func() { w.attrValue(a) }}
			}
		}
		return out
	}
	funcPiece := propPiece(c.islandFuncs, c.islandFuncTyp)
	statePiece := propPiece(c.islandStates, c.islandStateTyp)
	emitPiece := make([]*piece, len(c.islandEmits))
	for i, em := range c.islandEmits {
		if em.attr != nil && em.attr.kind == 'e' {
			ex := em.attr.expr
			emitPiece[i] = &piece{off: ex.start, typ: "any", write: func() { w.keep(ex) }}
		}
	}

	// The span-bearing pieces in emit order; a closure is needed when that
	// isn't already their source order.
	var order []*piece
	for _, p := range attrPiece {
		if p != nil {
			order = append(order, p)
		}
	}
	for _, p := range []*piece{stratPiece, slotPiece, fbPiece} {
		if p != nil {
			order = append(order, p)
		}
	}
	for _, ps := range [][]*piece{emitPiece, funcPiece, statePiece} {
		for _, p := range ps {
			if p != nil {
				order = append(order, p)
			}
		}
	}
	closure := false
	for i := 1; i < len(order); i++ {
		if order[i].off < order[i-1].off {
			closure = true
			break
		}
	}

	body := func() {
		w.gen(q + "MountIsland(" + strconv.Quote(c.islandSite) + ", ")
		if closure {
			w.gen(c.props)
		} else {
			w.keep(c.bodyTag)
		}
		w.gen("{")
		for i, a := range c.args {
			w.gen(a.key)
			if a.wrap != "" {
				w.gen(a.wrap + "(")
			}
			if a.conv != "" {
				w.gen(a.conv + "(")
			}
			switch {
			case closure && attrPiece[i] != nil:
				w.gen(attrPiece[i].name)
			default:
				w.attrValue(a.attr)
			}
			if a.conv != "" {
				w.gen(")")
			}
			if a.wrap != "" {
				w.gen(")")
			}
			w.gen(", ")
		}
		w.gen("}")
		if c.islandStrat != "" {
			w.gen(", " + q + c.islandStrat + "(")
			switch {
			case closure && stratPiece != nil:
				w.gen(stratPiece.name)
			case c.strat != nil && c.strat.kind == 'e':
				w.keep(c.strat.expr)
			case c.strat != nil && c.strat.kind == 's':
				w.gen(strconv.Quote(c.strat.val))
			}
			w.gen(")")
		}
		if c.hasSlot {
			w.gen(", " + q + "Slot(")
			if closure && slotPiece != nil {
				w.gen(slotPiece.name)
			} else {
				w.frame(c.slotKids)
			}
			w.gen(")")
		}
		if c.hasFallback {
			w.gen(", " + q + "Fallback(")
			if closure && fbPiece != nil {
				w.gen(fbPiece.name)
			} else {
				w.frame(c.fallbackKids)
			}
			w.gen(")")
		}
		for i, em := range c.islandEmits {
			w.gen(", " + q + "Emit(" + strconv.Quote(em.name) + ", ")
			switch {
			case closure && emitPiece[i] != nil:
				w.gen(emitPiece[i].name)
			case em.attr != nil && em.attr.kind == 'e':
				w.keep(em.attr.expr)
			default:
				w.attrValue(em.attr)
			}
			w.gen(")")
		}
		for _, x := range []struct {
			call  string
			ps    []islandProp
			piece []*piece
		}{{"Callback", c.islandFuncs, funcPiece}, {"StateProp", c.islandStates, statePiece}} {
			for i, p := range x.ps {
				w.gen(", " + q + x.call + "(" + strconv.Quote(p.prop) + ", ")
				if closure && x.piece[i] != nil {
					w.gen(x.piece[i].name)
				} else {
					w.attrValue(p.attr)
				}
				w.gen(")")
			}
		}
		if c.islandOnce {
			w.gen(", " + q + "Once()")
		}
		w.gen(")")
	}

	if !closure {
		body()
		return
	}

	// Name the pieces in source order; the closure's parameters and the call's
	// arguments share that order, so the kept spans come out monotonic.
	sorted := append([]*piece(nil), order...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].off < sorted[j].off })
	var params []string
	for i, p := range sorted {
		p.name = "__a" + itoa(i+1)
		params = append(params, p.name+" "+p.typ)
	}
	w.gen("func(" + strings.Join(params, ", ") + ") " + q + "Node { return ")
	body()
	w.gen(" }(")
	for _, p := range sorted {
		p.write()
		w.gen(", ")
	}
	w.gen(")")
}

// nodesKeep reports whether lowering ns keeps any source span: a hole, a
// block, a component tag, an element with an expression attribute, or any of
// those nested. Pure markup keeps none, so it can be emitted out of source
// order safely.
func nodesKeep(ns []jsxNode) bool {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxHole, *jsxBlock:
			return true
		case *jsxElem:
			if n.comp != nil {
				return true
			}
			for _, a := range n.attrs {
				if a.kind == 'e' {
					return true
				}
			}
			if nodesKeep(n.kids) {
				return true
			}
		}
	}
	return false
}

// nodesStart is the source offset of the first of ns.
func nodesStart(ns []jsxNode) int {
	if len(ns) == 0 {
		return 0
	}
	switch n := ns[0].(type) {
	case *jsxElem:
		return n.start
	case *jsxText:
		return n.start
	case *jsxHole:
		return n.expr.start
	case *jsxBlock:
		if len(n.heads) > 0 {
			return n.heads[0].start
		}
		if n.m != nil {
			return n.m.start
		}
	}
	return 0
}

// islandProp is a function or state prop of an island tag: its wire name
// and its attribute, passed as ui.Callback / ui.StateProp.
type islandProp struct {
	prop string
	attr *jsxAttr
}

// isIslandType reports whether a type is an island of this package: an
// @ui.Island struct or an imported component's generated type.
func (e *engine) isIslandType(tn *types.TypeName) bool {
	if tn.Pkg() == nil || tn.Pkg() != e.pkg {
		return false
	}
	return e.declaredIslands[tn.Name()] || e.islands[tn.Name()] != nil
}

// bindIsland binds an island tag (body) — inside a <ui.Island> wrapper, or
// bare — to its props struct: plain props to the struct's fields (a value
// for an optional, pointer, prop wrapped by Ptr; a string-literal union's
// value checked), function props and state props to ui.Callback and
// ui.StateProp, client:* to the strategy, once to Once, on<Emit> to Emit.
// It reports false when the tag is decided another way: failed, or
// waiting for types (c.fail).
func (e *engine) bindIsland(f *fileState, c *jsxComp, body *jsxElem, fail func(diag.Code, int, string, ...any)) bool {
	node := e.nodeType(c.target)
	c.islandEmits, c.islandFuncs, c.islandStates, c.islandOnce, c.strat, c.islandStrat, c.args = nil, nil, nil, false, nil, "", nil
	c.islandFuncTyp, c.islandStateTyp = nil, nil
	bodyExpr := e.compExpr(f, body.comp)
	if bodyExpr == nil {
		c.fail = "internal: island body placeholder not found"
		return false
	}
	tn, ok := e.info.Uses[compIdent(bodyExpr)].(*types.TypeName)
	if !ok {
		fail("E0307", body.start, "<%s> isn't an island component: an island body is a struct type with @ui.Island", body.tag)
		return false
	}
	t := tn.Type()
	if tv, ok := e.info.Types[bodyExpr]; ok && tv.IsType() && !isInvalid(tv.Type) {
		t = tv.Type
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		fail("E0307", body.start, "<%s> isn't an island component: %s isn't a struct", body.tag, e.display(t))
		return false
	}
	fields := e.propFields(c.target, st, true)
	var names []string
	for _, fl := range fields {
		names = append(names, fl.name)
	}
	typeOf := func(a *jsxAttr) (types.Type, ast.Expr, bool) {
		x := e.attrExpr(f, a)
		tv, ok := e.info.Types[x]
		if x == nil || !ok || isInvalid(tv.Type) {
			c.fail = "an attribute's type is unknown"
			return nil, nil, false
		}
		return tv.Type, x, true
	}

	var attrs []*jsxAttr
	for _, a := range body.attrs {
		switch {
		case a.name == "key":
			continue
		case strings.HasPrefix(a.name, "client:") || strings.HasPrefix(a.name, "server:"):
			if c.strat != nil {
				fail("E0308", a.off, "<%s> has more than one hydration directive", body.tag)
				return false
			}
			c.strat = a
			continue
		}
		i := bindName(names, a.name)
		if i < 0 {
			switch {
			case a.name == "once" && a.kind == 'b':
				c.islandOnce = true
			case isIslandEmit(a.name):
				c.islandEmits = append(c.islandEmits, islandEmit{name: emitName(a.name), attr: a})
			default:
				attrs = append(attrs, a) // bindFields says it's unknown
			}
			continue
		}
		fl := fields[i]
		prop := jsonName(st, fl)
		if sig, ok := fl.typ.Underlying().(*types.Signature); ok {
			if a.kind != 'e' {
				fail("E0309", a.off, "<%s>'s %s takes a function: a live view method or JS commands", body.tag, a.name)
				return false
			}
			ht, _, ok := typeOf(a)
			if !ok {
				return false
			}
			if b, ok := ht.(*types.Basic); ok && b.Kind() == types.UntypedNil {
				continue
			}
			if msg := e.callbackProblem(c.target, sig, ht); msg != "" {
				fail("E0309", a.off, "<%s>'s %s %s", body.tag, a.name, msg)
				return false
			}
			c.islandFuncs = append(c.islandFuncs, islandProp{prop, a})
			c.islandFuncTyp = append(c.islandFuncTyp, "any")
			continue
		}
		if a.kind == 'e' {
			at, x, ok := typeOf(a)
			if !ok {
				return false
			}
			want := fl.typ
			if p, ok := want.Underlying().(*types.Pointer); ok {
				want = p.Elem()
			}
			if elem, ok := stateOf(at); ok {
				if _, toState := stateOf(fl.typ); !toState {
					if !types.AssignableTo(elem, want) {
						fail("E0309", a.off, "%s is a ui.State[%s], and <%s>'s %s takes %s", e.nodeText(f, x), e.display(elem), body.tag, a.name, e.display(want))
						return false
					}
					c.islandStates = append(c.islandStates, islandProp{prop, a})
					c.islandStateTyp = append(c.islandStateTyp, e.typeTextAuto(f, at))
					continue
				}
			} else if e.readsState(x) {
				if !types.AssignableTo(types.Default(at), want) {
					fail("E0309", a.off, "%s reads a ui.State of %s, and <%s>'s %s takes %s", e.nodeText(f, x), e.display(types.Default(at)), body.tag, a.name, e.display(want))
					return false
				}
				bd := &binder{e: e, f: f, b: &stateBind{}, deps: map[string]int{}}
				if base, path := bd.fieldPath(x); base != nil {
					a.bind = &stateBind{call: "StateField", field: stateFieldText(c.target.q, bd.text(base), path)}
				} else {
					b, err := e.lowerState(f, x)
					if err != nil {
						fail("E0124", err.off, "%s", err.msg)
						return false
					}
					b.call = "NewDerived"
					a.bind = b
				}
				c.islandStates = append(c.islandStates, islandProp{prop, a})
				c.islandStateTyp = append(c.islandStateTyp, c.target.q+"State["+e.typeTextAuto(f, types.Default(at))+"]")
				continue
			}
		}
		attrs = append(attrs, a)
	}
	if c.strat != nil {
		strat := c.strat
		switch strat.name {
		case "client:load":
			c.islandStrat = "Load"
		case "client:idle":
			c.islandStrat = "Idle"
		case "client:visible":
			c.islandStrat = "Visible"
		case "client:media":
			c.islandStrat = "Media"
			if strat.kind == 'b' {
				fail("E0308", strat.off, "client:media needs a query, e.g. client:media={\"(max-width: 600px)\"}")
				return false
			}
		case "client:only":
			c.islandStrat = "ClientOnly"
		case "server:defer":
			c.islandStrat = "SSR"
		default:
			fail("E0308", strat.off, "unknown hydration directive %s", strat.name)
			return false
		}
	}

	// Bind the props: the body's attributes to the body type's fields, without
	// the body's children (those are the slot, not a Children field).
	bodyNoKids := *body
	bodyNoKids.kids = nil
	trial := &jsxComp{el: &bodyNoKids, target: c.target}
	if _, off, msg := e.bindFields(trial, st, attrs, node, true); msg != "" {
		fail("E0309", off, "%s", msg)
		return false
	}
	c.args = trial.args
	c.islandArgTyp = nil
	for _, a := range c.args {
		typ := a.typ
		if p, ok := typ.Underlying().(*types.Pointer); ok && a.attr != nil {
			wrap := a.attr.kind != 'e'
			if !wrap {
				at, _, ok := typeOf(a.attr)
				if !ok {
					return false
				}
				_, isPtr := at.Underlying().(*types.Pointer)
				b, isBasic := at.(*types.Basic)
				wrap = !isPtr && !(isBasic && b.Kind() == types.UntypedNil)
			}
			if wrap {
				typ = p.Elem()
				a.wrap = c.target.q + "Ptr[" + e.typeTextAuto(f, typ) + "]"
			}
		}
		if b, ok := typ.(*types.Basic); ok && b.Kind() == types.Float64 && a.attr != nil && a.attr.kind == 'e' {
			// A number prop takes any Go number.
			if at, x, ok := typeOf(a.attr); ok {
				if ab, ok := at.(*types.Basic); ok && ab.Info()&types.IsNumeric != 0 && ab.Info()&types.IsUntyped == 0 && ab.Kind() != types.Float64 && e.info.Types[x].Value == nil {
					a.conv = "float64"
				}
			} else {
				return false
			}
		}
		if msg := e.enumProblem(f, a, typ); msg != "" {
			fail("E0309", a.attr.off, "<%s>'s %s %s", body.tag, a.attr.name, msg)
			return false
		}
		if a.conv != "" {
			at, _, _ := typeOf(a.attr)
			typ = at
		}
		c.islandArgTyp = append(c.islandArgTyp, e.typeTextAuto(f, typ))
	}
	c.propsTyp = t
	c.props = e.typeTextAuto(f, t)
	c.bodyTag = span{body.start + 1, body.tagEnd}
	c.slotKids = body.kids
	c.hasSlot = len(body.kids) > 0
	return true
}

// jsonName is a props field's wire name: its json tag's, else its name.
func jsonName(st *types.Struct, fl propField) string {
	if len(fl.nest) == 0 {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() == fl.name {
				if n, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("json"), ","); n != "" && n != "-" {
					return n
				}
			}
		}
	}
	return fl.name
}

// enumProblem checks a value given to a prop of an imported island's string
// union type: a literal must be one of its values.
func (e *engine) enumProblem(f *fileState, a *compArg, typ types.Type) string {
	n, ok := types.Unalias(typ).(*types.Named)
	if !ok || n.Obj().Pkg() != e.pkg || a.attr == nil {
		return ""
	}
	var vals []string
	for _, im := range e.islands {
		if v, ok := im.enums[n.Obj().Name()]; ok {
			vals = v
		}
	}
	if vals == nil {
		return ""
	}
	val, lit := "", false
	switch a.attr.kind {
	case 's':
		val, lit = a.attr.val, true
	case 'e':
		x := e.attrExpr(f, a.attr)
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
			if b, ok := tv.Type.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
				val, lit = constant.StringVal(tv.Value), true
			}
		}
	}
	if !lit {
		return ""
	}
	for _, v := range vals {
		if v == val {
			return ""
		}
	}
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = strconv.Quote(v)
	}
	return "takes " + strings.Join(quoted, ", ") + ", not " + strconv.Quote(val)
}

// callbackProblem says why a value can't be a function prop of type want:
// it takes a live view method (a func after an optional context.Context
// taking nothing or one payload the function's first argument fits,
// returning nothing or an error) or JS commands.
func (e *engine) callbackProblem(tg *jsxTarget, want *types.Signature, got types.Type) string {
	if obj, ok := e.targetObj(tg, "JSChain").(*types.TypeName); ok {
		if iface, ok := obj.Type().Underlying().(*types.Interface); ok && types.Implements(got, iface) {
			return ""
		}
	}
	hs, ok := got.Underlying().(*types.Signature)
	if !ok {
		return "takes a function — a live view method or JS commands; this is " + e.display(got)
	}
	r := hs.Results()
	if r.Len() > 1 || r.Len() == 1 && !types.Identical(r.At(0).Type(), errorType) {
		return "takes a handler returning nothing or an error; this returns " + e.display(r.At(0).Type())
	}
	params := hs.Params()
	i := 0
	if params.Len() > 0 && isContext(params.At(0).Type()) {
		i = 1
	}
	switch params.Len() - i {
	case 0:
		return ""
	case 1:
	default:
		return "calls its handler with one payload, its first argument; this handler takes " + strconv.Itoa(params.Len()-i)
	}
	ht := params.At(i).Type()
	if want.Params().Len() == 0 {
		return "passes no argument; this handler takes " + e.display(ht)
	}
	at := want.Params().At(0).Type()
	if want.Variadic() && want.Params().Len() == 1 {
		at = at.(*types.Slice).Elem()
	}
	if types.AssignableTo(at, ht) || isAnyIface(at) || numeric(at) && numeric(ht) {
		return ""
	}
	return "passes " + e.display(at) + " to its handler; this handler takes " + e.display(ht)
}

func isAnyIface(t types.Type) bool {
	i, ok := t.Underlying().(*types.Interface)
	return ok && i.Empty()
}

func numeric(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsNumeric != 0
}

// nodeText is the source of an expression of this round's tree.
func (e *engine) nodeText(f *fileState, x ast.Node) string { return f.nodeText(x) }
