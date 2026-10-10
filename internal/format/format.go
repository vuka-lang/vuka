// Package format formats Vuka source: the Go in it exactly as gofmt does, and
// its JSX the way Prettier lays out markup, without changing what it renders.
//
// Each Vuka construct is masked with Go of a stable shape (JSX and statics by
// identifiers as wide as they print, so gofmt's alignment around them is
// right; match by a switch; ? by a selector; an attribute by a comment), the
// result goes through go/format, and the constructs are put back, formatted.
package format

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Source formats a .vuka file. On a syntax error it returns the error, with
// the position in src, and no output.
func Source(src []byte) ([]byte, error) {
	if !utf8.Valid(src) {
		return nil, errors.New("not valid UTF-8")
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")) // as gofmt's output has them
	fm := &formatter{exprs: map[string]exprResult{}}
	for i := 0; fm.mark == ""; i++ {
		m := "_" + string(rune('Q'+i%10))
		if i >= 10 {
			m += strconv.Itoa(i)
		}
		if !bytes.Contains(src, []byte(m)) {
			fm.mark = m
		}
	}
	return fm.file(src)
}

type formatter struct {
	mark  string                // what every placeholder identifier starts with, found nowhere in the source
	exprs map[string]exprResult // Go expressions already formatted, by text and position
}

type exprResult struct {
	text string
	ok   bool
}

// construct is a Vuka construct found in the source and its Go stand-in.
type construct struct {
	kind       byte // 'J' JSX, 'S' static field, 'F' static method, 'D' decorator, 'A' attribute, 'B' field attribute, 'G' guard, 'T' ?, 'M' match, 'C' a decorator's call parameter
	start, end int
	el         *jElem
	text       string // what is put back: the attribute, decorator head, static method head, static's name
	rest       string // a static's type and value
	width      int    // the JSX marker's width
	grown      bool   // the JSX went onto several lines in a pass
	sameLine   bool   // the attribute is followed by its declaration on the same line
}

func (fm *formatter) marker(prefix string, width int) string {
	m := fm.mark + prefix
	if n := width - utf8.RuneCountInString(m); n > 0 {
		m += strings.Repeat("_", n)
	}
	return m
}

func (fm *formatter) standIn(c *construct) string {
	switch c.kind {
	case 'J':
		return fm.marker("", c.width)
	case 'S':
		return fm.marker("S", textWidth(c.text)) + " " + fm.marker("", textWidth(firstLine(c.rest)))
	case 'F':
		return fm.marker("F", textWidth(c.text))
	case 'D':
		return "func " + fm.marker("D", textWidth(c.text)-len("func "))
	case 'A':
		// On a line of its own: gofmt would move the declaration off its
		// line only sometimes.
		if c.sameLine {
			return "/*" + fm.marker("A", 0) + "*/\n"
		}
		return "/*" + fm.marker("A", 0) + "*/"
	case 'B':
		// A trailing comment as wide as the attributes, so gofmt aligns them
		// as it aligns comments; a line comment takes in a comment after them.
		if c.sameLine {
			return "/*" + fm.marker("B", textWidth(c.text)-4) + "*/"
		}
		return "//" + fm.marker("B", textWidth(c.text)-2)
	case 'G':
		return ", " + fm.marker("G", 0) + ","
	case 'T':
		return "." + fm.marker("T", 0)
	case 'C':
		return " " + fm.marker("C", 0)
	case 'M':
		return "switch"
	}
	panic("unknown construct")
}

// multiWidth is the width of the stand-in for JSX laid out on several lines:
// wider than gofmt keeps a function on one line.
const multiWidth = 101

// file formats a file.
func (fm *formatter) file(src []byte) ([]byte, error) {
	cs, err := scan(src)
	if err != nil {
		return nil, err
	}
	var out []byte
	for pass := 0; pass < 4; pass++ {
		var b bytes.Buffer
		var es []edit
		last := 0
		for _, c := range cs {
			if c.start < last || c.end > len(src) {
				return nil, errorAt(src, c.start, "cannot read this Vuka construct")
			}
			b.Write(src[last:c.start])
			s := fm.standIn(c)
			es = append(es, edit{c.start, c.end, b.Len(), len(s)})
			b.WriteString(s)
			last = c.end
		}
		b.Write(src[last:])
		masked := b.Bytes()
		if _, err := parser.ParseFile(token.NewFileSet(), "", masked, parser.PackageClauseOnly); err != nil {
			return nil, mapError(err, masked, src, es) // not a fragment, which go/format would take
		}
		gofmted, err := format.Source(masked)
		if err != nil {
			return nil, mapError(err, masked, src, es)
		}
		var switches []bool
		for _, t := range scanTokens(masked, 0) {
			if t.tok == token.SWITCH {
				isMatch := false
				for i, c := range cs {
					isMatch = isMatch || c.kind == 'M' && es[i].gen == t.off
				}
				switches = append(switches, isMatch)
			}
		}
		var widths []int
		out, widths, err = fm.restore(gofmted, src, cs, switches)
		if err != nil {
			return nil, err
		}
		changed, j := false, 0
		for _, c := range cs {
			if c.kind == 'J' {
				// Once one goes onto several lines, it stays there: one
				// that only fits when gofmt makes room would flip back.
				if w := widths[j]; w != c.width && (w == multiWidth || !c.grown) {
					c.width, c.grown, changed = w, w == multiWidth, true
				}
				j++
			}
		}
		if !changed {
			break
		}
	}
	if err := sameMarkup(src, out); err != nil {
		return nil, err
	}
	return out, nil
}

// edit records a construct's stand-in in the masked text.
type edit struct{ start, end, gen, genLen int }

// mapError reports an error of go/format at its place in the source.
func mapError(err error, masked, src []byte, es []edit) error {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return err
	}
	e := list[0]
	off, delta := lineOffset(masked, e.Pos.Line, e.Pos.Column), 0
	for _, x := range es {
		if off < x.gen {
			break
		}
		if off < x.gen+x.genLen {
			off, delta = x.start, 0
			break
		}
		delta = x.end - (x.gen + x.genLen)
	}
	line, col := position(src, off+delta)
	return fmt.Errorf("%d:%d: %s", line, col, e.Msg)
}

