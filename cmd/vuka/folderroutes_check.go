package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// folderRoute is one route of the table: a page, one of its method
// handlers, or a method of an endpoint.
type folderRoute struct {
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Page       string   `json:"page"` // employer.Edit
	Kind       string   `json:"kind"` // live, page, handler or endpoint
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Middleware []string `json:"middleware,omitempty"`
	Guards     []string `json:"guards,omitempty"`
	Layouts    []string `json:"layouts,omitempty"`
	// Root is the route's root layout, the first of Layouts: one with no
	// layout above it. Pages under one root share a document, and web
	// navigates between two roots with a full page load.
	Root string `json:"root,omitempty"`

	dir     *routeDir
	typ     string   // the page's type; "" for a method func
	ctor    string   // its constructor; "" when it has none, and route.go builds it
	handler string   // the method, for a handler or an endpoint
	fn      string   // a method func's name in its package
	names   []string // a method func's parameters' names
	node    string   // the folder of urls it is in, a slash path ([id] for a parameter; route groups left out)
	leaf    string   // its field there, "" when it is the folder's Index
	mws     []*routeDir
	wraps   []wrap
}

// wrap is a guard or a layout over a folder's routes.
type wrap struct {
	dir   *routeDir
	guard bool
}

// dirInfo is what the type-checked package of a page directory makes it.
type dirInfo struct {
	pkg    *types.Package
	node   string      // the folder's slash path as the URL sees it, its route groups left out
	segs   []string    // the URL's segments down to the folder
	params []string    // the keys of the path parameters down to it, in order
	rest   bool        // it is a catch-all folder, [path...]
	mws    []*routeDir // the middlewares over its routes, outermost first
	wraps  []wrap      // the guards and layouts over its routes, outermost first
	pages  []string    // its pages' and endpoints' types, sorted
	broken bool        // its Guard, Layout or Middleware has an error: no routes
}

// urlParam is a path parameter, as a URL function takes it.
type urlParam struct {
	name string
	typ  types.Type
	kind paramKind
}

type paramKind int

const (
	paramString paramKind = iota
	paramInt
	paramUint
	paramText
	paramRest
)

// pathParam is a path parameter of the tree: a [id] folder, a [id] file, or
// both (E0713). Its key is its slash path from pages/, route groups left
// out: orgs/[org].
type pathParam struct {
	key, name    string
	rest         bool
	folder, file diag.Span // where it is named; a zero File when it isn't
	p            urlParam  // its type, from the fields that bind it
}

// carrier is a struct or method func a path's parameters bind into: a
// page, an endpoint, a guard or a layout.
type carrier struct {
	typ    types.Type
	sig    *types.Signature
	params []string // the keys of its path's parameters
	prefer string   // the key of the parameter whose folder's layout it is
	pos    token.Pos
}

// check type-checks the page packages through the overlay and builds the
// route table into res.
func (t *routeTree) check(ov map[string]string, overlay string, res *routesResult) {
	r := &routeImporter{t: t, ctx: overlayContext(ov), fset: token.NewFileSet(), im: load.NewModuleImporter(t.root),
		done: map[string]*types.Package{}, walking: map[string]bool{}, byPath: map[string]*routeDir{}, goVersion: goVersionOf(t.root)}
	r.im.Overlay = overlay
	var imports []string
	for _, d := range t.dirs {
		r.byPath[d.importPath] = d
	}
	for _, d := range t.dirs {
		if bp, err := r.ctx.ImportDir(d.goDir, 0); err == nil {
			for _, p := range bp.Imports {
				if r.byPath[p] == nil && !slices.Contains(imports, p) {
					imports = append(imports, p)
				}
			}
		}
	}
	_ = r.im.Prefetch(append(imports, load.UIPath))
	c := &routeChecker{t: t, r: r, res: res, info: map[*routeDir]*dirInfo{}, params: map[string]*pathParam{}}
	if ui, err := r.Import(load.UIPath); err == nil {
		c.node = lookupType(ui, "Node")
		c.stateful = lookupType(ui, "Stateful")
	}
	for _, d := range t.dirs {
		pkg, _ := r.checkDir(d)
		c.dir(d, pkg)
	}
	for _, rt := range res.routes {
		if len(rt.Layouts) > 0 {
			rt.Root = rt.Layouts[0]
		}
	}
	c.typeParams()
	c.conflicts()
	t.info = c.info
	t.urls = c.urlTree()
}

