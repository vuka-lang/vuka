package transpile

import (
	"go/ast"
	"go/types"
	"strconv"
)

// resolveStateful decides a stateful component's tag: a struct type whose
// pointer implements the target's Stateful (ui: embeds ui.Live and renders).
//
//	<Counter Start={5} key={id}>hi</Counter>   →   ui.Component("x.vuka#3", id, &Counter{Start: 5, Children: ui.Fragment(ui.Text("hi"), ), })
//
// The site, the file and the tag's ordinal among its component tags, tells
// the session which tag an instance comes from. Attributes bind to exported fields (promoted ones too), children to a
// Children field; the session keeps the instance and sets these on it again
// each time the tag renders.
func (e *engine) resolveStateful(f *fileState, c *jsxComp, expr ast.Expr, tn *types.TypeName, attrs []*jsxAttr, node types.Type, fail func(int, string, ...any)) {
	el := c.el
	t := tn.Type()
	if tv, ok := e.info.Types[expr]; ok && tv.IsType() && !isInvalid(tv.Type) {
		t = tv.Type
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.TypeParams().Len() > 0 && n.TypeArgs().Len() == 0 {
		fail(el.start, "<%s> is generic: write its type arguments, <%s[…]>", el.tag, el.tag)
		return
	}
	if msg := e.statefulProblem(c.target, el.tag, t); msg != "" {
		fail(el.start, "%s", msg)
		return
	}
	st := t.Underlying().(*types.Struct)
	kids, off, msg := e.bindFields(c, st, attrs, node, true)
	if msg != "" {
		fail(off, "%s", msg)
		return
	}
	if len(el.kids) > 0 && !kids {
		fail(el.start, "<%s> takes no children: add a Children %sNode field to %s", el.tag, c.target.q, e.display(t))
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
	n := 1
	for _, t := range f.jsx {
		for _, o := range t.comps {
			if o.el.start < el.start {
				n++
			}
		}
	}
	c.stateful, c.propsTyp = true, t
	c.site = f.name + "#" + strconv.Itoa(n)
	e.finishComp(f, c)
}

// statefulProblem says why a type isn't a stateful component of the target,
// if it isn't: a struct whose pointer implements the target's Stateful.
func (e *engine) statefulProblem(tg *jsxTarget, tag string, t types.Type) string {
	what := "<" + tag + "> is a type, not a component: a component is a function returning " + tg.q + "Node"
	iface, _ := e.targetObj(tg, "Stateful").(*types.TypeName)
	if iface == nil || !tg.has("Component") {
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
	if len(el.kids) == 0 {
		return false, 0, ""
	}
	if i := bindName(names, "Children"); i >= 0 && !used[i] && fields[i].ambiguous == "" && types.AssignableTo(node, fields[i].typ) {
		c.args = append(c.args, &compArg{key: fields[i].name + ": ", kids: true, typ: fields[i].typ, nest: fields[i].nest})
		return true, 0, ""
	}
	return false, 0, ""
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
			if msg := e.handlerProblem(tv.Type); msg != "" {
				e.errs.add(f.at(ev.attr.off), "%s on <%s> %s", ev.attr.name, ev.tag, msg)
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