func lineOffset(src []byte, line, col int) int {
	off := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return min(off+col-1, len(src))
}

func position(src []byte, off int) (line, col int) {
	off = min(max(off, 0), len(src))
	ls := bytes.LastIndexByte(src[:off], '\n') + 1
	return bytes.Count(src[:off], []byte("\n")) + 1, off - ls + 1
}

func errorAt(src []byte, off int, msg string) error {
	line, col := position(src, off)
	return fmt.Errorf("%d:%d: %s", line, col, msg)
}

func scanTokens(src []byte, base int) []tok {
	fset := token.NewFileSet()
	f := fset.AddFile("", -1, len(src)-base)
	var s scanner.Scanner
	s.Init(f, src[base:], func(token.Position, string) {}, scanner.ScanComments)
	var out []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out
		}
		out = append(out, tok{base + f.Offset(pos), t, lit})
	}
}

// scanAfterOperand scans src from off as it follows an operand: a newline
// there ends the statement.
func scanAfterOperand(src []byte, off int) []tok {
	buf := append([]byte{')'}, src[off:]...)
	ts := scanTokens(buf, 0)
	if len(ts) > 0 {
		ts = ts[1:]
	}
	for i := range ts {
		ts[i].off += off - 1
	}
	return ts
}

func isAt(t tok) bool       { return t.tok == token.ILLEGAL && t.lit == "@" }
func isQuestion(t tok) bool { return t.tok == token.ILLEGAL && t.lit == "?" }

