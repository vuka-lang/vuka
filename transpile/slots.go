package transpile

import (
	"go/types"
	"reflect"
	"strconv"
	"strings"
)

// Slots: a component's field (or a function component's parameter) a
// <slot:x> directly inside its tag fills.
//
//	Actions ui.Node            <slot:actions>…</slot:actions>
//	Row func(T) ui.Node        <slot:row let={t}>…{t.Name}…</slot:row>
//	Columns []Column[T]        <slot:column header="Name" let={t}>…</slot:column>, once per column
//
// A Node field takes the content; a func returning a Node takes it as the
// function's body, its parameters named by let (a scoped slot); a slice of
// structs takes each <slot:x> as an element, its attributes the struct's
// fields and its content the struct's one func or Node field (a repeated
// slot). Inside the component's own markup, <slot:x>fallback</slot:x> is
// the field, or the fallback when the caller left it out (ui.SlotOr).

// Slot kinds: how a field takes a <slot:x>.
const (
	slotNode = 'n' // a Node
	slotFunc = 'f' // a func returning the target's Node
	slotMany = 'r' // a slice of structs: one element per <slot:x>
)

// slotKind is how a field or parameter of type t takes a slot, 0 when it
// takes none.
func (e *engine) slotKind(t, node types.Type) byte {
	if t == nil || node == nil {
		return 0
	}
	if types.AssignableTo(node, t) {
		return slotNode
	}
	switch u := t.Underlying().(type) {
	case *types.Signature:
		if !u.Variadic() && u.Results().Len() == 1 && types.Identical(u.Results().At(0).Type(), node) {
			return slotFunc
		}
	case *types.Slice:
		if _, ok := u.Elem().Underlying().(*types.Struct); ok {
			if _, named := types.Unalias(u.Elem()).(*types.Named); named {
				return slotMany
			}
		}
	}
	return 0
}

// slotFor is the field or parameter a <slot:name> fills, of names (typed
// typs), and how; a repeated slot names its slice in the singular,
// <slot:column> for Columns. -1 when none has its name.
func (e *engine) slotFor(names []string, typs []types.Type, name string, node types.Type) (int, byte) {
	i := bindName(names, name)
	if i >= 0 {
		if k := e.slotKind(typs[i], node); k != 0 {
			return i, k
		}
	}
	for _, plural := range []string{name + "s", name + "es"} {
		if j := bindName(names, plural); j >= 0 && e.slotKind(typs[j], node) == slotMany {
			return j, slotMany
		}
	}
	return i, 0
}

// noSlot says why a <slot:name> fills nothing of a tag's names (typed
// typs): none has its name, or the one that does isn't a slot.
func (e *engine) noSlot(c *jsxComp, tag, name string, names []string, typs []types.Type, i int, node types.Type, what string) string {
	q := c.target.q
	if i >= 0 {
		return "<" + tag + ">'s " + names[i] + " is " + e.display(typs[i]) + ", not a slot: a slot is a " + q + "Node, a func returning one, or a slice of structs"
	}
	var slots []string
	for j, n := range names {
		switch e.slotKind(typs[j], node) {
		case slotNode, slotFunc:
			slots = append(slots, lowerFirst(n))
		case slotMany:
			slots = append(slots, lowerFirst(strings.TrimSuffix(n, "s")))
		}
	}
	msg := "<" + tag + "> has no slot " + name
	if s := closestName(name, slots); s != "" {
		return msg + "; did you mean <slot:" + s + ">?"
	}
	if len(slots) == 0 {
		return msg + ": it has no slots; a slot is a " + q + "Node " + what + ", or a func returning one"
	}
	for k, s := range slots {
		slots[k] = "<slot:" + s + ">"
	}
	return msg + "; its slots are " + strings.Join(slots, ", ")
}

