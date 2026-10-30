package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"github.com/vuka-lang/vuka/v2/diag"
)

// A field of an orm model holding a relation resolves through the rows of
// its level: the relation loaded into every parent a level of the query
// reaches at once (graphql.Related with orm.LoadRelation), one query (per
// 1000 keys) rather than one per parent. The relations:
//
//	Author orm.FK[User]      // the row a foreign key holds: User, nullable
//	Tags   orm.M2M[Tag]      // a many-to-many's rows: [Tag!]!
//	Author *User             // a Go model's foreign key
//	Books  []Book            // a Go model's rows held by many
//
// A field tagged graphql:",noload" reads only what the query loaded itself
// (SelectRelated, PrefetchRelated): null or empty otherwise.

// isPage reports whether t is graphql.Page: a parameter of it is the
// connection arguments first, after, last and before.
func isPage(t types.Type) bool {
	n := named(t)
	return n != nil && n.Obj().Name() == "Page" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == GraphQLPath
}

// isConnection reports whether t is a graphql.Connection.
func isConnection(t types.Type) bool {
	n := named(t)
	return n != nil && n.Obj().Name() == "Connection" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == GraphQLPath
}

// ormRel is what an orm relation field's type is: the package declaring
// it (the orm's), the related model, and whether it holds many rows.
func ormRel(t types.Type) (pkg *types.Package, target types.Type, kind string) {
	n := named(t)
	if n == nil || n.Obj().Pkg() == nil || n.TypeArgs().Len() != 1 {
		return nil, nil, ""
	}
	if _, ok := pkgMethod(types.NewPointer(n), n.Obj().Pkg(), "relTarget"); !ok {
		return nil, nil, ""
	}
	switch n.Obj().Name() {
	case "FK", "M2M":
		return n.Obj().Pkg(), n.TypeArgs().At(0), n.Obj().Name()
	}
	return nil, nil, ""
}

// isModel reports whether t is an orm model: a struct embedding the orm's
// Base or Model, whose typed fields have the vukaFields marker.
func isModel(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if _, ok := t.Underlying().(*types.Struct); !ok || named(t) == nil {
		return false
	}
	return hasMarker(t, "vukaFields")
}

// modelPkg is the orm package a model embeds its base from.
func modelPkg(t types.Type) *types.Package {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := range st.NumFields() {
		fv := st.Field(i)
		if !fv.Embedded() {
			continue
		}
		if n := namedOf(fv.Type()); n != nil && n.Obj().Pkg() != nil {
			if _, ok := pkgMethod(types.NewPointer(n), n.Obj().Pkg(), "vukaFields"); ok {
				return n.Obj().Pkg()
			}
		}
		if p := modelPkg(derefType(fv.Type())); p != nil {
			return p
		}
	}
	return nil
}

// relation is the rest of the Member literal of field fv of model t when
// it holds a relation — its type and its resolver — and whether it does;
// "" for a relation reported as a problem.
func (g *gqlGen) relation(t types.Type, fv *types.Var, r string, noload bool) (string, bool) {
	ft := fv.Type()
	pkg, target, kind := ormRel(ft)
	held, loaded := "", "" // what the field holds; when it needs loading
	switch {
	case kind == "FK":
		held = r + "." + fv.Name() + ".Related()"
		loaded = "!" + r + "." + fv.Name() + ".Loaded() && " + r + "." + fv.Name() + ".IsSet()"
	case kind == "M2M":
		held = r + "." + fv.Name() + ".Rows()"
		loaded = "!" + r + "." + fv.Name() + ".Loaded()"
	case !isModel(t):
		return "", false
	default:
		// A Go model's: a pointer to a model, a slice of models.
		el := ft
		many := false
		if s, ok := ft.Underlying().(*types.Slice); ok && named(ft) == nil {
			el, many = s.Elem(), true
		}
		if p, ok := el.(*types.Pointer); ok {
			el = p.Elem()
		} else if !many {
			return "", false
		}
		if !isModel(el) {
			return "", false
		}
		pkg, target = modelPkg(t), el
		if pkg == nil {
			return "", false
		}
		held = r + "." + fv.Name()
		loaded = r + "." + fv.Name() + " == nil"
		kind = "Go"
		if many {
			kind = "GoMany"
		}
	}
	q := g.gq + "."
	var typ string
	var ok bool
	switch kind {
	case "FK":
		typ, ok = g.outNullable(target, false)
	case "M2M":
		typ, ok = g.outType(types.NewSlice(target), false, false)
	case "Go":
		typ, ok = g.outNullable(target, false)
	case "GoMany":
		typ, ok = g.outType(ft, false, false)
	}
	if !ok {
		return "", true
	}
	tt := g.typ(t)
	if noload {
		return "Type: " + typ + ", Get: func(" + r + " *" + tt + ") any { return " + held + " }", true
	}
	load := g.qual(pkg) + ".LoadRelation[" + tt + "]"
	return "Type: " + typ + ",\n\tResolve: func(ctx " + g.ctx() + ".Context, " + r + " *" + tt + ", c *" + q + "Call) (any, error) {\n" +
		"\t\tif " + loaded + " {\n" +
		"\t\t\tif err := " + q + "Related(ctx, " + r + ", " + strconv.Quote(fv.Name()) + ", " + load + "); err != nil {\n" +
		"\t\t\t\treturn nil, err\n\t\t\t}\n\t\t}\n" +
		"\t\treturn " + held + ", nil\n\t}", true
}

