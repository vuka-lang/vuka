package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// lowerMatch rewrites a match statement into an if-else chain. Only the header
// and case lines change; bodies and guards stay where they are:
//
//	match r {                 if __m1 := r; false { panic(__m1)
//	case Ok(u):               } else if u := __m1.Value(); __m1.IsOk() { _ = u;
//	case Err(e) if retry(e):  } else if e := __m1.Err(); __m1.IsErr() && (retry(e)) { _ = e;
//	case _:                   } else if true {
//	}                         } else { panic("vuka: no case matched") }
//
// A match must be exhaustive; that is checked here, before Go sees it.
func (e *engine) lowerMatch(f *fileState, m *matchStmt) {
	at := f.cur.fromOrig(m.start)
	var head *ast.IfStmt
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if s, ok := n.(*ast.IfStmt); ok && f.off(s.Pos()) == at {
			head = s
		}
		return head == nil
	})
	if head == nil {
		m.fail = "internal: placeholder not found"
		return
	}
	if t := e.pendingTry(f, head); t != nil {
		m.fail = "it waits for the ? at " + f.at(t.off).String() + ", whose operand's type is unknown"
		return
	}
	subj := head.Init.(*ast.AssignStmt).Rhs[0]
	tv, ok := e.info.Types[subj]
	if !ok || isInvalid(tv.Type) {
		m.fail = "the subject's type is unknown"
		return
	}
	t := tv.Type
	fail := func(off int, format string, args ...any) {
		m.dead = true
		e.errs.add(f.at(off), format, args...)
	}
	scope := e.pkg.Scope().Innermost(head.Pos())
	if scope == nil {
		scope = e.pkg.Scope()
	}
	pc := &patCtx{e: e, f: f, scope: scope, pos: head.Pos()}
	v := "__m" + itoa(m.n)

	type header struct {
		pre, post string // around the guard; post alone when there is none
	}
	var heads []header
	var rows [][]*space // the unguarded cases' patterns
	for _, c := range m.cases {
		var alts []*compiled
		var spaces []*space
		if len(c.pats) == 0 {
			alts, spaces = append(alts, &compiled{}), append(spaces, anything)
		}
		for _, sp := range c.pats {
			x, err := parser.ParseExpr(string(f.src[sp.start:sp.end]))
			if err != nil {
				fail(sp.start, "bad pattern: %v", err)
				return
			}
			cp := &compiled{}
			if err := pc.compile(cp, x, v, t); err != nil {
				fail(sp.start, "%v", err)
				return
			}
			alts, spaces = append(alts, cp), append(spaces, pc.space(x, t))
		}
		reachable := false
		for _, s := range spaces {
			reachable = reachable || useful(rows, []*space{s}, []types.Type{t})
		}
		if !reachable {
			if catchAll(rows) {
				fail(c.start, "unreachable case: an earlier case matches everything")
			} else {
				fail(c.start, "unreachable case: earlier cases match everything it does")
			}
			return
		}
		if c.guard.start < 0 {
			for _, s := range spaces {
				rows = append(rows, []*space{s})
			}
		}

		var init, cond string
		var uses []string
		if len(alts) == 1 {
			var msg string
			init, cond, msg = pc.condition(alts[0])
			if msg != "" {
				fail(c.start, "%s", msg)
				return
			}
			for _, b := range alts[0].binds {
				uses = append(uses, "_ = "+b.name+";")
			}
		} else {
			var ors []string
			for _, a := range alts {
				if len(a.binds) > 0 || a.complex {
					fail(c.start, "patterns separated by commas can't bind variables or match inside pointers and interfaces")
					return
				}
				ors = append(ors, "("+a.tests()+")")
			}
			cond = strings.Join(ors, " || ")
		}
		pre := "} else if " + init + cond
		post := " {"
		if len(uses) > 0 {
			post += " " + strings.Join(uses, " ")
		}
		if c.guard.start >= 0 {
			heads = append(heads, header{pre: pre + " && (", post: ")" + post})
		} else {
			heads = append(heads, header{post: pre + post})
		}
	}
	if missing := pc.exhaustive(rows, t); missing != "" {
		fail(m.start, "match on %s isn't exhaustive: %s; add the missing cases or case _:", e.display(t), missing)
		return
	}

	f.add(m.start, m.subj.start, "if "+v+" := ")
	f.add(m.subj.end, m.lbrace+1, "; false { panic("+v+")")
	for i, c := range m.cases {
		h := heads[i]
		if c.guard.start >= 0 {
			f.add(c.start, c.guard.start, h.pre)
			f.add(c.guard.end, c.colon+1, h.post)
		} else {
			f.add(c.start, c.colon+1, h.post)
		}
	}
	f.add(m.rbrace, m.rbrace+1, `} else { panic("vuka: no case matched") }`)
	m.done = true
	e.progress = true
}

