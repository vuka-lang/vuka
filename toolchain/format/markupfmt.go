package format

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// A .vuka.html file's markup is laid out by its tokens: tags, holes, the Go
// statements of its blocks, text, comments and the whitespace between them.
// The formatter changes only whitespace that doesn't render — the runs
// holding a line break, which it re-indents two spaces a level, and the
// space inside tags — and the Go in holes and statements, as gofmt writes
// it. Text stays on its lines; content whose whitespace renders (<pre>,
// <textarea>) or that isn't markup (<script>, <style>) stays as written,
// but for a top-level stylesheet, whose CSS is formatted.

// mkind is a markup token's kind.
type mkind byte

const (
	mText    mkind = 't' // text, no whitespace in it
	mSpace   mkind = ' ' // whitespace with no line break
	mBreak   mkind = 'n' // whitespace holding line breaks
	mOpen    mkind = 'o' // an opening tag
	mClose   mkind = 'c' // a closing tag
	mHole    mkind = 'h' // {Go}
	mComment mkind = 'm' // <!-- … -->
	mRaw     mkind = 'r' // a raw element's content: <pre>, <textarea>, <script>, <style>
	mStmt    mkind = 's' // a Go statement of the markup: x := …, or a block's head ending in {
	mEnd     mkind = '}' // a block's }
	mElse    mkind = 'e' // } else … {
	mCase    mkind = 'k' // case …: or default:
)

type mtok struct {
	kind  mkind
	start int
	end   int
	name  string   // a tag's name
	attrs []string // an opening tag's attributes, as written
	self  bool     // an opening tag closed by />
	nl    int      // a break's line breaks
	opens bool     // a statement opens a block
	multi bool     // a tag or statement spanning lines, kept as written
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

var rawElements = map[string]bool{"pre": true, "textarea": true, "script": true, "style": true}

// mscan tokenizes markup.
type mscan struct {
	src    []byte
	toks   []mtok
	stack  []string // open elements, and "{" for blocks ("{switch" for a switch)
	err    error
	offset int // where src starts in the file, for errors
}

func (s *mscan) fail(at int, msg string) {
	if s.err == nil {
		s.err = &markupError{off: s.offset + at, msg: msg}
	}
}

type markupError struct {
	off int
	msg string
}

func (e *markupError) Error() string { return e.msg }

func (s *mscan) inBlock() bool {
	return len(s.stack) > 0 && strings.HasPrefix(s.stack[len(s.stack)-1], "{")
}

func (s *mscan) inSwitch() bool {
	return len(s.stack) > 0 && s.stack[len(s.stack)-1] == "{switch"
}

// atLineStart reports whether only spaces come before i on its line.
func (s *mscan) atLineStart(i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch s.src[j] {
		case ' ', '\t':
			continue
		case '\n':
			return true
		}
		return false
	}
	return true
}

