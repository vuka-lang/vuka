package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"
)

// Server state is tracked. A .vuka.html struct's unexported field that
// isn't a state or a dependency is the gen package's Assign of its type, so
// a live session renders the component again only when one changed: reads
// are Get, writes Set, and a write into it (an element, a field) Update.
//
//	rows []Todo                    →  rows uigen.Assign[[]Todo]
//	len(p.rows)                    →  len(p.rows.Get())
//	p.rows = append(p.rows, t)     →  p.rows.Set(append(p.rows.Get(), t))
//	p.n += 2                       →  p.n.Set(p.n.Get() + (2))
//	p.n++                          →  p.n.Set(p.n.Get() + 1)
//	p.rows[i].Done = true          →  p.rows.Update(func(rows []Todo) []Todo { rows[i].Done = true; return rows })
//	p.e.Addr.City                  →  p.e.Get().Addr.City
//	p.e.Label()                    →  p.e.Get().Label()
//	p.e.Name = x                   →  p.e.Update(func(e Employer) Employer { e.Name = x; return e })
//	p.e.Rename(x)  (*Employer)     →  p.e.Update(func(e Employer) Employer { e.Rename(x); return e })
//
// A dependency — a pointer, an interface, a func or a chan, which the
// container injects —, a sync value and a field tagged `vuka:"-"` stay as
// they are. Taking a tracked field's address is E0150: a change through it
// wouldn't be seen.

// lowerTracked lowers f's shape struct's server state: its fields' types in
// one round, their uses in the rounds after.
func (e *engine) lowerTracked(f *fileState, attrs []span) {
	h := f.html
	if h == nil || h.shape == nil || f.target == nil {
		return
	}
	gen := ""
	if f.target.pkg != nil {
		gen = stateGenPath(f.target.pkg)
	} else if f.target.path != "" && !stdPackage(f.target.path) {
		if p, err := e.imp.Import(f.target.path); err == nil {
			gen = stateGenPath(p)
		}
	}
	if gen == "" {
		return
	}
	if !f.trackedTypes {
		e.trackFields(f, h.shape.Type, gen)
		return
	}
	if len(f.tracked) > 0 {
		e.trackUses(f, h.shape.Type, attrs)
	}
}

// trackFields writes the struct's trackable fields' types as Assigns.
func (e *engine) trackFields(f *fileState, name, gen string) {
	var st *ast.StructType
	for _, d := range f.ast.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
			for _, sp := range g.Specs {
				if ts, ok := sp.(*ast.TypeSpec); ok && ts.Name.Name == name {
					st, _ = ts.Type.(*ast.StructType)
				}
			}
		}
	}
	if st == nil {
		return
	}
	type change struct {
		fl  *ast.Field
		typ types.Type
	}
	var changes []change
	for _, fl := range st.Fields.List {
		if len(fl.Names) == 0 {
			continue
		}
		t := e.info.TypeOf(fl.Type)
		if t == nil || isInvalid(t) {
			if e.progress || e.pendingLowering(f) {
				return // wait for the type
			}
			continue
		}
		ok := trackable(t)
		for _, n := range fl.Names {
			ok = ok && !n.IsExported()
		}
		if fl.Tag != nil {
			tag, _ := strconv.Unquote(fl.Tag.Value)
			ok = ok && reflect.StructTag(tag).Get("vuka") != "-"
		}
		if ok {
			changes = append(changes, change{fl, t})
		}
	}
	f.trackedTypes, f.tracked = true, map[string]bool{}
	for _, c := range changes {
		text := string(f.src[f.orig(c.fl.Type.Pos()):f.orig(c.fl.Type.End())])
		f.add(f.orig(c.fl.Type.Pos()), f.orig(c.fl.Type.End()), f.genQual(gen)+"Assign["+text+"]")
		for _, n := range c.fl.Names {
			f.tracked[n.Name] = true
		}
	}
	if len(changes) > 0 {
		e.progress = true
	}
}

// trackable reports whether a field of type t is tracked server state, not
// a state, a dependency or a value that mustn't be copied.
func trackable(t types.Type) bool {
	if _, ok := stateOf(t); ok {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Signature, *types.Chan:
		return false
	case *types.Basic:
		if u.Kind() == types.UnsafePointer {
			return false
		}
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
		switch p := n.Obj().Pkg().Path(); {
		case p == "sync", p == "sync/atomic":
			return false
		case n.Obj().Name() == "Assign":
			return false
		}
	}
	return true
}

