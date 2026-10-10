package transpile

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// The lowering wraps each piece of JSX — a tree's root, a block's body, a
// component tag's children — in a vuka.F: a fingerprint and the piece's
// shape (which nodes are fixed markup, which are Go expressions), so a live
// session can render it as statics and dynamics (vuka.Frame, vuka.Tree).

// frame writes ns as a vuka.F: the compile-time shape of a piece of JSX (see
// vuka.Frame) around its nodes, so a live session can tell the markup the
// source fixes from what its expressions render.
func (w *jsxWriter) frame(ns []jsxNode) {
	var sh strings.Builder
	shapeOf(&sh, ns)
	w.gen(w.rt + ".F(" + fingerprint(ns) + ", " + strconv.Quote(sh.String()) + ", ")
	w.kids(ns)
	w.gen(")")
}

// body writes a block's body: one frame per iteration or branch taken.
func (w *jsxWriter) body(ns []jsxNode) {
	if len(ns) == 0 {
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

// shapeOf writes the shape of nodes, one item each (vuka.Frame).
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
