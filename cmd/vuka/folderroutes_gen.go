package main

import (
	"fmt"
	"go/format"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// generate writes pages/urls/urls_gen.go, pages/route.go and each page
// package's vuka_route_gen.go; each also when one written earlier is on
// disk, to replace it.
func (t *routeTree) generate(routes []*folderRoute) []load.Generated {
	var out []load.Generated
	if target := filepath.Join(t.urlsDir(), urlsGoName); len(routes) > 0 || fileExists(target) {
		out = append(out, load.Generated{Target: target, Src: t.urlsFile()})
	}
	if target := filepath.Join(t.pages, routeGoName); len(routes) > 0 || fileExists(target) {
		out = append(out, load.Generated{Target: target, Src: t.tree(routes)})
	}
	byDir := map[*routeDir][]*folderRoute{}
	for _, r := range routes {
		byDir[r.dir] = append(byDir[r.dir], r)
	}
	for _, d := range t.dirs {
		target := filepath.Join(d.goDir, load.RouteGoName)
		if f := t.facts[d.importPath]; f != nil && (len(byDir[d]) > 0 || fileExists(target)) {
			out = append(out, load.Generated{Target: target, Src: t.packageRoutes(d, f, byDir[d])})
		}
	}
	return out
}

// generateURLs is generate's urls/urls_gen.go alone.
func (t *routeTree) generateURLs() []load.Generated {
	return []load.Generated{{Target: filepath.Join(t.urlsDir(), urlsGoName), Src: t.urlsFile()}}
}

// urlsNamable reports whether urls_gen.go can be written for a tree with the
// errors diags: it can unless two names of a folder are alike.
func (t *routeTree) urlsNamable(diags []diag.Diagnostic) bool {
	return t.urls != nil && t.urls.prune() && !slices.ContainsFunc(diags, func(d diag.Diagnostic) bool { return d.Code == "E0706" })
}

// imports names the packages a generated file imports, none of them
// taken by a name the file's package declares or by another import.
type imports struct {
	taken  map[string]bool
	byPath map[string]string
	self   string
}

func newImports(self string, scope []string, reserved ...string) *imports {
	im := &imports{taken: map[string]bool{}, byPath: map[string]string{}, self: self}
	for _, n := range scope {
		if !strings.HasSuffix(n, "URL") {
			im.taken[n] = true
		}
	}
	for _, n := range reserved {
		im.taken[n] = true
	}
	return im
}

// name is how the file refers to the package path, named name.
func (im *imports) name(path, name string) string {
	if path == im.self {
		return ""
	}
	if n, ok := im.byPath[path]; ok {
		return n
	}
	n := name
	for i := 2; im.taken[n] || token.Lookup(n).IsKeyword(); i++ {
		n = fmt.Sprintf("%s%d", name, i)
	}
	im.taken[n] = true
	im.byPath[path] = n
	return n
}

// block is the file's import declaration, sorted, standard library first.
func (im *imports) block() string {
	if len(im.byPath) == 0 {
		return ""
	}
	var std, other []string
	for path, n := range im.byPath {
		line := fmt.Sprintf("%q", path)
		if base := transpile.ImportName(path); base != n {
			line = n + " " + line
		}
		if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			other = append(other, line)
		} else {
			std = append(std, line)
		}
	}
	sortImports := func(ls []string) {
		slices.SortFunc(ls, func(a, b string) int { return strings.Compare(unaliased(a), unaliased(b)) })
	}
	sortImports(std)
	sortImports(other)
	if len(std)+len(other) == 1 {
		return "import " + slices.Concat(std, other)[0] + "\n\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, l := range std {
		b.WriteString("\t" + l + "\n")
	}
	if len(std) > 0 && len(other) > 0 {
		b.WriteString("\n")
	}
	for _, l := range other {
		b.WriteString("\t" + l + "\n")
	}
	b.WriteString(")\n\n")
	return b.String()
}

func unaliased(line string) string {
	if i := strings.IndexByte(line, '"'); i >= 0 {
		return line[i:]
	}
	return line
}

