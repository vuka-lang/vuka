package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// lowerTry rewrites a ?. One ending a statement keeps its operand where it
// is; the statement gains an error variable and an early return after it:
//
//	u := load(id)?   →   u, __e1 := load(id); if __e1 != nil { return nil, __e1 }
//
// One inside an expression moves its operand into statements before the
// statement holding it (tryexpr.go). The operand may be Go's (T…, error)
// or error, a Result (unwrapped with Get), or an Option (in a function
// that returns an Option, or with ?(err) saying which error None is).
func (e *engine) lowerTry(f *fileState, t *try) {
	at := f.cur.fromOrig(t.off)
	fail := func(msg string) {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "%s", msg)
	}
	op := f.tryOperand(at)
	if op == nil {
		fail("? goes right after the expression it unwraps, as in f()?")
		return
	}
	stmt := f.stmtOf(op)
	if stmt == nil {
		fail("? needs an enclosing function")
		return
	}
	top := f.topStmt(stmt)
	sp := span{f.orig(top.Pos()), f.origEnd(top.End())}
	if f.isTouched(sp) {
		t.fail = "another ? in its statement is lowered first"
		return
	}
	for _, o := range f.tries {
		if o.off < t.off && !o.done && !o.dead && o.off >= sp.start && o.off < sp.end {
			t.fail = "it waits for the ? at " + f.at(o.off).String()
			return
		}
	}
	if e.lowerMain(f, stmt) {
		f.touch(sp)
		return
	}
	tv, ok := e.info.Types[op]
	if !ok || isInvalid(tv.Type) {
		t.fail = "the operand's type is unknown"
		return
	}
	k, msg := e.tryKindOf(op, tv.Type)
	if msg != "" {
		fail(msg)
		return
	}
	if !f.wholeStmt(stmt, op) {
		e.lowerTryIn(f, t, op, stmt, k, sp)
		return
	}
	sig, _ := e.enclosingFunc(f, stmt)
	if sig == nil {
		fail("? needs an enclosing function")
		return
	}
	n := itoa(t.n)
	failVar, cond := "__e"+n, "__e"+n+" != nil"
	if k.option {
		failVar, cond = "__ok"+n, "!__ok"+n
	}
	plain := false // err, as Go names it: in an if's init, or beside new variables
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		plain = s.Tok == token.DEFINE
	case *ast.ExprStmt, *ast.ReturnStmt:
		plain = true
	}
	if plain = plain && !k.option && e.errFree(f, stmt); plain {
		failVar, cond = "err", "err != nil"
	}
	ret, msg := e.failBranch(f, t, stmt, k, failVar)
	if msg != "" {
		fail(msg)
		return
	}
	check := &gtext{}
	check.s("; if " + cond + " { ")
	check.add(ret)
	check.s(" }")
	end := t.end()
	values := k.values

	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Lhs) != len(values) {
			fail("? here yields " + itoa(len(values)) + " value(s), but " + itoa(len(s.Lhs)) + " are assigned")
			return
		}
		if s.Tok == token.ASSIGN {
			tracked, wait := e.trackedLhs(f, s)
			if wait {
				return // the shape's fields are lowered first
			}
			if tracked != nil {
				// Server state is set once the call succeeded, as
				// x := f()?; p.x = x would: into temporaries, then
				// assigned one by one, a tracked field through Set.
				vars := make([]string, len(values))
				var sets []string
				for i, l := range s.Lhs {
					vars[i] = "__v" + n + "_" + itoa(i+1)
					lt := string(f.src[f.orig(l.Pos()):f.orig(l.End())])
					switch {
					case tracked[i]:
						sets = append(sets, lt+".Set("+vars[i]+")")
					case lt == "_":
						vars[i] = "_"
					default:
						sets = append(sets, lt+" = "+vars[i])
					}
				}
				lhs := strings.Join(append(vars, failVar), ", ")
				f.add(f.orig(s.Pos()), f.orig(s.Rhs[0].Pos()), lhs+" := ")
				check.s("; " + strings.Join(sets, "; "))
				break
			}
			typ := "error"
			if k.option {
				typ = "bool"
			}
			f.insert(f.orig(s.Pos()), "var "+failVar+" "+typ+"; ", 0)
		}
		f.insert(f.orig(s.Lhs[len(s.Lhs)-1].End()), ", "+failVar, 2)
	case *ast.DeclStmt:
		v := s.Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
		if len(v.Names) != len(values) {
			fail("? here yields " + itoa(len(values)) + " value(s), but " + itoa(len(v.Names)) + " are declared")
			return
		}
		f.insert(f.orig(v.Names[len(v.Names)-1].End()), ", "+failVar, 2)
	case *ast.ExprStmt:
		if plain {
			f.insert(f.orig(s.Pos()), "if "+strings.Repeat("_, ", len(values))+"err := ", 0)
			check = &gtext{}
			check.s("; err != nil { ")
			check.add(ret)
			check.s(" }")
			break
		}
		f.insert(f.orig(s.Pos()), strings.Repeat("_, ", len(values))+failVar+" := ", 0)
	case *ast.ReturnStmt:
		vars := make([]string, len(values))
		for i := range vars {
			vars[i] = "__v" + n + "_" + itoa(i+1)
		}
		ok, msg := e.successReturn(f, sig, k.option, vars)
		if msg != "" {
			fail(msg)
			return
		}
		lhs := strings.Join(append(vars, failVar), ", ")
		start := f.orig(s.Pos())
		f.add(start, start+len("return"), lhs+" :=")
		check.s("; " + ok)
	}
	f.add(t.off, end, k.suffix)
	f.put(check, end, t.off, t, 3)
	f.touch(sp)
	t.done, t.fresh = true, true
	e.progress = true
}

