package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// A ui.Derived field is a value the browser computes from states, its
// expression the field's @ui.From, read with the struct's fields by their
// bare names as markup reads them. The trailer registers each struct's
// derivations, in the order they read each other, for the gen package to
// run once the struct's states are given:
//
//	total ui.Derived[int] @ui.From(count * price)
//
//	func init() {
//		uigen.Derive(func(c *Cart) {
//			c.total = uigen.From(uigen.Mul(c.count, c.price))
//		})
//	}
//
// A Derived without a @ui.From, a @ui.From on another field, one reading
// no state and Derived fields reading each other in a cycle are E0176; a
// write to a Derived is E0177.

// fromAttr is a field's @ui.From: the attribute, the span of its
// expression in src, and the gen package it lowers to.
type fromAttr struct {
	a        *Attr
	arg      span
	gen      string
	lowered  bool
	failed   bool
	option   string // a state option, "Notify", "Keep", "Shared": the attribute's whole value, ui.Notify(OnTab)
	recv     string
	typeName string
}

// fromAttrs are f's @ui.From attributes and state options (@ui.Notify,
// @ui.Keep, @ui.Shared), found once.
func (f *fileState) fromAttrs() []*fromAttr {
	if f.froms != nil {
		return *f.froms
	}
	if f.ast == nil {
		return nil
	}
	var out []*fromAttr
	for _, a := range f.attrs {
		if !a.Field || len(a.fieldNames) == 0 {
			continue
		}
		pkg, name, ok := strings.Cut(a.Name, ".")
		if !ok || name != "From" && name != "Notify" && name != "Keep" && name != "Shared" {
			continue
		}
		gen := f.statePackageNamed(pkg)
		if gen == "" {
			continue
		}
		if name != "From" {
			out = append(out, &fromAttr{a: a, arg: span{a.nameStart, a.end}, gen: gen, typeName: a.structOf, option: name})
			continue
		}
		args := strings.TrimSpace(a.Args)
		if !strings.HasPrefix(args, "(") || !strings.HasSuffix(args, ")") {
			continue
		}
		open, end := a.nameStart+len(a.Name)+strings.Index(a.Args, "(")+1, a.end-1
		for open < end && isSpace(f.src[open]) {
			open++
		}
		for end > open && isSpace(f.src[end-1]) {
			end--
		}
		out = append(out, &fromAttr{a: a, arg: span{open, end}, gen: gen, typeName: a.structOf})
	}
	f.froms = &out
	return out
}

// statePackageNamed is the gen package of the package f imports as name,
// "" when it declares no client state.
func (f *fileState) statePackageNamed(name string) string {
	for _, imp := range f.ast.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		n := ImportName(path)
		if imp.Name != nil {
			n = imp.Name.Name
		}
		if n != name || stdPackage(path) {
			continue
		}
		if p, err := f.eng.imp.Import(path); err == nil {
			return stateGenPath(p)
		}
	}
	return ""
}

// isFrom reports whether a is a @ui.From, which the trailer writes as a
// derivation rather than registering it.
func (f *fileState) isFrom(a *Attr) bool {
	for _, fa := range f.fromAttrs() {
		if fa.a == a {
			return true
		}
	}
	return false
}

