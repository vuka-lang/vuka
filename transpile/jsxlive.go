package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
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
	var urlKey *jsxAttr
	if !c.structComp {
		var rest []*jsxAttr
		for _, a := range attrs {
			if a.name == "urlKey" && !hasField(st, "urlKey") && !hasField(st, "URLKey") && !hasField(st, "UrlKey") {
				urlKey = a
				continue
			}
			rest = append(rest, a)
		}
		attrs = rest
	}
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
		if c.structComp {
			if fl := keyField(e.propFields(c.target, st, true)); fl != nil {
				// a struct component with a Key field takes the key as it:
				// <ui.Form for={row.Form} key={row.ID}>, the form's identity
				c.args = append(c.args, &compArg{key: fl.name + ": ", attr: a, typ: fl.typ, nest: fl.nest})
				continue
			}
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
	if urlKey != nil {
		if !c.target.has("URLKey") {
			fail("E0106", urlKey.off, "<%s urlKey>: %s has no URLKey; update it (urlKey needs github.com/vuka-lang/ui/v2 v2.4.0 or later)", el.tag, c.target.path)
			return
		}
		c.urlKey = &compArg{attr: urlKey, typ: types.Typ[types.String]}
		if urlKey.kind == 'e' {
			x := e.attrExpr(f, urlKey)
			tv, ok := e.info.Types[x]
			if x == nil || !ok || isInvalid(tv.Type) {
				c.fail = "its urlKey's type is unknown"
				return
			}
			if !types.AssignableTo(tv.Type, types.Typ[types.String]) && !types.AssignableTo(types.Default(tv.Type), types.Typ[types.String]) {
				fail("E0106", urlKey.off, "<%s>'s urlKey must be a string, the name its URL parameters go under; this is %s", el.tag, e.display(tv.Type))
				return
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
	required  bool   // tagged vuka:"required"
	pkg       *types.Package
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
					found[name] = append(found[name], propField{name: name, typ: fld.Type(), nest: lv.nest, required: required(lv.st.Tag(i)), pkg: fld.Pkg()})
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

// bindFields binds attributes to a props struct's fields, its <slot:x>
// children to the fields they fill, and the rest of its children to its
// Children field when it has one that takes a Node — a repeated slot's
// element (c.elem) to its one func or Node field; it reports whether the
// children are bound. The required fields nothing sets are c.unfilled.
func (e *engine) bindFields(c *jsxComp, st *types.Struct, attrs []*jsxAttr, node types.Type, stateful bool) (bool, int, string) {
	el := c.el
	c.args, c.rest, c.pre, c.unfilled, c.ctxWrap = nil, nil, nil, nil, false // a round that left the tag unresolved may have bound them
	fields := e.propFields(c.target, st, stateful)
	var names []string
	var typs []types.Type
	for _, fl := range fields {
		names = append(names, fl.name)
		typs = append(typs, fl.typ)
	}
	used := map[int]bool{}
	attrsAt, uiAttrs := -1, false
	if !stateful && !c.elem && c.target.has("TemplAttrs") {
		attrsAt = templAttrsField(fields)
	}
	if attrsAt < 0 && !c.elem && c.target != nil {
		// a struct whose Attrs field is a []ui.Attr takes the tag's other
		// HTML attributes there (<ui.Form autocomplete="off">)
		if attrsAt = uiAttrsField(fields, c.target.path); attrsAt >= 0 {
			uiAttrs = true
		}
	}
	var extra []*compArg
	for _, a := range attrs {
		i := bindName(names, a.name)
		if a.name == "model" && a.kind == 'e' && c.target != nil {
			// model={Form.Email} on a component: its Model field, a
			// ui.FieldRef its markup spreads on its control
			if i < 0 || !isNamed(fields[i].typ, c.target.path, "FieldRef") {
				return false, a.off, "<" + el.tag + "> takes no model: a component takes model={Form.Email} when its props have a field Model ui.FieldRef, which its markup gives its control (<input model={Model}/>)"
			}
			a.ref = true
		}
		if i < 0 {
			if stateful && hasField(st, a.name) {
				return false, a.off, "<" + el.tag + "> has no attribute " + a.name + ": unexported fields are its state; its props are its exported fields"
			}
			if attrsAt >= 0 && htmlAttr(a.name, names) {
				if a.kind == 'e' && isEvent(a.name) && !a.stmts {
					a.event = true
				}
				extra = append(extra, &compArg{attr: a, typ: types.Universe.Lookup("any").Type()})
				continue
			}
			if attrsAt >= 0 && uiAttrs {
				return false, a.off, unknownAttr(el.tag, a.name, "fields", names) + ` (an HTML attribute of that name: attrs={[]ui.Attr{{Name: "` + a.name + `", Value: …}}})`
			}
			if attrsAt >= 0 {
				return false, a.off, unknownAttr(el.tag, a.name, "fields", names) + ` (an HTML attribute of that name: attributes={templ.Attributes{"` + a.name + `": …}})`
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
		c.args = append(c.args, &compArg{key: fl.name + ": ", attr: a, typ: fl.typ, nest: fl.nest,
			nodeText: a.kind == 's' && node != nil && types.Identical(fl.typ, node)})
		if fl.name == "For" && a.kind == 'e' && fl.pkg != nil && c.target.pkg != nil && fl.pkg.Path() == c.target.pkg.Path() && isAny(fl.typ) {
			a.ref = true // a form component's for={Form.Email}: a reference (forms.go)
		}
	}
	if attrsAt >= 0 {
		var base *compArg
		for _, a := range c.args {
			if a.key == fields[attrsAt].name+": " && len(a.nest) == len(fields[attrsAt].nest) {
				base = a
			}
		}
		if base == nil && len(extra) > 0 {
			base = &compArg{key: fields[attrsAt].name + ": ", typ: fields[attrsAt].typ, nest: fields[attrsAt].nest}
			c.args = append(c.args, base)
			used[attrsAt] = true
		}
		switch {
		case base != nil && uiAttrs:
			base.uiAttrs, base.extra = len(extra) > 0, extra
		case base != nil:
			base.templAttrs, base.extra = true, extra
			c.ctxWrap = true
		}
	}
	rest := el.kids[:0:0]
	slots := 0
	repeated := map[int]*compArg{}
	for _, k := range el.kids {
		sl, ok := k.(*jsxElem)
		if !ok || !sl.isSlot() {
			rest = append(rest, k)
			continue
		}
		name := strings.TrimPrefix(sl.tag, "slot:")
		i, kind := e.slotFor(names, typs, exportName(name), node)
		if kind == 0 {
			return false, sl.start, e.noSlot(c, el.tag, name, names, typs, i, node, "field")
		}
		prev := repeated[i]
		switch {
		case used[i] && prev == nil:
			return false, sl.start, "<" + el.tag + "> sets " + fields[i].name + " twice"
		case fields[i].ambiguous != "":
			return false, sl.start, "<" + el.tag + ">'s field " + fields[i].name + " is ambiguous: it is in " + fields[i].ambiguous
		}
		a, off, msg := e.slotArg(c, sl, fields[i].typ, kind, prev, node)
		if msg != "" {
			return false, off, msg
		}
		if prev == nil {
			a.key, a.typ, a.nest = fields[i].name+": ", fields[i].typ, fields[i].nest
			c.args = append(c.args, a)
			if kind == slotMany {
				repeated[i] = a
			}
		}
		used[i] = true
		slots++
	}
	content := rest
	if slots > 0 && !c.elem {
		c.rest, c.pre = splitStmts(rest)
		content = c.rest
	}
	kids := slots > 0
	switch {
	case c.elem && (hasContent(content) || len(el.lets) > 0):
		a, off, msg := e.elemContent(c, fields, used, content, node)
		if msg != "" {
			return false, off, msg
		}
		c.args = append(c.args, a)
		kids = true
	case !hasContent(content):
	default:
		i := bindName(names, "Children")
		if i < 0 || used[i] || fields[i].ambiguous != "" || !types.AssignableTo(node, fields[i].typ) {
			return false, 0, ""
		}
		c.args = append(c.args, &compArg{key: fields[i].name + ": ", kids: true, typ: fields[i].typ, nest: fields[i].nest})
		used[i], kids = true, true
	}
	for i, fl := range fields {
		if fl.required && !used[i] {
			c.unfilled = append(c.unfilled, fl.name)
		}
	}
	return kids, 0, ""
}

// templAttrsField is the props' Attributes field taking templ.Attributes,
// where a tag's attributes that aren't fields go (templUI's, shadcn-templ's
// props), or -1.
// uiAttrsField is the index of a props struct's Attrs field when it is a
// []Attr of the JSX target (ui.Attr), -1 when it has none.
func uiAttrsField(fields []propField, target string) int {
	for i, fl := range fields {
		if fl.name != "Attrs" || fl.ambiguous != "" {
			continue
		}
		if sl, ok := types.Unalias(fl.typ).(*types.Slice); ok && isNamed(sl.Elem(), target, "Attr") {
			return i
		}
	}
	return -1
}

func templAttrsField(fields []propField) int {
	for i, fl := range fields {
		if fl.name == "Attributes" && fl.ambiguous == "" && isNamed(fl.typ, templPath, "Attributes") {
			return i
		}
	}
	return -1
}

// droppedHandlers warns of an on… handler in a templ.Attributes literal of
// a component tag's attribute that no ui.TemplAttrs binds: templ renders no
// function, so the event would do nothing (W0112).
func (e *engine) droppedHandlers(f *fileState) {
	if f.attrWarns != nil {
		return
	}
	f.attrWarns = ErrorList{}
	for _, t := range f.jsx {
		for _, c := range t.comps {
			if !c.resolved || c.dead {
				continue
			}
			bound := map[*jsxAttr]bool{}
			eachArg(c.args, func(a *compArg) {
				if a.templAttrs && a.attr != nil {
					bound[a.attr] = true
				}
			})
			for _, a := range c.el.attrs {
				if a.kind != 'e' || a.stmts || bound[a] {
					continue
				}
				x := e.attrExpr(f, a)
				if x == nil {
					continue
				}
				ast.Inspect(x, func(n ast.Node) bool {
					lit, ok := n.(*ast.CompositeLit)
					if !ok || !isNamed(e.info.TypeOf(lit), templPath, "Attributes") {
						return true
					}
					for _, el := range lit.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						k, ok := kv.Key.(*ast.BasicLit)
						if !ok || k.Kind != token.STRING {
							continue
						}
						name, _ := strconv.Unquote(k.Value)
						if !isEvent(name) {
							continue
						}
						vt := e.info.TypeOf(kv.Value)
						if vt == nil {
							continue
						}
						if _, fn := vt.Underlying().(*types.Signature); fn {
							w := f.attrWarns.add("W0112", f.at(f.orig(kv.Pos())), "%s in templ.Attributes binds no event: templ renders no function, so <%s> drops it", name, c.el.tag)
							if c.target.has("TemplAttrs") {
								w.help("set it on the tag, %s={…}, on a component whose props have an Attributes templ.Attributes field", name)
							} else {
								w.help("update github.com/vuka-lang/ui, whose TemplAttrs binds it, or wrap the component in an element with %s={…}", name)
							}
						}
					}
					return true
				})
			}
		}
	}
}

// htmlAttr reports whether an attribute that isn't one of the props' fields
// is an HTML attribute for their Attributes: an event, a name with a dash or
// colon (aria-label, data-x, hx-get), a global attribute, or any name not a
// near miss of a field's, which stays the error it likely is.
func htmlAttr(name string, fields []string) bool {
	if strings.HasPrefix(name, "bind:") || strings.HasPrefix(name, "class:") || strings.HasPrefix(name, "style:") {
		return false
	}
	if isEvent(name) || strings.ContainsAny(name, "-:.") || globalAttrs[strings.ToLower(name)] {
		return true
	}
	return closestName(name, fields) == ""
}

var globalAttrs = map[string]bool{
	"role": true, "title": true, "tabindex": true, "hidden": true, "lang": true, "dir": true, "style": true,
	"draggable": true, "autofocus": true, "accesskey": true, "contenteditable": true, "spellcheck": true,
	"translate": true, "inert": true, "popover": true, "popovertarget": true, "popovertargetaction": true,
	"slot": true, "part": true, "is": true, "nonce": true, "enterkeyhint": true, "inputmode": true,
	"name": true, "value": true, "type": true, "placeholder": true, "autocomplete": true, "rel": true,
	"download": true, "for": true, "form": true, "alt": true, "src": true, "width": true, "height": true,
	"min": true, "max": true, "step": true, "pattern": true, "required": true, "readonly": true,
	"checked": true, "selected": true, "multiple": true, "disabled": true, "href": true, "target": true,
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
			if ev.attr.stmts {
				continue // writes.go checks them
			}
			if ev.checked {
				continue
			}
			x := e.attrExpr(f, ev.attr)
			tv, ok := e.info.Types[x]
			if x == nil || !ok || isInvalid(tv.Type) {
				continue
			}
			ev.checked = true
			if _, call := ast.Unparen(x).(*ast.CallExpr); call && isHandlerCall(tv) {
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
	if types.Identical(t, errorType) {
		return "takes a function; this is an error value (a call returning only an error is the handler itself: onClick={Save(ctx)})"
	}
	if types.NewMethodSet(t).Len() > 0 || types.NewMethodSet(types.NewPointer(t)).Len() > 0 {
		return ""
	}
	return "takes a function; this is " + e.display(t)
}

// keyField is a props struct's Key field, which a struct component's key
// attribute sets.
func keyField(fields []propField) *propField {
	for i := range fields {
		if fields[i].name == "Key" && fields[i].ambiguous == "" {
			return &fields[i]
		}
	}
	return nil
}
