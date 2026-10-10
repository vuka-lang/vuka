package transpile

import (
	"hash/fnv"
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
// source fixes from what its expressions render.
func (w *jsxWriter) frame(ns []jsxNode) {
	if !w.f.target.has("F") {
		w.gen(w.q + "Fragment(")
		w.kids(ns)
		w.gen(")")
		return
	}
	var sh strings.Builder
	shapeOf(&sh, ns)
	w.gen(w.q + "F(" + fingerprint(ns) + ", " + strconv.Quote(sh.String()) + ", ")
	w.kids(ns)
	w.gen(")")
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
