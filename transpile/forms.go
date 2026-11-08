package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Forms: model={Form.Email} on a control, and for={Form.Email} on a label
// and on a form component (<ui.Field for>), name a field of a struct form.
// Both lower to calls of the target with pointers to the field and to the
// values holding it, innermost first — the target finds the form among
// them, a struct embedding its FormState:
//
//	<input model={Form.Email} class="input"/>
//	→ <input {ui.Model(ui.ModelSpec{Tag: "input", Own: "class"}, &p.Form.Email, &p.Form, p)...} class="input"/>
//	<label for={Form.Email}>     → <label for={ui.ModelID(&p.Form.Email, &p.Form, p)}>
//	<ui.Field for={Form.Email}/> → ui.Field{For: ui.For(&p.Form.Email, &p.Form, p)}
//
// A <textarea model> with no content shows the field's value
// (ui.ModelText), a <select model> with no options the field's choices
// (ui.ModelOptions), and each <option> of one is selected when the field
// is its value (ui.ModelSelected).

// chainRe is a field reference: names joined by dots, Form.Email.
var chainRe = regexp.MustCompile(`^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*$`)

var modelMods = map[string]bool{"lazy": true, "raw": true, "number": true}

// formAttr checks and marks an attribute of the forms' syntax: model and
// its modifiers (model.lazy, model.raw, model.number), and a label's for
// naming a field.
func (p *jsxParser) formAttr(el *jsxElem, a *jsxAttr, comp bool) {
	text := ""
	if a.kind == 'e' {
		text = strings.TrimSpace(string(p.src[a.expr.start:a.expr.end]))
	}
	if a.name == "model" || strings.HasPrefix(a.name, "model.") {
		if comp {
			// a component whose props have a Model ui.FieldRef field takes
			// it (bindFields checks), and spreads it on its control
			switch {
			case a.name != "model":
				p.fail(a.off, "%s on a component: a component takes model={Form.Email} and its control the modifiers (<input model.lazy={Model}/>)", a.name)
			case a.kind != 'e' || !chainRe.MatchString(text):
				p.fail(a.off, "model takes a field of a form: model={Form.Email}")
			}
			return
		}
		switch {
		case el.tag != "input" && el.tag != "textarea" && el.tag != "select":
			p.fail(a.off, "model goes on <input>, <textarea> or <select>, not <%s>", el.tag)
		case a.kind != 'e' || !chainRe.MatchString(text):
			p.fail(a.off, "model takes a field of a form: model={Form.Email}")
		case !p.f.target.has("Model"):
			p.fail(a.off, "model needs a target with forms: github.com/vuka-lang/ui/v2 v2.6 or later")
		}
		var mods []string
		for _, m := range strings.Split(a.name, ".")[1:] {
			if !modelMods[m] {
				p.fail(a.off, "model.%s isn't a modifier: model.lazy, model.raw and model.number are", m)
			}
			mods = append(mods, m)
		}
		a.model, a.mods = true, strings.Join(mods, ",")
		a.scope = p.scopeOf(text)
		return
	}
	if comp {
		if a.name == "for" && a.kind == 'e' && chainRe.MatchString(text) {
			// <ui.Field for={Street}/> in a <fieldset for={Form.Address}>: a
			// reference, whatever the component makes of it
			if a.scope = p.scopeOf(text); a.scope != nil {
				a.ref = true
			}
		}
		return
	}
	isFor := a.name == "for" || a.name == "htmlFor"
	switch {
	case el.tag == "label" && isFor && a.kind == 'e' && chainRe.MatchString(text) && p.f.target.has("ModelID") && (strings.Contains(text, ".") || len(p.scopes) > 0):
		a.ref, a.scope = true, p.scopeOf(text)
	case el.tag == "fieldset" && isFor:
		switch {
		case a.kind != 'e' || !chainRe.MatchString(text):
			p.failCode("E0186", a.off, "for on a <fieldset> takes a field of a form: <fieldset for={Form.Address}>")
		case !p.f.target.has("FieldsetAttrs"):
			p.failCode("E0186", a.off, "<fieldset for={…}> needs a target with plain form tags: github.com/vuka-lang/ui/v2 v2.18 or later")
		}
		a.ref, a.scope = true, p.scopeOf(text)
	case a.name == "form" && a.kind == 'e' && formControls[el.tag] && chainRe.MatchString(text) && p.f.target.has("ModelID"):
		a.ref, a.of, a.scope = true, true, nil
	}
}

// formControls are the elements a form={…} attribute associates with a
// form outside them.
var formControls = map[string]bool{"button": true, "input": true, "select": true, "textarea": true, "fieldset": true, "output": true, "object": true}

// scopeOf is the <fieldset for> a field reference's text is relative to:
// the innermost one around it, unless the text starts with that
// fieldset's first name (Form.Address.Street, written in full).
func (p *jsxParser) scopeOf(text string) *jsxAttr {
	if len(p.scopes) == 0 {
		return nil
	}
	s := p.scopes[len(p.scopes)-1]
	first, _, _ := strings.Cut(text, ".")
	if root, _, _ := strings.Cut(p.f.chainText(s), "."); first == root {
		return nil
	}
	return s
}

