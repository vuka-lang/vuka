package main

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
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
	facts   *typeFact // the page's or endpoint's
}

// wrap is a guard or a layout over a folder's routes.
type wrap struct {
	dir   *routeDir
	guard bool
}

// dirInfo is what a page directory's facts make it.
type dirInfo struct {
	facts  *pkgFacts
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
	typ  *goType // nil when nothing binds it well
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
	binds  []bindFact
	params []string // the keys of its path's parameters
	prefer string   // the key of the parameter whose folder's layout it is
}

// check builds the route table into res from the page packages' facts.
func (t *routeTree) check(res *routesResult) {
	t0 := time.Now()
	c := &routeChecker{t: t, res: res, info: map[*routeDir]*dirInfo{}, params: map[string]*pathParam{}, byPath: map[string]*routeDir{}}
	for _, d := range t.dirs {
		c.byPath[d.importPath] = d
	}
	for _, d := range t.dirs {
		c.dir(d, t.facts[d.importPath])
	}
	for _, rt := range res.routes {
		if len(rt.Layouts) > 0 {
			rt.Root = rt.Layouts[0]
		}
	}
	c.typeParams()
	c.urlParams()
	c.conflicts()
	t.info = c.info
	t.urls = c.urlTree()
	traceStep("route check: %d page packages assembled %v", len(t.dirs), time.Since(t0))
}

// routeChecker builds the table from the page packages' facts.
type routeChecker struct {
	t        *routeTree
	res      *routesResult
	info     map[*routeDir]*dirInfo
	params   map[string]*pathParam
	carriers []carrier
	byPath   map[string]*routeDir
}

// reportAt reports d at a span.
func (c *routeChecker) reportAt(d *diag.Diagnostic, at diag.Span) *diag.Diagnostic {
	d.Span = at
	c.res.diags = append(c.res.diags, *d)
	return &c.res.diags[len(c.res.diags)-1]
}

// reportBad reports a diagnostic the facts hold.
func (c *routeChecker) reportBad(d *diag.Diagnostic) *diag.Diagnostic {
	return c.reportAt(cloneDiag(d), d.Span)
}

