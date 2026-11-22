package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// A func literal bound by a template's statement, or set on a component's
// prop of a func type returning Node, takes markup for its body:
//
//	row := func(o Order) {          →   row := func(o Order) ui.Node {
//	  <li>{o.Number}</li>                   return ui.HTML(`<li>{o.Number}</li>`, o.Number)
//	}                                   }
//
// The body is a tree of its own, lowered as any other. A literal writing a
// result other than Node is Go, as it always was.

// markupFuncStmt reads a template statement binding a markup func literal,
// row := func(o Order) { … } or row = func(…) { … }, from its first token,
// and returns where the statement ends.
func (p *jsxParser) markupFuncStmt(first tok) (int, bool) {
	if first.tok != token.IDENT {
		return 0, false
	}
	g := newGoScan(p.src, first.end())
	if op := g.significant(); op.tok != token.DEFINE && op.tok != token.ASSIGN {
		return 0, false
	}
	fn := g.significant()
	if fn.tok != token.FUNC {
		return 0, false
	}
	end, ok := p.markupFunc(fn.off)
	if !ok {
		return 0, false
	}
	g.resume(end)
	for t := g.next(); ; t = g.next() {
		switch {
		case t.tok == token.COMMENT:
			continue
		case t.tok == token.SEMICOLON, t.tok == token.EOF, t.tok == token.RBRACE, t.tok == token.LSS:
			return end, true
		}
		p.fail(t.off, "expected the line to end after the func literal's }")
	}
}

// attrMarkupFunc reads a component attribute's value in the braces at i when
// it is a markup func literal, returning its span and where the braces end.
func (p *jsxParser) attrMarkupFunc(i int) (span, int, bool) {
	g := newGoScan(p.src, i+1)
	fn := g.significant()
	if fn.tok != token.FUNC {
		return span{}, 0, false
	}
	end, ok := p.markupFunc(fn.off)
	if !ok {
		return span{}, 0, false
	}
	g.reset(end)
	rb := g.significant()
	if rb.tok != token.RBRACE {
		p.fail(rb.off, "expected } after the func literal: a prop set to one takes the literal alone")
	}
	return span{fn.off, end}, rb.off + 1, true
}

// markupFunc reads the func literal at off, its body markup, and returns
// where it ends. It reads nothing, and reports false, for a literal whose
// result isn't Node.
func (p *jsxParser) markupFunc(off int) (int, bool) {
	g := newGoScan(p.src, off)
	g.significant()
	lp := g.significant()
	if lp.tok != token.LPAREN {
		return 0, false
	}
	rp, d := lp, 0
	for t := g.next(); ; t = g.next() {
		if t.tok == token.EOF {
			return 0, false
		}
		if isOpen(t.tok) {
			d++
		} else if isClose(t.tok) {
			if d == 0 {
				rp = t
				break
			}
			d--
		}
	}
	var result []string
	var lb tok
	for d = 0; lb.tok != token.LBRACE; {
		t := g.significant()
		switch {
		case t.tok == token.EOF || t.tok == token.SEMICOLON || t.tok == token.RBRACE && d == 0:
			return 0, false
		case t.tok == token.LBRACE && d == 0:
			lb = t
			continue
		case isOpen(t.tok):
			d++
		case isClose(t.tok):
			d--
		}
		result = append(result, string(p.src[t.off:t.end()]))
	}
	q := "ui."
	if p.f.target != nil {
		q = p.f.target.q
	}
	if r := strings.Join(result, ""); r != "" && r != q+"Node" && r != "("+q+"Node)" {
		return 0, false
	}
	p.markupParams(lp.off, rp.end())
	t := &jsxTree{start: rp.end(), declOff: p.f.declOff, fn: true, typed: len(result) > 0, lit: off}
	if t.typed {
		t.start = lb.off
	}
	outer, inComp := p.cur, p.inComp
	p.cur, p.inComp = t, 0
	root := &jsxElem{start: lb.off, frag: true}
	var rb int
	root.kids, rb = p.children(lb.off+1, nil, lb.off, false)
	root.end = rb + 1
	t.root, t.end = root, rb+1
	p.cur, p.inComp = outer, inComp
	p.trees = append(p.trees, t)
	return t.end, true
}

// markupParams fails unless a markup func's parameters, src[start:end] with
// their parentheses, are a Go parameter list naming each value it takes.
func (p *jsxParser) markupParams(start, end int) {
	params := string(p.src[start:end])
	_, err := parser.ParseExpr("func" + params + " {}")
	if err != nil {
		msg := err.Error()
		if _, after, ok := strings.Cut(msg, ": "); ok {
			msg = after
		}
		p.failCode("E0198", start, "a markup func's parameters don't parse: %s; write each with its type, func(o Order, i int)", msg)
	}
}

// opensFuncBody reports whether toks, the tokens of an expression read so
// far, end with the { of a func literal's body.
func opensFuncBody(toks []tok) bool {
	var ts []tok
	for _, t := range toks {
		if !trivia(t) {
			ts = append(ts, t)
		}
	}
	if len(ts) == 0 || ts[len(ts)-1].tok != token.LBRACE {
		return false
	}
	for j := len(ts) - 2; j >= 0; j-- {
		switch ts[j].tok {
		case token.RPAREN:
			d, k := 0, j
			for ; k >= 0; k-- {
				if ts[k].tok == token.RPAREN {
					d++
				} else if ts[k].tok == token.LPAREN {
					if d--; d == 0 {
						break
					}
				}
			}
			if k < 0 {
				return false
			}
			if k > 0 && ts[k-1].tok == token.FUNC {
				return true
			}
			j = k
		case token.IDENT, token.PERIOD, token.MUL, token.LBRACK, token.RBRACK, token.MAP, token.CHAN, token.ARROW:
		default:
			return false
		}
	}
	return false
}