// chainText is a field reference's text in full: its fieldset's first,
// then its own (Form.Address.Street for model={Street}).
func (f *fileState) chainText(a *jsxAttr) string {
	text := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
	if a.scope != nil {
		return f.chainText(a.scope) + "." + text
	}
	return text
}

// fieldsetScope opens the scope of a <fieldset for>: the field references
// inside it are relative to its field. It reports whether it opened one.
func (p *jsxParser) fieldsetScope(el *jsxElem) bool {
	if el.tag != "fieldset" || el.comp != nil {
		return false
	}
	for _, a := range el.attrs {
		if a.ref && !a.of {
			p.scopes = append(p.scopes, a)
			return true
		}
	}
	return false
}

// legend notes an empty <legend> in a <fieldset for>, which shows the
// field's label as an empty <label for/> does.
func (p *jsxParser) legend(el *jsxElem) {
	if el.tag == "legend" && el.comp == nil && len(p.scopes) > 0 && !hasContent(el.kids) {
		el.legendOf = p.scopes[len(p.scopes)-1]
	}
}

// addressable reports whether a field reference's expression is a variable
// (its type-check says so): only then is it lowered to a reference, a
// constant or a method value staying what it was.
func (w *jsxWriter) addressable(a *jsxAttr) bool {
	return w.f.eng != nil && w.f.eng.addressable(w.f, a)
}

func (e *engine) addressable(f *fileState, a *jsxAttr) bool {
	if e.info == nil {
		return false
	}
	x := e.chainAt(f, a)
	if x == nil {
		return false
	}
	tv, ok := e.info.Types[x]
	return ok && tv.Addressable()
}

// isFieldRef reports whether el's attribute a is lowered to a reference to
// its field, which takes the field as declared: an Option field stays one.
// A component's model is one before the component is resolved.
func (e *engine) isFieldRef(f *fileState, el *jsxElem, a *jsxAttr) bool {
	return a.model || el.comp != nil && a.name == "model" && a.kind == 'e' || a.ref && e.addressable(f, a)
}

// refArgs writes the pointers a field reference lowers to: the field, then
// each value holding it, innermost first, then a template's receiver.
func (w *jsxWriter) refArgs(a *jsxAttr) {
	// the reference's spans, outermost first: a <fieldset for>'s, then
	// its own (model={Street} in <fieldset for={Form.Address}>)
	var spans []span
	for x := a; x != nil; x = x.scope {
		text := string(w.f.src[x.expr.start:x.expr.end])
		lead := len(text) - len(strings.TrimLeft(text, " \t\n"))
		start := x.expr.start + lead
		spans = append([]span{{start, start + len(strings.TrimSpace(text))}}, spans...)
	}
	// the value at a dot: the spans before it, and the one it is in up to it
	upTo := func(si, at int) {
		for i := 0; i < si; i++ {
			w.copy(spans[i])
			w.gen(".")
		}
		w.copy(span{spans[si].start, at})
	}
	w.gen("&")
	upTo(len(spans)-1, spans[len(spans)-1].end)
	type dot struct{ si, at int }
	var dots []dot // the dots outside brackets: rows[i].Form's one
	for si, sp := range spans {
		if si > 0 {
			dots = append(dots, dot{si - 1, spans[si-1].end}) // the dot joining two spans
		}
		depth := 0
		for i := sp.start; i < sp.end; i++ {
			switch w.f.src[i] {
			case '[', '(':
				depth++
			case ']', ')':
				depth--
			case '.':
				if depth == 0 {
					dots = append(dots, dot{si, i})
				}
			}
		}
	}
	for i := len(dots) - 1; i >= 0; i-- {
		w.gen(", &")
		upTo(dots[i].si, dots[i].at)
	}
	if h := w.f.html; h != nil && h.recv != "" {
		first := string(w.f.src[spans[0].start:spans[0].end])
		if len(dots) > 0 {
			first = string(w.f.src[spans[0].start:dots[0].at])
		}
		if first != h.recv {
			w.gen(", " + h.recv)
		}
	}
}

// modelCall writes a call of the target's fn with lead (and a comma) first,
// then the reference's pointers.
func (w *jsxWriter) modelCall(fn, lead string, a *jsxAttr) {
	w.gen(w.q + fn + "(")
	if lead != "" {
		w.gen(lead + ", ")
	}
	w.refArgs(a)
	w.gen(")")
}

// modelSpec is the ui.ModelSpec of a control carrying model: its tag, its
// own type and value, the model's modifiers and the attributes it sets
// itself.
func (w *jsxWriter) modelSpec(el *jsxElem, model *jsxAttr) {
	w.gen(w.q + "ModelSpec{Tag: " + strconv.Quote(el.tag))
	var own []string
	for _, a := range el.attrs {
		if a == model || a.kind == 'r' || a.name == "is:inline" {
			continue
		}
		name := htmlName(a.name)
		own = append(own, name)
		switch name {
		case "type":
			if a.kind == 's' {
				w.gen(", Type: " + strconv.Quote(a.val))
			}
		case "value":
			w.gen(", HasValue: true, Value: ")
			w.attrValue(a)
		}
	}
	if model.mods != "" {
		w.gen(", Mods: " + strconv.Quote(model.mods))
	}
	if len(own) > 0 {
		w.gen(", Own: " + strconv.Quote(strings.Join(own, " ")))
	}
	w.gen("}")
}

