package format

import (
	"errors"
	"fmt"
	"go/token"
	"html"
	"strings"
)

var errNotGo = errors.New("not a Go expression")

// sameMarkup checks that every JSX expression of out renders what the one in
// the same place in src does: same elements, attributes and Go, and text equal
// after React's whitespace rules.
func sameMarkup(src, out []byte) error {
	a, b := markup(src, token.SEMICOLON, 0), markup(out, token.SEMICOLON, 0)
	for i := range a {
		if i >= len(b) || a[i].sig != b[i].sig {
			line, _ := position(src, a[i].off)
			return fmt.Errorf("%d: internal error: formatting would change what this markup renders; please report it", line)
		}
	}
	if len(a) != len(b) {
		return errors.New("internal error: formatting would change the markup; please report it")
	}
	return nil
}

type markupSig struct {
	off int
	sig string
}

// markup finds the JSX expressions in Go text, as the formatter does, and
// describes what each renders.
func markup(src []byte, prev token.Token, depth int) []markupSig {
	var out []markupSig
	toks := scanTokens(src, 0)
	p := tok{tok: prev}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.tok == token.COMMENT {
			continue
		}
		pt := p
		p = t
		switch {
		case isOpen(t.tok):
			depth++
		case isClose(t.tok):
			depth--
		case t.tok == token.LSS && (depth > 0 || pt.tok == token.ASSIGN) && jsxStarts(pt.tok) && jsxTagAt(src, t.off):
			el, err := parseJSX(src, t.off)
			if err != nil {
				return append(out, markupSig{t.off, "error"})
			}
			out = append(out, markupSig{t.off, sigElem(src, el)})
			toks = append(toks[:i+1:i+1], scanAfterOperand(src, el.end)...)
			p = tok{t.off, token.IDENT, ""}
		}
	}
	return out
}

// sigGo describes Go text: its tokens, and the markup in it.
func sigGo(src []byte, sp span) string {
	text := src[sp.start:sp.end]
	var b strings.Builder
	toks := scanTokens(text, 0)
	depth, p := 1, tok{tok: token.LBRACE}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.tok == token.SEMICOLON && t.lit == "\n" {
			continue
		}
		pt := p
		if t.tok != token.COMMENT {
			p = t
		}
		switch {
		case isOpen(t.tok):
			depth++
		case isClose(t.tok):
			depth--
		case t.tok == token.LSS && jsxStarts(pt.tok) && jsxTagAt(text, t.off):
			if el, err := parseJSX(text, t.off); err == nil {
				b.WriteString("⟨" + sigElem(text, el) + "⟩")
				toks = append(toks[:i+1:i+1], scanAfterOperand(text, el.end)...)
				p = tok{t.off, token.IDENT, ""}
				continue
			}
		}
		b.WriteString(string(text[t.off:min(t.end(), len(text))]) + " ")
	}
	return b.String()
}

func sigElem(src []byte, el *jElem) string {
	var b strings.Builder
	b.WriteString("<" + el.tag + el.targs)
	for _, a := range el.attrs {
		b.WriteString(" " + a.name)
		switch a.kind {
		case 's':
			b.WriteString("=" + html.UnescapeString(a.raw[1:len(a.raw)-1]))
		case 'e':
			b.WriteString("={" + sigGo(src, a.expr) + "}")
		case 'v':
			b.WriteString("={" + sigGo([]byte(a.raw), span{1, len(a.raw) - 1}) + "}")
		}
	}
	if el.self {
		return b.String() + "/>"
	}
	b.WriteString(">" + sigKids(src, el.kids, false) + "</>")
	return b.String()
}

// sigKids describes children as transpile lowers them.
func sigKids(src []byte, kids []jNode, body bool) string {
	var parts []string
	var texts []bool
	for _, k := range kids {
		switch n := k.(type) {
		case *jText:
			if v := textValue(string(src[n.start:n.end])); v != "" {
				parts, texts = append(parts, v), append(texts, true)
			}
		case *jElem:
			parts, texts = append(parts, sigElem(src, n)), append(texts, false)
		case *jHole:
			if n.expr.start >= 0 {
				parts, texts = append(parts, "{"+sigGo(src, span{n.start + 1, n.end - 1})+"}"), append(texts, false)
			}
		case *jBlock:
			parts, texts = append(parts, sigBlock(src, n)), append(texts, false)
		}
	}
	if body && len(parts) > 0 {
		if texts[0] {
			if parts[0] = strings.TrimLeft(parts[0], " "); parts[0] == "" {
				parts, texts = parts[1:], texts[1:]
			}
		}
	}
	if body && len(parts) > 0 {
		if n := len(parts) - 1; texts[n] {
			if parts[n] = strings.TrimRight(parts[n], " "); parts[n] == "" {
				parts = parts[:n]
			}
		}
	}
	for i, t := range texts[:len(parts)] {
		if t {
			parts[i] = "\"" + parts[i] + "\""
		}
	}
	return strings.Join(parts, "|")
}

func sigBlock(src []byte, b *jBlock) string {
	var s strings.Builder
	s.WriteString("{" + b.kw)
	if b.kw == "match" {
		s.WriteString(" " + sigGo(src, b.subj))
		for _, c := range b.cases {
			s.WriteString(" case")
			for _, p := range c.pats {
				s.WriteString(" " + sigGo(src, p))
			}
			if c.guard.start >= 0 {
				s.WriteString(" if " + sigGo(src, c.guard))
			}
			s.WriteString(": " + sigKids(src, c.body, true))
		}
		return s.String() + "}"
	}
	for i, h := range b.heads {
		if h.start >= 0 {
			s.WriteString(" " + sigGo(src, h))
		}
		s.WriteString(" {" + sigKids(src, b.bodies[i], true) + "}")
	}
	return s.String() + "}"
}

// textValue applies React's whitespace rules to text between tags, as
// transpile does: lines are trimmed where they meet a line break, blank lines
// dropped, the rest joined with one space; then HTML entities are decoded.
func textValue(raw string) string {
	lines := strings.Split(raw, "\n")
	last := 0
	for i, l := range lines {
		if strings.Trim(l, " \t\r") != "" {
			last = i
		}
	}
	var b strings.Builder
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", " ")
		if i > 0 {
			l = strings.TrimLeft(l, " \r")
		}
		if i < len(lines)-1 {
			l = strings.TrimRight(l, " \r")
		}
		if l == "" {
			continue
		}
		if i != last {
			l += " "
		}
		b.WriteString(l)
	}
	return html.UnescapeString(b.String())
}
