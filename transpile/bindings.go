package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// A template's statement binding names, categories := GetCategories(),
// binds them as a Go statement does: to the end of the block it is in (an
// if, for or switch body, or the whole markup). A part's bindings are their
// element's: one of a deferred method in a <ui.Suspense>, and one listed in
// a <ui.Suspense for={…}>; such a name used after the element is E0163 (a
// deferred method's binding elsewhere is its element's part: parts.go,
// E0344). A statement binding one name to one value is lazy: the value
// is computed where the render first reads the name, once, and never when
// no read renders —
//
//	categories := GetCategories()   →   categories := sync.OnceValue(func() []Category { return ui.Must(p.GetCategories(ctx)) })
//	<p>{len(categories)}</p>        →   <p>{len(categories())}</p>
//
// unless the markup writes the name, or takes its address.

// markupStmt is a statement of a template's markup, the element it is in
// (nil for the markup's top level), and the header of the block whose body
// it is in, if, for, switch or match (zero at the markup's top level).
type markupStmt struct {
	b     *jsxBlock
	el    *jsxElem
	block span
}

// markupStmts are f's markup's statements.
func (e *engine) markupStmts(f *fileState) []markupStmt {
	h := f.html
	if h == nil {
		return nil
	}
	var out []markupStmt
	var walk func(ns []jsxNode, el *jsxElem, block span)
	walk = func(ns []jsxNode, el *jsxElem, block span) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				inner := el
				if !n.frag {
					inner = n
				}
				walk(n.kids, inner, block)
			case *jsxBlock:
				if n.stmt {
					out = append(out, markupStmt{n, el, block})
					continue
				}
				for i, b := range n.bodies {
					walk(b, el, n.heads[min(i, len(n.heads)-1)])
				}
				for i, c := range n.cases {
					walk(c, el, n.heads[min(i+1, len(n.heads)-1)])
				}
			}
		}
	}
	for _, t := range f.jsx {
		if h.toSrc(t.start) >= h.markup {
			walk([]jsxNode{t.root}, nil, span{})
		}
	}
	return out
}

// stmtAssign is the assignment a statement of the markup is, in this
// round's syntax tree, or nil.
func (e *engine) stmtAssign(f *fileState, s span) *ast.AssignStmt {
	var out *ast.AssignStmt
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || f.off(as.Pos()) >= f.body {
			return true
		}
		// Its value's position: a statement's own may map to where a
		// block Vuka writes starts.
		if at := f.orig(as.Rhs[0].Pos()); at >= s.start && at < s.end && f.orig(as.Pos()) <= at {
			out = as
			return false
		}
		return true
	})
	return out
}

// usesOf are the identifiers of f's markup naming obj, declared as name:
// those naming nothing too, which Go reports undefined outside its scope.
func (e *engine) usesOf(f *fileState, obj types.Object, name string) []*ast.Ident {
	var out []*ast.Ident
	for _, id := range e.markupIdents(f) {
		if id.Name != name || e.info.Defs[id] != nil {
			continue
		}
		if o := e.info.Uses[id]; o == obj || o == nil {
			out = append(out, id)
		}
	}
	return out
}

// bindings checks the scope of f's markup's statements' names and makes
// the bindings of one value lazy. It reports whether it changed the
// source.
func (e *engine) bindings(f *fileState) bool {
	h := f.html
	if e.info == nil || h == nil || h.recvType == "" || !f.partsDone {
		return false
	}
	if f.bindDone == nil {
		f.bindDone = map[int]bool{}
	}
	changed := false
	for _, st := range e.markupStmts(f) {
		at := st.b.heads[0].start
		if f.bindDone[at] {
			continue
		}
		as := e.stmtAssign(f, st.b.heads[0])
		if as == nil || as.Tok != token.DEFINE {
			f.bindDone[at] = true
			continue
		}
		ready := true
		uses := map[*ast.Ident][]*ast.Ident{}
		for _, l := range as.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			obj := e.info.Defs[id]
			if obj == nil {
				if e.info.Uses[id] == nil {
					ready = false // not typed yet
				}
				continue // a name := assigns, declared before
			}
			uses[id] = e.usesOf(f, obj, id.Name)
		}
		if !ready {
			continue
		}
		if len(as.Rhs) == 1 && e.typeErrIn(f, as.Rhs[0]) {
			continue // decided once its value type-checks
		}
		f.bindDone[at] = true
		nerrs := len(*e.errs)
		for id, us := range uses {
			if f.susBindings[at] != nil || f.partStmts[at] {
				e.bindingScope(f, st, id, us)
			} else {
				e.blockScope(f, st, id, us)
			}
		}
		if len(*e.errs) > nerrs {
			continue
		}
		if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && e.info.Defs[id] != nil {
				changed = e.lazy(f, id, as.Rhs[0], uses[id]) || changed
			}
		}
	}
	if changed {
		e.progress = true
	}
	return changed
}

