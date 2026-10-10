package format

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

// The JSX here is read the way transpile reads it, but kept as written: raw
// text, quoted attribute values and the spans of Go expressions, so that it can
// be printed again without changing what it renders.

type span struct{ start, end int }

type jNode any

type jElem struct {
	start, end int
	tag        string // "" for a fragment
	targs      string // a component's type arguments as written: "[User]"
	attrs      []*jAttr
	self       bool
	kids       []jNode
	inner      span // from the end of the opening tag to the closing one
}

type jAttr struct {
	name string
	kind byte   // 's' a quoted string, 'e' an expression, 'v' one with comments, 'b' bare
	raw  string // 's': the value with its quotes; 'v': the braces and all
	expr span   // 'e': the expression inside the braces
}

type jText struct{ start, end int }

// jHole is a {…} child. A hole holding comments is kept as written.
type jHole struct {
	start, end int
	expr       span
	verbatim   bool
}

// jBlock is {for …}, {if … else …} or {match …}.
type jBlock struct {
	start, end int
	kw         string
	heads      []span // for and if: each branch's header; start < 0 for a plain else
	bodies     [][]jNode
	subj       span
	cases      []*jCase
}

type jCase struct {
	def   bool
	pats  []span
	guard span // start < 0: none
	body  []jNode
}

type jsxError struct {
	off int
	msg string
}

type jsxParser struct{ src []byte }

func (p *jsxParser) fail(off int, format string, args ...any) {
	panic(jsxError{off, fmt.Sprintf(format, args...)})
}

// parseJSX reads the JSX expression starting at off.
func parseJSX(src []byte, off int) (el *jElem, err *jsxError) {
	p := &jsxParser{src: src}
	defer func() {
		if r := recover(); r != nil {
			je, ok := r.(jsxError)
			if !ok {
				panic(r)
			}
			el, err = nil, &je
		}
	}()
	return p.element(off), nil
}

// jsxStarts reports whether an operand may start after a token of kind prev,
// so that a < there opens JSX rather than comparing.
func jsxStarts(prev token.Token) bool {
	switch prev {
	case token.RETURN, token.ASSIGN, token.DEFINE, token.LPAREN, token.COMMA, token.LBRACE,
		token.COLON, token.LBRACK, token.SEMICOLON, token.ARROW:
		return true
	}
	return false
}

func jsxTagAt(src []byte, off int) bool {
	return off+1 < len(src) && (isLetter(src[off+1]) || src[off+1] == '>')
}

