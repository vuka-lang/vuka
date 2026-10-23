package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// Writes of client state are assignments. In an element's on… attribute
// they are a chain of JS commands the browser runs at once; a call among
// them is a push to the server, after the writes before it:
//
//	onClick={editing = !editing}         →  onClick={uigen.Toggle(ui.JS, editing)}
//	onClick={tab = "env"}                →  onClick={ui.JS.Set(tab, "env")}
//	onClick={tab = "env"; open = false}  →  onClick={ui.JS.Set(tab, "env").Set(open, false)}
//	onClick={open = false; Save()}       →  onClick={ui.JS.Set(open, false).Push(Save)}
//	onClick={n++}                        →  onClick={uigen.Inc(ui.JS, n)}
//
// In a method of a live component — a handler, a hook, an Update — a write
// is pushed to the page with the reply:
//
//	p.editing = false                    →  p.PushJS(p.editing.Set(false))
//	p.open = !p.open                     →  p.PushJS(uigen.Toggle(ui.JS, p.open))
//
// A state written anywhere else — while the markup renders, in a function
// that isn't a live component's method — is E0149; a write the browser
// can't run (x += 2, a value reading states, a statement that isn't a
// write or a call) is E0150.

// isStmts reports whether src, an attribute's {…}, is statements rather
// than an expression: open = !open, n++, a = 1; Save().
func isStmts(src []byte) bool {
	text := strings.TrimSpace(string(src))
	if text == "" {
		return false
	}
	if _, err := parser.ParseExpr(text); err == nil {
		return false
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+text+"\n}", 0)
	if err != nil || len(f.Decls) != 1 {
		return false
	}
	body := f.Decls[0].(*ast.FuncDecl).Body.List
	if len(body) == 0 {
		return false
	}
	for _, st := range body {
		switch st.(type) {
		case *ast.AssignStmt, *ast.IncDecStmt:
			return true
		}
	}
	return false
}

// writeStep is one statement of a write: how it extends the chain.
type writeStep struct {
	op       string // "Set", "Toggle", "Inc", "Dec", "Push"
	lhs, rhs ast.Expr
	call     *ast.CallExpr
	void     bool // a Push's call returns nothing (else an error)
}

// writeSteps reads statements as a chain's steps, or reports why one can't
// be: msg with the statement's offset. inAttr is whether they are an on…
// attribute's (calls are pushes there).
func (e *engine) writeSteps(f *fileState, stmts []ast.Stmt, inAttr bool) ([]writeStep, *bindError) {
	var out []writeStep
	fail := func(n ast.Node, format string, args ...any) ([]writeStep, *bindError) {
		return nil, &bindError{f.orig(n.Pos()), sprintf(format, args...)}
	}
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	for _, st := range stmts {
		switch st := st.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
				return fail(st, "a write sets one state: %s", text(st))
			}
			lhs, rhs := st.Lhs[0], st.Rhs[0]
			elem, ok := stateOf(e.info.TypeOf(lhs))
			if !ok {
				if inAttr {
					return fail(lhs, "%s isn't a ui.State: an on… attribute's statements write states; change server state in a handler, onClick={func() { %s }}", text(lhs), text(st))
				}
				return nil, nil
			}
			if st.Tok != token.ASSIGN {
				return fail(st, "%s %s …: a state is written with =, ++ or --; set it to a value (%s = v)", text(lhs), st.Tok, text(lhs))
			}
			if u, ok := ast.Unparen(rhs).(*ast.UnaryExpr); ok && u.Op == token.NOT && text(ast.Unparen(u.X)) == text(lhs) {
				if b, _ := elem.Underlying().(*types.Basic); b == nil || b.Info()&types.IsBoolean == 0 {
					return fail(st, "%s = !%s needs a ui.State[bool]; %s is a %s", text(lhs), text(lhs), text(lhs), e.display(e.info.TypeOf(lhs)))
				}
				out = append(out, writeStep{op: "Toggle", lhs: lhs})
				continue
			}
			if e.readsState(rhs) {
				return fail(rhs, "%s reads a state: a state is set to a value of the server's, or flipped (%s = !%s)", text(rhs), text(lhs), text(lhs))
			}
			tv, known := e.exprType(rhs)
			if !known {
				return nil, &bindError{-1, ""}
			}
			if !valueFits(tv, elem) {
				return fail(rhs, "%s is a %s; it takes a %s, not %s", text(lhs), e.display(e.info.TypeOf(lhs)), e.display(elem), e.display(tv.Type))
			}
			out = append(out, writeStep{op: "Set", lhs: lhs, rhs: rhs})
		case *ast.IncDecStmt:
			elem, ok := stateOf(e.info.TypeOf(st.X))
			if !ok {
				if inAttr {
					return fail(st.X, "%s isn't a ui.State: an on… attribute's statements write states; change server state in a handler, onClick={func() { %s }}", text(st.X), text(st))
				}
				return nil, nil
			}
			if b, _ := elem.Underlying().(*types.Basic); b == nil || b.Info()&types.IsNumeric == 0 {
				return fail(st, "%s%s needs a ui.State of a number; %s is a %s", text(st.X), st.Tok, text(st.X), e.display(e.info.TypeOf(st.X)))
			}
			op := "Inc"
			if st.Tok == token.DEC {
				op = "Dec"
			}
			out = append(out, writeStep{op: op, lhs: st.X})
		case *ast.ExprStmt:
			call, ok := st.X.(*ast.CallExpr)
			if !ok || !inAttr {
				return fail(st, "%s isn't a write: an on… attribute's statements are writes of states and calls", text(st))
			}
			tv, known := e.info.Types[call]
			if !known {
				return nil, &bindError{-1, ""}
			}
			void := tv.IsVoid()
			if !void && !types.Identical(tv.Type, types.Universe.Lookup("error").Type()) {
				return fail(call, "%s returns %s: a call in an on… attribute is a handler, returning nothing or an error", text(call), e.display(tv.Type))
			}
			out = append(out, writeStep{op: "Push", call: call, void: void})
		default:
			return fail(st, "%s isn't a write: an on… attribute's statements are writes of states and calls", text(st))
		}
	}
	return out, nil
}

