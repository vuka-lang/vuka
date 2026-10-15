package transpile

import (
	"go/token"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
)

// For a target declaring F (ui does), the lowering wraps each piece of JSX —
// a tree's root, a block's body, a component tag's children — in a ui.F: a
// fingerprint and the piece's shape (which nodes are fixed markup, which are
// Go expressions), so a live session can render it as statics and dynamics
// (ui.Frame, ui.Tree). Other targets get the pieces as they are.

// frame writes ns as a vuka.F: the compile-time shape of a piece of JSX (see
// ui.Frame) around its nodes, so a live session can tell the markup the
// source fixes from what its expressions render. A target that also declares
// FT (newer ui) gets the lazy form instead (frameFT): the same fingerprint and
// shape, but only the dynamics plus a closure that rebuilds the F tree, so a
// plain HTML render needs no node tree at all.
func (w *jsxWriter) frame(ns []jsxNode) {
	switch {
	case w.f.target.has("FT"):
		w.frameFT(ns)
	case w.f.target.has("F"):
		var sh strings.Builder
		shapeOf(&sh, ns)
		w.gen(w.q + "F(" + fingerprint(ns) + ", " + strconv.Quote(sh.String()) + ", ")
		w.kids(ns)
		w.gen(")")
	default:
		w.gen(w.q + "Fragment(")
		w.kids(ns)
		w.gen(")")
	}
}

// frameFT writes ns as a vuka.FT: the fingerprint, the shape, a closure
// func(__d []any) Node that rebuilds exactly the F tree with each dynamic put
// back from __d, and then the dynamics themselves in shape order. ui derives
// the static HTML from the rebuilt tree once and, for a plain HTML render,
// writes it straight through — building no tree. The dynamics are in the same
// order shapeOf lists them (document order), so the closure's __d indices and
// the trailing arguments line up.
func (w *jsxWriter) frameFT(ns []jsxNode) {
	var sh strings.Builder
	shapeOf(&sh, ns)
	fp, shape := fingerprint(ns), strconv.Quote(sh.String())
	w.gen(w.q + "FT(" + fp + ", " + shape + ", func(__d []any) " + w.q + "Node { return " + w.q + "F(" + fp + ", " + shape + ", ")
	bi := 0
	w.buildKids(ns, &bi)
	w.gen(") }, ")
	ai := 0
	w.argKids(ns, &ai)
	w.gen(")")
}

// buildKids writes the rebuilding closure's body for ns: the F tree as it
// stands, but every dynamic replaced by Child(__d[i]) (a child) or __d[i] (an
// attribute value). idx advances once per dynamic, in document order.
func (w *jsxWriter) buildKids(ns []jsxNode, idx *int) {
	for _, n := range ns {
		w.buildNode(n, idx)
		w.gen(", ")
	}
}

func (w *jsxWriter) buildNode(n jsxNode, idx *int) {
	switch n := n.(type) {
	case *jsxText:
		w.gen(w.q + "Text(" + strconv.Quote(n.val) + ")")
	case *jsxHole:
		w.gen(w.q + "Child(__d[" + strconv.Itoa(*idx) + "])")
		*idx++
	case *jsxBlock:
		w.gen(w.q + "Child(__d[" + strconv.Itoa(*idx) + "])")
		*idx++
	case *jsxElem:
		switch {
		case n.comp != nil:
			w.gen(w.q + "Child(__d[" + strconv.Itoa(*idx) + "])")
			*idx++
		case n.frag:
			w.gen(w.q + "Fragment(")
			w.buildKids(n.kids, idx)
			w.gen(")")
		default:
			w.gen(w.q + "El(" + strconv.Quote(n.tag) + ", ")
			if len(n.attrs) == 0 {
				w.gen("nil")
			} else {
				w.gen("[]" + w.q + "Attr{")
				for _, a := range n.attrs {
					w.gen("{Name: " + strconv.Quote(a.name) + ", Value: ")
					switch a.kind {
					case 's':
						w.gen(strconv.Quote(a.val))
					case 'b':
						w.gen("true")
					default:
						w.gen("__d[" + strconv.Itoa(*idx) + "]")
						*idx++
					}
					w.gen("}, ")
				}
				w.gen("}")
			}
			w.gen(", ")
			w.buildKids(n.kids, idx)
			w.gen(")")
		}
	}
}

// argKids writes ns's dynamics as the trailing FT arguments, in document
// order: a child expression as it is (the closure wraps it in Child), a block
// as its Nodes, a component tag as its call, an attribute value as it is (or
// On(…) for an event). idx advances in lockstep with buildKids.
func (w *jsxWriter) argKids(ns []jsxNode, idx *int) {
	for _, n := range ns {
		w.argNode(n, idx)
	}
}