// renderDerived writes each struct's derivations into w.
func (f *fileState) renderDerived(w *genWriter) {
	var order []string
	byType := map[string][]*fromAttr{}
	for _, fa := range f.fromAttrs() {
		if fa.failed {
			continue
		}
		if byType[fa.typeName] == nil {
			order = append(order, fa.typeName)
		}
		byType[fa.typeName] = append(byType[fa.typeName], fa)
	}
	for _, typ := range order {
		list := byType[typ]
		if f.deriveOrder != nil && f.deriveOrder[typ] != nil {
			list = f.deriveOrder[typ]
			for _, fa := range byType[typ] {
				if fa.option != "" {
					list = append(list, fa)
				}
			}
		}
		first := list[0]
		recv := f.deriveRecv(typ)
		w.gen("\nfunc init() {\n\t"+f.genQual(first.gen)+"Derive(func("+recv+" *"+typ+") {\n", first.a.start)
		for _, fa := range list {
			fa.recv = recv
			if fa.option != "" {
				w.gen("\t\t"+f.genQual(fa.gen)+"With("+recv+"."+fa.a.fieldNames[0]+", ", fa.a.start)
				if !f.bare {
					w.gen(lineDirective(f.at(fa.arg.start)), fa.arg.start)
				}
				f.copyRewritten(w, fa.arg)
				if fa.a.Args == "" {
					w.gen("{}", fa.arg.end) // @ui.Shared: a value of the type
				}
				w.gen(")\n", fa.arg.end)
				continue
			}
			w.gen("\t\t"+recv+"."+fa.a.fieldNames[0]+" = "+f.genQual(fa.gen)+"From(", fa.a.start)
			if !f.bare {
				w.gen(lineDirective(f.at(fa.arg.start)), fa.arg.start)
			}
			f.copyRewritten(w, fa.arg)
			w.gen(")\n", fa.arg.end) // where the expression's end maps to
		}
		w.gen("\t})\n}\n", list[len(list)-1].a.end)
	}
}

// deriveRecv is the name a struct's derivations call it: the markup's
// receiver for the template's own struct, else its first letter.
func (f *fileState) deriveRecv(typ string) string {
	if h := f.html; h != nil && h.recvType == typ && h.recv != "" {
		return h.recv
	}
	return strings.ToLower(typ[:1])
}

// lowerDerived qualifies the bare field names in each @ui.From, lowers its
// expression once their types are known, and checks the struct's Derived
// fields.
func (e *engine) lowerDerived(f *fileState) {
	froms := f.fromAttrs()
	if len(froms) == 0 {
		e.underived(f, nil)
		return
	}
	pending := false
	for _, fa := range froms {
		if fa.failed || fa.lowered {
			continue
		}
		x := e.trailerExpr(f, fa.arg)
		if x == nil {
			pending = true
			continue
		}
		obj, _ := e.pkg.Scope().Lookup(fa.typeName).(*types.TypeName)
		if obj == nil {
			continue
		}
		if e.qualifyFrom(f, fa, x, types.NewPointer(obj.Type())) {
			pending = true
			continue
		}
		field, _, _ := types.LookupFieldOrMethod(obj.Type(), true, e.pkg, fa.a.fieldNames[0])
		if fa.option != "" {
			if v, ok := field.(*types.Var); !ok || isInvalid(v.Type()) {
				pending = true
			} else if fa.option == "Notify" {
				e.checkNotify(f, fa, x, v, obj)
			} else {
				e.checkKept(f, fa, x, v)
			}
			continue
		}
		if v, ok := field.(*types.Var); ok {
			if isInvalid(v.Type()) {
				pending = true
				continue
			}
			if _, d := derivedOf(v.Type()); !d {
				fa.failed = true
				e.errs.add("E0176", f.at(fa.a.start), "@%s goes on a ui.Derived field; %s is a %s", fa.a.Name, fa.a.fieldNames[0], e.display(v.Type())).
					help("declare it %s ui.Derived[%s] @%s(…), or drop the attribute", fa.a.fieldNames[0], e.display(elemOf(v.Type())), fa.a.Name)
				e.progress = true
				continue
			}
		}
		if id := e.undefinedIn(x); id != nil && !e.pendingLowering(f) {
			fa.failed = true
			err := e.errs.add("E0176", f.at(f.orig(id.Pos())), "@%s reads %s, which %s has no field named and the package doesn't declare", fa.a.Name, id.Name, fa.typeName)
			if near := closestName(id.Name, readFields(obj.Type())); near != "" {
				err.suggest(near)
			}
			e.progress = true
			continue
		}
		if e.unknownTypes(x) {
			pending = true
			continue
		}
		if name := e.serverField(f, fa, x); name != "" {
			fa.failed = true
			e.errs.add("E0176", f.at(fa.arg.start), "@%s(%s) reads %s, which isn't browser state: a Derived is computed from states, other Derived fields and constants", fa.a.Name, f.src[fa.arg.start:fa.arg.end], name).
				help("read %s where it is shown, in the markup: class:over={%s}", name, f.src[fa.arg.start:fa.arg.end])
			e.progress = true
			continue
		}
		if !e.readsState(x) {
			fa.failed = true
			e.errs.add("E0176", f.at(fa.arg.start), "@%s(%s) reads no ui.State: a Derived is computed from states", fa.a.Name, f.src[fa.arg.start:fa.arg.end]).
				help("read the states it follows, @%s(count * price), or make %s a constant", fa.a.Name, fa.a.fieldNames[0])
			e.progress = true
			continue
		}
		b, err := e.lowerState(f, x)
		if err != nil {
			fa.failed = true
			e.errs.add("E0124", f.at(err.off), "%s", err.msg)
			e.progress = true
			continue
		}
		fa.lowered = true
		f.fixed = append(f.fixed, pieceEdits(fa.arg.start, fa.arg.end, b.pieces)...)
		e.progress = true
	}
	if pending {
		return
	}
	e.underived(f, froms)
	e.orderDerived(f, froms)
}

