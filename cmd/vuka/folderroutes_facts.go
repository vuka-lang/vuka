package main

import (
	"bytes"
	"encoding/gob"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Route facts: what the route table needs of a page package's types,
// recorded while the package is transpiled (load.Options.Facts) and kept
// with its output, so a package not transpiled again brings them back
// without being type-checked. They are plain data: the assembler
// (routeTree.check) builds the table and its diagnostics from them alone.

// pkgFacts are a page package's route facts.
type pkgFacts struct {
	First      diag.Span  // its first declaration
	Scope      []string   // the names it declares at the top level
	Routes     *diag.Span // its Routes (pages/ itself only)
	Params     *diag.Span // its type Params, 1.x's
	OldLayout  *diag.Span // its type Layout
	Middleware *wrapFact
	Guard      *wrapFact
	Layout     *wrapFact // the type its _layout.vuka.html renders
	Types      []typeFact
	Funcs      []funcFact
	Queries    map[string][]queryFact // by page type: its @ui.URL("name") fields
	// Readers are its exported functions, each reading the request or
	// calling another page package's that may.
	Readers map[string]readFact
}

// wrapFact is a folder's Middleware, Guard or layout.
type wrapFact struct {
	Pos   diag.Span
	Bad   *diag.Diagnostic // why it is no middleware, guard or layout; nil when it is one
	Binds []bindFact
	Reads readFact // a layout's
}

// readFact says whether a function reads the request: Reads what it reads
// ("" nothing found in its package), Via the exported functions of other
// page packages it calls (import path and name), which may.
type readFact struct {
	Reads string
	Via   []string
}

// bindFact is the field or parameter of a carrier binding a path parameter.
type bindFact struct {
	Param  string // the parameter, as bracketed: id for [id]
	Typ    string // its type, packages by path
	What   string
	Pos    diag.Span
	Kind   paramKind
	KindOK bool
	Pkg    string // the package declaring its named type
	Go     goType
}

// goType is a type as Go source, its packages left to the file writing it.
type goType struct {
	Src   string // the type, each package a mark: \x00i\x00
	Pkgs  []typePkg
	Basic string // the predeclared type it is, when it is one
}

type typePkg struct{ Path, Name string }

// typeFact is a type of the package that may be a page or an endpoint.
type typeFact struct {
	Name       string
	Pos        diag.Span
	Ctor       string // New<Name>; "" when it has none
	Live, Page bool
	Methods    []methodFact // its own Get, Post, Put, Patch and Delete
	LoadUnused *diag.Span   // its own Load, which nothing calls
	Binds      []bindFact
	Renders    []litFact // the struct literals its Render and View write
}

type methodFact struct {
	Name string
	Pos  diag.Span
	Bad  *diag.Diagnostic // why it handles nothing
}

// litFact is a struct literal of a type a page renders.
type litFact struct {
	Pkg, Name string
	Pos       diag.Span
}

// funcFact is a method func of the package.
type funcFact struct {
	GoName string
	Pos    diag.Span
	Bad    *diag.Diagnostic
	Names  []string // its parameters' names
	Binds  []bindFact
}

// queryFact is a page's field kept in the query.
type queryFact struct {
	Field   string
	Pos     diag.Span
	Text    bool   // a URL's text can set it
	TypeStr string // its type, for the warning
	Named   []namedRef
	Go      goType
	Def     *defFact
}

// namedRef is a named type a query field's type is made of, outermost
// first.
type namedRef struct {
	Name, Pkg   string
	Exported    bool
	ImportsURLs bool
}

// defFact is a query field's default, written as a value of its type.
type defFact struct {
	Lit  string
	Conv *goType // the conversion it takes, when the literal alone isn't of the type
}

func goTypeOf(t types.Type) goType {
	var g goType
	g.Src = types.TypeString(t, func(p *types.Package) string {
		g.Pkgs = append(g.Pkgs, typePkg{p.Path(), p.Name()})
		return "\x00" + strconv.Itoa(len(g.Pkgs)-1) + "\x00"
	})
	if b, ok := types.Unalias(t).(*types.Basic); ok {
		g.Basic = b.Name()
	}
	return g
}

// render writes g, naming each package as q does.
func (g goType) render(q func(path, name string) string) string {
	s := g.Src
	for i, p := range g.Pkgs {
		mark := "\x00" + strconv.Itoa(i) + "\x00."
		if n := q(p.Path, p.Name); n != "" {
			s = strings.Replace(s, mark, n+".", 1)
		} else {
			s = strings.Replace(s, mark, "", 1)
		}
	}
	return s
}

// plain reports whether g is the predeclared type name.
func (g *goType) plain(name string) bool { return g != nil && g.Basic == name }

// options are opts recording the trees' route facts.
func (ts routeTrees) options(opts load.Options) load.Options {
	if len(ts) == 0 {
		return opts
	}
	opts.Facts = func(p *load.Package, check func(skip ...string) *load.Typed) []byte {
		t, d := ts.dirOf(p.ImportPath)
		if d == nil {
			return nil
		}
		var skip []string
		if d.rel == "" {
			skip = append(skip, routeGoName)
		}
		t0 := time.Now()
		ty := check(skip...)
		if ty == nil {
			return nil
		}
		defer func() { traceStep("route facts of %s %v", p.ImportPath, time.Since(t0)) }()
		var buf bytes.Buffer
		if gob.NewEncoder(&buf).Encode(t.factsOf(d, ty)) != nil {
			return nil
		}
		return buf.Bytes()
	}
	opts.Got = func(p *load.Package, data []byte) {
		t, d := ts.dirOf(p.ImportPath)
		if d == nil || data == nil {
			return
		}
		f := &pkgFacts{}
		if gob.NewDecoder(bytes.NewReader(data)).Decode(f) != nil {
			return
		}
		t.factsMu.Lock()
		defer t.factsMu.Unlock()
		if t.facts == nil {
			t.facts = map[string]*pkgFacts{}
		}
		t.facts[d.importPath] = f
	}
	return opts
}

// dirOf is the page directory of the import path, and its tree.
func (ts routeTrees) dirOf(path string) (*routeTree, *routeDir) {
	for _, t := range ts {
		for _, d := range t.dirs {
			if d.importPath == path {
				return t, d
			}
		}
	}
	return nil, nil
}

// factsSource is a page package type-checked, its facts to record.
type factsSource struct {
	t              *routeTree
	d              *routeDir
	ty             *load.Typed
	node, stateful types.Type
}

// factsOf is the route facts of d's package, ty.
func (t *routeTree) factsOf(d *routeDir, ty *load.Typed) *pkgFacts {
	s := &factsSource{t: t, d: d, ty: ty}
	if ui, err := ty.Import(load.UIPath); err == nil && ui != nil {
		s.node = lookupType(ui, "Node")
		s.stateful = lookupType(ui, "Stateful")
	}
	pkg := ty.Pkg
	scope := pkg.Scope()
	f := &pkgFacts{First: s.span(firstPos(pkg))}
	names := s.paramNames()
	f.Scope = scope.Names()
	if d.rel == "" {
		if obj := scope.Lookup("Routes"); obj != nil {
			f.Routes = s.spanPtr(obj.Pos())
		}
	}
	if obj, ok := scope.Lookup("Params").(*types.TypeName); ok {
		f.Params = s.spanPtr(obj.Pos())
	}
	if obj, ok := scope.Lookup("Layout").(*types.TypeName); ok {
		f.OldLayout = s.spanPtr(obj.Pos())
	}
	if obj, ok := scope.Lookup("Middleware").(*types.Func); ok {
		f.Middleware = &wrapFact{Pos: s.span(obj.Pos()), Bad: s.middlewareBad(obj)}
	}
	if obj, ok := scope.Lookup("Guard").(*types.TypeName); ok {
		f.Guard = &wrapFact{Pos: s.span(obj.Pos()), Bad: s.guardBad(obj), Binds: s.typeBinds(obj.Type(), names)}
	}
	if d.layout != "" {
		if obj, ok := scope.Lookup(d.layout).(*types.TypeName); ok {
			f.Layout = &wrapFact{Pos: s.span(obj.Pos()), Bad: s.layoutBad(obj), Binds: s.typeBinds(obj.Type(), names)}
			if f.Layout.Bad == nil {
				f.Layout.Reads = s.readsRequest(s.methodsOf(obj.Name()), true)
			}
		}
	}
	f.Readers = s.readers()
	s.types(f, names)
	s.funcs(f, names)
	f.Queries = s.queries()
	return f
}

func (s *factsSource) span(p token.Pos) diag.Span {
	if !p.IsValid() {
		return diag.Span{}
	}
	pos := s.ty.Fset.Position(p)
	return diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column}
}

