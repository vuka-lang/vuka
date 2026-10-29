package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lowerTryIn lowers a ? inside an expression: its operand is called in
// statements before the statement holding it, which then reads the value.
// Go's order of evaluation is kept: the calls and receives before the
// operand are called into variables first, an if's or switch's init moves
// before it, an else if becomes an if of its own, and the right side of
// && and || runs only when the left side lets it. A header that can't
// simply move is rewritten one step a round (an else, an init), the ? then
// lowered in the rewritten code.
func (e *engine) lowerTryIn(f *fileState, t *try, op ast.Expr, s ast.Stmt, k tryKind, sp span) {
	fail := func(msg string) {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "%s", msg)
	}
	p := f.parents[s]
	if _, ok := s.(*ast.CaseClause); ok {
		fail("? can't be in a case: cases are evaluated one by one, until one matches; pull it into a statement before the switch, or use if/else")
		return
	}
	if c, ok := p.(*ast.CommClause); ok && c.Comm == s {
		fail("? can't be in a select case; pull it into a statement before the select")
		return
	}
	for _, tr := range f.jsx {
		if tr.start <= t.off && t.off < tr.end && f.orig(s.Pos()) < tr.start {
			fail("? can't be in markup: call it in a statement before the markup, x := f()?, and write {x}")
			return
		}
	}
	if len(k.values) != 1 {
		if len(k.values) == 0 {
			fail("? on an error yields no value: give it a statement of its own, f()?")
		} else {
			fail("? here yields " + itoa(len(k.values)) + " values: give it a statement of its own, a, b := f()?")
		}
		return
	}
	step := func() {
		f.touch(sp)
		e.progress = true
	}
	elseIf := func(n ast.Stmt) bool {
		pi, ok := f.parents[n].(*ast.IfStmt)
		return ok && pi.Else == n
	}
	var root ast.Node
	var anchor ast.Stmt
	switch x := s.(type) {
	case *ast.IfStmt:
		switch {
		case elseIf(x):
			e.unElse(f, x)
			step()
			return
		case x.Init != nil:
			e.moveInit(f, x)
			step()
			return
		}
		e.lowerIfCond(f, t, op, x, k, sp)
		return
	case *ast.ForStmt:
		e.lowerForCond(f, t, op, x, k, sp)
		return
	case *ast.SwitchStmt:
		if x.Init != nil {
			e.moveInit(f, x)
			step()
			return
		}
		root, anchor = x.Tag, x
	case *ast.RangeStmt:
		if !within(f, op, x.X) {
			fail("? can't be in a range loop's key or value")
			return
		}
		root, anchor = x.X, x
	}
	if pp, ok := p.(*ast.IfStmt); ok && pp.Init == s && f.matchHead(pp) {
		root, anchor = s, pp // match f()? { … }: the subject's call before the match
	}
	if anchor == nil {
		switch pp := p.(type) {
		case *ast.IfStmt:
			if elseIf(pp) {
				e.unElse(f, pp)
			} else {
				e.moveInit(f, pp)
			}
			step()
			return
		case *ast.SwitchStmt:
			e.moveInit(f, pp)
			step()
			return
		case *ast.TypeSwitchStmt:
			if pp.Init == s || pp.Init != nil {
				e.moveInit(f, pp)
				step()
				return
			}
			root, anchor = s, pp
		case *ast.ForStmt:
			if pp.Post == s {
				fail("? can't be in a for loop's post statement; move it into the loop's body")
				return
			}
			root, anchor = s, pp
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.LabeledStmt:
			root, anchor = s, s
		default:
			fail("? can't be lowered here; give it a statement of its own")
			return
		}
	}
	e.lowerTryAt(f, t, op, root, anchor, k, sp)
}

