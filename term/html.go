package term

import (
	"html"
	"strings"

	"github.com/vuka-lang/vuka"
)

// Raw parses h as HTML into the tree at the current position: elements,
// attributes and decoded text, so markup a templ component or Safe produces
// lays out like JSX. Comments, doctypes and processing instructions are
// dropped; script and style contents are kept raw (the layout skips them).
// It never fails: an element left open closes at the end of h, an end tag
// matching nothing h opened is ignored, and a '<' that starts no tag is text.
func (b *builder) Raw(h string) error {
	base := len(b.stack)
	for len(h) > 0 {
		i := strings.IndexByte(h, '<')
		if i < 0 {
			b.Text(html.UnescapeString(h))
			break
		}
		if i > 0 {
			b.Text(html.UnescapeString(h[:i]))
		}
		h = b.markup(h[i:], base)
	}
	b.stack = b.stack[:base]
	return nil
}

// markup consumes the markup starting h (at a '<') and returns the rest.
func (b *builder) markup(h string, base int) string {
	switch {
	case strings.HasPrefix(h, "<!--"):
		return after(h[4:], "-->")
	case len(h) > 1 && (h[1] == '!' || h[1] == '?'):
		return after(h[2:], ">")
	case len(h) > 2 && h[1] == '/' && letter(h[2]):
		name, rest := tagName(h[2:])
		b.end(name, base)
		return after(rest, ">")
	case len(h) > 1 && letter(h[1]):
		name, rest := tagName(h[1:])
		attrs, rest, empty := attributes(rest)
		b.implied(name, base)
		b.open(name, attrs, empty)
		if empty || vuka.Void(name) {
			return rest
		}
		switch name {
		case "script", "style", "textarea", "title":
			j := indexFold(rest, "</"+name)
			if j < 0 {
				j = len(rest)
			}
			if s := rest[:j]; s != "" {
				if name == "textarea" || name == "title" {
					s = html.UnescapeString(s)
				}
				b.Text(s)
			}
			rest = rest[j:]
		}
		return rest
	}
	b.Text("<")
	return h[1:]
}

// end closes the innermost open element named name, if Raw opened one.
func (b *builder) end(name string, base int) {
	for k := len(b.stack) - 1; k >= base; k-- {
		if b.stack[k].tag == name {
			b.stack = b.stack[:k]
			return
		}
	}
}

// implied closes the open elements a start tag of name ends without an end
// tag: a list item, cell, row or option its sibling starts, a paragraph any
// block starts.
func (b *builder) implied(name string, base int) {
	for len(b.stack) > base {
		switch top := b.stack[len(b.stack)-1].tag; {
		case top == name && (name == "li" || name == "option" || name == "tr"),
			(top == "td" || top == "th") && (name == "td" || name == "th" || name == "tr"),
			(top == "dt" || top == "dd") && (name == "dt" || name == "dd"),
			top == "p" && isBlock(name):
			b.stack = b.stack[:len(b.stack)-1]
		default:
			return
		}
	}
}

// attributes reads a start tag's attributes up to its '>', reporting a
// self-closing "/>". Values are decoded; a bare attribute is true.
func attributes(h string) (attrs []vuka.Attr, rest string, empty bool) {
	for {
		h = strings.TrimLeft(h, " \t\n\r\f")
		switch {
		case h == "":
			return attrs, "", false
		case h[0] == '>':
			return attrs, h[1:], false
		case strings.HasPrefix(h, "/>"):
			return attrs, h[2:], true
		case h[0] == '/':
			h = h[1:]
			continue
		}
		i := strings.IndexAny(h[1:], " \t\n\r\f=>/") + 1
		if i == 0 {
			i = len(h)
		}
		name := strings.ToLower(h[:i])
		h = strings.TrimLeft(h[i:], " \t\n\r\f")
		if !strings.HasPrefix(h, "=") {
			attrs = append(attrs, vuka.Attr{Name: name, Value: true})
			continue
		}
		h = strings.TrimLeft(h[1:], " \t\n\r\f")
		var val string
		if h != "" && (h[0] == '"' || h[0] == '\'') {
			j := strings.IndexByte(h[1:], h[0])
			if j < 0 {
				val, h = h[1:], ""
			} else {
				val, h = h[1:j+1], h[j+2:]
			}
		} else {
			j := strings.IndexAny(h, " \t\n\r\f>")
			if j < 0 {
				j = len(h)
			}
			val, h = h[:j], h[j:]
		}
		attrs = append(attrs, vuka.Attr{Name: name, Value: html.UnescapeString(val)})
	}
}

// tagName reads a tag name, lowercased, from the start of h.
func tagName(h string) (string, string) {
	i := strings.IndexAny(h, " \t\n\r\f/>")
	if i < 0 {
		i = len(h)
	}
	return strings.ToLower(h[:i]), h[i:]
}

func letter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// after is h past the first sep, or "" without one.
func after(h, sep string) string {
	if _, rest, ok := strings.Cut(h, sep); ok {
		return rest
	}
	return ""
}

// indexFold is strings.Index ignoring ASCII case in s.
func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
