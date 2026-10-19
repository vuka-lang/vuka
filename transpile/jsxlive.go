package transpile

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/vuka-lang/vuka/diag"
)

// resolveStateful decides a stateful component's tag: a struct type whose
// pointer implements the target's Stateful (ui: embeds ui.Live and renders).
//
//	<Counter Start={5} key={id}>hi</Counter>   →   ui.Key(id, &Counter{Start: 5, Children: counterText.Render()})
//
// It is a template's hole, whose place tells the session which tag an
// instance comes from. Attributes bind to exported fields (promoted ones
// too), children to a Children field; the session keeps the instance and
// sets these on it again each time the tag renders.
func (e *engine) resolveStateful(f *fileState, c *jsxComp, expr ast.Expr, tn *types.TypeName, attrs []*jsxAttr, node types.Type, fail func(diag.Code, int, string, ...any)) {
	el := c.el
	t := tn.Type()
	if tv, ok := e.info.Types[expr]; ok && tv.IsType() && !isInvalid(tv.Type) {
		t = tv.Type
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.TypeParams().Len() > 0 && n.TypeArgs().Len() == 0 {
		inst, off, msg := e.inferStateful(f, c, n, attrs, node)
		if msg != "" {
			fail("E0106", off, "%s", msg)
			return
		}
		if inst == nil {
			c.fail = "an attribute's type is unknown"
			return
		}
		t = inst
	}
	if msg := e.statefulProblem(c.target, el.tag, t); msg != "" {
		if ptr, ok := e.views(t, node); ok {
			c.structComp, c.nodeComp, c.ptrRender = true, true, ptr
		} else if ptr, ok := e.renders(t, node); ok {
			c.structComp, c.ptrRender = true, ptr
		} else {
			fail("E0106", el.start, "%s", msg)
			return
		}
	}
	st := t.Underlying().(*types.Struct)
	kids, off, msg := e.bindFields(c, st, attrs, node, true)
	if msg != "" {
		fail("E0106", off, "%s", msg)
		return
	}
	if len(el.kids) > 0 && !kids {
		fail("E0106", el.start, "<%s> takes no children: add a Children %sNode field to %s", el.tag, c.target.q, e.display(t))
		return
	}
	for _, a := range el.attrs {
		if a.name != "key" {
			continue
		}
		c.key = &compArg{attr: a, typ: types.Typ[types.String]}
		if a.kind == 'e' {
			x := e.attrExpr(f, a)
			tv, ok := e.info.Types[x]
			if x == nil || !ok || isInvalid(tv.Type) {
				c.fail = "its key's type is unknown"
				return
			}
			c.key.typ = types.Default(tv.Type)
			if lit, ok := ast.Unparen(x).(*ast.BasicLit); ok {
				c.key.typ = types.Default(untypedLit[lit.Kind])
			}
		}
	}
	if c.structComp && c.key != nil {
		fail("E0106", c.key.attr.off, "<%s> is a struct component, its markup a function of its fields: a key goes on the element it renders, or on a stateful component", el.tag)
		return
	}
	c.stateful, c.propsTyp = !c.structComp, t
	e.finishComp(f, c)
}

// views reports whether t is a struct component that is itself a Node: a
// struct with a View() Node method — its markup — whose value (or pointer,
// reported) is a Node, rendering its View. Its tag is the literal, T{…}.
func (e *engine) views(t types.Type, node types.Type) (ptr, ok bool) {
	if _, isStruct := t.Underlying().(*types.Struct); !isStruct {
		return false, false
	}
	iface, isIface := node.Underlying().(*types.Interface)
	if !isIface {
		return false, false
	}
	m, _, _ := types.LookupFieldOrMethod(t, true, nil, "View")
	fn, isFunc := m.(*types.Func)
	if !isFunc {
		return false, false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 || !types.AssignableTo(sig.Results().At(0).Type(), node) {
		return false, false
	}
	if types.Implements(t, iface) {
		return false, true
	}
	return true, types.Implements(types.NewPointer(t), iface)
}

// renders reports whether t is a struct component of the earlier form: a
// struct with a Render() Node method, T{…}.Render(), and whether that method
// takes a pointer.
func (e *engine) renders(t types.Type, node types.Type) (ptr, ok bool) {
	if _, isStruct := t.Underlying().(*types.Struct); !isStruct {
		return false, false
	}
	m, _, _ := types.LookupFieldOrMethod(t, true, nil, "Render")
	fn, isFunc := m.(*types.Func)
	if !isFunc {
		return false, false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 || !types.AssignableTo(sig.Results().At(0).Type(), node) {
		return false, false
	}
	_, ptr = sig.Recv().Type().(*types.Pointer)
	return ptr, true
}

// inferStateful infers a generic stateful component's type arguments from its
// attributes, as inferProps does for a generic props struct: each attribute
// binds to a field of n's struct whose declared type still mentions the type
// parameters (Value T, []T, *T), and unifying that against the value's
// concrete type pins them. It returns the instantiated named type and, as a
// side effect, the text of its type arguments for the literal (c.targText);
// (nil, "") while an attribute's type is unknown, to retry next round.
func (e *engine) inferStateful(f *fileState, c *jsxComp, n *types.Named, attrs []*jsxAttr, node types.Type) (types.Type, int, string) {
	el := c.el
	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return nil, el.start, "<" + el.tag + "> is generic: write its type arguments, <" + el.tag + "[…]>"
	}
	trial := &jsxComp{el: el, target: c.target}
	if _, off, msg := e.bindFields(trial, st, attrs, node, true); msg != "" {
		return nil, off, msg
	}
	targs, u, status := e.solveTargs(f, trial.args, n.TypeParams())
	switch status {
	case inferRetry:
		return nil, 0, ""
	case inferConflict:
		return nil, el.start, conflictMsg(el.tag, u, e)
	case inferUnder:
		return nil, el.start, underMsg(el.tag)
	}
	inst, err := types.Instantiate(nil, n, targs, true)
	if err != nil {
		return nil, el.start, "<" + el.tag + ">'s inferred type arguments don't fit its constraints: write <" + el.tag + "[…]> (" + err.Error() + ")"
	}
	texts := make([]string, len(targs))
	for i, ta := range targs {
		texts[i] = e.typeTextAuto(f, ta)
	}
	c.targText = "[" + strings.Join(texts, ", ") + "]"
	return inst, 0, ""
}

// statefulProblem says why a type isn't a stateful component of the target,
// if it isn't: a struct whose pointer implements the target's Stateful.
func (e *engine) statefulProblem(tg *jsxTarget, tag string, t types.Type) string {
	what := "<" + tag + "> is a type, not a component: a component is a function returning " + tg.q + "Node"
	iface, _ := e.targetObj(tg, "Stateful").(*types.TypeName)
	if iface == nil || !tg.has("Key") {
		return what + "; " + tg.path + " has no stateful components"
	}
	if _, ok := t.Underlying().(*types.Struct); !ok {
		return what + ", or a struct whose pointer implements " + tg.q + "Stateful"
	}
	it, ok := iface.Type().Underlying().(*types.Interface)
	if !ok {
		return tg.q + "Stateful isn't an interface"
	}
	ptr := types.NewPointer(t)
	m, wrong := types.MissingMethod(ptr, it, true)
	if m == nil {
		return ""
	}
	why := "missing method " + m.Name()
	switch {
	case wrong:
		have, _, _ := types.LookupFieldOrMethod(ptr, true, m.Pkg(), m.Name())
		why = "wrong type for method " + m.Name() + ": has " + e.display(have.Type()) + ", wants " + e.display(m.Type())
	case !m.Exported():
		if h := provider(tg, m); h != "" {
			why += "; embed " + h
		}
	}
	return "<" + tag + "> isn't a stateful component: *" + e.display(t) + " doesn't implement " + tg.q + "Stateful (" + why + ")"
}

func isContext(t types.Type) bool { return isNamed(t, "context", "Context") }

// propField is a field a props struct's attribute may set: its own, or one
// promoted from an embedded struct.
type propField struct {
	name      string
	typ       types.Type
	nest      []nestStep
	ambiguous string // the embedded fields it is in twice at the same depth
}

// propFields is the fields of st an attribute can set, as Go's selectors see
// them: a field of an embedded struct is promoted unless one of the same name
// is shallower, and ambiguous when two are equally deep. Exported only, for a
// stateful component (whose unexported fields are state); fields of another
// package's struct are reached only through exported embedded fields.
func (e *engine) propFields(tg *jsxTarget, st *types.Struct, exported bool) []propField {
	type level struct {
		st   *types.Struct
		nest []nestStep
	}
	var out []propField
	taken := map[string]bool{}
	cur := []level{{st, nil}}
	for d := 0; len(cur) > 0 && d < 8; d++ {
		var next []level
		found := map[string][]propField{}
		var order []string
		for _, lv := range cur {
			for i := 0; i < lv.st.NumFields(); i++ {
				fld := lv.st.Field(i)
				visible := fld.Exported() || !exported && fld.Pkg() == e.pkg
				if fld.Embedded() && declaredBy(tg, fld.Type()) || !visible {
					continue
				}
				name := fld.Name()
				if !taken[name] {
					if found[name] == nil {
						order = append(order, name)
					}
					found[name] = append(found[name], propField{name: name, typ: fld.Type(), nest: lv.nest})
				}
				if !fld.Embedded() {
					continue
				}
				t, ptr := fld.Type(), false
				if p, ok := t.Underlying().(*types.Pointer); ok {
					t, ptr = p.Elem(), true
				}
				if est, ok := t.Underlying().(*types.Struct); ok {
					nest := append(append([]nestStep(nil), lv.nest...), nestStep{name: name, typ: t, ptr: ptr})
					next = append(next, level{est, nest})
				}
			}
		}
		for _, name := range order {
			fs := found[name]
			pf := fs[0]
			if len(fs) > 1 {
				pf.ambiguous = nestName(fs[0].nest) + " and " + nestName(fs[1].nest)
			}
			taken[name] = true
			out = append(out, pf)
		}
		cur = next
	}
	return out
}

func nestName(n []nestStep) string {
	if len(n) == 0 {
		return "the struct itself"
	}
	return n[len(n)-1].name
}

// bindFields binds attributes to a props struct's fields, and the children to
// its Children field when it has one that takes a Node; it reports whether
// the children are bound.
func (e *engine) bindFields(c *jsxComp, st *types.Struct, attrs []*jsxAttr, node types.Type, stateful bool) (bool, int, string) {
	el := c.el
	fields := e.propFields(c.target, st, stateful)
	var names []string
	for _, fl := range fields {
		names = append(names, fl.name)
	}
	used := map[int]bool{}
	for _, a := range attrs {
		i := bindName(names, a.name)
		if i < 0 {
			if stateful && hasField(st, a.name) {
				return false, a.off, "<" + el.tag + "> has no attribute " + a.name + ": unexported fields are its state; its props are its exported fields"
			}
			return false, a.off, unknownAttr(el.tag, a.name, "fields", names)
		}
		fl := fields[i]
		switch {
		case used[i]:
			return false, a.off, "<" + el.tag + "> sets " + fl.name + " twice"
		case fl.ambiguous != "":
			return false, a.off, "<" + el.tag + ">'s field " + fl.name + " is ambiguous: it is in " + fl.ambiguous
		}
		used[i] = true
		c.args = append(c.args, &compArg{key: fl.name + ": ", attr: a, typ: fl.typ, nest: fl.nest})
	}
	rest := el.kids[:0:0]
	slots := 0
	for _, k := range el.kids {
		sl, ok := k.(*jsxElem)
		if !ok || !strings.HasPrefix(sl.tag, "slot:") {
			rest = append(rest, k)
			continue
		}
		name := exportName(strings.TrimPrefix(sl.tag, "slot:"))
		i := bindName(names, name)
		switch {
		case i < 0:
			return false, sl.start, "<" + el.tag + "> has no slot " + strings.TrimPrefix(sl.tag, "slot:") + ": a slot is a " + c.target.q + "Node field, " + name
		case used[i]:
			return false, sl.start, "<" + el.tag + "> sets " + fields[i].name + " twice"
		case !types.AssignableTo(node, fields[i].typ):
			return false, sl.start, "<" + el.tag + ">'s slot " + fields[i].name + " is " + e.display(fields[i].typ) + ", not " + c.target.q + "Node"
		}
		used[i] = true
		slots++
		c.args = append(c.args, &compArg{key: fields[i].name + ": ", nodes: append([]jsxNode{}, sl.kids...), typ: fields[i].typ, nest: fields[i].nest})
	}
	if slots > 0 {
		c.rest = rest
	}
	if !hasContent(rest) {
		return slots > 0, 0, ""
	}
	if i := bindName(names, "Children"); i >= 0 && !used[i] && fields[i].ambiguous == "" && types.AssignableTo(node, fields[i].typ) {
		c.args = append(c.args, &compArg{key: fields[i].name + ": ", kids: true, typ: fields[i].typ, nest: fields[i].nest})
		return true, 0, ""
	}
	return false, 0, ""
}

// hasContent reports whether ns are more than white space.
func hasContent(ns []jsxNode) bool {
	for _, n := range ns {
		if t, ok := n.(*jsxText); !ok || strings.TrimSpace(t.val) != "" {
			return true
		}
	}
	return false
}

func hasField(st *types.Struct, name string) bool {
	for i := 0; i < st.NumFields(); i++ {
		if bindName([]string{st.Field(i).Name()}, name) == 0 {
			return true
		}
	}
	return false
}

// groupArgs orders a props literal's fields so those inside one embedded
// struct are together, at the place of the first.
func groupArgs(args []*compArg, d int) []*compArg {
	var out []*compArg
	done := map[*compArg]bool{}
	for i, a := range args {
		if done[a] {
			continue
		}
		if len(a.nest) <= d {
			out = append(out, a)
			continue
		}
		var grp []*compArg
		for _, b := range args[i:] {
			if !done[b] && len(b.nest) > d && b.nest[d].name == a.nest[d].name {
				grp, done[b] = append(grp, b), true
			}
		}
		out = append(out, groupArgs(grp, d+1)...)
	}
	return out
}

// checkEvents checks, once their types are known, the expressions of the
// elements' on… attributes: a function, nil, or a value of a type with
// methods (a target may take such a value, e.g. templ.ComponentScript). What
// a handler may take is the target's business, checked when it renders.
func (e *engine) checkEvents(f *fileState) {
	for _, t := range f.jsx {
		for _, ev := range t.events {
			if ev.checked {
				continue
			}
			x := e.attrExpr(f, ev.attr)
			tv, ok := e.info.Types[x]
			if x == nil || !ok || isInvalid(tv.Type) {
				continue
			}
			ev.checked = true
			if _, call := ast.Unparen(x).(*ast.CallExpr); call && tv.IsVoid() {
				continue // the call is the handler
			}
			if msg := e.handlerProblem(tv.Type); msg != "" {
				e.errs.add("E0107", f.at(ev.attr.off), "%s on <%s> %s", ev.attr.name, ev.tag, msg)
			}
		}
	}
}

func (e *engine) handlerProblem(t types.Type) string {
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return ""
	}
	if _, ok := t.Underlying().(*types.Signature); ok {
		return ""
	}
	if types.NewMethodSet(t).Len() > 0 || types.NewMethodSet(types.NewPointer(t)).Len() > 0 {
		return ""
	}
	return "takes a function; this is " + e.display(t)
}
