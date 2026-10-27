package transpile

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A @graphql.Loader function becomes a loader type of its name:
//
//	@graphql.Loader
//	func OrdersByUser(ctx context.Context, userIDs []int64, db *DB) (map[int64][]Order, error) { … }
//
// is, in Go,
//
//	func fetchOrdersByUser(ctx context.Context, userIDs []int64, db *DB) (map[int64][]Order, error) { … }
//
//	type OrdersByUser struct {
//		*graphql.LoaderOf[int64, []Order]
//	}
//
//	func NewOrdersByUser(db *DB) OrdersByUser { … }
//
// and the package's graphql.Register lists it (graphql.LoaderDef), so a
// request makes one for the fields and operations taking it. The type and
// its constructor are written as the package is first read, from the
// syntax: the field methods taking the type type-check from then on.

// gqlLoader is a @graphql.Loader function.
type gqlLoader struct {
	f           *fileState
	a           *Attr
	name, fetch string // the type's name (the function's as written); the function's in Go
	ctor        string
	deps        []string // the dependencies' names
}

// fetchName is the name a loader's function takes in Go.
func fetchName(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	return "fetch" + string(unicode.ToUpper(r)) + name[n:]
}

// ctorName is the name of a loader type's constructor: NewOrdersByUser, or
// newOrdersByUser for an unexported type.
func ctorName(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	if unicode.IsUpper(r) {
		return "New" + name
	}
	return "new" + string(unicode.ToUpper(r)) + name[n:]
}

// gqlLoaders writes the package's loader types; it reports whether it
// wrote any.
func (e *engine) gqlLoaders() bool {
	wrote := false
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if e.gqlAttr(f, a) != "Loader" {
				continue
			}
			l := e.gqlLoader(f, a)
			if l == nil {
				continue
			}
			e.loaders = append(e.loaders, l)
			wrote = true
		}
	}
	return wrote
}

func (e *engine) gqlLoader(f *fileState, a *Attr) *gqlLoader {
	fd, ok := a.decl.(*ast.FuncDecl)
	shape := "func Name(ctx context.Context, keys []K, deps…) (map[K]V, error)"
	switch {
	case !ok:
		e.errs.add("E0805", a.Pos, "@graphql.Loader goes on a function: %s", shape)
		return nil
	case fd.Recv != nil:
		e.errs.add("E0805", a.Pos, "@graphql.Loader goes on a function, not a method").help("declare a function taking what the receiver held as a dependency: %s", shape)
		return nil
	case fd.Type.TypeParams != nil:
		e.errs.add("E0805", a.Pos, "@graphql.Loader goes on a concrete function; %s is generic", fd.Name.Name)
		return nil
	case f.html != nil:
		e.errs.add("E0805", a.Pos, "@graphql.Loader goes on a function of a .vuka file, not a page's script").help("move the function to a .vuka file of the package")
		return nil
	case fd.Body == nil:
		e.errs.add("E0805", a.Pos, "@graphql.Loader goes on a function with a body")
		return nil
	}
	name := srcIdent(f.src, f.orig(fd.Name.Pos()))
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	type param struct {
		name, typ string
		expr      ast.Expr
	}
	var ps []param
	for _, fl := range fd.Type.Params.List {
		if len(fl.Names) == 0 {
			ps = append(ps, param{"", text(fl.Type), fl.Type})
		}
		for _, n := range fl.Names {
			ps = append(ps, param{n.Name, text(fl.Type), fl.Type})
		}
	}
	bad := func(why string) *gqlLoader {
		e.errs.add("E0805", a.Pos, "@graphql.Loader: %s %s; a loader is %s", name, why, shape).
			help("the function is given the keys a request's fields ask for, and returns their values by key")
		return nil
	}
	if len(ps) < 2 {
		return bad("takes no keys")
	}
	at, ok := ps[1].expr.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return bad("takes " + ps[1].typ + " for its keys, not a slice")
	}
	res := fd.Type.Results
	if res == nil || res.NumFields() != 2 {
		return bad("doesn't return a map and an error")
	}
	var rts []ast.Expr
	for _, fl := range res.List {
		for range max(len(fl.Names), 1) {
			rts = append(rts, fl.Type)
		}
	}
	mt, ok := rts[0].(*ast.MapType)
	if id, isErr := rts[1].(*ast.Ident); !ok || !isErr || id.Name != "error" {
		return bad("returns " + text(rts[0]) + ", " + text(rts[1]))
	}
	l := &gqlLoader{f: f, a: a, name: name, fetch: fetchName(name), ctor: ctorName(name)}
	for _, n := range []string{l.fetch, l.ctor} {
		if e.declared[n] {
			e.errs.add("E0803", a.Pos, "@graphql.Loader on %s writes %s, which the package declares already", name, n).
				help("rename %s, or the other %s", name, n)
			return nil
		}
	}
	q, _, _ := strings.Cut(a.Name, ".")
	ctxName, keysName := ps[0].name, ps[1].name
	taken := map[string]bool{}
	var deps, args []string
	for j, p := range ps[2:] {
		n := p.name
		if n == "" || n == "_" {
			n = "dep" + strconv.Itoa(j)
		}
		taken[n] = true
		deps = append(deps, n+" "+p.typ)
		args = append(args, n)
		l.deps = append(l.deps, n)
	}
	free := func(n, alt string) string {
		if n == "" || n == "_" || taken[n] {
			n = alt
		}
		for taken[n] {
			n += "_"
		}
		taken[n] = true
		return n
	}
	ctxName, keysName = free(ctxName, "ctx"), free(keysName, "keys")
	k, v := text(mt.Key), text(mt.Value)
	opts := ""
	if a.Args != "" {
		opts = ", " + q + ".Loader" + a.Args
	}
	var b strings.Builder
	b.WriteString("\n// " + name + " loads " + l.fetch + "'s values by key (@graphql.Loader): the keys\n")
	b.WriteString("// a request's fields ask for meanwhile load in one call of it.\n")
	b.WriteString("type " + name + " struct {\n\t*" + q + ".LoaderOf[" + k + ", " + v + "]\n}\n")
	b.WriteString("\n// " + l.ctor + " makes the loader " + name + ": " + l.fetch + " called with these dependencies.\n")
	b.WriteString("func " + l.ctor + "(" + strings.Join(deps, ", ") + ") " + name + " {\n")
	b.WriteString("\treturn " + name + "{" + q + ".NewLoader(" + strconv.Quote(name) + ", func(" + ctxName + " " + ps[0].typ + ", " + keysName + " []" + k + ") (map[" + k + "]" + v + ", error) {\n")
	b.WriteString("\t\treturn " + l.fetch + "(" + strings.Join(append([]string{ctxName, keysName}, args...), ", ") + ")\n")
	b.WriteString("\t}" + opts + ")}\n}\n")
	f.loaderGen.gen(b.String(), a.start)
	off := f.orig(fd.Name.Pos())
	f.add(off, off+len(name), l.fetch)
	if f.renamed == nil {
		f.renamed = map[int]string{}
	}
	f.renamed[off] = l.fetch // its attributes are the function's, under its name in Go
	for _, at := range f.attrs {
		if at.declOff == a.declOff {
			for i := range at.regs {
				if !at.regs[i].typ && at.regs[i].off == off {
					at.regs[i].name = l.fetch
				}
			}
		}
	}
	e.declared[name], e.typeNames[name] = true, true
	e.declared[l.fetch], e.declared[l.ctor] = true, true
	f.makeTrailer(e.bare)
	return l
}

