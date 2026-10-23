package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// staticDecl is a static field: `static Objects = Manager[User]{}` inside a
// struct type. It leaves the struct and becomes a package-level variable (or
// constant) named after the type, UserObjects; on a generic type, an accessor
// holding one value per instantiation, ModelObjects[Self]().V.
type staticDecl struct {
	typeName   string
	typeParams string   // the type's type parameter list, "[Self any]"; "" when not generic
	tparams    []string // its names
	name       string
	isConst    bool
	typ, init  span // in src; start < 0 when absent
	start, end int  // the whole field, in src
	nameOff    int
	vt, rt     string // a generic type's static: its value's type, the runtime's name
	infer      bool   // vt is a placeholder until the initializer is type-checked
}

// staticFunc is a static method: func User.Create(…) or func Model[Self].Find(…).
type staticFunc struct {
	typeName string
	tparams  []string
	head     span // `Model[Self].Find`, replaced by the Go name
	goName   string
}

// staticGoName is the Go name of a static member: the type's name and the
// member's, UserTable, unexported (userCount) when either is, so it stays
// private. A type's constructor, its static New, is Go's NewType (newType
// for an unexported type).
func staticGoName(typeName, member string) string {
	if member == "New" && typeName != "" {
		if token.IsExported(typeName) {
			return "New" + typeName
		}
		return "new" + strings.ToUpper(typeName[:1]) + typeName[1:]
	}
	name := typeName + strings.ToUpper(member[:1]) + member[1:]
	if !token.IsExported(member) {
		name = strings.ToLower(name[:1]) + name[1:]
	}
	return name
}

// oldStaticGoName is a static's name in packages built before TypeMember
// names: Type_Member, _Type_member unexported. A released package of that
// age (orm's Base_Objects) keeps its statics.
func oldStaticGoName(typeName, member string) string {
	if token.IsExported(member) {
		return typeName + "_" + member
	}
	return "_" + typeName + "_" + member
}

// namedStruct reports the type a `struct {` at toks[i] (the STRUCT token)
// declares, with its type parameter list, when it is a named type's struct
// rather than a field's.
func namedStruct(src []byte, toks []tok, i int, inTypeGroup bool) (name, tparams string, names []string, ok bool) {
	k := i - 1
	for k >= 0 && toks[k].tok == token.COMMENT {
		k--
	}
	if k < 0 {
		return
	}
	if toks[k].tok == token.RBRACK {
		d, open := 0, -1
		for m := k; m >= 0; m-- {
			switch toks[m].tok {
			case token.RBRACK:
				d++
			case token.LBRACK:
				if d--; d == 0 {
					open = m
				}
			}
			if open >= 0 {
				break
			}
		}
		if open < 1 {
			return
		}
		tparams = string(src[toks[open].off:toks[k].end()])
		for m := open + 1; m < k; m++ {
			if toks[m].tok == token.IDENT && (toks[m-1].tok == token.LBRACK || toks[m-1].tok == token.COMMA) {
				names = append(names, toks[m].lit)
			}
		}
		k = open - 1
	}
	if k < 1 || toks[k].tok != token.IDENT {
		return "", "", nil, false
	}
	switch p := toks[k-1].tok; {
	case p == token.TYPE, inTypeGroup && (p == token.SEMICOLON || p == token.LPAREN):
		return toks[k].lit, tparams, names, true
	}
	return "", "", nil, false
}

// staticAt reads the static field starting at toks[i] (the `static` word) in
// the struct of typeName. It returns the index of its last token.
func (f *fileState) staticAt(toks []tok, i int, typeName, tparams string, names []string) (*staticDecl, int, string) {
	s := &staticDecl{typeName: typeName, typeParams: tparams, tparams: names, start: toks[i].off,
		typ: span{-1, -1}, init: span{-1, -1}}
	j := i + 1
	if j < len(toks) && toks[j].tok == token.CONST {
		s.isConst = true
		j++
	}
	if j >= len(toks) || toks[j].tok != token.IDENT {
		return nil, i, "expected a name after static"
	}
	s.name, s.nameOff = toks[j].lit, toks[j].off
	j++
	// The field runs to the end of its line, or the struct's closing brace.
	end, d := j, 0
	for ; end < len(toks); end++ {
		t := toks[end].tok
		if d == 0 && (t == token.SEMICOLON || t == token.RBRACE) {
			break
		}
		switch {
		case isOpen(t):
			d++
		case isClose(t):
			d--
		}
	}
	if end == len(toks) {
		return nil, i, "static " + s.name + " isn't closed: its struct ends with the file"
	}
	assign := -1
	d = 0
	for k := j; k < end; k++ {
		switch t := toks[k].tok; {
		case isOpen(t):
			d++
		case isClose(t):
			d--
		case d == 0 && t == token.ASSIGN && assign < 0:
			assign = k
		}
	}
	last := end - 1
	for last >= j && toks[last].tok == token.COMMENT {
		last--
	}
	switch {
	case assign < 0:
		if last < j {
			return nil, i, "static " + s.name + " needs a type or a value"
		}
		s.typ = span{toks[j].off, toks[last].end()}
	default:
		if assign > j {
			s.typ = span{toks[j].off, toks[assign-1].end()}
		}
		if last <= assign {
			return nil, i, "static " + s.name + " = needs a value"
		}
		s.init = span{toks[assign+1].off, toks[last].end()}
	}
	if s.isConst && s.init.start < 0 {
		return nil, i, "static const " + s.name + " needs a value"
	}
	s.end = toks[last].end()
	return s, last, ""
}