// pendingTry is a ? before the match, in its function, whose lowering this
// round's types don't reflect yet: in `x := f()?; match x {…}` x still has the
// operand's type until the round after ? is lowered.
func (e *engine) pendingTry(f *fileState, head ast.Node) *try {
	var fn ast.Node
	for p := f.parents[head]; p != nil && fn == nil; p = f.parents[p] {
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			fn = p
		}
	}
	if fn == nil {
		return nil
	}
	from, at := f.orig(fn.Pos()), f.orig(head.Pos())
	for _, t := range f.tries {
		if t.off > from && t.off < at && (t.fresh || !t.done && !t.dead) {
			return t
		}
	}
	return nil
}

type binding struct {
	name, expr string
	typ        types.Type
}

// compiled is a pattern turned into steps, in order: tests, bindings, and type
// assertions into temporaries.
type compiled struct {
	steps   []patStep
	binds   []binding
	complex bool // needs steps run in order (pointers, interfaces)
	temps   int
}

type patStep struct {
	test   string
	bind   *binding
	assert [3]string // temp, from, type
}

func (c *compiled) test(cond string) { c.steps = append(c.steps, patStep{test: cond}) }

func (c *compiled) tests() string {
	var ts []string
	for _, s := range c.steps {
		if s.test != "" {
			ts = append(ts, s.test)
		}
	}
	if len(ts) == 0 {
		return "true"
	}
	return strings.Join(ts, " && ")
}

type patCtx struct {
	e     *engine
	f     *fileState
	scope *types.Scope
	pos   token.Pos
}

func (pc *patCtx) lookup(name string) types.Object {
	_, obj := pc.scope.LookupParent(name, pc.pos)
	return obj
}

// isValue reports whether a bare name in a pattern is a value to compare with
// (a constant, true, false, nil) rather than a variable to bind.
func (pc *patCtx) isValue(name string) bool {
	switch pc.lookup(name).(type) {
	case *types.Const, *types.Nil:
		return true
	}
	return false
}

func (pc *patCtx) compile(c *compiled, x ast.Expr, s string, t types.Type) error {
	kind, arg := runtimeType(t)
	switch x := x.(type) {
	case *ast.ParenExpr:
		return pc.compile(c, x.X, s, t)
	case *ast.Ident:
		switch {
		case x.Name == "_":
		case x.Name == "None" && kind == "Option":
			c.test(s + ".IsNone()")
		case pc.isValue(x.Name):
			c.test(s + " == " + x.Name)
		default:
			for _, b := range c.binds {
				if b.name == x.Name {
					return fmt.Errorf("%s is bound twice in one pattern", x.Name)
				}
			}
			b := binding{x.Name, s, t}
			c.binds = append(c.binds, b)
			c.steps = append(c.steps, patStep{bind: &b})
		}
		return nil
	case *ast.BasicLit:
		c.test(s + " == " + x.Value)
		return nil
	case *ast.SelectorExpr:
		c.test(s + " == " + types.ExprString(x))
		return nil
	case *ast.UnaryExpr:
		switch x.Op {
		case token.XOR:
			if id, ok := x.X.(*ast.Ident); ok {
				c.test(s + " == " + id.Name)
				return nil
			}
			return fmt.Errorf("^ pins a variable: ^name")
		case token.SUB, token.ADD:
			if _, ok := x.X.(*ast.BasicLit); ok {
				c.test(s + " == " + types.ExprString(x))
				return nil
			}
		case token.AND:
			if lit, ok := x.X.(*ast.CompositeLit); ok {
				return pc.structPat(c, lit, true, s, t)
			}
		}
	case *ast.CallExpr:
		id, ok := x.Fun.(*ast.Ident)
		if !ok || len(x.Args) != 1 {
			break
		}
		want := map[string]string{"Ok": "Result", "Err": "Result", "Some": "Option"}[id.Name]
		if want == "" {
			break
		}
		if kind != want {
			return fmt.Errorf("%s(…) matches a %s, not %s", id.Name, want, pc.e.display(t))
		}
		switch id.Name {
		case "Ok":
			c.test(s + ".IsOk()")
			return pc.compile(c, x.Args[0], s+".Value()", arg)
		case "Err":
			c.test(s + ".IsErr()")
			return pc.compile(c, x.Args[0], s+".Err()", errorType)
		default:
			c.test(s + ".IsSome()")
			return pc.compile(c, x.Args[0], s+".Value()", arg)
		}
	case *ast.CompositeLit:
		return pc.structPat(c, x, false, s, t)
	}
	return fmt.Errorf("%s isn't a pattern; patterns are _, names, literals, ^pins, Ok/Err/Some(…), None and T{Field: …}", types.ExprString(x))
}

