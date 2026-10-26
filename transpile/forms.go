package transpile

import (
	"go/ast"
	"go/types"
	"regexp"
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
		switch {
		case comp:
			p.fail(a.off, "model is a control's: <input model={Form.Email}/>; a form component takes for={Form.Email}")
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
	w.keep(a.expr)
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
		w.keep(span{start, start + dots[i]})
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
			if _, known := e.info.Selections[sel]; known {
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
					if a.model || a.ref || n.comp != nil && a.name == "for" && a.kind == 'e' {
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
	if a.model {
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