// staticFuncAt reads the head of a static method, `func User.Create(` or
// `func Model[Self].Find(`, where toks[i] is FUNC. ok is false for any other
// func.
func (f *fileState) staticFuncAt(toks []tok, i int) (*staticFunc, bool) {
	j := i + 1
	if j+1 >= len(toks) || toks[j].tok != token.IDENT {
		return nil, false
	}
	sf := &staticFunc{typeName: toks[j].lit}
	k := j + 1
	if toks[k].tok == token.LBRACK {
		next, _, ok := matchClose(toks, k)
		if !ok {
			return nil, false
		}
		for m := k + 1; m < next-1; m++ {
			if toks[m].tok == token.IDENT {
				sf.tparams = append(sf.tparams, toks[m].lit)
			} else if toks[m].tok != token.COMMA {
				return nil, false
			}
		}
		k = next
	}
	if k+2 >= len(toks) || toks[k].tok != token.PERIOD || toks[k+1].tok != token.IDENT || toks[k+2].tok != token.LPAREN {
		return nil, false
	}
	sf.head = span{toks[j].off, toks[k+1].end()}
	sf.goName = staticGoName(sf.typeName, toks[k+1].lit)
	return sf, true
}

// lowerStatics comments the static fields out of their structs, renames the
// static methods, and settles what each static's declaration needs;
// renderStatics writes them for the trailer.
func (f *fileState) lowerStatics(toks []tok, bare bool) {
	for _, sf := range f.staticFuncs {
		text := sf.goName
		if len(sf.tparams) > 0 {
			text += "[" + strings.Join(sf.tparams, " any, ") + " any]" // constraints fixed in prepare
		}
		f.add(sf.head.start, sf.head.end, text)
		head := string(f.src[sf.head.start:sf.head.end])
		f.renameDoc(sf.head.start, sf.goName, head, head[strings.LastIndexByte(head, '.')+1:])
	}
	for _, s := range f.statics {
		f.add(s.start, s.end, commentOut(f.src, s.start, s.end))
		if s.typeParams == "" || s.isConst {
			continue
		}
		switch {
		case s.typ.start >= 0:
			s.vt = string(f.src[s.typ.start:s.typ.end])
		default:
			s.vt = literalType(string(f.src[s.init.start:s.init.end]))
		}
		if s.vt == "" {
			s.vt, s.infer = "any", true
		}
		s.rt = f.scannedRuntime(toks)
	}
}

// renderStatics writes the statics' declarations, each initializer with the
// rewrites decided inside it so far.
func (f *fileState) renderStatics() {
	f.static = genWriter{}
	w := &f.static
	directive := func(off int) {
		if !f.bare {
			w.gen(lineDirective(f.at(off)), off)
		}
	}
	for _, s := range f.statics {
		goName := staticGoName(s.typeName, s.name)
		doc := docAbove(f.src, s.start)
		if s.typeParams == "" || s.isConst {
			kw := "var "
			if s.isConst {
				kw = "const "
			}
			w.gen("\n"+doc+kw, s.start)
			directive(s.nameOff)
			w.gen(goName, s.nameOff)
			if s.typ.start >= 0 {
				w.gen(" ", s.typ.start)
				w.copy(string(f.src[s.typ.start:s.typ.end]), s.typ.start)
			}
			if s.init.start >= 0 {
				w.gen(" = ", s.init.start)
				directive(s.init.start)
				f.copyEdited(w, s.init)
			}
			w.gen("\n", s.end)
			continue
		}
		// A generic type's static: one value per instantiation.
		store := "__" + goName + "Statics"
		self := s.typeName + "[" + strings.Join(s.tparams, ", ") + "]"
		w.gen("\nvar "+store+" "+s.rt+".Statics\n\n"+doc, s.start)
		directive(s.nameOff)
		w.gen("func "+goName+s.typeParams+"() *"+s.rt+".Static["+s.vt+"] {\n\treturn "+s.rt+".StaticOf["+self+"](&"+store+", func() "+s.vt+" { return ", s.start)
		if s.init.start >= 0 {
			directive(s.init.start)
			f.copyEdited(w, s.init)
		} else {
			w.gen("*new("+s.vt+")", s.start)
		}
		w.gen(" })\n}\n", s.end)
	}
}

