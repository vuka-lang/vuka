package transpile

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// fieldAttr records a, an attribute the scanner read inside a struct body,
// after p; next is the index of the token after it. A field attribute trails
// its field, after the tag if there is one:
//
//	Title string `json:"title"` @Char{Max: 200}
func (f *fileState) fieldAttr(toks []tok, a *Attr, p tok, next int, typeName string, topLevel bool) string {
	switch {
	case a.kind != attrTyped && a.kind != attrDecorator:
		return "@" + a.Name + " is for declarations; a field attribute is a value, such as @Char{Max: 200}"
	case typeName == "" || !topLevel:
		return "field attributes go on the fields of a package-level struct type"
	case p.tok == token.SEMICOLON || p.tok == token.LBRACE:
		return "a field attribute goes after its field, on the same line: Title string @Char{Max: 200}"
	}
	k := next
	for k < len(toks) && toks[k].tok == token.COMMENT {
		k++
	}
	if k < len(toks) {
		switch t := toks[k]; {
		case t.tok == token.STRING:
			return "a field's attributes go after its tag: Title string `json:\"title\"` @" + a.Name
		case isAt(t), t.tok == token.SEMICOLON, t.tok == token.RBRACE:
		default:
			return "only another attribute or a comment can follow a field attribute"
		}
	}
	a.kind, a.Decorator, a.bare, a.Field, a.structOf = attrField, false, false, true, typeName
	f.attrs = append(f.attrs, a)
	return ""
}

// attachField finds the field a trails: the last of its struct's fields
// ending before it.
func (f *fileState) attachField(a *Attr) {
	for _, d := range f.ast.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, s := range g.Specs {
			ts := s.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok || ts.Name.Name != a.structOf {
				continue
			}
			var last *ast.Field
			for _, field := range st.Fields.List {
				if f.orig(field.End()) <= a.start {
					last = field
				}
			}
			if last == nil {
				continue
			}
			a.fieldNames = nil
			for _, n := range last.Names {
				a.fieldNames = append(a.fieldNames, n.Name)
			}
			if len(last.Names) == 0 {
				a.fieldNames = []string{embeddedName(last.Type)}
			}
			a.Decl = a.structOf + "." + a.fieldNames[0]
		}
	}
}

// renderFields declares each struct's field attributes. A struct with a
// schema (schema.go) gets its schema method; any other registers them at
// init, before any type decorator runs:
//
//	func init() {
//		vuka.RegisterFields[Address](map[string][]any{
//			"City": {Char{Max: 50}},
//		})
//	}
//
// They are written with the trailer, each time it is, so the rewrites
// decided inside the attributes (Some and None, statics) reach them.
func (e *engine) renderFields(f *fileState) {
	for _, a := range f.attrs {
		if a.Field && len(a.fieldNames) > 0 {
			f.fieldsOn = true
			if e.schemas[a.structOf] == nil {
				f.runtime()
			}
		}
	}
	for _, si := range e.schemas {
		if si.file == f && len(si.hooks) > 0 {
			f.fieldsOn = true
		}
	}
}

func (f *fileState) renderFieldAttrs() {
	f.fieldGen = genWriter{}
	if !f.fieldsOn {
		return
	}
	var order []string
	byType := map[string][]*Attr{}
	for _, a := range f.attrs {
		if !a.Field || len(a.fieldNames) == 0 {
			continue
		}
		if byType[a.structOf] == nil {
			order = append(order, a.structOf)
		}
		byType[a.structOf] = append(byType[a.structOf], a)
	}
	// The types with hooks and no field attributes, in source order: the
	// schema table is a map.
	var hooked []string
	for _, si := range f.eng.schemas {
		if si.file == f && byType[si.typeName] == nil && len(si.hooks) > 0 {
			hooked = append(hooked, si.typeName)
		}
	}
	sort.SliceStable(hooked, func(i, j int) bool { return f.typeOff(hooked[i]) < f.typeOff(hooked[j]) })
	order = append(order, hooked...)
	w := &f.fieldGen
	for _, typ := range order {
		attrs := byType[typ]
		if si := f.eng.schemas[typ]; si != nil {
			f.renderSchema(w, si, attrs)
			continue
		}
		var names []string
		values := map[string][]*Attr{}
		for _, a := range attrs {
			for _, n := range a.fieldNames {
				if values[n] == nil {
					names = append(names, n)
				}
				values[n] = append(values[n], a)
			}
		}
		w.gen("\nfunc init() {\n\t"+f.rt+".RegisterFields["+typ+"](map[string][]any{\n", attrs[0].start)
		for _, n := range names {
			w.gen("\t\t"+strconv.Quote(n)+": {", values[n][0].start)
			f.writeAttrList(w, values[n])
			w.gen("},\n", values[n][0].end)
		}
		w.gen("\t})\n}\n", attrs[len(attrs)-1].end)
	}
}

