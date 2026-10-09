package vuka

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	templruntime "github.com/a-h/templ/runtime"
)

// Node is anything that renders HTML: what a JSX expression evaluates to. It is
// templ.Component itself, so Vuka components are templ components and templ
// components are Vuka children, with nothing in between. Nodes built here are
// immutable values; rendering one concurrently is safe.
type Node = templ.Component

// NodeFunc is a function rendering HTML: templ.ComponentFunc.
type NodeFunc = templ.ComponentFunc

// Attr is one attribute of an element, rendered the way templ renders the same
// attribute written as an expression:
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
//   - a Node is rendered and escaped; anything else (templ.ComponentScript
//     included) goes through templ.ResolveAttributeValue.
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

// El is an element: <tag attrs>children</tag>. A void element (br, img, input,
// …) has no closing tag, and children on one make Render fail; html is
// preceded by the doctype.
//
// Text inside script follows templ's Go expressions in a script: the value is
// JSON-encoded (templ's runtime.ScriptContentOutsideStringLiteral), so a
// string arrives as a quoted JS string with < > & escaped. templ has no
// expressions inside style; here Text in a style element is written as is
// with "</" written "<\/", so it can't end the element.
func El(tag string, attrs []Attr, children ...Node) Node {
	return &element{tag: tag, attrs: attrs, children: children}
}

// Text is text, escaped by templ.EscapeString: v formatted like fmt.Sprint,
// nil as nothing.
func Text(v any) Node { return textNode{v} }

// Child is a {expr} child: a Node as is, nil as nothing, an error as its
// message, a []Node or []any as each element in turn, anything else as Text.
func Child(v any) Node {
	switch v := v.(type) {
	case nil:
		return fragment(nil)
	case Node:
		return v
	case error:
		return textNode{v.Error()}
	case []Node:
		return fragment(v)
	case []any:
		ns := make([]Node, len(v))
		for i, x := range v {
			ns[i] = Child(x)
		}
		return fragment(ns)
	}
	return textNode{v}
}

// Fragment renders children in order, with no element around them.
func Fragment(children ...Node) Node { return fragment(children) }

// Nodes is the children a loop, if or match block produces: build runs once
// per Render and adds each child, rendered as it is added.
func Nodes(build func(add func(Node))) Node { return nodes(build) }

// Safe is trusted HTML, written as is (templ.Raw). Never pass it user input.
func Safe(html string) Node { return templ.Raw(html) }

// Try is a (Node, error) component's result as a child: Render fails with err
// when it isn't nil and renders n otherwise.
func Try(n Node, err error) Node { return tryNode{n, err} }

// ErrorBoundary renders children, or — when one of them fails — fallback(err)
// in place of everything they wrote.
func ErrorBoundary(fallback func(err error) Node, children ...Node) Node {
	return boundary{fallback, children}
}

type (
	element struct {
		tag      string
		attrs    []Attr
		children []Node
	}
	textNode struct{ v any }
	fragment []Node
	nodes    func(add func(Node))
	tryNode  struct {
		n   Node
		err error
	}
	boundary struct {
		fallback func(error) Node
		children []Node
	}
)

// rawTextKey marks the context of a script or style element's children.
type rawTextKey struct{}

const (
	inScript = 1
	inStyle  = 2
)