// docAbove is the comment block on the lines right above off in src — a
// static field's doc comment, carried onto its package-level declaration so
// editors show it. Each line comes back left-trimmed, newline-terminated.
func docAbove(src []byte, off int) string {
	end := lineStart(src, off)
	var lines []string
	for end > 0 {
		ls := lineStart(src, end-1)
		line := strings.TrimSpace(string(src[ls:end]))
		if !strings.HasPrefix(line, "//") || strings.HasPrefix(line, "//line ") {
			break
		}
		lines = append(lines, line)
		end = ls
	}
	if len(lines) == 0 {
		return ""
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

// inStaticInit reports whether src offset off is in a static's initializer,
// which is lowered where the trailer declares the static.
func (f *fileState) inStaticInit(start, end int) bool {
	for _, s := range f.statics {
		if s.init.start >= 0 && start >= s.init.start && end <= s.init.end {
			return true
		}
	}
	return false
}

// copyEdited copies src[sp] into w with the rewrites decided inside it.
func (f *fileState) copyEdited(w *genWriter, sp span) {
	last := sp.start
	for _, e := range f.fixed.sorted() {
		if e.start < sp.start || e.end > sp.end {
			continue
		}
		w.copy(string(f.src[last:e.start]), last)
		w.replace(e.text, e.start, e.end-e.start)
		last = e.end
		if !f.bare && len(e.text) != e.end-e.start && e.end < sp.end {
			w.gen(lineDirective(f.at(e.end)), e.end)
		}
	}
	w.copy(string(f.src[last:sp.end]), last)
}

// literalType is the type an initializer spells out, "*Store[Self]" for
// "&Store[Self]{}" or "new(Store[Self])", or "" when it spells none.
func literalType(text string) string {
	x, err := parser.ParseExpr(text)
	if err != nil {
		return ""
	}
	of := func(n ast.Node) string { return text[n.Pos()-1 : n.End()-1] }
	switch x := x.(type) {
	case *ast.CompositeLit:
		return of(x.Type)
	case *ast.UnaryExpr:
		if c, ok := x.X.(*ast.CompositeLit); ok && x.Op == token.AND {
			return "*" + of(c.Type)
		}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			return "*" + of(x.Args[0])
		}
	}
	return ""
}

// inferStatics gives each generic static without a spelled-out type the type
// of its initializer, read from the accessor's placeholder `func() any {
// return init }`, with the type's own parameters (Self) left as they are.
func (e *engine) inferStatics() bool {
	changed := false
	for _, f := range e.vuka {
		for _, s := range f.statics {
			if !s.infer {
				continue
			}
			x := e.staticInit(f, staticGoName(s.typeName, s.name))
			if x == nil {
				continue
			}
			t := e.info.Types[x].Type
			if isInvalid(t) {
				continue // waits for what the initializer uses
			}
			s.infer = false
			if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
				e.errs.add("E0117", f.at(s.nameOff), "can't infer the type of static %s.%s from nil; write it: static %s T = nil", s.typeName, s.name, s.name)
				continue
			}
			s.vt = types.TypeString(types.Default(t), func(p *types.Package) string {
				missing := ""
				if name := e.qualifier(f, &missing)(p); missing == "" {
					return name
				}
				return f.autoImport(p)
			})
			changed = true
		}
	}
	return changed
}

// staticInit is the initializer inside a generic static's accessor:
// func name[…]() … { return rt.StaticOf[…](&store, func() V { return init }) }.
func (e *engine) staticInit(f *fileState, name string) ast.Expr {
	for _, d := range f.ast.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != name || fd.Body == nil || len(fd.Body.List) != 1 {
			continue
		}
		ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return nil
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return nil
		}
		lit, ok := call.Args[1].(*ast.FuncLit)
		if !ok || len(lit.Body.List) != 1 {
			return nil
		}
		if r, ok := lit.Body.List[0].(*ast.ReturnStmt); ok && len(r.Results) == 1 {
			return r.Results[0]
		}
	}
	return nil
}

// commentOut is a replacement for src[start:end] that spans the same lines.
func commentOut(src []byte, start, end int) string {
	n := strings.Count(string(src[start:end]), "\n")
	if restOfLineHasCode(src, end) {
		return "/*" + strings.Repeat("\n", n) + "*/"
	}
	return strings.TrimSuffix(strings.Repeat("//\n", n+1), "\n")
}

// fixStaticFuncs gives each generic static method its type's real type
// parameter list, and reports statics that clash with a method or field.
func (e *engine) fixStaticFuncs() {
	specs := map[string]*ast.TypeSpec{}
	specFile := map[*ast.TypeSpec]*fileState{}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					specs[ts.Name.Name] = ts
					specFile[ts] = f
				}
			}
		}
	}
	members := map[string]bool{} // Type.member for methods and fields
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
				members[recvBase(fd)+"."+fd.Name.Name] = true
			}
		}
	}
	for name, ts := range specs {
		if st, ok := ts.Type.(*ast.StructType); ok {
			for _, field := range st.Fields.List {
				for _, n := range field.Names {
					members[name+"."+n.Name] = true
				}
			}
		}
	}
	own := e.ownNames()
	for _, f := range e.vuka {
		for _, s := range f.statics {
			if members[s.typeName+"."+s.name] {
				e.errs.add("E0116", f.at(s.nameOff), "%s has a field or method %s already; a static can't share its name", s.typeName, s.name)
			} else if n := staticGoName(s.typeName, s.name); own[n] {
				e.errs.add("E0116", f.at(s.nameOff), "static %s.%s is %s in Go, which the package declares already", s.typeName, s.name, n)
			}
		}
		for _, sf := range f.staticFuncs {
			ts := specs[sf.typeName]
			if ts == nil {
				e.errs.add("E0116", f.at(sf.head.start), "%s isn't a type of this package; static methods go with their type", sf.typeName)
				continue
			}
			if member := strings.SplitN(string(f.src[sf.head.start:sf.head.end]), ".", 2)[1]; members[sf.typeName+"."+member] {
				e.errs.add("E0116", f.at(sf.head.start), "%s has a field or method %s already; a static method can't share its name", sf.typeName, member)
			} else if own[sf.goName] {
				e.errs.add("E0116", f.at(sf.head.start), "static %s.%s is %s in Go, which the package declares already", sf.typeName, member, sf.goName)
			}
			if len(sf.tparams) == 0 {
				if ts.TypeParams != nil {
					e.errs.add("E0116", f.at(sf.head.start), "%s is generic; write func %s[…].%s", sf.typeName, sf.typeName, "…")
				}
				continue
			}
			var names []string
			if ts.TypeParams != nil {
				for _, field := range ts.TypeParams.List {
					for _, n := range field.Names {
						names = append(names, n.Name)
					}
				}
			}
			if strings.Join(names, ",") != strings.Join(sf.tparams, ",") {
				e.errs.add("E0116", f.at(sf.head.start), "write the type parameters as %s declares them: [%s]", sf.typeName, strings.Join(names, ", "))
				continue
			}
			text := sf.goName + specFile[ts].nodeText(ts.TypeParams)
			for i, ed := range f.fixed {
				if ed.start == sf.head.start {
					f.fixed[i].text = text
				}
			}
		}
	}
}

