package transpile

import (
	"go/scanner"
	"go/token"
	"strconv"
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

// scanFrom scans src from off. After an operand (JSX), a newline ends the
// statement, as it would after the ) the scanner is shown in its place.
func scanFrom(src []byte, off int, afterOperand bool) []tok {
	base, buf := off, src[off:]
	if afterOperand {
		base, buf = off-1, append([]byte{')'}, src[off:]...)
	}
	ts := scanTokens(buf)
	if afterOperand && len(ts) > 0 {
		ts = ts[1:]
	}
	for i := range ts {
		ts[i].off += base
	}
	return ts
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
	// fresh is set in the round that lowered it: that round's types still
	// see the operand, not what ? unwraps it to.
	fresh bool
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
	var pending, pendingParams []*Attr
	fstage, paramAttrEnd := 0, -1 // fstage: 1 inside a top-level func declaration's head, before its parameters
	type frame struct {
		typeGroup  bool
		structBody bool
		params     bool   // a top-level func declaration's parameter list
		structOf   string // the named type whose struct body this is
		tparams    string
		names      []string
	}
	var frames []frame
	top := func() frame {
		if len(frames) == 0 {
			return frame{}
		}
		return frames[len(frames)-1]
	}
	prev, prevIdx := tok{tok: token.SEMICOLON}, -1
	defer func() { f.lowerStatics(toks, f.bare) }()
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.tok == token.COMMENT {
			continue
		}
		p, pIdx := prev, prevIdx
		prev, prevIdx = t, i
		switch {
		case isOpen(t.tok):
			fr := frame{typeGroup: t.tok == token.LPAREN && p.tok == token.TYPE}
			if depth == 0 && fstage == 1 {
				switch {
				case t.tok == token.LPAREN && (p.tok == token.IDENT || p.tok == token.RBRACK):
					fr.params, fstage = true, 0
				case t.tok == token.LBRACE:
					fstage = 0
				}
			}
			if t.tok == token.LBRACE && p.tok == token.STRUCT {
				fr.structBody = true
				if name, tp, names, ok := namedStruct(f.src, toks, pIdx, top().typeGroup); ok {
					fr.structOf, fr.tparams, fr.names = name, tp, names
				}
			}
			frames = append(frames, fr)
			depth++
			continue
		case isClose(t.tok):
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
			depth--
			continue
		case t.tok == token.IDENT && t.lit == "static" && top().structOf != "" && (p.tok == token.SEMICOLON || p.tok == token.LBRACE):
			fr := top()
			sd, last, msg := f.staticAt(toks, i, fr.structOf, fr.tparams, fr.names)
			if msg != "" {
				errs.add(f.at(t.off), "%s", msg)
				continue
			}
			f.statics = append(f.statics, sd)
			i, prev, prevIdx = last, toks[last], last
			continue
		case t.tok == token.FUNC && depth == 0:
			if p.tok != token.ASSIGN && p.tok != token.COMMA && p.tok != token.DEFINE {
				fstage = 1
			}
			if sf, ok := f.staticFuncAt(toks, i); ok {
				f.staticFuncs = append(f.staticFuncs, sf)
			}
			continue
		case f.bodyConstruct(toks, i, p, depth, errs):
			continue
		case t.tok == token.IDENT && t.lit == "decorator" && depth == 0 && i+1 < len(toks) && toks[i+1].tok == token.IDENT:
			next, msg := f.decoratorAt(toks, i)
			if msg != "" {
				errs.add(f.at(t.off), "%s", msg)
			}
			if next > i+1 {
				i = min(next, len(toks)) - 1
				prev = toks[i]
			}
			continue
		case t.tok == token.LSS && (depth > 0 || p.tok == token.ASSIGN) && jsxStarts(p.tok) && jsxTagAt(f.src, t.off):
			f.jsxTarget(toks, t.off, errs)
			end, goToks, ok := f.parseJSX(t.off, errs)
			if !ok {
				// Resume at the next top-level func: what follows the bad
				// JSX isn't Go.
				next := strings.Index(string(f.src[t.off:]), "\nfunc ")
				if next < 0 {
					toks = toks[:i:i]
					continue
				}
				toks = append(toks[:i:i], scanFrom(f.src, t.off+next+1, false)...)
				depth, frames, prev, prevIdx = 0, nil, tok{tok: token.SEMICOLON}, -1
				i--
				continue
			}
			for _, g := range goToks {
				f.scanJSXGo(g, errs)
			}
			toks = append(toks[:i:i], append([]tok{{t.off, token.IDENT, string(f.src[t.off:end])}}, scanFrom(f.src, end, true)...)...)
			prev = toks[i]
			continue
		case t.tok == token.SEMICOLON && depth == 0:
			fstage = 0
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
		switch fr := top(); {
		case depth > 0 && fr.structBody:
			topLevel := len(frames) == 1 || len(frames) == 2 && frames[0].typeGroup
			if msg := f.fieldAttr(toks, a, p, next, fr.structOf, fr.tparams, topLevel); msg != "" {
				errs.add(a.Pos, "%s", msg)
			}
			i = next - 1
			prev = toks[i]
			continue
		case depth == 1 && fr.params:
			if msg := f.paramAttr(a, p, paramAttrEnd); msg != "" {
				errs.add(a.Pos, "%s", msg)
			}
			paramAttrEnd = a.end
			pendingParams = append(pendingParams, a)
			f.attrs = append(f.attrs, a)
			k := next
			for k < len(toks) && trivia(toks[k]) {
				k++
			}
			switch {
			case k < len(toks) && isAt(toks[k]):
			case k < len(toks) && toks[k].tok != token.COMMA && toks[k].tok != token.RPAREN:
				for _, pa := range pendingParams {
					pa.paramOff = toks[k].off
				}
				pendingParams = nil
			default:
				for _, pa := range pendingParams {
					errs.add(pa.Pos, "@%s must be followed by a parameter: func f(@%s id int)", pa.Name, pa.Name)
				}
				pendingParams = nil
			}
			i = next - 1
			prev = toks[i]
			continue
		case depth > 0:
			errs.add(a.Pos, "attributes are only allowed before top-level declarations, their parameters, and after struct fields")
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

// bodyConstruct reads the ? or match at toks[i], after p, if it is one.
func (f *fileState) bodyConstruct(toks []tok, i int, p tok, depth int, errs *ErrorList) bool {
	switch t := toks[i]; {
	case isQuestion(t):
		if p.end() != t.off || p.tok == token.SEMICOLON {
			errs.add(f.at(t.off), "unexpected ?: it goes right after the expression it unwraps, as in f()?")
			return true
		}
		f.tries = append(f.tries, &try{off: t.off, n: len(f.tries) + 1})
		return true
	case t.tok == token.IDENT && t.lit == "match" && depth > 0 && (p.tok == token.SEMICOLON || p.tok == token.LBRACE || p.tok == token.COLON):
		if m, msg := f.matchAt(toks, i); m != nil {
			m.n = len(f.matches) + 1
			f.matches = append(f.matches, m)
		} else if msg != "" {
			errs.add(f.at(t.off), "%s", msg)
		}
		return true
	}
	return false
}

// scanJSXGo finds the ? and match in one Go expression of a JSX tree, such as
// a function literal in {…}: its tokens, with the JSX it holds read as one.
func (f *fileState) scanJSXGo(toks []tok, errs *ErrorList) {
	depth, prev := 1, tok{tok: token.LPAREN}
	for i, t := range toks {
		p := prev
		prev = t
		switch {
		case isOpen(t.tok):
			depth++
		case isClose(t.tok):
			depth--
		case isAt(t):
			errs.add(f.at(t.off), "attributes are only allowed before top-level declarations")
		default:
			f.bodyConstruct(toks, i, p, depth, errs)
		}
	}
}

// decoratorAt lowers the decorator declaration whose keyword is toks[i]:
//
//	decorator logged(c) { … }          →  func logged(c *vuka.Call) { … }
//	decorator retry(n int)(c) { … }    →  func retry(n int) vuka.Decorator { return func(c *vuka.Call) { … } }
//	decorator api(p string) = @a(p) @b →  func api(p string) vuka.Bundle { return vuka.Compose(a(p), b) }
//
// It returns the index of the token after the body.
func (f *fileState) decoratorAt(toks []tok, i int) (int, string) {
	j := i + 2
	if j < len(toks) && toks[j].tok == token.ASSIGN {
		return f.bundleAt(toks, i, j, false)
	}
	if j >= len(toks) || toks[j].tok != token.LPAREN {
		return i + 1, "expected decorator name(c) { … }"
	}
	first, firstEnd, ok := matchClose(toks, j)
	if !ok {
		return i + 1, "unclosed parameter list"
	}
	if first < len(toks) && toks[first].tok == token.ASSIGN {
		return f.bundleAt(toks, i, first, true)
	}
	var callParam []tok // the (c) group
	var callOpen, callClose int
	switch {
	case first < len(toks) && toks[first].tok == token.LPAREN:
		second, secondEnd, ok := matchClose(toks, first)
		if !ok {
			return i + 1, "unclosed parameter list"
		}
		callParam, callOpen, callClose = toks[first+1:second-1], toks[first].off, secondEnd
		first = second
	default:
		callParam, callOpen, callClose = toks[j+1:first-1], toks[j].off, firstEnd
	}
	if len(callParam) != 1 || callParam[0].tok != token.IDENT {
		return i + 1, "a decorator takes the call as one name: decorator logged(c) { … }"
	}
	if first >= len(toks) || toks[first].tok != token.LBRACE {
		return i + 1, "expected the decorator's body"
	}
	bodyEnd, _, ok := matchClose(toks, first)
	if !ok {
		return i + 1, "unclosed decorator body"
	}
	rt := f.scannedRuntime(toks)
	c := callParam[0].lit
	f.add(toks[i].off, toks[i].off+len("decorator"), "func")
	if callOpen == toks[j].off {
		f.add(callOpen, callClose, "("+c+" *"+rt+".Call)")
	} else {
		f.add(callOpen, toks[first].off+1, " "+rt+".Decorator { return func("+c+" *"+rt+".Call) {")
		rb := toks[bodyEnd-1].off
		f.add(rb, rb+1, "}}")
	}
	return bodyEnd, ""
}

// scannedRuntime is the runtime's name in f, read from the tokens before the
// file can be parsed; the import is added when f lacks it.
func (f *fileState) scannedRuntime(toks []tok) string {
	if f.rt != "" {
		return f.rt
	}
	pkgEnd := -1
	for k, t := range toks {
		switch {
		case t.tok == token.PACKAGE && k+1 < len(toks):
			pkgEnd = toks[k+1].end()
		case t.tok == token.STRING && t.lit == strconv.Quote(RuntimePath):
			f.rt = "vuka"
			if k > 0 && toks[k-1].tok == token.IDENT {
				f.rt = toks[k-1].lit
			}
			return f.rt
		}
	}
	f.rt = "vuka"
	f.insert(pkgEnd, `; import vuka "`+RuntimePath+`"`, 0)
	return f.rt
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
