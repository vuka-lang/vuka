package main

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// Route names in the editor: route("…"), routePath("…") and, in Go,
// web.Route(ctx, "…") complete the names, sign their parameters, hover with
// the path, go to the page and find the links naming it; their query{…}
// complete the page's keys; and the links' E0725, E0726 (the count) and
// E0727 are reported as they are typed. The names are the last build's,
// else read from the folders under pages/ before it finishes.

// namedRoutes are the route names the editor works with.
func (p *proxy) namedRoutes() map[string]*namedRoute {
	p.sem.mu.Lock()
	named, scanned, root := p.sem.named, p.sem.scanned, p.sem.modRoot
	p.sem.mu.Unlock()
	if named != nil {
		return named
	}
	if scanned != nil {
		return scanned
	}
	if root == "" {
		p.mu.Lock()
		root = p.modRoot
		p.mu.Unlock()
	}
	if root == "" {
		return nil
	}
	root, modPath, err := load.ModuleRoot(root)
	if err != nil {
		return nil
	}
	scanned = scannedNames(scanRoutes(root, modPath, nil))
	p.sem.mu.Lock()
	p.sem.scanned = scanned
	p.sem.mu.Unlock()
	return scanned
}

// setNamed keeps a build's route names, each at its page's place in its
// source (routes, already moved there).
func (p *proxy) setNamed(named map[string]*namedRoute, routes []*folderRoute) {
	if named == nil {
		return
	}
	out := make(map[string]*namedRoute, len(named))
	for n, nr := range named {
		c := *nr
		for _, r := range routes {
			if r.Path == nr.Path && r.Page == nr.Page {
				c.At = diag.Span{File: r.File, Line: r.Line}
				break
			}
		}
		out[n] = &c
	}
	p.sem.named, p.sem.scanned = out, nil
}

// scannedNames are the route names as the folders under pages/ give them,
// before any build: the paths of their pages and method funcs, the
// parameters untyped.
func scannedNames(ts routeTrees) map[string]*namedRoute {
	out := map[string]*namedRoute{}
	for _, t := range ts {
		type found struct {
			page string
			at   diag.Span
			d    *routeDir
			typ  string
		}
		byPath := map[string]found{}
		var paths []string
		add := func(path string, f found) {
			if _, ok := byPath[path]; !ok {
				paths = append(paths, path)
				byPath[path] = f
			}
		}
		for _, d := range t.dirs {
			var segs []string
			for _, part := range strings.Split(d.rel, "/") {
				switch {
				case part == "" || load.Grouped(part):
				case load.Bracketed(part):
					segs = append(segs, "{"+bracketName(part)+map[bool]string{true: "...", false: ""}[isRest(part)]+"}")
				default:
					segs = append(segs, part)
				}
			}
			pathOf := func(seg string) string {
				s := slices.Clone(segs)
				switch {
				case seg == "":
				case load.Bracketed(seg):
					s = append(s, "{"+bracketName(seg)+"}")
				default:
					s = append(s, seg)
				}
				return "/" + strings.Join(s, "/")
			}
			for _, typ := range d.pages {
				if d.notPage(typ) {
					continue
				}
				seg := ""
				switch n := d.urlName(typ); {
				case load.Bracketed(n):
					seg = n
				case n != "Index":
					seg = kebab(n)
				}
				file := filepath.Join(d.dir, d.fileOf[typ])
				add(pathOf(seg), found{d.name + "." + typ, diag.Span{File: file, Line: 1}, d, typ})
			}
			for _, f := range d.funcs {
				add(pathOf(fileSeg(f.file)), found{d.name + "." + f.method, diag.Span{File: filepath.Join(d.dir, f.file), Line: 1}, d, ""})
			}
		}
		slices.Sort(paths)
		for path, name := range nameRoutes(paths) {
			f := byPath[path]
			nr := &namedRoute{Name: name, Path: path, Page: f.page, At: f.at}
			for _, s := range splitPath(path) {
				if strings.HasPrefix(s, "{") {
					nr.Params = append(nr.Params, routeParam{Name: strings.TrimSuffix(strings.Trim(s, "{}"), "...")})
				}
			}
			if f.typ != "" {
				for _, a := range f.d.attrs.structURLAttrs(f.d.dir, f.typ) {
					if !a.bare && a.key != "" && !slices.ContainsFunc(nr.Query, func(q routeQuery) bool { return q.Key == a.key }) {
						nr.Query = append(nr.Query, routeQuery{Key: a.key, Field: a.field})
					}
				}
			}
			if _, ok := out[name]; !ok {
				out[name] = nr
			}
		}
	}
	return out
}

