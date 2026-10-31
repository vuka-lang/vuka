package main

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// conflicts reports the routes no router can tell apart: parameter folders
// side by side, two routes of one method on one path, a pair that matches
// the same paths with neither more specific (Go's ServeMux refuses them),
// and folder routes on the path of a route the module declares itself.
func (c *routeChecker) conflicts() {
	byParent := map[string]*pathParam{}
	for _, key := range slices.Sorted(maps.Keys(c.params)) {
		p := c.params[key]
		if p.file.File != "" && p.folder.File != "" {
			c.reportAt(diag.New("E0713", "pages/%s is both a file and a folder", key), p.file).WithRelated(p.folder).
				WithHelp("move the file into the folder as its index: %s/index%s", key, strings.TrimPrefix(filepath.Base(p.file.File), filepath.Base(key)))
			continue
		}
		parent := parentRel(key)
		if first := byParent[parent]; first != nil {
			at := p.folder
			if at.File == "" {
				at = p.file
			}
			fat := first.folder
			if fat.File == "" {
				fat = first.file
			}
			c.reportAt(diag.New("E0702", "the parameters pages/%s and pages/%s are side by side", first.key, key), at).
				WithHelp("keep one parameter per level: a request can't say which of them it names").WithRelated(fat)
			continue
		}
		byParent[parent] = p
	}
	routes := c.res.routes
	for i, a := range routes {
		for _, b := range routes[:i] {
			if a.Method != b.Method {
				continue
			}
			pa, pb := splitPattern(a.Path), splitPattern(b.Path)
			if !patternsOverlap(pa, pb) {
				continue
			}
			ab, ba := patternWithin(pa, pb), patternWithin(pb, pa)
			switch {
			case ab && ba && a.dir != nil && b.dir != nil && a.dir != b.dir && (strings.Contains(a.dir.rel, "(") || strings.Contains(b.dir.rel, "(")):
				fa, fb := relPath(c.t.pages, a.File), relPath(c.t.pages, b.File)
				c.res.diags = append(c.res.diags, *diag.New("E0722", "pages/%s and pages/%s both serve %s %s: a route group adds nothing to the URL", fb, fa, a.Method, a.Path).
					At(diag.Span{File: a.File, Line: a.Line}).WithRelated(diag.Span{File: b.File, Line: b.Line}).
					WithHelp("keep one of them, or move it to a folder that names a segment of its own"))
			case ab && ba:
				c.res.diags = append(c.res.diags, *diag.New("E0701", "%s and %s both serve %s %s", b.Page, a.Page, a.Method, a.Path).
					At(diag.Span{File: a.File, Line: a.Line}).WithRelated(diag.Span{File: b.File, Line: b.Line}).
					WithHelp("%s", conflictHelp(a, b)))
			case !ab && !ba:
				c.res.diags = append(c.res.diags, *diag.New("E0702", "%s %s and %s match the same paths, neither more specific", a.Method, a.Path, b.Path).
					At(diag.Span{File: a.File, Line: a.Line}).WithRelated(diag.Span{File: b.File, Line: b.Line}).
					WithHelp("a request such as %s would match both: rename a folder or a page so one of them is fixed where the other has a parameter", overlapExample(pa, pb)))
			}
		}
	}
	c.rootLayouts()
	for _, h := range c.t.declared {
		for _, r := range routes {
			if h.method != r.Method && !(h.method == "LIVE" && r.Method == "GET") {
				continue
			}
			ph, pr := splitPattern(h.path), splitPattern(r.Path)
			if patternWithin(ph, pr) && patternWithin(pr, ph) {
				c.res.diags = append(c.res.diags, *diag.New("E0707", "%s %s is a folder route of %s, and is declared at %s:%d too", r.Method, r.Path, r.Page, relPath(c.t.root, h.file), h.line).
					At(diag.Span{File: r.File, Line: r.Line}).WithRelated(diag.Span{File: h.file, Line: h.line}).
					WithHelp("serve the path one way: remove the declared route, or move the page"))
			}
		}
	}
}

// conflictHelp is what to do about two routes on one method and path: a
// method func beside a page takes the page's path from its file's name.
func conflictHelp(a, b *folderRoute) string {
	if a.fn == "" {
		a, b = b, a
	}
	if a.fn == "" || b.typ == "" {
		return "rename one of them, or move it to another folder"
	}
	return fmt.Sprintf("%s in %s answers %s as %s does: remove it, or move it to a file named for another path", a.Method, filepath.Base(a.File), a.Path, b.Page)
}

