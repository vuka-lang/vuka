package main

import (
	"bufio"
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/diag"
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
			case ab && ba:
				c.res.diags = append(c.res.diags, *diag.New("E0701", "%s and %s both serve %s %s", b.Page, a.Page, a.Method, a.Path).
					At(diag.Span{File: a.File, Line: a.Line}).WithRelated(diag.Span{File: b.File, Line: b.Line}).
					WithHelp("rename one of them, or move it to another folder"))
			case !ab && !ba:
				c.res.diags = append(c.res.diags, *diag.New("E0702", "%s %s and %s match the same paths, neither more specific", a.Method, a.Path, b.Path).
					At(diag.Span{File: a.File, Line: a.Line}).WithRelated(diag.Span{File: b.File, Line: b.Line}).
					WithHelp("a request such as %s would match both: rename a folder or a page so one of them is fixed where the other has a parameter", overlapExample(pa, pb)))
			}
		}
	}
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
		src, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(src, []byte(`"`+webPath+`"`)) || bytes.HasPrefix(src, []byte(routesHeading)) {
			return nil // pages/route.go is the folder routes'
		}
		for _, m := range registeredCtor.FindAllSubmatch(src, -1) {
			registered[string(m[1])] = true
		}
		for _, m := range declaredRouteRe.FindAllSubmatchIndex(src, -1) {
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

// goDirectiveOf is the go directive of the go.mod at root ("1.25.0").
func goDirectiveOf(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
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

// routesUnserved is W0701 when the module has pages under pages/ and none
// of its main packages passes pages.Routes() to the app: route.go
// registers nothing until something calls it. nil when one does, or when
// the module has no main package (a library of pages).
func routesUnserved(root, modPath string, t *routeTree) []diag.Diagnostic {
	if t == nil || !slices.ContainsFunc(t.dirs, func(d *routeDir) bool { return len(d.pages) > 0 || len(d.funcs) > 0 }) {
		return nil
	}
	pagesPath := strconv.Quote(modPath + "/" + pagesDirName)
	importRe := regexp.MustCompile(`(?m)^\s*(?:import\s+)?(\w+)?\s*` + regexp.QuoteMeta(pagesPath))
	mains, served := 0, false
	walkSources(root, func(path, name string) error {
		if served || !(strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".vuka")) || strings.Contains(name, "_test.") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !regexp.MustCompile(`(?m)^package main\b`).Match(src) {
			return nil
		}
		mains++
		for _, m := range importRe.FindAllSubmatch(src, -1) {
			alias := string(m[1])
			if alias == "" {
				alias = pagesDirName
			}
			if bytes.Contains(src, []byte(alias+".Routes()")) {
				served = true
			}
		}
		return nil
	})
	if served || mains == 0 {
		return nil
	}
	return []diag.Diagnostic{*diag.Warn("W0701", "the pages under pages/ are served by no main package: none passes pages.Routes() to its app").
		At(diag.Span{File: filepath.Join(t.root, pagesDirName, routeGoName)}).
		WithHelp("register them: web.New(pages.Routes(), …), importing %s", pagesPath)}
}