// staticRef is what a Type.member selector names: a static of the type or of a
// type it embeds.
type staticRef struct {
	obj    types.Object
	owner  *types.Named // the (instantiated) type declaring it
	getter bool         // obj is a generic function named after the member, called for its value
}

// mirrors reports whether the method m is the static's own as a method: a
// getter static's owner declares a method of its name (orm.Model's Objects
// for orm.Objects[Self]()).
func (r staticRef) mirrors(m types.Object) bool {
	fn, ok := m.(*types.Func)
	if !ok || !r.getter || fn.Name() != r.obj.Name() || fn.Pkg() != r.obj.Pkg() {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	n := namedOf(recv.Type())
	return n != nil && n.Origin() == r.owner.Origin()
}

// findStatic looks for member among t's statics, then those of the types it
// embeds, nearest first, as Go promotes fields.
func (e *engine) findStatic(t *types.Named, member string) (staticRef, bool) {
	level := []*types.Named{t}
	seen := map[*types.Named]bool{}
	for len(level) > 0 {
		var found []staticRef
		var next []*types.Named
		for _, n := range level {
			if seen[n] {
				continue
			}
			seen[n] = true
			if obj, getter := lookupStatic(n, member, e.pkg); obj != nil {
				found = append(found, staticRef{obj, n, getter})
				continue
			}
			if st, ok := n.Underlying().(*types.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					if fv := st.Field(i); fv.Embedded() {
						if en := namedOf(fv.Type()); en != nil {
							next = append(next, en)
						}
					}
				}
			}
		}
		if len(found) == 1 {
			return found[0], true
		}
		if len(found) > 1 {
			return staticRef{}, false
		}
		level = next
	}
	return staticRef{}, false
}

// lookupStatic finds the Go declaration of t's static member: TypeMember
// in t's package (typeMember, inside the package itself). A generic type of
// another package may declare a static as a generic function named after
// the member, of the type's type parameters, called with none of its own
// arguments: Post.Objects, Post embedding orm.Base[Post], is
// orm.Objects[Post]() (getter).
func lookupStatic(t *types.Named, member string, current *types.Package) (obj types.Object, getter bool) {
	tn := t.Origin().Obj()
	pkg := tn.Pkg()
	if pkg == nil || !token.IsExported(member) && pkg != current {
		return nil, false
	}
	if found := pkg.Scope().Lookup(staticGoName(tn.Name(), member)); found != nil {
		return found, false
	}
	if pkg != current {
		if found := pkg.Scope().Lookup(oldStaticGoName(tn.Name(), member)); found != nil {
			return found, false
		}
	}
	if found := enumStatic(t, member); found != nil && (found.Exported() || found.Pkg() == current) {
		return found, false
	}
	if n := t.Origin().TypeParams().Len(); n > 0 && pkg != current {
		if fn, ok := pkg.Scope().Lookup(member).(*types.Func); ok {
			sig := fn.Type().(*types.Signature)
			if sig.TypeParams().Len() == n && sig.Results().Len() == 1 && (sig.Params().Len() == 0 || sig.Params().Len() == 1 && sig.Variadic()) {
				return fn, true
			}
		}
	}
	return nil, false
}