// routeImporter type-checks the page packages from source, tolerating
// errors (their URL functions are stubs), and imports everything else
// through the overlay.
type routeImporter struct {
	t         *routeTree
	ctx       *build.Context
	fset      *token.FileSet
	im        *load.Importer
	done      map[string]*types.Package
	walking   map[string]bool
	byPath    map[string]*routeDir
	goVersion string
	syntax    map[string]checked // a page package's files and uses, by import path
}

// checked is a page package's syntax as type-checked.
type checked struct {
	files []*ast.File
	uses  map[*ast.Ident]types.Object
}

func (r *routeImporter) Import(path string) (*types.Package, error) {
	return r.ImportFrom(path, r.t.root, 0)
}

func (r *routeImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if d := r.byPath[path]; d != nil {
		return r.checkDir(d)
	}
	return r.im.ImportFrom(path, dir, mode)
}

func (r *routeImporter) checkDir(d *routeDir) (*types.Package, error) {
	if p, ok := r.done[d.importPath]; ok {
		if p == nil {
			return nil, errors.New(d.importPath + " has no Go files")
		}
		return p, nil
	}
	if r.walking[d.importPath] {
		return nil, errors.New("import cycle through " + d.importPath)
	}
	r.walking[d.importPath] = true
	defer delete(r.walking, d.importPath)
	r.done[d.importPath] = nil
	bp, err := r.ctx.ImportDir(d.goDir, 0)
	if bp == nil || len(bp.GoFiles) == 0 {
		return nil, fmt.Errorf("%s: %v", d.importPath, err)
	}
	var files []*ast.File
	for _, name := range bp.GoFiles {
		if d.rel == "" && name == routeGoName {
			continue
		}
		path := filepath.Join(d.goDir, name)
		rc, err := r.ctx.OpenFile(path)
		if err != nil {
			continue
		}
		src, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		f, _ := parser.ParseFile(r.fset, path, src, parser.SkipObjectResolution)
		if f != nil {
			files = append(files, f)
		}
	}
	conf := types.Config{Importer: r, GoVersion: r.goVersion, Error: func(error) {}}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	pkg, _ := conf.Check(d.importPath, r.fset, files, info)
	r.done[d.importPath] = pkg
	if r.syntax == nil {
		r.syntax = map[string]checked{}
	}
	r.syntax[d.importPath] = checked{files, info.Uses}
	return pkg, nil
}

// routeChecker builds the table from the checked packages.
type routeChecker struct {
	t              *routeTree
	r              *routeImporter
	res            *routesResult
	info           map[*routeDir]*dirInfo
	params         map[string]*pathParam
	carriers       []carrier
	node, stateful types.Type
}

func (c *routeChecker) span(p token.Pos) diag.Span {
	pos := c.r.fset.Position(p)
	return diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column}
}

func (c *routeChecker) report(d *diag.Diagnostic, p token.Pos) *diag.Diagnostic {
	d.Span = c.span(p)
	c.res.diags = append(c.res.diags, *d)
	return &c.res.diags[len(c.res.diags)-1]
}

// reportAt reports d at a span.
func (c *routeChecker) reportAt(d *diag.Diagnostic, at diag.Span) *diag.Diagnostic {
	d.Span = at
	c.res.diags = append(c.res.diags, *d)
	return &c.res.diags[len(c.res.diags)-1]
}

// parentOf is the nearest directory above d with an info.
func (c *routeChecker) parentOf(d *routeDir) (*routeDir, *dirInfo) {
	if d.rel == "" {
		return nil, nil
	}
	for rel := parentRel(d.rel); ; rel = parentRel(rel) {
		for _, p := range c.t.dirs {
			if p.rel == rel && c.info[p] != nil {
				return p, c.info[p]
			}
		}
		if rel == "" {
			return nil, nil
		}
	}
}

