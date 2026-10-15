package transpile

import (
	"encoding/json"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

// Client state. A JSX target declaring NewState (ui does) has states the
// browser keeps: ui.State(v) declares one, and JSX reads them in plain Go
// expressions that the browser evaluates again when a state changes.
//
//	open := ui.State(false)              →  open := ui.NewState("s1x9k2", false)
//	var theme = ui.State("light")        →  var theme = ui.PageState("s4ab0c", "light")
//	<ul hidden={!open}>                  →  ui.El("ul", []ui.Attr{{Name: "hidden", Value: ui.Bind(!open.Get(), `["!",{"$":0}]`, open)}})
//	<span>{n}</span>                     →  ui.Child(ui.Bind(n.Get(), `{"$":0}`, n))
//	ready := ui.Derived(!open && ok)     →  ready := ui.NewDerived(!open.Get() && ok, `["&&",["!",{"$":0}],{"$":1}]`, open, ok)
//	f.Q.Set("x")                         →  ui.StateField(f, "Q", f.Get().Q).Set("x")
//
// A declaration's site is a hash of the package, file and its place, so the
// browser tells states apart however they are named; one in a function is
// the rendering component instance's, one at package level the page's.
// A read keeps its Go expression for the value the server renders, each
// state read as its Get(), so Go checks its types; the expression the
// browser evaluates is JSON, with the states and any server values it uses
// as deps. Only literals, state reads (a struct state's fields too), !, -,
// &&, ||, comparisons, + and -, len and ui.Class run in the browser; any
// other expression mixed with a state is an error.

// declareStates gives each ui.State(v) call its site: NewState in a
// function, PageState at package level.
func (e *engine) declareStates() {
	pkgs := map[string]bool{} // import path → declares NewState
	declares := func(path string) bool {
		if ok, seen := pkgs[path]; seen {
			return ok
		}
		ok := false
		if !stdPackage(path) {
			if p, err := e.imp.Import(path); err == nil && p.Scope().Lookup("NewState") != nil && p.Scope().Lookup("Bind") != nil {
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
		n := 0
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
			n++
			h := fnv.New32a()
			h.Write([]byte(e.pkgPath() + "/" + f.name + "#" + strconv.Itoa(n)))
			site := "s" + strconv.FormatUint(uint64(h.Sum32()), 36)
			fn := "PageState"
			if e.inFunc(f, call) {
				fn = "NewState"
			}
			f.add(f.orig(sel.Sel.Pos()), f.orig(sel.Sel.End()), fn)
			f.insert(f.orig(call.Lparen)+1, strconv.Quote(site)+", ", 0)
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
	if sc.Lookup("NewState") == nil || sc.Lookup("Bind") == nil {
		return nil, false
	}
	return n.TypeArgs().At(0), true
}

// stateBind is an expression of states lowered: the Go expression of the
// value it renders with (the source, with .Get() after each state read),
// the browser's expression, and its deps.
type stateBind struct {
	start, end int      // the expression in src
	gets       []int    // where .Get() goes, in src
	expr       string   // JSON
	deps       []string // Go
	call       string   // "Bind", "NewDerived" or "StateField"
	field      string   // StateField: the whole call
}

// bindError is a read the browser can't run.
type bindError struct {
	off int
	msg string
}

// binder lowers one expression.
type binder struct {
	e    *engine
	f    *fileState
	b    *stateBind
	err  *bindError
	deps map[string]int
}

// readsState reports whether x reads a state: a state-typed operand that
// isn't the receiver of its Get (which reads the server's value).
func (e *engine) readsState(x ast.Node) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found {
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

// lowerState lowers x, an expression reading states, as a binding.
func (e *engine) lowerState(f *fileState, x ast.Expr) (*stateBind, *bindError) {
	bd := &binder{e: e, f: f, b: &stateBind{start: f.orig(x.Pos()), end: f.orig(x.End()), call: "Bind"}, deps: map[string]int{}}
	j := bd.lower(x)
	if bd.err != nil {
		return nil, bd.err
	}
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.Encode(j)
	bd.b.expr = strings.TrimSuffix(out.String(), "\n")
	return bd.b, nil
}

func (bd *binder) fail(n ast.Node, msg string) any {
	if bd.err == nil {
		bd.err = &bindError{bd.f.orig(n.Pos()), msg}
	}
	return nil
}

func (bd *binder) text(n ast.Node) string {
	return string(bd.f.src[bd.f.orig(n.Pos()):bd.f.orig(n.End())])
}

// dep is x as a dep, {"$": i}, with path p: the same Go text is one dep.
func (bd *binder) dep(x ast.Expr, p []string) any {
	t := bd.text(x)
	i, ok := bd.deps[t]
	if !ok {
		i = len(bd.b.deps)
		bd.deps[t] = i
		bd.b.deps = append(bd.b.deps, t)
	}
	r := map[string]any{"$": i}
	if len(p) > 0 {
		r["p"] = p
	}
	return r
}

// fieldPath is x as a field of a struct state: the state and the field
// names down to x, each a field of the struct before it.
func (bd *binder) fieldPath(x ast.Expr) (ast.Expr, []string) { return bd.e.stateField(x) }

func (e *engine) stateField(x ast.Expr) (ast.Expr, []string) {
	var path []string
	for {
		x = ast.Unparen(x)
		if elem, ok := stateOf(e.info.TypeOf(x)); ok {
			for _, name := range path {
				st, _ := elem.Underlying().(*types.Struct)
				if st == nil {
					return nil, nil
				}
				var fl *types.Var
				for i := 0; i < st.NumFields(); i++ {
					if st.Field(i).Name() == name {
						fl = st.Field(i)
					}
				}
				if fl == nil {
					return nil, nil
				}
				elem = fl.Type()
			}
			return x, path
		}
		sel, ok := x.(*ast.SelectorExpr)
		if !ok || e.info.Selections[sel] != nil {
			return nil, nil
		}
		path = append([]string{sel.Sel.Name}, path...)
		x = sel.X
	}
}

// stateFieldText is a struct state's field as Go: base's StateField, one
// per field down the path.
func stateFieldText(q, base string, path []string) string {
	s := base
	for i := range path {
		s = q + "StateField(" + s + ", " + strconv.Quote(path[i]) + ", " + base + ".Get()." + strings.Join(path[:i+1], ".") + ")"
	}
	return s
}

var bindOps = map[token.Token]string{
	token.LAND: "&&", token.LOR: "||", token.EQL: "==", token.NEQ: "!=", token.LSS: "<", token.LEQ: "<=",
	token.GTR: ">", token.GEQ: ">=", token.ADD: "+", token.SUB: "-",
}

func (bd *binder) lower(x ast.Expr) any {
	e := bd.e
	if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
		return constJSON(tv.Value)
	}
	if !e.readsState(x) {
		return bd.dep(x, nil)
	}
	if base, path := bd.fieldPath(x); base != nil {
		bd.b.gets = append(bd.b.gets, bd.f.orig(base.End()))
		return bd.dep(base, path)
	}
	switch x := x.(type) {
	case *ast.ParenExpr:
		return bd.lower(x.X)
	case *ast.UnaryExpr:
		switch x.Op {
		case token.NOT:
			return []any{"!", bd.lower(x.X)}
		case token.SUB:
			return []any{"neg", bd.lower(x.X)}
		case token.ADD:
			return bd.lower(x.X)
		}
		return bd.fail(x, "the "+x.Op.String()+" operator can't run in the browser; compute it on the server")
	case *ast.BinaryExpr:
		op, ok := bindOps[x.Op]
		if !ok {
			return bd.fail(x, "the "+x.Op.String()+" operator can't run in the browser; compute it on the server")
		}
		return []any{op, bd.lower(x.X), bd.lower(x.Y)}
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && len(x.Args) == 1 {
			if b, ok := e.info.Uses[id].(*types.Builtin); ok && b.Name() == "len" {
				return []any{"len", bd.lower(x.Args[0])}
			}
		}
		return bd.fail(x, bd.text(x.Fun)+" can't run in the browser; compute it on the server")
	case *ast.CompositeLit:
		if n, ok := types.Unalias(e.info.TypeOf(x)).(*types.Named); ok && n.Obj().Name() == "Class" && n.Obj().Pkg() != nil && n.Obj().Pkg().Scope().Lookup("NewState") != nil {
			m := map[string]any{}
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				tv := e.info.Types[kv.Key]
				if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
					return bd.fail(el, "a ui.Class reading a state takes constant class names")
				}
				m[constant.StringVal(tv.Value)] = bd.lower(kv.Value)
			}
			return []any{"class", m}
		}
	}
	return bd.fail(x, bd.text(x)+" can't run in the browser; compute it on the server")
}

func constJSON(v constant.Value) any {
	switch v.Kind() {
	case constant.Bool:
		return constant.BoolVal(v)
	case constant.String:
		return constant.StringVal(v)
	case constant.Int:
		if i, ok := constant.Int64Val(v); ok {
			return i
		}
	}
	f, _ := constant.Float64Val(v)
	return f
}

// exprAt is the expression at a span of src in this round's syntax tree.
func (e *engine) exprAt(f *fileState, s span) ast.Expr {
	at, end := f.cur.fromOrig(s.start), f.cur.fromOrig(s.end)
	var expr ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok && expr == nil && f.off(x.Pos()) == at && f.off(x.End()) == end {
			expr = x
		}
		return expr == nil && (n == nil || f.off(n.Pos()) <= at)
	})
	return expr
}

