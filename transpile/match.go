package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/vuka-lang/vuka/v2/diag"
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
// A match with no guards and simple patterns is a switch instead, each
// case's bindings its body's first statement, and a switch on the subject
// when every case is values:
//
//	match r {                 switch {
//	case Ok(u):               case r.IsOk(): u := r.Value()
//	case Err(e):              case r.IsErr(): e := r.Err()
//	}                         default: panic("vuka: no case matched") }
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
	for _, t := range f.tries {
		if (t.fresh || !t.done && !t.dead) && t.off >= m.subj.start && t.off < m.subj.end {
			m.fail = "it waits for the ? in its subject"
			return
		}
	}
	subj := head.Init.(*ast.AssignStmt).Rhs[0]
	tv, ok := e.info.Types[subj]
	if !ok || isInvalid(tv.Type) {
		m.fail = "the subject's type is unknown"
		return
	}
	t := tv.Type
	fail := func(code diag.Code, off int, format string, args ...any) {
		m.dead = true
		e.errs.add(code, f.at(off), format, args...)
	}
	scope := e.pkg.Scope().Innermost(head.Pos())
	if scope == nil {
		scope = e.pkg.Scope()
	}
	pc := &patCtx{e: e, f: f, scope: scope, pos: head.Pos()}
	v := "__m" + itoa(m.n)
	// A call returning (T, error) is matched as the Result it is, its value
	// and error named in the match's init.
	var tuple *matchTuple
	if tup, ok := t.(*types.Tuple); ok {
		if tup.Len() != 2 || !types.Identical(tup.At(1).Type(), errorType) {
			fail("E0112", m.subj.start, "match takes one value or a call returning (T, error), not %s", e.display(tup))
			return
		}
		res, msg := e.resultOf(tup.At(0).Type())
		if msg != "" {
			m.fail = msg
			return
		}
		t = res
		tuple = e.tupleNames(f, m, pc, subj, tup.At(0).Type())
	}

	type header struct {
		pre, post string // around the guard; post alone when there is none
		bare      bool   // no init: the condition alone
	}
	var heads []header
	// A match whose cases have no guards and simple patterns is a switch:
	// each case its condition, its bindings the body's first statement.
	type arm struct {
		cond, binds string
		vals        []string // the values it equals, when that is all it tests
	}
	var arms []arm
	simple, values := true, true
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
				fail("E0112", sp.start, "bad pattern: %v", err)
				return
			}
			cp := &compiled{}
			if err := pc.compile(cp, x, v, t); err != nil {
				fail("E0112", sp.start, "%v", err)
				return
			}
			if tuple != nil {
				tuple.rewrite(cp, v, f.runtime)
			}
			alts, spaces = append(alts, cp), append(spaces, pc.space(x, t))
		}
		reachable := false
		for _, s := range spaces {
			reachable = reachable || useful(rows, []*space{s}, []types.Type{t})
		}
		if !reachable {
			if catchAll(rows) {
				fail("E0114", c.start, "unreachable case: an earlier case matches everything")
			} else {
				fail("E0114", c.start, "unreachable case: earlier cases match everything it does")
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
		body := m.rbrace
		if i := slices.Index(m.cases, c); i+1 < len(m.cases) {
			body = m.cases[i+1].start
		}
		used := func(name string) bool {
			return regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(name) + `\b`).Match(f.src[c.colon+1 : body])
		}
		var vals []string
		for _, a := range alts {
			ts := strings.Split(a.tests(), " && ")
			if val, ok := strings.CutPrefix(ts[0], v+" == "); ok && len(ts) == 1 && len(a.binds) == 0 && !a.complex {
				vals = append(vals, val)
			} else if ts[0] != "true" || len(a.binds) > 0 || len(alts) > 1 {
				values = false
			}
		}
		values = values && c.guard.start < 0
		simple = simple && c.guard.start < 0 && (len(alts) == 1 && !alts[0].complex || values)
		if len(alts) == 1 {
			var msg string
			init, cond, msg = pc.condition(alts[0])
			if msg != "" {
				fail("E0112", c.start, "%s", msg)
				return
			}
			for _, b := range alts[0].binds {
				if !used(b.name) {
					uses = append(uses, "_ = "+b.name+";")
				}
			}
			if simple {
				arms = append(arms, arm{cond: cond, binds: init + strings.Join(uses, " "), vals: vals})
			}
		} else {
			if simple {
				arms = append(arms, arm{vals: vals})
			}
			var ors []string
			for _, a := range alts {
				if len(a.binds) > 0 {
					fail("E0112", c.start, "patterns separated by commas can't bind variables")
					return
				}
				if !a.complex {
					if t := a.tests(); strings.Contains(t, " && ") || strings.Contains(t, " || ") {
						ors = append(ors, "("+t+")")
					} else {
						ors = append(ors, t)
					}
					continue
				}
				_, cond, msg := pc.condition(a)
				if msg != "" {
					fail("E0112", c.start, "%s", msg)
					return
				}
				ors = append(ors, "("+cond+")")
			}
			cond = strings.Join(ors, " || ")
		}
		pre := "} else if " + init + cond
		post := " {"
		if len(uses) > 0 {
			post += " " + strings.Join(uses, " ")
		}
		if c.guard.start >= 0 {
			heads = append(heads, header{pre: pre + " && (", post: ")" + post, bare: init == ""})
		} else {
			heads = append(heads, header{post: pre + post, bare: init == ""})
		}
	}
	if missing := pc.exhaustive(rows, t); missing != "" {
		fail("E0113", m.start, "match on %s isn't exhaustive: %s; add the missing cases or case _:", e.display(t), missing)
		return
	}

	if tuple != nil {
		var texts []string
		for _, a := range arms {
			texts = append(texts, a.cond, a.binds)
		}
		for _, h := range heads {
			texts = append(texts, h.pre, h.post)
		}
		lhs := tuple.lhs(texts, !simple)
		if simple {
			if lhs == "" {
				f.add(m.start, m.subj.start, "switch ")
			} else {
				f.add(m.start, m.subj.start, "switch "+lhs+" := ")
			}
			f.add(m.subj.end, m.lbrace+1, "; {")
		} else {
			f.add(m.start, m.subj.start, "if "+lhs+" := ")
			if h := &heads[0]; h.bare {
				// The first case's condition follows the init.
				f.add(m.subj.end, m.lbrace+1, "; ")
				if m.cases[0].guard.start >= 0 {
					h.pre = strings.TrimPrefix(h.pre, "} else if ")
				} else {
					h.post = strings.TrimPrefix(h.post, "} else if ")
				}
			} else {
				f.add(m.subj.end, m.lbrace+1, "; false { panic("+tuple.err+")")
			}
		}
	}
	if simple {
		subj := string(f.src[m.subj.start:m.subj.end])
		// The subject keeps its place: its own rewrites are there.
		pre, post := "switch "+v+" := ", "; {"
		switch {
		case values:
			pre, post = "switch ", " {"
			for i := range arms {
				arms[i].cond = strings.Join(arms[i].vals, ", ")
			}
		case token.IsIdentifier(subj):
			for i := range arms {
				arms[i].cond = strings.ReplaceAll(arms[i].cond, v, subj)
				arms[i].binds = strings.ReplaceAll(arms[i].binds, v, subj)
			}
		}
		switch {
		case tuple != nil:
		case !values && token.IsIdentifier(subj):
			f.add(m.start, m.lbrace+1, "switch {")
		default:
			f.add(m.start, m.subj.start, pre)
			f.add(m.subj.end, m.lbrace+1, post)
		}
		catchAll := false
		for i, c := range m.cases {
			a := arms[i]
			line := "case " + a.cond + ":"
			if a.cond == "true" && !values || a.cond == "" {
				line, catchAll = "default:", true
			}
			f.add(c.start, c.colon+1, line+" "+strings.TrimSuffix(strings.TrimSpace(a.binds), ";"))
		}
		if !catchAll {
			f.add(m.rbrace, m.rbrace, `default: panic("vuka: no case matched")
`)
		}
		m.done = true
		e.progress = true
		return
	}
	if tuple == nil {
		f.add(m.start, m.subj.start, "if "+v+" := ")
		f.add(m.subj.end, m.lbrace+1, "; false { panic("+v+")")
	}
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
	as     bool      // assert with errors.As, through the error's chain
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
	errs  string // the name f imports errors as, once a pattern needs it
}