// lowerTryAt lowers the ? of op, in root, before anchor: a hoist, or the
// right side of && or || run under an if.
func (e *engine) lowerTryAt(f *fileState, t *try, op ast.Expr, root ast.Node, anchor ast.Stmt, k tryKind, sp span) {
	conds := f.condsOf(op, root)
	switch len(conds) {
	case 0:
		e.hoist(f, t, op, root, anchor, k, sp)
	case 1:
		e.flag(f, t, op, root, conds[0], anchor, k, sp)
	default:
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "this ? is on the right of more than one && or ||: give it a statement of its own")
	}
}

// matchHead reports whether x stands for a match not lowered yet.
func (f *fileState) matchHead(x *ast.IfStmt) bool {
	for _, m := range f.matches {
		if !m.done && f.orig(x.Pos()) == m.start {
			return true
		}
	}
	return false
}

// within reports whether n is inside of.
func within(f *fileState, n, of ast.Node) bool {
	return of != nil && f.off(of.Pos()) <= f.off(n.Pos()) && f.off(n.End()) <= f.off(of.End())
}

// condsOf are the && and || in root that op is on the right side of,
// outermost first: whether op runs depends on their left sides.
func (f *fileState) condsOf(op ast.Expr, root ast.Node) []*ast.BinaryExpr {
	var out []*ast.BinaryExpr
	for n := ast.Node(op); n != root; {
		p := f.parents[n]
		if p == nil {
			break
		}
		if _, ok := p.(*ast.FuncLit); ok {
			break
		}
		if b, ok := p.(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) && within(f, op, b.Y) {
			out = append([]*ast.BinaryExpr{b}, out...)
		}
		n = p
	}
	return out
}

// anchorAt is where statements go before anchor, in src; false when
// anchor starts with text Vuka wrote, which has no place of its own.
func (f *fileState) anchorAt(anchor ast.Stmt) (int, bool) {
	if l, ok := f.parents[anchor].(*ast.LabeledStmt); ok {
		anchor = l
	}
	at := f.orig(anchor.Pos())
	return at, f.cur.fromOrig(at) == f.off(anchor.Pos())
}

// hoist writes, before anchor, the calls before op in root and then op
// with its check, and puts their variables where they were.
func (e *engine) hoist(f *fileState, t *try, op ast.Expr, root ast.Node, anchor ast.Stmt, k tryKind, sp span) {
	at, ok := f.anchorAt(anchor)
	if !ok {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "this ? is in code a lowering wrote; give it a statement of its own")
		return
	}
	g := &gtext{}
	e.spill(f, t, g, root, op)
	if !e.tryStmt(f, t, g, op, anchor, k) {
		return
	}
	f.put(g, at, t.off, t, f.nextPrio())
	e.lowered(f, t, sp)
}

func (e *engine) lowered(f *fileState, t *try, sp span) {
	f.touch(sp)
	t.done, t.fresh = true, true
	e.progress = true
}

// tryStmt writes op's statement and check into g and puts op's variable
// where op was: name, err := op; if err != nil { … }.
func (e *engine) tryStmt(f *fileState, t *try, g *gtext, op ast.Expr, at ast.Stmt, k tryKind) bool {
	name, failVar, cond := e.tryVars(f, t, op, at, k)
	ret, msg := e.failBranch(f, t, at, k, failVar)
	if msg != "" {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "%s", msg)
		return false
	}
	start := f.orig(op.Pos())
	g.s(name + ", " + failVar + " := ")
	g.cx(start, t.off)
	g.s(k.suffix + "; if " + cond + " { ")
	g.add(ret)
	g.s(" }; ")
	f.fixed = append(f.fixed, edit{start: start, end: t.off, text: name, own: t}, edit{start: t.off, end: t.end(), own: t})
	return true
}

// tryVars are the names of op's value and error, and the failure's test.
func (e *engine) tryVars(f *fileState, t *try, op ast.Expr, at ast.Stmt, k tryKind) (name, failVar, cond string) {
	name = f.tmpName(e.valueHint(op, k.values[0]))
	n := itoa(t.n)
	switch {
	case k.option:
		return name, "__ok" + n, "!__ok" + n
	case e.errFree(f, at):
		return name, "err", "err != nil"
	}
	return name, "__e" + n, "__e" + n + " != nil"
}

