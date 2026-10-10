package transpile

import (
	"go/ast"
	"go/types"
	"strconv"
)

// resolveStateful decides a stateful component's tag: a struct type embedding
// vuka.Live, whose pointer renders.
//
//	<Counter Start={5} key={id}>hi</Counter>   →   vuka.Component("x.vuka#3", id, &Counter{Start: 5, Children: vuka.Fragment(vuka.Text("hi"), ), })
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
	if msg := e.statefulProblem(el.tag, t, node); msg != "" {
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
		fail(el.start, "<%s> takes no children: add a Children vuka.Node field to %s", el.tag, e.display(t))
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

// runtimeObj is a name of the runtime package, if the package imports it.
func (e *engine) runtimeObj(name string) types.Object {
	for _, p := range e.pkg.Imports() {
		if p.Path() == RuntimePath {
			return p.Scope().Lookup(name)
		}
	}
	return nil
}

// statefulProblem says why a type isn't a stateful component, if it isn't.
func (e *engine) statefulProblem(tag string, t, node types.Type) string {
	what := "<" + tag + "> is a type, not a component: a component is a function returning a vuka.Node, or a struct embedding vuka.Live with a Render() vuka.Node method"
	if _, ok := t.Underlying().(*types.Struct); !ok {
		return what
	}
	iface := e.runtimeObj("Stateful")
	if iface == nil {
		return "the Vuka runtime this module requires has no stateful components; update it: go get " + RuntimePath + "@latest"
	}
	ptr := types.NewPointer(t)
	live, _, _ := types.LookupFieldOrMethod(ptr, true, iface.Pkg(), "vukaLive")
	if live == nil {
		return "<" + tag + "> is a struct that doesn't embed vuka.Live: a stateful component embeds vuka.Live and has a Render() vuka.Node method"
	}
	if lf, _, _ := types.LookupFieldOrMethod(ptr, true, iface.Pkg(), "Live"); lf != nil {
		if _, isPtr := lf.Type().(*types.Pointer); isPtr {
			return "<" + tag + "> embeds *vuka.Live; embed vuka.Live by value"
		}
	}
	render, _, _ := types.LookupFieldOrMethod(ptr, true, e.pkg, "Render")
	fn, ok := render.(*types.Func)
	if !ok {
		return "<" + tag + "> embeds vuka.Live but has no Render() vuka.Node method"
	}
	if sig := fn.Signature(); sig.Params().Len() != 0 || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), node) {
		return "<" + tag + ">'s Render is " + e.display(sig) + "; a stateful component's is func() vuka.Node"
	}
	if m, _, _ := types.LookupFieldOrMethod(ptr, true, e.pkg, "Mount"); m != nil {
		sig, _ := m.Type().(*types.Signature)
		ok := sig != nil && sig.Results().Len() <= 1 && (sig.Results().Len() == 0 || types.Identical(sig.Results().At(0).Type(), errorType)) &&
			(sig.Params().Len() == 0 || sig.Params().Len() == 1 && isContext(sig.Params().At(0).Type()))
		if !ok {
			return "<" + tag + ">'s Mount is " + e.display(m.Type()) + "; it takes nothing or a context.Context, and returns nothing or an error"
		}
	}
	if !types.Implements(ptr, iface.Type().Underlying().(*types.Interface)) {
		return what
	}
	return ""
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
func (e *engine) propFields(st *types.Struct, exported bool) []propField {
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
				if isRuntimeNamed(fld.Type(), "Live") || !visible {
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
	fields := e.propFields(st, stateful)
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
// elements' on… attributes: a function the live runtime can call, or a
// templ.ComponentScript.
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
	if isNamed(t, templPath, "ComponentScript") {
		return ""
	}
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return ""
	}
	want := "takes a func(), a func taking the event's value (a string, number or bool), a form (a struct, or url.Values) or a vuka.Event, optionally after a context.Context, returning nothing or an error"
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		return want + "; this is " + e.display(t)
	}
	ps := sig.Params()
	i := 0
	if ps.Len() > 0 && isContext(ps.At(0).Type()) {
		i = 1
	}
	rs := sig.Results()
	if sig.Variadic() || ps.Len()-i > 1 || rs.Len() > 1 || rs.Len() == 1 && !types.Identical(rs.At(0).Type(), errorType) ||
		ps.Len()-i == 1 && !payloadOK(ps.At(i).Type()) {
		return want + "; this is " + e.display(t)
	}
	return ""
}

func payloadOK(t types.Type) bool {
	if isRuntimeNamed(t, "Event") {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) != 0
	case *types.Struct:
		return true
	case *types.Pointer:
		_, ok := u.Elem().Underlying().(*types.Struct)
		return ok
	case *types.Map:
		k, kok := u.Key().Underlying().(*types.Basic)
		s, sok := u.Elem().Underlying().(*types.Slice)
		if !kok || !sok || k.Kind() != types.String {
			return false
		}
		v, ok := s.Elem().Underlying().(*types.Basic)
		return ok && v.Kind() == types.String
	}
	return false
}
