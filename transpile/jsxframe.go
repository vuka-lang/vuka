package transpile

import (
	"go/token"
	"slices"
)

// declName is the declaration a top-level func at toks[i] belongs to, for
// Options.Scopes: a method's receiver type (func (c *Counter[T]) Render →
// Counter), a static method's type (func User.New → User), else the
// function's name.
func declName(toks []tok, i int) string {
	j := i + 1
	for j < len(toks) && trivia(toks[j]) {
		j++
	}
	if j >= len(toks) {
		return ""
	}
	switch t := toks[j]; {
	case t.tok == token.LPAREN:
		name, d := "", 0
		for k := j + 1; k < len(toks); k++ {
			switch toks[k].tok {
			case token.LPAREN, token.LBRACK:
				d++
			case token.RBRACK:
				d--
			case token.RPAREN:
				if d == 0 {
					return name
				}
				d--
			case token.IDENT:
				if d == 0 {
					name = toks[k].lit
				}
			}
		}
	case t.tok == token.IDENT:
		return t.lit
	}
	return ""
}

// scopeAttr is the attribute marking an element as the markup of a scoped
// declaration (Options.Scopes).
const scopeAttr = "data-vk-s"

// markScope adds scope's attribute to every element of ns written in the
// source: elements in blocks and in a component tag's children too, but
// not the tags of components, whose markup is their own.
func markScope(ns []jsxNode, scope string) {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxElem:
			if !n.frag && n.comp == nil && !slices.ContainsFunc(n.attrs, func(a *jsxAttr) bool { return a.name == scopeAttr }) {
				n.attrs = append(n.attrs, &jsxAttr{name: scopeAttr, off: n.tagEnd, kind: 's', val: scope})
			}
			markScope(n.kids, scope)
		case *jsxBlock:
			for _, b := range n.bodies {
				markScope(b, scope)
			}
			for _, c := range n.cases {
				markScope(c, scope)
			}
		}
	}
}