func (s *factsSource) spanPtr(p token.Pos) *diag.Span {
	sp := s.span(p)
	return &sp
}

func (s *factsSource) bad(d *diag.Diagnostic, p token.Pos) *diag.Diagnostic {
	d.Span = s.span(p)
	return d
}

// paramNames are the parameters a carrier of the package may bind: those
// of its folders and of its bracketed files.
func (s *factsSource) paramNames() []string {
	var names []string
	for _, p := range strings.Split(s.d.rel, "/") {
		if load.Bracketed(p) {
			names = append(names, bracketName(p))
		}
	}
	for b := range s.d.files {
		names = append(names, bracketName(b))
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// typeBinds are the fields of t binding the parameters names.
func (s *factsSource) typeBinds(t types.Type, names []string) []bindFact {
	var out []bindFact
	for _, name := range names {
		f := s.boundField(t, name)
		if f == nil {
			continue
		}
		what := types.TypeString(t, types.RelativeTo(nil)) + "." + f.Name()
		if n := namedObj(t); n != nil {
			what = n.Pkg().Name() + "." + n.Name() + "." + f.Name()
		}
		out = append(out, bindOf(name, assignElem(f.Type()), what, s.span(f.Pos())))
	}
	return out
}

// sigBinds are the parameters of sig binding the parameters names.
func (s *factsSource) sigBinds(sig *types.Signature, names []string) []bindFact {
	var out []bindFact
	for _, name := range names {
		for i := 0; i < sig.Params().Len(); i++ {
			if v := sig.Params().At(i); strings.EqualFold(v.Name(), name) {
				out = append(out, bindOf(name, v.Type(), "the parameter "+v.Name(), s.span(v.Pos())))
				break
			}
		}
	}
	return out
}

func bindOf(name string, t types.Type, what string, at diag.Span) bindFact {
	b := bindFact{Param: name, Typ: types.TypeString(t, nil), What: what, Pos: at, Go: goTypeOf(t)}
	b.Kind, b.KindOK = paramKindOf(t)
	if obj := namedObj(t); obj != nil && obj.Pkg() != nil {
		b.Pkg = obj.Pkg().Path()
	}
	return b
}

// boundField is the field of t a path parameter binds: fieldFold's, else one
// the source marks with a bare @ui.URL, which may be unexported.
func (s *factsSource) boundField(t types.Type, name string) *types.Var {
	if f := fieldFold(t, name); f != nil {
		return f
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	tn, st := namedObj(t), (*types.Struct)(nil)
	if tn == nil || tn.Pkg() == nil {
		return nil
	}
	if st, _ = t.Underlying().(*types.Struct); st == nil {
		return nil
	}
	for _, b := range s.t.bareURLFields(tn.Pkg().Path(), tn.Name()) {
		for i := range st.NumFields() {
			if f := st.Field(i); f.Name() == b && strings.EqualFold(b, name) {
				return f
			}
		}
	}
	return nil
}

// types records the package's types that may be pages or endpoints.
func (s *factsSource) types(f *pkgFacts, names []string) {
	d, scope := s.d, s.ty.Pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || obj.IsAlias() {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 {
			continue
		}
		if _, ok := named.Underlying().(*types.Struct); !ok {
			continue
		}
		ctorName := "New" + name
		if ctor, ok := scope.Lookup(ctorName).(*types.Func); ok && !buildsPtr(ctor, named) {
			continue
		} else if !ok {
			ctorName = ""
		}
		tf := typeFact{Name: name, Pos: s.span(obj.Pos()), Ctor: ctorName, Live: s.isLive(named)}
		ms := types.NewMethodSet(types.NewPointer(named))
		for _, m := range append([]string{"Get"}, handlerMethods...) {
			if sel := ms.Lookup(nil, m); sel != nil && len(sel.Index()) == 1 {
				fn := sel.Obj().(*types.Func)
				tf.Methods = append(tf.Methods, methodFact{Name: m, Pos: s.span(fn.Pos()), Bad: s.handlerBad(name, fn)})
			}
		}
		island := false
		if file := d.fileOf[name]; file != "" {
			p, _, _, _ := islandPageOf(filepath.Join(d.dir, file))
			island = p == name
		}
		tf.Page = tf.Live || s.isNode(named) || island
		if !tf.Page && len(tf.Methods) == 0 {
			continue
		}
		if sel := ms.Lookup(nil, "Load"); sel != nil && len(sel.Index()) == 1 && !s.called(sel.Obj()) {
			tf.LoadUnused = s.spanPtr(sel.Obj().Pos())
		}
		tf.Binds = s.typeBinds(named, names)
		if tf.Page {
			tf.Renders = s.renders(name)
		}
		f.Types = append(f.Types, tf)
	}
}

// funcs records the package's method funcs.
func (s *factsSource) funcs(f *pkgFacts, names []string) {
	for _, mf := range s.d.funcs {
		fn, ok := s.ty.Pkg.Scope().Lookup(mf.goName).(*types.Func)
		if !ok {
			continue
		}
		sig := fn.Type().(*types.Signature)
		ff := funcFact{GoName: mf.goName, Pos: s.span(fn.Pos()), Bad: s.methodFuncBad(mf, fn), Binds: s.sigBinds(sig, names)}
		for i := 0; i < sig.Params().Len(); i++ {
			ff.Names = append(ff.Names, sig.Params().At(i).Name())
		}
		f.Funcs = append(f.Funcs, ff)
	}
}

// renders are the struct literals the Render and View methods of typ
// write, by the types they name.
func (s *factsSource) renders(typ string) []litFact {
	var out []litFact
	for _, f := range s.ty.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || recvTypeName(fd.Recv.List[0].Type) != typ ||
				fd.Name.Name != "Render" && fd.Name.Name != "View" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if tn := s.litType(lit.Type); tn != nil && tn.Pkg() != nil {
					out = append(out, litFact{Pkg: tn.Pkg().Path(), Name: tn.Name(), Pos: s.span(lit.Pos())})
				}
				return true
			})
		}
	}
	return out
}

