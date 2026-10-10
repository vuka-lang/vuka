package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// bundleDecl is a composed decorator:
//
//	decorator ApiRoute(path string) = @web.Get(path) @web.Use(auth) @timed
//
// It becomes a function returning a vuka.Bundle, so other packages use it as
// they use any decorator, and its elements are type-checked where it is
// declared:
//
//	func ApiRoute(path string) vuka.Bundle { return vuka.Compose(web.Get(path), web.Use(auth), timed) }
type bundleDecl struct {
	name  string
	off   int // the name's offset
	elems []*bundleElem
}

type bundleElem struct {
	a       *Attr
	decided bool // its form is known: a value as written, a type's zero value, or a factory called
}

// bundleAt lowers the composed decorator whose keyword is toks[i]; eq is the
// index of its =, and params says whether a parameter list precedes it. It
// returns the index of the token after it.
func (f *fileState) bundleAt(toks []tok, i, eq int, params bool) (int, string) {
	b := &bundleDecl{name: toks[i+1].lit, off: toks[i+1].off}
	k, paren := eq+1, false
	if k < len(toks) && toks[k].tok == token.LPAREN {
		paren = true
		k++
	}
	end := len(toks)
	for k < len(toks) {
		if paren && trivia(toks[k]) {
			k++
			continue
		}
		if paren && toks[k].tok == token.RPAREN {
			end = k
			break
		}
		if !isAt(toks[k]) {
			if !paren && (toks[k].tok == token.SEMICOLON || toks[k].tok == token.COMMENT) {
				break
			}
			return k, "a composed decorator is attributes and decorators, each starting with @: decorator " + b.name + " = @logged @timed"
		}
		a, next, msg := f.parseAttr(toks, k)
		if msg != "" {
			return next, msg
		}
		if a.kind != attrTyped && a.kind != attrDecorator {
			return next, "@" + a.Name + " is for declarations; it can't be composed into a decorator"
		}
		b.elems = append(b.elems, &bundleElem{a: a})
		k = next
	}
	switch {
	case paren && end == len(toks):
		return k, "unclosed ( in decorator " + b.name
	case len(b.elems) == 0:
		return k, "decorator " + b.name + " composes nothing; list its decorators and attributes: decorator " + b.name + " = @logged @timed"
	}
	rt := f.scannedRuntime(toks)
	f.add(toks[i].off, toks[i].off+len("decorator"), "func")
	if !params {
		f.insert(toks[i+1].end(), "()", 0)
	}
	head := " " + rt + ".Bundle { return " + rt + ".Compose("
	if paren {
		f.add(toks[eq].off, toks[eq+1].off+1, head)
		f.add(toks[end].off, toks[end].off+1, ")}")
		end++
	} else {
		f.add(toks[eq].off, toks[eq].off+1, head)
		f.insert(b.elems[len(b.elems)-1].a.end, ")}", 3)
		end = k
	}
	for n, el := range b.elems {
		f.add(el.a.start, el.a.nameStart, "")
		if n < len(b.elems)-1 || paren {
			f.insert(el.a.end, ",", 2)
		}
	}
	f.bundles = append(f.bundles, b)
	return end, ""
}

// prepareBundles decides, once the package's names are known, which bare
// elements name a type (an attribute: its zero value), and reports composed
// decorators that compose themselves.
func (e *engine) prepareBundles() {
	byName := map[string]*bundleDecl{}
	files := map[*bundleDecl]*fileState{}
	for _, f := range e.vuka {
		for _, b := range f.bundles {
			byName[b.name], files[b] = b, f
			for _, el := range b.elems {
				if a := el.a; a.bare && e.namesType(f, a.Name) {
					el.decided = true
					f.insert(a.nameStart, "*new(", 0)
					f.insert(a.end, ")", 1)
				}
			}
		}
	}
	state := map[*bundleDecl]int{} // 1 visiting, 2 done
	var path []string
	var visit func(b *bundleDecl) bool
	visit = func(b *bundleDecl) bool {
		state[b] = 1
		path = append(path, b.name)
		for _, el := range b.elems {
			next := byName[el.a.Name]
			switch {
			case next == nil:
			case state[next] == 1:
				e.errs.add(el.a.Pos, "decorator %s composes itself: %s", next.name, strings.Join(append(path[indexOf(path, next.name):], next.name), " → "))
				return false
			case state[next] == 0 && !visit(next):
				return false
			}
		}
		path = path[:len(path)-1]
		state[b] = 2
		return true
	}
	for _, f := range e.vuka {
		for _, b := range f.bundles {
			if state[b] == 0 && !visit(b) {
				return
			}
		}
	}
}

func indexOf(xs []string, x string) int {
	for i, s := range xs {
		if s == x {
			return i
		}
	}
	return 0
}

// resolveBundles finds, once types are known, each element's form: a bare
// function of optional arguments is called with none, and a typed decorator
// (func(F) F), which needs the decorated function's type, is an error.
func (e *engine) resolveBundles(f *fileState) {
	for _, b := range f.bundles {
		for _, el := range b.elems {
			if el.decided {
				continue
			}
			a := el.a
			end := a.end
			if a.bare {
				end = a.nameStart + len(a.Name)
			}
			// The end's last byte: an insertion right after it maps past it.
			t := e.typeAt(f, f.cur.fromOrig(a.nameStart), f.cur.fromOrig(end-1)+1)
			if t == nil {
				continue
			}
			el.decided = true
			if r := factoryResult(t); r != nil && a.bare {
				f.insert(a.end, "()", 1)
				e.progress, t = true, r
			}
			if _, ok := t.Underlying().(*types.Signature); ok && !isRuntimeFunc(t, "Call") && !isRuntimeFunc(t, "Decl") && !isRuntimeFunc(t, "Type") {
				e.errs.add(a.Pos, "@%s is a typed decorator, of the decorated function's own type; it can't be composed into decorator %s, so write it beside it", a.Name, b.name)
			}
		}
	}
}

// typeAt is the type of the expression spanning [start, end) of f's text, or
// nil while it is unknown.
func (e *engine) typeAt(f *fileState, start, end int) types.Type {
	var t types.Type
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || t != nil || f.off(n.End()) < start || f.off(n.Pos()) > end {
			return false
		}
		if x, ok := n.(ast.Expr); ok && f.off(n.Pos()) == start && f.off(n.End()) == end {
			if tv, ok := e.info.Types[x]; ok && !isInvalid(tv.Type) {
				t = tv.Type
			} else if id, ok := x.(*ast.Ident); ok && e.info.Uses[id] != nil {
				t = e.info.Uses[id].Type()
			}
		}
		return true
	})
	if isInvalid(t) {
		return nil
	}
	return t
}