// bindingScope reports the uses of a part's binding after the element it
// is in: E0163.
func (e *engine) bindingScope(f *fileState, st markupStmt, id *ast.Ident, uses []*ast.Ident) {
	if st.el == nil {
		return
	}
	why := "a deferred method's binding in a <ui.Suspense>"
	if f.susBindings[st.b.heads[0].start] != nil {
		why = "a binding listed in <ui.Suspense for={…}>"
	}
	decl := f.orig(id.Pos())
	for _, u := range uses {
		off := f.orig(u.Pos())
		if off >= st.el.start && off < st.el.end || off < decl {
			continue
		}
		at := f.at(decl)
		err := e.errs.add("E0163", f.at(off), "%s is declared in <%s> at line %d, and used after it; %s is its element's", id.Name, st.el.tag, at.Line, why).
			help("use %s inside the <%s>, or move the statement up, into an element holding every use", id.Name, st.el.tag)
		err.Len = len(id.Name)
	}
}

// blockScope reports the uses of a statement's name after the block whose
// body it is in, which Go reports undefined: E0163.
func (e *engine) blockScope(f *fileState, st markupStmt, id *ast.Ident, uses []*ast.Ident) {
	if st.block.end <= st.block.start || e.pkg == nil {
		return
	}
	if e.pkg.Scope().Lookup(id.Name) != nil || types.Universe.Lookup(id.Name) != nil {
		return
	}
	if obj, ok := e.pkg.Scope().Lookup(f.html.recvType).(*types.TypeName); ok {
		if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, e.pkg, id.Name); o != nil {
			return // the struct's, once qualified
		}
	}
	decl := f.orig(id.Pos())
	kw, _, _ := strings.Cut(strings.TrimSpace(string(f.src[st.block.start:st.block.end])), " ")
	for _, u := range uses {
		off := f.orig(u.Pos())
		if off < decl || e.info.Uses[u] != nil {
			continue
		}
		at := f.at(decl)
		err := e.errs.add("E0163", f.at(off), "%s is declared in the %s's body at line %d, and used after it", id.Name, kw, at.Line).
			help("a statement's names are its block's, as in Go: move the statement up, before the %s", kw)
		err.Len = len(id.Name)
	}
}

// lazy makes id := rhs compute rhs at the first read of id, reporting
// whether it did: when rhs calls a function, and every use of id reads it.
func (e *engine) lazy(f *fileState, id *ast.Ident, rhs ast.Expr, uses []*ast.Ident) bool {
	tv, ok := e.info.Types[rhs]
	if !ok || tv.Type == nil || isInvalid(tv.Type) || tv.IsType() {
		return false
	}
	if _, tuple := tv.Type.(*types.Tuple); tuple || tv.IsVoid() || !e.works(rhs) || e.typeErrIn(f, rhs) {
		return false
	}
	for _, u := range uses {
		if e.info.Uses[u] == nil || written(f, u) {
			return false
		}
	}
	start, end := f.orig(rhs.Pos()), f.orig(rhs.End())
	if end <= start || end > len(f.src) {
		return false
	}
	t := e.typeTextAuto(f, tv.Type)
	sync := f.syncName()
	f.insert(start, sync+".OnceValue(func() "+t+" { return ", -3)
	// After the value's last byte, inside its span: a copy of the
	// statement takes it.
	closing := string(f.src[end-1:end]) + " })"
	if len(uses) == 0 {
		closing += "; _ = " + id.Name
	}
	f.add(end-1, end, closing)
	for _, u := range uses {
		ue := f.orig(u.End())
		f.add(ue-1, ue, string(f.src[ue-1:ue])+"()")
	}
	f.lazies = append(f.lazies, f.orig(id.Pos()))
	return true
}

// typeErrIn reports whether this round's type-check failed inside n: what
// is in it isn't decided yet (a call short of its ctx, a bare name).
func (e *engine) typeErrIn(f *fileState, n ast.Node) bool {
	for _, te := range e.typeErrs {
		if e.fileOf(te.Pos) == f && te.Pos >= n.Pos() && te.Pos <= n.End() {
			return true
		}
	}
	return false
}

// works reports whether x calls a function or method of the app's or a
// library's: not only builtins, conversions and the standard library's.
func (e *engine) works(x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if e.callSig(n) != nil && !e.stdCall(n) {
				found = true
			}
		}
		return true
	})
	return found
}

// stdCall reports whether c calls a function or method of the standard
// library.
func (e *engine) stdCall(c *ast.CallExpr) bool {
	var obj types.Object
	switch fun := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		obj = e.info.Uses[fun]
	case *ast.SelectorExpr:
		if s := e.info.Selections[fun]; s != nil {
			obj = s.Obj()
		} else {
			obj = e.info.Uses[fun.Sel]
		}
	case *ast.IndexExpr, *ast.IndexListExpr:
		return false
	}
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	first, _, _ := strings.Cut(obj.Pkg().Path(), "/")
	return !strings.Contains(first, ".") && obj.Pkg() != e.pkg && obj.Pkg().Path() != "main"
}

// written reports whether id is written or has its address taken, which
// a lazy binding can't be.
func written(f *fileState, id *ast.Ident) bool {
	switch p := f.parents[id].(type) {
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == id {
				return true
			}
		}
	case *ast.IncDecStmt:
		return true
	case *ast.UnaryExpr:
		return p.Op == token.AND
	case *ast.RangeStmt:
		return p.Key == id || p.Value == id
	}
	return false
}

// syncName is the name f imports sync as, importing it on first use.
func (f *fileState) syncName() string {
	for _, imp := range f.ast.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "sync" {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return "sync"
		}
	}
	return f.importAs("sync", "sync")
}
