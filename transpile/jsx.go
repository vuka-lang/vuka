package transpile

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"html"
	"strconv"
	"strings"
)

// JSX is an expression in a function body: `<div className="card">{u.Name}</div>`.
// It is read here, from the source bytes (its text isn't Go tokens), and lowered
// to calls of the runtime: El, Text, Child, Fragment and Nodes, plus the
// components' own functions once their types are known.
//
// The lowering keeps every Go expression the JSX holds where it is, and writes
// only the gaps between them, each with as many newlines as the source it
// replaces, so lines, columns, errors, hover and completion map exactly:
//
//	<div id={u.ID}>{u.Name}</div>   →   vuka.El("div", []vuka.Attr{{Name: "id", Value: u.ID}, }, vuka.Child(u.Name), )

// jsxStarts reports whether an operand may start after a token of kind prev,
// so that a `<` there opens JSX rather than comparing.
func jsxStarts(prev token.Token) bool {
	switch prev {
	case token.RETURN, token.ASSIGN, token.DEFINE, token.LPAREN, token.COMMA, token.LBRACE,
		token.COLON, token.LBRACK, token.SEMICOLON, token.ARROW:
		return true
	}
	return false
}

// jsxTagAt reports whether the `<` at off is followed by a tag name or `>`.
func jsxTagAt(src []byte, off int) bool {
	return off+1 < len(src) && (isLetter(src[off+1]) || src[off+1] == '>')
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isNameByte(c byte) bool {
	return isLetter(c) || '0' <= c && c <= '9' || c == '_' || c == '-' || c == ':' || c == '.'
}

func isIdentByte(c byte) bool { return isLetter(c) || '0' <= c && c <= '9' || c == '_' }

// jsxTree is a JSX expression not inside another one's markup; JSX inside one of
// its Go expressions is a tree of its own.
type jsxTree struct {
	start, end int
	root       *jsxElem
	comps      []*jsxComp
	events     []*jsxEvent
	done       bool // its edits are in fixed
}

// jsxEvent is an on… attribute with an expression on an element, whose type is
// checked once known.
type jsxEvent struct {
	attr    *jsxAttr
	tag     string
	checked bool
}

type jsxNode interface{}

type jsxElem struct {
	start, end int // from < to just after the last >
	tag        string
	tagEnd     int
	frag       bool
	attrs      []*jsxAttr
	kids       []jsxNode
	comp       *jsxComp // for a component's tag
}

func (el *jsxElem) open() string { return "<" + el.tag + ">" }

type jsxAttr struct {
	name  string
	off   int
	kind  byte   // 's' a string, 'e' an expression, 'b' bare (true)
	val   string // the decoded string
	expr  span
	event bool // an element's event handler: onClick={…}
}

type jsxText struct {
	start int
	val   string
}

// jsxHole is a {expr} child.
type jsxHole struct{ expr span }

// jsxBlock is a {for …}, {if …} or {match …} child. For and if keep their
// headers (`for … {`, `else if c {`) in place; a match is lowered by match.go.
type jsxBlock struct {
	heads  []span
	bodies [][]jsxNode
	m      *matchStmt
	cases  [][]jsxNode
}

type jsxError struct {
	off int
	msg string
}

type jsxParser struct {
	f       *fileState
	src     []byte
	errs    *ErrorList
	trees   []*jsxTree
	matches []*matchStmt
	cur     *jsxTree
	goToks  [][]tok // each Go expression's tokens, its JSX read as one
}

func (p *jsxParser) fail(off int, format string, args ...any) {
	panic(jsxError{off, fmt.Sprintf(format, args...)})
}

// parseJSX reads the JSX expression at off and every one nested in it, and
// returns where it ends and the tokens of the Go expressions it holds. A
// malformed one is reported and nothing is kept.
func (f *fileState) parseJSX(off int, errs *ErrorList) (end int, goToks [][]tok, ok bool) {
	p := &jsxParser{f: f, src: f.src, errs: errs}
	defer func() {
		if r := recover(); r != nil {
			je, isJSX := r.(jsxError)
			if !isJSX {
				panic(r)
			}
			errs.add(f.at(je.off), "%s", je.msg)
			end, goToks, ok = 0, nil, false
		}
	}()
	end = p.tree(off)
	for _, m := range p.matches {
		m.n = len(f.matches) + 1
		f.matches = append(f.matches, m)
	}
	for _, t := range p.trees {
		f.jsx = append(f.jsx, t)
		if len(t.comps) == 0 {
			f.fixed = append(f.fixed, f.jsxEdits(t)...)
			t.done = true
		}
	}
	return end, p.goToks, true
}

func (p *jsxParser) tree(off int) int {
	t := &jsxTree{start: off}
	outer := p.cur
	p.cur = t
	t.root = p.element(off)
	t.end = t.root.end
	p.cur = outer
	p.trees = append(p.trees, t)
	return t.end
}

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

// isComponent reports whether a tag names a Go function: Capitalized, or pkg.Name.
func isComponent(tag string) bool {
	return tag != "" && ('A' <= tag[0] && tag[0] <= 'Z' || strings.Contains(tag, "."))
}

// isEvent reports whether an attribute is an event handler: onclick, onClick.
func isEvent(name string) bool {
	return len(name) > 2 && name[:2] == "on" && isLetter(name[2])
}

func (p *jsxParser) element(off int) *jsxElem {
	el := &jsxElem{start: off}
	i := off + 1
	if p.at(i) == '>' {
		el.frag = true
		el.kids, i = p.children(i+1, el, 0, false)
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
	comp := isComponent(el.tag)
	if comp && p.at(i) == '[' { // type arguments: <List[User] …>
		for d := 0; i == el.start+1+len(el.tag) || d > 0; i++ {
			switch p.at(i) {
			case 0:
				p.fail(off, "unclosed [ in <%s>", el.tag)
			case '[':
				d++
			case ']':
				d--
			}
		}
	}
	el.tagEnd = i
	if comp {
		el.comp = &jsxComp{el: el}
		p.cur.comps = append(p.cur.comps, el.comp)
	}
	for {
		i = p.space(i)
		switch c := p.at(i); {
		case c == 0:
			p.fail(off, "unclosed <%s>", el.tag)
		case c == '/' && p.at(i+1) == '>':
			el.end = i + 2
			return el
		case c == '>':
			el.kids, i = p.children(i+1, el, 0, false)
			j := p.space(i + 2)
			name, k := p.name(j)
			k = p.space(k)
			if name != el.tag || p.at(k) != '>' {
				found := "</" + name + ">"
				if p.at(k) != '>' {
					found = string(p.src[i:k])
				}
				p.fail(i, "expected </%s> to close <%s> at line %d, found %s", el.tag, el.tag, p.f.at(off).Line, found)
			}
			el.end = k + 1
			return el
		case isLetter(c) || c == '_':
			a := &jsxAttr{off: i}
			a.name, i = p.name(i)
			i = p.space(i)
			if p.at(i) != '=' {
				a.kind = 'b'
			} else {
				i = p.space(i + 1)
				switch q := p.at(i); q {
				case '"', '\'':
					end := strings.IndexByte(string(p.src[i+1:]), q)
					if end < 0 {
						p.fail(i, "unterminated attribute value")
					}
					a.kind, a.val = 's', html.UnescapeString(string(p.src[i+1:i+1+end]))
					i += end + 2
				case '{':
					var ok bool
					a.kind = 'e'
					a.expr, i, ok = p.expr(i)
					if !ok {
						p.fail(a.off, "attribute %s has an empty expression", a.name)
					}
				default:
					p.fail(i, "attribute %s needs a value: %s=\"…\" or %s={…}", a.name, a.name, a.name)
				}
			}
			if a.kind == 'e' && !comp && isEvent(a.name) {
				a.event = true
				p.cur.events = append(p.cur.events, &jsxEvent{attr: a, tag: el.tag})
			}
			el.attrs = append(el.attrs, a)
		default:
			p.fail(i, "unexpected %q in <%s>", c, el.tag)
		}
	}
}

// children reads children up to what ends them: a closing tag for an element
// (el), a } for the body of the block opened at open (el nil), or the next case
// in a match.
func (p *jsxParser) children(i int, el *jsxElem, open int, inMatch bool) ([]jsxNode, int) {
	var kids []jsxNode
	for {
		c := p.at(i)
		switch {
		case c == 0:
			if el != nil {
				p.fail(el.start, "unclosed %s", el.open())
			}
			p.fail(open, "unclosed block")
		case c == '<' && p.at(i+1) == '/':
			if el == nil {
				p.fail(i, "unexpected closing tag in a block; close the block's } first")
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
			var n jsxNode
			n, i = p.hole(i)
			if n != nil {
				kids = append(kids, n)
			}
		case c == '}':
			if el == nil {
				return trimBody(kids), i
			}
			p.fail(el.start, "unclosed %s: found } at line %d (write {\"}\"} for the text)", el.open(), p.f.at(i).Line)
		case inMatch && p.caseAt(i):
			return trimBody(kids), i
		case el == nil && p.nestedAt(p.space(i)):
			j := p.space(i)
			if v := jsxTextValue(string(p.src[i:j])); v != "" {
				kids = append(kids, &jsxText{start: i, val: v})
			}
			var n jsxNode
			n, i = p.nested(j)
			kids = append(kids, n)
		default:
			j := i
			for j < len(p.src) && strings.IndexByte("<{}", p.src[j]) < 0 && !(inMatch && j > i && p.caseAt(j)) {
				j++
			}
			if v := jsxTextValue(string(p.src[i:j])); v != "" {
				kids = append(kids, &jsxText{start: i, val: v})
			}
			i = j
		}
	}
}

// nestedAt reports whether a block's body holds a nested block at i, written
// without braces: if or for and a space or (, or match and an expression.
func (p *jsxParser) nestedAt(i int) bool {
	if i > 0 && isIdentByte(p.src[i-1]) {
		return false
	}
	for _, kw := range []string{"if", "for"} {
		if strings.HasPrefix(string(p.src[i:min(i+len(kw), len(p.src))]), kw) {
			c := p.at(i + len(kw))
			return c == ' ' || c == '\t' || c == '('
		}
	}
	if !strings.HasPrefix(string(p.src[i:min(i+5, len(p.src))]), "match") || isIdentByte(p.at(i+5)) {
		return false
	}
	g := newGoScan(p.src, i+5)
	return exprStart(g.significant().tok)
}

// nested reads the brace-less block at i in a block's body, as if it were
// in braces: {if …} as `if …`.
func (p *jsxParser) nested(i int) (jsxNode, int) {
	g := newGoScan(p.src, i)
	t := g.significant()
	if t.tok == token.IDENT {
		return p.matchBlock(g, t, g.significant(), i, true)
	}
	return p.block(g, t, i, true)
}

func exprStart(t token.Token) bool {
	switch t {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING,
		token.LPAREN, token.MUL, token.AND, token.NOT, token.SUB, token.ADD, token.XOR, token.FUNC, token.LBRACK:
		return true
	}
	return false
}

// trimBody drops the whitespace a block's body starts and ends with on the
// lines of its braces: {for … { <li/> }} adds one child.
func trimBody(kids []jsxNode) []jsxNode {
	if len(kids) > 0 {
		if t, ok := kids[0].(*jsxText); ok {
			if t.val = strings.TrimLeft(t.val, " "); t.val == "" {
				kids = kids[1:]
			}
		}
	}
	if len(kids) > 0 {
		if t, ok := kids[len(kids)-1].(*jsxText); ok {
			if t.val = strings.TrimRight(t.val, " "); t.val == "" {
				kids = kids[:len(kids)-1]
			}
		}
	}
	return kids
}

// caseAt reports whether a match body's next case starts at i.
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

// goScan scans Go tokens from an offset of src.
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

// resume scans on from off, just after an operand (JSX), where a newline ends
// a statement as it would after the ) the scanner is shown in its place.
func (g *goScan) resume(off int) {
	buf := append([]byte{')'}, g.src[off:]...)
	g.base = off - 1
	g.f = token.NewFileSet().AddFile("", -1, len(buf))
	g.s.Init(g.f, buf, func(token.Position, string) {}, scanner.ScanComments)
	g.s.Scan()
}

func (g *goScan) next() tok {
	pos, t, lit := g.s.Scan()
	return tok{g.base + g.f.Offset(pos), t, lit}
}

// significant is the next token that isn't a comment or an inserted semicolon.
func (g *goScan) significant() tok {
	for {
		t := g.next()
		if !trivia(t) {
			return t
		}
	}
}

// goUntil reads Go tokens from g, after first, until stop matches one at depth
// zero, and returns the first and last tokens before it and the stopping one.
// JSX in operand position is read as a tree of its own. The tokens read are
// kept for the scan for ? and match.
func (p *jsxParser) goUntil(g *goScan, first tok, prev token.Token, open int, stop func(tok) bool) (start, last, at tok) {
	start.off = -1
	d := 0
	var toks []tok
	for t := first; ; t = g.next() {
		switch {
		case t.tok == token.EOF:
			p.fail(open, "unclosed {")
		case t.tok == token.COMMENT:
			continue
		case trivia(t):
			toks = append(toks, t)
			continue
		case d == 0 && stop(t):
			p.goToks = append(p.goToks, toks)
			return start, last, t
		case isOpen(t.tok):
			d++
		case isClose(t.tok):
			d--
		case t.tok == token.LSS && jsxStarts(prev) && jsxTagAt(p.src, t.off):
			end := p.tree(t.off)
			t = tok{t.off, token.IDENT, string(p.src[t.off:end])}
			g.resume(end)
		}
		toks = append(toks, t)
		if start.off < 0 {
			start = t
		}
		last, prev = t, t.tok
	}
}

// expr reads the Go expression in the braces at i. It returns its span, where
// the braces end, and whether there is an expression (not just a comment).
func (p *jsxParser) expr(i int) (span, int, bool) {
	g := newGoScan(p.src, i+1)
	start, last, rb := p.goUntil(g, g.next(), token.LBRACE, i, func(t tok) bool { return t.tok == token.RBRACE })
	if start.off < 0 {
		return span{}, rb.off + 1, false
	}
	return span{start.off, last.end()}, rb.off + 1, true
}

// hole reads a {…} child: an expression, a comment, or a block.
func (p *jsxParser) hole(i int) (jsxNode, int) {
	g := newGoScan(p.src, i+1)
	t := g.significant()
	switch {
	case t.tok == token.FOR || t.tok == token.IF:
		return p.block(g, t, i, false)
	case t.tok == token.IDENT && t.lit == "match":
		save := g.s
		if n := g.significant(); exprStart(n.tok) {
			return p.matchBlock(g, t, n, i, false)
		}
		g.s = save
	}
	sp, end, ok := p.expr(i)
	if !ok {
		return nil, end
	}
	return &jsxHole{sp}, end
}

// header reads a for or if header starting at head to its body's {, returning
// the offset just after the {. A { that leaves the header unparsable opens a
// composite literal (range []string{"a"} {), so it is skipped like Go would.
func (p *jsxParser) header(g *goScan, kw token.Token, head, open int, bare bool) int {
	_, _, lb := p.goUntil(g, g.next(), kw, open, braceAt)
	p.bodyOpens(lb, head, bare)
	first := lb.off
	for lb.tok == token.LBRACE && !headerParses(p.src[head:lb.off]) {
		for depth := 1; depth > 0; {
			switch t := g.next(); t.tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
			case token.EOF:
				return first + 1
			}
		}
		_, _, lb = p.goUntil(g, g.next(), kw, open, braceAt)
		p.bodyOpens(lb, head, bare)
	}
	if lb.tok != token.LBRACE {
		return first + 1
	}
	return lb.off + 1
}

func braceAt(t tok) bool { return t.tok == token.LBRACE || t.tok == token.RBRACE }

// bodyOpens fails unless a header, from the keyword at head, stopped at its
// body's {.
func (p *jsxParser) bodyOpens(stop tok, head int, bare bool) {
	if stop.tok == token.LBRACE {
		return
	}
	kw, _ := p.name(head)
	if bare {
		p.fail(head, "%s in a block's body starts a nested block, and needs a { after its header (write {\"%s\"} for the text)", kw, kw)
	}
	p.fail(head, "expected { after the %s header", kw)
}

func headerParses(h []byte) bool {
	text := strings.TrimPrefix(strings.TrimSpace(string(h)), "else")
	_, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+text+" {}\n}\n", 0)
	return err == nil
}

// block reads a for or if block from its keyword; bare, written without the
// braces around it, it ends with its last body.
func (p *jsxParser) block(g *goScan, kw tok, open int, bare bool) (jsxNode, int) {
	b := &jsxBlock{}
	head, prev := kw.off, kw.tok
	for {
		var h int
		if prev == token.ELSE {
			h = g.significant().off + 1
		} else {
			h = p.header(g, prev, head, open, bare)
		}
		b.heads = append(b.heads, span{head, h})
		kids, rb := p.children(h, nil, open, false)
		b.bodies = append(b.bodies, kids)
		g.reset(rb + 1)
		next := g.significant()
		if kw.tok == token.IF && next.tok == token.ELSE {
			head, prev = next.off, token.ELSE
			save := g.s
			switch after := g.significant(); after.tok {
			case token.IF:
				prev = token.IF
			case token.LBRACE:
				g.s = save
			default:
				p.fail(after.off, "expected if or { after else")
			}
			continue
		}
		if bare {
			return b, rb + 1
		}
		if next.tok != token.RBRACE {
			p.fail(next.off, "expected } to close the block opened at line %d", p.f.at(open).Line)
		}
		return b, next.off + 1
	}
}

// matchBlock reads {match subject { case P: children … }}. The match is lowered
// like any other; its case bodies are children.
func (p *jsxParser) matchBlock(g *goScan, kw, first tok, open int, bare bool) (jsxNode, int) {
	subj, last, lb := p.goUntil(g, first, kw.tok, open, braceAt)
	p.bodyOpens(lb, kw.off, bare)
	m := &matchStmt{start: kw.off, subj: span{subj.off, last.end()}, lbrace: lb.off}
	b := &jsxBlock{m: m}
	i := lb.off + 1
	for {
		g.reset(i)
		t := g.significant()
		switch t.tok {
		case token.RBRACE:
			if len(m.cases) == 0 {
				p.fail(kw.off, "a match needs at least one case")
			}
			m.rbrace = t.off
			if bare {
				p.matches = append(p.matches, m)
				return b, t.off + 1
			}
			g.reset(t.off + 1)
			if end := g.significant(); end.tok != token.RBRACE {
				p.fail(end.off, "expected } to close the block opened at line %d", p.f.at(open).Line)
			} else {
				p.matches = append(p.matches, m)
				return b, end.off + 1
			}
		case token.CASE, token.DEFAULT:
			mc := p.caseHeader(g, t)
			m.cases = append(m.cases, mc)
			kids, end := p.children(mc.colon+1, nil, open, true)
			b.cases = append(b.cases, kids)
			i = end
		default:
			p.fail(t.off, "expected case or default in match")
		}
	}
}

// caseHeader reads `case P, Q if guard:` like matchAt does from tokens.
func (p *jsxParser) caseHeader(g *goScan, kw tok) *matchCase {
	mc := &matchCase{start: kw.off, guard: span{-1, -1}}
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
		if wantPat {
			patStart, wantPat = h.off, false
		}
		if wantGuard {
			mc.guard.start, wantGuard = h.off, false
		}
		switch {
		case isOpen(h.tok):
			d++
		case isClose(h.tok):
			d--
		case d == 0 && h.tok == token.COMMA && mc.guard.start < 0:
			mc.pats = append(mc.pats, span{patStart, prev.end()})
			wantPat = true
		case d == 0 && h.tok == token.IF && mc.guard.start < 0:
			if patStart >= 0 {
				mc.pats = append(mc.pats, span{patStart, prev.end()})
				patStart = -1
			}
			wantGuard = true
		case d == 0 && h.tok == token.COLON:
			if mc.guard.start >= 0 {
				mc.guard.end = prev.end()
			} else if patStart >= 0 {
				mc.pats = append(mc.pats, span{patStart, prev.end()})
			}
			mc.colon = h.off
			if kw.tok == token.CASE && len(mc.pats) == 0 {
				p.fail(kw.off, "a case needs a pattern; use case _: for anything")
			}
			return mc
		}
		prev = h
	}
}

