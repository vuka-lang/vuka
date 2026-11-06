package main

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// tagVariantValues are the values a component tag's attribute takes when
// the component passes the prop it sets to a ui.Variants
// (alert.Class("tone", Tone)): the variant's values, from the component's
// package, the file's own or the one its import names.
func (p *proxy) tagVariantValues(file string, src []byte, tag, attr string) []string {
	dir := filepath.Dir(file)
	name := tag
	if i := strings.LastIndexByte(tag, '.'); i >= 0 {
		qual := tag[:i]
		name, dir = tag[i+1:], ""
		for _, im := range sourceImports(file, src) {
			if im.Name == qual || im.Name == "" && path.Base(tagPathMajor.ReplaceAllString(im.Path, "")) == qual {
				dir = p.importDir(im.Path)
			}
		}
		if dir == "" {
			return nil
		}
	}
	return variantValues(packageSources(dir, file, src), name, exportField(attr))
}

func exportField(attr string) string {
	if attr == "" {
		return ""
	}
	return strings.ToUpper(attr[:1]) + attr[1:]
}

// packageSources are the .go, .vuka and .vuka.html files of dir, the file
// being edited as the editor holds it.
func packageSources(dir, file string, src []byte) map[string][]byte {
	out := map[string][]byte{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || !strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		path := filepath.Join(dir, n)
		if path == file {
			out[path] = src
		} else if b, err := os.ReadFile(path); err == nil {
			out[path] = b
		}
	}
	return out
}

var classCall = regexp.MustCompile(`\b(\w+)\.Class\(`)

// variantValues are the values of the variant typeName's field picks: in
// the file declaring the type (else the package's others), a call
// v.Class(…, "tone", Tone, …) or (…, "tone", x.Tone, …), and the keys of
// Variants["tone"] in the ui.Variants literal the package declares v as.
func variantValues(srcs map[string][]byte, typeName, field string) []string {
	if field == "" {
		return nil
	}
	decl := regexp.MustCompile(`\btype\s+` + regexp.QuoteMeta(typeName) + `\b`)
	var order [][]byte
	for _, src := range srcs {
		if decl.Match(src) {
			order = append([][]byte{src}, order...)
		} else {
			order = append(order, src)
		}
	}
	for _, src := range order {
		for _, m := range classCall.FindAllSubmatchIndex(src, -1) {
			dim := variantOf(src, m[1]-1, field)
			if dim == "" {
				continue
			}
			for _, s := range srcs {
				if vs := variantKeys(s, string(src[m[2]:m[3]]), dim); vs != nil {
					return vs
				}
			}
		}
	}
	return nil
}

// variantOf is the variant name a Class call's arguments, from its ( at
// open, pass field as: "tone" in ("tone", Tone) or ("tone", a.Tone).
func variantOf(src []byte, open int, field string) string {
	var toks []scanTok
	scanBalanced(src, open, func(t scanTok) { toks = append(toks, t) })
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].tok != token.STRING || toks[i+1].tok != token.COMMA {
			continue
		}
		j := i + 2
		if j+2 < len(toks) && toks[j].tok == token.IDENT && toks[j+1].tok == token.PERIOD {
			j += 2
		}
		if toks[j].tok == token.IDENT && toks[j].lit == field && (j+1 == len(toks) || toks[j+1].tok == token.COMMA || toks[j+1].tok == token.RPAREN) {
			name, _ := strconv.Unquote(toks[i].lit)
			return name
		}
	}
	return ""
}

type scanTok struct {
	tok token.Token
	lit string
	off int
}

// scanBalanced passes f the tokens from the bracket at open to the one
// closing it; it returns the offset after that one, -1 when it isn't closed.
func scanBalanced(src []byte, open int, f func(scanTok)) int {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src)-open)
	var s scanner.Scanner
	s.Init(file, src[open:], nil, scanner.ScanComments)
	depth := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return -1
		}
		off := open + file.Offset(pos)
		if lit == "" {
			lit = tok.String()
		}
		if f != nil {
			f(scanTok{tok, lit, off})
		}
		switch tok {
		case token.LPAREN, token.LBRACE, token.LBRACK:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACK:
			if depth--; depth == 0 {
				return off + 1
			}
		}
	}
}

// variantKeys are the keys of Variants[dim] in src's declaration of the
// ui.Variants v, in order; nil when src doesn't declare it.
func variantKeys(src []byte, v, dim string) []string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\s*=\s*(?:\w+\.)?Variants\s*\{`)
	m := re.FindIndex(src)
	if m == nil {
		return nil
	}
	start := bytesIndexFrom(src, m[0], '=') + 1
	end := scanBalanced(src, m[1]-1, nil)
	if end < 0 {
		return nil
	}
	x, err := parser.ParseExpr(string(src[start:end]))
	lit, _ := x.(*ast.CompositeLit)
	if err != nil || lit == nil {
		return nil
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if id, _ := kv.Key.(*ast.Ident); !ok || id == nil || id.Name != "Variants" {
			continue
		}
		dims, _ := kv.Value.(*ast.CompositeLit)
		if dims == nil {
			return nil
		}
		for _, d := range dims.Elts {
			kv, ok := d.(*ast.KeyValueExpr)
			k, _ := kv.Key.(*ast.BasicLit)
			if !ok || k == nil || k.Kind != token.STRING || unquote(k.Value) != dim {
				continue
			}
			values, _ := kv.Value.(*ast.CompositeLit)
			if values == nil {
				return nil
			}
			out := []string{}
			for _, e := range values.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.BasicLit); ok && k.Kind == token.STRING {
						out = append(out, unquote(k.Value))
					}
				}
			}
			return out
		}
	}
	return nil
}

func unquote(s string) string {
	u, _ := strconv.Unquote(s)
	return u
}

func bytesIndexFrom(b []byte, from int, c byte) int {
	for i := from; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return from
}