// errorsPkg is the name f imports errors as, importing it on first use.
func (pc *patCtx) errorsPkg() string {
	if pc.errs == "" {
		missing := ""
		pc.errs = pc.e.qualifier(pc.f, &missing)(types.NewPackage("errors", "errors"))
		if missing != "" {
			pc.errs = pc.f.importAs("errors", "__errors")
		}
	}
	return pc.errs
}

// errorLike reports whether t is error, or an interface holding only errors:
// its patterns test the error's chain, with errors.Is and errors.As.
func errorLike(t types.Type) bool {
	return types.IsInterface(t) && types.Implements(t, errorType.Underlying().(*types.Interface))
}

// errValue reports whether a bare name in a pattern against an error is a
// package-level error variable, a sentinel: a value to test with errors.Is
// rather than a variable to bind.
func (pc *patCtx) errValue(name string, t types.Type) bool {
	v, ok := pc.lookup(name).(*types.Var)
	return ok && errorLike(t) && v.Parent() == pc.e.pkg.Scope() && types.AssignableTo(v.Type(), errorType)
}

// equals is the test of s against a value: errors.Is against an error.
func (pc *patCtx) equals(s, val string, t types.Type) string {
	if errorLike(t) && val != "nil" {
		return pc.errorsPkg() + ".Is(" + s + ", " + val + ")"
	}
	return s + " == " + val
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
		case pc.isValue(x.Name), pc.errValue(x.Name, t):
			c.test(pc.equals(s, x.Name, t))
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
		if obj, text, ok := pc.enumVariant(x); ok {
			if _, isType := obj.(*types.TypeName); isType {
				return pc.structPat(c, &ast.CompositeLit{Type: x}, false, s, t)
			}
			c.test(pc.equals(s, text, t))
			return nil
		}
		c.test(pc.equals(s, types.ExprString(x), t))
		return nil
	case *ast.UnaryExpr:
		switch x.Op {
		case token.XOR:
			if id, ok := x.X.(*ast.Ident); ok {
				c.test(pc.equals(s, id.Name, t))
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
		if errorLike(t) && len(lit.Elts) == 0 {
			// Err(&NotFound{}): is there one in the chain.
			c.test(pc.errorsPkg() + ".As(" + s + ", new(" + text + "))")
			break
		}
		c.temps++
		cur = "__p" + itoa(c.temps)
		c.steps = append(c.steps, patStep{assert: [3]string{cur, s, text}, as: errorLike(t)})
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
		var key *ast.Ident
		if ok {
			key, ok = kv.Key.(*ast.Ident)
		}
		if !ok {
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
				break
			}
		}
		// Shape.Circle: a variant of an enum with data.
		if T, err := pc.typeExpr(x.X); err == nil {
			if info := enumOf(T); info != nil && info.sum {
				if v := info.byName(x.Sel.Name); v != nil {
					obj = v.obj
				}
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
	for i, s := range c.steps {
		switch {
		case s.test != "":
			body.WriteString("if !(" + s.test + ") { return }; ")
		case s.bind != nil:
			body.WriteString(s.bind.name + " = " + s.bind.expr + "; ")
		default:
			tmp, from, typ := s.assert[0], s.assert[1], s.assert[2]
			val := "_"
			for _, later := range c.steps[i+1:] {
				if strings.Contains(later.test, tmp) || later.bind != nil && strings.Contains(later.bind.expr, tmp) || strings.Contains(later.assert[1], tmp) {
					val = tmp
				}
			}
			switch {
			case s.as && val == "_":
				body.WriteString("if !" + pc.errorsPkg() + ".As(" + from + ", new(" + typ + ")) { return }; ")
			case s.as:
				body.WriteString("var " + tmp + " " + typ + "; if !" + pc.errorsPkg() + ".As(" + from + ", &" + tmp + ") { return }; ")
			default:
				body.WriteString(val + ", " + tmp + "ok := " + from + ".(" + typ + "); if !" + tmp + "ok { return }; ")
			}
		}
	}
	fn := "func() (" + strings.Join(append(results, "__ok bool"), ", ") + ") { " + body.String() + "__ok = true; return }()"
	if len(names) == 0 {
		return "", fn, ""
	}
	return strings.Join(append(names, "__ok"), ", ") + " := " + fn + "; ", "__ok", ""
}

// enumVariant resolves Enum.Variant (or pkg.Enum.Variant) in a pattern: the
// variant's constant or type, and its Go spelling here.
func (pc *patCtx) enumVariant(x *ast.SelectorExpr) (types.Object, string, bool) {
	T, err := pc.typeExpr(x.X)
	if err != nil {
		return nil, "", false
	}
	info := enumOf(T)
	if info == nil {
		return nil, "", false
	}
	v := info.byName(x.Sel.Name)
	if v == nil {
		return nil, "", false
	}
	text := v.obj.Name()
	if q, ok := x.X.(*ast.SelectorExpr); ok {
		text = types.ExprString(q.X) + "." + text
	}
	return v.obj, text, true
}

// constOf is the constant a pattern names directly: Const or pkg.Const.
func (pc *patCtx) constOf(x ast.Expr) *types.Const {
	switch x := x.(type) {
	case *ast.Ident:
		c, _ := pc.lookup(x.Name).(*types.Const)
		return c
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if pn, ok := pc.lookup(id.Name).(*types.PkgName); ok {
				c, _ := pn.Imported().Scope().Lookup(x.Sel.Name).(*types.Const)
				return c
			}
		}
	}
	return nil
}

// matchTuple names the value and error of a match on a call returning
// (T, error):
//
//	match users.First(ctx) {        switch user, err := users.First(ctx); {
//	case Ok(user):                  case err == nil:
//	case Err(orm.ErrNotFound):      case errors.Is(err, orm.ErrNotFound):
type matchTuple struct {
	val, err string
	bound    bool // a case binds the value by its name
}

// resultOf is the runtime's Result[t].
func (e *engine) resultOf(t types.Type) (types.Type, string) {
	pkg, err := e.imp.Import(RuntimePath)
	if err != nil {
		return nil, "the runtime doesn't import: " + err.Error()
	}
	tn, ok := pkg.Scope().Lookup("Result").(*types.TypeName)
	if !ok {
		return nil, "the runtime has no Result"
	}
	res, err := types.Instantiate(nil, tn.Type(), []types.Type{t}, false)
	if err != nil {
		return nil, err.Error()
	}
	return res, ""
}

// tupleNames chooses the names: the value is the name a case's Ok binds it
// to, else named after its type or the call; the error is err. A name
// another case uses for something else, or a pattern names, gets the next
// candidate.
func (e *engine) tupleNames(f *fileState, m *matchStmt, pc *patCtx, subj ast.Expr, T types.Type) *matchTuple {
	n := len(m.cases)
	texts := make([]string, n)
	okBind, errBind := make([]string, n), make([]string, n)
	for i, c := range m.cases {
		end := m.rbrace
		if i+1 < n {
			end = m.cases[i+1].start
		}
		texts[i] = string(f.src[c.start:end])
		if len(c.pats) != 1 {
			continue
		}
		x, err := parser.ParseExpr(string(f.src[c.pats[0].start:c.pats[0].end]))
		if err != nil {
			continue
		}
		call, ok := x.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		fn, ok1 := call.Fun.(*ast.Ident)
		id, ok2 := call.Args[0].(*ast.Ident)
		if !ok1 || !ok2 || id.Name == "_" {
			continue
		}
		if obj := e.pkg.Scope().Lookup(id.Name); obj != nil {
			if _, isVar := obj.(*types.TypeName); !isVar {
				continue // a constant or a sentinel, not a binding
			}
		}
		switch fn.Name {
		case "Ok":
			okBind[i] = id.Name
		case "Err":
			errBind[i] = id.Name
		}
	}
	reserved := map[string]bool{"errors": true, "vuka": true, f.rt: true}
	free := func(name string, binds []string, other string) bool {
		if name == other || reserved[name] || !token.IsIdentifier(name) || types.Universe.Lookup(name) != nil {
			return false
		}
		re := regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(name) + `\b`)
		for i := range texts {
			if binds[i] != name && re.MatchString(texts[i]) {
				return false
			}
		}
		return true
	}
	bound := func(name string, binds []string) bool { return slices.Contains(binds, name) }
	pick := func(binds []string, other string, cands ...string) string {
		for _, c := range cands {
			// A name of its own shadows nothing in scope.
			if c != "" && free(c, binds, other) && (bound(c, binds) || pc.lookup(c) == nil) {
				return c
			}
		}
		base := cands[len(cands)-1]
		for i := 2; ; i++ {
			if c := base + itoa(i); free(c, binds, other) && pc.lookup(c) == nil {
				return c
			}
		}
	}
	errName := pick(errBind, "", "err")
	var cands []string
	for i, b := range okBind {
		if b != "" && len(regexp.MustCompile(`(^|[^.\w])`+regexp.QuoteMeta(b)+`\b`).FindAllString(texts[i], 2)) > 1 {
			cands = append(cands, b)
			break
		}
	}
	cands = append(cands, typeVarName(T), calleeVarName(subj), "v")
	return &matchTuple{val: pick(okBind, errName, cands...), err: errName}
}

// rewrite points a pattern compiled against the Result v at the tuple's
// names, dropping the bindings that are those names.
func (mt *matchTuple) rewrite(c *compiled, v string, rt func() string) {
	parts := strings.NewReplacer(v+".IsOk()", mt.err+" == nil", v+".IsErr()", mt.err+" != nil",
		v+".Value()", mt.val, v+".Err()", mt.err)
	re := regexp.MustCompile(regexp.QuoteMeta(v) + `\b`)
	r := replacerFunc(func(s string) string {
		s = parts.Replace(s)
		if re.MatchString(s) {
			// The whole Result: case r: binds it.
			s = re.ReplaceAllLiteralString(s, rt()+".Of("+mt.val+", "+mt.err+")")
		}
		return s
	})
	var steps []patStep
	for _, s := range c.steps {
		s.test = r.Replace(s.test)
		s.assert[1] = r.Replace(s.assert[1])
		if s.bind != nil {
			b := *s.bind
			b.expr = r.Replace(b.expr)
			if b.expr == b.name && !c.complex {
				mt.bound = mt.bound || b.name == mt.val
				continue
			}
			s.bind = &b
		}
		steps = append(steps, s)
	}
	// errors.Is and errors.As are false for a nil error: err != nil before
	// one says nothing.
	isNil := mt.err + " != nil"
	chain := func(s patStep) bool {
		return s.as && s.assert[1] == mt.err || strings.Contains(s.test, ".Is("+mt.err+", ") || strings.Contains(s.test, ".As("+mt.err+", ")
	}
	c.steps = steps[:0:0]
	for i, s := range steps {
		if s.test == isNil && i+1 < len(steps) && chain(steps[i+1]) {
			continue
		}
		c.steps = append(c.steps, s)
	}
	var binds []binding
	for _, b := range c.binds {
		b.expr = r.Replace(b.expr)
		if b.expr == b.name && !c.complex {
			continue
		}
		binds = append(binds, b)
	}
	c.binds = binds
}

// lhs is the left side of the match's init: the names the lowered cases
// use, _ for one they don't; "" when they use neither.
func (mt *matchTuple) lhs(texts []string, errUsed bool) string {
	use := func(name string) bool {
		re := regexp.MustCompile(`(^|[^.\w])` + regexp.QuoteMeta(name) + `\b`)
		for _, t := range texts {
			if re.MatchString(t) {
				return true
			}
		}
		return false
	}
	val, err := mt.val, mt.err
	valUsed := mt.bound || use(val)
	errUsed = errUsed || use(err)
	if !valUsed && !errUsed {
		return ""
	}
	if !valUsed {
		val = "_"
	}
	if !errUsed {
		err = "_"
	}
	return val + ", " + err
}

// typeVarName is a variable's name after its type: User, *User → user;
// []Post → posts. "" for an unnamed type.
func typeVarName(t types.Type) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	plural := ""
	if s, ok := t.(*types.Slice); ok {
		t, plural = types.Unalias(s.Elem()), "s"
		if p, ok := t.(*types.Pointer); ok {
			t = types.Unalias(p.Elem())
		}
	}
	n, ok := t.(*types.Named)
	if !ok {
		return ""
	}
	name := goLower(n.Obj().Name()) + plural
	if token.IsKeyword(name) {
		return ""
	}
	return name
}

// calleeVarName is a variable's name after the call producing it: Exists →
// exists; GetUser → user.
func calleeVarName(x ast.Expr) string {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return ""
	}
	fn := ast.Unparen(call.Fun)
	switch f := fn.(type) {
	case *ast.IndexExpr:
		fn = f.X
	case *ast.IndexListExpr:
		fn = f.X
	}
	var name string
	switch f := fn.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	default:
		return ""
	}
	if rest, ok := strings.CutPrefix(name, "Get"); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
		name = rest
	}
	name = goLower(name)
	if token.IsKeyword(name) {
		return ""
	}
	return name
}

// goLower lowers a name's leading capitals as Go spells a variable: User →
// user, URLPath → urlPath, ID → id.
func goLower(name string) string {
	r := []rune(name)
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	if i > 1 && i < len(r) {
		i--
	}
	for j := range i {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}

type replacerFunc func(string) string

func (r replacerFunc) Replace(s string) string { return r(s) }