// rootLayouts reports the home page outside every root layout (E0724): with
// no pages/_layout.vuka.html, each route group at the top of pages/ may
// have a layout of its own, its pages' document, and then / is one of
// theirs.
func (c *routeChecker) rootLayouts() {
	if c.t.root0().layout != "" {
		return
	}
	var roots []*routeDir
	for _, d := range c.t.dirs {
		if load.Grouped(d.rel) && d.layout != "" {
			roots = append(roots, d)
		}
	}
	if len(roots) == 0 {
		return
	}
	for _, r := range c.res.routes {
		if r.Path != "/" || r.Method != "GET" || r.Kind == "handler" || r.Kind == "endpoint" {
			continue
		}
		top, _, _ := strings.Cut(r.dir.rel, "/")
		if slices.ContainsFunc(roots, func(d *routeDir) bool { return d.rel == top }) {
			continue
		}
		c.res.diags = append(c.res.diags, *diag.New("E0724", "pages/%s is the home page outside the route groups with a root layout, and pages/ has no _layout.vuka.html", filepath.ToSlash(relPath(c.t.pages, r.File))).
			At(diag.Span{File: r.File, Line: r.Line}).WithRelated(diag.Span{File: filepath.Join(roots[0].dir, roots[0].layoutFile), Line: 1}).
			WithHelp("move it into one of the groups, pages/%s/%s, or give pages/ a _layout.vuka.html", roots[0].rel, filepath.Base(r.File)))
	}
}

// pattern segments: a literal, "{}" for a parameter, "{...}" for the rest.
func splitPattern(path string) []string {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "{}"
			if strings.HasSuffix(s, "...}") {
				segs[i] = "{...}"
			}
		}
	}
	return segs
}

// patternsOverlap reports whether some path matches both patterns.
func patternsOverlap(a, b []string) bool {
	switch {
	case len(a) == 0 && len(b) == 0:
		return true
	case len(a) > 0 && a[0] == "{...}":
		return len(b) > 0
	case len(b) > 0 && b[0] == "{...}":
		return len(a) > 0
	case len(a) == 0 || len(b) == 0:
		return false
	case a[0] != "{}" && b[0] != "{}" && a[0] != b[0]:
		return false
	}
	return patternsOverlap(a[1:], b[1:])
}

// patternWithin reports whether every path a matches, b matches too.
func patternWithin(a, b []string) bool {
	switch {
	case len(a) == 0 && len(b) == 0:
		return true
	case len(b) > 0 && b[0] == "{...}":
		return len(a) > 0
	case len(a) == 0 || len(b) == 0 || a[0] == "{...}":
		return false
	case b[0] == "{}":
		return patternWithin(a[1:], b[1:])
	case a[0] != b[0]:
		return false
	}
	return patternWithin(a[1:], b[1:])
}

// overlapExample is a path two overlapping patterns both match.
func overlapExample(a, b []string) string {
	var out []string
	for i := 0; i < len(a) || i < len(b); i++ {
		s := "x"
		switch {
		case i < len(a) && a[i] != "{}" && a[i] != "{...}":
			s = a[i]
		case i < len(b) && b[i] != "{}" && b[i] != "{...}":
			s = b[i]
		}
		out = append(out, s)
	}
	return "/" + strings.Join(out, "/")
}

// declaredRoute is a route the module registers itself, by a decorator or
// a web.Routes / App call with a literal path.
type declaredRoute struct {
	method, path string
	file         string
	line         int
}

var (
	declaredRouteRe = regexp.MustCompile(`\.(Get|Post|Put|Patch|Delete|Live)\(\s*"(/[^"]*)"|\.Handle\(\s*"([A-Z]+)",\s*"(/[^"]*)"`)
	registeredCtor  = regexp.MustCompile(`\.(?:Get|Post|Put|Patch|Delete|Live|Handle)\((?:\s*"[A-Z]+",)?\s*"[^"]*",\s*(?:\w+\.)?New([A-Z]\w*)\b`)
)

