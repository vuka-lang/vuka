package transpile

import (
	"go/types"
	"strings"
)

// Two attribute forms set plain attributes without a literal (RFC
// components-seamless §3):
//
//	<Button attr:form={"rule-" + r.Target}>  →  Attrs: []ui.Attr{{Name: "form", Value: "rule-" + r.Target}}
//	<input id?={ID} />                       →  <input id={ui.OmitZero(ID)} />
//
// attr:name is the attribute name, whatever name means to the compiler: on
// a component it goes to the props' Attrs (or Attributes) with no field of
// that name looked for, on an element it is the attribute as written. And
// name?={v} leaves the attribute out when v is its type's zero value: on an
// element and in a component's Attrs it is ui.OmitZero(v), on a prop it is
// not set — the prop's default (vuka:"default=…", defaults.go) or its zero.
// A bool is already left out when false (E0135), and a state's value is the
// browser's, which the server can't omit by (E0135).

// passPrefix starts an attribute written by its HTML name: attr:form.
const passPrefix = "attr:"

// checkPass checks an attr:name's name.
func (p *jsxParser) checkPass(el *jsxElem, a *jsxAttr) {
	if !a.pass {
		return
	}
	name := strings.TrimPrefix(a.name, passPrefix)
	switch {
	case name == "":
		p.fail(a.off, "attr: names the attribute it sets: attr:form={id}")
	case strings.HasPrefix(name, passPrefix):
		p.fail(a.off, "%s: one attr: is the attribute %s", a.name, strings.TrimPrefix(name, passPrefix))
	case !isLetter(name[0]):
		p.fail(a.off, "%s: an attribute's name starts with a letter", a.name)
	case strings.HasPrefix(el.tag, "slot:"):
		p.fail(a.off, "<%s> is a slot, which has no attributes of its own: %s goes on an element", el.tag, a.name)
	}
}

// checkOmit refuses name?= on what isn't an attribute with a value.
func (p *jsxParser) checkOmit(el *jsxElem, a *jsxAttr, comp bool) {
	if !a.omit {
		return
	}
	name := a.name
	if a.pass {
		name = strings.TrimPrefix(name, passPrefix)
	}
	switch {
	case a.kind != 'e':
		p.failCode("E0135", a.off, "%s?= takes an expression, left out when its value is zero: %s?={v}", name, name)
	case !a.pass && (isEvent(a.name) || comp && a.name == "on"):
		p.failCode("E0135", a.off, "%s?=: an event's handler is left out when it is nil; write %s={…}", a.name, a.name)
	case !a.pass && (a.name == "key" || a.name == "let" || a.name == "model" || a.name == "innerHTML" || a.name == "ref" || strings.HasPrefix(a.name, "model.")):
		p.failCode("E0135", a.off, "%s?=: %s isn't an attribute the element writes, so it can't be left out; write %s={…}", a.name, a.name, a.name)
	case !a.pass && (strings.HasPrefix(a.name, "bind:") || strings.HasPrefix(a.name, "class:") || strings.HasPrefix(a.name, "style:")):
		p.failCode("E0135", a.off, "%s?=: %s takes a value that is the attribute's own; write %s={…}", a.name, a.name, a.name)
	case strings.HasPrefix(el.tag, "slot:"):
		p.failCode("E0135", a.off, "<%s> is a slot: ?= goes on an element's or a component's attribute", el.tag)
	}
}

// checkOmitZero checks, once their types are known, each name?={v} of f's:
// v is no bool (whose false is already left out) and reads no state
// (E0135).
func (e *engine) checkOmitZero(f *fileState) {
	if f.omitChecked || e.info == nil {
		return
	}
	pending := false
	for _, t := range f.jsx {
		walkNodes([]jsxNode{t.root}, func(n jsxNode) {
			el, ok := n.(*jsxElem)
			if !ok {
				return
			}
			for _, a := range el.attrs {
				if !a.omit || a.kind != 'e' {
					continue
				}
				x := e.attrExpr(f, a)
				if x == nil {
					pending = true
					continue
				}
				tt := e.info.TypeOf(x)
				if tt == nil || isInvalid(tt) {
					pending = true
					continue
				}
				if !el.isComp() && !f.target.has("OmitZero") {
					e.errs.add("E0135", f.at(a.off), "<%s %s?={…}> needs %sOmitZero: update %s", el.tag, attrShown(a), f.target.q, f.target.path)
					continue
				}
				src := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
				name := attrShown(a)
				if v, state := readOf(tt); state {
					e.errs.add("E0135", f.at(a.off), "<%s %s?={%s}>: %s is a state, whose value changes in the browser, so the server can't leave the attribute out by it", el.tag, name, src, src).
						help("bind the attribute itself, %s={%s}, which writes the state's %s as it changes", name, src, e.display(v))
					continue
				}
				if isBasic(tt, types.IsBoolean) {
					e.errs.add("E0135", f.at(a.off), "<%s %s?={%s}>: %s is a bool, and a false bool attribute is left out already", el.tag, name, src, src).
						help("write it %s={%s}: true writes the bare attribute, false none", name, src)
				}
			}
		})
	}
	f.omitChecked = !pending
}

// attrShown is an attribute's name as written, its attr: kept.
func attrShown(a *jsxAttr) string { return a.name }

// isComp reports whether el is a component's tag.
func (el *jsxElem) isComp() bool { return el.comp != nil }

// omitWrap writes v, the value of an element's or an Attrs entry's name?={v},
// as ui.OmitZero(v).
func (w *jsxWriter) omitWrap(a *jsxAttr, write func()) {
	if !a.omit {
		write()
		return
	}
	w.gen(w.q + "OmitZero(")
	write()
	w.gen(")")
}

// passName is the HTML name of a component's attribute for its Attrs:
// attr:form's form.
func passName(a *jsxAttr) string { return strings.TrimPrefix(a.name, passPrefix) }