// routeCallAt is the link around off in src: in its name, or in its
// arguments.
func routeCallAt(src []byte, off int, goFile bool) (routeCall, bool) {
	for _, rc := range findRouteCalls(src) {
		if goFile && !strings.HasPrefix(rc.fn, "web.") {
			continue
		}
		if off > rc.lparen && (off < rc.end || !rc.closed) {
			return rc, true
		}
	}
	return routeCall{}, false
}

// routeNameRequest answers a request on a link by route name at off in
// src, the text of path; nil leaves it to the rest.
func (p *proxy) routeNameRequest(method, path string, src []byte, off int) func() any {
	switch method {
	case "textDocument/completion", "textDocument/hover", "textDocument/definition", "textDocument/signatureHelp", "textDocument/references":
	default:
		return nil
	}
	goFile := strings.HasSuffix(path, ".go")
	if !goFile && !strings.Contains(string(src), "route") {
		return nil
	}
	rc, ok := routeCallAt(src, off, goFile)
	if !ok {
		return nil
	}
	inName := rc.inName(off)
	switch method {
	case "textDocument/completion":
		if inName {
			return func() any { return routeNameItems(p.namedRoutes(), src, rc, off) }
		}
		if from, ok := queryKeySpot(src, rc, off); ok {
			return func() any { return queryKeyItems(p.namedRoutes()[rc.name], src, from, off) }
		}
	case "textDocument/hover":
		if inName {
			return func() any {
				nr := p.namedRoutes()[rc.name]
				if nr == nil {
					return nil
				}
				return markdownHover(routeHover(nr), src, rc.nameStart, rc.nameEnd)
			}
		}
	case "textDocument/definition":
		if inName {
			return func() any {
				nr := p.namedRoutes()[rc.name]
				if nr == nil || nr.At.File == "" {
					return nil
				}
				pos := lspPosition{Line: uint32(max(nr.At.Line-1, 0))}
				return []any{map[string]any{"uri": pathToURI(p.display(nr.At.File)), "range": lspRange{pos, pos}}}
			}
		}
	case "textDocument/references":
		if inName {
			return func() any { return p.routeReferences(rc.name) }
		}
	case "textDocument/signatureHelp":
		if !inName && rc.nameClosed && off > rc.nameEnd {
			return func() any {
				nr := p.namedRoutes()[rc.name]
				if nr == nil {
					return nil
				}
				return routeSignature(rc, nr, src, off)
			}
		}
	}
	return nil
}