func scanMarkup(src []byte, offset int) ([]mtok, error) {
	s := &mscan{src: src, offset: offset}
	i := 0
	for i < len(src) && s.err == nil {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			j := i
			nl := 0
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				if src[j] == '\n' {
					nl++
				}
				j++
			}
			k := mSpace
			if nl > 0 {
				k = mBreak
			}
			s.toks = append(s.toks, mtok{kind: k, start: i, end: j, nl: nl})
			i = j
		case bytes.HasPrefix(src[i:], []byte("<!--")):
			end := bytes.Index(src[i+4:], []byte("-->"))
			if end < 0 {
				s.fail(i, "unterminated comment")
				return nil, s.err
			}
			s.toks = append(s.toks, mtok{kind: mComment, start: i, end: i + 4 + end + 3})
			i += 4 + end + 3
		case c == '<' && i+1 < len(src) && src[i+1] == '/':
			j := i + 2
			for j < len(src) && src[j] != '>' && src[j] != '\n' {
				j++
			}
			if j >= len(src) || src[j] != '>' {
				s.fail(i, "unterminated closing tag")
				return nil, s.err
			}
			name := strings.TrimSpace(string(src[i+2 : j]))
			s.toks = append(s.toks, mtok{kind: mClose, start: i, end: j + 1, name: name})
			for k := len(s.stack) - 1; k >= 0; k-- {
				if s.stack[k] == name {
					s.stack = s.stack[:k]
					break
				}
			}
			i = j + 1
		case c == '<' && i+1 < len(src) && (isLetter(src[i+1]) || src[i+1] == '>'):
			t, end := s.openTag(i)
			if s.err != nil {
				return nil, s.err
			}
			s.toks = append(s.toks, t)
			i = end
			if t.self || voidElements[strings.ToLower(t.name)] {
				break
			}
			if rawElements[strings.ToLower(t.name)] {
				closeAt := indexFold(src[i:], "</"+t.name)
				if closeAt < 0 {
					s.fail(t.start, "<"+t.name+"> has no </"+t.name+">")
					return nil, s.err
				}
				if closeAt > 0 {
					s.toks = append(s.toks, mtok{kind: mRaw, start: i, end: i + closeAt, name: strings.ToLower(t.name)})
				}
				i += closeAt
				s.stack = append(s.stack, t.name)
				break
			}
			s.stack = append(s.stack, t.name)
		case c == '}' && s.inBlock():
			j := i + 1
			rest := lineRest(src, j)
			if t := strings.TrimSpace(rest); strings.HasPrefix(t, "else") && s.atLineStart(i) {
				end := s.stmtEnd(i)
				s.toks = append(s.toks, mtok{kind: mElse, start: i, end: end, opens: true, multi: bytes.Contains(src[i:end], []byte("\n"))})
				i = end
				break
			}
			s.stack = s.stack[:len(s.stack)-1]
			s.toks = append(s.toks, mtok{kind: mEnd, start: i, end: j})
			i = j
		case s.inSwitch() && s.atLineStart(i) && (hasWord(src[i:], "case") || hasWord(src[i:], "default")):
			j := s.caseEnd(i)
			s.toks = append(s.toks, mtok{kind: mCase, start: i, end: j})
			i = j
		case s.atLineStart(i) && lineStmt(src, i):
			end := s.stmtEnd(i)
			text := strings.TrimSpace(string(src[i:end]))
			opens := strings.HasSuffix(text, "{")
			s.toks = append(s.toks, mtok{kind: mStmt, start: i, end: end, opens: opens, multi: bytes.Contains(src[i:end], []byte("\n"))})
			if opens {
				if hasWord(src[i:], "switch") {
					s.stack = append(s.stack, "{switch")
				} else {
					s.stack = append(s.stack, "{")
				}
			}
			i = end
		case c == '{' && i+1 < len(src) && src[i+1] == '{':
			s.text(i, i+2)
			i += 2
		case c == '{':
			end := closeGo(src, i+1)
			if end < 0 {
				s.fail(i, "unterminated {")
				return nil, s.err
			}
			s.toks = append(s.toks, mtok{kind: mHole, start: i, end: end + 1})
			i = end + 1
		default:
			j := i + 1
			for j < len(src) && !strings.ContainsRune(" \t\r\n<{}", rune(src[j])) {
				j++
			}
			if j < len(src) && src[j] == '}' && !s.inBlock() {
				j++
			}
			s.text(i, j)
			i = j
		}
	}
	return s.toks, s.err
}

// text adds text, joining it to text right before it.
func (s *mscan) text(i, j int) {
	if n := len(s.toks); n > 0 && s.toks[n-1].kind == mText && s.toks[n-1].end == i {
		s.toks[n-1].end = j
		return
	}
	s.toks = append(s.toks, mtok{kind: mText, start: i, end: j})
}

func lineRest(src []byte, i int) string {
	j := i
	for j < len(src) && src[j] != '\n' {
		j++
	}
	return string(src[i:j])
}

func indexFold(b []byte, s string) int {
	return strings.Index(strings.ToLower(string(b)), strings.ToLower(s))
}

func hasWord(b []byte, w string) bool {
	if !bytes.HasPrefix(b, []byte(w)) {
		return false
	}
	if len(b) == len(w) {
		return true
	}
	c := b[len(w)]
	return !isIdentByte(c)
}

