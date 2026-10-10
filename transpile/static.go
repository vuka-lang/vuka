package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// staticDecl is a static field: `static Objects = Manager[User]{}` inside a
// struct type. It leaves the struct and becomes a package-level variable (or
// constant) named after the type, User_Objects; on a generic type, an accessor
// holding one value per instantiation, Model_Objects[Self]().V.
type staticDecl struct {
	typeName   string
	typeParams string   // the type's type parameter list, "[Self any]"; "" when not generic
	tparams    []string // its names
	name       string
	isConst    bool
	typ, init  span // in src; start < 0 when absent
	start, end int  // the whole field, in src
	nameOff    int
}

// staticFunc is a static method: func User.Create(…) or func Model[Self].Find(…).
type staticFunc struct {
	typeName string
	tparams  []string
	head     span // `Model[Self].Find`, replaced by the Go name
	goName   string
}

// staticGoName is the Go name of a static member: Type_Name, with a leading
// underscore when the member is unexported, so it stays private.
func staticGoName(typeName, member string) string {
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
// static methods, and writes the statics' declarations for the trailer.
func (f *fileState) lowerStatics(toks []tok, bare bool) {
	if len(f.statics) == 0 && len(f.staticFuncs) == 0 {
		return
	}
	for _, sf := range f.staticFuncs {
		text := sf.goName
		if len(sf.tparams) > 0 {
			text += "[" + strings.Join(sf.tparams, " any, ") + " any]" // constraints fixed in prepare
		}
		f.add(sf.head.start, sf.head.end, text)
	}
	if len(f.statics) == 0 {
		return
	}
	w := &f.static
	for _, s := range f.statics {
		f.add(s.start, s.end, commentOut(f.src, s.start, s.end))
		goName := staticGoName(s.typeName, s.name)
		directive := func(off int) {
			if !bare {
				w.gen(lineDirective(f.at(off)), off)
			}
		}
		if s.typeParams == "" || s.isConst {
			kw := "var "
			if s.isConst {
				kw = "const "
			}
			w.gen("\n"+kw, s.start)
			directive(s.nameOff)
			w.gen(goName, s.nameOff)
			if s.typ.start >= 0 {
				w.gen(" ", s.typ.start)
				w.copy(string(f.src[s.typ.start:s.typ.end]), s.typ.start)
			}
			if s.init.start >= 0 {
				w.gen(" = ", s.init.start)
				directive(s.init.start)
				w.copy(string(f.src[s.init.start:s.init.end]), s.init.start)
			}
			w.gen("\n", s.end)
			continue
		}
		// A generic type's static: one value per instantiation.
		vt := ""
		switch {
		case s.typ.start >= 0:
			vt = string(f.src[s.typ.start:s.typ.end])
		case s.init.start >= 0:
			vt = compositeType(string(f.src[s.init.start:s.init.end]))
		}
		if vt == "" {
			f.staticErrs = append(f.staticErrs, &Error{Pos: f.at(s.nameOff),
				Msg: "a static of a generic type needs its type: static " + s.name + " T = …"})
			continue
		}
		rt := f.scannedRuntime(toks)
		store := "__" + strings.TrimPrefix(goName, "_") + "_statics"
		self := s.typeName + "[" + strings.Join(s.tparams, ", ") + "]"
		w.gen("\nvar "+store+" "+rt+".Statics\n\n", s.start)
		directive(s.nameOff)
		w.gen("func "+goName+s.typeParams+"() *"+rt+".Static["+vt+"] {\n\treturn "+rt+".StaticOf["+self+"](&"+store+", func() "+vt+" { return ", s.start)
		if s.init.start >= 0 {
			directive(s.init.start)
			w.copy(string(f.src[s.init.start:s.init.end]), s.init.start)
		} else {
			w.gen("*new("+vt+")", s.start)
		}
		w.gen(" })\n}\n", s.end)
	}
}

// compositeType is the type of a composite literal, "Manager[Self]" for
// "Manager[Self]{…}", or "" when text isn't one.
func compositeType(text string) string {
	d := 0
	for i, r := range text {
		switch r {
		case '[', '(':
			d++
		case ']', ')':
			d--
		case '{':
			if d == 0 && i > 0 {
				return strings.TrimSpace(text[:i])
			}
		}
	}
	return ""
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
	for _, f := range e.vuka {
		for _, s := range f.statics {
			if members[s.typeName+"."+s.name] {
				e.errs.add(f.at(s.nameOff), "%s has a field or method %s already; a static can't share its name", s.typeName, s.name)
			}
		}
		for _, sf := range f.staticFuncs {
			ts := specs[sf.typeName]
			if ts == nil {
				e.errs.add(f.at(sf.head.start), "%s isn't a type of this package; static methods go with their type", sf.typeName)
				continue
			}
			if member := strings.SplitN(string(f.src[sf.head.start:sf.head.end]), ".", 2)[1]; members[sf.typeName+"."+member] {
				e.errs.add(f.at(sf.head.start), "%s has a field or method %s already; a static method can't share its name", sf.typeName, member)
			}
			if len(sf.tparams) == 0 {
				if ts.TypeParams != nil {
					e.errs.add(f.at(sf.head.start), "%s is generic; write func %s[…].%s", sf.typeName, sf.typeName, "…")
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
				e.errs.add(f.at(sf.head.start), "write the type parameters as %s declares them: [%s]", sf.typeName, strings.Join(names, ", "))
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
	obj   types.Object
	owner *types.Named // the (instantiated) type declaring it
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
			if obj := lookupStatic(n, member, e.pkg); obj != nil {
				found = append(found, staticRef{obj, n})
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

// lookupStatic finds the Go declaration of t's static member: Type_member in
// t's package (or _Type_member, inside the package itself).
func lookupStatic(t *types.Named, member string, current *types.Package) types.Object {
	obj := t.Origin().Obj()
	if obj.Pkg() == nil {
		return nil
	}
	name := staticGoName(obj.Name(), member)
	if !token.IsExported(member) && obj.Pkg() != current {
		return nil
	}
	return obj.Pkg().Scope().Lookup(name)
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
		if !ok || e.info.Selections[sel] != nil || e.info.Uses[sel.Sel] != nil {
			return true
		}
		tv, ok := e.info.Types[sel.X]
		if !ok || !tv.IsType() || f.off(sel.Pos()) >= f.body {
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
		if !ok {
			return true
		}
		if obj, _, _ := types.LookupFieldOrMethod(named, false, e.pkg, sel.Sel.Name); obj != nil {
			f.done[start] = true
			e.errs.add(f.nodePos(sel.Sel.Pos()), "%s.%s is ambiguous: a field of %s and a static of %s",
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
		if fn, ok := ref.obj.(*types.Func); ok && fn.Type().(*types.Signature).TypeParams().Len() > 0 {
			var args []string
			for i := 0; i < ref.owner.TypeArgs().Len(); i++ {
				s, msg := e.typeText(f, ref.owner.TypeArgs().At(i))
				if msg != "" {
					e.errs.add(f.nodePos(sel.Pos()), "%s", msg)
					return true
				}
				args = append(args, s)
			}
			text += "[" + strings.Join(args, ", ") + "]"
			if isStaticAccessor(ref.obj) {
				text += "().V"
			}
		}
		f.done[start] = true
		f.add(start, f.orig(sel.End()), text)
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
	params := func(f *fileState, x ast.Expr) (first string, n int) {
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
					if named, ok := tn.Type().(*types.Named); ok && named.TypeParams().Len() > 0 {
						return named.TypeParams().At(0).Obj().Name(), named.TypeParams().Len()
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
							e.errs.add(f.nodePos(x.Pos()), "%s needs its other type arguments; Self is filled in: %s[…]", types.ExprString(x), types.ExprString(x))
							continue
						}
						f.insert(f.orig(x.End()), "["+self+"]", 1)
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
		statics = statics || len(v.statics) > 0 || len(v.staticFuncs) > 0
	}
	nonStd := map[string]bool{}
	for _, imp := range f.ast.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if isStd(path) {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
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
		if !ok || f.off(call.Pos()) >= f.body {
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
			e.errs.add(f.nodePos(sel.Sel.Pos()), "%s", msg)
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
		local := path[strings.LastIndex(path, "/")+1:]
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