func (w *jsxWriter) argNode(n jsxNode, idx *int) {
	switch n := n.(type) {
	case *jsxText:
	case *jsxHole:
		w.holeValue(n)
		w.gen(", ")
		*idx++
	case *jsxBlock:
		w.node(n)
		w.gen(", ")
		*idx++
	case *jsxElem:
		switch {
		case n.comp != nil:
			w.comp(n.comp)
			w.gen(", ")
			*idx++
		case n.frag:
			w.argKids(n.kids, idx)
		default:
			for _, a := range n.attrs {
				if a.kind != 'e' {
					continue
				}
				w.attrValue(a)
				w.gen(", ")
				*idx++
			}
			w.argKids(n.kids, idx)
		}
	}
}

// body writes a block's body: one frame per iteration or branch taken.
func (w *jsxWriter) body(ns []jsxNode) {
	if len(ns) == 0 {
		return
	}
	if !w.f.target.has("F") {
		for _, n := range ns {
			w.gen("__add(")
			w.node(n)
			w.gen("); ")
		}
		return
	}
	w.gen("__add(")
	w.frame(ns)
	w.gen("); ")
}

// fingerprint hashes what a piece of JSX's statics are made of — its tags,
// attribute names and literal values, text, and where expressions go — so
// pieces that render the same statics share it, wherever they are and
// however the source is formatted.
func fingerprint(ns []jsxNode) string {
	var b strings.Builder
	canon(&b, ns)
	h := fnv.New64a()
	h.Write([]byte(b.String()))
	return "0x" + strconv.FormatUint(h.Sum64(), 16)
}

func canon(b *strings.Builder, ns []jsxNode) {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxText:
			b.WriteString("t" + strconv.Quote(n.val))
		case *jsxHole:
			b.WriteByte('h')
		case *jsxBlock:
			b.WriteByte('b')
		case *jsxElem:
			switch {
			case n.comp != nil:
				b.WriteByte('c')
				continue
			case n.frag:
				b.WriteByte('G')
			default:
				b.WriteString("E" + n.tag)
				for _, a := range n.attrs {
					b.WriteString(" " + a.name + string(a.kind))
					if a.kind == 's' {
						b.WriteString(strconv.Quote(a.val))
					}
				}
			}
			b.WriteByte('(')
			canon(b, n.kids)
			b.WriteByte(')')
		}
	}
}

// shapeOf writes the shape of nodes, one item each (ui.Frame).
func shapeOf(b *strings.Builder, ns []jsxNode) {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxText:
			b.WriteByte('t')
		case *jsxHole:
			b.WriteByte('h')
		case *jsxBlock:
			b.WriteByte('b')
		case *jsxElem:
			switch {
			case n.comp != nil:
				b.WriteByte('c')
				continue
			case n.frag:
				b.WriteByte('G')
			default:
				b.WriteByte('E')
				for _, a := range n.attrs {
					if a.kind == 'e' {
						b.WriteByte('d')
					} else {
						b.WriteByte('s')
					}
				}
			}
			b.WriteByte('(')
			shapeOf(b, n.kids)
			b.WriteByte(')')
		}
	}
}

// declName is the declaration a top-level func at toks[i] belongs to, for
// Options.Scopes: a method's receiver type (func (c *Counter[T]) Render →
// Counter), a static method's type (func User.New → User), else the
// function's name.
func declName(toks []tok, i int) string {
	j := i + 1
	for j < len(toks) && trivia(toks[j]) {
		j++
	}
	if j >= len(toks) {
		return ""
	}
	switch t := toks[j]; {
	case t.tok == token.LPAREN:
		name, d := "", 0
		for k := j + 1; k < len(toks); k++ {
			switch toks[k].tok {
			case token.LPAREN, token.LBRACK:
				d++
			case token.RBRACK:
				d--
			case token.RPAREN:
				if d == 0 {
					return name
				}
				d--
			case token.IDENT:
				if d == 0 {
					name = toks[k].lit
				}
			}
		}
	case t.tok == token.IDENT:
		return t.lit
	}
	return ""
}

// scopeAttr is the attribute marking an element as the markup of a scoped
// declaration (Options.Scopes).
const scopeAttr = "data-vk-s"

// markScope adds scope's attribute to every element of ns written in the
// source: elements in blocks and in a component tag's children too, but
// not the tags of components, whose markup is their own.
func markScope(ns []jsxNode, scope string) {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxElem:
			if !n.frag && n.comp == nil && !slices.ContainsFunc(n.attrs, func(a *jsxAttr) bool { return a.name == scopeAttr }) {
				n.attrs = append(n.attrs, &jsxAttr{name: scopeAttr, off: n.tagEnd, kind: 's', val: scope})
			}
			markScope(n.kids, scope)
		case *jsxBlock:
			for _, b := range n.bodies {
				markScope(b, scope)
			}
			for _, c := range n.cases {
				markScope(c, scope)
			}
		}
	}
}
