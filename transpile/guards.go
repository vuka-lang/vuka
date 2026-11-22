package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// A live struct's render keeps a block's frame while the state it reads is
// unchanged: a loop or a branch of its markup reading nothing but its
// tracked fields and the props whose values compare is written as
// ui.Guard (a loop's items) or ui.GuardNode (a branch's node), with the
// versions of those fields and the values of those props:
//
//	for _, r := range rows {…}   →   lis := ui.Guard(0x…, func() []ui.Node {
//	                                      var lis []ui.Node
//	                                      for _, r := range p.rows.Get() {…}
//	                                      return lis
//	                                  }, p.rows.Version())
//
// A live session gives the block's last frame back, without running it,
// while those are the same; VUKA_LIVE_VERIFY renders it anyway and fails a
// difference. Only what the compiler can see is guarded: a block reading a
// field that isn't tracked (a dependency, a field tagged `vuka:"-"`), a prop
// that isn't a number, string or bool, a variable from outside it, or
// calling a function or method — but a tracked field's Get, a conversion,
// a builtin — renders with the rest of the render, as does one in a loop,
// one with a component tag, and every block of a markup with a statement.

// guard is how a block is guarded: its site and the Go of its deps.
type guard struct {
	site uint64
	deps []string
}

// guardOf is the guard of a block about to be written, or nil when it is
// written as it is.
func (w *jsxWriter) guardOf(n *jsxBlock) *guard {
	f, e := w.f, w.f.eng
	h := f.html
	if !w.final || w.inLoop || w.inCtx > 0 || w.streaming || n.stmt || n.m != nil || h == nil || h.shape == nil || !h.shape.Live ||
		h.recv == "" || !f.target.has("Guard") || !f.target.has("GuardNode") || e.info == nil || len(f.tracked) == 0 {
		return nil
	}
	if _, _, _, ok := w.streamLoop(n); ok || hasStmt(w.t.root.kids) || hasComp(n) {
		return nil
	}
	stmt, fd, decl := e.blockStmt(f, n)
	if stmt == nil || fd.Name.Name != "Render" || fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return nil
	}
	recv, _ := e.info.Defs[fd.Recv.List[0].Names[0]].(*types.Var)
	st := e.shapeStruct(h.shape.Type)
	if recv == nil || st == nil {
		return nil
	}
	g := &guardScan{e: e, f: f, recv: recv, st: st, block: decl, lo: stmt.Pos(), hi: stmt.End(), deps: map[string]string{}, name: recv.Name(), funcs: map[*types.Func]bool{}}
	if !g.stmt(stmt) {
		return nil
	}
	w.guards++
	hash := fnv.New64a()
	hash.Write([]byte(f.name + "\x00" + h.shape.Type + "\x00" + strconv.Itoa(w.guards)))
	out := &guard{site: hash.Sum64()}
	for _, k := range slices.Sorted(maps.Keys(g.deps)) {
		out.deps = append(out.deps, g.deps[k])
	}
	return out
}

// hasStmt reports whether a markup has a statement: what it runs would run
// before a guarded block that came after it.
func hasStmt(ns []jsxNode) bool {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxElem:
			if hasStmt(n.kids) {
				return true
			}
		case *jsxBlock:
			if n.stmt {
				return true
			}
			for _, b := range append(n.bodies, n.cases...) {
				if hasStmt(b) {
					return true
				}
			}
		}
	}
	return false
}

// hasComp reports whether a block holds a component's tag: what its render
// reads isn't the block's.
func hasComp(n *jsxBlock) bool {
	var in func(ns []jsxNode) bool
	in = func(ns []jsxNode) bool {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.comp != nil || in(n.kids) {
					return true
				}
			case *jsxBlock:
				if hasComp(n) {
					return true
				}
			}
		}
		return false
	}
	return slices.ContainsFunc(append(n.bodies, n.cases...), in)
}

