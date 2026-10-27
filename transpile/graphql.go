package transpile

import (
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/vuka-lang/vuka/v2/diag"
)

// GraphQLPath is the package whose attributes declare a GraphQL schema:
// @graphql.Query and @graphql.Mutation on a function make it an operation.
const GraphQLPath = "github.com/vuka-lang/web/v2/graphql"

// A package declaring GraphQL operations gets, appended to the Go of its
// first file declaring one, a graphql.Register call describing them and
// every Go type they reach: each operation's parameters and result, each
// struct's fields and methods, each enum's constants, with a resolver that
// calls the function, reads the field or calls the method directly — no
// reflection when a request resolves. Names are left to the runtime, which
// applies the app's Naming and the attributes it finds on each declaration;
// the compiler checks here what it can see: the types, the signatures, and
// the names the package itself gives.

// gqlOp is an operation declared in a file.
type gqlOp struct {
	f        *fileState
	a        *Attr
	fd       *ast.FuncDecl
	name     string // the function's name as written
	mutation bool
}

// graphql writes the package's GraphQL registrations: the operations' and
// their types' (one file's trailer), and each enum constant's attributes
// (its own file's). It reports whether it wrote anything.
func (e *engine) graphql() bool {
	g := &gqlGen{e: e, seen: map[string]bool{}, typeNames: map[string]string{}, docs: e.gqlDocs(), listed: map[string]bool{}, methods: map[string][]*types.Func{}}
	wrote := g.constAttrs()
	var ops []gqlOp
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			kind := e.gqlAttr(f, a)
			if kind != "Query" && kind != "Mutation" && kind != "Subscription" {
				continue
			}
			fd, ok := f.declOf(a).(*ast.FuncDecl)
			switch {
			case !ok:
				e.errs.add("E0805", a.Pos, "@graphql.%s goes on a function", kind)
				continue
			case kind == "Subscription":
				e.errs.add("E0806", a.Pos, "@graphql.Subscription isn't served yet: subscriptions come in a later version").
					help("serve the data as a @graphql.Query for now, or push it to live pages with web.Broadcast")
				continue
			case fd.Recv != nil:
				e.errs.add("E0805", a.Pos, "@graphql.%s goes on a function, not a method: an operation has no receiver", kind).
					help("declare a function taking what the method's receiver held as a dependency: func %s(ctx context.Context, …)", srcIdent(f.src, f.orig(fd.Name.Pos())))
				continue
			case fd.Type.TypeParams != nil:
				e.errs.add("E0805", a.Pos, "@graphql.%s goes on a concrete function; %s is generic", kind, srcIdent(f.src, f.orig(fd.Name.Pos())))
				continue
			case f.html != nil:
				e.errs.add("E0805", a.Pos, "@graphql.%s goes on a function of a .vuka or .go file, not a page's script", kind).
					help("move the function to a .vuka file of the package")
				continue
			}
			name := srcIdent(f.src, f.orig(fd.Name.Pos()))
			if slices.ContainsFunc(ops, func(o gqlOp) bool { return o.fd == fd }) {
				e.errs.add("E0805", a.Pos, "%s carries two operation attributes; give it one", name)
				continue
			}
			ops = append(ops, gqlOp{f: f, a: a, fd: fd, name: name, mutation: kind == "Mutation"})
		}
	}
	if len(ops) == 0 && len(e.loaders) == 0 {
		return wrote
	}
	if len(ops) > 0 {
		g.f, g.off = ops[0].f, ops[0].a.start
	} else {
		g.f, g.off = e.loaders[0].f, e.loaders[0].a.start
	}
	g.gq = g.pkgName(GraphQLPath)
	if g.gq == "" {
		return wrote
	}
	g.checkOpNames(ops)
	var opTexts []string
	for _, op := range ops {
		if t, ok := g.operation(op); ok {
			opTexts = append(opTexts, t)
		}
	}
	loaderTexts, _ := g.loaderDefs()
	if len(*e.errs) > 0 {
		return wrote
	}
	g.perParent()
	e.gqlWarns = g.warns
	q := g.gq + "."
	var b strings.Builder
	b.WriteString("\nfunc init() {\n\t" + q + "Register(" + q + "Package{\n\t\tPath: " + strconv.Quote(e.importPathOr()) + ",\n")
	if len(opTexts) > 0 {
		b.WriteString("\t\tOperations: []" + q + "Operation{\n")
		for _, t := range opTexts {
			b.WriteString(indent(t, 3) + ",\n")
		}
		b.WriteString("\t\t},\n")
	}
	if len(loaderTexts) > 0 {
		b.WriteString("\t\tLoaders: []" + q + "LoaderDef{\n")
		for _, t := range loaderTexts {
			b.WriteString(indent(t, 3) + ",\n")
		}
		b.WriteString("\t\t},\n")
	}
	if len(g.bindings) > 0 {
		b.WriteString("\t\tTypes: []" + q + "Binding{\n")
		for _, t := range g.bindings {
			b.WriteString(indent(t, 3) + ",\n")
		}
		b.WriteString("\t\t},\n")
	}
	b.WriteString("\t})\n}\n")
	g.f.graphqlGen.gen(b.String(), g.off)
	g.f.makeTrailer(e.bare)
	return true
}

func (e *engine) importPathOr() string {
	if e.importPath != "" {
		return e.importPath
	}
	return e.pkg.Name()
}

// declOf is the declaration a annotates in this round's syntax tree (a.decl
// is the first round's).
func (f *fileState) declOf(a *Attr) ast.Decl {
	if a.Field || a.Param {
		return nil
	}
	off := f.cur.fromOrig(a.declOff)
	for _, d := range f.ast.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if f.off(d.Type.Func) == off {
				return d
			}
		case *ast.GenDecl:
			if f.off(d.TokPos) == off {
				return d
			}
		}
	}
	return nil
}

