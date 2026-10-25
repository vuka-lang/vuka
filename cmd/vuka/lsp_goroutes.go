package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The folder routes in a page package's Go, .go and .vuka files alike: a
// hover on a page's or an endpoint's type name says the routes it serves, on
// a handler's or an endpoint's method its METHOD /path, on a live page's
// event method the page it is an event of. gopls's hover comes first, the
// note below it; a file of no folder-route package gets nothing.

var (
	// typeNameAt is a type's name where it is declared: type X struct, or X
	// struct in a type ( … ) group.
	typeNameAt = regexp.MustCompile(`^\s*(?:type\s+)?([A-Za-z_]\w*)(?:\[[^\]]*\])?\s+struct\b`)
	// methodNameAt is a method's name where it is declared, with its
	// receiver's type.
	methodNameAt = regexp.MustCompile(`^func\s*\(\s*(?:\w+\s+)?\*?\s*(\w+)(?:\[[^\]]*\])?\s*\)\s*(\w+)\s*[\[(]`)
	// funcNameAt is a func's name where it is declared: a .vuka method func.
	funcNameAt = regexp.MustCompile(`^func\s+(\w+)\s*[\[(]`)
)

// dirRoutes are the routes of the folder path is in.
func (p *proxy) dirRoutes(path string) []*folderRoute {
	routes, _ := p.routes()
	if len(routes) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	var out []*folderRoute
	for _, r := range routes {
		if r.dir != nil && (r.dir.dir == dir || r.dir.dir == real ||
			filepath.Base(r.dir.dir) == filepath.Base(dir) && samePath(r.dir.dir, dir)) {
			out = append(out, r)
		}
	}
	return out
}

// routeNote is the note on the name at off of a page package's file: the
// routes of a page's or an endpoint's type where it is declared, the route
// of a handler's or an endpoint's method, or the page an event method is an
// event of; "" on anything else.
func (p *proxy) routeNote(path string, src []byte, off int) string {
	if off < 0 || off > len(src) {
		return ""
	}
	routes := p.dirRoutes(path)
	if len(routes) == 0 {
		return ""
	}
	s, e := off, off
	for s > 0 && isIdentByte(src[s-1]) {
		s--
	}
	for e < len(src) && isIdentByte(src[e]) {
		e++
	}
	if s == e {
		return ""
	}
	word := string(src[s:e])
	ls := bytes0LineStart(src, s)
	line := string(lineAt(src, ls))
	col := s - ls
	at := func(m []int, g int) bool { return m != nil && m[2*g] == col && line[m[2*g]:m[2*g+1]] == word }
	if m := methodNameAt.FindStringSubmatchIndex(line); at(m, 2) {
		return p.methodNote(routes, line[m[2]:m[3]], word)
	}
	if m := funcNameAt.FindStringSubmatchIndex(line); at(m, 1) {
		for _, r := range routes {
			if r.fn != "" && r.Method == word && samePath(r.File, path) {
				return "**route** " + routeLine(r) + wrapLine(r)
			}
		}
		return ""
	}
	if m := typeNameAt.FindStringSubmatchIndex(line); at(m, 1) {
		var mine []*folderRoute
		for _, r := range routes {
			if r.typ == word && r.Kind != "handler" {
				mine = append(mine, r)
			}
		}
		for _, r := range routes {
			if r.typ == word && r.Kind == "handler" {
				mine = append(mine, r)
			}
		}
		return typeNote(mine)
	}
	return ""
}

// methodNote is the note on the method name of a receiver type typ.
func (p *proxy) methodNote(routes []*folderRoute, typ, name string) string {
	var page *folderRoute
	for _, r := range routes {
		if r.typ != typ {
			continue
		}
		if r.handler == name {
			return "**route** " + routeLine(r) + wrapLine(r)
		}
		if r.Kind == "page" || r.Kind == "live" {
			page = r
		}
	}
	if page == nil || !strings.HasSuffix(page.File, ".vuka.html") {
		return ""
	}
	if !sendsEvent(scanShape(p.sourceText(page.File)), name) {
		return ""
	}
	return "**event** `" + name + "` of the page `" + page.Page + "` · `GET " + page.Path + "` · runs on the server · patches the page"
}