// blockStmt is a block's statement in this round's syntax tree, the
// function it is in and the variable the statement before it declares —
// the block's own, which its statement sets.
func (e *engine) blockStmt(f *fileState, n *jsxBlock) (stmt ast.Stmt, fd *ast.FuncDecl, decl types.Object) {
	at := f.cur.fromOrig(n.heads[0].start)
	for _, d := range f.ast.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || f.off(fn.Pos()) > at || f.off(fn.End()) < at {
			continue
		}
		var list []ast.Stmt
		ast.Inspect(fn.Body, func(x ast.Node) bool {
			if stmt != nil {
				return false
			}
			b, ok := x.(*ast.BlockStmt)
			if !ok {
				return x != nil
			}
			for i, s := range b.List {
				if f.off(s.Pos()) == at {
					switch s.(type) {
					case *ast.RangeStmt, *ast.ForStmt, *ast.IfStmt, *ast.SwitchStmt:
						stmt, list = s, b.List[:i]
					}
					return false
				}
			}
			return true
		})
		if stmt == nil || len(list) == 0 {
			return nil, nil, nil
		}
		ds, ok := list[len(list)-1].(*ast.DeclStmt)
		if !ok {
			return nil, nil, nil
		}
		gd, ok := ds.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
			return nil, nil, nil
		}
		vs, ok := gd.Specs[0].(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 0 {
			return nil, nil, nil
		}
		return stmt, fn, e.info.Defs[vs.Names[0]]
	}
	return nil, nil, nil
}

// shapeStruct is the struct a markup renders.
func (e *engine) shapeStruct(name string) *types.Struct {
	obj, _ := e.pkg.Scope().Lookup(name).(*types.TypeName)
	if obj == nil {
		return nil
	}
	st, _ := obj.Type().Underlying().(*types.Struct)
	return st
}

// guardScan reads a block's statement for what it reads, or the body of a
// function of the package it calls.
type guardScan struct {
	e      *engine
	f      *fileState
	recv   *types.Var    // the render's receiver, or the method's; nil in a function
	st     *types.Struct // the render's struct
	block  types.Object  // the block's variable
	lo, hi token.Pos     // the statement, or the function
	deps   map[string]string
	name   string                // the render's receiver's name, which deps are written with
	funcs  map[*types.Func]bool  // the functions read so far: whether they can be guarded
	parent map[ast.Node]ast.Node // of the statement's nodes
}