// lineStmt reports whether a Go statement of the markup starts at i, the
// first thing on its line: an if, for or switch, or an assignment — as the
// compiler decides (transpile's lineStmt).
func lineStmt(src []byte, i int) bool {
	at := func(k int) byte {
		if k < len(src) {
			return src[k]
		}
		return 0
	}
	for _, kw := range []string{"if", "for", "switch"} {
		if bytes.HasPrefix(src[i:], []byte(kw)) {
			c := at(i + len(kw))
			return c == ' ' || c == '\t' || c == '(' || c == '{' && kw == "switch"
		}
	}
	if !isLetter(at(i)) && at(i) != '_' {
		return false
	}
	for _, t := range scanTokens(src[i:min(len(src), i+lineLen(src, i))], 0) {
		switch t.tok {
		case token.IDENT, token.PERIOD, token.COMMA:
		case token.DEFINE, token.ASSIGN, token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.INC, token.DEC:
			return true
		default:
			return false
		}
	}
	return false
}

func lineLen(src []byte, i int) int {
	j := bytes.IndexByte(src[i:], '\n')
	if j < 0 {
		return len(src) - i
	}
	return j
}

// stmtEnd is where the statement starting at i ends: its line's end, or the
// end of the line its brackets close on.
func (s *mscan) stmtEnd(i int) int {
	depth := 0
	j := i
	for j < len(s.src) {
		switch c := s.src[j]; c {
		case '"', '`', '\'':
			j = skipQuoted(s.src, j)
			continue
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '{':
			// A block's { ends its head.
			if depth == 0 && strings.TrimSpace(lineRest(s.src, j+1)) == "" {
				return j + 1
			}
			depth++
		case '}':
			if depth == 0 && j > i {
				return j
			}
			depth--
		case '\n':
			if depth <= 0 {
				return j
			}
		}
		j++
	}
	return j
}

func (s *mscan) caseEnd(i int) int {
	depth := 0
	for j := i; j < len(s.src); j++ {
		switch c := s.src[j]; c {
		case '"', '`', '\'':
			j = skipQuoted(s.src, j) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ':':
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(s.src)
}

// skipQuoted is the offset past the Go string or rune literal at i.
func skipQuoted(src []byte, i int) int {
	q := src[i]
	j := i + 1
	for j < len(src) && src[j] != q {
		if src[j] == '\\' && q != '`' {
			j++
		}
		if q != '`' && src[j] == '\n' {
			return j
		}
		j++
	}
	return min(j+1, len(src))
}

// closeGo is the } closing the Go starting at i, past brackets, literals
// and comments; -1 when none does.
func closeGo(src []byte, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch c := src[i]; c {
		case '"', '`':
			i = skipQuoted(src, i) - 1
		case '\'':
			// A rune literal is short; an apostrophe in markup inside the
			// hole's Go isn't one.
			if e := bytes.IndexByte(src[i+1:min(len(src), i+12)], '\''); e >= 0 && !bytes.Contains(src[i:i+1+e], []byte("\n")) {
				i += 1 + e
			}
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if i+1 < len(src) && src[i+1] == '*' {
				e := bytes.Index(src[i+2:], []byte("*/"))
				if e < 0 {
					return -1
				}
				i += 2 + e + 1
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if c == '}' {
					return i
				}
				return -1
			}
			depth--
		}
	}
	return -1
}

// openTag reads the opening tag at i.
func (s *mscan) openTag(i int) (mtok, int) {
	src := s.src
	j := i + 1
	for j < len(src) && (isNameByte(src[j]) || src[j] == '[' && false) {
		j++
	}
	// A generic component: <List[Task]>.
	if j < len(src) && src[j] == '[' {
		if e := bytes.IndexByte(src[j:], ']'); e >= 0 {
			j += e + 1
		}
	}
	t := mtok{kind: mOpen, start: i, name: string(src[i+1 : j])}
	for {
		for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
			j++
		}
		if j >= len(src) {
			s.fail(i, "unterminated tag <"+t.name)
			return t, j
		}
		if src[j] == '>' {
			t.end = j + 1
			break
		}
		if src[j] == '/' && j+1 < len(src) && src[j+1] == '>' {
			t.self, t.end = true, j+2
			break
		}
		// An attribute: a name, then =value; or a hole, {attrs...}.
		a := j
		if src[j] == '{' {
			e := closeGo(src, j+1)
			if e < 0 {
				s.fail(j, "unterminated {")
				return t, j
			}
			j = e + 1
		} else {
			for j < len(src) && !strings.ContainsRune(" \t\r\n=>{", rune(src[j])) && !(src[j] == '/' && j+1 < len(src) && src[j+1] == '>') {
				j++
			}
			if j < len(src) && src[j] == '=' {
				j++
				switch {
				case j < len(src) && (src[j] == '"' || src[j] == '\''):
					e := bytes.IndexByte(src[j+1:], src[j])
					if e < 0 {
						s.fail(j, "unterminated attribute value")
						return t, j
					}
					j += e + 2
				case j < len(src) && src[j] == '{':
					e := closeGo(src, j+1)
					if e < 0 {
						s.fail(j, "unterminated {")
						return t, j
					}
					j = e + 1
				default:
					for j < len(src) && !strings.ContainsRune(" \t\r\n>", rune(src[j])) && !(src[j] == '/' && j+1 < len(src) && src[j+1] == '>') {
						j++
					}
				}
			}
		}
		if j == a {
			s.fail(j, "cannot read this attribute")
			return t, j
		}
		t.attrs = append(t.attrs, string(src[a:j]))
	}
	t.multi = bytes.Contains(src[i:t.end], []byte("\n"))
	return t, t.end
}