// jsxTextValue applies React's whitespace rules to text between tags: lines are
// trimmed where they meet a line break, blank lines dropped, the rest joined
// with one space; then HTML entities are decoded.
func jsxTextValue(raw string) string {
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

// jsxPiece is a piece of a tree's lowering: generated text, or a span of the
// source left as it is.
type jsxPiece struct {
	text string
	keep span
}

type jsxWriter struct {
	rt     string
	f      *fileState
	tree   *jsxTree
	pieces []jsxPiece
}

func (w *jsxWriter) gen(s string) { w.pieces = append(w.pieces, jsxPiece{text: s, keep: span{-1, -1}}) }
func (w *jsxWriter) keep(s span)  { w.pieces = append(w.pieces, jsxPiece{keep: s}) }

// jsxEdits lowers a tree, as it stands, to edits of the gaps between what it keeps.
func (f *fileState) jsxEdits(t *jsxTree) edits {
	w := &jsxWriter{rt: f.rt, f: f, tree: t}
	w.frame(t.start, []jsxNode{t.root})
	var out edits
	var buf strings.Builder
	cur := t.start
	flush := func(to int) {
		text := padNewlines(buf.String(), strings.Count(string(f.src[cur:to]), "\n"))
		if to > cur || text != "" {
			out = append(out, edit{start: cur, end: to, text: text})
		}
		buf.Reset()
	}
	for _, p := range w.pieces {
		if p.keep.start < 0 {
			buf.WriteString(p.text)
			continue
		}
		flush(p.keep.start)
		cur = p.keep.end
	}
	flush(t.end)
	return out
}

// padNewlines puts n newlines into generated text where Go inserts no
// semicolon: after the last , ( [ { ; : or = outside a string.
func padNewlines(text string, n int) string {
	if n == 0 {
		return text
	}
	at, inStr, lastSig := 0, false, byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inStr && c == '\\':
			i++
			continue
		case c == '"':
			inStr = !inStr
		}
		if c != ' ' {
			lastSig = c
		}
		if !inStr && strings.IndexByte(",([{;:=", lastSig) >= 0 {
			at = i + 1
		}
	}
	return text[:at] + strings.Repeat("\n", n) + text[at:]
}

