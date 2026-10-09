package transpile

import (
	"go/ast"
	"go/parser"
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
	attrDecorator
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
	// Decorator is set for a decorator (a function wrapping the declaration)
	// rather than a typed attribute (metadata).
	Decorator bool

	kind       attrKind
	start, end int    // the attribute in src
	nameStart  int    // offset of the name, after the @
	declOff    int    // offset of the annotated declaration's keyword in src
	value      string // the string argument of a built-in
	bare       bool   // written @name or @name[T]: a type or a decorator, decided by what it names
	decl       ast.Decl
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
	switch args := strings.TrimSpace(a.Args); {
	case strings.HasSuffix(args, ")"):
		a.kind, a.Decorator = attrDecorator, true
	case !strings.HasSuffix(args, "}"):
		a.bare = true
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
	if strings.HasSuffix(expr, "}") || strings.HasSuffix(expr, ")") {
		return "var _ = ", expr
	}
	return "var _ ", expr
}
