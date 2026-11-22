package transpile

import "go/types"

// An element's innerHTML={x} is its content, x a ui.RawHTML written as is:
// <div innerHTML={Body}/> is <div>{Body}</div>, the hole's value checked
// once its type is known. Anything but a RawHTML there is E0133; innerHTML
// beside children, as a quoted value, or on an element that holds no
// markup, E0134.

// innerHTML takes el's innerHTML={…} out of its attributes and makes its
// expression the element's one child.
func (p *jsxParser) innerHTML(el *jsxElem) {
	for i, a := range el.attrs {
		if a.name != "innerHTML" {
			continue
		}
		switch {
		case a.kind != 'e':
			p.failCode("E0134", a.off, "<%s innerHTML> takes a %sRawHTML: innerHTML={body}", el.tag, p.f.target.qual())
		case isVoid(el.tag) || rawText[el.tag]:
			p.failCode("E0134", a.off, "<%s> holds no markup: innerHTML goes on an element that does, a <div>", el.tag)
		case hasContent(el.kids):
			p.failCode("E0134", a.off, "<%s> has innerHTML and children: innerHTML={…} is its content, so it takes no other", el.tag)
		}
		el.attrs = append(el.attrs[:i:i], el.attrs[i+1:]...)
		el.kids = []jsxNode{&jsxHole{expr: a.expr, inner: &innerUse{off: a.off, tag: el.tag}}}
		return
	}
}

// qual is the target's qualifier, "ui." when there is none yet.
func (t *jsxTarget) qual() string {
	if t == nil {
		return "ui."
	}
	return t.q
}

// innerUse is where an innerHTML={…} was written, for its hole's check.
type innerUse struct {
	off int
	tag string
}

// rawText are the elements whose content is text, never markup.
var rawText = setOf(`script style textarea title xmp iframe noembed noframes noscript plaintext`)

func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}

// checkInnerHTML checks that each innerHTML={…} of f's holds a RawHTML of
// the JSX target, once its type is known.
func (e *engine) checkInnerHTML(f *fileState) {
	if f.innerChecked || e.info == nil {
		return
	}
	pending := false
	defer func() { f.innerChecked = !pending }()
	for _, t := range f.jsx {
		walkNodes([]jsxNode{t.root}, func(n jsxNode) {
			h, ok := n.(*jsxHole)
			if !ok || h.inner == nil {
				return
			}
			var tt types.Type
			if x := e.exprAt(f, h.expr); x != nil {
				tt = e.info.TypeOf(x)
			}
			if tt == nil || isInvalid(tt) {
				pending = true
				return
			}
			raw, _ := e.targetObj(f.target, "RawHTML").(*types.TypeName)
			if raw != nil && types.Identical(tt, raw.Type()) {
				return
			}
			src := string(f.src[h.expr.start:h.expr.end])
			err := e.errs.add("E0133", f.at(h.inner.off), "<%s innerHTML={%s}>: innerHTML takes a %sRawHTML, and %s is a %s", h.inner.tag, src, f.target.q, src, e.display(tt))
			node := e.nodeType(f.target)
			_, state := readOf(tt)
			switch {
			case raw == nil:
				err.help("the JSX target %s has no RawHTML", f.target.path)
			case state:
				err.help("a state changes in the browser, as text: keep the HTML in a field the server renders, %sRawHTML", f.target.q)
			case node != nil && types.AssignableTo(tt, node):
				err.help("a %s goes in as a child: <%s>{%s}</%s>", e.display(tt), h.inner.tag, src, h.inner.tag)
			case !isBasic(tt, types.IsString):
				err.help("innerHTML takes HTML: a %sRawHTML, or %sSanitize of a string", f.target.q, f.target.q)
			default:
				err.help("wrap it in %sSanitize(%s) when users write it, or %sRawHTML(%s) when it is trusted", f.target.q, src, f.target.q, src)
			}
		})
	}
}
