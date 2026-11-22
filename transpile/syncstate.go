package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// One state (docs/rfc/components-seamless.md §12). A ui.State is one logical
// value with two replicas, the server's and the browser's; what the code
// does classifies each state, with no annotation:
//
//  1. never read by Go code (rendered and written only in the browser):
//     browser-local — its writes are never sent;
//  2. read or written by any Go code — a method, a hook, a handler func —
//     or passed where the compiler can't follow: synced — the browser's
//     dirty value rides every event (and every HTTP push on a page that
//     isn't live), applied before the handler runs, and a server write
//     (lowered to uigen.SrvSet) rides the next render, versioned;
//  3. a plain unexported field stays a tracked server value (uigen.Assign);
//  4. derived values are expressions.
//
// A Go read of a state lowers to .Get(), which under the merge is the
// server replica — browser truth as of the event — not the initial value.
// The classification is written into the generated code as
// uigen.SyncFields[T](fields…), each field with why it is synced, so
// `vuka explain` shows it; the LSP says the same on hover.

// recordSynced notes that typ's state field is synced, and why. The first
// reason sticks: it names the first Go use the compiler found.
func (e *engine) recordSynced(typ, field, why string) {
	if e.syncRecs == nil {
		e.syncRecs = map[string]map[string]string{}
	}
	if e.syncRecs[typ] == nil {
		e.syncRecs[typ] = map[string]string{}
	}
	if e.syncRecs[typ][field] == "" {
		e.syncRecs[typ][field] = why
	}
}

// syncedState reports whether typ's state field is classified synced so far.
func (e *engine) syncedState(typ, field string) bool {
	return e.syncRecs[typ] != nil && e.syncRecs[typ][field] != ""
}

// stateShape is a shape struct's State field, as classification sees it.
type stateShape struct {
	typ   string // the shape struct's name
	field string
}

// shapeStateVars maps each shape struct's ui.State fields' objects to their
// names, package-wide: what a Go use of one classifies.
func (e *engine) shapeStateVars() map[*types.Var]stateShape {
	if e.pkg == nil {
		return nil
	}
	var out map[*types.Var]stateShape
	for _, f := range e.vuka {
		if f.html == nil || f.html.shape == nil {
			continue
		}
		obj, _ := e.pkg.Scope().Lookup(f.html.shape.Type).(*types.TypeName)
		if obj == nil {
			continue
		}
		st, _ := obj.Type().Underlying().(*types.Struct)
		if st == nil {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			v := st.Field(i)
			if v.Embedded() || v.Exported() {
				continue
			}
			if _, ok := stateOf(v.Type()); !ok {
				continue
			}
			if _, d := derivedOf(v.Type()); d {
				continue
			}
			if out == nil {
				out = map[*types.Var]stateShape{}
			}
			out[v] = stateShape{typ: f.html.shape.Type, field: v.Name()}
		}
	}
	return out
}

// goUse reports whether a state use at off is Go code's rather than the
// markup's own expressions: outside the markup, or inside a func literal
// there (a handler), and outside an on… attribute's lowered statements.
func (e *engine) goUse(f *fileState, n ast.Node, attrs []span) bool {
	off := f.orig(n.Pos())
	for _, s := range attrs {
		if off >= s.start && off < s.end {
			return false
		}
	}
	if f.inFieldAttr(off, off) || f.inStaticInit(off, off) {
		return false // @ui.From's expression, a field attribute's value
	}
	if h := f.html; h != nil && off >= h.start && off < h.end {
		return e.inFuncLit(f, n)
	}
	return true
}

// useSite names where a use is, for the classification's why: the enclosing
// function — "(b *Board).Reset", "Mount" — else the file and line.
func (e *engine) useSite(f *fileState, n ast.Node) string {
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		if fd, ok := p.(*ast.FuncDecl); ok {
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				r := fd.Recv.List[0]
				rt := string(f.src[f.orig(r.Type.Pos()):f.orig(r.Type.End())])
				rn := ""
				if len(r.Names) == 1 {
					rn = r.Names[0].Name + " "
				}
				return "(" + rn + rt + ")." + name
			}
			return name
		}
	}
	return f.name + ":" + strconv.Itoa(f.at(f.orig(n.Pos())).Line)
}