// litType is the type a composite literal's type names, T or pkg.T.
func (s *factsSource) litType(x ast.Expr) *types.TypeName {
	var id *ast.Ident
	switch t := x.(type) {
	case *ast.Ident:
		id = t
	case *ast.SelectorExpr:
		id = t.Sel
	case *ast.IndexExpr:
		return s.litType(t.X)
	}
	if id == nil {
		return nil
	}
	tn, _ := s.ty.Uses[id].(*types.TypeName)
	return tn
}

// called reports whether code of the package uses obj: its method table
// (vuka_calls_gen.go) names methods without calling them.
func (s *factsSource) called(obj types.Object) bool {
	for id, used := range s.ty.Uses {
		if used == obj && filepath.Base(s.ty.Fset.Position(id.Pos()).Filename) != transpile.CallsGoName {
			return true
		}
	}
	return false
}

// factsStore is a tree's route facts, which a run's packages report as
// they transpile.
type factsStore struct {
	factsMu sync.Mutex
	facts   map[string]*pkgFacts // by import path
}

// layoutBad is why a folder's layout, the struct its _layout.vuka.html
// renders, is none; nil when it is one.
func (s *factsSource) layoutBad(obj *types.TypeName) *diag.Diagnostic {
	d := s.d
	bad := func(p token.Pos, help, format string, args ...any) *diag.Diagnostic {
		return s.bad(diag.New("E0708", "%s.%s, the layout of pages/%s, "+format, append([]any{obj.Pkg().Name(), obj.Name(), d.rel}, args...)...).WithHelp("%s", help), p)
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok || obj.IsAlias() {
		return bad(obj.Pos(), "declare its struct in _layout.vuka.html, with a field Children ui.Node", "isn't a struct")
	}
	if !obj.Exported() {
		return bad(obj.Pos(), "name it with a capital letter: Layout", "isn't exported, and the routes name it from package pages")
	}
	children := false
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Name() == "Children" && !f.Embedded() {
			if s.node == nil || !types.Identical(f.Type(), s.node) {
				return bad(f.Pos(), "declare it as Children ui.Node", "has Children of type %s, not ui.Node", types.TypeString(f.Type(), types.RelativeTo(obj.Pkg())))
			}
			children = true
		}
	}
	if !children {
		return bad(obj.Pos(), "add the field Children ui.Node: the page, or the next layout, renders there", "has no field Children")
	}
	if !s.isNode(obj.Type()) {
		return bad(obj.Pos(), "give _layout.vuka.html markup", "isn't a ui.Node: it has nothing to render")
	}
	ms := types.NewMethodSet(types.NewPointer(obj.Type()))
	if sel := ms.Lookup(nil, "Load"); sel != nil {
		sig := sel.Obj().Type().(*types.Signature)
		if sig.Params().Len() == 0 || !isContext(sig.Params().At(0).Type()) || sig.Variadic() ||
			sig.Results().Len() != 1 || !isError(sig.Results().At(0).Type()) {
			return bad(sel.Obj().Pos(), "declare it as Load(ctx context.Context, deps…) error", "has a Load method of type %s", types.TypeString(sig, types.RelativeTo(obj.Pkg())))
		}
	}
	return nil
}

