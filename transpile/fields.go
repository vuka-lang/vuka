package transpile

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// fieldAttr records a, an attribute the scanner read inside a struct body,
// after p; next is the index of the token after it. A field attribute trails
// its field, after the tag if there is one:
//
//	Title string `json:"title"` @Char{Max: 200}
func (f *fileState) fieldAttr(toks []tok, a *Attr, p tok, next int, typeName, tparams string, topLevel bool) string {
	switch {
	case a.kind != attrTyped && a.kind != attrDecorator:
		return "@" + a.Name + " is for declarations; a field attribute is a value, such as @Char{Max: 200}"
	case typeName == "" || !topLevel:
		return "field attributes go on the fields of a package-level struct type"
	case tparams != "":
		return typeName + " is generic; its fields can't carry attributes"
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

// renderFields registers each struct's field attributes at init, before any
// type decorator runs:
//
//	func init() {
//		vuka.RegisterFields[Post](map[string][]any{
//			"Title": {Char{Max: 200}},
//		})
//	}
func (e *engine) renderFields(f *fileState) {
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
	w := &f.deco
	for _, typ := range order {
		attrs := byType[typ]
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
		w.gen("\nfunc init() {\n\t"+f.runtime()+".RegisterFields["+typ+"](map[string][]any{\n", attrs[0].start)
		for _, n := range names {
			w.gen("\t\t"+strconv.Quote(n)+": {", values[n][0].start)
			for i, a := range values[n] {
				if i > 0 {
					w.gen(", ", a.start)
				}
				e.writeFieldAttr(f, a)
			}
			w.gen("},\n", values[n][0].end)
		}
		w.gen("\t})\n}\n", attrs[len(attrs)-1].end)
	}
}

// writeFieldAttr writes a field attribute's value: T{…} or a call as
// written, a bare T as T's zero value.
func (e *engine) writeFieldAttr(f *fileState, a *Attr) {
	w := &f.deco
	text := string(f.src[a.nameStart:a.end])
	bare := !strings.HasSuffix(text, "}") && !strings.HasSuffix(text, ")")
	if bare {
		w.gen("*new(", a.start)
	}
	if !e.bare {
		p := a.Pos
		p.Column++
		w.gen(lineDirective(p), a.nameStart)
	}
	f.copySrc(w, text, a.nameStart)
	if bare {
		w.gen(")", a.end)
	}
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
// Author's type. The generated call carries a function, never called, that
// selects the same fields, so the compiler and gopls see them:
//
//	vuka.StringRefOf[Post, string]([][]int{{2}, {1}}, func(__x *Post) { _ = __x.Author.Related().Name })
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
		re.errs = append(re.errs, &Error{Pos: f.nodePos(pos), Msg: fmt.Sprintf(format, args...)})
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
		base, first, names := e.refChain(sel)
		if base == nil || e.info.Selections[first] != nil || e.info.Uses[first.Sel] != nil {
			return true
		}
		root := e.info.Types[base].Type
		if _, ok := root.Underlying().(*types.Struct); !ok {
			return true
		}
		at := f.orig(names[0].Pos())
		if f.done[at] {
			return true
		}
		if named, ok := types.Unalias(root).(*types.Named); ok {
			if _, ok := e.findStatic(named, names[0].Name); ok {
				return true
			}
		}
		if e.mnames[names[0].Name] {
			return true // an overloaded method's expression
		}
		cur, fieldT := root, types.Type(nil)
		var steps [][]int
		var rels []bool
		for i, id := range names {
			if i > 0 {
				next, rel, ok := follow(fieldT)
				if !ok {
					if !e.isRefMethod(refKind(fieldT), id.Name) {
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
				continue
			}
			if i > 0 && obj == nil && e.isRefMethod(refKind(fieldT), id.Name) {
				break
			}
			fail(sel, id.Pos(), "%s", e.noField(cur, obj, index, id.Name))
			return false
		}
		if len(steps) == 0 {
			return true
		}
		f.done[at] = true
		rels = rels[:len(steps)-1]
		var idx []string
		for _, s := range steps {
			var parts []string
			for _, k := range s {
				parts = append(parts, itoa(k))
			}
			idx = append(idx, "{"+strings.Join(parts, ", ")+"}")
		}
		rt, baseText := f.runtime(), f.nodeText(base)
		f.insert(f.orig(base.Pos()), rt+"."+refKind(fieldT)+"Of[", 0)
		end := f.orig(base.End())
		dot := end + strings.IndexByte(string(f.src[end:at]), '.')
		f.add(dot, dot+1, ", "+e.typeTextAuto(f, fieldT)+"]([][]int{"+strings.Join(idx, ", ")+"}, func(__x *"+baseText+") { _ = __x.")
		for i, rel := range rels {
			if rel {
				from, to := f.orig(names[i].End()), f.orig(names[i+1].Pos())
				dot := from + strings.IndexByte(string(f.src[from:to]), '.')
				f.add(dot, dot+1, ".Related().")
			}
		}
		f.insert(f.orig(names[len(steps)-1].End()), " })", 0)
		e.progress = true
		return false
	})
}

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