// tmpName is a variable of a lowering, named hint when tidy finds that
// clashes with nothing (localNames).
func (f *fileState) tmpName(hint string) string {
	f.seq++
	return "__h" + itoa(f.seq) + "_" + hint
}

// spill writes into g, in order, each call and receive in root that Go
// evaluates before target, into a variable of its own; plain reads stay
// where they are, as Go leaves their order open.
func (e *engine) spill(f *fileState, t *try, g *gtext, root ast.Node, target ast.Node) {
	end := f.off(target.Pos())
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || f.off(n.Pos()) >= end {
			return false
		}
		if f.off(n.End()) > end {
			return true // holds target
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if !e.isCall(n) {
				return true
			}
		case *ast.UnaryExpr:
			if n.Op != token.ARROW {
				return true
			}
		default:
			return true
		}
		x := n.(ast.Expr)
		tv := e.info.Types[x]
		if _, tuple := tv.Type.(*types.Tuple); isInvalid(tv.Type) || tuple {
			return true
		}
		name := f.tmpName(e.valueHint(x, tv.Type))
		start, stop := f.orig(x.Pos()), f.origEnd(x.End())
		g.s(name + " := ")
		g.cx(start, stop)
		g.s("; ")
		f.fixed = append(f.fixed, edit{start: start, end: stop, text: name, own: t})
		return false
	})
}

// isCall reports whether c calls a function: not a conversion, nor a
// builtin but recover.
func (e *engine) isCall(c *ast.CallExpr) bool {
	tv := e.info.Types[c.Fun]
	if tv.IsType() {
		return false
	}
	if tv.IsBuiltin() {
		id, ok := ast.Unparen(c.Fun).(*ast.Ident)
		return ok && id.Name == "recover"
	}
	return true
}

// flag lowers op on the right of b: the && or || becomes a variable an
// if before anchor sets, op's statement running inside it:
//
//	var allowed bool
//	if a { var err error; if allowed, err = f(); err != nil { … } }
//	if allowed { … }
func (e *engine) flag(f *fileState, t *try, op ast.Expr, root ast.Node, b *ast.BinaryExpr, anchor ast.Stmt, k tryKind, sp span) {
	fail := func(msg string) {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "%s", msg)
	}
	at, ok := f.anchorAt(anchor)
	if !ok {
		fail("this ? is in code a lowering wrote; give it a statement of its own")
		return
	}
	bStart, bEnd := f.orig(b.Pos()), f.origEnd(b.End())
	if other := f.pendingIn(t, bStart, bEnd); other != nil {
		fail("more than one ? on the right of " + b.Op.String() + ": give them statements of their own")
		return
	}
	m := new(int) // owns the copies of b's sides
	g := &gtext{}
	e.spill(f, t, g, root, b)
	r := ast.Unparen(b.Y)
	name := f.tmpName("ok")
	if r == op {
		name = f.tmpName(e.valueHint(op, k.values[0]))
	}
	if b.Op == token.LAND {
		g.s("var " + name + " bool; if ")
		g.co(f.orig(b.X.Pos()), f.origEnd(b.X.End()), m)
	} else {
		g.s(name + " := true; if ")
		e.negate(f, g, b.X, m)
	}
	g.s(" { ")
	if r == op {
		_, failVar, cond := e.tryVars(f, t, op, anchor, k)
		ret, msg := e.failBranch(f, t, anchor, k, failVar)
		if msg != "" {
			fail(msg)
			return
		}
		typ := "error"
		if k.option {
			typ = "bool"
		}
		g.s("var " + failVar + " " + typ + "; if " + name + ", " + failVar + " = ")
		g.c(f.orig(op.Pos()), t.off)
		g.s(k.suffix + "; " + cond + " { ")
		g.add(ret)
		g.s(" } }; ")
		f.fixed = append(f.fixed, edit{start: t.off, end: t.end(), own: t})
	} else {
		e.spill(f, t, g, b.Y, op)
		if !e.tryStmt(f, t, g, op, anchor, k) {
			return
		}
		g.s(name + " = ")
		g.co(f.orig(b.Y.Pos()), f.origEnd(b.Y.End()), m)
		g.s(" }; ")
	}
	f.put(g, at, t.off, t, f.nextPrio())
	f.fixed = append(f.fixed, edit{start: bStart, end: bEnd, text: name, own: m, stay: true})
	e.lowered(f, t, sp)
}