// writeFieldAttr writes a field attribute's value: T{…} or a call as
// written, a bare T as T{} (T a struct, slice, map or array) or T's zero
// value.
func (f *fileState) writeFieldAttr(w *genWriter, a *Attr) {
	text := string(f.src[a.nameStart:a.end])
	bare := !strings.HasSuffix(text, "}") && !strings.HasSuffix(text, ")")
	composite := bare && f.eng.composite(f, a.Name)
	if bare && !composite {
		w.gen("*new(", a.start)
	}
	if !f.bare {
		p := a.Pos
		p.Column++
		w.gen(lineDirective(p), a.nameStart)
	}
	if a.Field {
		f.copyRewritten(w, span{a.nameStart, a.end})
	} else {
		f.copySrc(w, text, a.nameStart)
	}
	switch {
	case composite:
		w.gen("{}", a.end)
	case bare:
		w.gen(")", a.end)
	}
}

// inFieldAttr reports whether src[start:end] is inside a field attribute's
// value, which is lowered where the trailer registers it.
func (f *fileState) inFieldAttr(start, end int) bool {
	for _, a := range f.attrs {
		if a.Field && start >= a.nameStart && end <= a.end {
			return true
		}
	}
	return false
}

// copyRewritten copies src[sp] into w with the rewrites decided inside it
// and its vuka.File literals in their place.
func (f *fileState) copyRewritten(w *genWriter, sp span) {
	type piece struct {
		edit
		file bool
	}
	var ps []piece
	for _, e := range f.fixed.sorted() {
		if e.start >= sp.start && e.end <= sp.end {
			ps = append(ps, piece{edit: e})
		}
	}
	for _, l := range f.files {
		if l.off >= sp.start && l.end <= sp.end {
			ps = append(ps, piece{edit: edit{start: l.off, end: l.end, text: l.repl}, file: true})
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].start < ps[j].start })
	last := sp.start
	for _, p := range ps {
		if p.start < last {
			continue
		}
		w.copy(string(f.src[last:p.start]), last)
		if p.file {
			w.gen(p.text, p.start)
		} else {
			w.replace(p.text, p.start, p.end-p.start)
		}
		last = p.end
	}
	w.copy(string(f.src[last:sp.end]), last)
}

// refChain splits a selector chain at the type it starts from: Post.Author.Name
// is the type Post and the names Author, Name; pkg.User.Name is pkg.User and
// Name. base is nil when no prefix of the chain is a type.
func (e *engine) refChain(sel *ast.SelectorExpr) (base ast.Expr, first *ast.SelectorExpr, names []*ast.Ident) {
	var sels []*ast.SelectorExpr // outermost first
	x := ast.Expr(sel)
	for {
		s, ok := x.(*ast.SelectorExpr)
		if !ok {
			break
		}
		sels = append(sels, s)
		x = s.X
	}
	for k := len(sels); k > 0; k-- {
		b := x
		if k < len(sels) {
			b = sels[k]
		}
		if tv, ok := e.info.Types[b]; ok && tv.IsType() {
			for i := k - 1; i >= 0; i-- {
				names = append(names, sels[i].Sel)
			}
			return b, sels[k-1], names
		}
	}
	return nil, nil, nil
}

