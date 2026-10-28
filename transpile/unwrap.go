package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// A call in markup whose results are (T, error) is its T: the target's
// Must takes the two values and fails the render with the error.
//
//	<span>{s.Users()}</span>   →   ui.HTML(`<span>{ui.Must(s.Users())}</span>`, ui.Must(s.Users()))
//
// That holds wherever markup takes one value — a hole, an attribute, a
// component's prop, a for's range, an if's or a switch's condition —
// and nowhere else: a call passing its results on to another,
// fmt.Sprint(s.Users()), keeps them, and so does a template's statement
// binding them both, users, err := s.Users(); one binding a name to each
// value, users := s.Users(), unwraps them. Any other call with more than one result
// there, or with only an error shown as a value, is E0160.

// unwrapCalls wraps the markup's (T, error) calls in Must and reports the
// others. It reports whether it changed the source: the round then starts
// again, so nothing is decided from the types of the calls as written.
func (e *engine) unwrapCalls(f *fileState) bool {
	if e.info == nil || len(f.jsx) == 0 || f.target == nil {
		return false
	}
	if f.unwrapped == nil {
		f.unwrapped = map[span]bool{}
	}
	changed := false
	for _, x := range e.markupValues(f) {
		e.unwrapIn(f, x.expr, x.shown, &changed)
	}
	if changed {
		e.progress = true
	}
	return changed
}

// markupValue is an expression markup takes one value of; shown when markup
// shows it as text (a hole, an element's attribute), not when it passes it
// to a component, tests it or ranges over it.
type markupValue struct {
	expr  ast.Expr
	shown bool
}

// markupValues are the file's markup expressions taking one value, in this
// round's syntax tree.
func (e *engine) markupValues(f *fileState) []markupValue {
	var out []markupValue
	add := func(s span, shown bool) {
		if s.end <= s.start {
			return
		}
		if x := e.exprAt(f, s); x != nil {
			out = append(out, markupValue{x, shown})
		}
	}
	var heads, stmts []span
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				for _, a := range n.attrs {
					switch {
					case a.event || a.stmts || a.bind != nil || a.write != nil || strings.HasPrefix(a.name, "bind:"):
					case a.kind == 'e':
						add(a.expr, n.comp == nil)
					case a.kind == 'p':
						for _, p := range a.parts {
							add(p.expr, true)
						}
					}
				}
				walk(n.kids)
			case *jsxHole:
				if n.bind == nil {
					add(n.expr, true)
				}
			case *jsxBlock:
				switch {
				case n.stmt:
					stmts = append(stmts, n.heads...)
				case n.m == nil:
					heads = append(heads, n.heads...)
				}
				for _, b := range n.bodies {
					walk(b)
				}
				for _, c := range n.cases {
					walk(c)
				}
			}
		}
	}
	for _, t := range f.jsx {
		walk([]jsxNode{t.root})
	}
	if len(heads) == 0 && len(stmts) == 0 {
		return out
	}
	// The conditions, ranges and switch tags of the blocks' headers, and
	// the values a statement binds one name each to.
	within := func(n ast.Node, spans []span) bool {
		at := f.orig(n.Pos())
		for _, h := range spans {
			if at >= h.start && at < h.end {
				return true
			}
		}
		return false
	}
	in := func(n ast.Node) bool { return within(n, heads) }
	ast.Inspect(f.ast, func(n ast.Node) bool {
		var xs []ast.Expr
		switch n := n.(type) {
		case *ast.FuncLit:
			return !within(n, stmts) // a statement's function literal is Go's
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) && within(n, stmts) {
				for _, x := range n.Rhs {
					out = append(out, markupValue{x, false})
				}
			}
			return true
		case *ast.IfStmt:
			xs = []ast.Expr{n.Cond}
		case *ast.ForStmt:
			xs = []ast.Expr{n.Cond}
		case *ast.RangeStmt:
			xs = []ast.Expr{n.X}
		case *ast.SwitchStmt:
			xs = []ast.Expr{n.Tag}
		case *ast.CaseClause:
			xs = n.List
		default:
			return true
		}
		if in(n) {
			for _, x := range xs {
				if x != nil {
					out = append(out, markupValue{x, false})
				}
			}
		}
		return true
	})
	return out
}

