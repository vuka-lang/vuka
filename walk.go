package vuka

import (
	"context"
	"fmt"
	"strings"
)

// Renderer receives a node tree in document order; Walk drives it. The HTML
// renderer behind every Node's Render is one; package term is another.
type Renderer interface {
	Open(e *Element) error  // before children
	Close(e *Element) error // after children (not called for void elements)
	Text(s string) error
	Raw(html string) error                    // Safe content
	Opaque(ctx context.Context, n Node) error // a component that only renders HTML (templ, NodeFunc)
}

// Walk sends n's tree to r. Groups, Builders and Child conversions are
// flattened (r never sees them); a TryNode with an error stops the walk with
// it; a Boundary walks its children into a recording first, and replays it
// into r — or, when they fail, walks Fallback(err) instead, so r never sees
// the failed part. Inside a Boundary an opaque Node is rendered while
// recording and replayed as Opaque of its HTML (a RawHTML). Children on a void
// element are an error. The first error from r stops the walk.
func Walk(ctx context.Context, n Node, r Renderer) error {
	switch n := n.(type) {
	case nil:
		return nil
	case *Element:
		if n == nil {
			return nil
		}
		void := Void(n.Tag)
		if void && len(n.Children) > 0 {
			return fmt.Errorf("vuka: <%s> is a void element and can't have children", n.Tag)
		}
		if err := r.Open(n); err != nil || void {
			return err
		}
		for _, c := range n.Children {
			if err := Walk(ctx, c, r); err != nil {
				return err
			}
		}
		return r.Close(n)
	case TextNode:
		return r.Text(string(n))
	case RawHTML:
		return r.Raw(string(n))
	case Group:
		for _, c := range n {
			if err := Walk(ctx, c, r); err != nil {
				return err
			}
		}
		return nil
	case Builder:
		if n == nil {
			return nil
		}
		var err error
		n(func(c Node) {
			if err == nil {
				err = Walk(ctx, c, r)
			}
		})
		return err
	case TryNode:
		if n.Err != nil {
			return n.Err
		}
		return Walk(ctx, n.Node, r)
	case Boundary:
		var rec recording
		for _, c := range n.Children {
			if err := Walk(ctx, c, &rec); err != nil {
				if n.Fallback == nil {
					return nil
				}
				return Walk(ctx, n.Fallback(err), r)
			}
		}
		return rec.replay(ctx, r)
	}
	return r.Opaque(ctx, n)
}

type event struct {
	kind uint8
	e    *Element
	s    string
}

const (
	evOpen uint8 = iota
	evClose
	evText
	evRaw
	evOpaque
)

// recording is a Renderer keeping what it receives, for a Boundary.
type recording []event

func (r *recording) Open(e *Element) error  { *r = append(*r, event{evOpen, e, ""}); return nil }
func (r *recording) Close(e *Element) error { *r = append(*r, event{evClose, e, ""}); return nil }
func (r *recording) Text(s string) error    { *r = append(*r, event{evText, nil, s}); return nil }
func (r *recording) Raw(s string) error     { *r = append(*r, event{evRaw, nil, s}); return nil }

func (r *recording) Opaque(ctx context.Context, n Node) error {
	var b strings.Builder
	if err := n.Render(ctx, &b); err != nil {
		return err
	}
	*r = append(*r, event{evOpaque, nil, b.String()})
	return nil
}

func (r recording) replay(ctx context.Context, to Renderer) error {
	for _, ev := range r {
		var err error
		switch ev.kind {
		case evOpen:
			err = to.Open(ev.e)
		case evClose:
			err = to.Close(ev.e)
		case evText:
			err = to.Text(ev.s)
		case evRaw:
			err = to.Raw(ev.s)
		case evOpaque:
			err = to.Opaque(ctx, RawHTML(ev.s))
		}
		if err != nil {
			return err
		}
	}
	return nil
}