// stmt checks the statement. A func literal not called where it is runs
// later, as an event's handler: only what it captures counts.
func (g *guardScan) stmt(s ast.Stmt) bool {
	g.parent = map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(s, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			g.parent[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	ok := true
	ast.Inspect(s, func(n ast.Node) bool {
		if !ok || n == nil {
			return false
		}
		if lit, isLit := n.(*ast.FuncLit); isLit && !g.called(lit) {
			ok = g.captures(lit)
			return false
		}
		ok = g.node(n)
		return ok
	})
	return ok
}

// called reports whether a func literal is called where it is.
func (g *guardScan) called(lit *ast.FuncLit) bool {
	n := ast.Node(lit)
	for {
		p := g.parent[n]
		if pe, ok := p.(*ast.ParenExpr); ok {
			n = pe
			continue
		}
		c, ok := p.(*ast.CallExpr)
		return ok && c.Fun == n
	}
}

// node checks a node of the statement outside func literals.
func (g *guardScan) node(n ast.Node) bool {
	info := g.e.info
	switch n := n.(type) {
	case *ast.GoStmt, *ast.DeferStmt, *ast.SendStmt, *ast.SelectStmt, *ast.LabeledStmt, *ast.BranchStmt:
		return false
	case *ast.UnaryExpr:
		return n.Op != token.ARROW && n.Op != token.AND
	case *ast.RangeStmt:
		t := info.TypeOf(n.X)
		if t == nil {
			return false
		}
		switch t.Underlying().(type) {
		case *types.Signature, *types.Chan:
			return false // iterates by calling, or receiving
		}
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if !g.local(l) {
				return false
			}
		}
	case *ast.IncDecStmt:
		return g.local(n.X)
	case *ast.CallExpr:
		return g.call(n)
	case *ast.SelectorExpr:
		if g.isRecv(n.X) {
			return g.field(n)
		}
		if v, ok := info.Uses[n.Sel].(*types.Var); ok && g.own(v) {
			return false // the receiver's field, reached otherwise
		}
	case *ast.Ident:
		return g.ident(n)
	}
	return true
}

// local reports whether x, assigned to, is a variable of the statement or
// the block's own.
func (g *guardScan) local(x ast.Expr) bool {
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name == "_" {
		return true
	}
	obj := g.e.info.ObjectOf(id)
	return obj != nil && (obj == g.block || g.inside(obj))
}

func (g *guardScan) inside(obj types.Object) bool { return obj.Pos() >= g.lo && obj.Pos() < g.hi }

// isRecv reports whether x is the receiver.
func (g *guardScan) isRecv(x ast.Expr) bool {
	id, ok := ast.Unparen(x).(*ast.Ident)
	return ok && g.recv != nil && g.e.info.Uses[id] == g.recv
}

// ident checks a name the statement reads.
func (g *guardScan) ident(id *ast.Ident) bool {
	info := g.e.info
	if obj := info.Defs[id]; obj != nil {
		return true
	}
	switch obj := info.Uses[id].(type) {
	case nil, *types.PkgName, *types.TypeName, *types.Const, *types.Nil, *types.Label:
		return true
	case *types.Builtin:
		return pureBuiltins[obj.Name()]
	case *types.Func:
		return true // called: call checks it; else a func value, a handler
	case *types.Var:
		if obj == g.recv && obj != nil || obj == g.block || g.inside(obj) {
			return true
		}
		return obj.IsField() // a field of a value the statement reads (the receiver's: field)
	}
	return false
}

var pureBuiltins = map[string]bool{"len": true, "cap": true, "min": true, "max": true, "append": true, "make": true, "new": true, "complex": true, "real": true, "imag": true}

// call checks a call: a conversion, a pure builtin, a tracked field's Get,
// the target's HTML, a pure function of the standard library, a func
// literal run where it is, or a function of the package — a method of the
// receiver — whose body can be guarded itself.
func (g *guardScan) call(c *ast.CallExpr) bool {
	info := g.e.info
	fun := ast.Unparen(c.Fun)
	if tv, ok := info.Types[fun]; ok && tv.IsType() {
		return true
	}
	switch fn := fun.(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident:
		switch obj := info.Uses[fn].(type) {
		case *types.Builtin:
			return pureBuiltins[obj.Name()]
		case *types.Func:
			return g.body(obj, false)
		}
	case *ast.SelectorExpr:
		obj, _ := info.Uses[fn.Sel].(*types.Func)
		if obj == nil {
			return false
		}
		if pkg, ok := ast.Unparen(fn.X).(*ast.Ident); ok {
			if pn, ok := info.Uses[pkg].(*types.PkgName); ok {
				if pn.Imported() == g.f.target.pkg {
					return obj.Name() == "HTML"
				}
				return pureFunc(pn.Imported().Path(), obj.Name()) && !g.funcArgs(c) || g.body(obj, false)
			}
		}
		if obj.Name() == "Get" && len(c.Args) == 0 {
			if sel, ok := ast.Unparen(fn.X).(*ast.SelectorExpr); ok && g.isRecv(sel.X) {
				_, tracked := g.trackedField(sel.Sel.Name)
				return tracked
			}
		}
		return g.isRecv(fn.X) && g.body(obj, true)
	}
	return false
}

// pureFunc reports whether a function of the standard library returns what
// its arguments make: strconv's, strings', unicode's, math's, fmt.Sprint's.
func pureFunc(path, name string) bool {
	switch path {
	case "strconv", "strings", "unicode", "unicode/utf8", "math", "html":
		return name != "NewReader" && name != "NewReplacer"
	case "fmt":
		return name == "Sprint" || name == "Sprintf" || name == "Sprintln"
	}
	return false
}

// funcArgs reports whether a call passes a func, which it may call.
func (g *guardScan) funcArgs(c *ast.CallExpr) bool {
	for _, a := range c.Args {
		if t := g.e.info.TypeOf(a); t == nil || isSignature(t) {
			return true
		}
	}
	return false
}

func isSignature(t types.Type) bool {
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

// body reports whether a function of the package, a method of the receiver
// (method), reads nothing a guard can't see, adding what it reads to the
// deps.
func (g *guardScan) body(fn *types.Func, method bool) bool {
	fn = fn.Origin()
	if fn.Pkg() != g.e.pkg {
		return false
	}
	if ok, seen := g.funcs[fn]; seen {
		return ok // true while being read: what it reads is being added
	}
	g.funcs[fn] = true
	fd := g.e.funcDecl(fn)
	ok := fd != nil && fd.Body != nil && (fd.Recv != nil) == method
	if ok {
		var recv *types.Var
		if method {
			if len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
				ok = false
			} else {
				recv, _ = g.e.info.Defs[fd.Recv.List[0].Names[0]].(*types.Var)
			}
		}
		in := &guardScan{e: g.e, f: g.f, recv: recv, st: g.st, lo: fd.Pos(), hi: fd.End(), deps: g.deps, name: g.name, funcs: g.funcs}
		ok = ok && in.stmt(fd.Body)
	}
	g.funcs[fn] = ok
	return ok
}

// field checks a field or method of the receiver the statement names, and
// adds what it reads to the deps.
func (g *guardScan) field(sel *ast.SelectorExpr) bool {
	name := sel.Sel.Name
	switch obj := g.e.info.Uses[sel.Sel].(type) {
	case *types.Func:
		return true // a method value, or a call call checks
	case *types.Var:
		if !g.own(obj) {
			return false
		}
		if t, ok := g.trackedField(name); ok {
			if !hasPtrMethod(t, "Version") || !g.gotten(sel) {
				return false
			}
			g.deps[name] = g.name + "." + name + ".Version()"
			return true
		}
		if obj.Exported() && comparableProp(obj.Type()) {
			g.deps[name] = g.name + "." + name
			return true
		}
		if !g.browserState(name) {
			return false
		}
		if g.e.syncedState(g.f.html.shape.Type, name) {
			// A synced state's reference carries its server replica: the
			// block renders again when a write moved its version.
			gen := stateGenPathOf(g.e, g.f)
			if gen == "" {
				return false
			}
			g.deps[name] = g.f.genQual(gen) + "Ver(" + g.name + "." + name + ")"
		}
		return true
	}
	return false
}

// gotten reports whether sel, a tracked field, is read with Get.
func (g *guardScan) gotten(sel *ast.SelectorExpr) bool {
	get, ok := g.parent[sel].(*ast.SelectorExpr)
	if !ok || get.X != sel || get.Sel.Name != "Get" {
		return false
	}
	c, ok := g.parent[get].(*ast.CallExpr)
	return ok && c.Fun == get
}

// own reports whether a field is one of the receiver's struct's.
func (g *guardScan) own(v *types.Var) bool {
	v = v.Origin()
	for i := 0; i < g.st.NumFields(); i++ {
		if g.st.Field(i) == v {
			return !v.Embedded()
		}
	}
	return false
}

// trackedField is a tracked field's type, an Assign.
func (g *guardScan) trackedField(name string) (types.Type, bool) {
	if !g.f.tracked[name] {
		return nil, false
	}
	for i := 0; i < g.st.NumFields(); i++ {
		if v := g.st.Field(i); v.Name() == name && isAssign(v.Type()) {
			return v.Type(), true
		}
	}
	return nil, false
}

func (g *guardScan) browserState(name string) bool {
	for _, sf := range g.f.html.shape.Fields {
		if sf.Name == name {
			return sf.Role == BrowserState
		}
	}
	return false
}

func hasPtrMethod(t types.Type, name string) bool {
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, name)
	_, ok := obj.(*types.Func)
	return ok
}

// comparableProp reports whether a prop's value compares as what the block
// read: a number, a string or a bool.
func comparableProp(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) != 0 && b.Kind() != types.UnsafePointer
}

// captures checks what a func literal run later takes from around it: the
// receiver, the statement's own variables, package-level names.
func (g *guardScan) captures(lit *ast.FuncLit) bool {
	ok := true
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, isID := n.(*ast.Ident)
		if !ok || !isID {
			return ok
		}
		v, isVar := g.e.info.Uses[id].(*types.Var)
		if !isVar || v.IsField() || v == g.recv || g.inside(v) || v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			return true
		}
		ok = false
		return false
	})
	return ok
}

// guardOpen and guardClose write a guarded block's statements around its
// own, which set name: the guard declares it.
func (w *jsxWriter) guardOpen(g *guard, name, typ string) {
	w.gen(name + " := " + w.q + map[bool]string{true: "Guard", false: "GuardNode"}[strings.HasPrefix(typ, "[]")] + "(0x" + strconv.FormatUint(g.site, 16) + ", func() " + typ + " {\n")
}

func (w *jsxWriter) guardClose(g *guard, name string) {
	w.gen("return " + name + "\n}, " + strings.Join(g.deps, ", ") + ")\n")
}