// pendingIn is a ? other than t not lowered yet in src[from:to].
func (f *fileState) pendingIn(t *try, from, to int) *try {
	for _, o := range f.tries {
		if o != t && !o.done && !o.dead && o.off >= from && o.off < to {
			return o
		}
	}
	return nil
}

// negated are the comparisons whose negation is another: those of
// integers and strings (a float's NaN compares false both ways).
var negated = map[token.Token]token.Token{
	token.EQL: token.NEQ, token.NEQ: token.EQL,
	token.LSS: token.GEQ, token.GEQ: token.LSS, token.GTR: token.LEQ, token.LEQ: token.GTR,
}

// negate writes !x into g, x copied as m's: !a, a for !a, a != b for
// a == b, i >= n for i < n.
func (e *engine) negate(f *fileState, g *gtext, x ast.Expr, m any) {
	switch y := x.(type) {
	case *ast.UnaryExpr:
		if y.Op == token.NOT {
			g.co(f.orig(y.X.Pos()), f.origEnd(y.X.End()), m)
			return
		}
	case *ast.BinaryExpr:
		op, ok := negated[y.Op]
		if t := e.info.Types[y.X].Type; ok && y.Op != token.EQL && y.Op != token.NEQ {
			ok = false
			if t != nil {
				b, basic := t.Underlying().(*types.Basic)
				ok = basic && b.Info()&(types.IsInteger|types.IsString) != 0
			}
		}
		if ok {
			g.co(f.orig(y.X.Pos()), f.origEnd(y.X.End()), m)
			g.s(" " + op.String() + " ")
			g.co(f.orig(y.Y.Pos()), f.origEnd(y.Y.End()), m)
			return
		}
		g.s("!(")
		g.co(f.orig(x.Pos()), f.origEnd(x.End()), m)
		g.s(")")
		return
	}
	g.s("!")
	g.co(f.orig(x.Pos()), f.origEnd(x.End()), m)
}

// andChain is x's operands of top-level &&: a, b, c for a && b && c.
func andChain(x ast.Expr) []ast.Expr {
	if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.LAND {
		return append(andChain(b.X), b.Y)
	}
	return []ast.Expr{x}
}

// lowerIfCond lowers a ? in an if's condition: before the if, or, on the
// right of && in an if without else, in an if nested in the first:
//
//	if a && f()? { … }   →   if a { allowed, err := f(); if err != nil { … }; if allowed { … } }
func (e *engine) lowerIfCond(f *fileState, t *try, op ast.Expr, x *ast.IfStmt, k tryKind, sp span) {
	if x.Else == nil {
		chain := andChain(x.Cond)
		for i, o := range chain {
			if i == 0 || !within(f, op, o) {
				continue
			}
			if len(f.condsOf(op, o)) > 0 {
				break
			}
			andStart, oStart := f.origEnd(chain[i-1].End()), f.orig(o.Pos())
			g := &gtext{}
			g.s(" { ")
			e.spill(f, t, g, o, op)
			if !e.tryStmt(f, t, g, op, x, k) {
				return
			}
			g.s("if ")
			f.put(g, andStart, t.off, t, f.nextPrio())
			f.fixed = append(f.fixed, edit{start: andStart, end: oStart, own: t})
			f.insert(f.origEnd(x.End()), " }", f.nextPrio())
			e.lowered(f, t, sp)
			return
		}
	}
	e.lowerTryAt(f, t, op, x.Cond, x, k, sp)
}

