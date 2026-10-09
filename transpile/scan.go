package transpile

import (
	"go/scanner"
	"go/token"
	"strings"
)

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

func scanTokens(src []byte) []tok {
	fset := token.NewFileSet()
	f := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, func(token.Position, string) {}, scanner.ScanComments)
	var out []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out
		}
		out = append(out, tok{f.Offset(pos), t, lit})
	}
}

func isAt(t tok) bool       { return t.tok == token.ILLEGAL && t.lit == "@" }
func isQuestion(t tok) bool { return t.tok == token.ILLEGAL && t.lit == "?" }

func isOpen(t token.Token) bool  { return t == token.LPAREN || t == token.LBRACK || t == token.LBRACE }
func isClose(t token.Token) bool { return t == token.RPAREN || t == token.RBRACK || t == token.RBRACE }

func trivia(t tok) bool { return t.tok == token.COMMENT || t.tok == token.SEMICOLON && t.lit == "\n" }

type span struct{ start, end int }

// try is a ? that ends a statement: x := f()?, return f()?, f()?.
type try struct {
	off  int // offset of the ?
	n    int // ordinal in the file, for temporaries
	done bool
	dead bool   // reported as an error
	fail string // why the last round couldn't lower it
}

type matchCase struct {
	start int    // offset of case or default
	pats  []span // the patterns, split at top-level commas; none for default
	guard span   // the guard after if; start < 0 when there is none
	colon int
}

// matchStmt is `match subject { case pattern [if guard]: … }`.
type matchStmt struct {
	start          int // offset of the match keyword
	subj           span
	lbrace, rbrace int
	cases          []*matchCase
	n              int
	done, dead     bool
	fail           string
}

// scan finds the file's Vuka constructs. Only tokens are read, so everything
// else is left exactly as the installed Go toolchain sees it.
func (f *fileState) scan(errs *ErrorList) {
	toks := scanTokens(f.src)
	depth, lastEnd := 0, -1
	var pending []*Attr
	prev := tok{tok: token.SEMICOLON}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.tok == token.COMMENT {
			continue
		}
		p := prev
		prev = t
		switch {
		case isOpen(t.tok):
			depth++
			continue
		case isClose(t.tok):
			depth--
			continue
		case isQuestion(t):
			if p.end() != t.off || p.tok == token.SEMICOLON {
				errs.add(f.at(t.off), "unexpected ?: it goes right after the expression it unwraps, as in f()?")
				continue
			}
			f.tries = append(f.tries, &try{off: t.off, n: len(f.tries) + 1})
			continue
		case t.tok == token.IDENT && t.lit == "match" && depth > 0 && (p.tok == token.SEMICOLON || p.tok == token.LBRACE || p.tok == token.COLON):
			if m, msg := f.matchAt(toks, i); m != nil {
				m.n = len(f.matches) + 1
				f.matches = append(f.matches, m)
			} else if msg != "" {
				errs.add(f.at(t.off), "%s", msg)
			}
			continue
		case !isAt(t):
			continue
		}

		a, next, msg := f.parseAttr(toks, i)
		if msg != "" {
			errs.add(f.at(t.off), "%s", msg)
			i = next - 1
			continue
		}
		switch {
		case depth > 0:
			errs.add(a.Pos, "attributes are only allowed before top-level declarations")
			i = next - 1
			continue
		case !startsLine(f.src, a.start, lastEnd):
			errs.add(a.Pos, "an attribute must start its own line")
		}
		lastEnd = a.end
		pending = append(pending, a)

		k := next
		for k < len(toks) && trivia(toks[k]) {
			k++
		}
		switch {
		case k < len(toks) && isAt(toks[k]):
		case k < len(toks) && (toks[k].tok == token.FUNC || toks[k].tok == token.TYPE || toks[k].tok == token.VAR || toks[k].tok == token.CONST):
			for _, p := range pending {
				p.declOff = toks[k].off
				f.attrs = append(f.attrs, p)
			}
			pending = nil
		default:
			for _, p := range pending {
				errs.add(p.Pos, "@%s must be followed by a declaration (func, type, var or const)", p.Name)
			}
			pending = nil
		}
		i = k - 1
		prev = toks[i]
	}
}

func startsLine(src []byte, off, lastAttrEnd int) bool {
	ls := lineStart(src, off)
	if strings.TrimSpace(string(src[ls:off])) == "" {
		return true
	}
	return lastAttrEnd >= ls && strings.TrimSpace(string(src[lastAttrEnd:off])) == ""
}

// matchAt reads the match statement whose keyword is toks[i]. A nil result with
// no message means the identifier is ordinary Go (a variable named match).
func (f *fileState) matchAt(toks []tok, i int) (*matchStmt, string) {
	j := i + 1
	if j >= len(toks) {
		return nil, ""
	}
	switch toks[j].tok {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING,
		token.LPAREN, token.MUL, token.AND, token.NOT, token.SUB, token.ADD, token.XOR, token.FUNC, token.LBRACK:
	default:
		return nil, ""
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
		return nil, ""
	}
	n := k + 1
	for n < len(toks) && trivia(toks[n]) {
		n++
	}
	if n >= len(toks) || toks[n].tok != token.CASE && toks[n].tok != token.DEFAULT && toks[n].tok != token.RBRACE {
		return nil, ""
	}
	m := &matchStmt{start: toks[i].off, subj: span{toks[j].off, toks[k-1].end()}, lbrace: toks[k].off}

	depth = 0
	for c := n; c < len(toks); c++ {
		t := toks[c]
		switch {
		case isOpen(t.tok):
			depth++
			continue
		case isClose(t.tok):
			if depth == 0 {
				m.rbrace = t.off
				if len(m.cases) == 0 {
					return nil, "a match needs at least one case"
				}
				return m, ""
			}
			depth--
			continue
		case depth != 0 || t.tok != token.CASE && t.tok != token.DEFAULT:
			continue
		}
		mc := &matchCase{start: t.off, guard: span{-1, -1}}
		d, patStart := 0, -1
		c++
		if t.tok == token.CASE {
			patStart = toks[c].off
		}
		for ; c < len(toks); c++ {
			h := toks[c]
			switch {
			case isOpen(h.tok):
				d++
			case isClose(h.tok):
				d--
			case d == 0 && h.tok == token.COMMA && mc.guard.start < 0:
				mc.pats = append(mc.pats, span{patStart, toks[c-1].end()})
				patStart = toks[c+1].off
			case d == 0 && h.tok == token.IF && mc.guard.start < 0:
				if patStart >= 0 {
					mc.pats = append(mc.pats, span{patStart, toks[c-1].end()})
					patStart = -1
				}
				mc.guard.start = toks[c+1].off
			case d == 0 && h.tok == token.COLON:
				if mc.guard.start >= 0 {
					mc.guard.end = toks[c-1].end()
				} else if patStart >= 0 {
					mc.pats = append(mc.pats, span{patStart, toks[c-1].end()})
				}
				mc.colon = h.off
			}
			if mc.colon > 0 {
				break
			}
		}
		if mc.colon == 0 {
			return nil, "unterminated case in match"
		}
		if t.tok == token.CASE && len(mc.pats) == 0 {
			return nil, "a case needs a pattern; use case _: for anything"
		}
		m.cases = append(m.cases, mc)
	}
	return nil, "unterminated match"
}
