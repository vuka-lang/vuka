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

// recordProp notes that typ's exported State prop is touched by Go code —
// its own methods', or transitively a component it hands the prop to — so a
// state a parent passes into it must be synced.
func (e *engine) recordProp(typ, field, why string) {
	if e.syncProps == nil {
		e.syncProps = map[string]map[string]string{}
	}
	if e.syncProps[typ] == nil {
		e.syncProps[typ] = map[string]string{}
	}
	if e.syncProps[typ][field] == "" {
		e.syncProps[typ][field] = why
	}
}

// recordUse classifies a Go use of a shape's state: an unexported field is
// synced; an exported State prop is touched, which syncs whatever a parent
// passes into it (resolveSyncEdges).
func (e *engine) recordUse(sh stateShape, why string) {
	if sh.prop {
		e.recordProp(sh.typ, sh.field, why)
		return
	}
	e.recordSynced(sh.typ, sh.field, why)
}

// stateShape is a shape struct's State field, as classification sees it.
type stateShape struct {
	typ   string // the shape struct's name
	field string
	prop  bool // an exported field: a State-typed prop
}

// syncEdge is a state handed to a component's State-typed prop: the prop is
// the same state ("a state passed as a prop is the same state"), so the
// child's uses flow back to the source.
type syncEdge struct {
	src      stateShape
	dstType  string
	dstField string
	via      string
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
			if v.Embedded() {
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
			out[v] = stateShape{typ: f.html.shape.Type, field: v.Name(), prop: v.Exported()}
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
		// Inside the markup, only a func literal the user wrote is Go code
		// (a handler): the compiler's own closures — a slot's content, a
		// guard's block — are still the markup's expressions, and so is a
		// markup func's body (markupfn.go), wherever it is called.
		for p := f.parents[n]; p != nil; p = f.parents[p] {
			switch p := p.(type) {
			case *ast.FuncLit:
				fo := f.orig(p.Pos())
				if f.markupFuncAt(fo) {
					continue
				}
				return fo >= 0 && fo+4 <= len(f.src) && string(f.src[fo:fo+4]) == "func"
			case *ast.FuncDecl:
				return false
			}
		}
		return false
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
		if !ok {
			return true
		}
		// A state in a composite literal's State-typed spot — a component
		// tag's prop — is the state itself, identity-preserving: never
		// .Get(). The child's uses flow back (resolveSyncEdges).
		if d, decided := e.statePropDest(f, sel); decided && d != nil {
			e.recordPropEdge(f, sel, sh, d)
			return false
		} else if decided && d == nil && !e.goUse(f, sel, attrs) {
			return true // a value-typed spot in markup: E0125's business
		} else if !decided {
			return false // the literal's type isn't resolved yet: next round
		}
		if !e.goUse(f, sel, attrs) || inWrite(sel) {
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
						e.recordUse(sh, "written in "+e.useSite(f, sel))
						return false // uigen.SrvSet(p.q, …): the write's own lowering
					}
				}
			}
			expects, known := expectsStateArg(e, call, sel)
			if !known {
				return false // the callee's type isn't resolved yet: next round
			}
			if expects {
				e.recordUse(sh, "passed to "+callName(f, call)+" in "+e.useSite(f, sel))
				return false // escape: the state itself travels
			}
		}
		if p, ok := f.parents[sel].(*ast.AssignStmt); ok {
			for i, r := range p.Rhs {
				if r == sel && i < len(p.Lhs) {
					if _, ok := stateOf(e.info.TypeOf(p.Lhs[i])); ok {
						e.recordUse(sh, "copied in "+e.useSite(f, sel))
						return false // a state assigned to a state: an alias
					}
				}
			}
		}
		e.recordUse(sh, "read in "+e.useSite(f, sel))
		reads = append(reads, sel)
		return false
	})
	for _, sel := range reads {
		f.rewriteSel(sel, func(s string) string { return s + ".Get()" })
		e.progress = true
	}
	if e.goStateInfo != e.info {
		e.goStateInfo = e.info
		e.classifyGoStateUses(fields)
	}
	e.resolveSyncEdges()
}