// trailerExpr is the outermost expression of the trailer starting where
// src[sp] does: what was copied from it.
func (e *engine) trailerExpr(f *fileState, sp span) ast.Expr {
	var out ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if out != nil || n == nil {
			return false
		}
		if f.off(n.End()) <= f.body {
			return false
		}
		if x, ok := n.(ast.Expr); ok && f.off(x.Pos()) >= f.body && f.orig(x.Pos()) == sp.start {
			out = x
			return false
		}
		return true
	})
	return out
}

// checkKept checks a @ui.Keep or @ui.Shared once its expression is typed:
// it goes on a ui.State whose value round-trips JSON, and a key is a
// constant name no other field of the package keeps under.
func (e *engine) checkKept(f *fileState, fa *fromAttr, x ast.Expr, v *types.Var) {
	if fa.lowered || fa.failed {
		return
	}
	if t := e.info.TypeOf(x); t == nil || isInvalid(t) {
		return
	}
	fa.lowered = true
	fail := func(at int, format string, args ...any) *Error {
		fa.failed = true
		e.progress = true
		return e.errs.add("E0179", f.at(at), format, args...)
	}
	elem, ok := stateOf(v.Type())
	if !ok {
		fail(fa.a.start, "@%s goes on a ui.State field; %s is a %s", fa.a.Name, fa.a.fieldNames[0], e.display(v.Type())).
			help("keep the states a Derived reads; it follows them")
		return
	}
	if bad := unjsonable(elem); bad != "" {
		verb := map[string]string{"Keep": "stores", "Shared": "sends"}[fa.option]
		fail(fa.a.start, "@%s %s %s as JSON, and a %s holds %s, which JSON can't", fa.a.Name, verb, fa.a.fieldNames[0], e.display(elem), bad).
			help("keep a value of strings, numbers, bools and their structs, slices and maps")
		return
	}
	if fa.option != "Keep" {
		return
	}
	call, _ := ast.Unparen(x).(*ast.CallExpr)
	if call == nil || len(call.Args) != 1 {
		return
	}
	tv := e.info.Types[call.Args[0]]
	if tv.Value == nil || tv.Value.Kind() != constant.String || constant.StringVal(tv.Value) == "" {
		fail(f.orig(call.Args[0].Pos()), "@%s takes a constant name: the key the browser stores %s under", fa.a.Name, fa.a.fieldNames[0]).
			help("@%s(%q)", fa.a.Name, fa.a.fieldNames[0])
		return
	}
	key := constant.StringVal(tv.Value)
	if e.keptKeys == nil {
		e.keptKeys = map[string]string{}
	}
	at := fa.typeName + "." + fa.a.fieldNames[0]
	if other, ok := e.keptKeys[key]; ok && other != at {
		fail(f.orig(call.Args[0].Pos()), "%s and %s are both kept as %q: one value in the browser's storage", other, at, key).
			help("give each its own key, or share one state: a package-level ui.State")
		return
	}
	e.keptKeys[key] = at
}

