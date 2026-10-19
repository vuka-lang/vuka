package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Client state. A JSX target declaring UseState (ui does) has states the
// browser keeps: ui.State(v) declares one, and JSX reads them in plain Go
// expressions that the browser evaluates again when a state changes. They
// lower to what a person writes in Go: a state read as the state, an
// operator as State's method on its left operand.
//
//	open := ui.State(false)              →  open := ui.UseState(false)
//	var theme = ui.State("light")        →  var theme = ui.UsePageState("light")
//	<ul hidden={!open}>                  →  <ul hidden="{{0}}">, open.Not()
//	<span>{n}</span>                     →  <span>{{0}}</span>, n
//	ready := ui.Derived(!open && ok)     →  ready := open.Not().And(ok)
//	"x" + name                           →  ui.Derived("x").Plus(name)
//	f.Q.Set("x")                         →  ui.At[string](f, "Q").Set("x")
//	ui.Class{"on": open}                 →  ui.Classes{"on": open}
//
// A state's place is where UseState is called, so the browser tells states
// apart however they are named; one in a function is the rendering
// component instance's, one at package level the page's. Only literals,
// state reads (a struct state's fields too), !, -, &&, ||, comparisons, +
// and -, len and ui.Class run in the browser; any other expression mixed
// with a state is an error.

// declareStates writes each ui.State(v) call as UseState in a function,
// UsePageState at package level.
func (e *engine) declareStates() {
	pkgs := map[string]bool{} // import path → declares UseState
	declares := func(path string) bool {
		if ok, seen := pkgs[path]; seen {
			return ok
		}
		ok := false
		if !stdPackage(path) {
			if p, err := e.imp.Import(path); err == nil && p.Scope().Lookup("UseState") != nil {
				ok = true
			}
		}
		pkgs[path] = ok
		return ok
	}
	for _, f := range e.vuka {
		names := map[string]string{} // the file's import names → paths
		for _, imp := range f.ast.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			names[name] = path
		}
		ast.Inspect(f.ast, func(nd ast.Node) bool {
			call, ok := nd.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			fun := call.Fun
			if ix, ok := fun.(*ast.IndexExpr); ok {
				fun = ix.X
			}
			sel, ok := fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "State" || !f.lowerable(sel.Pos()) {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Obj != nil || names[x.Name] == "" || !declares(names[x.Name]) {
				return true
			}
			fn := "UsePageState"
			if e.inFunc(f, call) {
				fn = "UseState"
			}
			f.add(f.orig(sel.Sel.Pos()), f.orig(sel.Sel.End()), fn)
			e.progress = true
			return true
		})
	}
}

// inFunc reports whether n is in a function's body.
func (e *engine) inFunc(f *fileState, n ast.Node) bool {
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		switch p.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return true
		}
	}
	return false
}

// stateOf is the type argument of a ui.State, a state's value type.
func stateOf(t types.Type) (types.Type, bool) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Name() != "State" || n.Obj().Pkg() == nil || n.TypeArgs().Len() != 1 {
		return nil, false
	}
	sc := n.Obj().Pkg().Scope()
	if sc.Lookup("UseState") == nil {
		return nil, false
	}
	return n.TypeArgs().At(0), true
}

// stateBind is an expression of states lowered: the source's operands kept
// in place, its operators State's methods (state.go's header).
type stateBind struct {
	start, end int
	pieces     []jsxPiece
}

// bindError is a read the browser can't run.
type bindError struct {
	off int
	msg string
}

// binder lowers one expression.
type binder struct {
	e   *engine
	f   *fileState
	q   string // how the file names the target: "ui."
	b   *stateBind
	err *bindError
}

func (bd *binder) gen(s string) {
	bd.b.pieces = append(bd.b.pieces, jsxPiece{text: s, keep: span{-1, -1}})
}

func (bd *binder) keep(x ast.Node) {
	bd.b.pieces = append(bd.b.pieces, jsxPiece{keep: span{bd.f.orig(x.Pos()), bd.f.orig(x.End())}})
}