// slotArg is what a <slot:x> sets a field or parameter of type t to, as kind
// takes it; a repeated slot's element is added to prev, the slot's earlier
// elements, when there are some.
func (e *engine) slotArg(c *jsxComp, sl *jsxElem, t types.Type, kind byte, prev *compArg, node types.Type) (*compArg, int, string) {
	if kind != slotMany {
		if len(sl.attrs) > 0 {
			return nil, sl.attrs[0].off, "<" + sl.tag + "> takes no attributes; let={…} names the values a scoped slot's content takes"
		}
		for _, k := range sl.kids {
			if in, ok := k.(*jsxElem); ok && in.isSlot() {
				return nil, in.start, "<" + in.tag + "> is a component's slot: write it directly inside the component's tag"
			}
		}
	}
	switch kind {
	case slotNode:
		if len(sl.lets) > 0 {
			return nil, sl.lets[0].at.start, "<" + sl.tag + "> fills " + e.display(t) + ", which takes no values: let goes on a slot of a func type, such as Row func(T) " + c.target.q + "Node"
		}
		return &compArg{slot: true, nodes: append([]jsxNode{}, sl.kids...), at: sl.start}, 0, ""
	case slotFunc:
		sig := t.Underlying().(*types.Signature)
		if msg := letsFit(sl, sig, e); msg != "" {
			return nil, sl.lets[0].at.start, msg
		}
		return &compArg{slot: true, fn: sig, lets: sl.lets, nodes: append([]jsxNode{}, sl.kids...), at: sl.start}, 0, ""
	}
	elem := t.Underlying().(*types.Slice).Elem()
	sub := &jsxComp{el: sl, target: c.target, elem: true}
	if _, off, msg := e.bindFields(sub, elem.Underlying().(*types.Struct), sl.attrs, node, false); msg != "" {
		return nil, off, msg
	}
	if prev == nil {
		prev = &compArg{slot: true, at: sl.start}
	}
	prev.elems = append(prev.elems, &slotElem{el: sl, args: sub.args, unfilled: sub.unfilled})
	return prev, 0, ""
}

// letsFit says why a slot's let doesn't fit its function: more names than
// the function takes values.
func letsFit(sl *jsxElem, sig *types.Signature, e *engine) string {
	n := sig.Params().Len()
	if len(sl.lets) <= n {
		return ""
	}
	var ts []string
	for i := 0; i < n; i++ {
		ts = append(ts, e.display(sig.Params().At(i).Type()))
	}
	switch n {
	case 0:
		return "<" + sl.tag + ">'s function takes no values: drop the let"
	case 1:
		return "<" + sl.tag + "> passes one value, " + ts[0] + ": let={" + sl.lets[0].name + "}"
	}
	return "<" + sl.tag + "> passes " + strconv.Itoa(n) + " values (" + strings.Join(ts, ", ") + "); let names " + strconv.Itoa(len(sl.lets))
}

// elemContent is the field a repeated slot's content fills: the element
// struct's one func returning a Node (its let names that function's values),
// else its Children, else its one Node field.
func (e *engine) elemContent(c *jsxComp, fields []propField, used map[int]bool, content []jsxNode, node types.Type) (*compArg, int, string) {
	sl := c.el
	var fns, nodes []int
	for i, fl := range fields {
		if used[i] || fl.ambiguous != "" {
			continue
		}
		switch e.slotKind(fl.typ, node) {
		case slotFunc:
			fns = append(fns, i)
		case slotNode:
			if types.Identical(fl.typ, node) {
				nodes = append(nodes, i)
			}
		}
	}
	arg := func(i int) *compArg {
		used[i] = true
		return &compArg{key: fields[i].name + ": ", typ: fields[i].typ, nest: fields[i].nest, slot: true, nodes: append([]jsxNode{}, content...), at: sl.start}
	}
	switch {
	case len(fns) == 1:
		a := arg(fns[0])
		a.fn, a.lets = a.typ.Underlying().(*types.Signature), sl.lets
		if msg := letsFit(sl, a.fn, e); msg != "" {
			return nil, sl.lets[0].at.start, msg
		}
		return a, 0, ""
	case len(fns) > 1:
		return nil, sl.start, "<" + sl.tag + ">'s content could fill " + fields[fns[0]].name + " or " + fields[fns[1]].name + ": put it in <slot:" + lowerFirst(fields[fns[0]].name) + "> inside it"
	case len(sl.lets) > 0:
		return nil, sl.lets[0].at.start, "<" + sl.tag + "> has no func field for its content: let names the values a func field such as Cell func(T) " + c.target.q + "Node takes"
	}
	for _, i := range nodes {
		if fields[i].name == "Children" {
			return arg(i), 0, ""
		}
	}
	if len(nodes) == 1 {
		return arg(nodes[0]), 0, ""
	}
	return nil, sl.start, "<" + sl.tag + "> takes no content: its struct has no one " + c.target.q + "Node or func field for it"
}