// markupPrinter lays tokens out.
type markupPrinter struct {
	src   []byte
	toks  []mtok
	o     Options
	b     strings.Builder
	depth int
	css   func(string) (string, error)
}

func (p *markupPrinter) col() int {
	s := p.b.String()
	return textWidth(s[strings.LastIndexByte(s, '\n')+1:])
}

func (p *markupPrinter) lineIndent() string {
	s := p.b.String()
	l := s[strings.LastIndexByte(s, '\n')+1:]
	return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
}

func indentOf(n int) string { return strings.Repeat("  ", max(n, 0)) }

// formatMarkup lays out the markup src (the file's from offset).
func formatMarkup(src []byte, offset int, o Options, css func(string) (string, error)) (string, error) {
	toks, err := scanMarkup(src, offset)
	if err != nil {
		return "", err
	}
	p := &markupPrinter{src: src, toks: toks, o: o, css: css}
	// The top-level stylesheets and scripts after the markup are its
	// component's: a blank line before each.
	topAsset := make([]bool, len(toks))
	depth := 0
	for i, t := range toks {
		switch t.kind {
		case mOpen:
			if depth == 0 && (strings.EqualFold(t.name, "style") || strings.EqualFold(t.name, "script")) {
				topAsset[i] = true
			}
			if !t.self && !voidElements[strings.ToLower(t.name)] {
				depth++
			}
		case mClose:
			depth--
		case mStmt, mElse:
			if t.opens && t.kind == mStmt {
				depth++
			}
		case mEnd:
			depth--
		}
	}
	first := 0
	for first < len(toks) && (toks[first].kind == mBreak || toks[first].kind == mSpace) {
		first++
	}
	last := len(toks)
	for last > first && (toks[last-1].kind == mBreak || toks[last-1].kind == mSpace) {
		last--
	}
	for i := first; i < last; i++ {
		t := toks[i]
		text := string(src[t.start:t.end])
		switch t.kind {
		case mBreak, mSpace:
			next := toks[i+1]
			if topAsset[i+1] {
				p.b.WriteString("\n\n")
				continue
			}
			if t.kind == mSpace {
				p.b.WriteString(text)
				continue
			}
			d := p.depth
			switch next.kind {
			case mClose, mEnd, mElse, mCase:
				d--
			}
			prev := toks[i-1]
			nl := "\n"
			if t.nl > 1 && prev.kind != mStmt && !(prev.kind == mOpen && !prev.self) && prev.kind != mElse && prev.kind != mCase &&
				next.kind != mClose && next.kind != mEnd && next.kind != mElse {
				nl = "\n\n"
			}
			if prev.kind == mStmt && !prev.opens && t.nl > 1 && next.kind != mEnd && next.kind != mClose {
				nl = "\n\n"
			}
			p.b.WriteString(nl + indentOf(d))
		case mOpen:
			// A childless component closes itself: <X></X> is <X />.
			if !t.self && isComponent(t.name) && i+1 < len(toks) && toks[i+1].kind == mClose && toks[i+1].name == t.name {
				t.self = true
				toks[i+1].kind = 0
			}
			p.openTag(t, text)
			if !t.self && !voidElements[strings.ToLower(t.name)] {
				p.depth++
			}
		case 0: // a closing tag folded into its opening one
		case mClose:
			p.depth--
			p.b.WriteString("</" + t.name + ">")
		case mRaw:
			if t.name == "style" && i > 0 && topAsset[i-1] && p.css != nil {
				css, err := p.css(text)
				if err != nil {
					return "", err
				}
				p.b.WriteString("\n" + css)
				continue
			}
			p.b.WriteString(text)
		case mHole:
			p.b.WriteString(holesFormatted(text))
		case mStmt:
			if t.opens {
				p.b.WriteString(p.stmtHead(text))
				p.depth++
			} else if t.multi {
				p.b.WriteString(strings.TrimRight(text, " \t"))
			} else {
				p.b.WriteString(simpleStmt(strings.TrimSpace(text)))
			}
		case mElse:
			p.depth--
			p.b.WriteString(p.stmtHead(text))
			p.depth++
		case mEnd:
			p.depth--
			p.b.WriteString("}")
		case mCase:
			p.b.WriteString(caseHead(strings.TrimSpace(text)))
		default:
			p.b.WriteString(text)
		}
	}
	return p.b.String(), nil
}