// chain writes steps as one chain of commands, from start (the target's JS).
func (e *engine) chain(f *fileState, q, g string, steps []writeStep) *stateBind {
	b := &stateBind{}
	bd := &binder{e: e, f: f, b: b}
	pieces := []jsxPiece{{text: q + "JS", keep: span{-1, -1}}}
	wrap := func(before string, after ...jsxPiece) {
		pieces = append([]jsxPiece{{text: before, keep: span{-1, -1}}}, pieces...)
		pieces = append(pieces, after...)
	}
	gen := func(s string) jsxPiece { return jsxPiece{text: s, keep: span{-1, -1}} }
	keep := func(x ast.Node) jsxPiece { return jsxPiece{keep: span{f.orig(x.Pos()), f.orig(x.End())}} }
	for _, s := range steps {
		switch s.op {
		case "Set":
			pieces = append(pieces, gen(".Set("), keep(s.lhs), gen(", "), keep(s.rhs), gen(")"))
		case "Toggle", "Inc", "Dec":
			wrap(g+s.op+"(", gen(", "), keep(s.lhs), gen(")"))
		case "Push":
			pieces = append(pieces, gen(".Push("))
			switch {
			case len(s.call.Args) == 0 && !isConversion(e, s.call):
				pieces = append(pieces, keep(s.call.Fun))
			case s.void:
				pieces = append(pieces, gen("func() { "), keep(s.call), gen(" }"))
			default:
				pieces = append(pieces, gen("func() error { return "), keep(s.call), gen(" }"))
			}
			pieces = append(pieces, gen(")"))
		}
	}
	bd.b.pieces = pieces
	return b
}

func isConversion(e *engine, call *ast.CallExpr) bool {
	tv, ok := e.info.Types[call.Fun]
	return ok && tv.IsType()
}

// lowerWrites lowers what writes states, once types are known: an on…
// attribute's statements, and assignments of states in methods.
// It returns the spans of the on… attributes holding statements.
func (e *engine) lowerWrites(f *fileState) []span {
	if !f.target.has("VukaState") && (f.target != nil || !e.statesTarget()) {
		return nil
	}
	if f.writeDone == nil {
		f.writeDone = map[int]bool{}
	}
	attrSpans := e.lowerAttrWrites(f)
	e.lowerMethodWrites(f, attrSpans)
	return attrSpans
}

// statesTarget reports whether a file of the package renders with a target
// declaring VukaState: a file without JSX of its own — a .vuka file holding
// a shape's methods — writes states as the shape's file does.
func (e *engine) statesTarget() bool {
	for _, f := range e.vuka {
		if f.target.has("VukaState") {
			return true
		}
	}
	return false
}