// The vet warnings, written once every type of the schema is: a field
// method on a type of a list querying the orm per parent (W0801), a list
// without a limit (W0802).

// warn records a warning at p, a position of this round's syntax.
func (g *gqlGen) warn(p token.Pos, code, help, format string, args ...any) {
	f := g.e.fileOf(p)
	if f == nil || !f.vuka {
		return
	}
	if g.warns == nil {
		g.warns = map[*fileState]ErrorList{}
	}
	l := g.warns[f]
	l.add(diag.Code(code), f.nodePos(p), format, args...).help("%s", help)
	g.warns[f] = l
}

// limited reports whether a field's signature bounds the list it returns:
// a first, last or limit parameter, or a graphql.Page.
func limited(sig *types.Signature) bool {
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		if isPage(p.Type()) {
			return true
		}
		switch p.Name() {
		case "first", "last", "limit":
			return true
		}
	}
	return false
}

// returnsList reports whether a result type is a slice (not a connection).
func returnsList(t types.Type) bool {
	if kind, arg := runtimeType(t); kind == "Result" {
		t = arg
	}
	if el := nullableOf(t); el != nil {
		t = el
	}
	_, ok := t.Underlying().(*types.Slice)
	return ok && named(t) == nil
}

// unbounded warns about an operation or field method returning a list
// without a limit.
func (g *gqlGen) unbounded(p token.Pos, what string, sig *types.Signature) {
	if sig.Results().Len() == 0 || !returnsList(sig.Results().At(0).Type()) || limited(sig) {
		return
	}
	g.warn(p, "W0802", "take first int (or limit) and return at most that many, or take a graphql.Page and return a graphql.Connection",
		"%s returns a list without a limit: a client can ask for all of it", what)
}

// perParent warns about the methods of the types a list reaches that query
// the orm themselves: one query for each item of the list.
func (g *gqlGen) perParent() {
	for tt, fns := range g.methods {
		if !g.listed[tt] {
			continue
		}
		for _, fn := range fns {
			sig := fn.Type().(*types.Signature)
			takesLoader := false
			for i := 0; i < sig.Params().Len(); i++ {
				takesLoader = takesLoader || loaderType(sig.Params().At(i).Type())
			}
			if takesLoader {
				continue
			}
			if fd := g.e.funcDecl(fn); fd != nil && fd.Body != nil && g.e.callsORM(fd.Body) {
				recv := named(derefType(sig.Recv().Type())).Obj().Name()
				g.warn(fd.Name.Pos(), "W0801", "load through a @graphql.Loader the field takes as a parameter: the keys of the whole list in one query",
					"%s.%s queries the database once for each %s of a list (an N+1)", recv, fn.Name(), recv)
			}
		}
	}
}

// funcDecl is the declaration of a function of the package.
func (e *engine) funcDecl(fn *types.Func) *ast.FuncDecl {
	for _, f := range e.vuka {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && e.info.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	return nil
}

// callsORM reports whether body calls a function or method of an orm
// package: one declaring the Base models embed.
func (e *engine) callsORM(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var id *ast.Ident
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			id = fun.Sel
		case *ast.IndexExpr:
			if sel, ok := fun.X.(*ast.SelectorExpr); ok {
				id = sel.Sel
			}
		case *ast.Ident:
			id = fun
		}
		if id == nil {
			return true
		}
		if fn, ok := e.info.Uses[id].(*types.Func); ok && fn.Pkg() != nil && ormPackage(fn.Pkg()) {
			found = true
		}
		return true
	})
	return found
}

// ormPackage reports whether p is the orm (or a stand-in for it): a
// package orm declaring the Base models embed.
func ormPackage(p *types.Package) bool {
	return p.Path() == "github.com/vuka-lang/orm/v2" || p.Name() == "orm" && p.Scope().Lookup("Base") != nil
}