// isStaticAccessor reports whether fn is a generic static's accessor: it
// returns *vuka.Static[V].
func isStaticAccessor(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	p, ok := sig.Results().At(0).Type().(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == RuntimePath && n.Obj().Name() == "Static"
}

// resolveStatics rewrites every Type.member that names a static (of the type,
// or of a type it embeds) to the static's Go declaration.
func (e *engine) resolveStatics(f *fileState) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// A method expression may be a static all the same: another
		// package's model, compiled, embeds orm.Model, whose Objects method
		// mirrors the static Objects of the orm.Base it was written with.
		mexpr := e.info.Selections[sel] != nil && e.info.Selections[sel].Kind() == types.MethodExpr
		if !mexpr && (e.info.Selections[sel] != nil || e.info.Uses[sel.Sel] != nil) {
			return true
		}
		tv, ok := e.info.Types[sel.X]
		if !ok || !tv.IsType() || !f.lowerable(sel.Pos()) {
			return true
		}
		named, ok := types.Unalias(tv.Type).(*types.Named)
		if !ok {
			return true
		}
		start := f.orig(sel.Pos())
		if f.done[start] {
			return true
		}
		ref, ok := e.findStatic(named, sel.Sel.Name)
		if !ok || mexpr && !ref.mirrors(e.info.Selections[sel].Obj()) {
			return true
		}
		if obj, _, _ := types.LookupFieldOrMethod(named, false, e.pkg, sel.Sel.Name); obj != nil && !mexpr {
			if !e.declaredStatic(ref) {
				return true // a field reference: a Go name of the type's and the field's is no static
			}
			f.done[start] = true
			e.errs.add("E0119", f.nodePos(sel.Sel.Pos()), "%s.%s is ambiguous: a field of %s and a static of %s",
				named.Obj().Name(), sel.Sel.Name, named.Obj().Name(), ref.owner.Origin().Obj().Name())
			return true
		}
		missing := ""
		q := e.qualifier(f, &missing)
		text := ""
		if p := ref.owner.Origin().Obj().Pkg(); p != e.pkg {
			if pfx := q(p); missing != "" {
				// Reached through another type: import its package here.
				text = f.autoImport(p) + "."
			} else if pfx != "" {
				text = pfx + "."
			}
		}
		text += ref.obj.Name()
		if tn, ok := ref.obj.(*types.TypeName); ok {
			// An enum's variant: a type, or, without fields, a value too.
			if st, ok := tn.Type().Underlying().(*types.Struct); ok && !typeContext(f, e, sel) {
				if st.NumFields() > 0 {
					f.done[start] = true
					e.errs.add("E0119", f.nodePos(sel.Sel.Pos()), "%s.%s holds data: write %s.%s{…}", named.Obj().Name(), sel.Sel.Name, named.Obj().Name(), sel.Sel.Name)
					return true
				}
				text += "{}"
			}
		}
		if fn, ok := ref.obj.(*types.Func); ok && fn.Type().(*types.Signature).TypeParams().Len() > 0 {
			var args []string
			for i := 0; i < ref.owner.TypeArgs().Len(); i++ {
				s, msg := e.typeText(f, ref.owner.TypeArgs().At(i))
				if msg != "" {
					e.errs.add("E0119", f.nodePos(sel.Pos()), "%s", msg)
					return true
				}
				args = append(args, s)
			}
			text += "[" + strings.Join(args, ", ") + "]"
			switch {
			case ref.getter:
				text += "()"
			case isStaticAccessor(ref.obj):
				text += "().V"
			}
		}
		f.done[start] = true
		f.add(start, f.orig(sel.End()), text)
		f.spell(text, types.ExprString(sel), start)
		e.progress = true
		return false
	})
}

// inferSelf fills in the type argument of an embedded generic type whose first
// type parameter is named Self: `orm.Model` inside `type User struct` becomes
// `orm.Model[User]`, and `orm.Model[int]` becomes `orm.Model[User, int]`.
func (e *engine) inferSelf() {
	local := map[string]*ast.TypeSpec{}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					local[ts.Name.Name] = ts
				}
			}
		}
	}
	var named *types.Named // the last type params found of another package's
	params := func(f *fileState, x ast.Expr) (first string, n int) {
		named = nil
		switch x := x.(type) {
		case *ast.Ident:
			if ts := local[x.Name]; ts != nil && ts.TypeParams != nil {
				for _, field := range ts.TypeParams.List {
					for _, name := range field.Names {
						if n == 0 {
							first = name.Name
						}
						n++
					}
				}
			}
		case *ast.SelectorExpr:
			pkgID, ok := x.X.(*ast.Ident)
			if !ok {
				return
			}
			for _, imp := range f.ast.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if isStd(path) {
					continue // Go's own packages have no Self types
				}
				pkg, err := e.imp.Import(path)
				if err != nil {
					continue
				}
				localName := pkg.Name()
				if imp.Name != nil {
					localName = imp.Name.Name
				}
				if localName != pkgID.Name {
					continue
				}
				if tn, ok := pkg.Scope().Lookup(x.Sel.Name).(*types.TypeName); ok {
					if nt, ok := tn.Type().(*types.Named); ok && nt.TypeParams().Len() > 0 {
						named = nt
						return nt.TypeParams().At(0).Obj().Name(), nt.TypeParams().Len()
					}
				}
			}
		}
		return
	}
	e.genericEmbeds = map[string]bool{}
	for _, f := range e.vuka {
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, s := range g.Specs {
				ts := s.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				self := ts.Name.Name
				if ts.TypeParams != nil {
					var names []string
					for _, field := range ts.TypeParams.List {
						for _, n := range field.Names {
							names = append(names, n.Name)
						}
					}
					self += "[" + strings.Join(names, ", ") + "]"
				}
				for _, field := range st.Fields.List {
					if len(field.Names) > 0 {
						continue
					}
					x := field.Type
					if star, ok := x.(*ast.StarExpr); ok {
						x = star.X
					}
					switch x := x.(type) {
					case *ast.IndexExpr, *ast.IndexListExpr:
						e.genericEmbeds[ts.Name.Name] = true
					case *ast.SelectorExpr:
						// Another package's type may bring statics, unless it is
						// Go's own.
						if id, ok := x.X.(*ast.Ident); ok && !isStd(importPath(f, id.Name)) {
							e.genericEmbeds[ts.Name.Name] = true
						}
					}
					switch x := x.(type) {
					case *ast.Ident, *ast.SelectorExpr:
						first, n := params(f, x)
						if first != "Self" {
							continue
						}
						if n > 1 {
							e.errs.add("E0119", f.nodePos(x.Pos()), "%s needs its other type arguments; Self is filled in: %s[…]", types.ExprString(x), types.ExprString(x))
							continue
						}
						f.insert(f.orig(x.End()), "["+self+"]", 1)
						if sel, ok := x.(*ast.SelectorExpr); ok && named != nil {
							if plain := plainForm(named); plain != "" {
								e.plainEmbeds = append(e.plainEmbeds, plainEmbed{f: f, typ: ts.Name.Name, field: sel.Sel.Name, name: plain,
									start: f.orig(sel.Sel.Pos()), end: f.orig(sel.Sel.End())})
							}
						}
						e.progress = true
					case *ast.IndexExpr, *ast.IndexListExpr:
						base, lbrack, given := indexParts(x)
						if first, n := params(f, base); first == "Self" && given == n-1 {
							f.insert(f.orig(lbrack)+1, self+", ", 1)
							e.progress = true
						}
					}
				}
			}
		}
	}
}