// gqlAttr is the name a of graphql's attribute types a is ("Query",
// "Type", …); "" when it is another.
func (e *engine) gqlAttr(f *fileState, a *Attr) string {
	pkg, sel, ok := strings.Cut(a.Name, ".")
	if !ok {
		return ""
	}
	if p := e.importedAs(f, pkg); p == nil || p.Path() != GraphQLPath {
		return ""
	}
	return sel
}

type gqlGen struct {
	e         *engine
	f         *fileState // the file the registrations go in
	off       int        // where they are attributed in its source
	gq        string     // the graphql package's name there
	bindings  []string
	seen      map[string]bool   // bindings written, by kind and type
	typeNames map[string]string // GraphQL type names → the Go types taking them
	docs      map[types.Object]string
	ctxName   string
	refName   string
	pos       token.Position  // the operation being written, for errors
	via       string          // how it reaches the type being written
	lists     int             // lists around the type being written
	listed    map[string]bool // types a list reaches, by Go type
	methods   map[string][]*types.Func
	warns     map[*fileState]ErrorList
}

// pkgName is the name package path goes by in g.f, imported when it
// isn't.
func (g *gqlGen) pkgName(path string) string {
	p, err := g.e.imp.Import(path)
	if err != nil {
		return ""
	}
	return g.qual(p)
}

func (g *gqlGen) qual(p *types.Package) string {
	missing := ""
	q := g.e.qualifier(g.f, &missing)(p)
	if missing != "" {
		return g.f.autoImport(p)
	}
	return q
}

// typ is t as g.f writes it.
func (g *gqlGen) typ(t types.Type) string { return types.TypeString(t, g.qual) }

func (g *gqlGen) ctx() string {
	if g.ctxName == "" {
		g.ctxName = g.pkgName("context")
	}
	return g.ctxName
}

func (g *gqlGen) reflect() string {
	if g.refName == "" {
		g.refName = g.pkgName("reflect")
	}
	return g.refName
}

func indent(s string, n int) string {
	pad := strings.Repeat("\t", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// fail reports a problem of the operation being written.
func (g *gqlGen) fail(code, help, format string, args ...any) {
	e := g.e.errs.add(diag.Code(code), g.pos, format, args...)
	if help != "" {
		e.help("%s", help)
	}
}

// gqlDocs are the doc comments of the package's declarations, by object:
// its functions', types', fields' and methods'.
func (e *engine) gqlDocs() map[types.Object]string {
	docs := map[types.Object]string{}
	text := func(cg *ast.CommentGroup) string {
		if cg == nil {
			return ""
		}
		return strings.TrimSpace(cg.Text())
	}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if obj := e.info.Defs[d.Name]; obj != nil {
					docs[obj] = text(d.Doc)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						doc := text(sp.Doc)
						if doc == "" && len(d.Specs) == 1 {
							doc = text(d.Doc)
						}
						if obj := e.info.Defs[sp.Name]; obj != nil {
							docs[obj] = doc
						}
						if st, ok := sp.Type.(*ast.StructType); ok {
							for _, fl := range st.Fields.List {
								for _, n := range fl.Names {
									if obj := e.info.Defs[n]; obj != nil {
										docs[obj] = text(fl.Doc)
									}
								}
							}
						}
					case *ast.ValueSpec:
						doc := text(sp.Doc)
						if doc == "" && len(d.Specs) == 1 {
							doc = text(d.Doc)
						}
						for _, n := range sp.Names {
							if obj := e.info.Defs[n]; obj != nil {
								docs[obj] = doc
							}
						}
					}
				}
			}
		}
	}
	return docs
}

// doc is obj's doc comment, quoted, with "Doc: " before it; "" for none.
func (g *gqlGen) doc(obj types.Object) string {
	if d := g.docs[obj]; d != "" {
		return "Doc: " + strconv.Quote(d) + ", "
	}
	return ""
}

// attrsOn are the graphql attributes written on a declaration of the
// package: type name (recv "", fn false), function name (recv "", fn true)
// or method name of type recv. name → its string argument.
func (g *gqlGen) attrsOn(recv, name string, fn bool) map[string]string {
	out := map[string]string{}
	for kind, args := range g.attrArgs(recv, name, fn) {
		if len(args) > 0 {
			out[kind] = args[0]
		} else {
			out[kind] = ""
		}
	}
	return out
}

// attrArgs are the string arguments of every graphql attribute written on
// a declaration (as attrsOn names it), by attribute.
func (g *gqlGen) attrArgs(recv, name string, fn bool) map[string][]string {
	out := map[string][]string{}
	for _, f := range g.e.vuka {
		for _, a := range f.attrs {
			kind := g.e.gqlAttr(f, a)
			if kind == "" {
				continue
			}
			match := false
			switch d := f.declOf(a).(type) {
			case *ast.FuncDecl:
				same := srcIdent(f.src, f.orig(d.Name.Pos())) == name
				if recv != "" {
					match = same && d.Recv != nil && recvBase(d) == recv
				} else {
					match = same && fn && d.Recv == nil
				}
			case *ast.GenDecl:
				if len(d.Specs) == 1 {
					ts, ok := d.Specs[0].(*ast.TypeSpec)
					match = ok && recv == "" && !fn && ts.Name.Name == name
				}
			}
			if match {
				out[kind] = append(out[kind], stringArgs(a.Args)...)
			}
		}
	}
	return out
}