// readsState reports whether x reads a state: a state-typed operand that
// isn't the receiver of its Get (which reads the server's value), outside
// what only hands states to Go code (see passesStates).
func (e *engine) readsState(x ast.Node) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found {
			return false
		}
		if x, ok := n.(ast.Expr); ok && e.passesStates(x) {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Get" && len(c.Args) == 0 {
				if _, st := stateOf(e.info.TypeOf(s.X)); st {
					return false
				}
			}
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if x, ok := n.(ast.Expr); ok {
			if _, st := stateOf(e.info.TypeOf(x)); st {
				found = true
			}
		}
		return !found
	})
	return found
}

// passesStates reports whether x hands the states it mentions to Go code
// rather than reading them: a call or composite literal whose value isn't a
// scalar, a state or a ui.Class — a command (open.Toggle()), a node
// (p.menu(upload)), an attribute list. The browser has nothing to evaluate
// there; the Go code gets the states as they are.
func (e *engine) passesStates(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			if _, ok := e.info.Uses[id].(*types.Builtin); ok {
				return false
			}
		}
	case *ast.CompositeLit:
	default:
		return false
	}
	t := e.info.TypeOf(x)
	if t == nil || isInvalid(t) {
		return false
	}
	if _, st := stateOf(t); st {
		return false
	}
	if _, ok := t.Underlying().(*types.Basic); ok {
		return false
	}
	return !isClass(t)
}

// isClass reports whether t is a JSX target's ui.Class.
func isClass(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == "Class" && n.Obj().Pkg() != nil && n.Obj().Pkg().Scope().Lookup("UseState") != nil
}

// unknownTypes reports whether a name in x has no type yet.
func (e *engine) unknownTypes(x ast.Node) bool {
	unknown := false
	ast.Inspect(x, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			if obj := e.info.Uses[id]; obj == nil && e.info.Defs[id] == nil || obj != nil && isInvalid(obj.Type()) {
				unknown = true
			}
		}
		return !unknown
	})
	return unknown
}

// lowerState lowers x, an expression reading states, to State's methods.
func (e *engine) lowerState(f *fileState, x ast.Expr) (*stateBind, *bindError) {
	bd := &binder{e: e, f: f, q: e.statePkg(f, e.statePackage(x)), b: &stateBind{start: f.orig(x.Pos()), end: f.orig(x.End())}}
	bd.lower(x)
	if bd.err != nil {
		return nil, bd.err
	}
	return bd.b, nil
}

// statePackage is the package declaring the states x reads.
func (e *engine) statePackage(x ast.Expr) *types.Package {
	var pkg *types.Package
	ast.Inspect(x, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok && pkg == nil {
			if _, st := stateOf(e.info.TypeOf(x)); st {
				pkg = types.Unalias(e.info.TypeOf(x)).(*types.Named).Obj().Pkg()
			}
		}
		return pkg == nil
	})
	return pkg
}

func (bd *binder) fail(n ast.Node, msg string) {
	if bd.err == nil {
		bd.err = &bindError{bd.f.orig(n.Pos()), msg}
	}
}

func (bd *binder) text(n ast.Node) string {
	return string(bd.f.src[bd.f.orig(n.Pos()):bd.f.orig(n.End())])
}

// stateField is x as a field of a struct state: the state, the field names
// down to x, and the field's type.
func (e *engine) stateField(x ast.Expr) (ast.Expr, []string, types.Type) {
	var path []string
	for {
		x = ast.Unparen(x)
		if elem, ok := stateOf(e.info.TypeOf(x)); ok {
			for _, name := range path {
				st, _ := elem.Underlying().(*types.Struct)
				if st == nil {
					return nil, nil, nil
				}
				var fl *types.Var
				for i := 0; i < st.NumFields(); i++ {
					if st.Field(i).Name() == name {
						fl = st.Field(i)
					}
				}
				if fl == nil {
					return nil, nil, nil
				}
				elem = fl.Type()
			}
			return x, path, elem
		}
		sel, ok := x.(*ast.SelectorExpr)
		if !ok || e.info.Selections[sel] != nil {
			return nil, nil, nil
		}
		path = append([]string{sel.Sel.Name}, path...)
		x = sel.X
	}
}

