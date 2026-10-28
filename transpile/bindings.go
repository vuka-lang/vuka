package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// A template's statement binding names, categories := GetCategories(),
// binds them for the rest of its element: a name used after the element
// is E0163. A statement binding one name to one value is lazy: the value
// is computed where the render first reads the name, once, and never when
// no read renders —
//
//	categories := GetCategories()   →   categories := sync.OnceValue(func() []Category { return ui.Must(p.GetCategories(ctx)) })
//	<p>{len(categories)}</p>        →   <p>{len(categories())}</p>
//
// unless the markup writes the name, or takes its address.

// markupStmt is a statement of a template's markup, and the element it
// is in: nil for the markup's top level.
type markupStmt struct {
	b  *jsxBlock
	el *jsxElem
}

// markupStmts are f's markup's statements.
func (e *engine) markupStmts(f *fileState) []markupStmt {
	h := f.html
	if h == nil {
		return nil
	}
	var out []markupStmt
	var walk func(ns []jsxNode, el *jsxElem)
	walk = func(ns []jsxNode, el *jsxElem) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				inner := el
				if !n.frag {
					inner = n
				}
				walk(n.kids, inner)
			case *jsxBlock:
				if n.stmt {
					out = append(out, markupStmt{n, el})
					continue
				}
				for _, b := range n.bodies {
					walk(b, el)
				}
				for _, c := range n.cases {
					walk(c, el)
				}
			}
		}
	}
	for _, t := range f.jsx {
		if h.toSrc(t.start) >= h.markup {
			walk([]jsxNode{t.root}, nil)
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
		if at := f.orig(as.Pos()); at == s.start {
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
			e.bindingScope(f, st, id, us)
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

// bindingScope reports the uses of a statement's name after the element
// it is in: E0163.
func (e *engine) bindingScope(f *fileState, st markupStmt, id *ast.Ident, uses []*ast.Ident) {
	if st.el == nil {
		return
	}
	decl := f.orig(id.Pos())
	for _, u := range uses {
		off := f.orig(u.Pos())
		if off >= st.el.start && off < st.el.end || off < decl {
			continue
		}
		at := f.at(decl)
		err := e.errs.add("E0163", f.at(off), "%s is declared in <%s> at line %d, and used after it", id.Name, st.el.tag, at.Line).
			help("a statement's names are its element's: move the statement up, into an element holding both")
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
