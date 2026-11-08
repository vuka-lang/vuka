package transpile

import (
	"go/types"
	"strings"
)

// Widgets: a component whose props have a Model ui.FieldRef takes
// model={Form.Price} (package github.com/vuka-lang/ui/v2/widget holds the
// built-in ones). The form fills the props it declares by name and type,
// so a Value or a Choices' Row of another type than the field's is the
// component's mistake, found here rather than as the page renders (E0195):
//
//	type Picker struct {
//		widget.Props
//		Value   orm.FK[Category]                 // the field's value
//		Choices []widget.Choice[Category]        // the rows of an orm.FK[Category]
//	}

// checkWidgets reports a widget given a model whose Value or Choices
// don't fit the field.
func (e *engine) checkWidgets(f *fileState, t *jsxTree) {
	if e.info == nil {
		return
	}
	for _, c := range t.comps {
		if !c.resolved || c.dead || c.propsTyp == nil {
			continue
		}
		a := attrNamed(c.el, "model")
		if a == nil || a.kind != 'e' {
			continue
		}
		x := e.chainAt(f, a)
		if x == nil {
			continue
		}
		tv, ok := e.info.Types[x]
		if !ok || isInvalid(tv.Type) {
			continue
		}
		ft := tv.Type
		props := c.propsTyp
		chain := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
		if v := widgetProp(props, "Value"); v != nil && attrNamed(c.el, "value") == nil && !fieldFits(ft, v.Type()) {
			e.errs.add("E0195", f.at(a.expr.start), "<%s model={%s}>: its Value is of type %s, and %s of type %s", c.el.tag, chain, e.display(v.Type()), chain, e.display(ft)).
				help("make the widget's Value a %s, or give model a field of type %s", e.display(ft), e.display(v.Type()))
		}
		ch := widgetProp(props, "Choices")
		if ch == nil || attrNamed(c.el, "choices") != nil {
			continue
		}
		sl, ok := ch.Type().Underlying().(*types.Slice)
		if !ok {
			continue
		}
		row := widgetProp(sl.Elem(), "Row")
		related := relatedRow(ft)
		if row == nil || related == nil {
			continue
		}
		if !types.AssignableTo(related, row.Type()) {
			e.errs.add("E0195", f.at(a.expr.start), "<%s model={%s}>: its Choices' Row is of type %s, and the rows of %s are %s", c.el.tag, chain, e.display(row.Type()), chain, e.display(related)).
				help("make the widget's Choices a []widget.Choice[%s]", e.display(related))
		}
	}
}

// widgetProp is t's exported field (or promoted one) of that name.
func widgetProp(t types.Type, name string) *types.Var {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)
	if v, ok := obj.(*types.Var); ok && v.IsField() && v.Exported() {
		return v
	}
	return nil
}

// fieldFits reports whether a form field of type ft fills a widget's Value
// of type vt: assignable, or an Option of a value that is.
func fieldFits(ft, vt types.Type) bool {
	if types.AssignableTo(ft, vt) {
		return true
	}
	if inner := optionValue(ft); inner != nil {
		return types.AssignableTo(inner, vt)
	}
	return false
}

// optionValue is T of a vuka.Option[T]; nil for any other type.
func optionValue(t types.Type) types.Type {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "github.com/vuka-lang/vuka/v2" || n.Obj().Name() != "Option" || n.TypeArgs().Len() != 1 {
		return nil
	}
	return n.TypeArgs().At(0)
}

// relatedRow is T of a field referring to rows of T, an orm.FK[T] (a type
// whose Related method returns *T), through an Option or a pointer; nil
// for any other field.
func relatedRow(t types.Type) types.Type {
	if inner := optionValue(t); inner != nil {
		t = inner
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Related")
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return nil
	}
	if p, ok := sig.Results().At(0).Type().(*types.Pointer); ok {
		return p.Elem()
	}
	return nil
}