// scan finds the Vuka constructs, from tokens, the way transpile does.
func scan(src []byte) ([]*construct, error) {
	toks := scanTokens(src, 0)
	var cs []*construct
	type frame struct {
		typeGroup  bool
		structOf   bool
		structBody bool
	}
	var frames []frame
	top := func() frame {
		if len(frames) == 0 {
			return frame{}
		}
		return frames[len(frames)-1]
	}
	prev, prevIdx := tok{tok: token.SEMICOLON}, -1
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.tok == token.COMMENT {
			continue
		}
		p, pIdx := prev, prevIdx
		prev, prevIdx = t, i
		depth := len(frames)
		switch {
		case isOpen(t.tok):
			fr := frame{typeGroup: t.tok == token.LPAREN && p.tok == token.TYPE}
			fr.structBody = t.tok == token.LBRACE && p.tok == token.STRUCT
			fr.structOf = fr.structBody && namedStruct(toks, pIdx, top().typeGroup)
			frames = append(frames, fr)
		case isClose(t.tok):
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
		case t.tok == token.IDENT && t.lit == "static" && top().structOf && (p.tok == token.SEMICOLON || p.tok == token.LBRACE):
			c, last, err := static(src, toks, i)
			if err != nil {
				return nil, err
			}
			cs = append(cs, c)
			i, prev, prevIdx = last, toks[last], last
		case t.tok == token.FUNC && depth == 0:
			if c := staticFunc(toks, i); c != nil {
				cs = append(cs, c)
			}
		case isQuestion(t):
			if p.end() != t.off || p.tok == token.SEMICOLON {
				return nil, errorAt(src, t.off, "unexpected ?: it goes right after the expression it unwraps, as in f()?")
			}
			cs = append(cs, &construct{kind: 'T', start: t.off, end: t.off + 1})
		case t.tok == token.IDENT && t.lit == "decorator" && depth == 0 && i+1 < len(toks) && toks[i+1].tok == token.IDENT:
			cs = append(cs, &construct{kind: 'D', start: t.off, end: toks[i+1].end(), text: "decorator " + toks[i+1].lit})
			// decorator retry(n int)(c): (c) stands as a named result, or
			// gofmt would drop its parentheses.
			if i+2 < len(toks) && toks[i+2].tok == token.LPAREN {
				if j, _, ok := matchClose(toks, i+2); ok && j < len(toks) && toks[j].tok == token.LPAREN {
					if k, _, ok := matchClose(toks, j); ok {
						cs = append(cs, &construct{kind: 'C', start: toks[k-1].off, end: toks[k-1].off})
					}
				}
			}
			i++
			prev, prevIdx = toks[i], i
		case t.tok == token.IDENT && t.lit == "match" && depth > 0 && (p.tok == token.SEMICOLON || p.tok == token.LBRACE || p.tok == token.COLON):
			cs = append(cs, matchAt(toks, i)...)
		case t.tok == token.LSS && depth > 0 && jsxStarts(p.tok) && jsxTagAt(src, t.off):
			el, jerr := parseJSX(src, t.off)
			if jerr != nil {
				return nil, errorAt(src, jerr.off, jerr.msg)
			}
			c := &construct{kind: 'J', start: el.start, end: el.end, el: el, width: multiWidth}
			if raw := src[el.start:el.end]; !bytes.ContainsRune(raw, '\n') {
				c.width = textWidth(string(raw))
			}
			cs = append(cs, c)
			toks = append(toks[:i:i], append([]tok{{t.off, token.IDENT, string(src[t.off:el.end])}}, scanAfterOperand(src, el.end)...)...)
			prev = toks[i]
		case isAt(t) && top().structBody:
			c, next, err := attrAt(src, toks, i)
			if err != nil {
				return nil, err
			}
			c.kind = 'B'
			if n := len(cs); n > 0 && cs[n-1].kind == 'B' && strings.Trim(string(src[cs[n-1].end:c.start]), " \t") == "" {
				cs[n-1].end, cs[n-1].text = c.end, cs[n-1].text+"\x00"+c.text
			} else {
				cs = append(cs, c)
			}
			i = next - 1
			prev, prevIdx = toks[i], i
		case isAt(t) && depth == 0:
			c, next, err := attrAt(src, toks, i)
			if err != nil {
				return nil, err
			}
			// Attributes sharing a line stay together, one space apart.
			ls := bytes.LastIndexByte(src[:c.start], '\n') + 1
			switch n := len(cs); {
			case n > 0 && cs[n-1].kind == 'A' && strings.Trim(string(src[cs[n-1].end:c.start]), " \t") == "":
				cs[n-1].end, cs[n-1].text = c.end, cs[n-1].text+"\x00"+c.text
			case strings.Trim(string(src[ls:c.start]), " \t") != "":
				return nil, errorAt(src, c.start, "an attribute must start its own line")
			default:
				cs = append(cs, c)
			}
			i = next - 1
			prev, prevIdx = toks[i], i
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].start < cs[j].start })
	ok := true
	for _, c := range cs {
		if c.kind == 'A' || c.kind == 'B' {
			rest := src[c.end:]
			if i := bytes.IndexByte(rest, '\n'); i >= 0 {
				rest = rest[:i]
			}
			rest = bytes.TrimSpace(rest)
			c.sameLine = len(rest) > 0 && !bytes.HasPrefix(rest, []byte("//"))
		}
	}
	for _, c := range cs {
		switch c.kind {
		case 'S':
			if strings.TrimSpace(c.rest) == "" {
				return nil, errorAt(src, c.start, c.text+" needs a type or a value")
			}
			kw, name := "var ", c.text[len("static "):]
			if n, ok := strings.CutPrefix(name, "const "); ok {
				kw, name = "const ", n
			}
			if c.rest, ok = goSnippet(kw+name+" ", c.rest); !ok {
				return nil, errorAt(src, c.start, "bad "+c.text)
			}
		case 'A', 'B':
			attrs := strings.Split(c.text, "\x00")
			for i, a := range attrs {
				if attrs[i], ok = goSnippet("var _ = ", a[1:]); !ok {
					return nil, errorAt(src, c.start, "bad attribute "+a)
				}
				attrs[i] = "@" + attrs[i]
			}
			c.text = strings.Join(attrs, " ")
		}
	}
	return cs, nil
}