// field writes a struct state's field: ui.At[T](f, "Range.Hi").
func (bd *binder) field(base ast.Expr, path []string, t types.Type) {
	bd.gen(bd.q + "At[" + bd.e.typeTextAuto(bd.f, t) + "](")
	bd.keep(base)
	bd.gen(", " + strconv.Quote(strings.Join(path, ".")) + ")")
}

var bindMethods = map[token.Token]string{
	token.LAND: "And", token.LOR: "Or", token.EQL: "Eq", token.NEQ: "Ne", token.LSS: "Lt", token.LEQ: "Le",
	token.GTR: "Gt", token.GEQ: "Ge", token.ADD: "Plus", token.SUB: "Minus",
}

// lower writes x, which reads a state, as a state: one read as it is, an
// operator as State's method on its left operand (ui.Derived of a value
// that isn't a state), len as Len, a ui.Class as ui.Classes.
func (bd *binder) lower(x ast.Expr) {
	e := bd.e
	if base, path, t := e.stateField(x); base != nil {
		if len(path) == 0 {
			bd.keep(x)
		} else {
			bd.field(base, path, t)
		}
		return
	}
	switch x := x.(type) {
	case *ast.ParenExpr:
		bd.lower(x.X)
		return
	case *ast.UnaryExpr:
		switch x.Op {
		case token.NOT:
			bd.lower(x.X)
			bd.gen(".Not()")
			return
		case token.SUB:
			bd.lower(x.X)
			bd.gen(".Neg()")
			return
		case token.ADD:
			bd.lower(x.X)
			return
		}
		bd.fail(x, "the "+x.Op.String()+" operator can't run in the browser; compute it on the server")
		return
	case *ast.BinaryExpr:
		m, ok := bindMethods[x.Op]
		if !ok {
			bd.fail(x, "the "+x.Op.String()+" operator can't run in the browser; compute it on the server")
			return
		}
		if e.readsState(x.X) {
			bd.lower(x.X)
		} else {
			bd.gen(bd.q + "Derived(")
			bd.keep(x.X)
			bd.gen(")")
		}
		bd.gen("." + m + "(")
		bd.operand(x.Y)
		bd.gen(")")
		return
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && len(x.Args) == 1 {
			if b, ok := e.info.Uses[id].(*types.Builtin); ok && b.Name() == "len" {
				bd.lower(x.Args[0])
				bd.gen(".Len()")
				return
			}
		}
		bd.fail(x, bd.text(x.Fun)+" can't run in the browser; compute it on the server")
		return
	case *ast.CompositeLit:
		if isClass(e.info.TypeOf(x)) {
			bd.gen(bd.q + "Classes{")
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if tv := e.info.Types[kv.Key]; !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
					bd.fail(el, "a ui.Class reading a state takes constant class names")
					return
				}
				bd.keep(kv.Key)
				bd.gen(": ")
				bd.operand(kv.Value)
				bd.gen(", ")
			}
			bd.gen("}")
			return
		}
	}
	bd.fail(x, bd.text(x)+" can't run in the browser; compute it on the server")
}

// operand writes an operator's operand: a state lowered, a value as it is.
func (bd *binder) operand(x ast.Expr) {
	if bd.e.readsState(x) {
		bd.lower(x)
		return
	}
	bd.keep(x)
}

// exprAt is the expression at a span of src in this round's syntax tree.
func (e *engine) exprAt(f *fileState, s span) ast.Expr {
	if f.exprIdxOf != f.ast || f.exprIdx == nil {
		f.exprIdx, f.exprIdxOf = map[[2]int]ast.Expr{}, f.ast
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if x, ok := n.(ast.Expr); ok {
				k := [2]int{f.off(x.Pos()), f.off(x.End())}
				if _, seen := f.exprIdx[k]; !seen {
					f.exprIdx[k] = x // the outermost of a span
				}
			}
			return true
		})
	}
	return f.exprIdx[[2]int{f.cur.fromOrig(s.start), f.cur.fromOrig(s.end)}]
}