func parentRel(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

// joinRel is rel/name, name alone at pages/.
func joinRel(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

// param records the path parameter named by the bracketed name elem at key,
// a folder or a file, and returns its segment: {id}, {path...}.
func (c *routeChecker) param(key, elem string, at diag.Span, file bool) string {
	p := c.params[key]
	if p == nil {
		p = &pathParam{key: key, name: bracketName(elem), rest: isRest(elem)}
		c.params[key] = p
	}
	if file && p.file.File == "" {
		p.file = at
	} else if !file && p.folder.File == "" {
		p.folder = at
	}
	if p.rest {
		return "{" + p.name + "...}"
	}
	return "{" + p.name + "}"
}

func (c *routeChecker) dir(d *routeDir, pkg *types.Package) {
	in := &dirInfo{pkg: pkg, node: urlRel(d.rel)}
	c.info[d] = in
	if _, pin := c.parentOf(d); pin != nil {
		in.mws, in.wraps, in.broken = slices.Clone(pin.mws), slices.Clone(pin.wraps), pin.broken
	}
	below := false // below a catch-all folder
	if d.rel != "" {
		parts := strings.Split(d.rel, "/")
		var url []string // the parts the URL sees
		for i, p := range parts {
			if load.Grouped(p) {
				continue
			}
			url = append(url, p)
			if !load.Bracketed(p) {
				in.segs = append(in.segs, p)
				continue
			}
			key := strings.Join(url, "/")
			at := diag.Span{File: filepath.Join(c.t.pages, filepath.FromSlash(strings.Join(parts[:i+1], "/")))}
			in.segs = append(in.segs, c.param(key, p, at, false))
			in.params = append(in.params, key)
			if isRest(p) && i < len(parts)-1 {
				below = true
			}
		}
		in.rest = isRest(parts[len(parts)-1])
	}
	if pkg == nil {
		return
	}
	if below {
		c.report(diag.New("E0703", "pages/%s is below a catch-all folder", d.rel), firstPos(pkg)).
			WithHelp("a [path...] folder takes the rest of the path: it has no subfolders")
		in.broken = true
	}
	if obj := pkg.Scope().Lookup("Routes"); obj != nil && d.rel == "" {
		c.report(diag.New("E0706", "%s.Routes is declared, and vuka declares it in pages/route.go: the option registering the folder routes", d.name), obj.Pos()).
			WithHelp("rename %s", obj.Name())
	}
	// A params.go folder and a layout.go Layout are 1.x's: each is an error
	// naming the one-way form.
	if obj, ok := pkg.Scope().Lookup("Params").(*types.TypeName); ok {
		help := "rename the folder to [" + strings.ToLower(filepath.Base(d.dir)) + "]/ and declare the parameter's field on the structs that read it"
		c.report(diag.New("E0714", "%s.Params: a parameter folder is a bracketed folder now, and Params binds nothing", d.name), obj.Pos()).WithHelp("%s", help)
	}
	if obj, ok := pkg.Scope().Lookup("Layout").(*types.TypeName); ok && d.layout == "" {
		help := "declare the layout's struct and markup in _layout.vuka.html"
		if d.oldLayout != "" {
			help = "rename " + d.oldLayout + " to _layout.vuka.html"
		}
		c.report(diag.New("E0715", "%s.Layout: a folder's layout is its _layout.vuka.html now, and this Layout wraps nothing", d.name), obj.Pos()).WithHelp("%s", help)
	}
	if obj, ok := pkg.Scope().Lookup("Middleware").(*types.Func); ok {
		if c.middleware(obj) {
			in.mws = append(in.mws, d)
		} else {
			in.broken = true
		}
	}
	prefer := ""
	if d.rel != "" && load.Bracketed(filepath.Base(d.dir)) {
		prefer = in.node
	}
	if obj, ok := pkg.Scope().Lookup("Guard").(*types.TypeName); ok {
		if c.guard(obj) {
			in.wraps = append(in.wraps, wrap{d, true})
			c.carriers = append(c.carriers, carrier{typ: obj.Type(), params: in.params, pos: obj.Pos()})
		} else {
			in.broken = true
		}
	}
	if d.layout != "" {
		if obj, ok := pkg.Scope().Lookup(d.layout).(*types.TypeName); ok {
			if c.layout(obj, d) {
				d.reloads = c.readsRequest(obj, d) != ""
				in.wraps = append(in.wraps, wrap{d, false})
				c.carriers = append(c.carriers, carrier{typ: obj.Type(), params: in.params, prefer: prefer, pos: obj.Pos()})
			} else {
				in.broken = true
			}
		}
	}
	c.pages(d, in)
}

// typeParams gives each path parameter its type: the type of the fields
// binding it — the exported field named like it, case aside, of a struct
// below it (a page, an endpoint, a guard, a layout), or a method func's
// parameter of its name — which agree. The parameter folder's layout
// speaks first.
func (c *routeChecker) typeParams() {
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range c.params {
			if !yield(k) {
				return
			}
		}
	}) {
		p := c.params[key]
		at := p.folder
		if at.File == "" {
			at = p.file
		}
		type use struct {
			typ  types.Type
			pos  token.Pos
			what string
		}
		var uses []use
		for _, cr := range c.carriers {
			if !slices.Contains(cr.params, key) {
				continue
			}
			var u *use
			if cr.sig != nil {
				for i := 0; i < cr.sig.Params().Len(); i++ {
					if v := cr.sig.Params().At(i); strings.EqualFold(v.Name(), p.name) {
						u = &use{v.Type(), v.Pos(), "the parameter " + v.Name()}
						break
					}
				}
			} else if f := fieldFold(cr.typ, p.name); f != nil {
				u = &use{f.Type(), f.Pos(), types.TypeString(cr.typ, types.RelativeTo(nil)) + "." + f.Name()}
				if n := namedObj(cr.typ); n != nil {
					u.what = n.Pkg().Name() + "." + n.Name() + "." + f.Name()
				}
			}
			if u == nil {
				continue
			}
			if cr.prefer == key {
				uses = append([]use{*u}, uses...)
			} else {
				uses = append(uses, *u)
			}
		}
		if len(uses) == 0 {
			c.reportAt(diag.New("E0711", "pages/%s: nothing binds the parameter {%s}", key, p.name), at).
				WithHelp("give a page, endpoint or layout below it the field it binds: %s int (or string, a TextUnmarshaler…)", goIdent(p.name))
			continue
		}
		first := uses[0]
		bad := false
		for _, u := range uses[1:] {
			if !types.Identical(u.typ, first.typ) {
				c.report(diag.New("E0712", "%s is a %s, and %s binding {%s} too is a %s", u.what, types.TypeString(u.typ, nil), first.what, p.name, types.TypeString(first.typ, nil)), u.pos).
					WithRelated(c.span(first.pos)).WithHelp("give every field binding {%s} one type", p.name)
				bad = true
			}
		}
		k, ok := paramKindOf(first.typ)
		switch {
		case !ok:
			c.report(diag.New("E0705", "%s is a %s, which the path parameter {%s} can't be", first.what, types.TypeString(first.typ, nil), p.name), first.pos).
				WithHelp("use an integer, a string, web.Rest, or a type implementing encoding.TextMarshaler and TextUnmarshaler")
			bad = true
		case p.rest && k != paramRest && k != paramString:
			c.report(diag.New("E0705", "%s is a %s, and {%s...} takes the rest of the path", first.what, types.TypeString(first.typ, nil), p.name), first.pos).
				WithHelp("declare it as web.Rest or string")
			bad = true
		case !p.rest && k == paramRest:
			c.report(diag.New("E0705", "%s is a web.Rest, and {%s} is one segment", first.what, p.name), first.pos).
				WithHelp("name the parameter [%s...] to take the rest of the path, or declare the field a string", p.name)
			bad = true
		}
		if obj := namedObj(first.typ); !bad && obj != nil && obj.Pkg() != nil && c.r.byPath[obj.Pkg().Path()] != nil {
			c.report(diag.New("E0705", "%s is a %s, declared in a page package, which the package urls can't import", first.what, types.TypeString(first.typ, nil)), first.pos).
				WithHelp("declare the type outside pages/, in a package the pages and urls both import")
			bad = true
		}
		if p.rest {
			k = paramRest
		}
		p.p = urlParam{name: p.name, typ: first.typ, kind: k}
		if bad {
			p.p.typ = nil
		}
	}
}