// declaredRoutes are the routes the module's own sources declare, outside
// the generated files: those of its Go and Vuka files that import web. The
// types whose constructors they register go in registered.
func declaredRoutes(root string, registered map[string]bool) []declaredRoute {
	var out []declaredRoute
	walkSources(root, func(path, name string) error {
		if !(strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".vuka")) || strings.Contains(name, "_test.") ||
			strings.HasSuffix(name, "_gen.go") || strings.HasSuffix(name, "_vuka.go") || strings.HasSuffix(name, "_html.go") || strings.HasSuffix(name, "_templ.go") {
			return nil
		}
		src, err := load.ReadSource(path)
		if err != nil || !bytes.Contains(src, []byte(`"`+webPath+`"`)) || bytes.HasPrefix(src, []byte(routesHeading)) {
			return nil // pages/route.go is the folder routes'
		}
		for _, m := range registeredCtor.FindAllSubmatch(src, -1) {
			registered[string(m[1])] = true
		}
		for _, m := range declaredRouteRe.FindAllSubmatchIndex(src, -1) {
			if onMethod(src, m[0]) {
				continue // a controller's action: its path is below the controller's
			}
			r := declaredRoute{file: path, line: bytes.Count(src[:m[0]], []byte("\n")) + 1}
			if m[2] >= 0 {
				r.method, r.path = strings.ToUpper(string(src[m[2]:m[3]])), string(src[m[4]:m[5]])
			} else {
				r.method, r.path = string(src[m[6]:m[7]]), string(src[m[8]:m[9]])
			}
			out = append(out, r)
		}
		return nil
	})
	return out
}

// onMethod reports whether the route decorator at i, an attribute line
// @web.Get("/x"), is a method's: a controller's action.
func onMethod(src []byte, i int) bool {
	start := bytes.LastIndexByte(src[:i], '\n') + 1
	if !bytes.HasPrefix(bytes.TrimSpace(src[start:i]), []byte("@")) {
		return false
	}
	for rest := src[i:]; ; {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			return false
		}
		rest = rest[nl+1:]
		line := bytes.TrimSpace(rest[:max(bytes.IndexByte(rest, '\n'), 0)])
		if len(line) == 0 || line[0] == '@' || bytes.HasPrefix(line, []byte("//")) {
			continue
		}
		return bytes.HasPrefix(line, []byte("func ("))
	}
}

// goDirectiveOf is the go directive of the go.mod at root ("1.25.0").
func goDirectiveOf(root string) string {
	data, err := load.ReadSource(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "go" {
			return f[1]
		}
	}
	return ""
}

// routesUnserved is W0701 for each page tree with pages that no main
// package serves: route.go registers nothing until something passes
// pages.Routes() to an app. The module's pages/ are served by any of its
// main packages, and nil when it has none (a library of pages); a pages/
// directory beside a package main, by that main.
func routesUnserved(root, modPath string, ts routeTrees) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, t := range ts {
		if !slices.ContainsFunc(t.dirs, func(d *routeDir) bool { return len(d.pages) > 0 || len(d.funcs) > 0 }) {
			continue
		}
		pagesPath := strconv.Quote(t.pagesPath)
		mains, served := 0, false
		visit := func(path, name string) error {
			if served || !(strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".vuka")) || strings.Contains(name, "_test.") {
				return nil
			}
			src, err := load.ReadSource(path)
			if err != nil || !mainPkg.Match(src) {
				return nil
			}
			mains++
			served = passesRoutes(src, t.pagesPath, pagesDirName)
			return nil
		}
		msg := "the pages under pages/ are served by no main package: none passes pages.Routes() to its app"
		if t.main == "" {
			walkSources(root, visit)
			if mains == 0 {
				continue
			}
		} else {
			entries, _ := load.ReadDir(t.main)
			for _, e := range entries {
				if !e.IsDir() {
					visit(filepath.Join(t.main, e.Name()), e.Name())
				}
			}
			msg = "the pages under " + filepath.ToSlash(relPath(root, t.pages)) + "/ are served by no app: the package main beside them never passes pages.Routes() to it"
		}
		if !served {
			d := diag.Warn("W0701", "%s", msg).At(diag.Span{File: filepath.Join(t.pages, routeGoName)})
			if e := pagesRoutesEdit(root, modPath, t.pages); e.Src != nil {
				d.WithHelp("vuka fix routes adds it to web.New in %s: web.New(pages.Routes(), …), importing %s", e.File, pagesPath)
			} else {
				d.WithHelp("register them: web.New(pages.Routes(), …), importing %s", pagesPath)
			}
			out = append(out, *d)
		}
	}
	return out
}
