package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// schemaInfo is how a struct type declares its attributes: a type it embeds
// (orm.Base, ui.Live) has the marker method
//
//	vukaSchema(interface{ Schema(*Schema) })
//
// naming the method the struct gets and its parameter's type. Vuka writes
// that method from the struct's field attributes, the attributes on the
// embedded field (its Model call) and the hook attributes on its methods:
//
//	func (p *Post) Schema(s *orm.Schema) {
//		s.Model(orm.Meta{Table: "posts"})
//		s.Field(&p.Title, orm.Char{Max: 200})
//	}
type schemaInfo struct {
	typeName string
	file     *fileState
	method   string       // Schema, LiveSchema
	param    *types.Named // its parameter's type: orm.Schema
	embed    string       // the embedded field carrying the marker
	recv     string       // the receiver's name
	hooks    []*Attr      // hook attributes on its methods, in source order
}

// findSchemas finds the package's struct types declaring a schema, before
// any decorator is lowered: a hook attribute is no decorator.
func (e *engine) findSchemas() {
	e.schemas = map[string]*schemaInfo{}
	recvs := map[string]string{}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 && len(fd.Recv.List[0].Names) > 0 {
				if n := fd.Recv.List[0].Names[0].Name; n != "_" && recvs[recvBase(fd)] == "" {
					recvs[recvBase(fd)] = n
				}
			}
		}
	}
	for _, f := range e.vuka {
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, sp := range g.Specs {
				ts := sp.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || ts.TypeParams != nil {
					continue
				}
				for _, field := range st.Fields.List {
					if len(field.Names) > 0 {
						continue
					}
					method, param := e.schemaMarker(f, field.Type)
					if param == nil {
						continue
					}
					recv := recvs[ts.Name.Name]
					if recv == "" {
						recv = strings.ToLower(ts.Name.Name[:1])
					}
					e.schemas[ts.Name.Name] = &schemaInfo{typeName: ts.Name.Name, file: f, method: method, param: param,
						embed: embeddedName(field.Type), recv: recv}
					break
				}
			}
		}
	}
}

// schemaMarker reads the marker of an embedded field's type, pkg.T or
// pkg.T[…] (by value or pointer): the method a struct embedding it gets and
// that method's parameter's type.
func (e *engine) schemaMarker(f *fileState, x ast.Expr) (string, *types.Named) {
	if s, ok := x.(*ast.StarExpr); ok {
		x = s.X
	}
	if b, _, n := indexParts(x); n > 0 {
		x = b
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", nil
	}
	pkg := e.importedAs(f, id.Name)
	if pkg == nil {
		return "", nil
	}
	tn, ok := pkg.Scope().Lookup(sel.Sel.Name).(*types.TypeName)
	if !ok {
		return "", nil
	}
	m, ok := pkgMethod(types.NewPointer(tn.Type()), pkg, "vukaSchema")
	if !ok {
		return "", nil
	}
	sig := m.Type().(*types.Signature)
	if sig.Params().Len() != 1 {
		return "", nil
	}
	it, ok := sig.Params().At(0).Type().Underlying().(*types.Interface)
	if !ok || it.NumMethods() != 1 {
		return "", nil
	}
	want := it.Method(0)
	ws := want.Type().(*types.Signature)
	if ws.Params().Len() != 1 || ws.Results().Len() != 0 {
		return "", nil
	}
	p, ok := ws.Params().At(0).Type().(*types.Pointer)
	if !ok {
		return "", nil
	}
	n, ok := p.Elem().(*types.Named)
	if !ok {
		return "", nil
	}
	return want.Name(), n
}

// hookOf reports whether a, an attribute on a method, names a hook of its
// type's schema: @ui.Mount, @ui.Loads("lists") — a method of the schema's
// parameter type (ui.LiveSchema), written with its package. It records it.
func (e *engine) hookOf(f *fileState, a *Attr) bool {
	fd, ok := a.decl.(*ast.FuncDecl)
	if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || a.Field || a.Param {
		return false
	}
	si := e.schemas[recvBase(fd)]
	if si == nil {
		return false
	}
	pkgName, sel, ok := strings.Cut(a.Name, ".")
	if !ok {
		return false
	}
	pkg := e.importedAs(f, pkgName)
	if pkg == nil || pkg.Path() != si.param.Obj().Pkg().Path() {
		return false
	}
	m, ok := pkgMethod(types.NewPointer(si.param), pkg, sel)
	if !ok || !m.Exported() {
		return false
	}
	args := strings.TrimSpace(a.Args)
	if args != "" && !strings.HasPrefix(args, "(") {
		e.errs.add("E0130", a.Pos, "@%s is a hook; write its arguments in parentheses: @%s(…)", a.Name, a.Name)
		return true
	}
	a.kind, a.Decorator, a.bare = attrHook, false, false
	a.hookMethod = srcIdent(f.src, f.orig(fd.Name.Pos()))
	si.hooks = append(si.hooks, a)
	return true
}

// checkSchemas reports a schema type that declares its schema method by
// hand as well as with attributes.
func (e *engine) checkSchemas() {
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || f.vuka && f.off(fd.Pos()) >= f.body {
				continue
			}
			si := e.schemas[recvBase(fd)]
			if si == nil || fd.Name.Name != si.method || !si.declares() {
				continue
			}
			e.errs.add("E0129", e.fset.Position(fd.Name.Pos()), "%s has attributes, which Vuka writes into its %s method, and declares %s itself; move them into the method: %s",
				si.typeName, si.method, si.method, si.example())
		}
	}
}