// lowerStateReads lowers Go code's reads of a shape struct's ui.State
// fields to .Get() — under one state, the server replica: browser truth as
// of the event — and classifies each field so touched synced. A state
// passed where a state is expected is left as the state and classified
// synced (escape means synced). attrs are the on… attributes holding
// statement chains, which lowerAttrWrites owns.
func (e *engine) lowerStateReads(f *fileState, attrs []span) {
	if e.info == nil {
		return
	}
	fields := e.shapeStateVars()
	if len(fields) == 0 {
		return
	}
	// The statements lowerMethodWrites owns: a state write's own selectors
	// are its business (the flip's rhs included).
	writeStmt := map[ast.Stmt]bool{}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) == 1 && len(st.Rhs) == 1 && st.Tok != token.DEFINE {
				if _, ok := stateOf(e.info.TypeOf(st.Lhs[0])); ok {
					if _, rhsState := stateOf(e.info.TypeOf(st.Rhs[0])); !rhsState {
						writeStmt[st] = true
					}
				}
			}
		case *ast.IncDecStmt:
			if _, ok := stateOf(e.info.TypeOf(st.X)); ok {
				writeStmt[st] = true
			}
		}
		return true
	})
	inWrite := func(n ast.Node) bool {
		for p := ast.Node(n); p != nil; p = f.parents[p] {
			if st, ok := p.(ast.Stmt); ok {
				return writeStmt[st]
			}
		}
		return false
	}
	var reads []*ast.SelectorExpr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.lowerable(sel.Pos()) {
			return true
		}
		s := e.info.Selections[sel]
		if s == nil || s.Kind() != types.FieldVal {
			return true
		}
		v, _ := s.Obj().(*types.Var)
		sh, ok := fields[v.Origin()]
		if !ok || !e.goUse(f, sel, attrs) || inWrite(sel) {
			return true
		}
		// The state's own methods (Get, Set) and uigen's calls are the
		// lowering's plumbing, not a Go read.
		if p, ok := f.parents[sel].(*ast.SelectorExpr); ok && p.X == sel {
			if ps := e.info.Selections[p]; ps != nil && ps.Kind() == types.MethodVal {
				if _, isState := stateOf(ps.Recv()); isState {
					return false
				}
			}
		}
		if call, ok := f.parents[sel].(*ast.CallExpr); ok && call.Fun != sel {
			if fn, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := fn.X.(*ast.Ident); ok && id.Obj == nil {
					if pn, ok := e.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == stateGenPathOf(e, f) {
						e.recordSynced(sh.typ, sh.field, "written in "+e.useSite(f, sel))
						return false // uigen.SrvSet(p.q, …): the write's own lowering
					}
				}
			}
			expects, known := expectsStateArg(e, call, sel)
			if !known {
				return false // the callee's type isn't resolved yet: next round
			}
			if expects {
				e.recordSynced(sh.typ, sh.field, "passed to "+callName(f, call)+" in "+e.useSite(f, sel))
				return false // escape: the state itself travels
			}
		}
		if p, ok := f.parents[sel].(*ast.AssignStmt); ok {
			for i, r := range p.Rhs {
				if r == sel && i < len(p.Lhs) {
					if _, ok := stateOf(e.info.TypeOf(p.Lhs[i])); ok {
						e.recordSynced(sh.typ, sh.field, "copied in "+e.useSite(f, sel))
						return false // a state assigned to a state: an alias
					}
				}
			}
		}
		e.recordSynced(sh.typ, sh.field, "read in "+e.useSite(f, sel))
		reads = append(reads, sel)
		return false
	})
	for _, sel := range reads {
		f.rewriteSel(sel, func(s string) string { return s + ".Get()" })
		e.progress = true
	}
}

// stateGenPathOf is the gen package path of f's target, "" when it has none.
func stateGenPathOf(e *engine, f *fileState) string {
	if f.target == nil {
		return ""
	}
	if f.target.pkg != nil {
		return stateGenPath(f.target.pkg)
	}
	if f.target.path != "" && !stdPackage(f.target.path) {
		if p, err := e.imp.Import(f.target.path); err == nil {
			return stateGenPath(p)
		}
	}
	return ""
}