// fieldFold is t's exported field named name, case aside, promoted ones
// included: the field a path parameter binds.
func fieldFold(t types.Type, name string) *types.Var {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); !f.Embedded() && f.Exported() && strings.EqualFold(f.Name(), name) {
			return f
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Embedded() {
			if g := fieldFold(f.Type(), name); g != nil {
				return g
			}
		}
	}
	return nil
}

// paramKindOf says how a path parameter of type t is parsed and written.
func paramKindOf(t types.Type) (paramKind, bool) {
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == webPath && n.Obj().Name() == "Rest" {
		return paramRest, true
	}
	if hasMethod(t, "MarshalText") && hasMethod(types.NewPointer(t), "UnmarshalText") {
		return paramText, true
	}
	b, ok := t.Underlying().(*types.Basic)
	switch {
	case !ok:
	case b.Info()&types.IsUnsigned != 0:
		return paramUint, true
	case b.Info()&types.IsInteger != 0:
		return paramInt, true
	case b.Kind() == types.String:
		return paramString, true
	}
	return 0, false
}

func hasMethod(t types.Type, name string) bool {
	ms := types.NewMethodSet(t)
	sel := ms.Lookup(nil, name)
	return sel != nil
}

// layout checks a folder's layout, the struct its _layout.vuka.html
// renders.
func (c *routeChecker) layout(obj *types.TypeName, d *routeDir) bool {
	bad := func(p token.Pos, help, format string, args ...any) bool {
		c.report(diag.New("E0708", "%s.%s, the layout of pages/%s, "+format, append([]any{obj.Pkg().Name(), obj.Name(), d.rel}, args...)...), p).WithHelp("%s", help)
		return false
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
			if c.node == nil || !types.Identical(f.Type(), c.node) {
				return bad(f.Pos(), "declare it as Children ui.Node", "has Children of type %s, not ui.Node", types.TypeString(f.Type(), types.RelativeTo(obj.Pkg())))
			}
			children = true
		}
	}
	if !children {
		return bad(obj.Pos(), "add the field Children ui.Node: the page, or the next layout, renders there", "has no field Children")
	}
	if !c.isNode(obj.Type()) {
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
	return true
}

