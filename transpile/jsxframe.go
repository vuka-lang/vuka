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
// source fixes from what its expressions render. off, the piece's place in
// its tree, makes its fingerprint.
func (w *jsxWriter) frame(off int, ns []jsxNode) {
	var sh strings.Builder
	shapeOf(&sh, ns)
	w.gen(w.rt + ".F(" + w.fp(off) + ", " + strconv.Quote(sh.String()) + ", ")
	w.kids(ns)
	w.gen(")")
}

// body writes a block's body: one frame per iteration or branch taken.
func (w *jsxWriter) body(off int, ns []jsxNode) {
	if len(ns) == 0 {
		return
	}
	w.gen("__add(")
	w.frame(off, ns)
	w.gen("); ")
}

// fp fingerprints the piece of JSX at off: its file, its tree's source and
// its place in the tree.
func (w *jsxWriter) fp(off int) string {
	h := fnv.New64a()
	t := w.tree
	h.Write([]byte(w.f.name + "\x00"))
	h.Write(w.f.src[t.start:t.end])
	h.Write([]byte("\x00" + strconv.Itoa(off-t.start)))
	return "0x" + strconv.FormatUint(h.Sum64(), 16)
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

