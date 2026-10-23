package format

import (
	"fmt"
	"strings"
)

// CSS formats a stylesheet: each rule's selectors one a line, its
// declarations one a line as prop: value;, blocks indented two spaces,
// comments kept, at most one blank line kept between rules. Values and
// selectors keep their text, their runs of whitespace one space.
func CSS(src string) (string, error) {
	c := &cssFmt{src: src}
	var b strings.Builder
	i := c.items(&b, 0, 0, false)
	if c.err != nil {
		return "", c.err
	}
	if i < len(src) {
		return "", c.errAt(i, "unexpected }")
	}
	out := strings.TrimLeft(b.String(), "\n")
	if out == "" {
		return "", nil
	}
	return strings.TrimRight(out, "\n") + "\n", nil
}

type cssFmt struct {
	src string
	err error
}

func (c *cssFmt) errAt(i int, msg string) error {
	line := 1 + strings.Count(c.src[:min(i, len(c.src))], "\n")
	return fmt.Errorf("%d:1: %s", line, msg)
}

// items formats the rules, declarations and comments from i up to the }
// ending a block (nested) or the end, and returns where it stopped.
func (c *cssFmt) items(b *strings.Builder, i, depth int, nested bool) int {
	ind := strings.Repeat("  ", depth)
	first := true
	for c.err == nil {
		// The blank lines before the next item.
		j := i
		nl := 0
		for j < len(c.src) && strings.ContainsRune(" \t\r\n\f", rune(c.src[j])) {
			if c.src[j] == '\n' {
				nl++
			}
			j++
		}
		i = j
		if i >= len(c.src) {
			if nested {
				c.err = c.errAt(i, "a { has no }")
			}
			return i
		}
		blank := nl > 1 && !first
		if c.src[i] == '}' {
			if !nested {
				return i
			}
			return i + 1
		}
		if strings.HasPrefix(c.src[i:], "/*") {
			e := strings.Index(c.src[i+2:], "*/")
			if e < 0 {
				c.err = c.errAt(i, "unterminated comment")
				return i
			}
			end := i + 2 + e + 2
			c.line(b, blank, ind+c.src[i:end])
			i, first = end, false
			continue
		}
		// A chunk up to {, ; or }.
		start := i
		parens := 0
		for i < len(c.src) {
			ch := c.src[i]
			if ch == '"' || ch == '\'' {
				i = cssString(c.src, i)
				continue
			}
			if strings.HasPrefix(c.src[i:], "/*") {
				if e := strings.Index(c.src[i+2:], "*/"); e >= 0 {
					i += 2 + e + 2
					continue
				}
			}
			if ch == '(' || ch == '[' {
				parens++
			} else if ch == ')' || ch == ']' {
				parens--
			} else if parens <= 0 && (ch == '{' || ch == ';' || ch == '}') {
				break
			}
			i++
		}
		chunk := strings.TrimSpace(c.src[start:i])
		switch {
		case i >= len(c.src) || c.src[i] == '}':
			// A last declaration without its ;.
			if chunk != "" {
				c.line(b, blank, ind+c.declaration(chunk))
			}
			if i >= len(c.src) {
				if nested {
					c.err = c.errAt(start, "a { has no }")
				}
				return i
			}
			if !nested {
				c.err = c.errAt(i, "unexpected }")
				return i
			}
			return i + 1
		case c.src[i] == ';':
			if chunk != "" {
				c.line(b, blank, ind+c.declaration(chunk))
			}
			i++
		default: // {
			c.line(b, blank, ind+c.prelude(chunk, ind)+" {")
			var inner strings.Builder
			i = c.items(&inner, i+1, depth+1, true)
			if inner.Len() == 0 {
				s := b.String()
				b.Reset()
				b.WriteString(strings.TrimSuffix(s, "\n") + "}\n")
			} else {
				b.WriteString(inner.String())
				b.WriteString(ind + "}\n")
			}
		}
		first = false
	}
	return i
}

func (c *cssFmt) line(b *strings.Builder, blank bool, s string) {
	if blank {
		b.WriteString("\n")
	}
	b.WriteString(s + "\n")
}

// prelude is a rule's selectors, one a line, or an at-rule's prelude.
func (c *cssFmt) prelude(p, ind string) string {
	if strings.HasPrefix(p, "@") {
		return cssSpaces(p)
	}
	sels := splitCSS(p, ',')
	for i, s := range sels {
		sels[i] = cssSpaces(s)
	}
	return strings.Join(sels, ",\n"+ind)
}

// declaration is prop: value, or an at-rule statement (@apply …, @import …).
func (c *cssFmt) declaration(d string) string {
	if strings.HasPrefix(d, "@") {
		return cssSpaces(d) + ";"
	}
	colon := strings.IndexByte(d, ':')
	if colon < 0 {
		return cssSpaces(d) + ";"
	}
	prop := strings.TrimSpace(d[:colon])
	value := strings.TrimSpace(d[colon+1:])
	if !strings.HasPrefix(prop, "--") {
		value = cssSpaces(value)
	}
	return prop + ": " + value + ";"
}

// cssSpaces is s with each run of whitespace outside strings one space.
func cssSpaces(s string) string {
	var b strings.Builder
	space := false
	for i := 0; i < len(s); {
		ch := s[i]
		if ch == '"' || ch == '\'' {
			if space {
				b.WriteByte(' ')
				space = false
			}
			e := cssString(s, i)
			b.WriteString(s[i:e])
			i = e
			continue
		}
		if strings.ContainsRune(" \t\r\n\f", rune(ch)) {
			space = b.Len() > 0
			i++
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}

// cssString is the offset past the string at i.
func cssString(s string, i int) int {
	q := s[i]
	j := i + 1
	for j < len(s) && s[j] != q && s[j] != '\n' {
		if s[j] == '\\' {
			j++
		}
		j++
	}
	return min(j+1, len(s))
}

// splitCSS splits s at sep outside parentheses, brackets and strings.
func splitCSS(s string, sep byte) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '"' || ch == '\'':
			i = cssString(s, i) - 1
		case ch == '(' || ch == '[':
			depth++
		case ch == ')' || ch == ']':
			depth--
		case ch == sep && depth == 0:
			out = append(out, strings.TrimSpace(s[last:i]))
			last = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[last:]))
}