// loaderType reports whether t is a loader type: a struct embedding a
// *graphql.LoaderOf only, what @graphql.Loader writes. A field method or
// an operation takes one as a dependency.
func loaderType(t types.Type) bool {
	n := named(t)
	if n == nil {
		return false
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok || st.NumFields() != 1 || !st.Field(0).Embedded() {
		return false
	}
	p, ok := st.Field(0).Type().(*types.Pointer)
	if !ok {
		return false
	}
	ln := named(p.Elem())
	return ln != nil && ln.Obj().Name() == "LoaderOf" && ln.Obj().Pkg() != nil && ln.Obj().Pkg().Path() == GraphQLPath
}

// loaderDefs are the LoaderDef literals of the package's loaders.
func (g *gqlGen) loaderDefs() ([]string, bool) {
	q := g.gq + "."
	var out []string
	ok := true
	for _, l := range g.e.loaders {
		g.pos, g.via = l.a.Pos, l.name
		fn, isFn := g.e.pkg.Scope().Lookup(l.fetch).(*types.Func)
		if !isFn {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if !isContext(sig.Params().At(0).Type()) {
			g.fail("E0805", "a loader's function takes the context first", "@graphql.Loader: %s takes %s first, not the context", l.name, g.typ(sig.Params().At(0).Type()))
			ok = false
			continue
		}
		keys := sig.Params().At(1).Type().Underlying().(*types.Slice).Elem()
		m, isMap := sig.Results().At(0).Type().Underlying().(*types.Map)
		if !isMap || !types.Identical(m.Key(), keys) || !types.Comparable(keys) {
			g.fail("E0805", "return the values by the keys the function is given: map[K]V for keys []K", "@graphql.Loader: %s takes keys of %s and returns %s", l.name, g.typ(keys), g.typ(sig.Results().At(0).Type()))
			ok = false
			continue
		}
		var params, args []string
		for i := 2; i < sig.Params().Len(); i++ {
			p := sig.Params().At(i)
			if !g.dependency(p.Type()) || loaderType(p.Type()) {
				g.fail("E0805", "a loader takes the context, its keys and what the app provides: pointers and interfaces", "@graphql.Loader: %s's parameter %s is %s, which isn't a dependency", l.name, p.Name(), g.typ(p.Type()))
				ok = false
				continue
			}
			params = append(params, "{Name: "+strconv.Quote(l.deps[i-2])+", Dep: "+g.reflect()+".TypeFor["+g.typ(p.Type())+"]()}")
			args = append(args, q+"Get["+g.typ(p.Type())+"](c, "+strconv.Itoa(i-2)+")")
		}
		var b strings.Builder
		b.WriteString("{\n\tGo: " + strconv.Quote(l.name) + ", Type: " + g.reflect() + ".TypeFor[" + l.name + "](),\n")
		if len(params) > 0 {
			b.WriteString("\tParams: []" + q + "Param{" + strings.Join(params, ", ") + "},\n")
		}
		b.WriteString("\tNew: func(c *" + q + "Call) any { return " + l.ctor + "(" + strings.Join(args, ", ") + ") },\n}")
		out = append(out, b.String())
	}
	return out, ok
}
