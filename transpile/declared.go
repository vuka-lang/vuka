package transpile

import (
	"go/token"
	"strings"
)

// Declared is what the .vuka or .vuka.html file name declares at its top
// level, read from its tokens alone, so a file that doesn't transpile still
// says: its types, funcs, vars and consts, an enum's Go names, and its
// methods as Type.Method.
func Declared(name string, src []byte) []string {
	if strings.HasSuffix(name, ".vuka.html") {
		parts, err := SplitTemplate(src)
		if err != nil {
			return nil
		}
		src = src[parts.GoStart:parts.GoEnd]
	}
	var toks []tok
	for _, t := range scanTokens(src) {
		if t.tok != token.COMMENT {
			toks = append(toks, t)
		}
	}
	var out []string
	ident := func(i int) string {
		if i < len(toks) && toks[i].tok == token.IDENT {
			return toks[i].lit
		}
		return ""
	}
	// close is the index of the token closing the one opening at i.
	close := func(i int) int {
		depth := 0
		for j := i; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LPAREN, token.LBRACE, token.LBRACK:
				depth++
			case token.RPAREN, token.RBRACE, token.RBRACK:
				if depth--; depth == 0 {
					return j
				}
			}
		}
		return len(toks)
	}
	// specs are the names starting each spec of the group opening at i, or
	// of the one spec at i: a, b of var a, b = …
	specs := func(i int) int {
		from, end := i, i
		if toks[i].tok == token.LPAREN {
			from, end = i+1, close(i)
		} else {
			for end < len(toks) && toks[end].tok != token.SEMICOLON {
				if k := toks[end].tok; k == token.LPAREN || k == token.LBRACE || k == token.LBRACK {
					end = close(end)
				}
				end++
			}
		}
		start := true
		for j := from; j < end && j < len(toks); j++ {
			switch toks[j].tok {
			case token.LPAREN, token.LBRACE, token.LBRACK:
				j = close(j)
				start = false
				continue
			case token.SEMICOLON:
				start = true
				continue
			}
			if !start {
				continue
			}
			if n := ident(j); n != "" {
				out = append(out, n)
				if j+1 < len(toks) && toks[j+1].tok == token.COMMA {
					j++
					continue
				}
			}
			start = false
		}
		return end
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.tok == token.LPAREN || t.tok == token.LBRACE || t.tok == token.LBRACK:
			i = close(i)
		case t.tok == token.FUNC && i+1 < len(toks) && toks[i+1].tok == token.LPAREN:
			end := close(i + 1)
			recv := ""
			for j := i + 2; j < end; j++ {
				if toks[j].tok == token.LBRACK {
					j = close(j)
				} else if n := ident(j); n != "" {
					recv = n
				}
			}
			if n := ident(end + 1); n != "" && recv != "" {
				out = append(out, recv+"."+n)
			}
			i = end
		case t.tok == token.FUNC:
			if n := ident(i + 1); n != "" {
				out = append(out, n)
			}
		case t.tok == token.TYPE || t.tok == token.VAR || t.tok == token.CONST:
			if i+1 < len(toks) {
				i = specs(i+1) - 1
			}
		case isEnumKeyword(toks, i):
			enum := toks[i+1].lit
			out = append(out, enum, enum+"Values", parseName(enum))
			j := i + 2
			for j < len(toks) && toks[j].tok != token.LBRACE {
				j++
			}
			end := close(j)
			start := true
			for k := j + 1; k < end; k++ {
				switch toks[k].tok {
				case token.LBRACE, token.LPAREN, token.LBRACK:
					k = close(k)
				case token.COMMA, token.SEMICOLON:
					start = true
				case token.IDENT:
					if start {
						out = append(out, enum+toks[k].lit)
					}
					start = false
				default:
					start = false
				}
			}
			i = end
		}
	}
	return out
}