// guardBad is why a folder's Guard is none: a guard is a struct with
// Load(ctx, deps…) error.
func (s *factsSource) guardBad(obj *types.TypeName) *diag.Diagnostic {
	bad := func(p token.Pos, format string, args ...any) *diag.Diagnostic {
		return s.bad(diag.New("E0708", "%s.Guard "+format, append([]any{obj.Pkg().Name()}, args...)...).
			WithHelp("declare type Guard struct{ … } with Load(ctx context.Context, deps…) error"), p)
	}
	if _, ok := obj.Type().Underlying().(*types.Struct); !ok || obj.IsAlias() {
		return bad(obj.Pos(), "isn't a struct")
	}
	sel := types.NewMethodSet(types.NewPointer(obj.Type())).Lookup(nil, "Load")
	if sel == nil {
		return bad(obj.Pos(), "has no Load method: it checks nothing")
	}
	sig := sel.Obj().Type().(*types.Signature)
	if sig.Params().Len() == 0 || !isContext(sig.Params().At(0).Type()) || sig.Variadic() ||
		sig.Results().Len() != 1 || !isError(sig.Results().At(0).Type()) {
		return bad(sel.Obj().Pos(), "has a Load method of type %s", types.TypeString(sig, types.RelativeTo(obj.Pkg())))
	}
	return nil
}

