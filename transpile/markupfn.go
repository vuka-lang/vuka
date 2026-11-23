package transpile

import (
	"bytes"
	"fmt"
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
	end, ok := p.markupFunc(fn.off, false)
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
	end, ok := p.markupFunc(fn.off, true)
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
func (p *jsxParser) markupFunc(off int, attr bool) (int, bool) {
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
		if b := p.space(lb.off + 1); p.at(b) == '<' && jsxTagAt(p.src, b) {
			p.failCode("E0197", b, "the func literal's result is %s, so it is Go and its body markup: a markup func's result is %sNode, or left out — func%s { <li>…</li> }", r, q, string(p.src[lp.off:rp.end()]))
		}
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
	p.goInMarkupBody(t, root.kids, attr)
	t.root, t.end = root, rb+1
	p.cur, p.inComp = outer, inComp
	p.trees = append(p.trees, t)
	return t.end, true
}

// markupFuncAt reports whether the func literal at off is a markup func's:
// its body is markup, not Go code.
func (f *fileState) markupFuncAt(off int) bool { return f.markupFuncTree(off) != nil }

// markupFuncTree is the body of the markup func literal at off, nil when
// none is there.
func (f *fileState) markupFuncTree(off int) *jsxTree {
	for _, t := range f.jsx {
		if t.fn && t.lit == off {
			return t
		}
	}
	return nil
}

// goInMarkupBody fails for a line of a markup func's body, outside its
// elements, that is a Go statement rather than markup — a return, or a
// call standing alone: the body is the markup the func renders, so it has
// neither. Read as markup they would render as text.
//
// A literal set on a component's attribute may yet be Go, by its prop's
// type: there the problem waits on the tree for checkMarkupFuncs.
func (p *jsxParser) goInMarkupBody(t *jsxTree, kids []jsxNode, attr bool) {
	for i, k := range kids {
		switch n := k.(type) {
		case *jsxBlock:
			for _, b := range n.bodies {
				p.goInMarkupBody(t, b, attr)
			}
			for _, b := range n.cases {
				p.goInMarkupBody(t, b, attr)
			}
		case *jsxText:
			end := n.start
			for end < len(p.src) && strings.IndexByte("<{}", p.src[end]) < 0 {
				end++
			}
			for off := n.start; off < end; {
				nl := bytes.IndexByte(p.src[off:end], '\n')
				line := end
				if nl >= 0 {
					line = off + nl
				}
				lead := off
				for lead < line && (p.src[lead] == ' ' || p.src[lead] == '\t') {
					lead++
				}
				if lead < line && (off > n.start || i == 0 || lineOpens(p.src, n.start)) {
					if msg := goStmtLine(strings.TrimRight(string(p.src[lead:line]), " \t\r"), line == end && end < len(p.src) && p.src[end] == '<'); msg != "" {
						if !attr {
							p.failCode("E0100", lead, "%s", msg)
						}
						if t.goErr == nil {
							t.goErr = &jsxError{off: lead, msg: msg, code: "E0100"}
						}
					}
				}
				off = line + 1
			}
		}
	}
}

// lineOpens reports whether nothing but spaces is before off on its line,
// or since the { opening a block.
func lineOpens(src []byte, off int) bool {
	for off > 0 && (src[off-1] == ' ' || src[off-1] == '\t') {
		off--
	}
	return off == 0 || src[off-1] == '\n' || src[off-1] == '{'
}

// goStmtLine is why text starting a markup func body's line is a Go statement
// — a return, or a call alone — "" when it is markup. tagged is whether a
// tag follows the text on its line, return <li>…</li>.
func goStmtLine(text string, tagged bool) string {
	if rest, ok := strings.CutPrefix(text, "return"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '(') {
		rest = strings.TrimSpace(rest)
		if _, err := parser.ParseExpr(rest); rest == "" || tagged || err == nil {
			return "a markup func's body is the markup it renders, so it has no return: write the markup itself, and an if around what renders only sometimes"
		}
	}
	x, err := parser.ParseExpr(text)
	if err != nil {
		return ""
	}
	call, ok := x.(*ast.CallExpr)
	if !ok || call.Rparen != token.Pos(len(text)) {
		return ""
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		if fun.End() == call.Lparen {
			return fmt.Sprintf("%s is a Go statement, and a markup func's body is the markup it renders: show a value in a hole, {%s}, and call what does work in an event or a method", text, text)
		}
	}
	return ""
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
	if p.goFunc != "" {
		p.failCode("E0197", off, "%s", p.goFunc)
	}
	p.failCode("E0197", off, "markup starts a func literal's body where the literal is Go: a func takes markup for its body when a template statement binds it, row := func(o Order) { … }, or a component's prop is set to it, row={func(o Order) { … }}")
}

// goFuncProp is, for <tag>'s attribute name whose value in the braces at i
// starts with a func literal an earlier compile found is given to a prop
// not of a func type returning Node, why that literal is Go: the position
// decides, and the position is the prop's type. "" otherwise.
func (p *jsxParser) goFuncProp(tag, name string, i int) string {
	if p.f.eng == nil || len(p.f.eng.goFuncs) == 0 {
		return ""
	}
	fn := newGoScan(p.src, i+1).significant()
	if fn.tok != token.FUNC {
		return ""
	}
	typ, ok := p.f.eng.goFuncs[p.f.at(fn.off).String()]
	if !ok {
		return ""
	}
	return goFuncMsg(tag, name, typ, p.f.target)
}

// goFuncMsg is E0197 for markup in a func literal set on <tag>'s name,
// whose prop is of type typ: the literal is Go.
func goFuncMsg(tag, name, typ string, target *jsxTarget) string {
	q := "ui."
	if target != nil {
		q = target.q
	}
	return fmt.Sprintf("<%s>'s %s is %s, so the func literal set on it is Go, and its body markup: a func takes markup for its body when its prop is a func returning %sNode", tag, name, typ, q)
}

// checkMarkupFuncs reports, once types are known, a markup func literal set
// on a component's attribute whose prop isn't a func returning Node — and
// has the package compile again, the literal read as Go, when it writes no
// result: its prop's type says it is Go, as it was before markup funcs.
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
					lit := f.markupFuncTree(a.expr.start)
					var sig *types.Signature
					if want != nil {
						sig, _ = want.Underlying().(*types.Signature)
					}
					if want == nil || sig != nil && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), node) {
						if lit != nil && lit.goErr != nil {
							e.errs.add(lit.goErr.code, f.at(lit.goErr.off), "%s", lit.goErr.msg)
						}
						continue
					}
					if lit != nil && !lit.typed {
						key := f.at(a.expr.start).String()
						if _, again := e.goFuncs[key]; !again {
							if e.goFuncs == nil {
								e.goFuncs = map[string]string{}
							}
							e.goFuncs[key], e.recompile = e.display(want), true
							continue
						}
					}
					e.errs.add("E0197", f.at(a.off), "%s", goFuncMsg(c.el.tag, a.name, e.display(want), f.target)).
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