// resolveFieldRefs lowers every field reference: Post.Title, where Post is a
// struct type, becomes a typed reference to the field, a vuka.StringRef[Post,
// string]; Post.Author.Name goes on into the struct (or Related target) of
// Author's type. The generated call is the field by address, so the
// compiler and gopls see the fields:
//
//	vuka.StringRefTo(func(__x *Post) *string { return &__x.Author.Related().Name })
//
// A name that is no field is reported once no round makes progress: a static
// New, say, only exists once its decorator's form is known.
func (e *engine) resolveFieldRefs(f *fileState) {
	delete(e.refErrs, f)
	fail := func(sel *ast.SelectorExpr, pos token.Pos, format string, args ...any) {
		re := e.refErrs[f]
		if re == nil {
			re = &refErrs{hide: map[token.Position]bool{}}
			e.refErrs[f] = re
		}
		re.errs = append(re.errs, &Error{Pos: f.nodePos(pos), Msg: fmt.Sprintf(format, args...), Code: "E0109"})
		ast.Inspect(sel, func(n ast.Node) bool { // Go's errors on the same selectors say less
			if n != nil {
				re.hide[f.nodePos(n.Pos())] = true
			}
			return true
		})
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.lowerable(sel.Pos()) {
			return true
		}
		if sel.Sel.Name == completionPlaceholder {
			return true // mid-edit: the inner chain still lowers
		}
		base, first, names := e.refChain(sel)
		if base == nil || e.info.Selections[first] != nil || e.info.Uses[first.Sel] != nil {
			return true
		}
		root := e.info.Types[base].Type
		if _, ok := root.Underlying().(*types.Struct); !ok {
			return true
		}
		at := f.orig(names[0].Pos())
		if f.done[at] || f.done[f.orig(base.Pos())] { // the base is a field: resolveBare took it
			return true
		}
		if named, ok := types.Unalias(root).(*types.Named); ok {
			if ref, ok := e.findStatic(named, names[0].Name); ok {
				if obj, _, _ := types.LookupFieldOrMethod(named, false, e.pkg, names[0].Name); obj == nil || e.declaredStatic(ref) {
					return true
				}
			}
		}
		if e.mnames[names[0].Name] {
			return true // an overloaded method's expression
		}
		// A type with generated typed fields reads the methods after its
		// fields from them (orm.Field's Gte, Expr): the compiler checks those.
		typed := hasMarker(root, "vukaFields")
		cur, fieldT := root, types.Type(nil)
		var steps [][]int
		var rels []bool
		var stepTs []types.Type
		for i, id := range names {
			if i > 0 {
				next, rel, ok := follow(fieldT)
				if !ok {
					if !typed && !e.isRefMethod(refKind(fieldT), id.Name) {
						fail(sel, id.Pos(), "%s is a %s; it has no field %s", e.refName(base, names[:i]), e.display(fieldT), id.Name)
						return false
					}
					break
				}
				cur = next
				rels = append(rels, rel)
			}
			obj, index, _ := types.LookupFieldOrMethod(cur, false, e.pkg, id.Name)
			if v, ok := obj.(*types.Var); ok && v.IsField() {
				steps = append(steps, index)
				fieldT = v.Type()
				stepTs = append(stepTs, fieldT)
				continue
			}
			if i > 0 && obj == nil && (typed || e.isRefMethod(refKind(fieldT), id.Name)) {
				break
			}
			if i == 0 && obj == nil && typed && modelMethods[id.Name] {
				f.done[at] = true
				e.typedFields(f, base, nil, nil)
				return false
			}
			fail(sel, id.Pos(), "%s", e.noField(cur, obj, index, id.Name))
			return false
		}
		if len(steps) == 0 {
			return true
		}
		f.done[at] = true
		if typed {
			e.typedFields(f, base, names[:len(steps)], stepTs)
			return false
		}
		rels = rels[:len(steps)-1]
		rt, vt := f.runtime(), e.typeTextAuto(f, fieldT)
		f.insert(f.orig(base.Pos()), rt+"."+refKind(fieldT)+"To(func(__x *", 0)
		end := f.orig(base.End())
		dot := end + strings.IndexByte(string(f.src[end:at]), '.')
		f.add(dot, dot+1, ") *"+vt+" { return &__x.")
		// go/types and the compiler abbreviate the function literal differently.
		b, ref := types.ExprString(base), e.refName(base, names[:len(steps)])
		for _, fn := range []string{"func(__x *" + b + ") *" + vt + " {…}", "(func(__x *" + b + ") *" + vt + " literal)"} {
			f.spell(rt+"."+refKind(fieldT)+"To("+fn+")", ref, f.orig(base.Pos()))
		}
		for i, rel := range rels {
			if rel {
				from, to := f.orig(names[i].End()), f.orig(names[i+1].Pos())
				dot := from + strings.IndexByte(string(f.src[from:to]), '.')
				f.add(dot, dot+1, ".Related().")
			}
		}
		// The closing replaces the last name, not inserted after it: a copy of
		// the expression — a hole hoisted out of its markup — takes only what
		// is inside it.
		lastID := names[len(steps)-1]
		from := f.orig(lastID.Pos())
		f.fixed = append(f.fixed, edit{start: from, end: from + len(lastID.Name), text: lastID.Name + " })",
			segs: []segment{{genLen: len(lastID.Name), src: from, srcLen: len(lastID.Name), copy: true}, {gen: len(lastID.Name), genLen: 3, src: from + len(lastID.Name)}}})
		e.progress = true
		return false
	})
}