// bindTree decides which of a tree's element attributes and children read
// states. It reports false while a type they need is unknown.
func (e *engine) bindTree(f *fileState, t *jsxTree) bool {
	if !f.target.has("UseState") {
		return true
	}
	ok := true
	var walk func(ns []jsxNode)
	try := func(sp span) *stateBind {
		x := e.exprAt(f, sp)
		if x == nil || !e.readsState(x) {
			if x != nil && e.unknownTypes(x) && (e.progress || e.pendingLowering(f)) {
				ok = false
			}
			return nil
		}
		b, err := e.lowerState(f, x)
		if err != nil {
			e.errs.add("E0124", f.at(err.off), "%s", err.msg)
			return nil
		}
		return b
	}
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxHole:
				if n.bind == nil {
					n.bind = try(n.expr)
				}
			case *jsxBlock:
				for _, b := range n.bodies {
					walk(b)
				}
				for _, c := range n.cases {
					walk(c)
				}
			case *jsxElem:
				if n.comp == nil && !n.frag {
					for _, a := range n.attrs {
						if a.kind == 'e' && !a.event && a.bind == nil {
							a.bind = try(a.expr)
						}
					}
				}
				walk(n.kids)
			}
		}
	}
	walk([]jsxNode{t.root})
	return ok
}

// pendingLowering reports whether f has a ? or match not lowered yet, which
// may leave names untyped this round.
func (e *engine) pendingLowering(f *fileState) bool {
	for _, t := range f.tries {
		if !t.done && !t.dead {
			return true
		}
	}
	for _, m := range f.matches {
		if !m.done && !m.dead {
			return true
		}
	}
	return false
}

// bindArg lowers a component's attribute reading states, for a prop that
// is a ui.State or takes any value: a state as it is, an expression of
// states as a derived one. A prop of another type takes no state.
func (e *engine) bindArg(f *fileState, c *jsxComp, a *compArg) {
	at := a.attr
	if at == nil || at.kind != 'e' || at.bind != nil {
		return
	}
	x := e.exprAt(f, at.expr)
	if x == nil || !e.readsState(x) {
		return
	}
	if _, st := stateOf(e.info.TypeOf(x)); st {
		return
	}
	if at.name == "key" {
		e.errs.add("E0125", f.at(at.off), "<%s>'s key can't read a ui.State", c.el.tag)
		return
	}
	_, toState := stateOf(a.typ)
	_, iface := a.typ.Underlying().(*types.Interface)
	if !toState && !iface {
		e.errs.add("E0125", f.at(at.off), "%s reads a ui.State, and <%s>'s %s takes %s: pass .Get() for the value it renders with, or make the prop a ui.State",
			f.src[f.orig(x.Pos()):f.orig(x.End())], c.el.tag, at.name, e.display(a.typ))
		return
	}
	b, err := e.lowerState(f, x)
	if err != nil {
		e.errs.add("E0124", f.at(err.off), "%s", err.msg)
		return
	}
	at.bind = b
}

// bind writes a lowered read.
func (w *jsxWriter) bind(b *stateBind) {
	w.put(b.pieces)
}

// pieceEdits are the edits turning the source's span into pieces.
func pieceEdits(start, end int, pieces []jsxPiece) edits {
	var out edits
	var text strings.Builder
	cur := start
	for _, p := range pieces {
		if p.keep.start < 0 {
			text.WriteString(p.text)
			continue
		}
		if p.keep.start > cur || text.Len() > 0 {
			out = append(out, edit{start: cur, end: p.keep.start, text: text.String()})
		}
		text.Reset()
		cur = p.keep.end
	}
	if end > cur || text.Len() > 0 {
		out = append(out, edit{start: cur, end: end, text: text.String()})
	}
	return out
}

