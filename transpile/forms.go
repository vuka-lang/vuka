package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
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
		return
	}
	if !comp && el.tag == "label" && (a.name == "for" || a.name == "htmlFor") && a.kind == 'e' && strings.Contains(text, ".") && chainRe.MatchString(text) && p.f.target.has("ModelID") {
		a.ref = true
	}
}

// addressable reports whether a field reference's expression is a variable
// (its type-check says so): only then is it lowered to a reference, a
// constant or a method value staying what it was.
func (w *jsxWriter) addressable(a *jsxAttr) bool {
	e := w.f.eng
	if e == nil || e.info == nil {
		return false
	}
	x := e.chainAt(w.f, a)
	if x == nil {
		return false
	}
	tv, ok := e.info.Types[x]
	return ok && tv.Addressable()
}

// refArgs writes the pointers a field reference lowers to: the field, then
// each value holding it, innermost first, then a template's receiver.
func (w *jsxWriter) refArgs(a *jsxAttr) {
	text := string(w.f.src[a.expr.start:a.expr.end])
	lead := len(text) - len(strings.TrimLeft(text, " \t\n"))
	start := a.expr.start + lead
	chain := strings.TrimSpace(text)
	w.gen("&")
	w.copy(a.expr)
	var dots []int // the dots outside brackets: rows[i].Form's one
	depth := 0
	for i := 0; i < len(chain); i++ {
		switch chain[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '.':
			if depth == 0 {
				dots = append(dots, i)
			}
		}
	}
	for i := len(dots) - 1; i >= 0; i-- {
		w.gen(", &")
		w.copy(span{start, start + dots[i]})
	}
	if h := w.f.html; h != nil && h.recv != "" {
		first := chain
		if len(dots) > 0 {
			first = chain[:dots[0]]
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

func attrName(a *jsxAttr) string {
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

// formValueType is T of a FormFor[T] (or a pointer to one); nil for any
// other type.
func formValueType(t types.Type) types.Type {
	n := namedOf(t)
	if n == nil || n.TypeArgs().Len() != 1 || n.Obj().Pkg() == nil {
		return nil
	}
	if _, ok := pkgMethod(types.NewPointer(n), n.Obj().Pkg(), "vukaFormFor"); !ok {
		return nil
	}
	return n.TypeArgs().At(0)
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
	if obj, _, _ := types.LookupFieldOrMethod(tv.Type, true, e.pkg, sel.Sel.Name); obj != nil {
		return false
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
		if !ok || !f.lowerable(sel.Pos()) || e.info.Selections[sel] != nil {
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
		if !c.resolved || c.dead || n == nil || n.Obj().Name() != "Form" || n.Obj().Pkg() == nil || c.target == nil || n.Obj().Pkg().Path() != c.target.path {
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
						text := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
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
		"Required": "validate:required", "Email": "validate:email", "Min": "validate:min", "Max": "validate:max", "OneOf": "validate:oneof"}
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