// guard checks a folder's Guard: a struct with Load(ctx, deps…) error.
func (c *routeChecker) guard(obj *types.TypeName) bool {
	bad := func(p token.Pos, format string, args ...any) bool {
		c.report(diag.New("E0708", "%s.Guard "+format, append([]any{obj.Pkg().Name()}, args...)...), p).
			WithHelp("declare type Guard struct{ … } with Load(ctx context.Context, deps…) error")
		return false
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
	return true
}

// middleware checks a folder's Middleware: func(next http.Handler)
// http.Handler, or a constructor func(deps…) web.Middleware, its
// dependencies provided types.
func (c *routeChecker) middleware(fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	bad := func() bool {
		c.report(diag.New("E0710", "%s.Middleware is %s", fn.Pkg().Name(), types.TypeString(sig, types.RelativeTo(fn.Pkg()))), fn.Pos()).
			WithHelp("declare func Middleware(next http.Handler) http.Handler, or func Middleware(deps…) web.Middleware")
		return false
	}
	res := sig.Results()
	if sig.Variadic() || res.Len() != 1 {
		return bad()
	}
	if isHTTPHandler(res.At(0).Type()) {
		if sig.Params().Len() != 1 || !isHTTPHandler(sig.Params().At(0).Type()) {
			return bad()
		}
		return true
	}
	if !isMiddlewareType(res.At(0).Type()) {
		return bad()
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if !isPtrOrIface(sig.Params().At(i).Type()) {
			return bad()
		}
	}
	return true
}

func isHTTPHandler(t types.Type) bool { return isNamed(t, "net/http", "Handler", false) }

// isMiddlewareType reports whether t is web.Middleware, an alias of
// func(http.Handler) http.Handler.
func isMiddlewareType(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	return ok && !sig.Variadic() && sig.Params().Len() == 1 && isHTTPHandler(sig.Params().At(0).Type()) &&
		sig.Results().Len() == 1 && isHTTPHandler(sig.Results().At(0).Type())
}

// isNode reports whether t or *t renders: a ui.Node, or Render() / View()
// giving one.
func (c *routeChecker) isNode(t types.Type) bool {
	if c.node == nil {
		return false
	}
	iface, _ := c.node.Underlying().(*types.Interface)
	if iface != nil && (types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)) {
		return true
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	for _, name := range []string{"Render", "View"} {
		if sel := ms.Lookup(nil, name); sel != nil {
			sig := sel.Obj().Type().(*types.Signature)
			if sig.Params().Len() == 0 && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), c.node) {
				return true
			}
		}
	}
	return false
}