// modelMethods are the methods of a model's typed fields that name the
// model's table rather than a field: Product.On(…) is ProductColumns.On(…),
// the table joined where the conditions hold, and User.As("editor") the
// table under another name.
var modelMethods = map[string]bool{"On": true, "As": true}

// refErrs are a file's field references that name no field, as of this
// round, and the positions where Go's own errors about them are hidden.
type refErrs struct {
	errs []*Error
	hide map[token.Position]bool
}

// refName writes a reference as the source does, for messages.
func (e *engine) refName(base ast.Expr, names []*ast.Ident) string {
	s := types.ExprString(base)
	for _, n := range names {
		s += "." + n.Name
	}
	return s
}

// noField explains why name is no field of t.
func (e *engine) noField(t types.Type, obj types.Object, index []int, name string) string {
	ts := e.display(t)
	switch {
	case obj != nil:
		return ts + "." + name + " is a method; a field reference names fields"
	case index != nil:
		return ts + "." + name + " is ambiguous: more than one embedded struct has a field " + name
	}
	if n, ok := types.Unalias(derefType(t)).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg() != e.pkg {
		if obj, _, _ := types.LookupFieldOrMethod(t, false, n.Obj().Pkg(), name); obj != nil {
			return ts + "." + name + " is unexported; another package's code can't refer to it"
		}
	}
	names := structFields(t)
	if s := closestName(name, names); s != "" {
		return ts + " has no field " + name + "; did you mean " + s + "?"
	}
	return ts + " has no field " + name
}

// structFields are the names of t's fields, promoted ones included.
func structFields(t types.Type) []string {
	var names []string
	seen := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		st, ok := derefType(t).Underlying().(*types.Struct)
		if !ok || seen[t] {
			return
		}
		seen[t] = true
		for i := range st.NumFields() {
			fv := st.Field(i)
			names = append(names, fv.Name())
			if fv.Embedded() {
				walk(fv.Type())
			}
		}
	}
	walk(t)
	return names
}

// follow is the struct a field reference continues into from a field of type
// t: the target of a Related type (rel), else the struct t is or points to.
func follow(t types.Type) (next types.Type, rel, ok bool) {
	pt := t
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		pt = types.NewPointer(t)
	}
	if fn, ok := lookupMethod(pt, "Related"); ok {
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			if p, ok := sig.Results().At(0).Type().Underlying().(*types.Pointer); ok {
				if _, ok := p.Elem().Underlying().(*types.Struct); ok {
					return p.Elem(), true, true
				}
			}
		}
	}
	t = derefType(t)
	if _, ok := t.Underlying().(*types.Struct); ok {
		return t, false, true
	}
	return nil, false, false
}

func lookupMethod(t types.Type, name string) (*types.Func, bool) {
	obj, _, _ := types.LookupFieldOrMethod(t, false, nil, name)
	fn, ok := obj.(*types.Func)
	return fn, ok
}

// refKind is the runtime's reference type for a field of type v.
func refKind(v types.Type) string {
	if _, ok := v.(*types.TypeParam); ok {
		return "Ref"
	}
	switch u := v.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			return "StringRef"
		case u.Info()&(types.IsInteger|types.IsFloat) != 0:
			return "OrderedRef"
		case u.Kind() == types.UnsafePointer:
			return "NullableRef"
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface, *types.Chan, *types.Signature:
		return "NullableRef"
	}
	if fn, ok := lookupMethod(v, "Compare"); ok {
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() == 1 && types.Identical(sig.Params().At(0).Type(), v) && sig.Results().Len() == 1 &&
			types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) {
			return "CompareRef"
		}
	}
	if name, _ := runtimeType(v); name == "Option" {
		return "NullableRef"
	}
	return "Ref"
}

// isRefMethod reports whether name is a method of the runtime's reference
// type kind, such as Eq or Desc. Without the runtime at hand it says yes, and
// the compiler judges.
func (e *engine) isRefMethod(kind, name string) bool {
	pkg, err := e.imp.Import(RuntimePath)
	if err != nil {
		return true
	}
	obj := pkg.Scope().Lookup(kind)
	if obj == nil {
		return true
	}
	m, _, _ := types.LookupFieldOrMethod(obj.Type(), false, pkg, name)
	_, ok := m.(*types.Func)
	return ok
}

