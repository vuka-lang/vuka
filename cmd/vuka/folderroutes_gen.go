package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

// generate writes each page package's routes_gen.go and pages_gen.go.
func (t *routeTree) generate(routes []*folderRoute) []load.Generated {
	var out []load.Generated
	for _, d := range t.dirs {
		if in := t.info[d]; in != nil && len(in.pages) > 0 {
			out = append(out, load.Generated{Target: filepath.Join(d.dir, routesGoName), Src: t.urlFuncs(d, in)})
		}
	}
	// pages_gen.go once there is a route, or to replace one written earlier.
	if target := filepath.Join(t.root, pagesDirName, pagesGoName); len(routes) > 0 || fileExists(target) {
		out = append(out, load.Generated{Target: target, Src: t.registrations(routes)})
	}
	return out
}

// imports names the packages a generated file imports, none of them
// taken by a name the file's package declares or by another import.
type imports struct {
	taken  map[string]bool
	byPath map[string]string
	self   string
}

func newImports(self string, scope *types.Scope, reserved ...string) *imports {
	im := &imports{taken: map[string]bool{}, byPath: map[string]string{}, self: self}
	if scope != nil {
		for _, n := range scope.Names() {
			if !strings.HasSuffix(n, "URL") {
				im.taken[n] = true
			}
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

func (im *imports) qualifier(p *types.Package) string { return im.name(p.Path(), p.Name()) }

// block is the file's import declaration, sorted, standard library first.
func (im *imports) block() string {
	if len(im.byPath) == 0 {
		return ""
	}
	var std, other []string
	for path, n := range im.byPath {
		line := fmt.Sprintf("%q", path)
		if base := path[strings.LastIndexByte(path, '/')+1:]; base != n {
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

// urlFuncs is a page package's routes_gen.go: <Page>URL for each page.
func (t *routeTree) urlFuncs(d *routeDir, in *dirInfo) []byte {
	var names []string
	for _, p := range in.params {
		names = append(names, p.name)
	}
	im := newImports(d.importPath, in.pkg.Scope(), names...)
	var body bytes.Buffer
	args := make([]string, len(in.params))
	for i, p := range in.params {
		args[i] = paramIdent(p.name) + " " + types.TypeString(p.typ, im.qualifier)
	}
	for _, page := range in.pages {
		fmt.Fprintf(&body, "\n// %sURL is the path of the page %s.\nfunc %sURL(%s) string {\n", page, page, page, strings.Join(args, ", "))
		var parts []string
		lit := ""
		pi := 0
		segs := in.segs
		if page != "Index" {
			segs = append(slices.Clone(segs), kebab(page))
		}
		for _, s := range segs {
			if !strings.HasPrefix(s, "{") {
				lit += "/" + s
				continue
			}
			p := in.params[pi]
			pi++
			parts = append(parts, fmt.Sprintf("%q", lit+"/"), t.segment(&body, im, p, pi))
			lit = ""
		}
		if lit != "" || len(parts) == 0 {
			if lit == "" {
				lit = "/"
			}
			parts = append(parts, fmt.Sprintf("%q", lit))
		}
		fmt.Fprintf(&body, "\treturn %s\n}\n", strings.Join(parts, " + "))
	}
	var types []string
	for _, name := range d.templates {
		if slices.Contains(in.pages, name) {
			types = append(types, name)
		}
	}
	src := routesHeading + "package " + d.name + "\n\n" + im.block() + strings.TrimPrefix(d.templateTypes(types)+body.String(), "\n")
	return gofmt(src)
}

// segment is the expression writing a path parameter into the path,
// statements it needs written to body first.
func (t *routeTree) segment(body *bytes.Buffer, im *imports, p urlParam, n int) string {
	v := paramIdent(p.name)
	switch p.kind {
	case paramInt, paramUint:
		b := p.typ.Underlying().(*types.Basic)
		fn, conv := "FormatInt", "int64"
		if p.kind == paramUint {
			fn, conv = "FormatUint", "uint64"
		}
		sc := im.name("strconv", "strconv")
		switch {
		case b.Kind() == types.Int && types.Identical(p.typ, types.Typ[types.Int]):
			return sc + ".Itoa(" + v + ")"
		case types.Identical(p.typ, types.Typ[types.Int64]) && p.kind == paramInt, types.Identical(p.typ, types.Typ[types.Uint64]):
			return sc + "." + fn + "(" + v + ", 10)"
		}
		return sc + "." + fn + "(" + conv + "(" + v + "), 10)"
	case paramText:
		text := fmt.Sprintf("text%d", n)
		fmt.Fprintf(body, "\t%s, _ := %s.MarshalText()\n", text, v)
		return im.name("net/url", "url") + ".PathEscape(string(" + text + "))"
	case paramRest:
		u, s := im.name("net/url", "url"), im.name("strings", "strings")
		fmt.Fprintf(body, "\tsegs := %s.Split(string(%s), \"/\")\n\tfor i, seg := range segs {\n\t\tsegs[i] = %s.PathEscape(seg)\n\t}\n", s, v, u)
		return s + `.Join(segs, "/")`
	}
	if types.Identical(p.typ, types.Typ[types.String]) {
		return im.name("net/url", "url") + ".PathEscape(" + v + ")"
	}
	return im.name("net/url", "url") + ".PathEscape(string(" + v + "))"
}

// paramIdent is a path parameter's name as a Go parameter.
func paramIdent(name string) string {
	switch name {
	case "segs", "seg", "i":
		return name + "_"
	}
	if token.Lookup(name).IsKeyword() {
		return name + "_"
	}
	return name
}

// registrations is pages/pages_gen.go: every page, its handlers and their
// layouts registered with web.Routes.
func (t *routeTree) registrations(routes []*folderRoute) []byte {
	self := t.modPath + "/" + pagesDirName
	var scope *types.Scope
	pkgName := pagesDirName
	for _, d := range t.dirs {
		if d.rel == "" {
			pkgName = d.name
			if t.info[d] != nil && t.info[d].pkg != nil {
				scope = t.info[d].pkg.Scope()
			}
		}
	}
	im := newImports(self, scope)
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
			name = strings.NewReplacer("/", "_", "-", "_", ".", "_").Replace(d.rel)
		}
		if n := im.name(d.importPath, name); n != "" {
			return n + "."
		}
		return ""
	}
	sorted := sortedRoutes(routes)
	var body strings.Builder
	for _, r := range sorted {
		q := qual(r.dir)
		var opts []string
		if r.Kind == "handler" {
			opts = append(opts, web+".Receiver("+q+r.ctor+")")
		} else {
			opts = append(opts, web+".PageRoute()")
		}
		for _, l := range r.layouts {
			opts = append(opts, fmt.Sprintf("%s.LayoutOf[%sLayout]()", web, qual(l)))
		}
		call, fn := "Get", q+r.ctor
		switch {
		case r.Kind == "live":
			call = "Live"
		case r.Kind == "handler":
			call, fn = r.handler, "(*"+q+r.typ+")."+r.handler
		}
		fmt.Fprintf(&body, "\t%s.Routes.%s(%q, %s, %s)\n", web, call, r.Path, fn, strings.Join(opts, ", "))
	}
	src := routesHeading + "// Every page under pages/, registered with its method handlers and layouts.\n\npackage " + pkgName + "\n\n"
	if len(sorted) == 0 {
		return gofmt(src)
	}
	return gofmt(src + im.block() + "func init() {\n" + body.String() + "}\n")
}

func gofmt(src string) []byte {
	b, err := format.Source([]byte(src))
	if err != nil {
		return []byte(src)
	}
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
