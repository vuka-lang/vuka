package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

type attrKind int

const (
	attrTyped attrKind = iota
	attrDoc
	attrDeprecated
	attrExport
	attrDerive
)

var builtins = map[string]attrKind{
	"doc":        attrDoc,
	"deprecated": attrDeprecated,
	"export":     attrExport,
	"derive":     attrDerive,
}

// Attr is an attribute written before a declaration.
//
// Built-in attributes (@doc, @deprecated, @export) take one string. Any other
// attribute is a Go type, written bare (@Inline) or as a composite literal
// (@cache.Memo{TTL: time.Minute}); the generated code type-checks it.
type Attr struct {
	Name string         // "doc", "Route", "cache.Memo"
	Args string         // what follows the name: `("text")`, `{TTL: x}`, or ""
	Pos  token.Position // where the attribute is written
	Decl string         // the declaration it annotates: "area", "Shape.Scale", "Config"

	kind       attrKind
	start, end int    // the attribute in src
	nameStart  int    // offset of the name, after the @
	declOff    int    // offset of the annotated declaration's keyword in src
	value      string // the string argument of a built-in
	decl       ast.Decl
}

type tok struct {
	off int
	tok token.Token
	lit string
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

func isAt(t tok) bool { return t.tok == token.ILLEGAL && t.lit == "@" }

func isOpen(t token.Token) bool  { return t == token.LPAREN || t == token.LBRACK || t == token.LBRACE }
func isClose(t token.Token) bool { return t == token.RPAREN || t == token.RBRACK || t == token.RBRACE }

// scanAttrs finds the file's attributes. Only tokens are read, so everything that
// isn't an attribute is left exactly as the installed Go toolchain sees it.
func (f *fileState) scanAttrs(errs *ErrorList) {
	toks := scanTokens(f.src)
	depth, lastEnd := 0, -1
	var pending []*Attr
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case isOpen(t.tok):
			depth++
			continue
		case isClose(t.tok):
			depth--
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
		for k < len(toks) && (toks[k].tok == token.SEMICOLON && toks[k].lit == "\n" || toks[k].tok == token.COMMENT) {
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
	}
}

func startsLine(src []byte, off, lastAttrEnd int) bool {
	ls := lineStart(src, off)
	if strings.TrimSpace(string(src[ls:off])) == "" {
		return true
	}
	return lastAttrEnd >= ls && strings.TrimSpace(string(src[lastAttrEnd:off])) == ""
}

// parseAttr reads the attribute whose @ is toks[i]. It returns the index of the
// first token after it.
func (f *fileState) parseAttr(toks []tok, i int) (*Attr, int, string) {
	at := toks[i].off
	j := i + 1
	if j >= len(toks) || toks[j].tok != token.IDENT || toks[j].off != at+1 {
		return nil, j, "expected an attribute name after @"
	}
	name := toks[j].lit
	end := toks[j].off + len(name)
	j++
	if j+1 < len(toks) && toks[j].tok == token.PERIOD && toks[j+1].tok == token.IDENT {
		name += "." + toks[j+1].lit
		end = toks[j+1].off + len(toks[j+1].lit)
		j += 2
	}
	nameEnd := end
	if j < len(toks) && toks[j].tok == token.LBRACK && toks[j].off == end {
		var ok bool
		if j, end, ok = matchClose(toks, j); !ok {
			return nil, j, "unclosed [ in @" + name
		}
	}
	if j < len(toks) && (toks[j].tok == token.LPAREN || toks[j].tok == token.LBRACE) {
		var ok bool
		if j, end, ok = matchClose(toks, j); !ok {
			return nil, j, "unclosed argument list in @" + name
		}
	}
	a := &Attr{
		Name:      name,
		Args:      string(f.src[nameEnd:end]),
		Pos:       f.at(at),
		start:     at,
		end:       end,
		nameStart: at + 1,
	}
	if kind, ok := builtins[name]; ok {
		a.kind = kind
		if kind == attrDerive {
			return nil, j, "@derive is not implemented yet"
		}
		v, ok := stringArg(a.Args)
		if !ok {
			return nil, j, "@" + name + ` takes one string: @` + name + `("...")`
		}
		if kind == attrExport && (!token.IsIdentifier(v) || v == "_") {
			return nil, j, "@export needs an identifier, got " + strconv.Quote(v)
		}
		a.value = v
		return a, j, ""
	}
	if strings.HasPrefix(strings.TrimSpace(a.Args), "(") {
		return nil, j, "an attribute is a type, written @" + name + " or @" + name + "{...}"
	}
	return a, j, ""
}

func matchClose(toks []tok, j int) (int, int, bool) {
	d := 0
	for ; j < len(toks); j++ {
		switch {
		case isOpen(toks[j].tok):
			d++
		case isClose(toks[j].tok):
			if d--; d == 0 {
				return j + 1, toks[j].off + 1, true
			}
		}
	}
	return j, 0, false
}

func stringArg(args string) (string, bool) {
	e, err := parser.ParseExpr(args)
	if err != nil {
		return "", false
	}
	p, ok := e.(*ast.ParenExpr)
	if !ok {
		return "", false
	}
	lit, ok := p.X.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// replacement is what the attribute becomes in place: comments spanning exactly
// the lines it spanned, so every line after it keeps its number.
func (a *Attr) replacement(src []byte) string {
	n := strings.Count(string(src[a.start:a.end]), "\n")
	var text []string
	switch a.kind {
	case attrDoc:
		text = strings.Split(a.value, "\n")
	case attrDeprecated:
		text = strings.Split("Deprecated: "+a.value, "\n")
	}
	if restOfLineHasCode(src, a.end) {
		s := strings.ReplaceAll(strings.Join(text, " "), "*/", "* /")
		if s != "" {
			s = " " + s + " "
		}
		return "/*" + s + strings.Repeat("\n", n) + "*/"
	}
	lines := make([]string, n+1)
	if len(text) > n+1 {
		text = append(text[:n], strings.Join(text[n:], " "))
	}
	copy(lines[n+1-len(text):], text)
	return commentLines(lines)
}

// check is the declaration that makes the Go compiler type-check a typed
// attribute, split around the point where its source text begins.
func (a *Attr) check(src []byte) (prefix, expr string) {
	expr = string(src[a.nameStart:a.end])
	if strings.HasSuffix(expr, "}") {
		return "var _ = ", expr
	}
	return "var _ ", expr
}