// required reports whether a struct field's tag marks it required:
// `vuka:"required"`.
func required(tag string) bool {
	for _, v := range strings.Split(reflect.StructTag(tag).Get("vuka"), ",") {
		if strings.TrimSpace(v) == "required" {
			return true
		}
	}
	return false
}

// eachArg calls f on args and on the arguments of their repeated slots'
// elements.
func eachArg(args []*compArg, f func(*compArg)) {
	for _, a := range args {
		if a == nil {
			continue
		}
		f(a)
		for _, el := range a.elems {
			eachArg(el.args, f)
		}
	}
}

// unfilled is the first tag, the tag itself or one of its repeated slots'
// elements, leaving out fields its struct requires (vuka:"required"), and
// those fields; none when none does.
func unfilled(c *jsxComp) (*jsxElem, []string) {
	if len(c.unfilled) > 0 {
		return c.el, c.unfilled
	}
	var at *jsxElem
	var fields []string
	eachArg(c.args, func(a *compArg) {
		for _, el := range a.elems {
			if at == nil && len(el.unfilled) > 0 {
				at, fields = el.el, el.unfilled
			}
		}
	})
	return at, fields
}

// fillHelp is how a tag fills a required field: its content for Children,
// else its slot or its attribute.
func fillHelp(field string) string {
	if field == "Children" {
		return "give the tag content: its children fill Children"
	}
	n := lowerFirst(field)
	return "fill it with <slot:" + n + ">…</slot:" + n + ">, or set " + n + "={…}"
}

// slotFunc writes a scoped slot's function: its content as the body, the
// statements of its blocks inside it, its parameters named by let.
func (w *jsxWriter) slotFunc(lets []letVar, params []string, body func()) {
	w.gen("func(")
	for i, p := range params {
		if i > 0 {
			w.gen(", ")
		}
		if i < len(lets) {
			w.keep(lets[i].at)
		} else {
			w.gen("_")
		}
		w.gen(" " + p)
	}
	w.gen(") " + w.q + "Node {\n")
	sink := w.sink
	w.sink = &stream{}
	e := w.expr(body)
	w.put(w.sink.p)
	w.sink = sink
	w.gen("return ")
	w.put(e)
	w.gen("\n}")
}

// unresolvedLet is the type of a let's names until the slot's function is
// known: none, so what reads them waits for it.
const unresolvedLet = "__vukaLet"

// hasLets reports whether a slot among ns, or among a slot's children,
// declares names.
func hasLets(ns []jsxNode) bool {
	for _, n := range ns {
		if sl, ok := n.(*jsxElem); ok && sl.isSlot() && (len(sl.lets) > 0 || hasLets(sl.kids)) {
			return true
		}
	}
	return false
}