// stringArgs are the string literals among an attribute's arguments:
// ("id", "ownerID") is id and ownerID.
func stringArgs(args string) []string {
	var out []string
	for _, sp := range splitArgs(args) {
		if v, err := strconv.Unquote(strings.TrimSpace(args[sp[0]:sp[1]])); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// idName reports whether a Go name names an id, or ids: id, ID, ids, IDs,
// or one ending in ID, Id, IDs or Ids (UserID, ownerId, tagIDs).
func idName(n string) bool {
	switch n {
	case "id", "ID", "Id", "ids", "IDs", "Ids":
		return true
	}
	for _, suf := range []string{"ID", "Id", "IDs", "Ids"} {
		if len(n) > len(suf) && strings.HasSuffix(n, suf) {
			return true
		}
	}
	return false
}

// intLike reports whether t (a pointer's or a slice's element) is a plain
// integer type: no enum, no type writing itself as text — what an id's name
// makes an ID.
func (g *gqlGen) intLike(t types.Type) bool {
	for {
		switch u := t.(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		}
		if s, ok := t.Underlying().(*types.Slice); ok && named(t) == nil {
			t = s.Elem()
			continue
		}
		break
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && g.enumConsts(t) == nil && !isTextual(t) && !isDuration(t)
}

// idable reports whether t can be an ID: a string or integer type, or a
// slice of one.
func (g *gqlGen) idable(t types.Type) bool {
	if g.intLike(t) {
		return true
	}
	for {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
			continue
		}
		if s, ok := t.Underlying().(*types.Slice); ok && named(t) == nil {
			t = s.Elem()
			continue
		}
		break
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0 && g.enumConsts(t) == nil && !isTextual(t)
}

// isID decides whether a value named name of type t is an ID: forced by
// an override (id: @graphql.AsID, graphql:",id"; keep: @graphql.AsInt,
// @graphql.AsString, graphql:",int", graphql:",string"), else when name
// names an id and t is an integer or a string type.
func (g *gqlGen) isID(name string, t types.Type, id, keep bool) bool {
	switch {
	case keep:
		return false
	case id:
		if !g.idable(t) {
			g.fail("E0805", "only strings and integers are IDs", "%s is %s, which can't be an ID", g.via, g.typ(t))
			return false
		}
		return true
	}
	return idName(name) && g.idable(t)
}

// checkOpNames reports two operations of the package named alike, and
// names reserved by the specification.
func (g *gqlGen) checkOpNames(ops []gqlOp) {
	seen := map[string]gqlOp{}
	for _, op := range ops {
		kind := "Query"
		if op.mutation {
			kind = "Mutation"
		}
		v, _ := stringArg(op.a.Args)
		name := v
		if name == "" {
			name = lowerCamel(op.name)
		}
		if strings.HasPrefix(name, "__") {
			g.e.errs.add("E0804", op.a.Pos, "the operation name %q starts with __, which the GraphQL specification reserves for introspection", name).
				help("name it with @graphql.%s(\"…\")", kind)
			continue
		}
		key := kind + " " + name
		if prev, ok := seen[key]; ok {
			g.e.errs.add("E0803", op.a.Pos, "%s and %s are both %s %s", prev.name, op.name, strings.ToLower(kind), name).
				help("name one with @graphql.%s(\"…\")", kind)
			continue
		}
		seen[key] = op
	}
}

// lowerCamel is a Go name in lower camel case, as graphql.CamelCase has it.
func lowerCamel(s string) string {
	r := []rune(s)
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	switch {
	case i == 0:
		return s
	case i == len(r):
		return strings.ToLower(s)
	case i > 1:
		i--
	}
	for j := range i {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}

// operation is an Operation literal for op.
func (g *gqlGen) operation(op gqlOp) (string, bool) {
	g.pos, g.via = op.a.Pos, op.name
	fn, ok := g.e.pkg.Scope().Lookup(op.name).(*types.Func)
	if !ok {
		return "", false
	}
	sig := fn.Type().(*types.Signature)
	q := g.gq + "."
	var b strings.Builder
	b.WriteString("{\n\tGo: " + strconv.Quote(op.name) + ", Func: " + op.name)
	if op.mutation {
		b.WriteString(", Mutation: true")
	}
	if obj := g.e.info.Defs[op.fd.Name]; obj != nil {
		if d := g.docs[obj]; d != "" {
			b.WriteString(",\n\tDoc: " + strconv.Quote(d))
		}
	}
	b.WriteString(",\n")
	params, args, ok := g.params(sig, op.name, g.attrArgs("", op.name, true))
	if !ok {
		return "", false
	}
	resType, call, ok := g.result(sig, op.name+"("+strings.Join(args, ", ")+")", op.name)
	if !ok {
		return "", false
	}
	g.unbounded(op.fd.Name.Pos(), op.name, sig)
	if len(params) > 0 {
		b.WriteString("\tParams: []" + q + "Param{" + strings.Join(params, ", ") + "},\n")
	}
	b.WriteString("\tType: " + resType + ",\n")
	b.WriteString("\tResolve: func(ctx " + g.ctx() + ".Context, c *" + q + "Call) (any, error) {\n")
	b.WriteString(indent(call, 2) + "\n\t},\n}")
	return b.String(), true
}

// params are the Param literals of sig's parameters, and the arguments a
// resolver calls its function with: the context, each argument's value
// (graphql.Get), each dependency's.
func (g *gqlGen) params(sig *types.Signature, what string, attrs map[string][]string) (params, args []string, ok bool) {
	q := g.gq + "."
	ok = true
	ids, ints := map[string]bool{}, map[string]bool{}
	for _, n := range attrs["AsID"] {
		ids[n] = true
	}
	for _, n := range append(attrs["AsInt"], attrs["AsString"]...) {
		ints[n] = true
	}
	defer func() {
		var names []string
		for i := 0; i < sig.Params().Len(); i++ {
			names = append(names, sig.Params().At(i).Name())
		}
		for _, set := range []struct {
			attr string
			m    map[string]bool
		}{{"AsID", ids}, {"AsInt or AsString", ints}} {
			for _, n := range slices.Sorted(maps.Keys(set.m)) {
				if slices.Contains(names, n) {
					continue
				}
				e := g.e.errs.add("E0805", g.pos, "%s: @graphql.%s(%q) names no parameter of it", what, set.attr, n)
				if s := closestName(n, names); s != "" {
					e.suggest(s)
				}
				ok = false
			}
		}
	}()
	if sig.Variadic() {
		g.fail("E0805", "take a slice instead", "%s is variadic: a GraphQL field's arguments are named values", what)
		return nil, nil, false
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t := p.Type()
		name := p.Name()
		idx := strconv.Itoa(i)
		switch {
		case isContext(t):
			params = append(params, "{Name: "+strconv.Quote(name)+"}")
			args = append(args, "ctx")
		case g.dependency(t):
			params = append(params, "{Name: "+strconv.Quote(name)+", Dep: "+g.reflect()+".TypeFor["+g.typ(t)+"]()}")
			args = append(args, q+"Get["+g.typ(t)+"](c, "+idx+")")
		case isPage(t):
			params = append(params, "{Name: "+strconv.Quote(name)+", Type: "+q+"PageArgs}")
			args = append(args, q+"Get["+g.typ(t)+"](c, "+idx+")")
		default:
			if name == "" || name == "_" {
				g.fail("E0805", "name the parameter: its name is the argument's", "%s's parameter %d (%s) has no name", what, i+1, g.typ(t))
				ok = false
				continue
			}
			g.via = what + "'s parameter " + name
			elem, ptr := t, false
			if pt, isPtr := t.(*types.Pointer); isPtr {
				elem, ptr = pt.Elem(), true
			}
			in, good := g.inType(elem, g.isID(name, elem, ids[name], ints[name]))
			if !good {
				ok = false
				continue
			}
			params = append(params, "{Name: "+strconv.Quote(name)+", Type: "+in+"}")
			if ptr {
				args = append(args, q+"GetPtr["+g.typ(elem)+"](c, "+idx+")")
			} else {
				args = append(args, q+"Get["+g.typ(t)+"](c, "+idx+")")
			}
		}
	}
	return params, args, ok
}

// result is the GraphQL type of sig's result, and the statement returning
// a resolver's value from call: (T), (T, error) or a vuka.Result[T].
func (g *gqlGen) result(sig *types.Signature, call, what string) (typ, stmt string, ok bool) {
	res := sig.Results()
	bad := func() (string, string, bool) {
		g.fail("E0805", "return a value, or a value and an error: func "+what+"(…) (T, error)", "%s returns %s, which isn't a GraphQL field's result", what, res)
		return "", "", false
	}
	var t types.Type
	switch {
	case res.Len() == 1 && !types.Identical(res.At(0).Type(), errorType):
		t = res.At(0).Type()
		if kind, arg := runtimeType(t); kind == "Result" {
			t, call = arg, call+".Get()"
			res = nil
		}
	case res.Len() == 2 && types.Identical(res.At(1).Type(), errorType):
		t = res.At(0).Type()
	default:
		return bad()
	}
	g.via = what + "'s result"
	typ, ok = g.outType(t, false, false)
	if !ok {
		return "", "", false
	}
	q := g.gq + "."
	deref := g.leafPointer(t)
	twoResults := res == nil || res.Len() == 2
	switch {
	case deref && twoResults:
		stmt = "v, err := " + call + "\nreturn " + q + "Deref(v), err"
	case deref:
		stmt = "return " + q + "Deref(" + call + "), nil"
	case twoResults:
		stmt = "return " + call
	default:
		stmt = "return " + call + ", nil"
	}
	return typ, stmt, true
}

// leafPointer reports whether t is a pointer to a scalar or an enum, whose
// value a resolver gives dereferenced (nil for nil).
func (g *gqlGen) leafPointer(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	return ok && g.isLeaf(p.Elem())
}

// dependency reports whether a parameter of type t is filled by the app:
// an interface, or a pointer to anything but a scalar or an enum (a
// service, *http.Request).
func (g *gqlGen) dependency(t types.Type) bool {
	if types.IsInterface(t) || loaderType(t) {
		return true
	}
	p, ok := t.(*types.Pointer)
	return ok && !g.isLeaf(p.Elem())
}

// named is t's named type, alias resolved; nil for an unnamed type.
func named(t types.Type) *types.Named {
	n, _ := types.Unalias(t).(*types.Named)
	return n
}

func isTime(t types.Type) bool {
	n := named(t)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Time"
}

// isLeaf reports whether t is a scalar or an enum of the schema: a basic
// type, time.Time, an enum, a type that reads and writes itself as text.
func (g *gqlGen) isLeaf(t types.Type) bool {
	if isTime(t) || g.enumConsts(t) != nil || isTextual(t) {
		return true
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) != 0 && b.Kind() != types.UnsafePointer
}

// isTextual reports whether t marshals itself as text and *t unmarshals.
func isTextual(t types.Type) bool {
	if named(t) == nil {
		return false
	}
	return methodOf(t, "MarshalText") && methodOf(types.NewPointer(t), "UnmarshalText")
}

func methodOf(t types.Type, name string) bool {
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Obj().Name() == name {
			return true
		}
	}
	return false
}

// enumConsts are the constants of t, an enum: a named integer or string
// type whose package declares constants of it that g.f can name, in the
// order they are declared; nil for any other type.
func (g *gqlGen) enumConsts(t types.Type) []*types.Const {
	n := named(t)
	if n == nil || n.Obj().Pkg() == nil || n.TypeArgs().Len() > 0 || stdlib(n.Obj().Pkg().Path()) {
		return nil
	}
	b, ok := n.Underlying().(*types.Basic)
	if !ok || b.Info()&(types.IsInteger|types.IsString) == 0 {
		return nil
	}
	scope := n.Obj().Pkg().Scope()
	var out []*types.Const
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if ok && types.Identical(c.Type(), t) && (c.Exported() || c.Pkg() == g.e.pkg) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
}

// stdlib reports whether path is a standard library package's: its
// constants (time.Second, os.ModeDir) make no enum.
func stdlib(path string) bool {
	if v, ok := stdlibs.Load(path); ok {
		return v.(bool)
	}
	first, _, _ := strings.Cut(path, "/")
	std := false
	if !strings.Contains(first, ".") && path != "main" {
		p, err := build.Default.Import(path, "", build.FindOnly)
		std = err == nil && p.Goroot
	}
	stdlibs.Store(path, std)
	return std
}

var stdlibs sync.Map // import path → whether it is the standard library's

func isDuration(t types.Type) bool {
	n := named(t)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Duration"
}

// leaf is the scalar or enum t is, as an expression; id makes a string or
// integer an ID.
func (g *gqlGen) leaf(t types.Type, id bool) (string, bool) {
	q := g.gq + "."
	tt := func() string { return g.typ(t) }
	switch {
	case isTime(t):
		return q + "DateTime", true
	case isDuration(t):
		g.unsupported(t, "GraphQL has no duration; give a number of seconds (an int or a float64)")
		return "", false
	case !id && g.enumConsts(t) != nil:
		g.enumBinding(t)
		return q + "EnumOf[" + tt() + "]()", true
	case !id && isTextual(t):
		g.claim(typeNameOf(t), "scalar "+tt(), t)
		return q + "TextScalar[" + tt() + "]()", true
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return "", false
	}
	exact := func(k types.BasicKind) bool {
		bt, ok := t.(*types.Basic)
		return ok && bt.Kind() == k
	}
	info := b.Info()
	switch {
	case info&types.IsBoolean != 0 && !id:
		if exact(types.Bool) {
			return q + "Boolean", true
		}
		return q + "BooleanOf[" + tt() + "]()", true
	case info&types.IsString != 0:
		switch {
		case id && exact(types.String):
			return q + "ID", true
		case id:
			return q + "IDOf[" + tt() + "]()", true
		case exact(types.String):
			return q + "String", true
		}
		return q + "StringOf[" + tt() + "]()", true
	case info&types.IsInteger != 0 && b.Kind() != types.Uintptr:
		switch {
		case id:
			return q + "IntIDOf[" + tt() + "]()", true
		case exact(types.Int):
			return q + "Int", true
		}
		return q + "IntOf[" + tt() + "]()", true
	case info&types.IsFloat != 0 && !id:
		if exact(types.Float64) {
			return q + "Float", true
		}
		return q + "FloatOf[" + tt() + "]()", true
	}
	return "", false
}

// unsupported reports a type GraphQL has none for.
func (g *gqlGen) unsupported(t types.Type, why string) {
	if why == "" {
		why = "GraphQL has no type for it"
	}
	g.fail("E0802", "leave a struct field out with a graphql:\"-\" tag; give the value a struct, a slice, a scalar or an enum type", "%s is %s: %s", g.via, g.typ(t), why)
	if invalidThrough(t, g.e.brokenPaths()) {
		(*g.e.errs)[len(*g.e.errs)-1].knockOn = true
	}
}

// nonNull is "&graphql.NonNull{Of: x}".
func (g *gqlGen) nonNull(x string) string { return "&" + g.gq + ".NonNull{Of: " + x + "}" }

// outType is the GraphQL type of a result or field of Go type t: non-null
// unless t is a pointer, or nullable is set (omitempty).
func (g *gqlGen) outType(t types.Type, nullable, id bool) (string, bool) {
	if p, ok := t.(*types.Pointer); ok {
		switch p.Elem().Underlying().(type) {
		case *types.Pointer, *types.Slice:
			g.unsupported(t, "a pointer to a pointer or a slice; use the slice itself, a nil slice is an empty list")
			return "", false
		}
		return g.outNullable(p.Elem(), id)
	}
	x, ok := g.outNullable(t, id)
	if !ok || nullable {
		return x, ok
	}
	return g.nonNull(x), true
}

// outNullable is the nullable GraphQL type of Go type t; id makes a string
// or an integer an ID.
func (g *gqlGen) outNullable(t types.Type, id bool) (string, bool) {
	if g.isLeaf(t) {
		x, ok := g.leaf(t, id)
		if !ok {
			g.unsupported(t, "")
		}
		return x, ok
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		g.lists++
		defer func() { g.lists-- }()
		el := u.Elem()
		var inner string
		ok := false
		if p, isPtr := el.(*types.Pointer); isPtr {
			if g.isLeaf(p.Elem()) {
				g.unsupported(t, "a list of pointers to "+g.typ(p.Elem())+"; use []"+g.typ(p.Elem()))
				return "", false
			}
			inner, ok = g.outNullable(p.Elem(), false)
		} else {
			if b, isByte := el.Underlying().(*types.Basic); isByte && b.Kind() == types.Byte && named(el) == nil {
				g.unsupported(t, "bytes have no GraphQL type; give the field a string")
				return "", false
			}
			inner, ok = g.outType(el, false, id)
		}
		if !ok {
			return "", false
		}
		return g.gq + ".ListOf[" + g.typ(el) + "](" + inner + ")", true
	case *types.Struct:
		n := named(t)
		if n == nil {
			g.unsupported(t, "an unnamed struct; declare it as a type")
			return "", false
		}
		if g.lists > 0 {
			g.listed[g.typ(t)] = true
		}
		g.structBinding(t)
		return g.gq + ".ObjectOf[" + g.typ(t) + "]()", true
	}
	g.unsupported(t, "")
	return "", false
}

// inType is the GraphQL type of an argument or input field of Go type t
// (a pointer's element): always nullable, null and absent giving t's zero
// value (nil for a pointer); id makes a string or an integer (a list's
// elements) an ID.
func (g *gqlGen) inType(t types.Type, id bool) (string, bool) {
	if g.isLeaf(t) {
		x, ok := g.leaf(t, id)
		if !ok {
			g.unsupported(t, "")
		}
		return x, ok
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		el := u.Elem()
		if _, isPtr := el.(*types.Pointer); isPtr {
			g.unsupported(t, "a list of pointers; use []"+g.typ(el.(*types.Pointer).Elem()))
			return "", false
		}
		inner, ok := g.inType(el, id)
		if !ok {
			return "", false
		}
		return g.gq + ".ListOf[" + g.typ(el) + "](" + inner + ")", true
	case *types.Struct:
		if named(t) == nil {
			g.unsupported(t, "an unnamed struct; declare it as a type")
			return "", false
		}
		g.inputBinding(t)
		return g.gq + ".InputOf[" + g.typ(t) + "]()", true
	}
	g.unsupported(t, "")
	return "", false
}

// typeNameOf is the GraphQL name a named type takes by default: its name;
// an instantiation's type arguments' names before its own (Page[User] is
// UserPage).
func typeNameOf(t types.Type) string {
	n := named(t)
	if n == nil {
		return ""
	}
	name := n.Obj().Name()
	var pre strings.Builder
	for i := 0; i < n.TypeArgs().Len(); i++ {
		arg := n.TypeArgs().At(i)
		for {
			switch a := arg.(type) {
			case *types.Pointer:
				arg = a.Elem()
				continue
			case *types.Slice:
				arg = a.Elem()
				continue
			}
			break
		}
		an := typeNameOf(arg)
		if an == "" {
			an = types.TypeString(arg, func(*types.Package) string { return "" })
		}
		pre.WriteString(strings.ToUpper(an[:1]) + an[1:])
	}
	return pre.String() + name
}

// claim takes a GraphQL type name for a Go type, reporting a name another
// Go type took. An override the package writes (@graphql.Type, @graphql.Input)
// counts; one another package writes is checked when the app builds.
func (g *gqlGen) claim(name, what string, t types.Type) {
	if n := named(t); n != nil && n.Obj().Pkg() == g.e.pkg {
		attrs := g.attrsOn("", n.Obj().Name(), false)
		if v := attrs["Type"]; v != "" {
			name = v
		}
		if v := attrs["Input"]; v != "" && strings.HasPrefix(what, "input ") {
			name = v
		}
	}
	if strings.HasPrefix(name, "__") {
		g.fail("E0804", "rename it with @graphql.Type(\"…\")", "%s takes the GraphQL name %q, which starts with __: the specification reserves it for introspection", what, name)
		return
	}
	if prev, ok := g.typeNames[name]; ok && prev != what {
		g.fail("E0803", "rename one with @graphql.Type(\"…\") on its type, or @graphql.Input(\"…\") for an input", "two types take the GraphQL name %q: %s and %s", name, prev, what)
		return
	}
	g.typeNames[name] = what
}

// gqlFields are a struct's exported fields, those of embedded structs in
// their place (as Go promotes them), with their index paths.
func gqlFields(st *types.Struct) []*types.Var {
	var out []*types.Var
	for i := 0; i < st.NumFields(); i++ {
		fv := st.Field(i)
		tag := reflect.StructTag(st.Tag(i)).Get("graphql")
		if tag == "-" {
			continue
		}
		if fv.Embedded() && tag == "" {
			et := fv.Type()
			if p, ok := et.(*types.Pointer); ok {
				et = p.Elem()
			}
			if est, ok := et.Underlying().(*types.Struct); ok && !isTime(et) {
				out = append(out, gqlFields(est)...)
				continue
			}
		}
		if fv.Exported() {
			out = append(out, fv)
		}
	}
	return out
}

// fieldTag is a struct field's graphql:"name,id,omitempty,int,string,noload"
// tag, found through the structs embedding it: asInt is set by int and
// string, which keep an id-named field an Int or a String.
func fieldTag(st *types.Struct, fv *types.Var) (name string, id, omit, asInt, noload bool) {
	var tag string
	var find func(st *types.Struct) bool
	find = func(st *types.Struct) bool {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i) == fv {
				tag = reflect.StructTag(st.Tag(i)).Get("graphql")
				return true
			}
			et := st.Field(i).Type()
			if p, ok := et.(*types.Pointer); ok {
				et = p.Elem()
			}
			if est, ok := et.Underlying().(*types.Struct); ok && st.Field(i).Embedded() && find(est) {
				return true
			}
		}
		return false
	}
	find(st)
	name, opts, _ := strings.Cut(tag, ",")
	for _, o := range strings.Split(opts, ",") {
		switch o {
		case "id":
			id = true
		case "omitempty":
			omit = true
		case "int", "string":
			asInt = true
		case "noload":
			noload = true
		}
	}
	return name, id, omit, asInt, noload
}

// methodNames are methods never taken as fields: the ones Go's standard
// interfaces and forms give a type.
var skipMethods = map[string]bool{
	"String": true, "GoString": true, "Error": true, "Format": true, "Unwrap": true, "Is": true, "As": true,
	"MarshalText": true, "UnmarshalText": true, "MarshalJSON": true, "UnmarshalJSON": true,
	"MarshalBinary": true, "UnmarshalBinary": true, "AppendText": true, "AppendBinary": true,
	"Scan": true, "Value": true, "Valid": true, "Clean": true, "Render": true,
}

// recvName is the name a struct's resolvers call their value: its type's
// initial, lowered.
func recvName(t types.Type) string {
	name := "v"
	if n := named(t); n != nil {
		name = strings.ToLower(n.Obj().Name()[:1])
	}
	if name == "c" || !token.IsIdentifier(name) || token.IsKeyword(name) {
		name = "v"
	}
	return name
}

func (g *gqlGen) structBinding(t types.Type) {
	tt := g.typ(t)
	key := "object " + tt
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	slot := g.slot()
	g.claim(typeNameOf(t), "object "+tt, t)
	n := named(t)
	st := t.Underlying().(*types.Struct)
	q := g.gq + "."
	r := recvName(t)
	saved := g.via
	defer func() { g.via = saved }()
	var members []string
	names := map[string]string{}
	taken := func(name, what string) bool {
		if prev, ok := names[name]; ok {
			g.fail("E0803", "rename one with a graphql:\"name\" tag, or @graphql.Field(\"…\") on a method", "%s and %s are both field %q of %s", prev, what, name, tt)
			return true
		}
		names[name] = what
		return false
	}
	samePkg := n.Obj().Pkg() == g.e.pkg
	for _, fv := range gqlFields(st) {
		name, id, omit, asInt, noload := fieldTag(st, fv)
		g.via = tt + "." + fv.Name()
		if rel, ok := g.relation(t, fv, r, noload); ok {
			if rel == "" {
				continue
			}
			wire := name
			if wire == "" {
				wire = lowerCamel(fv.Name())
			}
			if taken(wire, fv.Name()) {
				continue
			}
			m := "{Go: " + strconv.Quote(fv.Name())
			if name != "" {
				m += ", Name: " + strconv.Quote(name)
			}
			if d := g.docs[fv]; d != "" {
				m += ", Doc: " + strconv.Quote(d)
			}
			members = append(members, m+", "+rel+"}")
			continue
		}
		id = g.isID(fv.Name(), fv.Type(), id, asInt)
		wire := name
		if wire == "" {
			wire = lowerCamel(fv.Name())
		}
		if strings.HasPrefix(wire, "__") {
			g.fail("E0804", "rename it with a graphql:\"name\" tag", "%s takes the GraphQL name %q, which starts with __: the specification reserves it for introspection", g.via, wire)
			continue
		}
		if taken(wire, fv.Name()) {
			continue
		}
		ft := fv.Type()
		var typ string
		var ok bool
		get := r + "." + fv.Name()
		switch {
		case g.leafPointer(ft):
			typ, ok = g.outNullable(ft.(*types.Pointer).Elem(), id)
			get = q + "Deref(" + get + ")"
		case isPointer(ft):
			typ, ok = g.outType(ft, false, id)
		default:
			typ, ok = g.outNullable(ft, id)
			if ok && !omit {
				typ = g.nonNull(typ)
			}
			if omit {
				if _, isSlice := ft.Underlying().(*types.Slice); isSlice {
					get = q + "OmitEmpty(" + get + ")"
				} else if types.Comparable(ft) {
					get = q + "OmitZero(" + get + ")"
				}
			}
		}
		if !ok {
			continue
		}
		m := "{Go: " + strconv.Quote(fv.Name())
		if name != "" {
			m += ", Name: " + strconv.Quote(name)
		}
		if d := g.docs[fv]; d != "" {
			m += ", Doc: " + strconv.Quote(d)
		}
		m += ", Type: " + typ + ", Get: func(" + r + " *" + tt + ") any { return " + get + " }}"
		members = append(members, m)
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	for i := 0; i < ms.Len(); i++ {
		fn, ok := ms.At(i).Obj().(*types.Func)
		if !ok || !fn.Exported() || skipMethods[fn.Name()] || strings.HasPrefix(fn.Name(), "Clean") || fn.Pkg() != n.Obj().Pkg() {
			continue
		}
		attrs := map[string]string{}
		if samePkg {
			attrs = g.attrsOn(n.Obj().Name(), fn.Name(), false)
		}
		_, marked := attrs["Field"]
		sig := fn.Type().(*types.Signature)
		g.via = tt + "." + fn.Name()
		if !marked && !g.fits(sig) {
			continue
		}
		wire := attrs["Field"]
		if wire == "" {
			wire = lowerCamel(fn.Name())
		}
		if strings.HasPrefix(wire, "__") {
			g.fail("E0804", "rename it with @graphql.Field(\"…\")", "%s takes the GraphQL name %q, which starts with __: the specification reserves it for introspection", g.via, wire)
			continue
		}
		if taken(wire, fn.Name()+"()") {
			continue
		}
		var pattrs map[string][]string
		if samePkg {
			pattrs = g.attrArgs(n.Obj().Name(), fn.Name(), false)
		}
		params, args, ok := g.params(sig, tt+"."+fn.Name(), pattrs)
		if !ok {
			continue
		}
		typ, stmt, ok := g.result(sig, r+"."+fn.Name()+"("+strings.Join(args, ", ")+")", tt+"."+fn.Name())
		if !ok {
			continue
		}
		g.methods[tt] = append(g.methods[tt], fn)
		g.unbounded(fn.Pos(), tt+"."+fn.Name(), sig)
		m := "{Go: " + strconv.Quote(fn.Name())
		if d := g.docs[fn]; d != "" {
			m += ", Doc: " + strconv.Quote(d)
		}
		m += ", Type: " + typ + ",\n"
		if len(params) > 0 {
			m += "\tParams: []" + q + "Param{" + strings.Join(params, ", ") + "},\n"
		}
		m += "\tResolve: func(ctx " + g.ctx() + ".Context, " + r + " *" + tt + ", c *" + q + "Call) (any, error) {\n" + indent(stmt, 2) + "\n\t}}"
		members = append(members, m)
	}
	var b strings.Builder
	b.WriteString(q + "Struct[" + tt + "]{" + g.doc(n.Obj()) + "Fields: []" + q + "Member[" + tt + "]{\n")
	for _, m := range members {
		b.WriteString(indent(m, 1) + ",\n")
	}
	b.WriteString("}}")
	g.bindings[slot] = b.String()
}

// slot reserves the next binding's place: a type comes before those its
// fields reach.
func (g *gqlGen) slot() int {
	g.bindings = append(g.bindings, "")
	return len(g.bindings) - 1
}

func isPointer(t types.Type) bool { _, ok := t.(*types.Pointer); return ok }

// fits reports whether a method's signature makes a field: a value, or a
// value and an error, from the context, arguments (scalars, enums, input
// structs and lists of them) and dependencies. Methods that don't fit are
// left out, unless @graphql.Field says they are fields.
func (g *gqlGen) fits(sig *types.Signature) bool {
	res := sig.Results()
	switch {
	case res.Len() == 1 && !types.Identical(res.At(0).Type(), errorType):
	case res.Len() == 2 && types.Identical(res.At(1).Type(), errorType):
	default:
		return false
	}
	if sig.Variadic() || !g.outFits(res.At(0).Type(), 0) {
		return false
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t := p.Type()
		if isContext(t) || g.dependency(t) {
			continue
		}
		if p.Name() == "" || p.Name() == "_" || !g.inFits(t, 0) {
			return false
		}
	}
	return true
}

func (g *gqlGen) outFits(t types.Type, depth int) bool {
	if depth > 4 {
		return false
	}
	if kind, arg := runtimeType(t); kind == "Result" {
		t = arg
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if g.isLeaf(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return g.outFits(u.Elem(), depth+1)
	case *types.Struct:
		return named(t) != nil
	}
	return false
}

func (g *gqlGen) inFits(t types.Type, depth int) bool {
	if depth > 4 {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if g.isLeaf(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return g.inFits(u.Elem(), depth+1)
	case *types.Struct:
		return named(t) != nil
	}
	return false
}

func (g *gqlGen) inputBinding(t types.Type) {
	tt := g.typ(t)
	key := "input " + tt
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	slot := g.slot()
	g.claim(typeNameOf(t), "input "+tt, t)
	st := t.Underlying().(*types.Struct)
	q := g.gq + "."
	saved := g.via
	defer func() { g.via = saved }()
	var members []string
	names := map[string]string{}
	for _, fv := range gqlFields(st) {
		name, id, _, asInt, _ := fieldTag(st, fv)
		g.via = tt + "." + fv.Name()
		wire := name
		if wire == "" {
			wire = lowerCamel(fv.Name())
		}
		if prev, dup := names[wire]; dup {
			g.fail("E0803", "rename one with a graphql:\"name\" tag", "%s and %s are both field %q of input %s", prev, fv.Name(), wire, tt)
			continue
		}
		names[wire] = fv.Name()
		ft := fv.Type()
		elem, ptr := ft, false
		if p, isPtr := ft.(*types.Pointer); isPtr {
			elem, ptr = p.Elem(), true
		}
		typ, ok := g.inType(elem, g.isID(fv.Name(), elem, id, asInt))
		if !ok {
			continue
		}
		set := q + "As[" + g.typ(ft) + "](v)"
		if ptr {
			set = q + "Ptr[" + g.typ(elem) + "](v)"
		}
		m := "{Go: " + strconv.Quote(fv.Name())
		if name != "" {
			m += ", Name: " + strconv.Quote(name)
		}
		if d := g.docs[fv]; d != "" {
			m += ", Doc: " + strconv.Quote(d)
		}
		m += ", Type: " + typ + ", Set: func(f *" + tt + ", v any) { f." + fv.Name() + " = " + set + " }}"
		members = append(members, m)
	}
	var b strings.Builder
	b.WriteString(q + "FormInput[" + tt + "]{" + g.doc(named(t).Obj()) + "Fields: []" + q + "InputMember[" + tt + "]{\n")
	for _, m := range members {
		b.WriteString(indent(m, 1) + ",\n")
	}
	b.WriteString("}}")
	g.bindings[slot] = b.String()
}

func (g *gqlGen) enumBinding(t types.Type) {
	tt := g.typ(t)
	key := "enum " + tt
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	slot := g.slot()
	g.claim(typeNameOf(t), "enum "+tt, t)
	q := g.gq + "."
	pkg := named(t).Obj().Pkg()
	prefix := ""
	if pkg != g.e.pkg {
		prefix = g.qual(pkg) + "."
	}
	var b strings.Builder
	b.WriteString(q + "Consts[" + tt + "]{" + g.doc(named(t).Obj()) + "Values: []" + q + "Const[" + tt + "]{\n")
	for _, c := range g.enumConsts(t) {
		b.WriteString("\t{Go: " + strconv.Quote(c.Name()) + ", " + g.doc(c) + "Value: " + prefix + c.Name() + "},\n")
	}
	b.WriteString("}}")
	g.bindings[slot] = b.String()
}

// constAttrs registers the graphql attributes written on the package's
// constants (@graphql.Value, @graphql.Deprecated) with graphql.RegisterValue,
// in each constant's own file. It reports whether it wrote any.
func (g *gqlGen) constAttrs() bool {
	wrote := false
	for _, f := range g.e.vuka {
		var names []string
		attrs := map[string][]string{}
		pkg := map[string]string{}
		first := -1
		for _, a := range f.attrs {
			kind := g.e.gqlAttr(f, a)
			gd, ok := f.declOf(a).(*ast.GenDecl)
			if kind == "" || !ok || gd.Tok != token.CONST {
				continue
			}
			if kind != "Value" && kind != "Deprecated" {
				g.e.errs.add("E0805", a.Pos, "@graphql.%s doesn't go on a constant", kind)
				continue
			}
			if len(gd.Specs) != 1 || len(gd.Specs[0].(*ast.ValueSpec).Names) != 1 {
				g.e.errs.add("E0805", a.Pos, "@graphql.%s goes on one constant: declare it on its own, const Name Type = value", kind)
				continue
			}
			if first < 0 {
				first = a.start
			}
			name := srcIdent(f.src, f.orig(gd.Specs[0].(*ast.ValueSpec).Names[0].Pos()))
			if _, ok := attrs[name]; !ok {
				names = append(names, name)
			}
			attrs[name] = append(attrs[name], a.Name+a.Args)
			pkg[name], _, _ = strings.Cut(a.Name, ".")
		}
		if len(names) == 0 {
			continue
		}
		var b strings.Builder
		b.WriteString("\nfunc init() {\n")
		for _, n := range names {
			b.WriteString("\t" + pkg[n] + ".RegisterValue(" + n + ", " + strings.Join(attrs[n], ", ") + ")\n")
		}
		b.WriteString("}\n")
		f.graphqlGen.gen(b.String(), first)
		f.makeTrailer(g.e.bare)
		wrote = true
	}
	return wrote
}