func (w *jsxWriter) kids(ns []jsxNode) {
	for _, n := range ns {
		w.node(n)
		w.gen(", ")
	}
}

func (w *jsxWriter) node(n jsxNode) {
	switch n := n.(type) {
	case *jsxElem:
		w.elem(n)
	case *jsxText:
		w.gen(w.rt + ".Text(" + strconv.Quote(n.val) + ")")
	case *jsxHole:
		w.gen(w.rt + ".Child(")
		w.keep(n.expr)
		w.gen(")")
	case *jsxBlock:
		w.gen(w.rt + ".Nodes(func(__add func(" + w.rt + ".Node)) { ")
		if m := n.m; m != nil {
			w.keep(span{m.start, m.lbrace + 1})
			for i, c := range m.cases {
				w.keep(span{c.start, c.colon + 1})
				w.gen(" ")
				w.body(c.colon, n.cases[i])
			}
			w.keep(span{m.rbrace, m.rbrace + 1})
		} else {
			for i, h := range n.heads {
				if i > 0 {
					w.gen("} ")
				}
				w.keep(h)
				w.gen(" ")
				w.body(h.end, n.bodies[i])
			}
			w.gen("}")
		}
		w.gen(" })")
	}
}

func (w *jsxWriter) attrValue(a *jsxAttr) {
	switch a.kind {
	case 's':
		w.gen(strconv.Quote(a.val))
	case 'b':
		w.gen("true")
	default:
		if a.event {
			w.gen(w.rt + ".On(")
			w.keep(a.expr)
			w.gen(")")
			return
		}
		w.keep(a.expr)
	}
}

func (w *jsxWriter) elem(el *jsxElem) {
	switch {
	case el.frag:
		w.gen(w.rt + ".Fragment(")
		w.kids(el.kids)
		w.gen(")")
	case el.comp != nil:
		w.comp(el.comp)
	default:
		w.gen(w.rt + ".El(" + strconv.Quote(el.tag) + ", ")
		attrs := el.attrs
		if len(attrs) == 0 {
			w.gen("nil")
		} else {
			w.gen("[]" + w.rt + ".Attr{")
			for _, a := range attrs {
				w.gen("{Name: " + strconv.Quote(a.name) + ", Value: ")
				w.attrValue(a)
				w.gen("}, ")
			}
			w.gen("}")
		}
		w.gen(", ")
		w.kids(el.kids)
		w.gen(")")
	}
}
