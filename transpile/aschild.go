package transpile

import (
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// asChild (RFC components-seamless §5) runs fallthrough in reverse: a bare
// asChild on a component's tag makes the tag's one element child the
// component's forward element. The child goes in the tag's Fallthrough,
// the tag in ui.AsChild:
//
//	<design.Button asChild variant="outline"><a href="/r/42">Report</a></design.Button>
//	→ ui.AsChild(design.Button{Variant: "outline", Fallthrough: ui.Fallthrough{ui.AsChildAttr(<a…>)}})
//
// and the runtime renders the child in the forward element's place, with
// its attributes: the child's class merged last, its own attributes kept,
// handlers of one event both run. The tag's other attributes fall through
// as without asChild, onto the forward element, so onto the child.
//
// The child is one element, written in the tag (E0171 for none, several,
// text, or a component's tag: chaining onto its forward element is for
// later); the component has a forward element to give (E0138, as for
// fallthrough).

const asChildAttr = "asChild"

// asChildArg takes a tag's asChild out of attrs: the Fallthrough entry
// carrying its child, or why it can't be. explicit says the component
// takes its attributes otherwise (Attrs) or is a live component.
func (e *engine) asChildArg(c *jsxComp, attrs []*jsxAttr, fall *fallInfo, explicit bool) ([]*jsxAttr, *compArg, int, diag.Code, string) {
	var at *jsxAttr
	rest := attrs[:0:0]
	for _, a := range attrs {
		if a.name == asChildAttr && !a.pass {
			at = a
			continue
		}
		rest = append(rest, a)
	}
	if at == nil {
		return attrs, nil, 0, "", ""
	}
	el := c.el
	tag := "<" + el.tag + " asChild>"
	switch {
	case at.kind != 'b':
		return nil, nil, at.off, "E0106", tag + " takes asChild bare: <" + el.tag + " asChild>, the child its element"
	case c.elem:
		return nil, nil, at.off, "E0106", "<" + el.tag + "> is a repeated slot's element; asChild goes on a component's tag"
	case explicit || fall == nil:
		why := el.tag + " takes no Fallthrough: it declares Attrs, its struct isn't declared in its template, or its JSX target has no Fallthrough"
		if fall != nil {
			why = el.tag + " is a live component, whose markup renders in its own instance: asChild takes a component that isn't"
		}
		return nil, nil, at.off, "E0138", tag + " gives " + el.tag + "'s forward element to the child, and " + why
	case !c.target.has("AsChild"):
		return nil, nil, at.off, "E0138", tag + " needs " + c.target.q + "AsChild: update " + c.target.path
	}
	if why, help := fall.nowhere(); why != "" {
		return nil, nil, at.off, "E0138", tag + " gives the forward element to the child, and " + why + "; " + help
	}
	var elems []*jsxElem
	text := false
	others := 0
	for _, k := range el.kids {
		switch k := k.(type) {
		case *jsxText:
			if strings.TrimSpace(k.val) != "" {
				text = true
			}
		case *jsxElem:
			if k.isSlot() {
				continue
			}
			elems = append(elems, k)
		default:
			others++
		}
	}
	n := len(elems) + others
	switch {
	case n == 0 && text:
		return nil, nil, el.start, "E0171", tag + " has text and no element: asChild makes its one element child " + el.tag + "'s forward element; wrap the text in the element it is: <a href=…>text</a>"
	case n == 0:
		return nil, nil, el.start, "E0171", tag + " has no children: asChild makes its one element child " + el.tag + "'s forward element; write it inside: " + tag + "<a href=…>…</a></" + el.tag + ">"
	case n > 1 || text:
		what := strconv.Itoa(n) + " children"
		if text {
			what += " and text"
		}
		return nil, nil, el.start, "E0171", tag + " has " + what + ": asChild makes its one element child " + el.tag + "'s forward element; keep one, its content inside it"
	case others > 0:
		return nil, nil, el.start, "E0171", tag + "'s child is a hole or a block, not an element: asChild makes its one element child " + el.tag + "'s forward element"
	case elems[0].comp != nil:
		return nil, nil, elems[0].start, "E0171", tag + "'s child is the component <" + elems[0].tag + ">, and asChild takes an element: write the element it renders, or set the attributes on <" + elems[0].tag + ">"
	}
	c.asChild = true
	return rest, &compArg{attr: at, asChild: true, nodes: []jsxNode{elems[0]}}, 0, "", ""
}