// plainEmbed is an embedded type plain Go writes as another: orm.Base[Post]
// as orm.Model[Post].
type plainEmbed struct {
	f          *fileState
	typ, field string // the struct and the embedded field's name
	name       string // the type plain Go embeds
	start, end int    // the embedded type's name in the source
}

// plainForm is the type plain Go embeds in t's place, the parameter of t's
// marker method vukaPlain, a generic type of t's package taking Self: Model
// for orm.Base, whose func (Base[Self]) vukaPlain(Model[Self]); "" for none.
func plainForm(t *types.Named) string {
	for i := range t.NumMethods() {
		m := t.Method(i)
		if m.Name() != "vukaPlain" {
			continue
		}
		sig := m.Type().(*types.Signature)
		if sig.Params().Len() != 1 {
			return ""
		}
		p, ok := sig.Params().At(0).Type().(*types.Named)
		if !ok || p.Obj().Pkg() != t.Obj().Pkg() || p.TypeArgs().Len() != 1 {
			return ""
		}
		return p.Obj().Name()
	}
	return ""
}

// writePlainEmbeds writes each embedded type as plain Go embeds it, once
// the package type-checked with the Vuka form: unless the code names the
// embedded field, or the struct has a member of the other's name.
func (e *engine) writePlainEmbeds() {
	if e.info == nil || e.pkg == nil {
		return
	}
	for _, p := range e.plainEmbeds {
		tn, ok := e.pkg.Scope().Lookup(p.typ).(*types.TypeName)
		if !ok {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		var field *types.Var
		for i := range st.NumFields() {
			if v := st.Field(i); v.Embedded() && v.Name() == p.field {
				field = v
			}
		}
		if obj, _, _ := types.LookupFieldOrMethod(tn.Type(), true, e.pkg, p.name); field == nil || obj != nil {
			continue
		}
		named := false
		for _, sel := range e.info.Selections {
			named = named || sel.Obj() == field
		}
		for _, use := range e.info.Uses {
			named = named || use == field
		}
		if !named {
			p.f.add(p.start, p.end, p.name)
		}
	}
}

func indexParts(x ast.Expr) (base ast.Expr, lbrack token.Pos, n int) {
	switch x := x.(type) {
	case *ast.IndexExpr:
		return x.X, x.Lbrack, 1
	case *ast.IndexListExpr:
		return x.X, x.Lbrack, len(x.Indices)
	}
	return nil, token.NoPos, 0
}

// mayUseStatics reports whether f might reach a static by Type.member, so the
// package has to be type-checked: a selector on one of the package's types
// naming none of its fields or methods, or pkg.Type.member on a package outside
// the standard library (which has no statics).
func (e *engine) mayUseStatics(f *fileState) bool {
	if e.members == nil {
		e.members = e.memberNames()
	}
	members := e.members
	statics := false
	for _, v := range e.vuka {
		statics = statics || len(v.statics) > 0 || len(v.staticFuncs) > 0 || len(v.enums) > 0
	}
	nonStd := map[string]bool{}
	for _, imp := range f.ast.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if isStd(path) {
			continue
		}
		name := ImportName(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		nonStd[name] = true
	}
	if !statics && len(nonStd) == 0 && len(e.genericEmbeds) == 0 {
		return false
	}
	found := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if found || !ok {
			return !found
		}
		local := func(name string) bool {
			return e.typeNames[name] && !members[name+"."+sel.Sel.Name] && (statics || e.genericEmbeds[name])
		}
		switch x := ast.Unparen(sel.X).(type) {
		case *ast.Ident:
			found = local(x.Name)
		case *ast.IndexExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				found = local(id.Name)
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				found = nonStd[id.Name] && token.IsExported(x.Sel.Name)
			}
		}
		return !found
	})
	return found
}

// isStd reports whether path is a standard library package: its first element
// has no dot.
func isStd(path string) bool {
	return !strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
}