// tryKind is what a ? operand holds and how it fails.
type tryKind struct {
	values []types.Type // what ? yields
	suffix string       // written after the operand: .Get() for a Result or an Option
	option bool         // None fails, not an error
}

func (e *engine) tryKindOf(op ast.Expr, t types.Type) (tryKind, string) {
	switch kind, arg := runtimeType(t); {
	case kind == "Result":
		return tryKind{values: []types.Type{arg}, suffix: ".Get()"}, ""
	case kind == "Option":
		return tryKind{values: []types.Type{arg}, suffix: ".Get()", option: true}, ""
	case types.Identical(t, errorType):
		return tryKind{}, ""
	}
	tup, ok := t.(*types.Tuple)
	if !ok || tup.Len() < 2 || !types.Identical(tup.At(tup.Len()-1).Type(), errorType) {
		return tryKind{}, "? needs (T, error), error, a Result or an Option; " + types.ExprString(op) + " is " + e.display(t)
	}
	var k tryKind
	for i := 0; i < tup.Len()-1; i++ {
		k.values = append(k.values, tup.At(i).Type())
	}
	return k, ""
}

// wholeStmt reports whether op is all of stmt's value, as a statement
// ending in ? has it: x := op, x = op, var x = op, return op, op.
func (f *fileState) wholeStmt(stmt ast.Stmt, op ast.Expr) bool {
	switch f.parents[stmt].(type) {
	case *ast.BlockStmt, *ast.LabeledStmt:
	case *ast.CaseClause:
		if c := f.parents[stmt].(*ast.CaseClause); !inList(c.Body, stmt) {
			return false
		}
	case *ast.CommClause:
		if c := f.parents[stmt].(*ast.CommClause); !inList(c.Body, stmt) {
			return false
		}
	default:
		return false
	}
	var operand ast.Expr
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 && (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) {
			operand = s.Rhs[0]
		}
	case *ast.ReturnStmt:
		if len(s.Results) == 1 {
			operand = s.Results[0]
		}
	case *ast.ExprStmt:
		operand = s.X
	case *ast.DeclStmt:
		if g, ok := s.Decl.(*ast.GenDecl); ok && g.Tok == token.VAR && len(g.Specs) == 1 {
			if v := g.Specs[0].(*ast.ValueSpec); len(v.Values) == 1 && v.Type == nil {
				operand = v.Values[0]
			}
		}
	}
	return operand == op
}