func cloneDiag(d *diag.Diagnostic) *diag.Diagnostic {
	cp := *d
	cp.Related = slices.Clone(d.Related)
	return &cp
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
	if name == "" {
		return rel
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

func (c *routeChecker) dir(d *routeDir, f *pkgFacts) {
	in := &dirInfo{facts: f, node: urlRel(d.rel)}
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
	if f == nil {
		return
	}
	if below {
		c.reportAt(diag.New("E0703", "pages/%s is below a catch-all folder", d.rel), f.First).
			WithHelp("a [path...] folder takes the rest of the path: it has no subfolders")
		in.broken = true
	}
	if f.Routes != nil && d.rel == "" {
		c.reportAt(diag.New("E0706", "%s.Routes is declared, and vuka declares it in pages/route.go: the option registering the folder routes", d.name), *f.Routes).
			WithHelp("rename %s", "Routes")
	}
	// A params.go folder and a layout.go Layout are 1.x's: each is an error
	// naming the one-way form.
	if f.Params != nil {
		help := "rename the folder to [" + strings.ToLower(filepath.Base(d.dir)) + "]/ and declare the parameter's field on the structs that read it"
		c.reportAt(diag.New("E0714", "%s.Params: a parameter folder is a bracketed folder now, and Params binds nothing", d.name), *f.Params).WithHelp("%s", help)
	}
	if f.OldLayout != nil && d.layout == "" {
		help := "declare the layout's struct and markup in _layout.vuka.html"
		if d.oldLayout != "" {
			help = "rename " + d.oldLayout + " to _layout.vuka.html"
		}
		c.reportAt(diag.New("E0715", "%s.Layout: a folder's layout is its _layout.vuka.html now, and this Layout wraps nothing", d.name), *f.OldLayout).WithHelp("%s", help)
	}
	if mw := f.Middleware; mw != nil {
		if mw.Bad == nil {
			in.mws = append(in.mws, d)
		} else {
			c.reportBad(mw.Bad)
			in.broken = true
		}
	}
	prefer := ""
	if d.rel != "" && load.Bracketed(filepath.Base(d.dir)) {
		prefer = in.node
	}
	if g := f.Guard; g != nil {
		if g.Bad == nil {
			in.wraps = append(in.wraps, wrap{d, true})
			c.carriers = append(c.carriers, carrier{binds: g.Binds, params: in.params})
		} else {
			c.reportBad(g.Bad)
			in.broken = true
		}
	}
	if l := f.Layout; d.layout != "" && l != nil {
		if l.Bad == nil {
			d.reloads = c.reads(l.Reads, map[string]bool{})
			in.wraps = append(in.wraps, wrap{d, false})
			c.carriers = append(c.carriers, carrier{binds: l.Binds, params: in.params, prefer: prefer})
		} else {
			c.reportBad(l.Bad)
			in.broken = true
		}
	}
	c.pages(d, in)
}

// reads reports whether a function reading r reads the request, through
// the page packages it calls too.
func (c *routeChecker) reads(r readFact, seen map[string]bool) bool {
	if r.Reads != "" {
		return true
	}
	for _, fn := range r.Via {
		if seen[fn] {
			continue
		}
		seen[fn] = true
		i := strings.LastIndexByte(fn, '.')
		d := c.byPath[fn[:i]]
		if d == nil || c.t.facts[d.importPath] == nil {
			continue
		}
		if g, ok := c.t.facts[d.importPath].Readers[fn[i+1:]]; ok && c.reads(g, seen) {
			return true
		}
	}
	return false
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
		var uses []bindFact
		for _, cr := range c.carriers {
			if !slices.Contains(cr.params, key) {
				continue
			}
			i := slices.IndexFunc(cr.binds, func(b bindFact) bool { return b.Param == p.name })
			if i < 0 {
				continue
			}
			if cr.prefer == key {
				uses = append([]bindFact{cr.binds[i]}, uses...)
			} else {
				uses = append(uses, cr.binds[i])
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
			if u.Typ != first.Typ {
				c.reportAt(diag.New("E0712", "%s is a %s, and %s binding {%s} too is a %s", u.What, u.Typ, first.What, p.name, first.Typ), u.Pos).
					WithRelated(first.Pos).WithHelp("give every field binding {%s} one type", p.name)
				bad = true
			}
		}
		k, ok := first.Kind, first.KindOK
		switch {
		case !ok:
			c.reportAt(diag.New("E0705", "%s is a %s, which the path parameter {%s} can't be", first.What, first.Typ, p.name), first.Pos).
				WithHelp("use an integer, a string, web.Rest, or a type implementing encoding.TextMarshaler and TextUnmarshaler")
			bad = true
		case p.rest && k != paramRest && k != paramString:
			c.reportAt(diag.New("E0705", "%s is a %s, and {%s...} takes the rest of the path", first.What, first.Typ, p.name), first.Pos).
				WithHelp("declare it as web.Rest or string")
			bad = true
		case !p.rest && k == paramRest:
			c.reportAt(diag.New("E0705", "%s is a web.Rest, and {%s} is one segment", first.What, p.name), first.Pos).
				WithHelp("name the parameter [%s...] to take the rest of the path, or declare the field a string", p.name)
			bad = true
		}
		if !bad && first.Pkg != "" && c.byPath[first.Pkg] != nil {
			c.reportAt(diag.New("E0705", "%s is a %s, declared in a page package, which the package urls can't import", first.What, first.Typ), first.Pos).
				WithHelp("declare the type outside pages/, in a package the pages and urls both import")
			bad = true
		}
		if p.rest {
			k = paramRest
		}
		typ := first.Go
		p.p = urlParam{name: p.name, typ: &typ, kind: k}
		if bad {
			p.p.typ = nil
		}
	}
}

// handlerMethods are the methods of a page type that handle other HTTP
// methods on its path, in the order they register.
var handlerMethods = []string{"Post", "Put", "Patch", "Delete"}

// renameHandler is the help for a method a page's markup sends or pushes
// under a name that also answers an HTTP method.
const renameHandler = "rename the method (Remove, Save, …): Post, Put, Patch and Delete on a page answer those HTTP methods on its path"

// pages finds a folder's pages, their method handlers and its method funcs.
func (c *routeChecker) pages(d *routeDir, in *dirInfo) {
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
	at := func(seg string, span diag.Span, pos diag.Span) (node string, path string, params []string, ok bool) {
		node, params = in.node, in.params
		segs := in.segs
		switch {
		case seg == "":
		case in.rest:
			c.reportAt(diag.New("E0703", "pages/%s: a route below the catch-all parameter {%s...}", joinRel(d.rel, seg), c.params[in.params[len(in.params)-1]].name), pos).
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
	for i := range in.facts.Types {
		tf := &in.facts.Types[i]
		name := tf.Name
		if d.notPage(name) || c.t.registered[name] || c.t.registered[d.importPath+"."+name] {
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
			file = tf.Pos.File
		}
		node, path, params, ok := at(seg, diag.Span{File: file, Line: 1}, tf.Pos)
		if !ok {
			continue
		}
		if leaf != "" {
			node = in.node
		}
		in.pages = append(in.pages, name)
		c.carriers = append(c.carriers, carrier{binds: tf.Binds, params: params})
		if in.broken {
			continue
		}
		if !tf.Page {
			for _, m := range tf.Methods {
				if m.Bad != nil {
					c.reportBad(m.Bad)
					continue
				}
				c.res.routes = append(c.res.routes, &folderRoute{Method: strings.ToUpper(m.Name), Path: path, Page: d.name + "." + name + "." + m.Name,
					Kind: "endpoint", File: m.Pos.File, Line: m.Pos.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: tf.Ctor,
					handler: m.Name, node: node, leaf: leaf, mws: in.mws, wraps: in.wraps, facts: tf})
			}
			continue
		}
		r := &folderRoute{Method: "GET", Path: path, Page: d.name + "." + name, Kind: "page", File: tf.Pos.File, Line: tf.Pos.Line,
			Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: tf.Ctor, node: node, leaf: leaf, mws: in.mws, wraps: in.wraps, facts: tf}
		if tf.Live {
			r.Kind = "live"
		}
		c.res.routes = append(c.res.routes, r)
		if tf.LoadUnused != nil {
			c.reportAt(diag.Warn("W0704", "%s.Load never runs: a page has no Load, only layouts and guards do", name), *tf.LoadUnused).
				WithHelp("use a guard or the folder's _layout for what guards the page, or a @ui.Mount hook on a live page; on a page that isn't live, load in the markup")
		}
		for _, fm := range tf.Methods {
			m := fm.Name
			if m == "Get" {
				c.reportAt(diag.New("E0709", "%s.Get: a page's GET is the page itself", name), fm.Pos).
					WithHelp("rename the method: Post, Put, Patch and Delete handle those methods on the page's path")
				continue
			}
			if slices.Contains(d.sends[name], m) {
				c.reportAt(diag.New("E0709", "%s.%s is the page's %s handler, and its markup sends it as an event", name, m, strings.ToUpper(m)), fm.Pos).
					WithHelp("rename the event method (Remove, Save, …): Post, Put, Patch and Delete on a page answer those HTTP methods on its path")
				continue
			}
			pushed := slices.Contains(d.pushes[name], m)
			if fm.Bad != nil {
				if b := c.reportBad(fm.Bad); pushed {
					b.Help = renameHandler
				}
				continue
			}
			if pushed {
				c.reportAt(diag.New("E0709", "%s.%s is the page's %s handler, and its markup pushes it: it answers HTTP %[3]s on the page's path as well", name, m, strings.ToUpper(m)), fm.Pos).
					WithHelp(renameHandler)
				continue
			}
			c.res.routes = append(c.res.routes, &folderRoute{Method: strings.ToUpper(m), Path: path, Page: r.Page + "." + m, Kind: "handler",
				File: fm.Pos.File, Line: fm.Pos.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, typ: name, ctor: r.ctor, handler: m,
				node: node, leaf: leaf, mws: in.mws, wraps: in.wraps, facts: tf})
		}
	}
	for _, mf := range d.funcs {
		i := slices.IndexFunc(in.facts.Funcs, func(f funcFact) bool { return f.GoName == mf.goName })
		if i < 0 {
			continue
		}
		ff := in.facts.Funcs[i]
		seg := fileSeg(mf.file)
		node, path, params, ok := at(seg, diag.Span{File: filepath.Join(d.dir, mf.file), Line: 1}, ff.Pos)
		if !ok {
			continue
		}
		c.carriers = append(c.carriers, carrier{binds: ff.Binds, params: params})
		if in.broken {
			continue
		}
		if ff.Bad != nil {
			c.reportBad(ff.Bad)
			continue
		}
		c.res.routes = append(c.res.routes, &folderRoute{Method: mf.method, Path: path, Page: d.name + "." + mf.method, Kind: "endpoint",
			File: ff.Pos.File, Line: ff.Pos.Line, Middleware: mws, Guards: guards, Layouts: layouts, dir: d, fn: mf.goName, names: ff.Names,
			node: node, mws: in.mws, wraps: in.wraps})
	}
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