func renderAll(ctx context.Context, w io.Writer, children []Node) error {
	for _, c := range children {
		if c == nil {
			continue
		}
		if err := c.Render(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

func (n *element) Render(ctx context.Context, w io.Writer) (err error) {
	if !validName(n.tag) {
		return fmt.Errorf("vuka: invalid tag name %q", n.tag)
	}
	void := isVoid(n.tag)
	if void && len(n.children) > 0 {
		return fmt.Errorf("vuka: <%s> is a void element and can't have children", n.tag)
	}
	buf, existing := templruntime.GetBuffer(w)
	if !existing {
		defer func() {
			if ferr := templruntime.ReleaseBuffer(buf); err == nil {
				err = ferr
			}
		}()
	}
	if n.tag == "html" {
		buf.WriteString("<!DOCTYPE html>")
	}
	buf.WriteString("<")
	buf.WriteString(n.tag)
	if len(n.attrs) > 0 {
		if err = n.renderAttrs(ctx, buf); err != nil {
			return err
		}
	}
	if _, err = buf.WriteString(">"); err != nil || void {
		return err
	}
	cctx := ctx
	switch n.tag {
	case "script":
		cctx = context.WithValue(ctx, rawTextKey{}, inScript)
	case "style":
		cctx = context.WithValue(ctx, rawTextKey{}, inStyle)
	}
	if err = renderAll(cctx, buf, n.children); err != nil {
		return err
	}
	buf.WriteString("</")
	buf.WriteString(n.tag)
	_, err = buf.WriteString(">")
	return err
}

// renderAttrs writes the attributes as templ's generated code does: each
// value resolved the way templ resolves an attribute expression of that name,
// then escaped by templ.EscapeString.
func (n *element) renderAttrs(ctx context.Context, w *templruntime.Buffer) error {
	for _, a := range n.attrs {
		name := a.Name
		switch name {
		case "className":
			name = "class"
		case "htmlFor":
			name = "for"
		}
		if !validName(name) {
			return fmt.Errorf("vuka: invalid attribute name %q", a.Name)
		}
		v, err := attrValue(ctx, n.tag, name, a.Value)
		if err != nil {
			return err
		}
		switch v := v.(type) {
		case bool:
			if v {
				w.WriteString(" ")
				w.WriteString(name)
			}
		case rawAttr:
			w.WriteString(" ")
			w.WriteString(name)
			w.WriteString(`="`)
			w.WriteString(string(v))
			w.WriteString(`"`)
		case string:
			w.WriteString(" ")
			w.WriteString(name)
			w.WriteString(`="`)
			w.WriteString(templ.EscapeString(v))
			w.WriteString(`"`)
		}
	}
	return nil
}

// attrValue is an attribute's value resolved: nil omits it, a bool is bare
// or omitted, a string is escaped when written, a rawAttr isn't.
func attrValue(ctx context.Context, tag, name string, v any) (any, error) {
	switch v := v.(type) {
	case nil, bool:
		return v, nil
	case templ.ComponentScript: // a Node too; templ writes its Call as is
		return rawAttr(v.Call), nil
	case Node:
		s, err := String(ctx, v)
		return s, err
	}
	switch {
	case name == "class":
		if s, ok := v.(string); ok {
			return s, nil
		}
		return templ.Classes(v).String(), nil
	case name == "style":
		if s, ok := v.(Style); ok {
			v = map[string]string(s)
		}
		// Sanitized and escaped, then escaped once more when written, as templ does.
		return templruntime.SanitizeStyleAttributeValues(v)
	case isURLAttr(tag, name):
		var u templ.SafeURL
		switch v := v.(type) {
		case templ.SafeURL:
			u = v
		case string:
			u = templ.URL(v)
		default:
			u = templ.URL(toString(v))
		}
		return string(u), nil
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	return toString(v), nil // templ.ResolveAttributeValue: fmt.Sprint, escaped
}

// rawAttr is an attribute value written without escaping: a
// templ.ComponentScript's Call, which templ writes as is.
type rawAttr string

func (n textNode) Render(ctx context.Context, w io.Writer) (err error) {
	if n.v == nil {
		return nil
	}
	var s string
	switch mode, _ := ctx.Value(rawTextKey{}).(int); mode {
	case inScript:
		if s, err = templruntime.ScriptContentOutsideStringLiteral(n.v); err != nil {
			return err
		}
	case inStyle:
		s = strings.ReplaceAll(toString(n.v), "</", `<\/`)
	default:
		s = templ.EscapeString(toString(n.v))
	}
	_, err = io.WriteString(w, s)
	return err
}

func (n fragment) Render(ctx context.Context, w io.Writer) error {
	return renderAll(ctx, w, n)
}

func (n nodes) Render(ctx context.Context, w io.Writer) error {
	if n == nil {
		return nil
	}
	var err error
	n(func(c Node) {
		if err == nil && c != nil {
			err = c.Render(ctx, w)
		}
	})
	return err
}

func (n tryNode) Render(ctx context.Context, w io.Writer) error {
	if n.err != nil {
		return n.err
	}
	if n.n == nil {
		return nil
	}
	return n.n.Render(ctx, w)
}

func (n boundary) Render(ctx context.Context, w io.Writer) error {
	buf := templ.GetBuffer()
	defer templ.ReleaseBuffer(buf)
	if err := renderAll(ctx, buf, n.children); err != nil {
		if n.fallback == nil {
			return nil
		}
		if fb := n.fallback(err); fb != nil {
			return fb.Render(ctx, w)
		}
		return nil
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// String renders n into a string.
func String(ctx context.Context, n Node) (string, error) {
	if n == nil {
		return "", nil
	}
	var b bytes.Buffer
	err := n.Render(ctx, &b)
	return b.String(), err
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

// validName reports whether s is a tag or attribute name the renderer accepts:
// letters, digits, - _ : and ., starting with a letter.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case i > 0 && ('0' <= c && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.'):
		default:
			return false
		}
	}
	return true
}

func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}

// isURLAttr is templ's set of URL attributes (generator's
// isExpressionAttributeValueURL).
func isURLAttr(tag, name string) bool {
	switch tag {
	case "a", "link":
		return name == "href"
	case "form":
		return name == "action"
	case "object":
		return name == "data"
	}
	return false
}