// modelOf is an element's model attribute.
func modelOf(el *jsxElem) *jsxAttr {
	for _, a := range el.attrs {
		if a.model {
			return a
		}
	}
	return nil
}

func attrNamed(el *jsxElem, name string) *jsxAttr {
	for _, a := range el.attrs {
		if htmlName(a.name) == name {
			return a
		}
	}
	return nil
}

// hasElems reports whether nodes hold an element (a select's options).
func hasElems(ns []jsxNode) bool {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxElem:
			return true
		case *jsxBlock:
			return true
		case *jsxHole:
			return true
		case *jsxText:
			if strings.TrimSpace(n.val) != "" {
				return true
			}
		}
	}
	return false
}

// checkModels reports a field reference naming a field its value doesn't
// have, with the field it most likely meant: model={Form.Emial}.
func (e *engine) checkModels(f *fileState, t *jsxTree) {
	if e.info == nil {
		return
	}
	var walk func(ns []jsxNode)
	check := func(el *jsxElem, a *jsxAttr) {
		x := e.chainAt(f, a)
		for x != nil {
			sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
			if !ok {
				return
			}
			if _, known := e.info.Selections[sel]; known || e.formValueField(sel) {
				return
			}
			if _, isPkg := e.info.Uses[identOf(sel.X)].(*types.PkgName); isPkg {
				return
			}
			tv, ok := e.info.Types[sel.X]
			if !ok || isInvalid(tv.Type) {
				x = sel.X
				continue
			}
			names := fieldNames(tv.Type)
			for _, n := range names {
				if len(sel.Sel.Name) < len(n) && strings.EqualFold(n[:len(sel.Sel.Name)], sel.Sel.Name) {
					return // a name being typed: Go's own error says it at a build, and an editor completes it
				}
			}
			what := attrName(a)
			msg := "%s={%s}: %s has no field %s"
			args := []any{what, strings.TrimSpace(string(f.src[a.expr.start:a.expr.end])), e.display(derefPtr(tv.Type)), sel.Sel.Name}
			text := string(f.src[a.expr.start:a.expr.end])
			off := strings.LastIndex(text, "."+sel.Sel.Name) + 1
			at := f.at(a.expr.start + off)
			if s := closestName(sel.Sel.Name, names); s != "" {
				e.errs.add("E0159", at, msg+"; did you mean %s?", append(args, s)...).help("write %s={…%s}", what, s)
			} else {
				e.errs.add("E0159", at, msg, args...).help("its fields are %s", strings.Join(names, ", "))
			}
			return
		}
	}
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				for _, a := range n.attrs {
					if a.model || a.ref || n.comp != nil && (a.name == "for" || a.name == "model") && a.kind == 'e' {
						check(n, a)
					}
					if a.of {
						e.checkFormAttr(f, n, a)
					}
				}
				walk(n.kids)
			case *jsxBlock:
				for _, b := range n.bodies {
					walk(b)
				}
				for _, c := range n.cases {
					walk(c)
				}
			}
		}
	}
	walk([]jsxNode{t.root})
}

// checkFormAttr reports a form={…} on a control naming a value that is
// neither a form nor an id (E0188): form={Form} names the form's element,
// form={id} or form="id" an element's id.
func (e *engine) checkFormAttr(f *fileState, el *jsxElem, a *jsxAttr) {
	x := e.chainAt(f, a)
	tv, ok := e.info.Types[x]
	if x == nil || !ok || isInvalid(tv.Type) || isFormType(tv.Type) {
		return
	}
	if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 {
		return
	}
	text := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
	e.errs.add("E0188", f.at(a.off), "form={%s} on a <%s>: %s is of type %s, not a form", text, el.tag, text, e.display(tv.Type)).
		help("name the form, form={Form} (a struct embedding ui.FormState, or a ui.FormFor), or the id of a form element, form=\"product-form\"")
}

func attrName(a *jsxAttr) string {
	if a.of {
		return "form"
	}
	if a.model || a.name == "model" {
		return "model"
	}
	return "for"
}

func identOf(x ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(x).(*ast.Ident)
	return id
}

func derefPtr(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// fieldNames are the exported fields of a struct, or of the struct a
// pointer points to.
func fieldNames(t types.Type) []string {
	st, ok := derefPtr(t).Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < st.NumFields(); i++ {
		if fl := st.Field(i); fl.Exported() && !fl.Embedded() {
			out = append(out, fl.Name())
		}
	}
	return out
}

func isAny(t types.Type) bool {
	i, ok := t.Underlying().(*types.Interface)
	return ok && i.Empty()
}

// chainAt is a field reference's expression in this round's syntax tree:
// the longest name or selector ending where the attribute's does. (Its
// lowering writes the references' prefixes from the same source, so its
// start is ambiguous; its end isn't.)
func (e *engine) chainAt(f *fileState, a *jsxAttr) ast.Expr {
	end := f.cur.fromOrigEnd(a.expr.end)
	var best ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr, *ast.Ident:
			if f.off(x.End()) == end && (best == nil || x.Pos() < best.Pos()) {
				best = x.(ast.Expr)
			}
		}
		return true
	})
	return best
}