// lowerAttrWrites lowers each on… attribute holding statements to a chain.
// It returns the attributes' spans, whose statements are theirs.
func (e *engine) lowerAttrWrites(f *fileState) []span {
	var spans []span
	for _, t := range f.jsx {
		walkNodes([]jsxNode{t.root}, func(n jsxNode) {
			el, ok := n.(*jsxElem)
			if !ok {
				return
			}
			for _, a := range el.attrs {
				if !a.stmts {
					continue
				}
				spans = append(spans, a.expr)
				if a.write != nil || f.writeDone[a.expr.start] {
					continue
				}
				var prop types.Type
				if c := el.comp; c != nil {
					// A component's prop takes the chain as a value.
					if !c.resolved {
						continue
					}
					for _, arg := range c.args {
						if arg.attr == a {
							prop = arg.typ
						}
					}
				}
				stmts := e.stmtsIn(f, a.expr)
				if len(stmts) == 0 || e.waitTypes(f, stmts) {
					continue
				}
				steps, err := e.writeSteps(f, stmts, true)
				if err != nil {
					if err.off >= 0 {
						f.writeDone[a.expr.start] = true
						e.errs.add("E0150", f.at(err.off), "%s", err.msg)
					}
					continue
				}
				pkg := stepsPackage(e, steps)
				if prop != nil && !e.takesCommands(prop, pkg) {
					f.writeDone[a.expr.start] = true
					e.errs.add("E0101", f.at(a.off), "<%s>'s %s is %s: the statements of %s={…} are browser commands, which it can't hold", el.tag, a.name, e.display(prop), a.name).
						help("declare the prop as ui.JSChain, the value an element's %s={open = !open} is, and forward it to the element: <button onClick={%s}>", a.name, exportName(a.name))
					continue
				}
				a.write = e.chain(f, e.statePkg(f, pkg), f.genQual(stateGenPath(pkg)), steps)
				a.write.start, a.write.end = a.expr.start, a.expr.end
				f.dropEdits(a.expr, a.write.pieces)
				e.progress = true
			}
		})
	}
	return spans
}

// takesCommands reports whether a value of type t can hold the chain of
// browser commands statements lower to: the states package's Commands.
func (e *engine) takesCommands(t types.Type, pkg *types.Package) bool {
	if pkg == nil {
		return true
	}
	cmds, ok := pkg.Scope().Lookup("Commands").(*types.TypeName)
	return !ok || types.AssignableTo(cmds.Type(), t)
}

// waitTypes reports whether a name in stmts has no type yet while the
// package is still changing: a bare field the template's receiver will
// qualify, a ? not lowered yet.
func (e *engine) waitTypes(f *fileState, stmts []ast.Stmt) bool {
	for _, st := range stmts {
		if e.unknownTypes(st) && (e.progress || e.pendingLowering(f)) {
			return true
		}
	}
	return false
}

// stepsPackage is the package of the states steps write.
func stepsPackage(e *engine, steps []writeStep) *types.Package {
	for _, s := range steps {
		if s.lhs != nil {
			if n, ok := types.Unalias(e.info.TypeOf(s.lhs)).(*types.Named); ok {
				return n.Obj().Pkg()
			}
		}
	}
	return nil
}

// stmtsIn is the statements an attribute's span holds: the body of the
// function its statements are written in until they are lowered.
func (e *engine) stmtsIn(f *fileState, s span) []ast.Stmt {
	var out []ast.Stmt
	ast.Inspect(f.ast, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || len(out) > 0 {
			return len(out) == 0
		}
		body := lit.Body.List
		if len(body) == 0 {
			return true
		}
		if f.orig(body[0].Pos()) >= s.start && f.orig(body[len(body)-1].End()) <= s.end {
			out = body
			return false
		}
		return true
	})
	return out
}