func (c *routeChecker) isLive(t types.Type) bool {
	if c.stateful == nil {
		return false
	}
	iface, _ := c.stateful.Underlying().(*types.Interface)
	return iface != nil && types.Implements(types.NewPointer(t), iface)
}

// handlerMethods are the methods of a page type that handle other HTTP
// methods on its path, in the order they register.
var handlerMethods = []string{"Post", "Put", "Patch", "Delete"}

// pages finds a folder's pages, their method handlers and its method funcs.
func (c *routeChecker) pages(d *routeDir, in *dirInfo) {
	scope := in.pkg.Scope()
	var mws, guards, layouts []string
	for _, m := range in.mws {
		mws = append(mws, m.name+".Middleware")
	}
	for _, w := range in.wraps {
		if w.guard {
			guards = append(guards, w.dir.name+".Guard")
		} else {
			layouts = append(layouts, w.dir.name+"."+w.dir.layout)
		}
	}
	// at is where a route of a file is: its node in urls, its path and its
	// parameters; "" seg for the folder's own path.
	at := func(seg string, span diag.Span, pos token.Pos) (node string, path string, params []string, ok bool) {
		node, params = in.node, in.params
		segs := in.segs
		switch {
		case seg == "":
		case in.rest:
			c.report(diag.New("E0703", "pages/%s: a route below the catch-all parameter {%s...}", joinRel(d.rel, seg), c.params[in.params[len(in.params)-1]].name), pos).
				WithHelp("a catch-all folder serves its index alone: move it up a folder")
			return "", "", nil, false
		case load.Bracketed(seg):
			node = joinRel(in.node, seg)
			segs = append(slices.Clone(segs), c.param(node, seg, span, true))
			params = append(slices.Clone(params), node)
		default:
			node = joinRel(in.node, seg)
			segs = append(slices.Clone(segs), seg)
		}
		return node, "/" + strings.Join(segs, "/"), params, true
	}
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || obj.IsAlias() || d.notPage(name) {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 {
			continue
		}
		if _, ok := named.Underlying().(*types.Struct); !ok {
			continue
		}
		if c.t.registered[name] || c.t.registered[d.importPath+"."+name] {
			continue
		}
		ctorName := "New" + name
		if ctor, ok := scope.Lookup(ctorName).(*types.Func); ok && !buildsPtr(ctor, named) {
			continue
		} else if !ok {
			ctorName = ""
		}
		live := c.isLive(named)
		ms := types.NewMethodSet(types.NewPointer(named))
		var methods []*types.Func // its own Get, Post, Put, Patch and Delete
		for _, m := range append([]string{"Get"}, handlerMethods...) {
			if sel := ms.Lookup(nil, m); sel != nil && len(sel.Index()) == 1 {
				methods = append(methods, sel.Obj().(*types.Func))
			}
		}
		// A .vuka.html file whose markup is one island tag is the page its
		// island backs, live or not.
		island := false
		if f := d.fileOf[name]; f != "" {
			p, _, _, _ := islandPageOf(filepath.Join(d.dir, f))
			island = p == name
		}
		page := live || c.isNode(named) || island
		if !page && len(methods) == 0 {
			continue
		}
		urlName := d.urlName(name)
		seg, leaf := "", ""
		switch {
		case load.Bracketed(urlName):
			seg = urlName
		case urlName != "Index":
			seg, leaf = kebab(urlName), urlName
		}
		file := filepath.Join(d.dir, d.fileOf[name])
		if b := d.bracket[name]; b != "" && d.fileOf[name] == "" {
			file = c.r.fset.Position(obj.Pos()).Filename
		}
		node, path, params, ok := at(seg, diag.Span{File: file, Line: 1}, obj.Pos())
		if !ok {
			continue
		}
		if leaf != "" {
			node = in.node
		}
		in.pages = append(in.pages, name)
		c.carriers = append(c.carriers, carrier{typ: named, params: params, pos: obj.Pos()})
		if in.broken {
			continue
		}
		if !page {
			for _, fn := range methods {
				if !c.handler(name, fn) {
					continue
				}
				hp := c.r.fset.Position(fn.Pos())
				c.res.routes = append(c.res.routes, &folderRoute{Method: strings.ToUpper(fn.Name()), Path: path, Page: d.name + "." + name + "." + fn.Name(),
					Kind: "endpoint", File: hp.Filename, Line: hp.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: ctorName,
					handler: fn.Name(), node: node, leaf: leaf, mws: in.mws, wraps: in.wraps})
			}
			continue
		}
		pos := c.r.fset.Position(obj.Pos())
		r := &folderRoute{Method: "GET", Path: path, Page: d.name + "." + name, Kind: "page", File: pos.Filename, Line: pos.Line,
			Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: ctorName, node: node, leaf: leaf, mws: in.mws, wraps: in.wraps}
		if live {
			r.Kind = "live"
		}
		c.res.routes = append(c.res.routes, r)
		for _, fn := range methods {
			m := fn.Name()
			if m == "Get" {
				c.report(diag.New("E0709", "%s.Get: a page's GET is the page itself", name), fn.Pos()).
					WithHelp("rename the method: Post, Put, Patch and Delete handle those methods on the page's path")
				continue
			}
			if slices.Contains(d.sends[name], m) {
				c.report(diag.New("E0709", "%s.%s is the page's %s handler, and its markup sends it as an event", name, m, strings.ToUpper(m)), fn.Pos()).
					WithHelp("rename the event method (Remove, Save, …): Post, Put, Patch and Delete on a page answer those HTTP methods on its path")
				continue
			}
			if !c.handler(name, fn) {
				continue
			}
			hp := c.r.fset.Position(fn.Pos())
			c.res.routes = append(c.res.routes, &folderRoute{Method: strings.ToUpper(m), Path: path, Page: r.Page + "." + m, Kind: "handler",
				File: hp.Filename, Line: hp.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: r.ctor, handler: m,
				node: node, leaf: leaf, mws: in.mws, wraps: in.wraps})
		}
	}
	for _, mf := range d.funcs {
		fn, ok := scope.Lookup(mf.goName).(*types.Func)
		if !ok {
			continue
		}
		seg := fileSeg(mf.file)
		node, path, params, ok := at(seg, diag.Span{File: filepath.Join(d.dir, mf.file), Line: 1}, fn.Pos())
		if !ok {
			continue
		}
		sig := fn.Type().(*types.Signature)
		c.carriers = append(c.carriers, carrier{sig: sig, params: params, pos: fn.Pos()})
		if in.broken || !c.methodFunc(mf, fn) {
			continue
		}
		var names []string
		for i := 0; i < sig.Params().Len(); i++ {
			names = append(names, sig.Params().At(i).Name())
		}
		hp := c.r.fset.Position(fn.Pos())
		c.res.routes = append(c.res.routes, &folderRoute{Method: mf.method, Path: path, Page: d.name + "." + mf.method, Kind: "endpoint",
			File: hp.Filename, Line: hp.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, fn: mf.goName, names: names,
			node: node, mws: in.mws, wraps: in.wraps})
	}
}