// checkFormOf reports a ui.FormOf[T]("Name", …) naming a field T doesn't
// have, with the one it most likely meant: the names are the form's
// allow-list, written as strings, so Go's own type-check can't see them.
func (e *engine) checkFormOf() {
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			idx, ok := ast.Unparen(call.Fun).(*ast.IndexExpr)
			if !ok {
				return true
			}
			sel, ok := ast.Unparen(idx.X).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "FormOf" {
				return true
			}
			fn, ok := e.info.Uses[sel.Sel].(*types.Func)
			tv, typed := e.info.Types[idx.Index]
			if !ok || !typed || fn.Pkg() == nil || fn.Pkg().Path() != DefaultJSXTarget && (f.target == nil || fn.Pkg().Path() != f.target.path) {
				return true
			}
			for _, a := range call.Args {
				lit, ok := ast.Unparen(a).(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if missing, at, owner, names := missingField(tv.Type, name); missing != "" {
					pos := f.pos(f.off(lit.Pos()) + 1 + at)
					model, in := e.display(derefPtr(tv.Type)), e.display(derefPtr(owner))
					if s := closestName(missing, names); s != "" {
						e.errs.add("E0161", pos, "ui.FormOf[%s]: %s has no field %s; did you mean %s?", model, in, missing, s).help("name the field as it is declared: %q", name[:at]+s)
					} else {
						e.errs.add("E0161", pos, "ui.FormOf[%s]: %s has no field %s", model, in, missing).help("its fields are %s", strings.Join(names, ", "))
					}
				}
			}
			return true
		})
	}
}

// missingField is the first name of a Go path ("Address.City") that its
// struct has no exported field of, its offset in the path, the struct, and
// its field names; "" when every one is there.
func missingField(t types.Type, path string) (string, int, types.Type, []string) {
	at := 0
	for _, name := range strings.Split(path, ".") {
		names := fieldNames(t)
		if len(names) == 0 {
			return "", 0, nil, nil // not a struct the compiler sees into
		}
		obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)
		v, ok := obj.(*types.Var)
		if !ok || !v.IsField() || !v.Exported() {
			return name, at, t, names
		}
		t = v.Type()
		at += len(name) + 1
	}
	return "", 0, nil, nil
}

// A FormFor[T] of the ui target (its type has the marker method
// vukaFormFor) holds its values in its Value field; Form.Name, Name a field
// of T and not one of the form's own, is Form.Value.Name:
//
//	<ui.Field for={Form.Name}/>  →  ui.Field{For: ui.For(&p.Form.Value.Name, &p.Form, p)}
//	p.Form.Price = 0             →  p.Form.Value.Price = 0
//
// A struct embedding a FormFor[T] (by value) is a form type, a form of T
// as the FormFor is, its fields reached through it the same way. Its own
// Init, Clean, Clean<Field> and <Field>Choices are the form's (formMethods),
// which Load and Save run: a call of either on a form type with Init or a
// Clean method is given ui.Attach, which makes the form type the form its
// FormFor's methods are of before a render does.
//
//	p.Form.Load(ctx, row)  →  ui.Attach(&p.Form).Load(ctx, row)

// formValueType is T of a FormFor[T] or of a form type (or a pointer to
// either); nil for any other type.
func formValueType(t types.Type) types.Type {
	vt, _ := formForOf(t)
	return vt
}

// formForOf is T of a FormFor[T] or of a form type, and the FormFor's
// type; nil for any other type.
func formForOf(t types.Type) (types.Type, *types.Named) {
	if n := uiFormFor(t); n != nil {
		return n.TypeArgs().At(0), n
	}
	if namedOf(t) == nil {
		return nil, nil
	}
	st, ok := derefPtr(t).Underlying().(*types.Struct)
	if !ok {
		return nil, nil
	}
	for i := 0; i < st.NumFields(); i++ {
		fl := st.Field(i)
		if _, ptr := types.Unalias(fl.Type()).(*types.Pointer); !fl.Embedded() || ptr {
			continue
		}
		if n := uiFormFor(fl.Type()); n != nil {
			return n.TypeArgs().At(0), n
		}
	}
	return nil, nil
}

// uiFormFor is t, or what t points to, when it is a FormFor of the ui
// target.
func uiFormFor(t types.Type) *types.Named {
	n := namedOf(t)
	if n == nil || n.TypeArgs().Len() != 1 || n.Obj().Pkg() == nil {
		return nil
	}
	if _, ok := pkgMethod(types.NewPointer(n), n.Obj().Pkg(), "vukaFormFor"); !ok {
		return nil
	}
	return n
}

