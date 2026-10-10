package vuka

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"
	templruntime "github.com/a-h/templ/runtime"
)

// renderHTML is every built-in Node's Render: a walk into an HTMLRenderer,
// buffered like templ's generated code.
func renderHTML(ctx context.Context, w io.Writer, n Node) (err error) {
	buf, existing := templruntime.GetBuffer(w)
	if !existing {
		defer func() {
			if ferr := templruntime.ReleaseBuffer(buf); err == nil {
				err = ferr
			}
		}()
	}
	h := HTMLRenderer{ctx: ctx, w: buf}
	return Walk(ctx, n, &h)
}

// HTMLRenderer is the Renderer writing HTML: what Render does. Escaping and
// sanitizing are templ's (see Attr and Element).
type HTMLRenderer struct {
	ctx  context.Context
	w    io.StringWriter
	mode uint8
	err  error
}

const (
	inHTML uint8 = iota
	inScript
	inStyle
)

// NewHTMLRenderer is an HTMLRenderer writing to w, unbuffered; ctx renders
// Node attribute values.
func NewHTMLRenderer(ctx context.Context, w io.Writer) *HTMLRenderer {
	sw, ok := w.(io.StringWriter)
	if !ok {
		sw = stringWriter{w}
	}
	return &HTMLRenderer{ctx: ctx, w: sw}
}

type stringWriter struct{ io.Writer }

func (w stringWriter) WriteString(s string) (int, error) { return io.WriteString(w.Writer, s) }

func (h *HTMLRenderer) write(s string) {
	if h.err == nil {
		_, h.err = h.w.WriteString(s)
	}
}

func (h *HTMLRenderer) Open(e *Element) error {
	if !validName(e.Tag) {
		return fmt.Errorf("vuka: invalid tag name %q", e.Tag)
	}
	if e.Tag == "html" {
		h.write("<!DOCTYPE html>")
	}
	h.write("<")
	h.write(e.Tag)
	if err := h.attrs(e); err != nil {
		return err
	}
	h.write(">")
	switch e.Tag {
	case "script":
		h.mode = inScript
	case "style":
		h.mode = inStyle
	}
	return h.err
}

func (h *HTMLRenderer) Close(e *Element) error {
	h.mode = inHTML
	h.write("</")
	h.write(e.Tag)
	h.write(">")
	return h.err
}

func (h *HTMLRenderer) Text(s string) error {
	switch h.mode {
	case inScript:
		js, err := templruntime.ScriptContentOutsideStringLiteral(s)
		if err != nil {
			return err
		}
		h.write(js)
	case inStyle:
		h.write(strings.ReplaceAll(s, "</", `<\/`))
	default:
		h.write(templ.EscapeString(s))
	}
	return h.err
}

func (h *HTMLRenderer) Raw(html string) error {
	h.write(html)
	return h.err
}

func (h *HTMLRenderer) Opaque(ctx context.Context, n Node) error {
	if h.err != nil {
		return h.err
	}
	w, ok := h.w.(io.Writer)
	if !ok {
		w = writerOf{h.w}
	}
	return n.Render(ctx, w)
}

type writerOf struct{ io.StringWriter }

func (w writerOf) Write(p []byte) (int, error) { return w.WriteString(string(p)) }

// attrs writes the attributes as templ's generated code does: each value
// resolved the way templ resolves an attribute expression of that name, then
// escaped by templ.EscapeString.
func (h *HTMLRenderer) attrs(e *Element) error {
	for _, a := range e.Attrs {
		name := a.Name
		if eh, ok := a.Value.(EventHandler); ok {
			if _, script := eh.Fn.(templ.ComponentScript); !script {
				if err := h.handlerAttr(name, eh); err != nil {
					return err
				}
				continue
			}
			a.Value = eh.Fn
		}
		switch name {
		case "key":
			continue
		case "className":
			name = "class"
		case "htmlFor":
			name = "for"
		}
		if !validName(name) {
			return fmt.Errorf("vuka: invalid attribute name %q", a.Name)
		}
		v, kind, err := attrValue(h.ctx, e.Tag, name, a.Value)
		if err != nil {
			return err
		}
		switch kind {
		case attrOmit:
		case attrBare:
			h.write(" ")
			h.write(name)
		default:
			if kind == attrEscaped {
				v = templ.EscapeString(v)
			}
			h.write(" ")
			h.write(name)
			h.write(`="`)
			h.write(v)
			h.write(`"`)
		}
	}
	return nil
}

// What attrValue resolved an attribute to.
const (
	attrOmit    uint8 = iota
	attrBare          // true: the name alone
	attrEscaped       // a value to escape
	attrRaw           // a templ.ComponentScript's Call, which templ writes as is
)

// attrValue resolves an attribute's value the way templ resolves an attribute
// expression of that name.
func attrValue(ctx context.Context, tag, name string, v any) (string, uint8, error) {
	switch v := v.(type) {
	case nil:
		return "", attrOmit, nil
	case bool:
		if v {
			return "", attrBare, nil
		}
		return "", attrOmit, nil
	case string:
		switch {
		case name == "style":
		case isURLAttr(tag, name):
			return string(templ.URL(v)), attrEscaped, nil
		default:
			return v, attrEscaped, nil
		}
	case templ.ComponentScript: // a Node too
		return v.Call, attrRaw, nil
	case Node:
		s, err := String(ctx, v)
		return s, attrEscaped, err
	}
	switch {
	case name == "class":
		return templ.Classes(v).String(), attrEscaped, nil
	case name == "style":
		if s, ok := v.(Style); ok {
			v = map[string]string(s)
		}
		// Sanitized and escaped, then escaped once more when written, as templ does.
		s, err := templruntime.SanitizeStyleAttributeValues(v)
		return s, attrEscaped, err
	case isURLAttr(tag, name):
		if u, ok := v.(templ.SafeURL); ok {
			return string(u), attrEscaped, nil
		}
		return string(templ.URL(toString(v))), attrEscaped, nil
	}
	return toString(v), attrEscaped, nil // templ.ResolveAttributeValue: fmt.Sprint, escaped
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