func inList(list []ast.Stmt, s ast.Stmt) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// tryOperand is what the ? at at unwraps: the expression ending there that
// a postfix operator binds, as Rust's ? does — calls, selectors, indexes,
// parentheses, not a binary or unary expression around them.
func (f *fileState) tryOperand(at int) ast.Expr {
	var inner ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || f.off(n.Pos()) > at || f.off(n.End()) < at {
			return false
		}
		if x, ok := n.(ast.Expr); ok && f.off(x.End()) == at && f.off(x.Pos()) < at {
			inner = x
		}
		return true
	})
	if inner == nil {
		return nil
	}
	for {
		p, ok := f.parents[inner].(ast.Expr)
		if !ok || f.off(p.End()) != at {
			return inner
		}
		switch p.(type) {
		case *ast.CallExpr, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.SliceExpr, *ast.TypeAssertExpr, *ast.ParenExpr, *ast.CompositeLit:
			inner = p
		default:
			return inner
		}
	}
}

// stmtOf is the innermost statement holding n.
func (f *fileState) stmtOf(n ast.Node) ast.Stmt {
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		switch p := p.(type) {
		case *ast.FuncDecl:
			return nil
		case ast.Stmt:
			return p
		}
	}
	return nil
}

// topStmt is the statement a lowering of a ? in s may rewrite: s, or the
// if-else chain and label it is part of.
func (f *fileState) topStmt(s ast.Stmt) ast.Stmt {
	for {
		switch p := f.parents[s].(type) {
		case *ast.IfStmt:
			if p.Else == s || p.Init == s {
				s = p
				continue
			}
		case *ast.ForStmt:
			if p.Init == s || p.Post == s {
				s = p
				continue
			}
		case *ast.SwitchStmt:
			if p.Init == s {
				s = p
				continue
			}
		case *ast.TypeSwitchStmt:
			if p.Init == s || p.Assign == s {
				s = p
				continue
			}
		case *ast.LabeledStmt:
			s = p
			continue
		}
		return s
	}
}

func (f *fileState) touch(sp span) { f.touched = append(f.touched, sp) }

func (f *fileState) isTouched(sp span) bool {
	for _, t := range f.touched {
		if t.start < sp.end && sp.start < t.end {
			return true
		}
	}
	return false
}

// origEnd is the src offset just after the node ending at p.
func (f *fileState) origEnd(p token.Pos) int {
	off := f.off(p)
	if off == 0 || off > f.body {
		return f.orig(p)
	}
	return f.cur.toOrig(off-1) + 1
}

// errFree reports whether the function holding stmt names nothing err in
// its source, and the package declares no err: then ? declares err itself.
// The err the lowering writes (an around decorator's, a ? lowered
// before) doesn't count; the answer is kept per function.
func (e *engine) errFree(f *fileState, stmt ast.Stmt) bool {
	var fd *ast.FuncDecl
	for p := f.parents[stmt]; p != nil; p = f.parents[p] {
		if d, ok := p.(*ast.FuncDecl); ok {
			fd = d
		}
	}
	if fd == nil || e.declared["err"] {
		return false
	}
	key := f.name + " " + declKey(fd)
	if free, ok := e.errFuncs[key]; ok {
		return free
	}
	free := true
	ast.Inspect(fd, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "err" && f.orig(id.End())-f.orig(id.Pos()) == len("err") {
			free = false
		}
		return free
	})
	if e.errFuncs == nil {
		e.errFuncs = map[string]bool{}
	}
	e.errFuncs[key] = free
	return free
}