// lowerStates lowers what Go code does with states, once types are known:
// ui.Derived(expr) and a struct state's fields; and checks the writes
// Go's types can't: Toggle on a bool state, Inc and Dec on a number's, Set's
// value.
func (e *engine) lowerStates(f *fileState) {
	if f.stateDone == nil {
		f.stateDone = map[int]bool{}
	}
	// what JSX bindings lower themselves: the expressions reading states (a
	// struct state's field in one handed to Go code is lowered here)
	var skip []span
	reads := func(sp span) {
		if x := e.exprAt(f, sp); x == nil || e.readsState(x) {
			skip = append(skip, sp)
		}
	}
	for _, t := range f.jsx {
		if t.done {
			continue
		}
		var walk func(ns []jsxNode)
		walk = func(ns []jsxNode) {
			for _, n := range ns {
				switch n := n.(type) {
				case *jsxHole:
					reads(n.expr)
				case *jsxBlock:
					for _, b := range n.bodies {
						walk(b)
					}
					for _, c := range n.cases {
						walk(c)
					}
				case *jsxElem:
					for _, a := range n.attrs {
						if a.kind == 'e' && !a.event {
							reads(a.expr)
						}
					}
					walk(n.kids)
				}
			}
		}
		walk([]jsxNode{t.root})
	}
	skipped := func(off int) bool {
		for _, s := range skip {
			if off >= s.start && off < s.end {
				return true
			}
		}
		return false
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || !f.lowerable(n.Pos()) {
			return true
		}
		off := f.orig(n.Pos())
		switch n := n.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				if ix, ok := n.Fun.(*ast.IndexExpr); ok {
					sel, _ = ix.X.(*ast.SelectorExpr)
				}
			}
			if sel == nil || f.stateDone[off] {
				return true
			}
			switch fn := e.info.Uses[sel.Sel].(type) {
			case *types.Func:
				if fn.Pkg() == nil || fn.Pkg().Scope().Lookup("UseState") == nil {
					return true
				}
				if fn.Name() == "Derived" && fn.Signature().Recv() == nil && len(n.Args) == 1 {
					f.stateDone[off] = true
					if !e.readsState(n.Args[0]) {
						return true
					}
					b, err := e.lowerState(f, n.Args[0])
					if err != nil {
						e.errs.add("E0124", f.at(err.off), "%s", err.msg)
						return false
					}
					f.fixed = append(f.fixed, pieceEdits(off, f.orig(n.End()), b.pieces)...)
					e.progress = true
					return false
				}
				e.checkStateWrite(f, n, sel, fn)
			}
		case *ast.SelectorExpr:
			if f.stateDone[off] || skipped(off) || e.info.Selections[n] != nil {
				return true
			}
			base, path, t := e.stateField(n)
			if base == nil || len(path) == 0 {
				return true
			}
			f.stateDone[off] = true
			bd := &binder{e: e, f: f, q: e.statePkg(f, e.statePackage(base)), b: &stateBind{}}
			bd.field(base, path, t)
			f.fixed = append(f.fixed, pieceEdits(off, f.orig(n.End()), bd.b.pieces)...)
			e.progress = true
			return false
		}
		return true
	})
}

// statePkg is how f names pkg in generated code: "ui." or the like.
func (e *engine) statePkg(f *fileState, pkg *types.Package) string {
	missing := ""
	q := e.qualifier(f, &missing)(pkg)
	if missing != "" {
		q = f.autoImport(pkg)
	}
	if q == "" {
		return ""
	}
	return q + "."
}