// looseKids writes an unresolved tag's children as arguments of a Fragment,
// each slot declaring its let's names for the expressions in it.
func (w *jsxWriter) looseKids(ns []jsxNode) {
	var run []jsxNode
	flush := func() {
		if len(run) > 0 {
			w.piece(run)
			w.gen(", ")
		}
		run = nil
	}
	for _, n := range ns {
		sl, ok := n.(*jsxElem)
		if !ok || !sl.isSlot() {
			run = append(run, n)
			continue
		}
		flush()
		for _, a := range sl.attrs {
			if a.kind == 'e' {
				w.gen(w.q + "Child(")
				w.attrValue(a)
				w.gen("), ")
			}
		}
		body := func() {
			w.gen(w.q + "Fragment(")
			w.looseKids(sl.kids)
			w.gen(")")
		}
		if len(sl.lets) == 0 {
			body()
			w.gen(", ")
			continue
		}
		params := make([]string, len(sl.lets))
		for i := range params {
			params[i] = unresolvedLet
		}
		w.gen(w.q + "Child(")
		w.slotFunc(sl.lets, params, body)
		w.gen("), ")
	}
	flush()
}

// fallbackValue writes a component's own <slot:x>fallback</slot:x>: its
// field, or the fallback when the field is no content (ui.SlotOr).
func (w *jsxWriter) fallbackValue(el *jsxElem) {
	field := el.slotField
	if field == "" {
		field = exportName(strings.TrimPrefix(el.tag, "slot:"))
	}
	ref := w.f.html.recv + "." + field
	if !hasContent(el.kids) {
		w.gen(ref)
		return
	}
	w.gen(w.q + "SlotOr(" + ref + ", ")
	w.piece(el.kids)
	w.gen(")")
}

// checkFallbacks finds the field of each of a tree's fallbacks, a <slot:x>
// in the component's own markup, or reports why it has none. It reports
// false while the component's type isn't known.
func (e *engine) checkFallbacks(f *fileState, t *jsxTree) bool {
	if len(t.fallbacks) == 0 {
		return true
	}
	h := f.html
	obj, _ := e.pkg.Scope().Lookup(h.recvType).(*types.TypeName)
	node := e.nodeType(f.target)
	if obj == nil || node == nil {
		return false
	}
	st, _ := obj.Type().Underlying().(*types.Struct)
	if st == nil {
		return false
	}
	if !f.target.has("SlotOr") {
		el := t.fallbacks[0]
		e.errs.add("E0106", f.at(el.start), "<%s> with fallback content needs %sSlotOr: update %s", el.tag, f.target.q, f.target.path)
		return false
	}
	fields := e.propFields(f.target, st, false)
	var names []string
	var typs []types.Type
	for _, fl := range fields {
		names = append(names, fl.name)
		typs = append(typs, fl.typ)
	}
	ok := true
	for _, el := range t.fallbacks {
		name := strings.TrimPrefix(el.tag, "slot:")
		i := bindName(names, exportName(name))
		switch {
		case i < 0:
			c := &jsxComp{el: el, target: f.target}
			msg := e.noSlot(c, h.recvType, name, names, typs, -1, node, "field")
			if !strings.Contains(msg, "did you mean") {
				field := exportName(name)
				msg = "<" + el.tag + "> marks where " + h.recvType + "'s " + field + " goes, and " + h.recvType + " has no field " + field +
					": declare " + field + " " + f.target.q + "Node, or move the slot directly inside the component tag it fills"
			}
			e.errs.add("E0106", f.at(el.start), "%s", msg)
			ok = false
		case e.slotKind(typs[i], node) == slotFunc:
			e.errs.add("E0106", f.at(el.start), "%s's %s is %s: call it where its content goes, {%s(…)}", h.recvType, names[i], e.display(typs[i]), names[i])
			ok = false
		case !types.Identical(typs[i], node):
			e.errs.add("E0106", f.at(el.start), "%s's %s is %s, not %sNode: <%s> stands for a slot", h.recvType, names[i], e.display(typs[i]), f.target.q, el.tag)
			ok = false
		default:
			el.slotField = names[i]
		}
	}
	return ok
}