// formType is the form type t is (or points to), and its FormFor; nil for
// a FormFor itself or any type that isn't a form.
func formType(t types.Type) (*types.Named, *types.Named) {
	if uiFormFor(t) != nil {
		return nil, nil
	}
	_, ff := formForOf(t)
	if ff == nil {
		return nil, nil
	}
	return namedOf(t), ff
}

// ownMethod reports whether m is declared by n, not promoted from what it
// embeds.
func ownMethod(n *types.Named, m *types.Func) bool {
	sig, ok := m.Type().(*types.Signature)
	return ok && sig.Recv() != nil && namedOf(sig.Recv().Type()) != nil && namedOf(sig.Recv().Type()).Obj() == n.Obj()
}

// formMethods are a form type's own methods of a form's: Init, Clean,
// Clean<Field> and <Field>Choices.
func formMethods(n *types.Named) []*types.Func {
	var out []*types.Func
	ms := types.NewMethodSet(types.NewPointer(n))
	for i := 0; i < ms.Len(); i++ {
		m, ok := ms.At(i).Obj().(*types.Func)
		if !ok || !ownMethod(n, m) {
			continue
		}
		if name := m.Name(); name == "Init" || strings.HasPrefix(name, "Clean") || strings.HasSuffix(name, "Choices") {
			out = append(out, m)
		}
	}
	return out
}

// runsHooks reports whether a form type has methods Load or Save run:
// Init, or a Clean method.
func runsHooks(n *types.Named) bool {
	for _, m := range formMethods(n) {
		if !strings.HasSuffix(m.Name(), "Choices") {
			return true
		}
	}
	return false
}

// attachForms gives each Load and Save of a form type with hooks
// ui.Attach, so a Load before its page attached the form runs its Init.
func (e *engine) attachForms(f *fileState) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Load" && sel.Sel.Name != "Save" || !f.lowerable(sel.Pos()) {
			return true
		}
		s := e.info.Selections[sel]
		if s == nil || s.Kind() != types.MethodVal || len(s.Index()) < 2 {
			return true // not promoted: the form type's own method, or a FormFor's
		}
		ft, ff := formType(s.Recv())
		if ft == nil || s.Obj().Pkg() != ff.Obj().Pkg() || !runsHooks(ft) {
			return true
		}
		attach := ff.Obj().Pkg().Scope().Lookup("Attach")
		if attach == nil {
			return true // a ui before form types' hooks
		}
		tv, ok := e.info.Types[sel.X]
		_, ptr := types.Unalias(tv.Type).(*types.Pointer)
		if !ok || !ptr && !tv.Addressable() {
			return true
		}
		start, end := f.orig(sel.X.Pos()), f.orig(sel.X.End())
		if f.attached[start] {
			return true
		}
		if f.attached == nil {
			f.attached = map[int]bool{}
		}
		f.attached[start] = true
		missing := ""
		q := e.qualifier(f, &missing)(attach.Pkg())
		if missing != "" {
			q = f.importAs(attach.Pkg().Path(), "__ui")
		}
		lead := q + ".Attach("
		if !ptr {
			lead += "&"
		}
		f.insert(start, lead, 0)
		f.insert(end, ")", 0)
		e.progress = true
		return true
	})
}

// checkFormTypes reports, of each form type a .vuka file declares, an
// Init or Clean method of another shape than a form's (E0184), and a field
// or form method of its own that T has too (E0185): Form.Name names T's
// field through the form, and a form's method has one place.
func (e *engine) checkFormTypes() {
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || !f.lowerable(spec.Pos()) {
				return true
			}
			obj, ok := e.info.Defs[spec.Name].(*types.TypeName)
			if !ok {
				return false
			}
			ft, ff := formType(obj.Type())
			if ft == nil {
				return false
			}
			vt := ff.TypeArgs().At(0)
			name, tName := ft.Obj().Name(), e.display(vt)
			at := func(p token.Pos) (*fileState, bool) {
				pf := e.fileOf(p)
				return pf, pf != nil && pf.vuka
			}
			if st, ok := ft.Underlying().(*types.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					fl := st.Field(i)
					if fl.Embedded() || isUIMeta(fl.Type()) {
						continue // a ui.Meta member describes T's field of its name
					}
					other, _, _ := types.LookupFieldOrMethod(vt, true, fl.Pkg(), fl.Name())
					if v, ok := other.(*types.Var); ok && v.IsField() {
						if pf, ok := at(fl.Pos()); ok {
							e.errs.add("E0185", pf.nodePos(fl.Pos()), "%s.%s and %s.%s share a name: a form type's own fields aren't the form's, and Form.%s names %s.%[4]s", name, fl.Name(), tName, fl.Name(), fl.Name(), tName).
								help("rename %s.%s", name, fl.Name())
						}
					}
				}
			}
			for _, m := range formMethods(ft) {
				pf, ok := at(m.Pos())
				if !ok {
					continue
				}
				if m.Name() != "Init" {
					other, _, _ := types.LookupFieldOrMethod(types.NewPointer(vt), true, m.Pkg(), m.Name())
					if _, ok := other.(*types.Func); ok {
						e.errs.add("E0185", pf.nodePos(m.Pos()), "%s.%s and %s.%s: the form type and its %s both declare the form's method %s", name, m.Name(), tName, m.Name(), tName, m.Name()).
							help("keep one: the form type's, or %s's", tName)
						continue
					}
				}
				sig := m.Type().(*types.Signature)
				errRes := sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type())
				switch {
				case m.Name() == "Init" && (!errRes || sig.Params().Len() == 0 || !isContext(sig.Params().At(0).Type())):
					e.errs.add("E0184", pf.nodePos(m.Pos()), "%s.Init is the form type's Init: it runs when the form is created and on every Load, and takes a ctx (then any dependencies) and returns an error", name).
						help("declare it func (f *%s) Init(ctx context.Context) error", name)
				case strings.HasPrefix(m.Name(), "Clean") && !errRes:
					e.errs.add("E0184", pf.nodePos(m.Pos()), "%s.%s is a form's check: it returns an error, nil when the form passes", name, m.Name()).
						help("declare it func (f *%s) %s(ctx context.Context) error", name, m.Name())
				}
			}
			return false
		})
	}
}