// matchAt reads the match whose keyword is toks[i]: its keyword and guards
// become a switch's. Nothing is returned for ordinary Go (a variable named match).
func matchAt(toks []tok, i int) []*construct {
	j := i + 1
	if j >= len(toks) || !exprStart(toks[j].tok) {
		return nil
	}
	depth, k := 0, j
	for ; k < len(toks); k++ {
		t := toks[k].tok
		if depth == 0 && (t == token.LBRACE || t == token.SEMICOLON) {
			break
		}
		switch {
		case isOpen(t):
			depth++
		case isClose(t):
			depth--
		}
	}
	if k >= len(toks) || toks[k].tok != token.LBRACE {
		return nil
	}
	n := k + 1
	for n < len(toks) && trivia(toks[n]) {
		n++
	}
	if n >= len(toks) || toks[n].tok != token.CASE && toks[n].tok != token.DEFAULT && toks[n].tok != token.RBRACE {
		return nil
	}
	out := []*construct{{kind: 'M', start: toks[i].off, end: toks[i].end()}}
	depth = 0
	for c := n; c < len(toks); c++ {
		t := toks[c]
		switch {
		case isOpen(t.tok):
			depth++
		case isClose(t.tok):
			if depth == 0 {
				return out
			}
			depth--
		case depth == 0 && t.tok == token.CASE:
			d := 0
			for c++; c < len(toks); c++ {
				h := toks[c]
				switch {
				case isOpen(h.tok):
					d++
				case isClose(h.tok):
					d--
				case d == 0 && h.tok == token.IF:
					out = append(out, &construct{kind: 'G', start: h.off, end: h.end()})
				}
				if d == 0 && h.tok == token.COLON || d < 0 {
					break
				}
			}
		}
	}
	return out
}

// attrAt reads the attribute whose @ is toks[i]; it returns the index of the
// token after it.
func attrAt(src []byte, toks []tok, i int) (*construct, int, error) {
	at := toks[i].off
	j := i + 1
	if j >= len(toks) || toks[j].tok != token.IDENT || toks[j].off != at+1 {
		return nil, 0, errorAt(src, at, "expected an attribute name after @")
	}
	end := toks[j].end()
	j++
	if j+1 < len(toks) && toks[j].tok == token.PERIOD && toks[j+1].tok == token.IDENT {
		end = toks[j+1].end()
		j += 2
	}
	if j < len(toks) && toks[j].tok == token.LBRACK && toks[j].off == end {
		var ok bool
		if j, end, ok = matchClose(toks, j); !ok {
			return nil, 0, errorAt(src, at, "unclosed [ in attribute")
		}
	}
	if j < len(toks) && (toks[j].tok == token.LPAREN || toks[j].tok == token.LBRACE) {
		var ok bool
		if j, end, ok = matchClose(toks, j); !ok {
			return nil, 0, errorAt(src, at, "unclosed argument list in attribute")
		}
	}
	return &construct{kind: 'A', start: at, end: end, text: string(src[at:end])}, j, nil
}

func matchClose(toks []tok, j int) (int, int, bool) {
	d := 0
	for ; j < len(toks); j++ {
		switch {
		case isOpen(toks[j].tok):
			d++
		case isClose(toks[j].tok):
			if d--; d == 0 {
				return j + 1, toks[j].end(), true
			}
		}
	}
	return j, 0, false
}