// trackUses writes the tracked fields' uses: reads first, then — in a later
// round, so no two edits meet — writes.
func (e *engine) trackUses(f *fileState, name string, attrs []span) {
	obj, _ := e.pkg.Scope().Lookup(name).(*types.TypeName)
	if obj == nil {
		return
	}
	st, _ := obj.Type().Underlying().(*types.Struct)
	if st == nil {
		return
	}
	fields := map[*types.Var]bool{}
	for i := 0; i < st.NumFields(); i++ {
		if f.tracked[st.Field(i).Name()] {
			fields[st.Field(i)] = true
		}
	}
	inAttr := func(off int) bool {
		for _, s := range attrs {
			if off >= s.start && off < s.end {
				return true
			}
		}
		return false
	}
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	type use struct {
		sel *ast.SelectorExpr
		top ast.Expr // the expression rooted at sel the statement writes or reads
		st  ast.Stmt // the statement writing it, nil for a read
	}
	var reads, writes []use
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.lowerable(sel.Pos()) || inAttr(f.orig(sel.Pos())) {
			return true
		}
		s := e.info.Selections[sel]
		if s == nil || s.Kind() != types.FieldVal || !fields[s.Obj().(*types.Var)] {
			return true
		}
		if p, ok := f.parents[sel].(*ast.SelectorExpr); ok && p.X == sel {
			if ps := e.info.Selections[p]; ps != nil && ps.Kind() == types.MethodVal && isAssign(ps.Recv()) {
				return false // sel.Get(), sel.Set(…): lowered, or Assign's own
			}
			// A field or method of the value (p.e.Name, e.Label()): the
			// selector reads or writes through the Assign.
		}
		var top ast.Expr = sel
		for {
			switch p := f.parents[top].(type) {
			case *ast.IndexExpr:
				if p.X == top {
					top = p
					continue
				}
			case *ast.SelectorExpr:
				if p.X == top {
					top = p
					continue
				}
			case *ast.StarExpr:
				top = p
				continue
			case *ast.ParenExpr:
				top = p
				continue
			}
			break
		}
		if m, ok := top.(*ast.SelectorExpr); ok && m != sel && e.pointerMethod(sel, m) {
			// A method changing the value (p.e.Rename(x)): a statement is
			// a write into it; anywhere else it can't be called.
			if call, ok := f.parents[m].(*ast.CallExpr); ok && call.Fun == m {
				if st, ok := f.parents[call].(*ast.ExprStmt); ok {
					writes = append(writes, use{sel, top, st})
					return false
				}
			}
			e.errs.add("E0150", f.at(f.orig(m.Pos())), "%s changes server state inside an expression: call it as a statement of its own, which updates %s", text(m), sel.Sel.Name)
			return false
		}
		switch p := f.parents[top].(type) {
		case *ast.AssignStmt:
			for _, l := range p.Lhs {
				if l == top {
					writes = append(writes, use{sel, top, p})
					return false
				}
			}
		case *ast.IncDecStmt:
			writes = append(writes, use{sel, top, p})
			return false
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				e.errs.add("E0150", f.at(f.orig(p.Pos())), "&%s takes the address of server state: a change through it wouldn't be tracked; tag the field `vuka:\"-\"`, or make it a pointer", text(top))
				return false
			}
		}
		reads = append(reads, use{sel: sel})
		return false
	})
	for _, r := range reads {
		f.rewriteSel(r.sel, func(sel string) string { return sel + ".Get()" })
		e.progress = true
	}
	if len(reads) > 0 {
		return
	}
	for _, w := range writes {
		sel := w.sel
		if w.top != ast.Expr(sel) {
			// A write into the value: Update with it as a variable.
			v := e.freeName(sel.Sel.Name, w.st)
			elem := types.Unalias(e.info.TypeOf(sel)).(*types.Named).TypeArgs().At(0)
			t := e.typeTextAuto(f, elem)
			f.rewriteSel(sel, func(sel string) string { return sel + ".Update(func(" + v + " " + t + ") " + t + " { " + v })
			f.insert(f.orig(w.st.End()), "; return "+v+" })", 0)
			e.progress = true
			continue
		}
		switch st := w.st.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
				e.errs.add("E0150", f.at(f.orig(st.Pos())), "server state %s in a multiple assignment: assign it alone", text(sel))
				continue
			}
			rhs := st.Rhs[0]
			set := ".Set("
			closing := ")"
			if st.Tok != token.ASSIGN {
				op := strings.TrimSuffix(st.Tok.String(), "=")
				set, closing = ".Set("+f.selText(sel)+".Get() "+op+" (", "))"
			}
			f.add(f.orig(sel.End()), f.orig(rhs.Pos()), set)
			f.insert(f.orig(rhs.End()), closing, 0)
		case *ast.IncDecStmt:
			op := "+"
			if st.Tok == token.DEC {
				op = "-"
			}
			f.add(f.orig(st.TokPos), f.orig(st.TokPos)+2, ".Set("+f.selText(sel)+".Get() "+op+" 1)")
		}
		e.progress = true
	}
}