// formValueField reports whether sel is a FormFor's X naming a field of T
// that the form itself has no member of.
func (e *engine) formValueField(sel *ast.SelectorExpr) bool {
	tv, ok := e.info.Types[sel.X]
	if !ok {
		return false
	}
	vt := formValueType(tv.Type)
	if vt == nil {
		return false
	}
	if obj, _, _ := types.LookupFieldOrMethod(tv.Type, true, e.pkg, sel.Sel.Name); obj != nil && !isUIMeta(obj.Type()) {
		return false // the form's own member; a ui.Meta one stands for T's field
	}
	obj, _, _ := types.LookupFieldOrMethod(vt, true, e.pkg, sel.Sel.Name)
	v, ok := obj.(*types.Var)
	return ok && v.IsField()
}

// resolveFormFields writes each Form.X naming a field of a FormFor's T as
// Form.Value.X.
func (e *engine) resolveFormFields(f *fileState) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.lowerable(sel.Pos()) || e.info.Selections[sel] != nil && !isUIMeta(e.info.Selections[sel].Type()) {
			return true
		}
		at := f.orig(sel.Sel.Pos())
		if f.done[at] || !e.formValueField(sel) {
			return true
		}
		from := f.orig(sel.X.End())
		dot := strings.IndexByte(string(f.src[from:at]), '.')
		if dot < 0 {
			return true
		}
		f.done[at] = true
		f.add(from+dot, from+dot+1, ".Value.")
		e.progress = true
		return true
	})
}

// boundFields gives each <ui.Form for={Form}> of a FormFor, with markup
// inside, the fields that markup binds — the for={…} and model={…} naming
// Form's fields — as its Bound: the only fields a submit sets.
//
//	<ui.Form for={Form}><ui.Field for={Form.Name}/></ui.Form>
//	→ ui.Form{For: ui.For(&p.Form), Bound: []string{"Name"}, Children: …}
func (e *engine) boundFields(f *fileState, t *jsxTree) {
	for _, c := range t.comps {
		n := namedOf(c.propsTyp)
		if !c.resolved || c.dead || n == nil || !isFormComp(n.Obj().Name()) || n.Obj().Pkg() == nil || c.target == nil || n.Obj().Pkg().Path() != c.target.path {
			continue
		}
		if obj, _, _ := types.LookupFieldOrMethod(n, false, n.Obj().Pkg(), "Bound"); obj == nil || attrNamed(c.el, "bound") != nil || !hasElems(c.el.kids) {
			continue
		}
		forAttr := attrNamed(c.el, "for")
		if forAttr == nil || forAttr.kind != 'e' {
			continue
		}
		x := e.chainAt(f, forAttr)
		if x == nil {
			continue
		}
		if tv, ok := e.info.Types[x]; !ok || formValueType(tv.Type) == nil {
			continue
		}
		prefix := strings.TrimSpace(string(f.src[forAttr.expr.start:forAttr.expr.end])) + "."
		var names []string
		var walk func(ns []jsxNode)
		walk = func(ns []jsxNode) {
			for _, nd := range ns {
				switch nd := nd.(type) {
				case *jsxElem:
					for _, a := range nd.attrs {
						if a.kind != 'e' || !(a.model || a.ref || nd.comp != nil && (a.name == "for" || a.name == "model")) {
							continue
						}
						text := f.chainText(a)
						if rest, ok := strings.CutPrefix(text, prefix); ok && chainRe.MatchString(rest) {
							rest = strings.TrimPrefix(rest, "Value.")
							if !slices.Contains(names, rest) {
								names = append(names, rest)
							}
						}
					}
					walk(nd.kids)
				case *jsxBlock:
					for _, b := range nd.bodies {
						walk(b)
					}
					for _, cs := range nd.cases {
						walk(cs)
					}
				}
			}
		}
		walk(c.el.kids)
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = strconv.Quote(name)
		}
		c.args = append(c.args, &compArg{key: "Bound: ", text: "[]string{" + strings.Join(quoted, ", ") + "}"})
	}
}