// middlewareBad is why a folder's Middleware is none: middleware is
// func(next http.Handler) http.Handler, or a constructor func(deps…)
// web.Middleware, its dependencies provided types.
func (s *factsSource) middlewareBad(fn *types.Func) *diag.Diagnostic {
	sig := fn.Type().(*types.Signature)
	bad := func() *diag.Diagnostic {
		return s.bad(diag.New("E0710", "%s.Middleware is %s", fn.Pkg().Name(), types.TypeString(sig, types.RelativeTo(fn.Pkg()))).
			WithHelp("declare func Middleware(next http.Handler) http.Handler, or func Middleware(deps…) web.Middleware"), fn.Pos())
	}
	res := sig.Results()
	if sig.Variadic() || res.Len() != 1 {
		return bad()
	}
	if isHTTPHandler(res.At(0).Type()) {
		if sig.Params().Len() != 1 || !isHTTPHandler(sig.Params().At(0).Type()) {
			return bad()
		}
		return nil
	}
	if !isMiddlewareType(res.At(0).Type()) {
		return bad()
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if !isPtrOrIface(sig.Params().At(i).Type()) {
			return bad()
		}
	}
	return nil
}

// isNode reports whether t or *t renders: a ui.Node, or Render() / View()
// giving one.
func (s *factsSource) isNode(t types.Type) bool {
	if s.node == nil {
		return false
	}
	iface, _ := s.node.Underlying().(*types.Interface)
	if iface != nil && (types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)) {
		return true
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	for _, name := range []string{"Render", "View"} {
		if sel := ms.Lookup(nil, name); sel != nil {
			sig := sel.Obj().Type().(*types.Signature)
			if sig.Params().Len() == 0 && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), s.node) {
				return true
			}
		}
	}
	return false
}