// lowerMethodWrites lowers a live component's method's writes of states to
// commands pushed to its page, and refuses writes anywhere else. Statements
// of an on… attribute (in attrs) are lowerAttrWrites'.
func (e *engine) lowerMethodWrites(f *fileState, attrs []span) {
	inAttr := func(off int) bool {
		for _, s := range attrs {
			if off >= s.start && off < s.end {
				return true
			}
		}
		return false
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		st, ok := n.(ast.Stmt)
		if !ok || n == nil || !f.lowerable(n.Pos()) {
			return true
		}
		var lhs ast.Expr
		switch st := st.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 || st.Tok == token.DEFINE {
				return true
			}
			if _, isState := stateOf(e.info.TypeOf(st.Rhs[0])); isState {
				return true // a state assigned, not a value written
			}
			lhs = st.Lhs[0]
		case *ast.IncDecStmt:
			lhs = st.X
		default:
			return true
		}
		if _, ok := stateOf(e.info.TypeOf(lhs)); !ok {
			return true
		}
		off := f.orig(st.Pos())
		if f.writeDone[off] || inAttr(off) {
			return false
		}
		f.writeDone[off] = true
		recv := e.liveReceiver(f, st)
		if recv == "" {
			where := "outside a live component's method"
			if h := f.html; h != nil && off >= h.start && off < h.end && !e.inFuncLit(f, st) {
				where = "while the markup renders"
			}
			e.errs.add("E0149", f.at(off), "%s is written %s: write a state in an on… attribute (onClick={%s}), or in a method of a live component, which pushes it to the page",
				string(f.src[f.orig(lhs.Pos()):f.orig(lhs.End())]), where, string(f.src[off:f.orig(st.End())]))
			return false
		}
		steps, err := e.writeSteps(f, []ast.Stmt{st}, false)
		if err != nil {
			if err.off >= 0 {
				e.errs.add("E0150", f.at(err.off), "%s", err.msg)
			} else {
				f.writeDone[off] = false // types not known yet
			}
			return false
		}
		pkg := stepsPackage(e, steps)
		var pieces []jsxPiece
		gen := func(s string) jsxPiece { return jsxPiece{text: s, keep: span{-1, -1}} }
		if s := steps[0]; s.op == "Set" {
			pieces = []jsxPiece{gen(recv + ".PushJS("), {keep: span{f.orig(s.lhs.Pos()), f.orig(s.lhs.End())}}, gen(".Set("), {keep: span{f.orig(s.rhs.Pos()), f.orig(s.rhs.End())}}, gen("))")}
		} else {
			pieces = append([]jsxPiece{gen(recv + ".PushJS(")}, e.chain(f, e.statePkg(f, pkg), f.genQual(stateGenPath(pkg)), steps).pieces...)
			pieces = append(pieces, gen(")"))
		}
		f.dropEdits(span{off, f.orig(st.End())}, pieces)
		f.fixed = append(f.fixed, pieceEdits(off, f.orig(st.End()), pieces)...)
		e.progress = true
		return false
	})
}

// liveReceiver is the receiver of the method st is in, when that method's
// type is a live component (it has PushJS); "" otherwise.
func (e *engine) liveReceiver(f *fileState, st ast.Node) string {
	for p := f.parents[st]; p != nil; p = f.parents[p] {
		fd, ok := p.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
			return ""
		}
		name := fd.Recv.List[0].Names[0]
		obj := e.info.Defs[name]
		if obj == nil || name.Name == "_" {
			return ""
		}
		t := obj.Type()
		if _, ok := t.(*types.Pointer); !ok {
			t = types.NewPointer(t)
		}
		if sel := types.NewMethodSet(t).Lookup(nil, "PushJS"); sel == nil {
			return ""
		}
		return name.Name
	}
	return ""
}

// inFuncLit reports whether n is in a function literal: a handler the
// markup passes, not code running as it renders.
func (e *engine) inFuncLit(f *fileState, n ast.Node) bool {
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		switch p.(type) {
		case *ast.FuncLit:
			return true
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// exprType is x's type and value: as checked, or checked now — an
// assignment Go refuses (to a struct state's field, f.Q = "x", until it is
// lowered) leaves its value unchecked.
func (e *engine) exprType(x ast.Expr) (types.TypeAndValue, bool) {
	if tv, ok := e.info.Types[x]; ok && !isInvalid(tv.Type) {
		return tv, true
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(e.fset, e.pkg, x.Pos(), x, info); err != nil {
		return types.TypeAndValue{}, false
	}
	tv, ok := info.Types[x]
	return tv, ok && !isInvalid(tv.Type)
}

// pendingWrites reports whether an on… attribute of the tree holds
// statements not yet lowered nor refused: the tree waits for them.
func pendingWrites(f *fileState, t *jsxTree) bool {
	pending := false
	walkNodes([]jsxNode{t.root}, func(n jsxNode) {
		if el, ok := n.(*jsxElem); ok {
			for _, a := range el.attrs {
				if a.stmts && a.write == nil && !f.writeDone[a.expr.start] {
					pending = true
				}
			}
		}
	})
	return pending
}

// dropEdits removes the edits inside sp that no piece keeps: a write drops
// part of its statement (!open, when it toggles open), and an edit made
// there — a bare name qualified — would have nowhere to go.
func (f *fileState) dropEdits(sp span, pieces []jsxPiece) {
	kept := func(e edit) bool {
		for _, p := range pieces {
			if p.keep.start >= 0 && e.start >= p.keep.start && e.end <= p.keep.end {
				return true
			}
		}
		return false
	}
	out := f.fixed[:0]
	for _, e := range f.fixed {
		if e.start >= sp.start && e.end <= sp.end && !kept(e) {
			continue
		}
		out = append(out, e)
	}
	f.fixed = out
}