// unjsonable is what in t JSON can't hold — a func, a chan, a complex
// number —, "" when there is nothing.
func unjsonable(t types.Type) string {
	seen := map[types.Type]bool{}
	var walk func(t types.Type) string
	walk = func(t types.Type) string {
		if seen[t] {
			return ""
		}
		seen[t] = true
		switch u := t.Underlying().(type) {
		case *types.Signature:
			return "a func"
		case *types.Chan:
			return "a chan"
		case *types.Basic:
			if u.Info()&types.IsComplex != 0 || u.Kind() == types.UnsafePointer {
				return "a " + u.Name()
			}
		case *types.Pointer:
			return walk(u.Elem())
		case *types.Slice:
			return walk(u.Elem())
		case *types.Array:
			return walk(u.Elem())
		case *types.Map:
			return walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if u.Field(i).Exported() {
					if bad := walk(u.Field(i).Type()); bad != "" {
						return bad
					}
				}
			}
		}
		return ""
	}
	return walk(t)
}

// checkNotify checks a @ui.Notify once its expression is typed: it goes
// on a ui.State of a live component, and names a method of it taking the
// state's value (after a context.Context), as its one argument.
func (e *engine) checkNotify(f *fileState, fa *fromAttr, x ast.Expr, v *types.Var, obj *types.TypeName) {
	if fa.lowered || fa.failed {
		return
	}
	if id := e.undefinedIn(x); id != nil {
		if e.pendingLowering(f) {
			return
		}
		fa.failed = true
		err := e.errs.add("E0178", f.at(f.orig(id.Pos())), "@%s names %s, which isn't a method of %s", fa.a.Name, id.Name, fa.typeName)
		if near := closestName(id.Name, methodNames(obj.Type())); near != "" {
			err.suggest(near)
		}
		e.progress = true
		return
	}
	if t := e.info.TypeOf(x); t == nil || isInvalid(t) {
		return
	}
	fa.lowered = true
	elem, ok := stateOf(v.Type())
	if !ok {
		fa.failed = true
		e.errs.add("E0178", f.at(fa.a.start), "@%s goes on a ui.State field; %s is a %s", fa.a.Name, fa.a.fieldNames[0], e.display(v.Type())).
			help("a Derived follows its states: notify one of those")
		e.progress = true
		return
	}
	if h := f.html; h != nil && h.recvType == fa.typeName && h.shape != nil && !h.shape.Live {
		fa.failed = true
		e.errs.add("E0178", f.at(fa.a.start), "@%s calls a method over the page's live connection, and %s isn't live", fa.a.Name, fa.typeName).
			help("embed ui.Live in %s", fa.typeName)
		e.progress = true
		return
	}
	call, _ := ast.Unparen(x).(*ast.CallExpr)
	for call != nil {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name != "Notify" {
			call, _ = ast.Unparen(sel.X).(*ast.CallExpr)
			continue
		}
		break
	}
	if call == nil || len(call.Args) != 1 {
		return
	}
	sig, _ := e.info.TypeOf(call.Args[0]).(*types.Signature)
	name := string(f.src[f.orig(call.Args[0].Pos()):f.orig(call.Args[0].End())])
	if sig == nil {
		fa.failed = true
		e.errs.add("E0178", f.at(f.orig(call.Args[0].Pos())), "@%s takes a method of %s, not %s", fa.a.Name, fa.typeName, name).
			help("name the method the value goes to: @%s(On%s)", fa.a.Name, exportName(fa.a.fieldNames[0]))
		e.progress = true
		return
	}
	params := sig.Params()
	first := 0
	if params.Len() > 0 && isContext(params.At(0).Type()) {
		first = 1
	}
	if params.Len()-first != 1 || !types.AssignableTo(elem, params.At(first).Type()) {
		fa.failed = true
		e.errs.add("E0178", f.at(f.orig(call.Args[0].Pos())), "%s takes %s; @%s on %s calls it with one %s", name, types.TypeString(types.NewTuple(paramList(params)...), types.RelativeTo(e.pkg)), fa.a.Name, fa.a.fieldNames[0], e.display(elem)).
			help("declare it func (…) %s(%s %s)", strings.TrimPrefix(name, fa.recv+"."), fa.a.fieldNames[0], e.display(elem))
		e.progress = true
	}
}

func paramList(t *types.Tuple) []*types.Var {
	out := make([]*types.Var, t.Len())
	for i := range out {
		out[i] = t.At(i)
	}
	return out
}

