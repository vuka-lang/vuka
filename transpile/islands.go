package transpile

import (
	"go/types"
	"sort"
	"strconv"
	"strings"
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

// resolveIsland decides a <ui.Island> wrapper: it partitions the wrapper's
// children into <ui.Template> slots and the one island-body component tag,
// binds the body's attributes to the body type's fields, and records the
// strategy, slot and fallback for the MountIsland call the writer emits. The
// body and template tags are marked consumed so they are not emitted on their
// own.
func (e *engine) resolveIsland(f *fileState, c *jsxComp, fail func(int, string, ...any)) {
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
			fail(el.start, "<%s> may contain only <ui.Template> tags and one island component", el.tag)
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
			fail(ce.start, "<%s> can't nest inside another <%s>", ce.tag, el.tag)
			return
		case body != nil:
			fail(ce.start, "<%s> takes one island component as its body; it already has <%s>", el.tag, body.tag)
			return
		default:
			body = ce
		}
	}
	if body == nil {
		fail(el.start, "<%s> needs one island component as its body", el.tag)
		return
	}
	if body.comp == nil {
		fail(body.start, "<%s>'s body must be an island component (a type with @ui.Island), not <%s>", el.tag, body.tag)
		return
	}

	// The body type: a struct whose exported fields are the props.
	bodyExpr := e.compExpr(f, body.comp)
	if bodyExpr == nil {
		c.fail = "internal: island body placeholder not found"
		return
	}
	tn, ok := e.info.Uses[compIdent(bodyExpr)].(*types.TypeName)
	if !ok {
		fail(body.start, "<%s> isn't an island component: an island body is a struct type with @ui.Island", body.tag)
		return
	}
	t := tn.Type()
	if tv, ok := e.info.Types[bodyExpr]; ok && tv.IsType() && !isInvalid(tv.Type) {
		t = tv.Type
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		fail(body.start, "<%s> isn't an island component: %s isn't a struct", body.tag, e.display(t))
		return
	}

	// Split the client:*/server:defer directive off the props attributes.
	var attrs []*jsxAttr
	var strat *jsxAttr
	for _, a := range body.attrs {
		switch {
		case a.name == "key":
			continue
		case strings.HasPrefix(a.name, "client:") || strings.HasPrefix(a.name, "server:"):
			if strat != nil {
				fail(a.off, "<%s> has more than one hydration directive", body.tag)
				return
			}
			strat = a
		default:
			attrs = append(attrs, a)
		}
	}
	if strat != nil {
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
				fail(strat.off, "client:media needs a query, e.g. client:media={\"(max-width: 600px)\"}")
				return
			}
		case "client:only":
			c.islandStrat = "ClientOnly"
		case "server:defer":
			c.islandStrat = "SSR"
		default:
			fail(strat.off, "unknown hydration directive %s", strat.name)
			return
		}
		c.strat = strat
	}

	// Bind the props: the body's attributes to the body type's fields, without
	// the body's children (those are the slot, not a Children field).
	bodyNoKids := *body
	bodyNoKids.kids = nil
	trial := &jsxComp{el: &bodyNoKids, target: c.target}
	if _, off, msg := e.bindFields(trial, st, attrs, node, true); msg != "" {
		fail(off, "%s", msg)
		return
	}
	c.args = trial.args
	c.propsTyp = t
	c.props = e.typeTextAuto(f, t)
	c.bodyTag = span{body.start + 1, body.tagEnd}
	for _, a := range c.args {
		c.islandArgTyp = append(c.islandArgTyp, e.typeTextAuto(f, a.typ))
	}

	c.slotKids = body.kids
	c.hasSlot = len(body.kids) > 0

	for _, tm := range templates {
		named := ""
		for _, a := range tm.attrs {
			if a.name == "fallback" && a.kind == 'b' {
				named = "fallback"
				continue
			}
			fail(a.off, "<ui.Template> supports only the boolean fallback slot in this version, not %s", a.name)
			return
		}
		if named != "fallback" {
			fail(tm.start, "<ui.Template> needs a slot name; this version supports fallback")
			return
		}
		if c.hasFallback {
			fail(tm.start, "<%s> has more than one fallback template", el.tag)
			return
		}
		c.fallbackKids, c.hasFallback = tm.kids, true
		tm.comp.consumed, tm.comp.resolved = true, true
	}

	body.comp.consumed, body.comp.resolved = true, true
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
	closure := false
	for i := 1; i < len(order); i++ {
		if order[i].off < order[i-1].off {
			closure = true
			break
		}
	}

	body := func() {
		w.gen(q + "MountIsland(")
		if closure {
			w.gen(c.props)
		} else {
			w.keep(c.bodyTag)
		}
		w.gen("{")
		for i, a := range c.args {
			w.gen(a.key)
			switch {
			case closure && attrPiece[i] != nil:
				w.gen(attrPiece[i].name)
			default:
				w.attrValue(a.attr)
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