// routeNameItems are the names completing the literal of rc.
func routeNameItems(named map[string]*namedRoute, src []byte, rc routeCall, off int) any {
	end := off
	if rc.nameClosed {
		end = rc.nameEnd - 1
	}
	r := lspRange{positionOf(src, rc.nameStart+1), positionOf(src, end)}
	items := []any{}
	for _, n := range slices.Sorted(func(yield func(string) bool) {
		for n := range named {
			if !yield(n) {
				return
			}
		}
	}) {
		nr := named[n]
		items = append(items, map[string]any{
			"label":        n,
			"kind":         18, // Reference
			"detail":       nr.signature(),
			"labelDetails": map[string]any{"description": bracketPath(nr.Path)},
			"filterText":   n,
			"textEdit":     map[string]any{"range": r, "newText": n},
		})
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

// queryKeySpot reports whether off is where rc's query takes a key: in a
// key's literal, from the byte after its quote.
func queryKeySpot(src []byte, rc routeCall, off int) (int, bool) {
	q := rc.query
	if q == nil || off <= q.lbrace || off > q.end {
		return 0, false
	}
	i := off - 1
	for i > q.lbrace && isIdentByte(src[i]) {
		i--
	}
	if src[i] != '"' {
		return 0, false
	}
	j := i - 1
	for j > q.lbrace && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n') {
		j--
	}
	if src[j] != '{' && src[j] != ',' {
		return 0, false
	}
	return i + 1, true
}

func queryKeyItems(nr *namedRoute, src []byte, from, off int) any {
	items := []any{}
	if nr == nil {
		return map[string]any{"isIncomplete": false, "items": items}
	}
	r := lspRange{positionOf(src, from), positionOf(src, off)}
	for _, q := range nr.Query {
		items = append(items, map[string]any{
			"label":    q.Key,
			"kind":     5, // Field
			"detail":   strings.TrimSpace(q.Field + " " + shortType(q.Type)),
			"textEdit": map[string]any{"range": r, "newText": q.Key},
		})
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

// routeHover is what hover says of a route's name.
func routeHover(nr *namedRoute) string {
	var b strings.Builder
	b.WriteString("**" + nr.Name + "** `" + bracketPath(nr.Path) + "`\n\n")
	if len(nr.Params) == 0 {
		b.WriteString("takes no path parameters")
	} else {
		var ps []string
		for _, p := range nr.Params {
			ps = append(ps, "`"+strings.TrimSpace(p.Name+" "+shortType(p.Type))+"`")
		}
		b.WriteString("takes " + strings.Join(ps, ", "))
	}
	if len(nr.Query) > 0 {
		var qs []string
		for _, q := range nr.Query {
			qs = append(qs, "`"+q.Key+"`")
		}
		b.WriteString("\n\nquery: " + strings.Join(qs, ", "))
	}
	if nr.Page != "" {
		b.WriteString("\n\n" + nr.Page)
		if nr.At.File != "" {
			b.WriteString(" — " + filepath.Base(nr.At.File) + ":" + strconv.Itoa(max(nr.At.Line, 1)))
		}
	}
	return b.String()
}

// routeSignature is signature help in rc's arguments: the route's path
// parameters in order, then its query when its page keeps one.
func routeSignature(rc routeCall, nr *namedRoute, src []byte, off int) any {
	label := rc.fn + "("
	if strings.HasPrefix(rc.fn, "web.") {
		label += "ctx, "
	}
	label += strconv.Quote(nr.Name)
	var params []any
	add := func(s string) {
		label += ", "
		params = append(params, map[string]any{"label": []int{len16(label), len16(label + s)}})
		label += s
	}
	for _, p := range nr.Params {
		add(strings.TrimSpace(p.Name + " " + shortType(p.Type)))
	}
	if len(nr.Query) > 0 {
		add("query{…}")
	}
	label += ")"
	active := len(params)
	if i := rc.argAt(off); i >= 0 {
		active = min(i, max(len(params)-1, 0))
	} else if len(rc.args) == 0 {
		active = 0
	}
	sig := map[string]any{"label": label, "parameters": params, "documentation": bracketPath(nr.Path)}
	return map[string]any{"signatures": []any{sig}, "activeSignature": 0, "activeParameter": active}
}

func len16(s string) int {
	n := 0
	for _, r := range s {
		n += runeLen16(r)
	}
	return n
}

// routeReferences are the links naming the route name in the module.
func (p *proxy) routeReferences(name string) any {
	p.sem.mu.Lock()
	root := p.sem.modRoot
	p.sem.mu.Unlock()
	out := []any{}
	if root == "" {
		return out
	}
	load.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if n := e.Name(); path != root && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" || n == "testdata" || n == "build" || fileExists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isVuka(path) && !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_gen.go") {
			return nil
		}
		src := p.sourceText(path)
		for _, rc := range findRouteCalls(src) {
			if rc.name != name || rc.nameStart < 0 || strings.HasSuffix(path, ".go") && !strings.HasPrefix(rc.fn, "web.") {
				continue
			}
			out = append(out, map[string]any{"uri": pathToURI(p.display(path)),
				"range": lspRange{positionOf(src, rc.nameStart+1), positionOf(src, rc.nameEnd-1)}})
		}
		return nil
	})
	return out
}

// routeLinkDiags are the problems of the links of src, the text of path, as
// it is typed: names, counts and query keys against the last build's names.
func (p *proxy) routeLinkDiags(path string, src []byte) []any {
	if !isVuka(path) || !strings.Contains(string(src), "route") {
		return nil
	}
	p.sem.mu.Lock()
	named := p.sem.named
	p.sem.mu.Unlock()
	if named == nil {
		return nil
	}
	var out []any
	for _, rc := range findRouteCalls(src) {
		if strings.HasPrefix(rc.fn, "web.") || !rc.nameClosed || !rc.closed || rc.nameStart < 0 {
			continue
		}
		l := linkFact{Name: rc.name, Fn: rc.fn}
		for i := 0; i < rc.pathArgs(src); i++ {
			l.Args = append(l.Args, argFact{})
		}
		if rc.query != nil {
			l.Keys = []string{}
			for _, k := range rc.query.keys {
				l.Keys = append(l.Keys, k.key)
			}
		}
		for _, pr := range linkProblems(named, l, false) {
			from, to := rc.nameStart, rc.nameEnd
			if pr.key >= 0 {
				from, to = rc.query.keys[pr.key].start, rc.query.keys[pr.key].end
			}
			ld := lspFrom(lspRange{positionOf(src, from), positionOf(src, to)}, *pr.d)
			if s := pr.d.Suggest; s != "" {
				r := lspRange{positionOf(src, from+1), positionOf(src, to-1)}
				ld.Data = map[string]any{"vukaFix": map[string]any{"title": "Change to " + strconv.Quote(s),
					"edits": []any{map[string]any{"range": r, "newText": s, "old": string(src[from+1 : to-1])}}}}
			}
			out = append(out, ld)
		}
	}
	return out
}
