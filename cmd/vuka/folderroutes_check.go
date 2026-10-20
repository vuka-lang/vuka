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

	"github.com/vuka-lang/vuka/diag"
	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// folderRoute is one route of the table: a page, or one of its method
// handlers.
type folderRoute struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Page    string   `json:"page"` // employer.Edit
	Kind    string   `json:"kind"` // live, page, template or handler
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Layouts []string `json:"layouts,omitempty"`

	dir     *routeDir
	typ     string // the page's type
	ctor    string // its constructor
	handler string // the method, for a handler
	layouts []*routeDir
}

// dirInfo is what the type-checked package of a page directory makes it.
type dirInfo struct {
	pkg     *types.Package
	segs    []string   // the URL's segments down to the folder
	params  []urlParam // the path parameters down to it, in order
	rest    bool       // its parameter is web.Rest
	param   bool       // it is a parameter folder
	paramAt token.Pos  // its Params
	layout  bool
	layouts []*routeDir // the layouts wrapping its pages, outermost first
	pages   []string    // its pages' types, sorted
	broken  bool        // its Params or Layout has an error: no routes
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
		if bp, err := r.ctx.ImportDir(d.dir, 0); err == nil {
			for _, p := range bp.Imports {
				if r.byPath[p] == nil && !slices.Contains(imports, p) {
					imports = append(imports, p)
				}
			}
		}
	}
	_ = r.im.Prefetch(append(imports, load.UIPath))
	c := &routeChecker{t: t, r: r, res: res, info: map[*routeDir]*dirInfo{}}
	if ui, err := r.Import(load.UIPath); err == nil {
		c.node = lookupType(ui, "Node")
		c.stateful = lookupType(ui, "Stateful")
	}
	for _, d := range t.dirs {
		pkg, _ := r.checkDir(d)
		c.dir(d, pkg)
	}
	c.conflicts()
	t.info = c.info
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
	bp, err := r.ctx.ImportDir(d.dir, 0)
	if bp == nil || len(bp.GoFiles) == 0 {
		return nil, fmt.Errorf("%s: %v", d.importPath, err)
	}
	var files []*ast.File
	for _, name := range bp.GoFiles {
		if d.rel == "" && name == pagesGoName {
			continue
		}
		path := filepath.Join(d.dir, name)
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
	pkg, _ := conf.Check(d.importPath, r.fset, files, nil)
	r.done[d.importPath] = pkg
	return pkg, nil
}