// declares reports whether the type has attributes for its schema method.
func (si *schemaInfo) declares() bool {
	if len(si.hooks) > 0 {
		return true
	}
	for _, a := range si.file.attrs {
		if a.Field && a.structOf == si.typeName && len(a.fieldNames) > 0 {
			return true
		}
	}
	return false
}

func (si *schemaInfo) paramName() string {
	n := si.param.Obj().Name()
	p := strings.ToLower(n[:1])
	if p == si.recv {
		p = strings.ToLower(n[:1]) + n[1:]
	}
	return p
}

func (si *schemaInfo) example() string {
	return si.paramName() + ".Field(&" + si.recv + ".X, …)"
}

// renderSchema writes the schema method of the type si: its embedded
// field's attributes as a Model call, its fields' as Field calls, then its
// hooks, each bound to the receiver.
func (f *fileState) renderSchema(w *genWriter, si *schemaInfo, attrs []*Attr) {
	at := attrs
	start := 0
	if len(at) > 0 {
		start = at[0].start
	} else if len(si.hooks) > 0 {
		start = f.typeOff(si.typeName)
	}
	q := f.pkgQual(si.param.Obj().Pkg())
	p := si.paramName()
	// The header stands for the line before the first attribute's, so the
	// calls after it follow the fields' lines with no directive between.
	head := start
	if ls := strings.LastIndexByte(string(f.src[:start]), '\n'); ls > 0 && len(attrs) > 0 {
		head = strings.LastIndexByte(string(f.src[:ls]), '\n') + 1
	}
	w.gen("\nfunc ("+si.recv+" *"+si.typeName+") "+si.method+"("+p+" *"+q+si.param.Obj().Name()+") {\n", head)
	var model []*Attr
	byField := map[string][]*Attr{}
	var order []string
	for _, a := range attrs {
		if len(a.fieldNames) == 1 && a.fieldNames[0] == si.embed && f.isEmbed(si.typeName, si.embed) {
			model = append(model, a)
			continue
		}
		for _, n := range a.fieldNames {
			if byField[n] == nil {
				order = append(order, n)
			}
			byField[n] = append(byField[n], a)
		}
	}
	if len(model) > 0 {
		w.gen("\t"+p+".Model(", model[0].start)
		f.writeAttrList(w, model)
		w.gen(")\n", model[len(model)-1].end)
	}
	for _, n := range order {
		as := byField[n]
		w.gen("\t"+p+".Field(&"+si.recv+"."+n+", ", as[0].start)
		f.writeAttrList(w, as)
		w.gen(")\n", as[len(as)-1].end)
	}
	for _, h := range si.hooks {
		args := strings.TrimSpace(h.Args)
		args = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(args, "("), ")"))
		if args != "" {
			args += ", "
		}
		_, sel, _ := strings.Cut(h.Name, ".")
		w.gen("\t"+p+"."+sel+"("+args+si.recv+"."+h.hookMethod+")\n", start)
	}
	w.gen("}\n", start)
}

func (f *fileState) writeAttrList(w *genWriter, as []*Attr) {
	for i, a := range as {
		if i > 0 {
			w.gen(", ", a.start)
		}
		f.writeFieldAttr(w, a)
	}
}

// isEmbed reports whether name is an embedded field of the type.
func (f *fileState) isEmbed(typeName, name string) bool {
	st := f.structType(typeName)
	if st == nil {
		return false
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 && embeddedName(field.Type) == name {
			return true
		}
	}
	return false
}

func (f *fileState) structType(name string) *ast.StructType {
	for _, d := range f.ast.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
			for _, sp := range g.Specs {
				if ts := sp.(*ast.TypeSpec); ts.Name.Name == name {
					st, _ := ts.Type.(*ast.StructType)
					return st
				}
			}
		}
	}
	return nil
}

// typeOff is the src offset of the type's name.
func (f *fileState) typeOff(name string) int {
	for _, d := range f.ast.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
			for _, sp := range g.Specs {
				if ts := sp.(*ast.TypeSpec); ts.Name.Name == name {
					return f.orig(ts.Name.Pos())
				}
			}
		}
	}
	return 0
}

// pkgQual is "name." for p as f imports it, importing it when f doesn't.
func (f *fileState) pkgQual(p *types.Package) string {
	for _, imp := range f.ast.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == p.Path() {
			if imp.Name == nil {
				return p.Name() + "."
			}
			if imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name + "."
		}
	}
	return f.autoImport(p) + "."
}

// composite reports whether name, as f writes it, is a struct, slice, map
// or array type: a bare attribute of it is T{}.
func (e *engine) composite(f *fileState, name string) bool {
	if pkgName, sel, ok := strings.Cut(name, "."); ok {
		pkg := e.importedAs(f, pkgName)
		if pkg == nil {
			return false
		}
		tn, ok := pkg.Scope().Lookup(sel).(*types.TypeName)
		if !ok {
			return false
		}
		switch tn.Type().Underlying().(type) {
		case *types.Struct, *types.Slice, *types.Map, *types.Array:
			return true
		}
		return false
	}
	for _, g := range e.files {
		for _, d := range g.ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, sp := range gd.Specs {
					if ts := sp.(*ast.TypeSpec); ts.Name.Name == name {
						switch ts.Type.(type) {
						case *ast.StructType, *ast.ArrayType, *ast.MapType:
							return true
						}
						return false
					}
				}
			}
		}
	}
	return false
}
