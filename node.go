package vuka

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

// VukaJSX marks the package as a JSX target implementing version 1 of the
// contract: .vuka files lower their JSX to its El, Text, Child, Fragment,
// Nodes, Try, On and Component.
const VukaJSX = 1

// Node is anything that renders HTML: what a JSX expression evaluates to. It is
// templ.Component itself, so Vuka components are templ components and templ
// components are Vuka children, with nothing in between.
//
// The Nodes built here (*Element, TextNode, RawHTML, Group, Builder, TryNode,
// Boundary) are also a tree any Renderer can Walk; any other Node — a templ
// component, a NodeFunc — is opaque: it can only render HTML. Nodes are
// immutable values; rendering one concurrently is safe.
type Node = templ.Component

// NodeFunc is a function rendering HTML: templ.ComponentFunc.
type NodeFunc = templ.ComponentFunc

// Attr is one attribute of an element. In HTML it renders the way templ
// renders the same attribute written as an expression:
//   - nil and false omit it, true writes it bare;
//   - class takes a string or anything templ.Classes does ([]string,
//     map[string]bool, templ.KV, …);
//   - style takes a Style or anything templ's style attributes do, sanitized
//     by templ (an unsafe property or value becomes templ's placeholder);
//   - href on a and link, action on form and data on object are URLs: templ.URL
//     keeps relative URLs and http, https, mailto, tel, ftp and ftps, and turns
//     anything else into about:invalid#TemplFailedSanitizationURL; a
//     templ.SafeURL passes as is. Like templ, other attributes (img src, …)
//     aren't URL-checked;
//   - a templ.ComponentScript writes its Call; a Node is rendered and escaped;
//     anything else goes through fmt.Sprint and is escaped
//     (templ.ResolveAttributeValue).
//
// className and htmlFor name class and for. Names must be letters, digits,
// - _ : and . (templ only ever sees literal names; here a dynamic one could
// smuggle in another attribute), else Render fails.
type Attr struct {
	Name  string
	Value any
}

// Style is an inline style: style={vuka.Style{"color": "red"}}, rendered by
// templ as "color:red;" sorted by property, each property and value sanitized.
type Style map[string]string

// Element is an element: <Tag Attrs>Children</Tag>. A void element (br, img,
// input, …; see Void) has no closing tag, and children on one are an error; in
// HTML, html is preceded by the doctype.
//
// Text inside script follows templ's Go expressions in a script: it is
// JSON-encoded (templ's runtime.ScriptContentOutsideStringLiteral), so it
// arrives as a quoted JS string with < > & escaped. templ has no expressions
// inside style; here text in a style element is written as is with "</"
// written "<\/", so it can't end the element.
type Element struct {
	Tag      string
	Attrs    []Attr
	Children []Node
}

// TextNode is text, escaped when rendered.
type TextNode string

// RawHTML is trusted HTML, written as is.
type RawHTML string

// Group is children with no element around them.
type Group []Node

// Builder produces children as it runs: a loop, if or match block. It runs
// once per Render or Walk, each added child rendered as it is added.
type Builder func(add func(Node))

// TryNode is a (Node, error) component's result as a child: Err fails the
// render, else Node renders.
type TryNode struct {
	Node Node
	Err  error
}

// Boundary renders Children or, when one of them fails, Fallback(err) in place
// of everything they produced.
type Boundary struct {
	Fallback func(err error) Node
	Children []Node
}

// El is an element.
func El(tag string, attrs []Attr, children ...Node) Node {
	return &Element{Tag: tag, Attrs: attrs, Children: children}
}

// Text is text: v formatted like fmt.Sprint, nil as nothing.
func Text(v any) Node { return TextNode(toString(v)) }

// Child is a {expr} child: a Node as is, nil as nothing, an error as its
// message, a []Node or []any as each element in turn, anything else as Text.
func Child(v any) Node {
	switch v := v.(type) {
	case nil:
		return Group(nil)
	case Node:
		return v
	case string:
		return TextNode(v)
	case []Node:
		return Group(v)
	case []any:
		ns := make(Group, len(v))
		for i, x := range v {
			ns[i] = Child(x)
		}
		return ns
	}
	return TextNode(toString(v))
}

// Fragment renders children in order, with no element around them.
func Fragment(children ...Node) Node { return Group(children) }

// Nodes is the children a loop, if or match block produces.
func Nodes(build func(add func(Node))) Node { return Builder(build) }

// Safe is trusted HTML, written as is. Never pass it user input.
func Safe(html string) Node { return RawHTML(html) }

// Try is a (Node, error) component's result as a child.
func Try(n Node, err error) Node { return TryNode{n, err} }

// ErrorBoundary renders children, or — when one of them fails — fallback(err)
// in place of everything they wrote.
func ErrorBoundary(fallback func(err error) Node, children ...Node) Node {
	return Boundary{fallback, children}
}

func (n *Element) Render(ctx context.Context, w io.Writer) error { return renderHTML(ctx, w, n) }
func (n TextNode) Render(ctx context.Context, w io.Writer) error { return renderHTML(ctx, w, n) }
func (n RawHTML) Render(ctx context.Context, w io.Writer) error  { return renderHTML(ctx, w, n) }
func (n Group) Render(ctx context.Context, w io.Writer) error    { return renderHTML(ctx, w, n) }
func (n Builder) Render(ctx context.Context, w io.Writer) error  { return renderHTML(ctx, w, n) }
func (n TryNode) Render(ctx context.Context, w io.Writer) error  { return renderHTML(ctx, w, n) }
func (n Boundary) Render(ctx context.Context, w io.Writer) error { return renderHTML(ctx, w, n) }

// String renders n as HTML into a string.
func String(ctx context.Context, n Node) (string, error) {
	if n == nil {
		return "", nil
	}
	var b strings.Builder
	err := n.Render(ctx, &b)
	return b.String(), err
}

// Void reports whether tag is a void element: no children, no closing tag.
func Void(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}

// toString formats v like fmt.Sprint, with nil as "".
func toString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	}
	return fmt.Sprint(v)
}