// typeNote is the note on a type serving routes.
func typeNote(routes []*folderRoute) string {
	switch len(routes) {
	case 0:
		return ""
	case 1:
		return "**route** " + routeLine(routes[0]) + wrapLine(routes[0])
	}
	var b strings.Builder
	b.WriteString("**routes**\n")
	for _, r := range routes {
		b.WriteString("\n- " + routeLine(r))
	}
	return b.String() + wrapLine(routes[0])
}

// routeLine is a route as its hover says it: `POST /orgs/new` · handler
// `orgs.New.Post`.
func routeLine(r *folderRoute) string {
	return "`" + r.Method + " " + r.Path + "` · " + r.Kind + " `" + r.Page + "`"
}

// wrapLine is what a route runs within, its guards, layouts and
// middleware, as a paragraph of its own; "" when nothing wraps it.
func wrapLine(r *folderRoute) string {
	quote := func(names []string) string { return "`" + strings.Join(names, "`, `") + "`" }
	var parts []string
	if len(r.Guards) > 0 {
		parts = append(parts, "guard "+quote(r.Guards))
	}
	if len(r.Layouts) > 0 {
		parts = append(parts, "layout "+quote(r.Layouts))
	}
	if len(r.Middleware) > 0 {
		parts = append(parts, "middleware "+quote(r.Middleware))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(parts, " · ")
}

// typeLenses are the lines of the endpoint types a file declares, each with
// the routes its methods serve: an endpoint's routes are on its methods'
// lines, its type gets the lens of them all.
func typeLenses(src []byte, routes []*folderRoute) map[int][]*folderRoute {
	byType := map[string][]*folderRoute{}
	for _, r := range routes {
		if r.Kind == "endpoint" && r.typ != "" {
			byType[r.typ] = append(byType[r.typ], r)
		}
	}
	if len(byType) == 0 {
		return nil
	}
	out := map[int][]*folderRoute{}
	for i, line := range strings.SplitAfter(string(src), "\n") {
		if m := typeNameAt.FindStringSubmatch(line); m != nil && byType[m[1]] != nil {
			group := slices.Clone(byType[m[1]])
			slices.SortStableFunc(group, func(a, b *folderRoute) int { return a.Line - b.Line })
			out[i+1] = group
		}
	}
	return out
}

// goRouteAfter keeps, for a hover or a code lens request on a .go file of a
// page package that goes to gopls, what to add to gopls's answer: the
// route note below its hover, the route lenses after its lenses.
func (p *proxy) goRouteAfter(m *rpcMsg, path string) {
	var after func(any) any
	switch m.Method {
	case "textDocument/codeLens":
		if len(p.dirRoutes(path)) == 0 {
			return
		}
		after = func(v any) any {
			lenses, _ := v.([]any)
			return append(lenses, p.codeLenses(path)...)
		}
	case "textDocument/hover":
		var params struct {
			Position lspPosition `json:"position"`
		}
		if json.Unmarshal(m.Params, &params) != nil || len(p.dirRoutes(path)) == 0 {
			return
		}
		src := p.sourceText(path)
		note := p.routeNote(path, src, offsetOf(src, params.Position))
		if note == "" {
			return
		}
		after = func(v any) any { return withNote(v, note) }
	default:
		return
	}
	p.mu.Lock()
	if p.goAfter == nil {
		p.goAfter = map[string]func(any) any{}
	}
	p.goAfter[string(m.ID)] = after
	p.mu.Unlock()
}

// withGoAfter is gopls's answer to an editor request with what
// goRouteAfter kept for it added; an error gopls answers is left as it is.
func withGoAfter(m *rpcMsg, after func(any) any) bool {
	if after == nil || len(m.Error) > 0 {
		return false
	}
	var v any
	if len(m.Result) > 0 && json.Unmarshal(m.Result, &v) != nil {
		return false
	}
	b, err := json.Marshal(after(v))
	if err != nil {
		return false
	}
	m.Result = b
	return true
}

// sendsEvent reports whether an on… attribute of a shape's markup sends
// the method name, declared outside its header.
func sendsEvent(s *shapeScan, name string) bool {
	if s.wired[name] {
		return true
	}
	if !isExported(name) || s.fields[name] != nil {
		return false
	}
	for _, e := range s.events {
		if e.attr.valEnd > e.attr.valStart && wordIn(s.src[e.attr.valStart:e.attr.valEnd], name) >= 0 {
			return true
		}
	}
	return false
}