func (s *factsSource) isLive(t types.Type) bool {
	if s.stateful == nil {
		return false
	}
	iface, _ := s.stateful.Underlying().(*types.Interface)
	return iface != nil && types.Implements(types.NewPointer(t), iface)
}

// methodFuncBad checks a method func's signature: what a handler takes, its
// scalars bound by their names from the path or the query.
func (s *factsSource) methodFuncBad(mf methodFunc, fn *types.Func) *diag.Diagnostic {
	sig := fn.Type().(*types.Signature)
	bad := func(help, format string, args ...any) *diag.Diagnostic {
		return s.bad(diag.New("E0709", "%s (%s) "+format, append([]any{mf.method, mf.file}, args...)...).WithHelp("%s", help), fn.Pos())
	}
	if sig.Variadic() || sig.TypeParams().Len() > 0 {
		return bad("take a struct for what the request carries", "is variadic or generic")
	}
	body := false
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t := p.Type()
		switch {
		case isContext(t), isNamed(t, "net/http", "Request", true), isNamed(t, "net/http", "ResponseWriter", false):
		case scalarParam(t):
			if p.Name() == "" || p.Name() == "_" {
				return bad("name it: a scalar binds the path parameter or query value of its name", "takes a scalar without a name")
			}
		case structParam(t) && !isPointer(t):
			if body {
				return bad("join them in one struct", "takes a second struct to bind from the request")
			}
			body = true
		case isPtrOrIface(t):
		default:
			return bad("take a struct for what the request carries, or a provided dependency", "takes %s, which can't be bound from a request", types.TypeString(t, types.RelativeTo(fn.Pkg())))
		}
	}
	res := sig.Results()
	if res.Len() > 2 || res.Len() == 2 && !isError(res.At(1).Type()) {
		return bad("return nothing, error, a value, or (a value, error)", "returns %s", types.TypeString(res, types.RelativeTo(fn.Pkg())))
	}
	return nil
}

// handlerBad checks a method handler's signature against what web binds: a
// context, the request and response, provided dependencies, and one struct
// from the body; it returns nothing, error, T or (T, error).
func (s *factsSource) handlerBad(page string, fn *types.Func) *diag.Diagnostic {
	sig := fn.Type().(*types.Signature)
	bad := func(help, format string, args ...any) *diag.Diagnostic {
		return s.bad(diag.New("E0709", "%s.%s "+format, append([]any{page, fn.Name()}, args...)...).WithHelp("%s", help), fn.Pos())
	}
	if sig.Variadic() {
		return bad("take a struct for what the request carries", "is variadic")
	}
	body := false
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t := p.Type()
		switch {
		case isContext(t), isNamed(t, "net/http", "Request", true), isNamed(t, "net/http", "ResponseWriter", false):
		case scalarParam(t):
			return bad("put it in a struct the handler takes: its fields bind from the form, the query and the path", "takes the scalar %s, which binds by name, and a method's parameters have none", p.Name())
		case structParam(t) && !isPointer(t):
			if body {
				return bad("join them in one struct", "takes a second struct to bind from the request")
			}
			body = true
		case isPtrOrIface(t):
		default:
			return bad("take a struct for what the request carries, or a provided dependency", "takes %s, which can't be bound from a request", types.TypeString(t, types.RelativeTo(fn.Pkg())))
		}
	}
	res := sig.Results()
	if res.Len() > 2 || res.Len() == 2 && !isError(res.At(1).Type()) {
		return bad("return nothing, error, a value, or (a value, error)", "returns %s", types.TypeString(res, types.RelativeTo(fn.Pkg())))
	}
	return nil
}