func isComponent(tag string) bool {
	return tag != "" && ('A' <= tag[0] && tag[0] <= 'Z' || strings.Contains(tag, "."))
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isNameByte(c byte) bool {
	return isLetter(c) || '0' <= c && c <= '9' || c == '_' || c == '-' || c == ':' || c == '.'
}

func isIdentByte(c byte) bool { return isLetter(c) || '0' <= c && c <= '9' || c == '_' }

func (p *jsxParser) at(i int) byte {
	if i < len(p.src) {
		return p.src[i]
	}
	return 0
}

func (p *jsxParser) space(i int) int {
	for i < len(p.src) && strings.IndexByte(" \t\r\n", p.src[i]) >= 0 {
		i++
	}
	return i
}

func (p *jsxParser) name(i int) (string, int) {
	j := i
	for j < len(p.src) && isNameByte(p.src[j]) {
		j++
	}
	return string(p.src[i:j]), j
}

func (p *jsxParser) element(off int) *jElem {
	el := &jElem{start: off}
	i := off + 1
	if p.at(i) == '>' {
		el.kids, i = p.children(i+1, el, 0, false)
		el.inner = span{off + 2, i}
		if !strings.HasPrefix(string(p.src[i:min(i+3, len(p.src))]), "</>") {
			p.fail(i, "expected </> to close the fragment")
		}
		el.end = i + 3
		return el
	}
	el.tag, i = p.name(i)
	if c := el.tag[len(el.tag)-1]; c == '.' || c == '-' || c == ':' {
		p.fail(off+1, "bad tag name %s", el.tag)
	}
	if isComponent(el.tag) && p.at(i) == '[' { // type arguments: <List[User] …>
		start := i
		for d := 0; i == start || d > 0; i++ {
			switch p.at(i) {
			case 0:
				p.fail(off, "unclosed [ in <%s>", el.tag)
			case '[':
				d++
			case ']':
				d--
			}
		}
		el.targs = string(p.src[start:i])
	}
	for {
		i = p.space(i)
		switch c := p.at(i); {
		case c == 0:
			p.fail(off, "unclosed <%s>", el.tag)
		case c == '/' && p.at(i+1) == '>':
			el.self, el.end = true, i+2
			return el
		case c == '>':
			el.inner.start = i + 1
			el.kids, i = p.children(i+1, el, 0, false)
			el.inner.end = i
			j := p.space(i + 2)
			name, k := p.name(j)
			k = p.space(k)
			if name != el.tag || p.at(k) != '>' {
				p.fail(i, "expected </%s> to close <%s>", el.tag, el.tag)
			}
			el.end = k + 1
			return el
		case isLetter(c) || c == '_':
			a := &jAttr{kind: 'b'}
			a.name, i = p.name(i)
			if j := p.space(i); p.at(j) == '=' {
				i = p.space(j + 1)
				switch q := p.at(i); q {
				case '"', '\'':
					end := strings.IndexByte(string(p.src[i+1:]), q)
					if end < 0 {
						p.fail(i, "unterminated attribute value")
					}
					a.kind, a.raw = 's', string(p.src[i:i+end+2])
					i += end + 2
				case '{':
					open := i
					var ok, comment bool
					a.kind = 'e'
					a.expr, i, ok, comment = p.expr(i)
					if !ok {
						p.fail(open, "attribute %s has an empty expression", a.name)
					}
					if comment {
						a.kind, a.raw = 'v', string(p.src[open:i])
					}
				default:
					p.fail(i, "attribute %s needs a value", a.name)
				}
			}
			el.attrs = append(el.attrs, a)
		default:
			p.fail(i, "unexpected %q in <%s>", c, el.tag)
		}
	}
}

// children reads children up to what ends them: a closing tag for an element,
// a } for a block's body (el nil), or the next case in a match.
func (p *jsxParser) children(i int, el *jElem, open int, inMatch bool) ([]jNode, int) {
	var kids []jNode
	for {
		c := p.at(i)
		switch {
		case c == 0:
			if el != nil {
				p.fail(el.start, "unclosed element")
			}
			p.fail(open, "unclosed block")
		case c == '<' && p.at(i+1) == '/':
			if el == nil {
				p.fail(i, "unexpected closing tag in a block")
			}
			return kids, i
		case c == '<':
			if !jsxTagAt(p.src, i) {
				p.fail(i, "expected a tag name after <")
			}
			n := p.element(i)
			kids = append(kids, n)
			i = n.end
		case c == '{':
			var n jNode
			n, i = p.hole(i)
			kids = append(kids, n)
		case c == '}':
			if el == nil {
				return kids, i
			}
			p.fail(i, "unexpected } in an element")
		case inMatch && p.caseAt(i):
			return kids, i
		default:
			j := i
			for j < len(p.src) && strings.IndexByte("<{}", p.src[j]) < 0 && !(inMatch && j > i && p.caseAt(j)) {
				j++
			}
			kids = append(kids, &jText{i, j})
			i = j
		}
	}
}

func (p *jsxParser) caseAt(i int) bool {
	if i > 0 && isIdentByte(p.src[i-1]) {
		return false
	}
	rest := p.src[i:]
	for _, kw := range []string{"case", "default"} {
		if strings.HasPrefix(string(rest[:min(len(rest), len(kw))]), kw) && !isIdentByte(p.at(i+len(kw))) {
			return kw == "case" || p.at(p.space(i+len(kw))) == ':'
		}
	}
	return false
}

type tok struct {
	off int
	tok token.Token
	lit string
}

func (t tok) end() int {
	if t.lit != "" && t.tok != token.SEMICOLON {
		return t.off + len(t.lit)
	}
	return t.off + len(t.tok.String())
}

func trivia(t tok) bool { return t.tok == token.COMMENT || t.tok == token.SEMICOLON && t.lit == "\n" }

func isOpen(t token.Token) bool  { return t == token.LPAREN || t == token.LBRACK || t == token.LBRACE }
func isClose(t token.Token) bool { return t == token.RPAREN || t == token.RBRACK || t == token.RBRACE }

type goScan struct {
	src  []byte
	base int
	s    scanner.Scanner
	f    *token.File
}

func newGoScan(src []byte, off int) *goScan {
	g := &goScan{src: src}
	g.reset(off)
	return g
}

func (g *goScan) reset(off int) {
	g.base = off
	g.f = token.NewFileSet().AddFile("", -1, len(g.src)-off)
	g.s.Init(g.f, g.src[off:], func(token.Position, string) {}, scanner.ScanComments)
}

func (g *goScan) next() tok {
	pos, t, lit := g.s.Scan()
	return tok{g.base + g.f.Offset(pos), t, lit}
}

func (g *goScan) significant() tok {
	for {
		if t := g.next(); !trivia(t) {
			return t
		}
	}
}

// goUntil reads Go tokens from first until stop matches one at depth zero,
// returning the first and last tokens before it, the stopping one, and whether
// a comment came between.
func (p *jsxParser) goUntil(g *goScan, first tok, prev token.Token, open int, stop func(tok) bool) (start, last, at tok, comment bool) {
	start.off = -1
	d := 0
	for t := first; ; t = g.next() {
		switch {
		case t.tok == token.EOF:
			p.fail(open, "unclosed {")
		case t.tok == token.COMMENT:
			comment = true
			continue
		case trivia(t):
			continue
		case d == 0 && stop(t):
			return start, last, t, comment
		case isOpen(t.tok):
			d++
		case isClose(t.tok):
			d--
		case t.tok == token.LSS && jsxStarts(prev) && jsxTagAt(p.src, t.off):
			end := p.element(t.off).end
			t = tok{t.off, token.IDENT, string(p.src[t.off:end])}
			g.reset(end)
		}
		if start.off < 0 {
			start = t
		}
		last, prev = t, t.tok
	}
}

// expr reads the Go expression in the braces at i.
func (p *jsxParser) expr(i int) (sp span, end int, ok, comment bool) {
	g := newGoScan(p.src, i+1)
	start, last, rb, comment := p.goUntil(g, g.next(), token.LBRACE, i, func(t tok) bool { return t.tok == token.RBRACE })
	if start.off < 0 {
		return span{-1, -1}, rb.off + 1, false, comment
	}
	return span{start.off, last.end()}, rb.off + 1, true, comment
}

func (p *jsxParser) hole(i int) (jNode, int) {
	g := newGoScan(p.src, i+1)
	t := g.significant()
	switch {
	case t.tok == token.FOR || t.tok == token.IF:
		n, end := p.block(g, t, i)
		n.start, n.end = i, end
		return n, end
	case t.tok == token.IDENT && t.lit == "match":
		save := g.s
		if n := g.significant(); exprStart(n.tok) {
			b, end := p.matchBlock(g, t, n, i)
			b.start, b.end = i, end
			return b, end
		}
		g.s = save
	}
	sp, end, ok, comment := p.expr(i)
	return &jHole{start: i, end: end, expr: sp, verbatim: !ok || comment}, end
}

func exprStart(t token.Token) bool {
	switch t {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING,
		token.LPAREN, token.MUL, token.AND, token.NOT, token.SUB, token.ADD, token.XOR, token.FUNC, token.LBRACK:
		return true
	}
	return false
}

func (p *jsxParser) block(g *goScan, kw tok, open int) (*jBlock, int) {
	b := &jBlock{kw: kw.tok.String()}
	prev, headStart, kwStart := kw.tok, kw.end(), kw.off
	for {
		head := span{-1, -1}
		var h int
		if prev == token.ELSE {
			h = g.significant().off + 1
		} else {
			lb := p.header(g, prev, kwStart, open)
			head, h = span{headStart, lb}, lb+1
		}
		b.heads = append(b.heads, head)
		kids, rb := p.children(h, nil, open, false)
		b.bodies = append(b.bodies, kids)
		g.reset(rb + 1)
		next := g.significant()
		if kw.tok == token.IF && next.tok == token.ELSE {
			prev = token.ELSE
			save := g.s
			switch after := g.significant(); after.tok {
			case token.IF:
				prev, headStart, kwStart = token.IF, after.end(), after.off
			case token.LBRACE:
				g.s = save
			default:
				p.fail(after.off, "expected if or { after else")
			}
			continue
		}
		if next.tok != token.RBRACE {
			p.fail(next.off, "expected } to close the block")
		}
		return b, next.off + 1
	}
}

// header reads a for or if header from the keyword at head to its body's {,
// returning the {'s offset. A { that leaves the header unparsable opens a
// composite literal (range []string{"a"} {) and is skipped, as transpile does.
func (p *jsxParser) header(g *goScan, kw token.Token, head, open int) int {
	_, _, lb, _ := p.goUntil(g, g.next(), kw, open, func(t tok) bool { return t.tok == token.LBRACE })
	first := lb.off
	for lb.tok == token.LBRACE && !headerParses(p.src[head:lb.off]) {
		for depth := 1; depth > 0; {
			switch t := g.next(); t.tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
			case token.EOF:
				return first
			}
		}
		_, _, lb, _ = p.goUntil(g, g.next(), kw, open, func(t tok) bool { return t.tok == token.LBRACE })
	}
	if lb.tok != token.LBRACE {
		return first
	}
	return lb.off
}