// mayUseFieldRefs reports whether f might hold a field reference, so the
// package has to be type-checked: a selector on one of the package's types
// (or a type declared in a function) naming none of its methods. References
// on another package's types are found by mayUseStatics.
func (e *engine) mayUseFieldRefs(f *fileState) bool {
	if e.methodNames == nil {
		e.methodNames = map[string]bool{}
		for _, file := range e.files {
			for _, d := range file.ast.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
					e.methodNames[recvBase(fd)+"."+fd.Name.Name] = true
				}
			}
		}
	}
	found := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if found || !ok {
			return !found
		}
		x := ast.Unparen(sel.X)
		if b, _, n := indexParts(x); n > 0 {
			x = b
		}
		if id, ok := x.(*ast.Ident); ok {
			isType := e.typeNames[id.Name]
			if id.Obj != nil {
				isType = id.Obj.Kind == ast.Typ
			}
			found = isType && !e.methodNames[id.Name+"."+sel.Sel.Name]
		}
		return !found
	})
	return found
}

// typedFields lowers a reference to a field of a type with generated typed
// fields (its embedded type has the marker method vukaFields): Post.Slug is
// PostFields.Slug, Post.Author.Email PostFields.Author.Email; a relation
// beyond the first is reached by a method, PostFields.Author.Posts().Title.
func (e *engine) typedFields(f *fileState, base ast.Expr, names []*ast.Ident, ts []types.Type) {
	suffix := "Fields"
	if columnsRuntime(e.info.Types[base].Type, map[types.Type]bool{}) {
		suffix = "Columns"
	}
	if n, ok := derefType(e.info.Types[base].Type).(*types.Named); ok && n.Obj().Pkg() != nil {
		if _, ok := n.Obj().Pkg().Scope().Lookup(n.Obj().Name() + "Columns").(*types.Var); ok {
			suffix = "Columns"
		}
	}
	relationsAreMethods := suffix == "Columns"
	if n, ok := derefType(e.info.Types[base].Type).(*types.Named); ok && n.Obj().Pkg() != nil {
		if root, ok := n.Obj().Pkg().Scope().Lookup(n.Obj().Name() + suffix).(*types.Var); ok {
			_, relationsAreMethods = types.Unalias(root.Type()).(*types.Named)
		}
	}
	f.insert(f.orig(base.End()), suffix, 0)
	gen := types.ExprString(base) + suffix
	for i, name := range names {
		gen += "." + name.Name
		if (i > 0 || relationsAreMethods) && modelRel(ts[i]) {
			f.insert(f.orig(name.End()), "()", 0)
			gen += "()"
		}
	}

	f.spell(gen, e.refName(base, names), f.orig(base.Pos()))
	e.progress = true
}

// modelRel reports whether a field of type t is a relation to a model with
// typed fields: an FK or M2M (a Related method), or such a model, a pointer
// to one or a slice of them.
func modelRel(t types.Type) bool {
	if _, rel, ok := follow(t); ok && rel {
		return true
	}
	t = derefType(t)
	if s, ok := t.Underlying().(*types.Slice); ok {
		t = derefType(s.Elem())
	}
	return hasMarker(t, "vukaFields")
}

// hasMarker reports whether struct type t embeds, at any depth, a type with
// the marker method name, declared in that type's package.
func hasMarker(t types.Type, name string) bool {
	seen := map[types.Type]bool{}
	var walk func(t types.Type) bool
	walk = func(t types.Type) bool {
		st, ok := derefType(t).Underlying().(*types.Struct)
		if !ok || seen[t] {
			return false
		}
		seen[t] = true
		for i := range st.NumFields() {
			fv := st.Field(i)
			if !fv.Embedded() {
				continue
			}
			if n := namedOf(fv.Type()); n != nil && n.Obj().Pkg() != nil {
				if _, ok := pkgMethod(types.NewPointer(n), n.Obj().Pkg(), name); ok {
					return true
				}
			}
			if walk(fv.Type()) {
				return true
			}
		}
		return false
	}
	return walk(t)
}

// columnsRuntime recognizes the ORM generation API before the first generated
// file exists, so a clean build selects the same root name as ormgen.
func columnsRuntime(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if p, ok := t.(*types.Pointer); ok {
		return columnsRuntime(p.Elem(), seen)
	}
	if n, ok := t.(*types.Named); ok {
		if p := n.Obj().Pkg(); p != nil && p.Path() == "github.com/vuka-lang/orm/v2" && p.Scope().Lookup("ModelAnnotation") != nil {
			return true
		}
	}
	if st, ok := t.Underlying().(*types.Struct); ok {
		for i := range st.NumFields() {
			if st.Field(i).Anonymous() && columnsRuntime(st.Field(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}