// methodNames are the names of the methods of *t.
func methodNames(t types.Type) []string {
	ms := types.NewMethodSet(types.NewPointer(t))
	out := make([]string, ms.Len())
	for i := range out {
		out[i] = ms.At(i).Obj().Name()
	}
	return out
}

// serverField is the first field of the struct x reads that is neither a
// state nor a Derived — a prop, server state —, "" when there is none: a
// Derived is computed once, when the struct's states are given.
func (e *engine) serverField(f *fileState, fa *fromAttr, x ast.Expr) string {
	name := ""
	ast.Inspect(x, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || name != "" {
			return name == ""
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != fa.recv {
			return true
		}
		if v, ok := e.info.Uses[sel.Sel].(*types.Var); ok && v.IsField() {
			if _, st := readOf(v.Type()); !st {
				name = sel.Sel.Name
			}
		}
		return false
	})
	return name
}

// qualifyFrom writes the bare names in a @ui.From's expression x naming a
// field or method of recv as the receiver's. It reports whether it wrote
// any: the expression is typed again first.
func (e *engine) qualifyFrom(f *fileState, fa *fromAttr, x ast.Expr, recv types.Type) bool {
	wrote := false
	ast.Inspect(x, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && e.qualifyIdent(f, fa, id, recv) {
					wrote = true
				}
				return true
			})
			return false
		case *ast.KeyValueExpr:
			ast.Inspect(n.Value, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && e.qualifyIdent(f, fa, id, recv) {
					wrote = true
				}
				return true
			})
			return false
		case *ast.Ident:
			if e.qualifyIdent(f, fa, n, recv) {
				wrote = true
			}
		}
		return true
	})
	return wrote
}

func (e *engine) qualifyIdent(f *fileState, fa *fromAttr, id *ast.Ident, recv types.Type) bool {
	off := f.orig(id.Pos())
	if f.done[off] {
		return false
	}
	if o := e.info.Uses[id]; o != nil && o.Parent() != e.pkg.Scope() {
		return false
	}
	if o, _, _ := types.LookupFieldOrMethod(recv, true, e.pkg, id.Name); o == nil {
		return false
	}
	f.done[off] = true
	q := fa.recv + "."
	if fa.recv == "" {
		q = f.deriveRecv(fa.typeName) + "."
	}
	f.fixed = append(f.fixed, edit{start: off, end: off + len(id.Name), text: q + id.Name,
		segs: []segment{{genLen: len(q), src: off}, {gen: len(q), genLen: len(id.Name), src: off, srcLen: len(id.Name), copy: true}}})
	e.progress = true
	return true
}

// underived reports each Derived field of f's structs without a @ui.From,
// and a field with two.
func (e *engine) underived(f *fileState, froms []*fromAttr) {
	if f.underivedDone || e.info == nil {
		return
	}
	f.underivedDone = true
	has := map[string]*fromAttr{}
	for _, fa := range froms {
		if fa.option != "" {
			continue
		}
		k := fa.typeName + "." + fa.a.fieldNames[0]
		if has[k] != nil {
			fa.failed = true
			e.errs.add("E0176", f.at(fa.a.start), "%s has a second @%s", fa.a.fieldNames[0], fa.a.Name).help("keep one")
			continue
		}
		has[k] = fa
	}
	for _, d := range f.ast.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE || !f.lowerable(g.Pos()) {
			continue
		}
		for _, sp := range g.Specs {
			ts := sp.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, fl := range st.Fields.List {
				elem, d := derivedOf(e.info.TypeOf(fl.Type))
				if !d {
					continue
				}
				for _, n := range fl.Names {
					if has[ts.Name.Name+"."+n.Name] != nil {
						continue
					}
					if ts.TypeParams != nil {
						continue
					}
					e.errs.add("E0176", f.at(f.orig(n.Pos())), "%s is a ui.Derived with no @ui.From: it says what the browser computes it from", n.Name).
						help("%s ui.Derived[%s] @ui.From(count * price)", n.Name, e.display(elem))
				}
			}
			if ts.TypeParams != nil {
				for _, fa := range froms {
					if fa.typeName == ts.Name.Name && !fa.failed {
						fa.failed = true
						e.errs.add("E0176", f.at(fa.a.start), "%s has type parameters: its Derived fields can't be derived", ts.Name.Name).
							help("derive the value in a struct without type parameters")
					}
				}
			}
		}
	}
}