// namedStruct reports whether the struct keyword at toks[i] declares a named type.
func namedStruct(toks []tok, i int, inTypeGroup bool) bool {
	k := i - 1
	for k >= 0 && toks[k].tok == token.COMMENT {
		k--
	}
	if k >= 0 && toks[k].tok == token.RBRACK {
		d := 0
		for ; k >= 0; k-- {
			if toks[k].tok == token.RBRACK {
				d++
			} else if toks[k].tok == token.LBRACK {
				if d--; d == 0 {
					break
				}
			}
		}
		k--
	}
	if k < 1 || toks[k].tok != token.IDENT {
		return false
	}
	p := toks[k-1].tok
	return p == token.TYPE || inTypeGroup && (p == token.SEMICOLON || p == token.LPAREN)
}

// staticFunc reads `func T.Name(` or `func T[P].Name(`.
func staticFunc(toks []tok, i int) *construct {
	j := i + 1
	if j+1 >= len(toks) || toks[j].tok != token.IDENT {
		return nil
	}
	head := toks[j].lit
	k := j + 1
	if toks[k].tok == token.LBRACK {
		next, _, ok := matchClose(toks, k)
		if !ok {
			return nil
		}
		var params []string
		for m := k + 1; m < next-1; m++ {
			switch toks[m].tok {
			case token.IDENT:
				params = append(params, toks[m].lit)
			case token.COMMA:
			default:
				return nil
			}
		}
		head += "[" + strings.Join(params, ", ") + "]"
		k = next
	}
	if k+2 >= len(toks) || toks[k].tok != token.PERIOD || toks[k+1].tok != token.IDENT || toks[k+2].tok != token.LPAREN {
		return nil
	}
	return &construct{kind: 'F', start: toks[j].off, end: toks[k+1].end(), text: head + "." + toks[k+1].lit}
}

// static reads the static field whose word is toks[i], to the end of its
// line; it returns the index of its last token.
func static(src []byte, toks []tok, i int) (*construct, int, error) {
	j := i + 1
	name := "static "
	if j < len(toks) && toks[j].tok == token.CONST {
		name += "const "
		j++
	}
	if j >= len(toks) || toks[j].tok != token.IDENT {
		return nil, 0, errorAt(src, toks[i].off, "expected a name after static")
	}
	name += toks[j].lit
	end, d := j+1, 0
	for ; end < len(toks); end++ {
		t := toks[end].tok
		if d == 0 && (t == token.SEMICOLON || t == token.RBRACE) {
			break
		}
		if isOpen(t) {
			d++
		} else if isClose(t) {
			d--
		}
	}
	if d != 0 {
		return nil, 0, errorAt(src, toks[i].off, "unclosed static "+name)
	}
	last := end - 1
	for last > j && trivia(toks[last]) {
		last--
	}
	c := &construct{kind: 'S', start: toks[i].off, end: toks[last].end(), text: name}
	if last > j {
		c.rest = string(src[toks[j+1].off:toks[last].end()])
	}
	return c, last, nil
}

// goSnippet formats text as gofmt does following prefix in a top-level
// declaration: a static's type and value as a var's, an attribute as a value.
func goSnippet(prefix, text string) (string, bool) {
	out, err := format.Source([]byte("package p\n\n" + prefix + text + "\n"))
	if err != nil {
		return text, false
	}
	s := strings.TrimSuffix(string(out), "\n")
	i := strings.Index(s, "\n"+prefix)
	if i < 0 {
		return text, false
	}
	return s[i+1+len(prefix):], true
}