// expectsStateArg reports whether call's parameter for arg is a state, an
// interface or a type parameter — the state itself travels, so the read
// isn't lowered — and whether the callee's type is known.
func expectsStateArg(e *engine, call *ast.CallExpr, arg ast.Expr) (expects, known bool) {
	t := e.info.TypeOf(call.Fun)
	if t == nil || isInvalid(t) {
		return false, false
	}
	sig, _ := t.Underlying().(*types.Signature)
	if sig == nil {
		return false, true // a conversion: the value is wanted
	}
	for i, a := range call.Args {
		if a != arg {
			continue
		}
		var pt types.Type
		switch {
		case sig.Variadic() && i >= sig.Params().Len()-1:
			pt = sig.Params().At(sig.Params().Len() - 1).Type()
			if s, ok := pt.(*types.Slice); ok {
				pt = s.Elem()
			}
		case i < sig.Params().Len():
			pt = sig.Params().At(i).Type()
		default:
			return true, true
		}
		if _, ok := stateOf(pt); ok {
			return true, true
		}
		if _, ok := pt.Underlying().(*types.Interface); ok {
			return true, true
		}
		if _, ok := types.Unalias(pt).(*types.TypeParam); ok {
			return true, true
		}
		return false, true
	}
	return true, true
}

// callName is the callee as written, for the classification's why.
func callName(f *fileState, call *ast.CallExpr) string {
	return string(f.src[f.orig(call.Fun.Pos()):f.orig(call.Fun.End())])
}

// recordStateWrite classifies a method write's state synced: written by Go
// code (one state, §12).
func (e *engine) recordStateWrite(f *fileState, lhs ast.Expr, at ast.Node) {
	sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
	if !ok || e.info == nil {
		return
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return
	}
	v, _ := s.Obj().(*types.Var)
	if v == nil {
		return
	}
	if sh, ok := e.shapeStateVars()[v.Origin()]; ok {
		e.recordSynced(sh.typ, sh.field, "written in "+e.useSite(f, at))
	}
}

// shapeReceiver reports whether t (a pointer) is a struct a .vuka.html file
// of the package renders: its methods may write states even when the page
// isn't live — the write rides the reply of the HTTP push that ran it.
func (e *engine) shapeReceiver(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok {
		return false
	}
	for _, f := range e.vuka {
		if f.html != nil && f.html.shape != nil && f.html.shape.Type == n.Obj().Name() && n.Obj().Pkg() == e.pkg {
			return true
		}
	}
	return false
}

// syncNeeded reports whether the file's shape has synced states to
// register: the trailer regenerates while classification may still move.
func (f *fileState) syncNeeded() bool {
	return f.html != nil && f.html.shape != nil && f.eng != nil && len(f.eng.syncRecs[f.html.shape.Type]) > 0
}

// renderSyncFields writes the shape's classification: one SyncFields call
// per shape struct with synced states, each field with why — what `vuka
// explain` shows.
func (f *fileState) renderSyncFields(w *genWriter) {
	h := f.html
	if h == nil || h.shape == nil || f.eng == nil {
		return
	}
	recs := f.eng.syncRecs[h.shape.Type]
	if len(recs) == 0 {
		return
	}
	gen := stateGenPathOf(f.eng, f)
	if gen == "" {
		return
	}
	names := slices.Sorted(maps.Keys(recs))
	var q, why []string
	for _, n := range names {
		q = append(q, strconv.Quote(n))
		why = append(why, n+": "+recs[n])
	}
	off := f.typeOff(h.shape.Type)
	w.gen("\n// Synced states (one state, §12): their browser writes ride each event,\n// applied before the handler; a server write rides the next render.\n// "+strings.Join(why, "; ")+".\nfunc init() {\n\t"+f.genQual(gen)+"SyncFields["+h.shape.Type+"]("+strings.Join(q, ", ")+")\n}\n", off)
}