// memberNames are the package's Type.field and Type.method names.
func (e *engine) memberNames() map[string]bool {
	members := map[string]bool{}
	for _, file := range e.files {
		for _, d := range file.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					members[recvBase(d)+"."+d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						if st, ok := ts.Type.(*ast.StructType); ok {
							for _, field := range st.Fields.List {
								for _, n := range field.Names {
									members[ts.Name.Name+"."+n.Name] = true
								}
							}
						}
					}
				}
			}
		}
	}
	return members
}

// selfCalls passes the outer value to methods that take it: a method of a
// generic type whose first parameter is `self *Self` receives the value that
// embeds the type. `u.Save(ctx)` on a User embedding orm.Model[User] becomes
// `u.Save(u, ctx)`, so Save sees the whole User.
func (e *engine) selfCalls(f *fileState) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || !f.lowerable(call.Pos()) {
			return true
		}
		selection := e.info.Selections[sel]
		if selection == nil || selection.Kind() != types.MethodVal {
			return true
		}
		sig := selection.Obj().Type().(*types.Signature)
		if sig.Params().Len() == 0 || sig.Params().At(0).Name() != "self" || len(call.Args) != sig.Params().Len()-1 {
			return true
		}
		at := f.orig(call.Lparen) + 1
		if f.done[at] {
			return true
		}
		f.done[at] = true
		want := sig.Params().At(0).Type()
		self, msg := e.selfValue(f, sel, selection, want)
		if msg != "" {
			e.errs.add("E0119", f.nodePos(sel.Sel.Pos()), "%s", msg)
			return true
		}
		if len(call.Args) > 0 {
			self += ", "
		}
		f.insert(at, self, 1)
		e.progress = true
		return true
	})
}

// selfValue is the expression for the value that embeds the method's type:
// the selector's operand, down the embedding path to the struct holding the
// generic field. Inside a method that has self itself, a call on the embedded
// value passes that self on.
func (e *engine) selfValue(f *fileState, sel *ast.SelectorExpr, s *types.Selection, want types.Type) (string, string) {
	path := s.Index()
	if len(path) == 1 {
		// Called on the embedded value itself: m.Save(ctx) inside a self
		// method passes its self along.
		if sig, _ := e.enclosingFunc(f, sel); sig != nil && sig.Params().Len() > 0 && sig.Params().At(0).Name() == "self" &&
			types.Identical(sig.Params().At(0).Type(), want) {
			return "self", ""
		}
		if outer, ok := sel.X.(*ast.SelectorExpr); ok {
			if tv := e.info.Types[outer.X]; types.Identical(pointerTo(tv.Type), want) || types.Identical(tv.Type, want) {
				return e.addressed(f, outer.X, tv.Type, want)
			}
		}
		return "", "call " + sel.Sel.Name + " on the value that embeds it, so it gets that value as self"
	}
	if !sideEffectFree(sel.X) {
		return "", "assign the value to a variable first: " + sel.Sel.Name + " takes it as self, so it is used twice"
	}
	text := f.nodeText(sel.X)
	t := e.info.Types[sel.X].Type
	for _, idx := range path[:len(path)-2] {
		st, ok := derefType(t).Underlying().(*types.Struct)
		if !ok {
			return "", "can't find the value embedding " + sel.Sel.Name
		}
		field := st.Field(idx)
		text += "." + field.Name()
		t = field.Type()
	}
	return e.addressedText(text, t, want)
}

func (e *engine) addressed(f *fileState, x ast.Expr, t, want types.Type) (string, string) {
	if !sideEffectFree(x) {
		return "", "assign the value to a variable first: it is passed as self too"
	}
	return e.addressedText(f.nodeText(x), t, want)
}

func (e *engine) addressedText(text string, t, want types.Type) (string, string) {
	switch {
	case types.Identical(t, want):
		return text, ""
	case types.Identical(pointerTo(t), want):
		return "&" + text, ""
	}
	return "", "self is a " + types.TypeString(want, nil) + ", but the value is a " + types.TypeString(t, nil)
}

func pointerTo(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return types.NewPointer(t)
}

func derefType(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// sideEffectFree reports whether evaluating x twice is the same as once: names,
// field selections, dereferences and parentheses.
func sideEffectFree(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return sideEffectFree(x.X)
	case *ast.StarExpr:
		return sideEffectFree(x.X)
	case *ast.ParenExpr:
		return sideEffectFree(x.X)
	}
	return false
}

// importPath is the path f imports under name, or "" when it imports none.
func importPath(f *fileState, name string) string {
	for _, imp := range f.ast.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		local := ImportName(path)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if local == name {
			return path
		}
	}
	return ""
}

// autoImport imports p into f under a name of its own, for a static reached
// through another package's type, and returns that name.
func (f *fileState) autoImport(p *types.Package) string {
	return f.importAs(p.Path(), "__"+p.Name())
}