// misplacedMarkupFunc fails for markup starting a func literal's body where
// a func literal is Go: anywhere but a template statement's binding and a
// component's prop.
func (p *jsxParser) misplacedMarkupFunc(off int) {
	p.failCode("E0197", off, "markup starts a func literal's body where the literal is Go: a func takes markup for its body when a template statement binds it, row := func(o Order) { … }, or a component's prop is set to it, row={func(o Order) { … }}")
}

// checkMarkupFuncs reports, once types are known, a markup func literal set
// on a component's attribute whose prop isn't a func returning Node.
func (e *engine) checkMarkupFuncs() {
	for _, f := range e.vuka {
		if f.html == nil {
			continue
		}
		node := e.nodeType(f.target)
		if node == nil {
			continue
		}
		for _, t := range f.jsx {
			if t.fn {
				e.untypedParams(f, t)
			}
			for _, c := range t.comps {
				if !c.resolved || c.dead || c.island {
					continue
				}
				for _, a := range c.el.attrs {
					if !a.mfn {
						continue
					}
					want := e.markupFuncTarget(f, c, a)
					if want == nil {
						continue
					}
					sig, _ := want.Underlying().(*types.Signature)
					if sig != nil && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), node) {
						continue
					}
					e.errs.add("E0197", f.at(a.off), "<%s>'s %s is %s, so the func literal set on it is Go, and its body markup: a func takes markup for its body when its prop is a func returning %sNode", c.el.tag, a.name, e.display(want), f.target.q).
						help("set %s to a method or func declared in the script, or declare the prop as func(…) %sNode", a.name, f.target.q)
				}
			}
		}
	}
}

// untypedParams reports a markup func's parameter written without its
// type, func(o): Go reads the name as a type, and the package has none.
func (e *engine) untypedParams(f *fileState, t *jsxTree) {
	var lit *ast.FuncLit
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && f.orig(l.Pos()) == t.lit {
			lit = l
		}
		return lit == nil
	})
	if lit == nil {
		return
	}
	for _, fl := range lit.Type.Params.List {
		id, ok := fl.Type.(*ast.Ident)
		if len(fl.Names) > 0 || !ok || e.info.Uses[id] != nil {
			continue
		}
		e.errs.add("E0198", f.at(f.orig(id.Pos())), "%s has no type: a markup func's parameters are Go's, each named with its type", id.Name).
			help("write func(%s T), T the type of the value it takes", id.Name)
	}
}

// markupFuncTarget is the type an attribute's markup func literal is given
// to: its prop's, or the parameter's of a function component.
func (e *engine) markupFuncTarget(f *fileState, c *jsxComp, a *jsxAttr) types.Type {
	var typ types.Type
	eachArg(c.args, func(x *compArg) {
		if x.attr == a && x.typ != nil {
			typ = x.typ
		}
	})
	if typ != nil {
		return typ
	}
	lit, _ := e.exprAt(f, a.expr).(*ast.FuncLit)
	if lit == nil {
		return nil
	}
	switch p := f.parents[lit].(type) {
	case *ast.KeyValueExpr:
		if lit, ok := f.parents[p].(*ast.CompositeLit); ok && e.info.TypeOf(lit) != nil {
			tv := e.info.Types[lit]
			if st, ok := derefType(tv.Type).Underlying().(*types.Struct); ok {
				if id, ok := p.Key.(*ast.Ident); ok {
					for i := range st.NumFields() {
						if st.Field(i).Name() == id.Name {
							return st.Field(i).Type()
						}
					}
				}
			}
		}
	case *ast.CallExpr:
		if sig, ok := e.info.TypeOf(p.Fun).(*types.Signature); ok {
			for i, x := range p.Args {
				if x == lit && i < sig.Params().Len() {
					return sig.Params().At(i).Type()
				}
			}
		}
	}
	return nil
}

// markupFuncCall reports, for an event's call of a markup func, that it
// renders markup rather than handling the event: onClick={row(o)}.
func (e *engine) markupFuncCall(f *fileState, a *jsxAttr, x ast.Expr) bool {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := e.info.Uses[id].(*types.Var)
	node := e.nodeType(f.target)
	if !ok || node == nil {
		return false
	}
	sig, ok := v.Type().Underlying().(*types.Signature)
	if !ok || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), node) {
		return false
	}
	e.errs.add("E0199", f.at(f.orig(call.Pos())), "%s={%s(…)} calls %s, which renders markup: an event takes a handler, and a markup func's call is its markup", a.name, id.Name, id.Name).
		help("render it in a hole, {%s(…)}, and give the event a method of the shape: %s={Save(…)}", id.Name, a.name)
	return true
}

// funcVar reports whether a template's line at i declares func variables,
// var a func(Order) ui.Node: a markup func's, assigned below it.
func (p *jsxParser) funcVar(i int) bool {
	g := newGoScan(p.src, i)
	if g.next().tok != token.VAR {
		return false
	}
	for {
		if g.next().tok != token.IDENT {
			return false
		}
		switch g.next().tok {
		case token.COMMA:
		case token.FUNC:
			return true
		default:
			return false
		}
	}
}
