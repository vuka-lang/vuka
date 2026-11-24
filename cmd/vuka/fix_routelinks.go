package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// vuka fix routes also writes the urls chains it can name statically as
// links by route name: urls.Orgs.Org(id).Edit.URL(ctx) is
// route("orgs.edit", id) in a Vuka file and web.Route(ctx, "orgs.edit", id)
// in Go, .Path() routePath(…), a query struct urls.Orders{Page: 2}
// query{"page": 2}. A chain it can't name — a folder's value kept in a
// variable, passed along, a query given as a variable — stays, with W0706.

// fixRouteLinks finds the chains of the module's sources to rewrite.
func fixRouteLinks(m *fixModule) ([]fixChange, error) {
	_, res, err := routeTable(m.root, m.modPath)
	if err != nil || res == nil || len(res.named) == 0 {
		return nil, err
	}
	byPath := map[string]*namedRoute{}
	for _, nr := range res.named {
		byPath[nr.Path] = nr
	}
	var changes []fixChange
	err = walkModule(m.root, func(path string) error {
		goFile := strings.HasSuffix(path, ".go")
		if !goFile && !isVuka(path) || strings.HasSuffix(path, "_gen.go") || strings.Contains(path, string(filepath.Separator)+"urls"+string(filepath.Separator)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(src), "urls.") {
			return nil
		}
		out, n, notes := rewriteRouteLinks(src, goFile, strings.HasSuffix(path, ".vuka.html"), res.routes, byPath)
		rel := relPath(m.root, path)
		for _, note := range notes {
			m.notes = append(m.notes, rel+":"+note)
		}
		if n == 0 {
			return nil
		}
		changes = append(changes, fixChange{
			what:  fmt.Sprintf("%s: %d urls %s written by route name", filepath.ToSlash(rel), n, countWord(n, "chain", "chains")),
			apply: func() error { return os.WriteFile(path, out, 0o644) },
		})
		return nil
	})
	return changes, err
}

// rewriteRouteLinks rewrites the chains of src it can name; it returns the
// source, how many it rewrote and a W0706 note for each it left.
func rewriteRouteLinks(src []byte, goFile, html bool, routes []*folderRoute, byPath map[string]*namedRoute) ([]byte, int, []string) {
	script := scriptSpan(src, html)
	var edits []spliceEdit
	var notes []string
	leave := func(c urlChain, why string) {
		line, _ := lineCol(src, c.start)
		notes = append(notes, fmt.Sprintf("%d: W0706 %s: %s", line, string(src[c.start:c.end]), why))
	}
	for _, c := range findChains(src) {
		text := string(src[c.start:c.end])
		segs, mode := c.segs, ""
		switch {
		case len(segs) > 0 && segs[len(segs)-1].name == "URL" && segs[len(segs)-1].args[0] >= 0:
			mode, segs = "URL", segs[:len(segs)-1]
		case strings.HasSuffix(text, ".Path()"):
			mode = "Path"
		default:
			leave(c, "the chain is kept or passed along: name its route where it links, route(\"…\", …)")
			continue
		}
		names := make([]string, len(segs))
		var args []string
		for i, s := range segs {
			names[i] = s.name
			if s.args[0] >= 0 {
				if a := strings.TrimSpace(string(src[s.args[0]+1 : s.args[1]])); a != "" {
					args = append(args, a)
				}
			}
		}
		r := pageFor(routes, names)
		nr := (*namedRoute)(nil)
		if r != nil {
			nr = byPath[r.Path]
		}
		if nr == nil || len(args) != len(nr.Params) {
			leave(c, "no route has this chain's path")
			continue
		}
		ctx, query := "", ""
		if mode == "URL" {
			u := c.segs[len(c.segs)-1]
			parts := splitTopLevel(string(src[u.args[0]+1 : u.args[1]]))
			if len(parts) == 0 || len(parts) > 2 {
				leave(c, "its URL takes ctx and one query")
				continue
			}
			ctx = parts[0]
			if len(parts) == 2 {
				q, ok := queryLiteral(parts[1], nr)
				if !ok {
					leave(c, "its query isn't a literal of the page's query struct")
					continue
				}
				query = q
			}
		}
		inScript := c.start >= script[0] && c.start < script[1]
		var call string
		switch {
		case goFile && mode == "Path":
			leave(c, "Path() in Go has no ctx: web.RoutePath(ctx, …) needs one")
			continue
		case goFile:
			call = "web.Route(" + ctx + ", " + strconv.Quote(nr.Name)
		case mode == "Path" && inScript:
			leave(c, "Path() in a template's Go: routePath(…) needs a ctx in scope")
			continue
		case mode == "Path":
			call = "routePath(" + strconv.Quote(nr.Name)
		case ctx != "ctx":
			leave(c, "its URL is given "+ctx+", not ctx")
			continue
		default:
			call = "route(" + strconv.Quote(nr.Name)
		}
		for _, a := range args {
			call += ", " + a
		}
		if query != "" {
			call += ", " + query
		}
		edits = append(edits, spliceEdit{c.start, c.end, call + ")"})
	}
	if len(edits) == 0 {
		return src, 0, notes
	}
	out := spliceEdits(src, edits)
	out = fixLinkImports(out, goFile)
	if goFile {
		if b, err := format.Source(out); err == nil {
			out = b
		}
	}
	return out, len(edits), notes
}