// segment is the expression writing a path parameter into the path,
// statements it needs written to body first.
func (t *routeTree) segment(body *strings.Builder, im *imports, p urlParam) string {
	v := paramIdent(p.name)
	switch p.kind {
	case paramInt, paramUint:
		fn, conv := "FormatInt", "int64"
		if p.kind == paramUint {
			fn, conv = "FormatUint", "uint64"
		}
		sc := im.name("strconv", "strconv")
		switch {
		case p.typ.plain("int"):
			return sc + ".Itoa(" + v + ")"
		case p.typ.plain("int64") && p.kind == paramInt, p.typ.plain("uint64"):
			return sc + "." + fn + "(" + v + ", 10)"
		}
		return sc + "." + fn + "(" + conv + "(" + v + "), 10)"
	case paramText:
		fmt.Fprintf(body, "\ttext, _ := %s.MarshalText()\n", v)
		return im.name("net/url", "url") + ".PathEscape(string(text))"
	case paramRest:
		u, s := im.name("net/url", "url"), im.name("strings", "strings")
		fmt.Fprintf(body, "\tsegs := %s.Split(string(%s), \"/\")\n\tfor i, seg := range segs {\n\t\tsegs[i] = %s.PathEscape(seg)\n\t}\n", s, v, u)
		return s + `.Join(segs, "/")`
	}
	if p.typ.plain("string") {
		return im.name("net/url", "url") + ".PathEscape(" + v + ")"
	}
	return im.name("net/url", "url") + ".PathEscape(string(" + v + "))"
}

// paramIdent is a path parameter's name as a Go parameter.
func paramIdent(name string) string {
	switch name {
	case "segs", "seg", "i", "u", "text":
		return name + "_"
	}
	if token.Lookup(name).IsKeyword() {
		return name + "_"
	}
	return name
}

// tree is pages/route.go: func Routes, the option registering every page
// package's routes on the app with the middleware, guards and layouts of
// its folders. It names no page, so it changes only with the tree.
func (t *routeTree) tree(routes []*folderRoute) []byte {
	root := t.root0()
	var scope []string
	if f := t.facts[root.importPath]; f != nil {
		scope = f.Scope
	}
	im := newImports(t.pagesPath, scope, "app", "Routes")
	web := im.name(webPath, "web")
	// Packages named alike are told apart by their folders.
	seen := map[string]map[string]bool{}
	for _, d := range t.dirs {
		if seen[d.name] == nil {
			seen[d.name] = map[string]bool{}
		}
		seen[d.name][d.importPath] = true
	}
	qual := func(d *routeDir) string {
		name := d.name
		if len(seen[d.name]) > 1 && d.rel != "" {
			name = strings.NewReplacer("/", "_", "-", "_", ".", "_", "[", "", "]", "", "(", "", ")", "").Replace(d.rel)
		}
		if n := im.name(d.importPath, name); n != "" {
			return n + "."
		}
		return ""
	}
	register := "vukaRoute"
	for i := 2; slices.Contains(scope, register); i++ {
		register = fmt.Sprintf("vukaRoute%d", i)
	}
	routed := map[*routeDir]*folderRoute{}
	funcs := false
	for _, r := range routes {
		if routed[r.dir] == nil {
			routed[r.dir] = r
		}
		funcs = funcs || r.fn != ""
	}
	var body strings.Builder
	for _, d := range t.dirs {
		r := routed[d]
		if r == nil {
			continue
		}
		q := qual(d)
		opts := []string{"app"}
		for _, m := range r.mws {
			opts = append(opts, fmt.Sprintf("%s.UseOf(%sMiddleware)", web, qual(m)))
		}
		for _, w := range r.wraps {
			if w.guard {
				opts = append(opts, fmt.Sprintf("%s.GuardOf[%sGuard]()", web, qual(w.dir)))
			} else {
				opts = append(opts, fmt.Sprintf("%s.LayoutOf[%s%s]()", web, qual(w.dir), w.dir.layout))
				if w.dir.reloads {
					opts = append(opts, fmt.Sprintf("%s.ReloadLayout[%s%s]()", web, qual(w.dir), w.dir.layout))
				}
			}
		}
		fmt.Fprintf(&body, "\t\t%s%s(%s(%s))\n", q, pageRoutesFunc, register, strings.Join(opts, ", "))
	}
	params := ""
	if funcs {
		// A scalar of a method func binds the path parameter or query value
		// of its name: the names are recorded as the compiler records a
		// registered handler's, vuka.RegisterParams.
		params = "\t\tcase \"func\":\n\t\t\tif len(params) > 0 {\n\t\t\t\t" + im.name(vukaModPath, "vuka") + ".RegisterParams(fn, params...)\n\t\t\t}\n"
	}
	src := routesHeading + "package " + root.name + "\n\n" + im.block() +
		"// Routes registers every page and endpoint under pages/ on the app, with\n// its middleware, guards and layouts: web.New(pages.Routes(), …).\n" +
		"func Routes() " + web + ".Option {\n\treturn " + web + ".OptionFunc(func(app *" + web + ".App) {\n" + body.String() + "\t})\n}\n\n" +
		"// " + register + " registers on app each route a page package's " + pageRoutesFunc + " gives,\n// with opts after the route's own.\n//\n//go:noinline\n" +
		"func " + register + "(app *" + web + ".App, opts ..." + web + ".RouteOption) func(kind, method, path string, fn, ctor any, params ...string) {\n" +
		"\treturn func(kind, method, path string, fn, ctor any, params ...string) {\n" +
		"\t\topt := " + web + ".PageRoute()\n\t\tswitch kind {\n" +
		"\t\tcase \"live\":\n\t\t\tapp.Live(path, fn, append([]" + web + ".RouteOption{opt}, opts...)...)\n\t\t\treturn\n" +
		"\t\tcase \"handler\":\n\t\t\topt = " + web + ".Receiver(ctor)\n" + params +
		"\t\t}\n\t\tapp.Handle(method, path, fn, append([]" + web + ".RouteOption{opt}, opts...)...)\n\t}\n}\n"
	return gofmtKept(src)
}