// routeChecker builds the table from the checked packages.
type routeChecker struct {
	t              *routeTree
	r              *routeImporter
	res            *routesResult
	info           map[*routeDir]*dirInfo
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

func (c *routeChecker) dir(d *routeDir, pkg *types.Package) {
	in := &dirInfo{pkg: pkg}
	c.info[d] = in
	parent, pin := c.parentOf(d)
	// The folders between the parent's and this one are plain segments.
	from := ""
	if parent != nil {
		in.segs, in.params, in.layouts = slices.Clone(pin.segs), slices.Clone(pin.params), slices.Clone(pin.layouts)
		in.broken = pin.broken
		from = parent.rel
	}
	var between []string
	if d.rel != "" {
		rest := strings.TrimPrefix(strings.TrimPrefix(d.rel, from), "/")
		between = strings.Split(rest, "/")
	}
	if len(between) > 0 {
		in.segs = append(in.segs, between[:len(between)-1]...)
	}
	if pkg == nil {
		if len(between) > 0 {
			in.segs = append(in.segs, between[len(between)-1])
		}
		return
	}
	folder := ""
	if len(between) > 0 {
		folder = between[len(between)-1]
	}
	if obj, ok := pkg.Scope().Lookup("Params").(*types.TypeName); ok && folder != "" {
		in.param, in.paramAt = true, obj.Pos()
		p, ok := c.params(d, obj, folder, pin)
		if !ok {
			in.broken = true
		}
		in.rest = p.kind == paramRest
		in.params = append(in.params, p)
		seg := "{" + folder + "}"
		if in.rest {
			seg = "{" + folder + "...}"
		}
		in.segs = append(in.segs, seg)
	} else if folder != "" {
		in.segs = append(in.segs, folder)
	}
	if obj, ok := pkg.Scope().Lookup("Layout").(*types.TypeName); ok {
		if c.layout(obj) {
			in.layout = true
			in.layouts = append(in.layouts, d)
		} else {
			in.broken = true
		}
	}
	if pin != nil && pin.rest {
		c.report(diag.New("E0703", "%s is below the catch-all folder %s", d.rel, parent.rel), firstPos(pkg)).
			WithHelp("a folder whose parameter is web.Rest takes the rest of the path: it has no subfolders")
		in.broken = true
	}
	c.pages(d, in)
}

// params checks a parameter folder's Params and returns its parameter.
func (c *routeChecker) params(d *routeDir, obj *types.TypeName, folder string, pin *dirInfo) (urlParam, bool) {
	p := urlParam{name: folder}
	bad := func(format string, args ...any) *diag.Diagnostic {
		return c.report(diag.New("E0704", "%s.Params: "+format, append([]any{d.name}, args...)...), obj.Pos())
	}
	if !token.IsIdentifier(folder) {
		bad("the folder %q names a path parameter, and isn't an identifier", folder).WithHelp("name the folder as a Go identifier: employer, permitID")
		return p, false
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok || obj.IsAlias() {
		bad("not a struct").WithHelp("declare type Params struct{ %s int }", upperFirst(folder))
		return p, false
	}
	var parent types.Type
	if pin != nil {
		for _, above := range pin.params {
			if above.name == folder {
				bad("the parameter {%s} is bound above already", folder).WithHelp("name the folder for what it binds here")
				return p, false
			}
		}
		parent = c.parentParams(d)
	}
	var own []*types.Var
	embedsParent := false
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			own = append(own, f)
			continue
		}
		if parent != nil && types.Identical(f.Type(), parent) {
			embedsParent = true
			continue
		}
		c.report(diag.New("E0704", "%s.Params embeds %s, which isn't the parent folder's Params", d.name, types.TypeString(f.Type(), types.RelativeTo(obj.Pkg()))), f.Pos()).
			WithHelp("a Params embeds the Params of the nearest parameter folder above it, and nothing else")
		return p, false
	}
	switch {
	case len(own) != 1:
		bad("%d fields of its own; it has one, the folder's parameter", len(own)).WithHelp("declare one field, named like the folder: %s", upperFirst(folder))
		return p, false
	case !strings.EqualFold(own[0].Name(), folder):
		c.report(diag.New("E0704", "%s.Params.%s: the field is named like its folder, %s", d.name, own[0].Name(), folder), own[0].Pos()).
			WithHelp("rename it %s", upperFirst(folder))
		return p, false
	case parent != nil && !embedsParent:
		bad("doesn't embed the parent folder's Params, %s", types.TypeString(parent, types.RelativeTo(obj.Pkg()))).
			WithHelp("embed it, so the pages here get the parameters above too")
		return p, false
	}
	p.typ = own[0].Type()
	k, ok := paramKindOf(p.typ)
	if !ok {
		c.report(diag.New("E0705", "%s.Params.%s is a %s, which a path parameter can't be", d.name, own[0].Name(), types.TypeString(p.typ, types.RelativeTo(obj.Pkg()))), own[0].Pos()).
			WithHelp("use an integer, a string, web.Rest, or a type implementing encoding.TextMarshaler and TextUnmarshaler")
		return p, false
	}
	p.kind = k
	return p, true
}