// methodFunc checks a method func's signature: what a handler takes, its
// scalars bound by their names from the path or the query.
func (c *routeChecker) methodFunc(mf methodFunc, fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	bad := func(help, format string, args ...any) bool {
		c.report(diag.New("E0709", "%s (%s) "+format, append([]any{mf.method, mf.file}, args...)...), fn.Pos()).WithHelp("%s", help)
		return false
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
	return true
}

// buildsPtr reports whether ctor returns *t or (*t, error).
func buildsPtr(ctor *types.Func, t *types.Named) bool {
	res := ctor.Type().(*types.Signature).Results()
	if res.Len() == 0 || res.Len() > 2 || res.Len() == 2 && !isError(res.At(1).Type()) {
		return false
	}
	p, ok := res.At(0).Type().(*types.Pointer)
	return ok && types.Identical(p.Elem(), t)
}

// handler checks a method handler's signature against what web binds: a
// context, the request and response, provided dependencies, and one struct
// from the body; it returns nothing, error, T or (T, error).
func (c *routeChecker) handler(page string, fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	bad := func(help, format string, args ...any) bool {
		c.report(diag.New("E0709", "%s.%s "+format, append([]any{page, fn.Name()}, args...)...), fn.Pos()).WithHelp("%s", help)
		return false
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
	return true
}

func isContext(t types.Type) bool { return isNamed(t, "context", "Context", false) }

func isError(t types.Type) bool { return types.Identical(t, types.Universe.Lookup("error").Type()) }

// isNamed reports whether t is the named type path.name, or a pointer to it.
func isNamed(t types.Type, path, name string, ptr bool) bool {
	if p, ok := t.(*types.Pointer); ok && ptr {
		t = p.Elem()
	} else if ptr {
		return false
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func scalarParam(t types.Type) bool {
	if hasMethod(types.NewPointer(t), "UnmarshalText") {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsFloat|types.IsString|types.IsBoolean) != 0
	case *types.Pointer:
		return scalarParam(u.Elem())
	case *types.Slice:
		return scalarParam(u.Elem())
	}
	return false
}

func structParam(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Exported() {
			return true
		}
	}
	return false
}

// isPointer reports whether t is a pointer: a pointer to a struct is a
// provided dependency (a service) or, when nothing provides it, the body;
// only the container knows which, so it is never a second body here.
func isPointer(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}

func isPtrOrIface(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}

func lookupType(pkg *types.Package, name string) types.Type {
	if obj, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
		return obj.Type()
	}
	return nil
}

// firstPos is where a package's first declaration is, for a diagnostic
// about the package.
func firstPos(pkg *types.Package) token.Pos {
	best := token.NoPos
	for _, name := range pkg.Scope().Names() {
		if p := pkg.Scope().Lookup(name).Pos(); p.IsValid() && (!best.IsValid() || p < best) {
			best = p
		}
	}
	return best
}

// kebab is a page type's URL segment: ResetPassword is reset-password.
func kebab(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) && i > 0 && (!unicode.IsUpper(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func goVersionOf(root string) string {
	if v := goDirectiveOf(root); v != "" {
		return "go" + v
	}
	return ""
}

// goIdent is a parameter's name as a Go identifier, initialisms upper case:
// id is ID, petId PetID, org Org.
func goIdent(name string) string {
	var words []string
	start := 0
	rs := []rune(name)
	for i := 1; i <= len(rs); i++ {
		if i == len(rs) || rs[i] == '_' || rs[i] == '-' || unicode.IsUpper(rs[i]) && !unicode.IsUpper(rs[i-1]) {
			if w := strings.Trim(string(rs[start:i]), "_-"); w != "" {
				words = append(words, w)
			}
			start = i
		}
	}
	var b strings.Builder
	for _, w := range words {
		if initialisms[strings.ToUpper(w)] {
			b.WriteString(strings.ToUpper(w))
		} else {
			b.WriteString(upperFirst(w))
		}
	}
	return b.String()
}

// initialisms are the words Go writes in one case.
var initialisms = map[string]bool{"ID": true, "URL": true, "URI": true, "API": true, "UUID": true, "HTTP": true, "JSON": true, "SQL": true,
	"HTML": true, "IP": true, "UID": true, "XML": true, "CSS": true, "DNS": true}
