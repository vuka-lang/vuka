package main

import (
	"fmt"
	"go/format"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

// generate writes pages/urls/urls_gen.go and pages/route.go; each also
// when one written earlier is on disk, to replace it.
func (t *routeTree) generate(routes []*folderRoute) []load.Generated {
	var out []load.Generated
	if target := filepath.Join(t.urlsDir(), urlsGoName); len(routes) > 0 || fileExists(target) {
		out = append(out, load.Generated{Target: target, Src: t.urlsFile()})
	}
	if target := filepath.Join(t.root, pagesDirName, routeGoName); len(routes) > 0 || fileExists(target) {
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

// segment is the expression writing a path parameter into the path,
// statements it needs written to body first.
func (t *routeTree) segment(body *strings.Builder, im *imports, p urlParam) string {
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
		fmt.Fprintf(body, "\ttext, _ := %s.MarshalText()\n", v)
		return im.name("net/url", "url") + ".PathEscape(string(text))"
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
	case "segs", "seg", "i", "u", "text":
		return name + "_"
	}
	if token.Lookup(name).IsKeyword() {
		return name + "_"
	}
	return name
}

// registrations is pages/route.go: func Routes, the option registering
// every page, its handlers and their layouts on the app.
func (t *routeTree) registrations(routes []*folderRoute) []byte {
	self := t.modPath + "/" + pagesDirName
	var scope *types.Scope
	root := t.root0()
	if t.info[root] != nil && t.info[root].pkg != nil {
		scope = t.info[root].pkg.Scope()
	}
	im := newImports(self, scope, "app", "Routes")
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
			name = strings.NewReplacer("/", "_", "-", "_", ".", "_", "[", "", "]", "").Replace(d.rel)
		}
		if n := im.name(d.importPath, name); n != "" {
			return n + "."
		}
		return ""
	}
	sorted := sortedRoutes(routes)
	var body, inits strings.Builder
	recorded := map[string]bool{}
	for _, r := range sorted {
		q := qual(r.dir)
		ctor := q + r.ctor
		if r.ctor == "" {
			ctor = fmt.Sprintf("func() *%s%s { return &%s%s{} }", q, r.typ, q, r.typ)
		}
		var opts []string
		switch {
		case r.fn != "":
			// A scalar binds the path parameter of its name, spelt as the
			// path spells it: id for [id] when the Go says ID. The names
			// are recorded at init, as the compiler records a registered
			// handler's: vuka.RegisterParams.
			var names []string
			for _, n := range r.names {
				for _, seg := range splitPath(r.Path) {
					if p := strings.TrimSuffix(strings.Trim(seg, "{}"), "..."); strings.HasPrefix(seg, "{") && strings.EqualFold(p, n) {
						n = p
					}
				}
				names = append(names, strconv.Quote(n))
			}
			if fn := q + r.fn; !recorded[fn] && len(names) > 0 {
				recorded[fn] = true
				fmt.Fprintf(&inits, "\t%s.RegisterParams(%s, %s)\n", im.name(vukaModPath, "vuka"), fn, strings.Join(names, ", "))
			}
			opts = append(opts, web+".PageRoute()")
		case r.handler != "":
			opts = append(opts, web+".Receiver("+ctor+")")
		default:
			opts = append(opts, web+".PageRoute()")
		}
		for _, m := range r.mws {
			opts = append(opts, fmt.Sprintf("%s.UseOf(%sMiddleware)", web, qual(m)))
		}
		for _, w := range r.wraps {
			if w.guard {
				opts = append(opts, fmt.Sprintf("%s.GuardOf[%sGuard]()", web, qual(w.dir)))
			} else {
				opts = append(opts, fmt.Sprintf("%s.LayoutOf[%s%s]()", web, qual(w.dir), w.dir.layout))
			}
		}
		call, fn := "Get", ctor
		switch {
		case r.Kind == "live":
			call = "Live"
		case r.fn != "":
			call, fn = upperFirst(strings.ToLower(r.Method)), q+r.fn
		case r.handler != "":
			call, fn = r.handler, "(*"+q+r.typ+")."+r.handler
		}
		fmt.Fprintf(&body, "\t\tapp.%s(%q, %s, %s)\n", call, r.Path, fn, strings.Join(opts, ", "))
	}
	var initFn string
	if inits.Len() > 0 {
		initFn = "func init() {\n" + inits.String() + "}\n\n"
	}
	src := routesHeading + "package " + root.name + "\n\n" + im.block() + initFn +
		"// Routes registers every page and endpoint under pages/ on the app, with\n// its middleware, guards and layouts: web.New(pages.Routes(), …).\n" +
		"func Routes() " + web + ".Option {\n\treturn " + web + ".OptionFunc(func(app *" + web + ".App) {\n" + body.String() + "\t})\n}\n"
	return gofmt(src)
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

func splitPath(path string) []string { return strings.Split(strings.TrimPrefix(path, "/"), "/") }