// scriptSpan is the span of a template's Go: its script, or the header
// above its markup; all of a .vuka file.
func scriptSpan(src []byte, html bool) [2]int {
	s := string(src)
	if !html {
		return [2]int{0, len(s)}
	}
	if i := strings.Index(s, "<script lang=\"go\">"); i >= 0 {
		if j := strings.Index(s[i:], "</script>"); j >= 0 {
			return [2]int{i, i + j}
		}
		return [2]int{i, len(s)}
	}
	if i := strings.Index(s, "\n<"); i >= 0 {
		return [2]int{0, i}
	}
	return [2]int{0, 0}
}

// queryLiteral is a query struct's literal, urls.Orders{Page: 2}, as
// query{"page": 2}.
func queryLiteral(text string, nr *namedRoute) (string, bool) {
	text = strings.TrimSpace(text)
	open := strings.IndexByte(text, '{')
	if open < 0 || !strings.HasPrefix(text, "urls.") || !strings.HasSuffix(text, "}") {
		return "", false
	}
	var kvs []string
	for _, f := range splitTopLevel(text[open+1 : len(text)-1]) {
		name, value, ok := strings.Cut(f, ":")
		if !ok {
			return "", false
		}
		name = strings.TrimSpace(name)
		i := slices.IndexFunc(nr.Query, func(q routeQuery) bool { return upperFirst(q.Field) == name })
		if i < 0 {
			return "", false
		}
		kvs = append(kvs, strconv.Quote(nr.Query[i].Key)+": "+strings.TrimSpace(value))
	}
	return "query{" + strings.Join(kvs, ", ") + "}", true
}

// splitTopLevel splits s at its commas outside brackets and literals.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '`':
			i = skipLit(s, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

// fixLinkImports drops the urls import nothing uses any more, and, in Go,
// imports web for web.Route.
func fixLinkImports(src []byte, goFile bool) []byte {
	s := string(src)
	lines := strings.Split(s, "\n")
	usesURLs := len(findChains(src)) > 0
	hasWeb := strings.Contains(s, "\"github.com/vuka-lang/web/v2\"")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasSuffix(t, "/pages/urls\"") || strings.Contains(t, "//") {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		switch {
		case !usesURLs && goFile && !hasWeb:
			lines[i] = indent + "\"github.com/vuka-lang/web/v2\""
		case !usesURLs:
			lines = slices.Delete(lines, i, i+1)
		case goFile && !hasWeb:
			lines[i] = l + "\n" + indent + "\"github.com/vuka-lang/web/v2\""
		}
		break
	}
	return []byte(strings.Join(lines, "\n"))
}