// structPat matches a struct's fields, T{X: 0, Y: y}. Against an interface it is
// a type test too; &T{…} matches a pointer.
func (pc *patCtx) structPat(c *compiled, lit *ast.CompositeLit, ptr bool, s string, t types.Type) error {
	if lit.Type == nil {
		return fmt.Errorf("a struct pattern names its type: T{Field: …}")
	}
	T, err := pc.typeExpr(lit.Type)
	if err != nil {
		return err
	}
	want := T
	if ptr {
		want = types.NewPointer(T)
	}
	cur := s
	q := func(p *types.Package) string {
		if p == pc.e.pkg || p.Path() == RuntimePath {
			return ""
		}
		return p.Name()
	}
	switch {
	case types.Identical(t, want):
		if ptr {
			c.test(s + " != nil")
			c.complex = true
		}
	case types.IsInterface(t):
		if !types.AssignableTo(want, t) {
			return fmt.Errorf("%s can't hold a %s", types.TypeString(t, q), types.TypeString(want, q))
		}
		text, msg := pc.e.typeText(pc.f, want)
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		c.temps++
		cur = "__p" + itoa(c.temps)
		c.steps = append(c.steps, patStep{assert: [3]string{cur, s, text}})
		c.complex = true
		if ptr {
			c.test(cur + " != nil")
		}
	default:
		return fmt.Errorf("a %s pattern can't match a %s", types.TypeString(want, q), types.TypeString(t, q))
	}
	if _, ok := T.Underlying().(*types.Struct); !ok {
		return fmt.Errorf("%s isn't a struct", types.TypeString(T, q))
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		key, isIdent := kv.Key.(*ast.Ident)
		if !ok || !isIdent {
			return fmt.Errorf("struct patterns name their fields: %s{Field: pattern}", types.TypeString(T, q))
		}
		obj, _, _ := types.LookupFieldOrMethod(T, false, pc.e.pkg, key.Name)
		field, ok := obj.(*types.Var)
		if !ok || !field.IsField() {
			return fmt.Errorf("%s has no field %s", types.TypeString(T, q), key.Name)
		}
		if err := pc.compile(c, kv.Value, cur+"."+key.Name, field.Type()); err != nil {
			return err
		}
	}
	return nil
}

func (pc *patCtx) typeExpr(x ast.Expr) (types.Type, error) {
	var obj types.Object
	switch x := x.(type) {
	case *ast.Ident:
		obj = pc.lookup(x.Name)
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if pn, ok := pc.lookup(id.Name).(*types.PkgName); ok {
				obj = pn.Imported().Scope().Lookup(x.Sel.Name)
			}
		}
	default:
		return nil, fmt.Errorf("struct patterns of generic types aren't supported yet")
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s isn't a type", types.ExprString(x))
	}
	return tn.Type(), nil
}

// condition is a case's if-statement init and condition. Simple patterns bind
// with one assignment (every expression is safe to evaluate even when the
// tests fail); patterns through pointers or interfaces run their steps in a
// function literal, in order.
func (pc *patCtx) condition(c *compiled) (init, cond, msg string) {
	if !c.complex {
		if len(c.binds) > 0 {
			var names, exprs []string
			for _, b := range c.binds {
				names = append(names, b.name)
				exprs = append(exprs, b.expr)
			}
			init = strings.Join(names, ", ") + " := " + strings.Join(exprs, ", ") + "; "
		}
		return init, c.tests(), ""
	}
	var results, names []string
	for _, b := range c.binds {
		text, msg := pc.e.typeText(pc.f, b.typ)
		if msg != "" {
			return "", "", msg
		}
		results = append(results, b.name+" "+text)
		names = append(names, b.name)
	}
	var body strings.Builder
	for _, s := range c.steps {
		switch {
		case s.test != "":
			body.WriteString("if !(" + s.test + ") { return }; ")
		case s.bind != nil:
			body.WriteString(s.bind.name + " = " + s.bind.expr + "; ")
		default:
			tmp, from, typ := s.assert[0], s.assert[1], s.assert[2]
			body.WriteString(tmp + ", " + tmp + "ok := " + from + ".(" + typ + "); if !" + tmp + "ok { return }; ")
		}
	}
	fn := "func() (" + strings.Join(append(results, "__ok bool"), ", ") + ") { " + body.String() + "__ok = true; return }()"
	if len(names) == 0 {
		return "", fn, ""
	}
	return strings.Join(append(names, "__ok"), ", ") + " := " + fn + "; ", "__ok", ""
}