// restore puts the constructs back into gofmt's output; switches says which
// of its switch keywords stand for a match. It returns the width each JSX
// expression printed at (multiWidth for one on several lines).
func (fm *formatter) restore(src, orig []byte, cs []*construct, switches []bool) ([]byte, []int, error) {
	queue := map[byte][]*construct{}
	for _, c := range cs {
		queue[c.kind] = append(queue[c.kind], c)
	}
	stray := false
	next := func(kind byte) *construct {
		q := queue[kind]
		if len(q) == 0 {
			stray = true
			return &construct{el: &jElem{}}
		}
		queue[kind] = q[1:]
		return q[0]
	}
	var b bytes.Buffer
	var widths []int
	last := 0
	put := func(start, end int, s string) {
		b.Write(src[last:start])
		b.WriteString(s)
		last = end
	}
	m := fm.mark
	toks := scanTokens(src, 0)
	si := 0
	for k := 0; k < len(toks); k++ {
		t := toks[k]
		switch {
		case t.tok == token.SWITCH:
			if si < len(switches) && switches[si] {
				next('M')
				put(t.off, t.end(), "match")
			}
			si++
		case t.tok == token.COMMENT && t.lit == "/*"+m+"A*/":
			put(t.off, t.end(), next('A').text)
		case t.tok == token.COMMENT && len(t.lit) > 2 && strings.HasPrefix(t.lit[2:], m+"B"):
			rest := strings.TrimLeft(t.lit[2+len(m)+1:], "_")
			if strings.HasPrefix(t.lit, "/*") {
				rest = strings.TrimPrefix(rest, "*/")
			}
			b.Write(src[last:t.off])
			b.WriteString(reindent(next('B').text, lineIndent(b.Bytes())) + rest)
			last = t.end()
		case t.tok != token.IDENT || !strings.HasPrefix(t.lit, m):
		case t.lit == m+"T" && k > 0 && toks[k-1].tok == token.PERIOD:
			next('T')
			put(toks[k-1].off, t.end(), "?")
		case t.lit == m+"G" && k > 0 && k+1 < len(toks) && toks[k-1].tok == token.COMMA && toks[k+1].tok == token.COMMA:
			next('G')
			put(toks[k-1].off, toks[k+1].end(), " if")
			k++
		case strings.HasPrefix(t.lit, m+"D") && k > 0 && toks[k-1].tok == token.FUNC:
			put(toks[k-1].off, t.end(), next('D').text)
		case t.lit == m+"C" && k > 2:
			next('C')
			if toks[k-2].tok == token.LPAREN && toks[k-3].tok == token.RPAREN {
				put(toks[k-3].end(), toks[k-2].off, "")
			}
			put(toks[k-1].end(), t.end(), "")
		case strings.HasPrefix(t.lit, m+"F"):
			put(t.off, t.end(), next('F').text)
		case strings.HasPrefix(t.lit, m+"S") && k+1 < len(toks) && toks[k+1].tok == token.IDENT:
			c := next('S')
			put(t.off, t.end(), c.text)
			b.Write(src[t.end():toks[k+1].off])
			b.WriteString(reindent(c.rest, lineIndent(b.Bytes())))
			last = toks[k+1].end()
			k++
		case strings.Trim(t.lit[len(m):], "_") == "":
			c := next('J')
			b.Write(src[last:t.off])
			indent := lineIndent(b.Bytes())
			p := &printer{fm: fm, src: orig}
			text := p.elem(c.el, indent, lineWidth(b.Bytes()))
			w := multiWidth
			if !strings.Contains(text, "\n") {
				w = textWidth(text)
			}
			widths = append(widths, w)
			b.WriteString(text)
			last = t.end()
		}
	}
	b.Write(src[last:])
	for _, q := range queue {
		stray = stray || len(q) > 0
	}
	if stray {
		return nil, nil, errors.New("internal error: placeholders lost in formatting; please report it")
	}
	return b.Bytes(), widths, nil
}

// lineIndent is the indentation of the last line of b.
func lineIndent(b []byte) string {
	line := b[bytes.LastIndexByte(b, '\n')+1:]
	n := 0
	for n < len(line) && line[n] == '\t' {
		n++
	}
	return string(line[:n])
}

// lineWidth is the width of the last line of b, a tab counting 4.
func lineWidth(b []byte) int { return textWidth(string(b[bytes.LastIndexByte(b, '\n')+1:])) }

func textWidth(s string) int {
	n := 0
	for _, r := range s {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// reindent prefixes each line of Go text after the first with indent, except
// lines that start inside a raw string or a comment.
func reindent(text, indent string) string {
	if indent == "" || !strings.Contains(text, "\n") {
		return text
	}
	var keep []span
	for _, t := range scanTokens([]byte(text), 0) {
		if t.tok == token.STRING || t.tok == token.COMMENT {
			keep = append(keep, span{t.off, t.end()})
		}
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		b.WriteByte(text[i])
		if text[i] != '\n' || i+1 == len(text) || text[i+1] == '\n' {
			continue
		}
		inside := false
		for _, s := range keep {
			if i > s.start && i < s.end {
				inside = true
			}
		}
		if !inside {
			b.WriteString(indent)
		}
	}
	return b.String()
}