// lowerForCond lowers a ? in a for loop's condition: the loop tests it
// first thing in its body, so continue still runs the post statement.
//
//	for f()? { … }   →   for { more, err := f(); if err != nil { … }; if !more { break }; … }
func (e *engine) lowerForCond(f *fileState, t *try, op ast.Expr, x *ast.ForStmt, k tryKind, sp span) {
	fail := func(msg string) {
		t.dead = true
		e.errs.add("E0110", f.at(t.off), "%s", msg)
	}
	condStart, condEnd := f.orig(x.Cond.Pos()), f.origEnd(x.Cond.End())
	if f.pendingIn(t, condStart, condEnd) != nil {
		fail("more than one ? in a loop's condition: give them statements of their own")
		return
	}
	m := new(int)
	g := &gtext{}
	for _, o := range andChain(x.Cond) {
		if within(f, op, o) {
			if len(f.condsOf(op, o)) > 0 {
				fail("this ? is on the right of || in a loop's condition: give it a statement of its own")
				return
			}
			e.spill(f, t, g, o, op)
			if !e.tryStmt(f, t, g, op, x, k) {
				return
			}
		}
		g.s("if ")
		e.negate(f, g, o, m)
		g.s(" { break }; ")
	}
	f.put(g, f.orig(x.Body.Lbrace)+1, t.off, t, f.nextPrio())
	f.fixed = append(f.fixed, edit{start: condStart, end: condEnd, own: m, stay: true})
	e.lowered(f, t, sp)
}

// unElse makes the else if x an if of its own: after the chain when every
// branch before it leaves (returns, breaks, continues, panics), the guard
// clauses kept flat; else in an else block.
func (e *engine) unElse(f *fileState, x *ast.IfStmt) {
	p := f.parents[x].(*ast.IfStmt)
	flat := true
	for n := ast.Node(p); ; {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			break
		}
		if ifs.Init != nil || len(ifs.Body.List) == 0 || !e.leaves(ifs.Body.List[len(ifs.Body.List)-1]) {
			flat = false
		}
		parent, ok := f.parents[ifs].(*ast.IfStmt)
		if !ok || parent.Else != ifs {
			break
		}
		n = parent
	}
	from, to := f.origEnd(p.Body.End()), f.orig(x.Pos())
	if flat {
		f.add(from, to, "; ")
		return
	}
	f.add(from, to, " else { ")
	f.insert(f.origEnd(x.End()), " }", f.nextPrio())
}

// leaves reports whether s ends its branch: return, break, continue, goto
// or panic.
func (e *engine) leaves(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok != token.FALLTHROUGH
	case *ast.ExprStmt:
		return e.isPanic(s)
	}
	return false
}

// moveInit moves x's init statement before x, in a block of their own
// when a name it declares would clash with the code after x.
func (e *engine) moveInit(f *fileState, x ast.Stmt) {
	var init ast.Stmt
	var next token.Pos
	switch x := x.(type) {
	case *ast.IfStmt:
		init, next = x.Init, x.Cond.Pos()
	case *ast.SwitchStmt:
		init, next = x.Init, x.Body.Lbrace
		if x.Tag != nil {
			next = x.Tag.Pos()
		}
	case *ast.TypeSwitchStmt:
		init, next = x.Init, x.Assign.Pos()
	}
	anchor := ast.Stmt(x)
	if l, ok := f.parents[x].(*ast.LabeledStmt); ok {
		anchor = l
	}
	m := new(int)
	from, to := f.orig(init.Pos()), f.orig(next)
	g := &gtext{}
	clash := e.initClash(f, init, anchor)
	if clash {
		g.s("{ ")
	}
	g.co(from, to, m)
	f.put(g, f.orig(anchor.Pos()), from, m, f.nextPrio())
	f.fixed = append(f.fixed, edit{start: from, end: to, own: m, stay: true})
	if clash {
		f.insert(f.origEnd(anchor.End()), " }", f.nextPrio())
	}
}

