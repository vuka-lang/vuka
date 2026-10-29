package transpile

import (
	"go/ast"

	"go/types"
)

// vetTries finds, once, the ? vuka vet warns about: more than one in an
// expression (W0109), one on the right of && or || (W0110), and with
// Options.WrapErrors a bare one on a call into another package (W0111).
func (e *engine) vetTries(f *fileState) {
	if f.tryVetDone || len(f.tries) == 0 {
		return
	}
	f.tryVetDone = true
	count := map[ast.Node]int{}
	for _, t := range f.tries {
		if t.dead || t.done {
			continue
		}
		op := f.tryOperand(f.cur.fromOrig(t.off))
		if op == nil {
			continue
		}
		s := f.stmtOf(op)
		if s == nil {
			continue
		}
		root := ast.Node(s)
		switch x := s.(type) {
		case *ast.IfStmt:
			root = x.Cond
		case *ast.ForStmt:
			root = x.Cond
		case *ast.SwitchStmt:
			root = x.Tag
		case *ast.RangeStmt:
			root = x.X
		}
		if count[root]++; count[root] == 2 {
			f.tryWarns.add("W0109", f.at(t.off), "more than one ? in one expression: each may return").
				help("give each its own statement")
		}
		if conds := f.condsOf(op, root); len(conds) > 0 {
			f.tryWarns.add("W0110", f.at(t.off), "? on the right of %s returns only when the left side lets it run", conds[len(conds)-1].Op).
				help("give it a statement of its own")
		}
		if e.wrapErrors && t.args.start < 0 {
			if pkg := e.calleePkg(op); pkg != nil && pkg != e.pkg {
				f.tryWarns.add("W0111", f.at(t.off), "a bare ? passes on %s's error without saying what failed", types.ExprString(ast.Unparen(op).(*ast.CallExpr).Fun)).
					help(`add context: ?("…")`)
			}
		}
	}
}

// calleePkg is the package of the function or method op calls, nil when
// it is no call of one.
func (e *engine) calleePkg(op ast.Expr) *types.Package {
	c, ok := ast.Unparen(op).(*ast.CallExpr)
	if !ok {
		return nil
	}
	var id *ast.Ident
	switch fun := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	}
	if id == nil {
		return nil
	}
	if fn, ok := e.info.Uses[id].(*types.Func); ok {
		return fn.Pkg()
	}
	return nil
}