// bindTree decides which of a tree's element attributes and children read
// states. It reports false while a type they need is unknown.
func (e *engine) bindTree(f *fileState, t *jsxTree) bool {
	if !f.target.has("Bind") {
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
			e.errs.add(f.at(err.off), "%s", err.msg)
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

// bindArg decides how a component's attribute reading states is passed: a
// state as it is; a struct state's field as its StateField; another
// expression as a Binding to a prop taking one (any), or as a derived state
// to a prop that is a ui.State. A prop of another type takes no state.
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
		e.errs.add(f.at(at.off), "<%s>'s key can't read a ui.State", c.el.tag)
		return
	}
	_, toState := stateOf(a.typ)
	_, iface := a.typ.Underlying().(*types.Interface)
	bd := &binder{e: e, f: f, b: &stateBind{}, deps: map[string]int{}}
	if base, path := bd.fieldPath(x); base != nil && (toState || iface) {
		at.bind = &stateBind{call: "StateField", field: stateFieldText(c.target.q, bd.text(base), path)}
		return
	}
	if !toState && !iface {
		e.errs.add(f.at(at.off), "%s reads a ui.State, and <%s>'s %s takes %s: pass .Get() for the value it renders with, or make the prop a ui.State",
			bd.text(x), c.el.tag, at.name, e.display(a.typ))
		return
	}
	b, err := e.lowerState(f, x)
	if err != nil {
		e.errs.add(f.at(err.off), "%s", err.msg)
		return
	}
	if toState {
		b.call = "NewDerived"
	}
	at.bind = b
}