// parentParams is the Params type of the nearest parameter folder above d.
func (c *routeChecker) parentParams(d *routeDir) types.Type {
	for p, in := c.parentOf(d); p != nil; p, in = c.parentOf(p) {
		if in.param && in.pkg != nil {
			if obj, ok := in.pkg.Scope().Lookup("Params").(*types.TypeName); ok {
				return obj.Type()
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

// layout checks a folder's Layout.
func (c *routeChecker) layout(obj *types.TypeName) bool {
	bad := func(p token.Pos, help, format string, args ...any) bool {
		c.report(diag.New("E0708", "%s.Layout "+format, append([]any{obj.Pkg().Name()}, args...)...), p).WithHelp("%s", help)
		return false
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok || obj.IsAlias() {
		return bad(obj.Pos(), "declare type Layout struct{ Children ui.Node }, rendered by its template", "isn't a struct")
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
		return bad(obj.Pos(), "give it a template (layout.vuka.html) or a Render method", "isn't a ui.Node: it has nothing to render")
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

// pages finds a folder's pages and their method handlers.
func (c *routeChecker) pages(d *routeDir, in *dirInfo) {
	scope := in.pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || obj.IsAlias() || name == "Layout" || name == "Params" {
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
		ctor, ok := scope.Lookup("New" + name).(*types.Func)
		if !ok || !buildsPtr(ctor, named) {
			continue
		}
		live := c.isLive(named)
		if !live && !c.isNode(named) {
			continue
		}
		if fn, ok := scope.Lookup(name + "URL").(*types.Func); ok && !generatedRoutesFile(c.r.fset, fn.Pos()) {
			c.report(diag.New("E0706", "%sURL is declared, and vuka declares it for the page %s", name, name), fn.Pos()).
				WithHelp("rename the function: vuka writes %sURL in %s", name, routesGoName).WithRelated(c.span(obj.Pos()))
			continue
		}
		seg := ""
		if name != "Index" {
			seg = kebab(name)
			if in.rest {
				c.report(diag.New("E0703", "%s.%s: a page below the catch-all parameter {%s...}", d.name, name, in.params[len(in.params)-1].name), obj.Pos()).
					WithHelp("a catch-all folder serves its Index alone: move %s up a folder", name)
				continue
			}
		}
		in.pages = append(in.pages, name)
		if in.broken {
			continue
		}
		segs := in.segs
		if seg != "" {
			segs = append(slices.Clone(segs), seg)
		}
		path := "/" + strings.Join(segs, "/")
		pos := c.r.fset.Position(obj.Pos())
		r := &folderRoute{Method: "GET", Path: path, Page: d.name + "." + name, Kind: "page", File: pos.Filename, Line: pos.Line,
			dir: d, typ: name, ctor: "New" + name, layouts: in.layouts}
		switch {
		case live:
			r.Kind = "live"
		case slices.Contains(d.templates, name) && generatedRoutesFile(c.r.fset, obj.Pos()):
			r.Kind, r.File, r.Line = "template", filepath.Join(d.dir, d.templateFiles[slices.Index(d.templates, name)]), 1
		}
		for _, l := range in.layouts {
			r.Layouts = append(r.Layouts, l.name+".Layout")
		}
		c.res.routes = append(c.res.routes, r)
		ms := types.NewMethodSet(types.NewPointer(named))
		for _, m := range append([]string{"Get"}, handlerMethods...) {
			sel := ms.Lookup(nil, m)
			if sel == nil || len(sel.Index()) != 1 {
				continue
			}
			fn := sel.Obj().(*types.Func)
			if m == "Get" {
				c.report(diag.New("E0709", "%s.Get: a page's GET is the page itself", name), fn.Pos()).
					WithHelp("rename the method: Post, Put, Patch and Delete handle those methods on the page's path")
				continue
			}
			if !c.handler(name, fn) {
				continue
			}
			hp := c.r.fset.Position(fn.Pos())
			c.res.routes = append(c.res.routes, &folderRoute{Method: strings.ToUpper(m), Path: path, Page: r.Page + "." + m, Kind: "handler",
				File: hp.Filename, Line: hp.Line, Layouts: r.Layouts, dir: d, typ: name, ctor: r.ctor, handler: m, layouts: in.layouts})
		}
	}
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
		case structParam(t):
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

func isPtrOrIface(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}

// generatedRoutesFile reports whether pos is in a routes_gen.go file or a
// stub: what vuka declares.
func generatedRoutesFile(fset *token.FileSet, pos token.Pos) bool {
	name := filepath.Base(fset.PositionFor(pos, false).Filename)
	return name == routesGoName || name == transpile.GoName(routesStub)
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