func headerParses(h []byte) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+strings.TrimSpace(string(h))+" {}\n}\n", 0)
	return err == nil
}

func (p *jsxParser) matchBlock(g *goScan, kw, first tok, open int) (*jBlock, int) {
	subj, last, lb, _ := p.goUntil(g, first, kw.tok, open, func(t tok) bool { return t.tok == token.LBRACE })
	b := &jBlock{kw: "match", subj: span{subj.off, last.end()}}
	i := lb.off + 1
	for {
		g.reset(i)
		t := g.significant()
		switch t.tok {
		case token.RBRACE:
			if len(b.cases) == 0 {
				p.fail(kw.off, "a match needs at least one case")
			}
			g.reset(t.off + 1)
			end := g.significant()
			if end.tok != token.RBRACE {
				p.fail(end.off, "expected } to close the block")
			}
			return b, end.off + 1
		case token.CASE, token.DEFAULT:
			c, colon := p.caseHeader(g, t)
			c.body, i = p.children(colon+1, nil, open, true)
			b.cases = append(b.cases, c)
		default:
			p.fail(t.off, "expected case or default in match")
		}
	}
}

func (p *jsxParser) caseHeader(g *goScan, kw tok) (*jCase, int) {
	c := &jCase{def: kw.tok == token.DEFAULT, guard: span{-1, -1}}
	d, patStart := 0, -1
	wantPat, wantGuard := kw.tok == token.CASE, false
	var prev tok
	for {
		h := g.next()
		switch {
		case h.tok == token.EOF:
			p.fail(kw.off, "unterminated case in match")
		case trivia(h):
			continue
		}
		if (wantPat || wantGuard) && d == 0 && (h.tok == token.COMMA || h.tok == token.COLON || h.tok == token.IF) {
			p.fail(h.off, "expected a pattern or a guard")
		}
		if wantPat {
			patStart, wantPat = h.off, false
		}
		if wantGuard {
			c.guard.start, wantGuard = h.off, false
		}
		switch {
		case isOpen(h.tok):
			d++
		case isClose(h.tok):
			d--
		case d == 0 && h.tok == token.COMMA && c.guard.start < 0:
			c.pats = append(c.pats, span{patStart, prev.end()})
			wantPat = true
		case d == 0 && h.tok == token.IF && c.guard.start < 0:
			if patStart >= 0 {
				c.pats = append(c.pats, span{patStart, prev.end()})
				patStart = -1
			}
			wantGuard = true
		case d == 0 && h.tok == token.COLON:
			if c.guard.start >= 0 {
				c.guard.end = prev.end()
			} else if patStart >= 0 {
				c.pats = append(c.pats, span{patStart, prev.end()})
			}
			if !c.def && len(c.pats) == 0 {
				p.fail(kw.off, "a case needs a pattern")
			}
			return c, h.off
		}
		prev = h
	}
}