// openTag writes an opening tag: on one line when it fits, else each
// attribute on a line of its own.
func (p *markupPrinter) openTag(t mtok, text string) {
	end := ">"
	if t.self || voidElements[strings.ToLower(t.name)] {
		end = " />"
	}
	attrs := make([]string, len(t.attrs))
	multi := false
	for i, a := range t.attrs {
		attrs[i] = holesFormatted(a)
		if p.o.Tailwind {
			attrs[i] = tailwindAttr(attrs[i])
		}
		multi = multi || strings.Contains(a, "\n")
	}
	if multi {
		// An attribute's value spans lines: the tag stays as written.
		p.b.WriteString(text)
		return
	}
	flat := "<" + t.name
	for _, a := range attrs {
		flat += " " + a
	}
	if len(attrs) == 0 && end == " />" {
		flat += " />"
	} else {
		flat += end
	}
	if len(attrs) < 2 || p.col()+textWidth(flat) <= p.o.width() {
		p.b.WriteString(flat)
		return
	}
	ind := p.lineIndent()
	p.b.WriteString("<" + t.name)
	for _, a := range attrs {
		p.b.WriteString("\n" + ind + "  " + a)
	}
	p.b.WriteString("\n" + ind + strings.TrimSpace(end))
}

// stmtHead is a block's head — if, for, switch, } else … — as gofmt
// writes it, or as written when it isn't Go on its own.
func (p *markupPrinter) stmtHead(text string) string {
	t := strings.TrimSpace(text)
	if strings.Contains(t, "\n") {
		return strings.TrimRight(text, " \t")
	}
	body := strings.TrimSuffix(t, "{")
	prefix, suffix := "", " {\n}"
	if strings.HasPrefix(body, "}") {
		// } else if c { → if true {} else if c {}
		prefix, body = "if true {\n} ", strings.TrimSpace(strings.TrimPrefix(body, "}"))
	}
	src := "package p\n\nfunc _() {\n" + prefix + body + suffix + "\n}\n"
	out, err := format.Source([]byte(src))
	if err != nil {
		return t
	}
	s := strings.TrimPrefix(string(out), "package p\n\nfunc _() {\n")
	if prefix != "" {
		i := strings.Index(s, "} else")
		if i < 0 {
			return t
		}
		s = s[i:]
	}
	s = strings.TrimLeft(s, "\t")
	nl := strings.IndexByte(s, '\n')
	if nl < 0 || !strings.HasSuffix(s[:nl], "{") {
		return t
	}
	return s[:nl]
}

// simpleStmt is a statement as gofmt writes it.
func simpleStmt(t string) string {
	src := "package p\n\nfunc _() {\n" + t + "\n}\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, 0); err != nil {
		return t
	}
	out, err := format.Source([]byte(src))
	if err != nil {
		return t
	}
	s := strings.TrimSuffix(strings.TrimPrefix(string(out), "package p\n\nfunc _() {\n\t"), "\n}\n")
	if strings.Contains(s, "\n") {
		return t
	}
	return s
}