// staticCycles reports statics whose initializers depend on themselves,
// through other statics, variables, functions or methods: Go would report the
// non-generic ones in generated names, and a generic static's lazy initializer
// would never finish.
func (e *engine) staticCycles() {
	decls := map[types.Object]*staticDecl{}
	declFile := map[types.Object]*fileState{}
	for _, f := range e.vuka {
		for _, s := range f.statics {
			if obj := e.pkg.Scope().Lookup(staticGoName(s.typeName, s.name)); obj != nil {
				decls[obj], declFile[obj] = s, f
			}
		}
	}
	if len(decls) == 0 {
		return
	}
	bodies := map[types.Object][]ast.Node{}
	var order []types.Object
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if obj := e.info.Defs[d.Name]; obj != nil && d.Body != nil {
					bodies[obj] = []ast.Node{d.Body}
					order = append(order, obj)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for i, n := range vs.Names {
							obj := e.info.Defs[n]
							if obj == nil {
								continue
							}
							switch {
							case len(vs.Values) == len(vs.Names):
								bodies[obj] = []ast.Node{vs.Values[i]}
							case len(vs.Values) > 0:
								bodies[obj] = []ast.Node{vs.Values[0]}
							}
							order = append(order, obj)
						}
					}
				}
			}
		}
	}
	edges := map[types.Object][]types.Object{}
	for _, obj := range order {
		seen := map[types.Object]bool{}
		for _, n := range bodies[obj] {
			ast.Inspect(n, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if to := e.info.Uses[id]; to != nil {
						if fn, ok := to.(*types.Func); ok {
							to = fn.Origin()
						}
						if _, ok := bodies[to]; ok && !seen[to] {
							seen[to] = true
							edges[obj] = append(edges[obj], to)
						}
					}
				}
				return true
			})
		}
	}
	name := func(obj types.Object) string {
		if s := decls[obj]; s != nil {
			return s.typeName + "." + s.name
		}
		if fn, ok := obj.(*types.Func); ok {
			if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
				if n := namedOf(recv.Type()); n != nil {
					return n.Obj().Name() + "." + fn.Name()
				}
			}
			for _, f := range e.vuka {
				for _, sf := range f.staticFuncs {
					if sf.goName == fn.Name() {
						return string(f.src[sf.head.start:sf.head.end])
					}
				}
			}
		}
		return obj.Name()
	}
	reported := map[types.Object]bool{}
	for _, obj := range order {
		s := decls[obj]
		if s == nil || reported[obj] {
			continue
		}
		// The shortest way back to obj, breadth first.
		prev := map[types.Object]types.Object{}
		queue, found := []types.Object{obj}, false
		for len(queue) > 0 && !found {
			cur := queue[0]
			queue = queue[1:]
			for _, to := range edges[cur] {
				if to == obj {
					prev[obj], found = cur, true
					break
				}
				if _, ok := prev[to]; !ok {
					prev[to] = cur
					queue = append(queue, to)
				}
			}
		}
		if !found {
			continue
		}
		path := []string{name(obj)}
		for cur := prev[obj]; cur != obj; cur = prev[cur] {
			path = append([]string{name(cur)}, path...)
			reported[cur] = true
		}
		path = append([]string{name(obj)}, path...)
		reported[obj] = true
		if len(path) == 2 {
			e.errs.add("E0118", declFile[obj].at(s.nameOff), "static %s is initialized with itself", path[0])
			continue
		}
		e.errs.add("E0118", declFile[obj].at(s.nameOff), "initialization cycle: static %s depends on itself: %s", path[0], strings.Join(path, " → "))
	}
}

// typeContext reports whether sel stands where Go expects a type: a
// composite literal's, a declaration's or a conversion's.
func typeContext(f *fileState, e *engine, sel ast.Expr) bool {
	switch p := f.parents[sel].(type) {
	case *ast.CompositeLit:
		return p.Type == sel
	case *ast.Field, *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.Ellipsis, *ast.TypeSpec, *ast.FuncType:
		return true
	case *ast.ValueSpec:
		return p.Type == sel
	case *ast.TypeAssertExpr:
		return p.Type == sel
	case *ast.CallExpr:
		return p.Fun == sel
	case *ast.CaseClause:
		if b, ok := f.parents[p].(*ast.BlockStmt); ok {
			_, ok := f.parents[b].(*ast.TypeSwitchStmt)
			return ok
		}
	case *ast.IndexExpr:
		return p.Index == sel && e.info.Types[p.X].IsType()
	case *ast.IndexListExpr:
		return e.info.Types[p.X].IsType()
	}
	return false
}

// declaredStatic reports whether ref is certainly a static, where a field
// of the same name is ambiguous: one declared as a static in this
// package's Vuka files, or a generic type's accessor.
func (e *engine) declaredStatic(ref staticRef) bool {
	if isStaticAccessor(ref.obj) {
		return true
	}
	if ref.obj.Pkg() != e.pkg {
		return false
	}
	for _, f := range e.vuka {
		for _, s := range f.statics {
			if staticGoName(s.typeName, s.name) == ref.obj.Name() {
				return true
			}
		}
		for _, sf := range f.staticFuncs {
			if sf.goName == ref.obj.Name() {
				return true
			}
		}
	}
	return false
}

// ownNames are the package-level names the package's source declares, not
// counting what Vuka writes after a file's own lines.
func (e *engine) ownNames() map[string]bool {
	out := map[string]bool{}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			if f.vuka && f.off(d.Pos()) >= f.body {
				continue
			}
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && !f.isStaticFunc(f.orig(d.Name.Pos())) {
					out[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						out[sp.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							out[n.Name] = true
						}
					}
				}
			}
		}
	}
	return out
}

// isStaticFunc reports whether a function's name at src offset off is a
// static method's, func User.Create.
func (f *fileState) isStaticFunc(off int) bool {
	for _, sf := range f.staticFuncs {
		if sf.head.start == off {
			return true
		}
	}
	return false
}