// failReturn is the statement that hands fe, the failure, to the caller.
func (e *engine) failReturn(f *fileState, sig *types.Signature, fe *gtext) (*gtext, string) {
	res := sig.Results()
	n := res.Len()
	g := &gtext{}
	if n > 0 && types.Identical(res.At(n-1).Type(), errorType) {
		g.s("return ")
		for i := 0; i < n-1; i++ {
			z, msg := e.zero(f, res.At(i).Type())
			if msg != "" {
				return nil, msg
			}
			g.s(z + ", ")
		}
		g.add(fe)
		return g, ""
	}
	if kind, arg := runtimeType(single(res)); kind == "Result" {
		s, msg := e.typeText(f, arg)
		g.s("return " + f.runtime() + ".Err[" + s + "](")
		g.add(fe)
		g.s(")")
		return g, msg
	}
	return nil, "? passes the error on, so the function must return an error last, or a Result"
}

// successReturn is what `return x?` returns when x succeeded.
func (e *engine) successReturn(f *fileState, sig *types.Signature, option bool, vars []string) (string, string) {
	res := sig.Results()
	n := res.Len()
	if n == len(vars)+1 && types.Identical(res.At(n-1).Type(), errorType) {
		return "return " + strings.Join(append(vars, "nil"), ", "), ""
	}
	if n == len(vars) && testParam(sig) != "" {
		return "return " + strings.Join(vars, ", "), "" // a test helper: failing fails the test
	}
	if kind, arg := runtimeType(single(res)); kind != "" && len(vars) == 1 && (kind == "Option") == option {
		s, msg := e.typeText(f, arg)
		ctor := ".Ok["
		if option {
			ctor = ".Some["
		}
		return "return " + f.runtime() + ctor + s + "](" + vars[0] + ")", msg
	}
	return "", "return f()? needs the function's results to match what ? yields"
}

func single(t *types.Tuple) types.Type {
	if t.Len() != 1 {
		return nil
	}
	return t.At(0).Type()
}

// gtext is generated text: made-up pieces and copies of the source.
type gtext struct{ parts []gpart }

type gpart struct {
	text     string
	from, to int // a copy of src[from:to]
	copy     bool
	excl     bool // see edit.excl
	own      any  // when not nil, the copy's owner, else put's
}

func (g *gtext) s(text string) { g.parts = append(g.parts, gpart{text: text}) }

func (g *gtext) c(from, to int) {
	if to > from {
		g.parts = append(g.parts, gpart{from: from, to: to, copy: true})
	}
}

// cx is a copy moved out of where a name now stands.
func (g *gtext) cx(from, to int) {
	if to > from {
		g.parts = append(g.parts, gpart{from: from, to: to, copy: true, excl: true})
	}
}

// co is a copy owned by own.
func (g *gtext) co(from, to int, own any) {
	if to > from {
		g.parts = append(g.parts, gpart{from: from, to: to, copy: true, own: own})
	}
}

func (g *gtext) add(o *gtext) {
	if o != nil {
		g.parts = append(g.parts, o.parts...)
	}
}

// put inserts g at src offset off, its made-up text attributed to src: a
// copy is the source with the rewrites inside it applied.
func (f *fileState) put(g *gtext, off, src int, own any, prio int) {
	for _, p := range g.parts {
		if p.copy {
			o := own
			if p.own != nil {
				o = p.own
			}
			f.fixed = append(f.fixed, edit{start: off, end: off, text: string(f.src[p.from:p.to]), src: p.from + 1, copy: true, own: o, excl: p.excl, prio: prio})
			continue
		}
		if p.text != "" {
			f.fixed = append(f.fixed, edit{start: off, end: off, text: p.text, src: src + 1, own: own, prio: prio})
		}
	}
}

// nextPrio orders an insertion after every earlier lowering's at the same
// offset, and before the statement-level ones (prio 0 and up).
func (f *fileState) nextPrio() int {
	f.seq++
	return -1_000_000 + f.seq
}