// pageRoutesFunc is the function of a page package's vuka_route_gen.go
// giving its routes.
const pageRoutesFunc = "VukaRoutes"

// packageRoutes is the vuka_route_gen.go of the page package of d: the
// function giving each of its routes to pages/route.go, which registers
// them with the middleware, guards and layouts of its folders. It imports
// nothing: a page package compiles with no more than its own imports.
func (t *routeTree) packageRoutes(d *routeDir, f *pkgFacts, routes []*folderRoute) []byte {
	head := routesHeading + "package " + d.name + "\n\n"
	if len(routes) == 0 {
		return gofmtKept(head)
	}
	var body strings.Builder
	for _, r := range sortedRoutes(routes) {
		ctor := r.ctor
		if r.ctor == "" {
			ctor = fmt.Sprintf("func() *%s { return &%s{} }", r.typ, r.typ)
		}
		switch {
		case r.fn != "":
			// A scalar binds the path parameter of its name, spelt as the
			// path spells it: id for [id] when the Go says ID.
			args := []string{`"func"`, strconv.Quote(r.Method), strconv.Quote(r.Path), r.fn, "nil"}
			for _, n := range r.names {
				for _, seg := range splitPath(r.Path) {
					if p := strings.TrimSuffix(strings.Trim(seg, "{}"), "..."); strings.HasPrefix(seg, "{") && strings.EqualFold(p, n) {
						n = p
					}
				}
				args = append(args, strconv.Quote(n))
			}
			fmt.Fprintf(&body, "\troute(%s)\n", strings.Join(args, ", "))
		case r.handler != "":
			fmt.Fprintf(&body, "\troute(\"handler\", %q, %q, (*%s).%s, %s)\n", r.Method, r.Path, r.typ, r.handler, ctor)
		case r.Kind == "live":
			fmt.Fprintf(&body, "\troute(\"live\", %q, %q, %s, nil)\n", r.Method, r.Path, ctor)
		default:
			fmt.Fprintf(&body, "\troute(\"page\", %q, %q, %s, nil)\n", r.Method, r.Path, ctor)
		}
	}
	where := "pages/"
	if d.rel != "" {
		where += d.rel + "/"
	}
	src := head + "// " + pageRoutesFunc + " gives route each page and endpoint of " + where + ": its kind, method,\n// path, handler, the constructor of a handler's page, and a method func's\n// parameters' names. pages.Routes registers them.\n" +
		"//\n//go:noinline\n" +
		"func " + pageRoutesFunc + "(route func(kind, method, path string, fn, ctor any, params ...string)) {\n" + body.String() + "}\n"
	return gofmtKept(src)
}

func gofmt(src string) []byte {
	b, err := format.Source([]byte(src))
	if err != nil {
		return []byte(src)
	}
	return b
}

// gofmtMemo is gofmt of the files a watch generated before: most of a
// tree's are the same on the next build.
var gofmtMemo = struct {
	sync.Mutex
	m map[string][]byte
}{m: map[string][]byte{}}

func gofmtKept(src string) []byte {
	gofmtMemo.Lock()
	b, ok := gofmtMemo.m[src]
	gofmtMemo.Unlock()
	if ok {
		return b
	}
	b = gofmt(src)
	gofmtMemo.Lock()
	if len(gofmtMemo.m) > 4096 {
		clear(gofmtMemo.m)
	}
	gofmtMemo.m[src] = b
	gofmtMemo.Unlock()
	return b
}

// sortedRoutes is the routes by path, a page before its handlers.
func sortedRoutes(routes []*folderRoute) []*folderRoute {
	order := map[string]int{"GET": 0, "POST": 1, "PUT": 2, "PATCH": 3, "DELETE": 4}
	sorted := slices.Clone(routes)
	slices.SortStableFunc(sorted, func(a, b *folderRoute) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return order[a.Method] - order[b.Method]
	})
	return sorted
}

func splitPath(path string) []string { return strings.Split(strings.TrimPrefix(path, "/"), "/") }