// caseHead is a case's head as gofmt writes it.
func caseHead(t string) string {
	if t == "default:" {
		return t
	}
	src := "package p\n\nfunc _() {\nswitch {\n" + t + "\n}\n}\n"
	out, err := format.Source([]byte(src))
	if err != nil {
		return t
	}
	s := strings.TrimPrefix(string(out), "package p\n\nfunc _() {\n\tswitch {\n\t")
	if i := strings.IndexByte(s, '\n'); i >= 0 && strings.HasSuffix(s[:i], ":") {
		return s[:i]
	}
	return t
}

// templateSig is what the markup renders, for checking the formatter changed
// none of it: its tokens but whitespace that doesn't render, a hole's or
// statement's Go by its tokens, a tag by its name, attributes and whether
// it closes itself.
func templateSig(src []byte) (string, error) {
	toks, err := scanMarkup(src, 0)
	if err != nil {
		return "", err
	}
	for len(toks) > 0 && (toks[0].kind == mBreak || toks[0].kind == mSpace) {
		toks = toks[1:]
	}
	for len(toks) > 0 && (toks[len(toks)-1].kind == mBreak || toks[len(toks)-1].kind == mSpace) {
		toks = toks[:len(toks)-1]
	}
	var b strings.Builder
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		text := string(src[t.start:t.end])
		switch t.kind {
		case mBreak:
			b.WriteString("⏎")
		case mSpace:
			b.WriteString("␠" + text)
		case mOpen:
			self := t.self || voidElements[strings.ToLower(t.name)]
			if !self && isComponent(t.name) && i+1 < len(toks) && toks[i+1].kind == mClose && toks[i+1].name == t.name {
				self = true
				i++
			}
			b.WriteString("<" + t.name)
			for _, a := range t.attrs {
				b.WriteString(" " + goSig(classSet(a)))
			}
			if self {
				b.WriteString("/")
			}
			b.WriteString(">")
		case mHole, mStmt, mElse, mCase:
			b.WriteString(string(t.kind) + goSig(text))
		case mRaw:
			b.WriteString("raw:" + t.name)
			if t.name != "style" {
				b.WriteString(text)
			}
		default:
			b.WriteString(string(t.kind) + text)
		}
		b.WriteString("\x00")
	}
	return b.String(), nil
}

// goSig is Go text by its tokens.
func goSig(s string) string {
	var b strings.Builder
	for _, t := range scanTokens([]byte(s), 0) {
		if t.tok == token.SEMICOLON && t.lit == "\n" {
			continue
		}
		b.WriteString(t.tok.String() + t.lit + " ")
	}
	return b.String()
}

// tailwindAttr is a class attribute with its static list in Tailwind's
// order; any other attribute as it is.
func tailwindAttr(a string) string {
	for _, q := range []string{`"`, `'`} {
		if strings.HasPrefix(a, "class="+q) && strings.HasSuffix(a, q) && len(a) > len("class=")+1 {
			v := a[len("class=")+1 : len(a)-1]
			if strings.ContainsAny(v, "{}"+q) {
				return a
			}
			lead, trail := v[:len(v)-len(strings.TrimLeft(v, " "))], v[len(strings.TrimRight(v, " ")):]
			return "class=" + q + lead + Tailwind(strings.TrimSpace(v)) + trail + q
		}
	}
	return a
}

// classSet is a static class attribute with its classes sorted: Tailwind
// ordering moves them, and their order doesn't render.
func classSet(a string) string {
	if t := tailwindAttr(a); t == a && !strings.HasPrefix(a, "class=") {
		return a
	}
	for _, q := range []string{`"`, `'`} {
		if strings.HasPrefix(a, "class="+q) && strings.HasSuffix(a, q) && len(a) > len("class=")+1 {
			v := a[len("class=")+1 : len(a)-1]
			if strings.ContainsAny(v, "{}") {
				return a
			}
			f := strings.Fields(v)
			sort.Strings(f)
			return "class=" + q + strings.Join(f, " ") + q
		}
	}
	return a
}