// orderDerived orders each struct's derivations so a Derived is computed
// after those it reads, or reports the cycle they read each other in.
func (e *engine) orderDerived(f *fileState, froms []*fromAttr) {
	if f.deriveOrder != nil {
		return
	}
	f.deriveOrder = map[string][]*fromAttr{}
	byType := map[string][]*fromAttr{}
	for _, fa := range froms {
		if !fa.failed && fa.option == "" {
			byType[fa.typeName] = append(byType[fa.typeName], fa)
		}
	}
	for typ, list := range byType {
		byName := map[string]*fromAttr{}
		for _, fa := range list {
			byName[fa.a.fieldNames[0]] = fa
		}
		reads := map[*fromAttr][]*fromAttr{}
		for _, fa := range list {
			for _, name := range identsIn(string(f.src[fa.arg.start:fa.arg.end])) {
				if r := byName[name]; r != nil {
					reads[fa] = append(reads[fa], r)
				}
			}
		}
		var out []*fromAttr
		state := map[*fromAttr]int{} // 1 visiting, 2 done
		var path []*fromAttr
		var visit func(fa *fromAttr) bool
		visit = func(fa *fromAttr) bool {
			switch state[fa] {
			case 1:
				i := 0
				for path[i] != fa {
					i++
				}
				names := []string{}
				for _, p := range path[i:] {
					names = append(names, p.a.fieldNames[0])
				}
				names = append(names, fa.a.fieldNames[0])
				for _, p := range path[i:] {
					p.failed = true
				}
				e.errs.add("E0176", f.at(fa.a.start), "Derived fields read each other: %s", strings.Join(names, " → ")).
					help("derive one of them from states only")
				return false
			case 2:
				return true
			}
			state[fa] = 1
			path = append(path, fa)
			for _, r := range reads[fa] {
				if !visit(r) {
					return false
				}
			}
			path = path[:len(path)-1]
			state[fa] = 2
			out = append(out, fa)
			return true
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].a.start < list[j].a.start })
		for _, fa := range list {
			if state[fa] == 0 && !visit(fa) {
				break
			}
		}
		f.deriveOrder[typ] = out
	}
	e.progress = true
}

// identsIn are the identifiers of Go expression text not after a dot.
func identsIn(src string) []string {
	var out []string
	for i := 0; i < len(src); {
		c := src[i]
		if c == '"' || c == '`' || c == '\'' {
			q := c
			for i++; i < len(src) && src[i] != q; i++ {
				if src[i] == '\\' && q != '`' {
					i++
				}
			}
			i++
			continue
		}
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			if k := strings.TrimRight(src[:i], " \t"); !strings.HasSuffix(k, ".") {
				out = append(out, src[i:j])
			}
			i = j
			continue
		}
		i++
	}
	return out
}

// undefinedIn is the first name x reads that nothing declares.
func (e *engine) undefinedIn(x ast.Expr) *ast.Ident {
	var out *ast.Ident
	ast.Inspect(x, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && out == nil && e.info.Uses[id] == nil && e.info.Defs[id] == nil && id.Name != "_" {
					out = id
				}
				return out == nil
			})
			return false
		case *ast.Ident:
			if e.info.Uses[n] == nil && e.info.Defs[n] == nil && n.Name != "_" {
				out = n
			}
		}
		return true
	})
	return out
}

// readFields are the names of t's state and Derived fields.
func readFields(t types.Type) []string {
	st, _ := t.Underlying().(*types.Struct)
	if st == nil {
		return nil
	}
	var out []string
	for i := 0; i < st.NumFields(); i++ {
		if _, ok := readOf(st.Field(i).Type()); ok {
			out = append(out, st.Field(i).Name())
		}
	}
	return out
}

// elemOf is a State's or Derived's value type, else t.
func elemOf(t types.Type) types.Type {
	if elem, ok := readOf(t); ok {
		return elem
	}
	return t
}

// derivedWrite is the message of a write to a Derived, lhs as written.
func derivedWrite(lhs string) string {
	return lhs + " is a ui.Derived: the browser computes it from the states it reads, and nothing writes it"
}