// checkStateWrite checks a write's types: Toggle needs a bool state, Inc
// and Dec a number's, Set (the chain's) a value of the state's type.
func (e *engine) checkStateWrite(f *fileState, call *ast.CallExpr, sel *ast.SelectorExpr, fn *types.Func) {
	recv := fn.Signature().Recv()
	if recv == nil {
		return
	}
	off := f.orig(call.Pos())
	switch fn.Name() {
	case "Toggle", "Inc", "Dec":
		elem, ok := stateOf(e.info.TypeOf(sel.X))
		if !ok || len(call.Args) != 0 {
			return
		}
		f.stateDone[off] = true
		b, _ := elem.Underlying().(*types.Basic)
		switch {
		case fn.Name() == "Toggle" && (b == nil || b.Info()&types.IsBoolean == 0):
			e.errs.add("E0125", f.at(f.orig(sel.Sel.Pos())), "Toggle needs a ui.State[bool]; %s is a %s", string(f.src[f.orig(sel.X.Pos()):f.orig(sel.X.End())]), e.display(e.info.TypeOf(sel.X)))
		case fn.Name() != "Toggle" && (b == nil || b.Info()&types.IsNumeric == 0):
			e.errs.add("E0125", f.at(f.orig(sel.Sel.Pos())), "%s needs a ui.State of a number; %s is a %s", fn.Name(), string(f.src[f.orig(sel.X.Pos()):f.orig(sel.X.End())]), e.display(e.info.TypeOf(sel.X)))
		}
	case "Set":
		if _, isState := stateOf(recv.Type()); isState || len(call.Args) != 2 {
			return
		}
		elem, ok := stateOf(e.info.TypeOf(call.Args[0]))
		tv, known := e.info.Types[call.Args[1]]
		if !ok || !known || isInvalid(tv.Type) {
			return
		}
		f.stateDone[off] = true
		fits := types.AssignableTo(tv.Type, elem)
		if tv.Value != nil {
			fits = fits || constFits(tv.Value, elem)
		}
		if !fits {
			e.errs.add("E0125", f.at(f.orig(call.Args[1].Pos())), "Set: %s is a %s; it takes a %s, not %s",
				string(f.src[f.orig(call.Args[0].Pos()):f.orig(call.Args[0].End())]), e.display(e.info.TypeOf(call.Args[0])), e.display(elem), e.display(tv.Type))
		}
	}
}

func constFits(v constant.Value, t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch {
	case b.Info()&types.IsBoolean != 0:
		return v.Kind() == constant.Bool
	case b.Info()&types.IsString != 0:
		return v.Kind() == constant.String
	case b.Info()&types.IsInteger != 0:
		_, exact := constant.Int64Val(constant.ToInt(v))
		return v.Kind() == constant.Int || v.Kind() == constant.Float && exact
	case b.Info()&types.IsNumeric != 0:
		return v.Kind() == constant.Int || v.Kind() == constant.Float
	}
	return false
}

// hasBlocks reports whether a tree has a block: a loop's is ui.Each once
// its type is known.
func (t *jsxTree) hasBlocks() bool {
	found := false
	walkNodes([]jsxNode{t.root}, func(n jsxNode) {
		if _, ok := n.(*jsxBlock); ok {
			found = true
		}
	})
	return found
}

// walkNodes calls fn for each of ns and what they hold.
func walkNodes(ns []jsxNode, fn func(jsxNode)) {
	for _, n := range ns {
		fn(n)
		switch n := n.(type) {
		case *jsxBlock:
			for _, b := range n.bodies {
				walkNodes(b, fn)
			}
			for _, c := range n.cases {
				walkNodes(c, fn)
			}
		case *jsxElem:
			walkNodes(n.kids, fn)
		}
	}
}

// mayBind reports whether a tree has expressions an element's attribute or
// a child holds, which may read states: it waits for their types.
func (t *jsxTree) mayBind() bool {
	var any func(ns []jsxNode) bool
	any = func(ns []jsxNode) bool {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxHole:
				return true
			case *jsxBlock:
				for _, b := range n.bodies {
					if any(b) {
						return true
					}
				}
				for _, c := range n.cases {
					if any(c) {
						return true
					}
				}
			case *jsxElem:
				if n.comp == nil {
					for _, a := range n.attrs {
						if a.kind == 'e' && !a.event {
							return true
						}
					}
				}
				if any(n.kids) {
					return true
				}
			}
		}
		return false
	}
	return any([]jsxNode{t.root})
}

// holeValue writes a child's expression, lowered when it reads states.
func (w *jsxWriter) holeValue(h *jsxHole) {
	if h.bind != nil {
		w.bind(h.bind)
		return
	}
	w.keep(h.expr)
}