// statePropDest is the composite-literal spot sel fills, when it fills one:
// the destination's shape when its declared type is a state (the prop takes
// the state itself), nil for a value-typed spot, and decided false while
// the literal's type isn't known yet. Not a composite spot is (nil, true)
// with goUse deciding the rest.
func (e *engine) statePropDest(f *fileState, sel *ast.SelectorExpr) (*syncEdge, bool) {
	var lit *ast.CompositeLit
	key := ""
	at := -1
	switch p := f.parents[sel].(type) {
	case *ast.KeyValueExpr:
		if p.Value != sel {
			return nil, true
		}
		lit, _ = f.parents[p].(*ast.CompositeLit)
		if id, ok := p.Key.(*ast.Ident); ok {
			key = id.Name
		}
	case *ast.CompositeLit:
		lit = p
		for i, el := range p.Elts {
			if el == sel {
				at = i
			}
		}
	}
	if lit == nil || key == "" && at < 0 {
		return nil, true
	}
	t := e.info.TypeOf(lit)
	if t == nil || isInvalid(t) {
		return nil, false
	}
	for {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
			continue
		}
		break
	}
	n, _ := types.Unalias(t).(*types.Named)
	st, _ := t.Underlying().(*types.Struct)
	if st == nil {
		return nil, true // a map or slice literal: the value is wanted
	}
	var fv *types.Var
	if key != "" {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() == key {
				fv = st.Field(i)
			}
		}
	} else if at < st.NumFields() {
		fv = st.Field(at)
	}
	if fv == nil {
		return nil, true
	}
	if _, ok := stateOf(fv.Type()); !ok {
		return nil, true // a value-typed spot
	}
	d := &syncEdge{dstField: fv.Name()}
	if n != nil {
		d.dstType = n.Obj().Name()
		if n.Obj().Pkg() != e.pkg {
			d.dstType = n.Obj().Pkg().Name() + "." + d.dstType
			d.via = "foreign"
		}
	}
	return d, true
}

// recordPropEdge records that sel's state fills the destination's State
// prop: the same state, so the child's uses flow back. A component of
// another package can't be followed — escape means synced.
func (e *engine) recordPropEdge(f *fileState, sel *ast.SelectorExpr, sh stateShape, d *syncEdge) {
	if d.via == "foreign" || d.dstType == "" {
		e.recordUse(sh, "passed to "+d.dstType+"."+d.dstField+" (another package) in "+e.useSite(f, sel))
		return
	}
	ed := syncEdge{src: sh, dstType: d.dstType, dstField: d.dstField, via: e.useSite(f, sel)}
	k := sh.typ + "." + sh.field + "→" + ed.dstType + "." + ed.dstField
	if e.syncEdges == nil {
		e.syncEdges = map[string]syncEdge{}
	}
	e.syncEdges[k] = ed
}

// resolveSyncEdges flows the classification along prop edges to a fixpoint:
// a state handed to a Go-touched State prop is synced; a prop handed on to
// a Go-touched prop is touched.
func (e *engine) resolveSyncEdges() {
	touched := func(t, f string) (string, bool) {
		if e.syncProps[t] == nil {
			return "", false
		}
		why, ok := e.syncProps[t][f]
		return why, ok && why != ""
	}
	for changed := true; changed; {
		changed = false
		for _, ed := range e.syncEdges {
			why, ok := touched(ed.dstType, ed.dstField)
			if !ok {
				continue
			}
			why = "passed to " + ed.dstType + "." + ed.dstField + " in " + ed.via + ", " + why
			if ed.src.prop {
				if _, done := touched(ed.src.typ, ed.src.field); !done {
					e.recordProp(ed.src.typ, ed.src.field, why)
					changed = true
				}
			} else if !e.syncedState(ed.src.typ, ed.src.field) {
				e.recordSynced(ed.src.typ, ed.src.field, why)
				changed = true
			}
		}
	}
}

// classifyGoStateUses classifies the shape state fields plain .go files of
// the package touch: the compiler doesn't rewrite those files — a .go read
// is a hand-written .Get() — but a read is a read, so the field is synced.
func (e *engine) classifyGoStateUses(fields map[*types.Var]stateShape) {
	for _, f := range e.files {
		if f.vuka || f.ast == nil {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s := e.info.Selections[sel]
			if s == nil || s.Kind() != types.FieldVal {
				return true
			}
			v, _ := s.Obj().(*types.Var)
			if v == nil {
				return true
			}
			if sh, ok := fields[v.Origin()]; ok {
				e.recordUse(sh, "used in "+e.useSite(f, sel)+" ("+f.name+")")
			}
			return true
		})
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
		e.recordUse(sh, "written in "+e.useSite(f, at))
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
