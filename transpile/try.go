package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// lowerTry rewrites a statement ending in ?. The operand stays where it is; the
// statement gains an error variable and an early return after it:
//
//	u := load(id)?   →   u, __e1 := load(id); if __e1 != nil { return nil, __e1 }
//
// The operand may be Go's (T…, error) or error, a Result (unwrapped with Get),
// or an Option (in a function that returns an Option).
func (e *engine) lowerTry(f *fileState, t *try) {
	at := f.cur.fromOrig(t.off)
	var stmt ast.Stmt
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if s, ok := n.(ast.Stmt); ok && f.off(s.End()) == at {
			stmt = s
		}
		return n == nil || f.off(n.Pos()) <= at
	})
	fail := func(msg string) {
		t.dead = true
		e.errs.add(f.at(t.off), "%s", msg)
	}
	if stmt == nil {
		fail("? must end a statement: x := f()?, return f()?, or f()?")
		return
	}
	switch f.parents[stmt].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		fail("? can't be used in an if, for or switch header; give it its own statement")
		return
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
	if operand == nil || f.off(operand.End()) != at {
		fail("? must end a statement: x := f()?, return f()?, or f()?")
		return
	}
	tv, ok := e.info.Types[operand]
	if !ok || isInvalid(tv.Type) {
		t.fail = "the operand's type is unknown"
		return
	}

	// What the operand holds and how it fails.
	var values []types.Type
	suffix, option := "", false
	switch kind, arg := runtimeType(tv.Type); {
	case kind == "Result":
		values, suffix = []types.Type{arg}, ".Get()"
	case kind == "Option":
		values, suffix, option = []types.Type{arg}, ".Get()", true
	case types.Identical(tv.Type, errorType):
	default:
		tup, ok := tv.Type.(*types.Tuple)
		if !ok || tup.Len() < 2 || !types.Identical(tup.At(tup.Len()-1).Type(), errorType) {
			fail("? needs (T, error), error, a Result or an Option; " + types.ExprString(operand) + " is " + e.display(tv.Type))
			return
		}
		for i := 0; i < tup.Len()-1; i++ {
			values = append(values, tup.At(i).Type())
		}
	}

	sig, _ := e.enclosingFunc(f, stmt)
	if sig == nil {
		fail("? needs an enclosing function")
		return
	}
	n := itoa(t.n)
	failVar, cond := "__e"+n, "__e"+n+" != nil"
	if option {
		failVar, cond = "__ok"+n, "!__ok"+n
	}
	ret, msg := e.failReturn(f, sig, option, failVar)
	if msg != "" {
		fail(msg)
		return
	}
	check := "; if " + cond + " { " + ret + " }"
	end := t.off + 1

	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Lhs) != len(values) {
			fail("? here yields " + itoa(len(values)) + " value(s), but " + itoa(len(s.Lhs)) + " are assigned")
			return
		}
		if s.Tok == token.ASSIGN {
			typ := "error"
			if option {
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
		f.insert(f.orig(s.Pos()), strings.Repeat("_, ", len(values))+failVar+" := ", 0)
	case *ast.ReturnStmt:
		vars := make([]string, len(values))
		for i := range vars {
			vars[i] = "__v" + n + "_" + itoa(i+1)
		}
		ok, msg := e.successReturn(f, sig, option, vars)
		if msg != "" {
			fail(msg)
			return
		}
		lhs := strings.Join(append(vars, failVar), ", ")
		start := f.orig(s.Pos())
		f.add(start, start+len("return"), lhs+" :=")
		check += "; " + ok
	}
	f.add(t.off, t.off+1, suffix)
	f.insert(end, check, 3)
	t.done = true
	e.progress = true
}

// failReturn is the statement that hands the failure to the caller.
func (e *engine) failReturn(f *fileState, sig *types.Signature, option bool, failVar string) (string, string) {
	res := sig.Results()
	n := res.Len()
	if option {
		if kind, arg := runtimeType(single(res)); kind == "Option" {
			s, msg := e.typeText(f, arg)
			return "return " + f.runtime() + ".None[" + s + "]()", msg
		}
		return "", "? on an Option needs the function to return an Option"
	}
	if n > 0 && types.Identical(res.At(n-1).Type(), errorType) {
		parts := make([]string, 0, n)
		for i := 0; i < n-1; i++ {
			z, msg := e.zero(f, res.At(i).Type())
			if msg != "" {
				return "", msg
			}
			parts = append(parts, z)
		}
		return "return " + strings.Join(append(parts, failVar), ", "), ""
	}
	if kind, arg := runtimeType(single(res)); kind == "Result" {
		s, msg := e.typeText(f, arg)
		return "return " + f.runtime() + ".Err[" + s + "](" + failVar + ")", msg
	}
	return "", "? passes the error on, so the function must return an error last, or a Result"
}

// successReturn is what `return x?` returns when x succeeded.
func (e *engine) successReturn(f *fileState, sig *types.Signature, option bool, vars []string) (string, string) {
	res := sig.Results()
	n := res.Len()
	if !option && n == len(vars)+1 && types.Identical(res.At(n-1).Type(), errorType) {
		return "return " + strings.Join(append(vars, "nil"), ", "), ""
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