// pointerMethod reports whether m, a selector chain on the tracked field
// sel (p.e.Rename, p.e.Addr.Fix), names a method with a pointer receiver,
// which may change the value it is called on. The chain is resolved on the
// Assign's element: the field's own selections are Assign's now.
func (e *engine) pointerMethod(sel, m *ast.SelectorExpr) bool {
	var chain []*ast.SelectorExpr
	for x := ast.Expr(m); x != ast.Expr(sel); {
		s, ok := x.(*ast.SelectorExpr)
		if !ok {
			return false // an index or a dereference on the way: left as written
		}
		chain = append([]*ast.SelectorExpr{s}, chain...)
		x = s.X
	}
	n, ok := types.Unalias(e.info.TypeOf(sel)).(*types.Named)
	if !ok || !isAssign(n) || n.TypeArgs().Len() != 1 {
		return false
	}
	t := n.TypeArgs().At(0)
	for i, s := range chain {
		obj, _, _ := types.LookupFieldOrMethod(t, true, e.pkg, s.Sel.Name)
		switch o := obj.(type) {
		case *types.Var:
			t = o.Type()
		case *types.Func:
			_, ptr := o.Type().(*types.Signature).Recv().Type().(*types.Pointer)
			return ptr && i == len(chain)-1
		default:
			return false
		}
	}
	return false
}

// isAssign reports whether t is the gen package's Assign, or a pointer
// to it.
func isAssign(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == "Assign" && n.Obj().Pkg() != nil
}

// freeName is name, or name with underscores after it, not an identifier
// of n.
func (e *engine) freeName(name string, n ast.Node) string {
	used := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for used[name] {
		name += "_"
	}
	return name
}

// bareEdit is the edit qualifying sel when the markup wrote its name bare
// (n, the template's b.n), nil when the source wrote the selector.
func (f *fileState) bareEdit(sel *ast.SelectorExpr) *edit {
	start, end := f.orig(sel.Sel.Pos()), f.orig(sel.Sel.End())
	if f.orig(sel.Pos()) != start {
		return nil
	}
	for i := range f.fixed {
		if e := &f.fixed[i]; e.start == start && e.end == end && strings.HasSuffix(e.text, "."+sel.Sel.Name) {
			return e
		}
	}
	return nil
}

// selText is sel as the code writes it, qualified when it was bare.
func (f *fileState) selText(sel *ast.SelectorExpr) string {
	if e := f.bareEdit(sel); e != nil {
		return e.text
	}
	return string(f.src[f.orig(sel.Pos()):f.orig(sel.End())])
}

// rewriteSel writes sel as to(its text): in the edit qualifying it when it
// was bare, else in place of its name — inside its own span either way, so
// a hole's label has it too.
func (f *fileState) rewriteSel(sel *ast.SelectorExpr, to func(string) string) {
	if e := f.bareEdit(sel); e != nil {
		old := len(e.text)
		e.text = to(e.text)
		if e.segs != nil {
			e.segs = append(e.segs, segment{gen: old, genLen: len(e.text) - old, src: e.end})
		}
		return
	}
	name := sel.Sel.Name
	f.add(f.orig(sel.Sel.Pos()), f.orig(sel.Sel.End()), strings.TrimPrefix(to(f.selText(sel)), f.selText(sel)[:len(f.selText(sel))-len(name)]))
}
