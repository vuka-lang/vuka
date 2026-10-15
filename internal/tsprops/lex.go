// Package tsprops reads the props a client component declares — a Vue SFC's
// defineProps/defineModel/defineEmits, a React component's props parameter, a
// Svelte component's $props(), a vanilla island's mount(el, props, ctx) — from
// its source, without Node: a small reader of the TypeScript types people
// write for props. What it can't read it reports, so the caller can say to
// declare the props in Go instead.
package tsprops

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tString
	tNumber
	tTemplate
	tPunct
	tRegexp
)

type token struct {
	kind tokKind
	text string // the punctuation, identifier or literal text (a string's value unquoted)
	off  int    // offset in the source
	nl   bool   // a newline precedes it
}

// lex splits TypeScript (or JavaScript) source into tokens, comments and
// whitespace dropped. It is forgiving: what it doesn't know becomes a
// punctuation token of one character, and a string ends at its line's end.
func lex(src string) []token {
	var toks []token
	nl := false
	prevSignificant := func() *token {
		if len(toks) == 0 {
			return nil
		}
		return &toks[len(toks)-1]
	}
	regexOK := func() bool {
		p := prevSignificant()
		if p == nil {
			return true
		}
		switch p.kind {
		case tIdent:
			switch p.text {
			case "return", "typeof", "case", "in", "of", "new", "delete", "void", "throw", "instanceof", "yield", "await":
				return true
			}
			return false
		case tString, tNumber, tTemplate, tRegexp:
			return false
		case tPunct:
			return p.text != ")" && p.text != "]" && p.text != "}"
		}
		return true
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			nl = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
				continue
			}
			if strings.Contains(src[i:i+2+end], "\n") {
				nl = true
			}
			i += end + 4
			continue
		}
		start := i
		t := token{off: start, nl: nl}
		nl = false
		switch {
		case c == '"' || c == '\'':
			var b strings.Builder
			i++
			for i < len(src) && src[i] != c && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) {
					i++
					switch src[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(src[i])
					}
					i++
					continue
				}
				b.WriteByte(src[i])
				i++
			}
			if i < len(src) && src[i] == c {
				i++
			}
			t.kind, t.text = tString, b.String()
		case c == '`':
			i++
			depth := 0
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if depth == 0 && src[i] == '`' {
					i++
					break
				}
				if src[i] == '$' && i+1 < len(src) && src[i+1] == '{' {
					depth++
					i += 2
					continue
				}
				if depth > 0 && src[i] == '}' {
					depth--
				}
				i++
			}
			t.kind, t.text = tTemplate, src[start:min(i, len(src))]
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			for i < len(src) && (isIdentByte(src[i]) || src[i] == '.') {
				i++
			}
			t.kind, t.text = tNumber, src[start:i]
		case isIdentStart(src, i):
			for i < len(src) {
				r, n := utf8.DecodeRuneInString(src[i:])
				if !(r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
					break
				}
				i += n
			}
			t.kind, t.text = tIdent, src[start:i]
		case c == '/' && regexOK():
			i++
			class := false
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == '[' {
					class = true
				} else if src[i] == ']' {
					class = false
				} else if src[i] == '/' && !class {
					i++
					break
				}
				i++
			}
			for i < len(src) && isIdentByte(src[i]) {
				i++
			}
			t.kind, t.text = tRegexp, src[start:min(i, len(src))]
		default:
			t.kind = tPunct
			for _, p := range []string{"...", "=>", "?.", "??", "==", "!=", "<=", ">=", "&&", "||"} {
				if strings.HasPrefix(src[i:], p) {
					t.text = p
					break
				}
			}
			if t.text == "" {
				t.text = src[i : i+1]
			}
			i += len(t.text)
		}
		toks = append(toks, t)
	}
	return append(toks, token{kind: tEOF, off: len(src)})
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isIdentStart(src string, i int) bool {
	r, _ := utf8.DecodeRuneInString(src[i:])
	return r == '_' || r == '$' || unicode.IsLetter(r)
}