// isFormComp reports whether a target's type is a form component:
// <ui.Form>, or the FormElement a <form for> is.
func isFormComp(name string) bool { return name == "Form" || name == "FormElement" }

// checkFormSaves reports a FormFor's Save whose T has no SaveFields method
// to save the form's fields with: a plain struct's form hands its value to
// its submit handler instead (E0182).
func (e *engine) checkFormSaves() {
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Save" {
				return true
			}
			s := e.info.Selections[sel]
			if s == nil || s.Kind() != types.MethodVal {
				return true
			}
			vt := formValueType(s.Recv())
			if vt == nil {
				return true
			}
			if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(vt), true, nil, "SaveFields"); obj != nil {
				return true
			}
			t := e.display(vt)
			e.errs.add("E0182", f.nodePos(sel.Sel.Pos()), "ui.FormFor[%s].Save saves a model's row, and %s has no SaveFields method to save the form's fields with", t, t).
				help("make %s a model (embed orm.Base), or take the checked %s as a parameter of the submit handler and save it there", t, t)
			return true
		})
	}
}

// uiTagSettings and ormTagSettings are what a ui or orm field attribute sets that a form
// field's tag sets too: the tag's key (label, input) or validate rule
// (validate:max), by the attribute's name, and by a composite attribute's
// member.
var (
	uiTagSettings = map[string]string{"Label": "label", "Help": "help", "Placeholder": "placeholder", "Widget": "input", "Autocomplete": "autocomplete",
		"Required": "validate:required", "Email": "validate:email", "Min": "validate:min", "Max": "validate:max", "OneOf": "validate:oneof",
		// a ui.Spec's members
		".Label": "label", ".Help": "help", ".Placeholder": "placeholder", ".Widget": "input", ".Autocomplete": "autocomplete",
		".Required": "validate:required", ".Optional": "validate:required", ".Min": "validate:min", ".Max": "validate:max", ".Choices": "validate:oneof"}
	ormTagSettings = map[string]string{"LongText": "input", ".Label": "label", ".Help": "help", ".Max": "validate:max", ".Min": "validate:min", ".Choices": "validate:oneof"}
)

// tagSettings is the table for pkg: ui's (the package whose client state
// the compiler lowers), the orm's, or none.
func tagSettings(pkg *types.Package) map[string]string {
	if declaresState(pkg) {
		return uiTagSettings
	}
	if ormPackage(pkg) {
		return ormTagSettings
	}
	return nil
}

// checkTagAttrs reports a field whose tag and attribute both set the same
// thing of it as a form field — label:"…" and @ui.Label, validate:"max=50"
// and @orm.Char{Max: 100} — so each setting has one source (E0183).
func (e *engine) checkTagAttrs() {
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if !a.Field || len(a.fieldNames) == 0 {
				continue
			}
			pkgName, name, ok := strings.Cut(a.Name, ".")
			if !ok {
				continue
			}
			pkg := e.importedAs(f, pkgName)
			if pkg == nil {
				continue
			}
			table := tagSettings(pkg)
			tag := f.fieldTag(a.structOf, a.fieldNames[0])
			if table == nil || tag == "" {
				continue
			}
			var sets []string
			if s := table[name]; s != "" {
				sets = append(sets, s)
			}
			args := strings.TrimSpace(a.Args)
			if strings.HasPrefix(args, "{") {
				if x, err := parser.ParseExpr("T" + args); err == nil {
					if lit, ok := x.(*ast.CompositeLit); ok {
						for _, el := range lit.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok && table["."+id.Name] != "" {
									sets = append(sets, table["."+id.Name])
								}
								if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Msg" && name == "Spec" {
									for _, r := range slices.Sorted(maps.Keys(msgKeys(kv.Value, 0))) {
										sets = append(sets, "msg:"+r)
									}
								}
							}
						}
					}
				}
			}
			if name == "Msg" {
				if x, err := parser.ParseExpr("f" + args); err == nil {
					if c, ok := x.(*ast.CallExpr); ok && len(c.Args) > 0 {
						if lit, ok := c.Args[0].(*ast.BasicLit); ok {
							if r, err := strconv.Unquote(lit.Value); err == nil {
								sets = append(sets, "msg:"+r)
							}
						}
					}
				}
			}
			for _, s := range sets {
				if !tagSets(tag, s) {
					continue
				}
				what, _, _ := strings.Cut(s, ":")
				if rule, ok := strings.CutPrefix(s, what+":"); ok {
					what += ":\"" + rule + "\""
				}
				e.errs.add("E0183", a.Pos, "%s.%s's %s is set by its tag (%s) and by @%s: a setting has one source",
					a.structOf, a.fieldNames[0], strings.TrimPrefix(s, "validate:"), what, a.Name).
					help("drop it from the tag, or drop the attribute")
			}
		}
	}
}