// unwrapIn unwraps the calls in root taking one value: root itself, and
// any call inside it but one passing its results on as a call's only
// argument.
func (e *engine) unwrapIn(f *fileState, root ast.Expr, shown bool, changed *bool) {
	var visit func(n ast.Node, single bool)
	visit = func(n ast.Node, single bool) {
		if n == nil {
			return
		}
		if c, ok := n.(*ast.CallExpr); ok && single {
			if e.unwrapCall(f, c, shown && n == root) {
				*changed = true
				return
			}
		}
		if c, ok := n.(*ast.CallExpr); ok {
			visit(c.Fun, true)
			spreads := len(c.Args) == 1 && !c.Ellipsis.IsValid() && e.isFunc(c.Fun)
			for _, a := range c.Args {
				visit(a, !spreads)
			}
			return
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return // its statements are Go's
		}
		ast.Inspect(n, func(m ast.Node) bool {
			if m == n || m == nil {
				return true
			}
			visit(m, true)
			return false
		})
	}
	visit(root, true)
}

// unwrapCall wraps c, taking one value, in the target's Must when it
// returns (T, error), and reports E0160 when it returns other values
// markup can't take — or only an error, when shown is set. It reports
// whether it wrapped c.
func (e *engine) unwrapCall(f *fileState, c *ast.CallExpr, shown bool) bool {
	tv, ok := e.info.Types[c]
	if !ok || tv.Type == nil {
		return false
	}
	start, end := f.orig(c.Pos()), f.orig(c.End())
	if f.unwrapped[span{start, end}] || end <= start || end > len(f.src) || f.src[end-1] != ')' {
		return false
	}
	tup, _ := tv.Type.(*types.Tuple)
	onlyErr := shown && types.Identical(tv.Type, errorType) && e.returnsErrorOnly(c)
	if tup == nil && !onlyErr || tup != nil && tup.Len() < 2 {
		return false
	}
	text := string(f.src[start:end])
	if tup != nil && tup.Len() == 2 && types.Identical(tup.At(1).Type(), errorType) && f.target.has("Must") {
		// The ) goes in before the call's own, the same text: inside the
		// call's span, so a label copying the span has it.
		f.unwrapped[span{start, end}] = true
		f.unwraps = append(f.unwraps, Unwrap{Off: start, Lparen: f.orig(c.Lparen), End: end, Type: e.display(tup.At(0).Type())})
		f.insert(start, f.target.q+"Must(", -1)
		f.insert(end-1, ")", -1)
		return true
	}
	f.unwrapped[span{start, end}] = true
	var err *Error
	switch {
	case onlyErr:
		err = e.errs.add("E0160", f.at(start), "%s returns only an error, which markup can't show", text).
			help("call it in Load, Mount or an event handler, and show a field it sets")
	case tup.Len() == 2 && types.Identical(tup.At(1).Type(), errorType):
		err = e.errs.add("E0160", f.at(start), "%s returns %s, and %s has no Must to unwrap it with", text, e.display(tup), f.target.path).
			help("call a method returning one value, or set a field in Load or Mount and show the field")
	default:
		err = e.errs.add("E0160", f.at(start), "%s returns %s; markup takes one value, unwrapping only a call returning (T, error)", text, e.display(tup)).
			help("call a method returning one value, or set a field in Load or Mount and show the field")
	}
	if !strings.Contains(text, "\n") {
		err.Len = len(text)
	}
	return false
}

// isFunc reports whether x is a function value, whose call takes another
// call's results as its arguments: not a builtin or a conversion.
func (e *engine) isFunc(x ast.Expr) bool {
	tv, ok := e.info.Types[x]
	if !ok || tv.IsType() || tv.IsBuiltin() {
		return false
	}
	_, ok = types.Unalias(tv.Type).Underlying().(*types.Signature)
	return ok
}

// returnsErrorOnly reports whether c calls a function whose one result is
// an error (not a conversion, not a value of type error).
func (e *engine) returnsErrorOnly(c *ast.CallExpr) bool {
	if !e.isFunc(c.Fun) {
		return false
	}
	sig := types.Unalias(e.info.Types[c.Fun].Type).Underlying().(*types.Signature)
	return sig.Results().Len() == 1
}

// Unwrap is a call in markup returning (T, error), unwrapped: where it is
// in the source — Off its first byte, Lparen its arguments' (, End just
// after its ) — and its T, as the package spells it.
type Unwrap struct {
	Off, Lparen, End int
	Type             string
}

// unwrapsOf are f's unwrapped calls, at their offsets in the file as written.
func (f *fileState) unwrapsOf() []Unwrap {
	out := slices.Clone(f.unwraps)
	if f.html != nil {
		for i := range out {
			u := &out[i]
			u.Off, u.Lparen, u.End = f.html.toSrc(u.Off), f.html.toSrc(u.Lparen), f.html.toSrc(u.End)
		}
	}
	return out
}
