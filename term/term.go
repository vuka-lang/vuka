// Package term renders a vuka Node tree as terminal text: headings, paragraphs
// word-wrapped at a width, lists, tables, quotes, code, links, and inline
// bold/italic/underline as ANSI styles when Color is on.
//
// It walks the tree (vuka.Walk), so it sees elements and text. A component
// that only renders HTML — a templ component, a NodeFunc — and Safe content
// arrive as HTML, which is parsed into the same tree, so their headings,
// tables and lists lay out as if written in JSX.
//
// The package is pure: whether to use color (NO_COLOR, a tty) is the
// caller's decision.
package term

import (
	"context"
	"html"
	"io"
	"strings"

	"github.com/vuka-lang/vuka"
)

// Options shape the output.
type Options struct {
	Width int  // wrap paragraphs at this many columns; 0 doesn't wrap
	Color bool // ANSI styles: bold, italic, underline, dim, inverse
}

// Render writes n as terminal text to w, ending with a newline unless empty.
func Render(ctx context.Context, w io.Writer, n vuka.Node, o Options) error {
	s, err := String(ctx, n, o)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, s)
	return err
}

// String renders n as terminal text.
func String(ctx context.Context, n vuka.Node, o Options) (string, error) {
	b := builder{}
	b.stack = []*node{&b.root}
	if err := vuka.Walk(ctx, n, &b); err != nil {
		return "", err
	}
	l := layout{o: o}
	lines := l.blocks(b.root.kids, o.Width, nil, false)
	if len(lines) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for _, ln := range lines {
		sb.WriteString(strings.TrimRight(ln.s, " "))
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// node is the tree the layout works on: an element or, with tag "", text.
type node struct {
	tag   string
	attrs []vuka.Attr
	text  string
	kids  []*node
}

func (n *node) attr(name string) (string, bool) {
	for _, a := range n.attrs {
		an := a.Name
		if an == "className" {
			an = "class"
		}
		if an != name {
			continue
		}
		switch v := a.Value.(type) {
		case nil:
			return "", false
		case bool:
			return "", v
		case string:
			return v, true
		}
		s, _ := vuka.String(context.Background(), vuka.Text(a.Value))
		return html.UnescapeString(s), true
	}
	return "", false
}

// builder is the vuka.Renderer building the node tree.
type builder struct {
	root  node
	stack []*node
}

func (b *builder) top() *node { return b.stack[len(b.stack)-1] }

func (b *builder) Open(e *vuka.Element) error {
	b.open(strings.ToLower(e.Tag), e.Attrs, false)
	return nil
}

func (b *builder) open(tag string, attrs []vuka.Attr, empty bool) {
	n := &node{tag: tag, attrs: attrs}
	t := b.top()
	t.kids = append(t.kids, n)
	if !empty && !vuka.Void(tag) {
		b.stack = append(b.stack, n)
	}
}

func (b *builder) Close(*vuka.Element) error {
	b.stack = b.stack[:len(b.stack)-1]
	return nil
}

func (b *builder) Text(s string) error {
	t := b.top()
	t.kids = append(t.kids, &node{text: s})
	return nil
}

func (b *builder) Opaque(ctx context.Context, n vuka.Node) error {
	var sb strings.Builder
	if err := n.Render(ctx, &sb); err != nil {
		return err
	}
	return b.Raw(sb.String())
}

func isBlock(tag string) bool {
	switch tag {
	case "p", "div", "section", "header", "footer", "article", "main", "form", "nav", "aside",
		"h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "pre", "blockquote", "table",
		"thead", "tbody", "tfoot", "tr", "hr", "figure", "figcaption", "details", "summary",
		"dl", "dt", "dd", "body", "html", "address", "fieldset", "legend",
		"script", "style", "head", "title", "template", "noscript":
		return true
	}
	return false
}

// skipped elements render nothing.
func skipped(tag string) bool {
	switch tag {
	case "script", "style", "head", "title", "template", "noscript":
		return true
	}
	return false
}