// tagSets reports whether a struct tag sets s: a key (label), a validate
// rule (validate:max) or a rule's message (msg:max).
func tagSets(tag reflect.StructTag, s string) bool {
	key, rule, ok := strings.Cut(s, ":")
	v, has := tag.Lookup(key)
	if !ok || !has {
		return has
	}
	if key == "msg" {
		return regexp.MustCompile(`(^|,)\s*` + regexp.QuoteMeta(rule) + `\s*=`).MatchString(v)
	}
	for _, part := range strings.Split(v, ",") {
		if n, _, _ := strings.Cut(strings.TrimSpace(part), "="); n == rule {
			return true
		}
	}
	return false
}

// fieldTag is the tag of the struct type's field name, as written in f.
func (f *fileState) fieldTag(typeName, name string) reflect.StructTag {
	st := f.structType(typeName)
	if st == nil {
		return ""
	}
	for _, fl := range st.Fields.List {
		for _, n := range fl.Names {
			if n.Name == name && fl.Tag != nil {
				s, _ := strconv.Unquote(fl.Tag.Value)
				return reflect.StructTag(s)
			}
		}
	}
	return ""
}

// formAttrs are the attributes a <form for={Form}> consumes, as
// <ui.Form>'s options: every other attribute passes through to the
// element.
var formOptions = []string{"for", "key", "validate", "submit", "keep", "confirmLeave", "noSummary", "eager", "onSubmit", "onChange", "error", "submitLabel", "bound"}

// formTag makes a <form> with for={Form} the target's FormElement: the
// form <ui.Form> renders, bound, checked and submitted by ui, its options
// attributes and every other attribute the element's own. It reports
// whether el became a component.
func (p *jsxParser) formTag(el *jsxElem) bool {
	if el.tag == "input" && el.comp == nil {
		if t, m := attrNamed(el, "type"), modelOf(el); m != nil && t != nil && t.kind == 's' && strings.EqualFold(t.val, "hidden") {
			p.failCode("E0187", m.off, "model on an <input type=\"hidden\">: a form takes no value the visitor can't see or change (an id, an owner); take it from the route or the session, and leave the field out of the form with form:\"-\"")
		}
	}
	if el.tag != "form" || el.comp != nil {
		return false
	}
	forA := attrNamed(el, "for")
	if forA == nil {
		return false
	}
	text := ""
	if forA.kind == 'e' {
		text = strings.TrimSpace(string(p.src[forA.expr.start:forA.expr.end]))
	}
	switch {
	case forA.kind != 'e' || !chainRe.MatchString(text):
		p.failCode("E0186", forA.off, "for on a <form> takes the form it renders: <form for={Form}>")
	case !p.f.target.has("FormElement"):
		p.failCode("E0186", forA.off, "<form for={…}> needs a target with plain form tags: github.com/vuka-lang/ui/v2 v2.18 or later")
	}
	for _, a := range el.attrs {
		switch {
		case a.kind == 'r':
			p.failCode("E0186", a.off, "an attribute set on a <form for={…}>: pass them as attrs={…}, a []%sAttr", p.f.target.q)
		case a.name == "model" || strings.HasPrefix(a.name, "model."):
			p.fail(a.off, "model goes on <input>, <textarea> or <select>, not <form>")
		case a.name == "class:" || strings.HasPrefix(a.name, "class:"):
			p.failCode("E0186", a.off, "%s on a <form for={…}>: write the form's classes as class={…}", a.name)
		}
		if a.event {
			a.event = false
			for i, ev := range p.cur.events {
				if ev.attr == a {
					p.cur.events = append(p.cur.events[:i], p.cur.events[i+1:]...)
					break
				}
			}
		}
	}
	el.formFor = true
	el.comp = &jsxComp{el: el, target: p.f.target}
	p.cur.comps = append(p.cur.comps, el.comp)
	return true
}

// labelRef is the field an empty <label for={Form.Name}/> or an empty
// <legend/> in a <fieldset for> shows the label of; nil for any other
// element.
func (w *jsxWriter) labelRef(el *jsxElem, kids []jsxNode) *jsxAttr {
	if hasContent(kids) || !w.f.target.has("ModelLabel") {
		return nil
	}
	if el.legendOf != nil {
		return el.legendOf
	}
	if el.tag != "label" {
		return nil
	}
	for _, a := range el.attrs {
		if a.ref && !a.of && (w.addressable(a) || a.scope != nil) {
			return a
		}
	}
	return nil
}

// isFormRef reports whether a form={…} names a form (a struct embedding
// the target's FormState): it is the form's element id then, and the
// value's own text otherwise.
func (e *engine) isFormRef(f *fileState, a *jsxAttr) bool {
	if e.info == nil || !e.addressable(f, a) {
		return false
	}
	tv, ok := e.info.Types[e.chainAt(f, a)]
	return ok && isFormType(tv.Type)
}

// isFormType reports whether t (or what it points to) is a form: a
// struct with an embedded FormState, its own or promoted (FormFor[T], a
// form type embedding one).
func isFormType(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(derefPtr(t), true, nil, "FormState")
	v, ok := obj.(*types.Var)
	if !ok || !v.Embedded() {
		return false
	}
	n := namedOf(v.Type())
	return n != nil && n.Obj().Name() == "FormState"
}
