package vuka

import (
	"context"
	"io"
	"slices"
)

// Frame is a piece of JSX as the compiler saw it: a tree's root, a block's
// body (one per loop iteration or branch taken), or a component tag's
// children. It renders exactly as its Kids would; under a live session it is
// also a frame of the render tree, whose markup splits into statics (what the
// source fixes, identical every render) and dynamics (what Go expressions
// produce), so a session sends the statics once and then only the dynamics
// that changed.
//
// FP identifies the JSX it comes from: its file, the tree's source and the
// frame's place in it. Shape describes Kids, one item each, in the order they
// appear:
//
//	t        text from the source
//	h b c    a dynamic: an {expr} child, a {for}/{if}/{match} block, a component tag
//	E…(…)    an element: a flag per attribute, s (a literal) or d (an expression),
//	         then its children's items in parentheses
//	G(…)     a fragment, <>…</>
//
// Hand-written trees need no Frame: a Node that isn't one is a single dynamic
// of the frame around it, rendered to HTML.
type Frame struct {
	FP    uint64
	Shape string
	Kids  []Node
	extra int // attributes RootAttrs added to the root element, beyond Shape's
}

// F is a Frame. Generated code calls it.
func F(fp uint64, shape string, kids ...Node) Node {
	return &Frame{FP: fp, Shape: shape, Kids: kids}
}

func (n *Frame) Render(ctx context.Context, w io.Writer) error { return renderHTML(ctx, w, n) }

// RootAttrs is n with attrs added after its root element's own, when n is an
// element or a frame of one element; ok is false otherwise.
func RootAttrs(n Node, attrs ...Attr) (Node, bool) {
	switch n := n.(type) {
	case *Element:
		if n == nil {
			return nil, false
		}
		cp := *n
		cp.Attrs = append(slices.Clip(n.Attrs), attrs...)
		return &cp, true
	case *Frame:
		if n == nil || len(n.Kids) != 1 || n.Shape == "" || n.Shape[0] != 'E' {
			return nil, false
		}
		root, ok := RootAttrs(n.Kids[0], attrs...)
		if !ok {
			return nil, false
		}
		cp := *n
		cp.Kids, cp.extra = []Node{root}, n.extra+len(attrs)
		return &cp, true
	}
	return nil, false
}