// bind writes a lowered read: Bind(value, expr, deps…), the value's
// expression kept in place.
func (w *jsxWriter) bind(b *stateBind) {
	if b.call == "StateField" {
		w.gen(b.field)
		return
	}
	w.gen(w.q + b.call + "(")
	writeKept(w.keep, w.gen, b)
	w.gen(", " + goString(b.expr))
	for _, d := range b.deps {
		w.gen(", " + d)
	}
	w.gen(")")
}

func writeKept(keep func(span), gen func(string), b *stateBind) {
	gets := append([]int(nil), b.gets...)
	sort.Ints(gets)
	at := b.start
	for _, g := range gets {
		keep(span{at, g})
		gen(".Get()")
		at = g
	}
	keep(span{at, b.end})
}

// goString is s as a Go string literal, raw when it can be.
func goString(s string) string {
	if !strings.ContainsAny(s, "`\r") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

// lowerStates lowers what Go code does with states, once types are known:
// ui.Derived(expr) and a struct state's fields; and checks the writes
// Go's types can't: Toggle on a bool state, Inc and Dec on a number's, Set's
// value.
func (e *engine) lowerStates(f *fileState) {
	if f.stateDone == nil {
		f.stateDone = map[int]bool{}
	}
	// what JSX bindings lower themselves
	var skip []span
	for _, t := range f.jsx {
		if t.done {
			continue
		}
		var walk func(ns []jsxNode)
		walk = func(ns []jsxNode) {
			for _, n := range ns {
				switch n := n.(type) {
				case *jsxHole:
					skip = append(skip, n.expr)
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
							skip = append(skip, a.expr)
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
				if fn.Pkg() == nil || fn.Pkg().Scope().Lookup("NewState") == nil {
					return true
				}
				if fn.Name() == "Derived" && fn.Signature().Recv() == nil && len(n.Args) == 1 {
					f.stateDone[off] = true
					if !e.readsState(n.Args[0]) {
						return true
					}
					b, err := e.lowerState(f, n.Args[0])
					if err != nil {
						e.errs.add(f.at(err.off), "%s", err.msg)
						return false
					}
					f.add(f.orig(sel.Sel.Pos()), f.orig(sel.Sel.End()), "NewDerived")
					for _, g := range b.gets {
						f.insert(g, ".Get()", 0)
					}
					tail := ", " + goString(b.expr)
					for _, d := range b.deps {
						tail += ", " + d
					}
					f.insert(b.end, tail, 1)
					e.progress = true
					return false
				}
				e.checkStateWrite(f, n, sel, fn)
			}
		case *ast.SelectorExpr:
			if f.stateDone[off] || skipped(off) || e.info.Selections[n] != nil {
				return true
			}
			base, path := e.stateField(n)
			if base == nil || len(path) == 0 {
				return true
			}
			f.stateDone[off] = true
			pkg := types.Unalias(e.info.TypeOf(base)).(*types.Named).Obj().Pkg()
			b := string(f.src[f.orig(base.Pos()):f.orig(base.End())])
			f.add(off, f.orig(n.End()), stateFieldText(e.statePkg(f, pkg), b, path))
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
			e.errs.add(f.at(f.orig(sel.Sel.Pos())), "Toggle needs a ui.State[bool]; %s is a %s", string(f.src[f.orig(sel.X.Pos()):f.orig(sel.X.End())]), e.display(e.info.TypeOf(sel.X)))
		case fn.Name() != "Toggle" && (b == nil || b.Info()&types.IsNumeric == 0):
			e.errs.add(f.at(f.orig(sel.Sel.Pos())), "%s needs a ui.State of a number; %s is a %s", fn.Name(), string(f.src[f.orig(sel.X.Pos()):f.orig(sel.X.End())]), e.display(e.info.TypeOf(sel.X)))
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
			e.errs.add(f.at(f.orig(call.Args[1].Pos())), "Set: %s is a %s; it takes a %s, not %s",
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