// initClash reports whether a name init declares would clash, moved
// before anchor: declared in the block already, or named after anchor.
func (e *engine) initClash(f *fileState, init ast.Stmt, anchor ast.Stmt) bool {
	var names []string
	switch s := init.(type) {
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			for _, l := range s.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
					names = append(names, id.Name)
				}
			}
		}
	case *ast.DeclStmt:
		if g, ok := s.Decl.(*ast.GenDecl); ok {
			for _, sp := range g.Specs {
				if v, ok := sp.(*ast.ValueSpec); ok {
					for _, id := range v.Names {
						names = append(names, id.Name)
					}
				}
			}
		}
	}
	if len(names) == 0 {
		return false
	}
	scope := e.pkg.Scope().Innermost(anchor.Pos() - 1)
	var after []ast.Stmt
	switch p := f.parents[anchor].(type) {
	case *ast.BlockStmt:
		after = p.List
	case *ast.CaseClause:
		after = p.Body
	case *ast.CommClause:
		after = p.Body
	default:
		return true
	}
	for _, n := range names {
		if scope != nil && scope.Lookup(n) != nil {
			return true
		}
		for _, s := range after {
			if s.Pos() <= anchor.Pos() {
				continue
			}
			used := false
			ast.Inspect(s, func(x ast.Node) bool {
				if id, ok := x.(*ast.Ident); ok && id.Name == n {
					used = true
				}
				return !used
			})
			if used {
				return true
			}
		}
	}
	return false
}

// valueHint is the name a lowering's variable holding x, of type t, goes
// by: after its type (User user, []Post posts, Status status), else what
// it calls (Exists exists, GetUser user), else v.
func (e *engine) valueHint(x ast.Expr, t types.Type) string {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	plural := false
	if s, ok := t.(*types.Slice); ok {
		t, plural = s.Elem(), true
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
	}
	name := ""
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() != RuntimePath {
		name = n.Obj().Name()
		if plural {
			name = pluralName(name)
		}
	}
	if name == "" {
		if c, ok := ast.Unparen(x).(*ast.CallExpr); ok {
			fun := ast.Unparen(c.Fun)
			switch y := fun.(type) {
			case *ast.IndexExpr:
				fun = y.X
			case *ast.IndexListExpr:
				fun = y.X
			}
			switch y := fun.(type) {
			case *ast.Ident:
				name = y.Name
			case *ast.SelectorExpr:
				name = y.Sel.Name
			}
			if rest, ok := strings.CutPrefix(name, "Get"); ok && rest != "" {
				if r, _ := utf8.DecodeRuneInString(rest); unicode.IsUpper(r) {
					name = rest
				}
			}
		}
	}
	name = lowerName(name)
	if name == "" || !token.IsIdentifier(name) || name == "err" || name == "ok" || name == "_" {
		return "v"
	}
	return name
}

func pluralName(s string) string { return plural(s) }

// lowerName is a Go name with its leading initialism or capital lowered:
// User user, HTTPClient httpClient, ID id.
func lowerName(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	n := 0
	for n < len(rs) && unicode.IsUpper(rs[n]) {
		n++
	}
	switch {
	case n == 0:
		return s
	case n == 1 || n == len(rs):
	case unicode.IsLower(rs[n]) && string(rs[n:]) != "s":
		n--
	}
	if n == len(rs)-1 && string(rs[n:]) == "s" {
		n = len(rs)
	}
	for i := 0; i < n; i++ {
		rs[i] = unicode.ToLower(rs[i])
	}
	return string(rs)
}
