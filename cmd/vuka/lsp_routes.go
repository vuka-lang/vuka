package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// The folder routes in the editor, from the route table the last build of
// the module made (the one `vuka routes` prints): a code lens above each
// page and handler, urls.….Path() chains and href="/…" literals as links to
// the page they name, the chains and literals naming a page as its
// references, and a page file or folder renamed under pages/ renaming the
// chains that name its pages.

// setRoutes keeps the route table of a build, each route at its place in
// its source rather than in the Go generated from it; a build whose routes
// broke keeps the last good table.
func (p *proxy) setRoutes(modRoot string, res *routesResult, gens []load.Generated) {
	if res == nil {
		return
	}
	byTarget := map[string]*load.Generated{}
	for i := range gens {
		if gens[i].Source != "" {
			byTarget[gens[i].Target] = &gens[i]
		}
	}
	routes := make([]*folderRoute, 0, len(res.routes))
	for _, r := range res.routes {
		if g := byTarget[r.File]; g != nil {
			c := *r
			c.File, c.Line = g.Source, sourceLine(g, r.Line, r)
			r = &c
		}
		routes = append(routes, r)
	}
	p.sem.mu.Lock()
	defer p.sem.mu.Unlock()
	if len(routes) == 0 && res.err() != nil {
		return
	}
	p.sem.routes = routes
	p.sem.modRoot = modRoot
}

// sourceLine is the line of the source a line of its generated Go declaring
// r comes from.
func sourceLine(g *load.Generated, line int, r *folderRoute) int {
	if g.Map == nil || line < 1 {
		return line
	}
	start := lineStart(g.Src, uint32(line-1))
	name := r.typ
	if r.handler != "" {
		name = r.handler
	}
	off := start
	if c := wordIn(lineAt(g.Src, min(start, len(g.Src))), name); c >= 0 && name != "" {
		off += c
	}
	if s, _, ok := g.Map.ToSource(off); ok && s <= len(g.From) {
		return strings.Count(string(g.From[:s]), "\n") + 1
	}
	return line
}

func (p *proxy) routes() ([]*folderRoute, string) {
	p.sem.mu.Lock()
	defer p.sem.mu.Unlock()
	return p.sem.routes, p.sem.modRoot
}

// routeEntry is a route as `vuka routes -json` prints it, its file relative
// to the module.
func routeEntry(r *folderRoute, modRoot string) map[string]any {
	file := r.File
	if rel, err := filepath.Rel(modRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = filepath.ToSlash(rel)
	}
	e := map[string]any{"method": r.Method, "path": r.Path, "page": r.Page, "kind": r.Kind, "file": file, "line": r.Line}
	for k, v := range map[string][]string{"middleware": r.Middleware, "guards": r.Guards, "layouts": r.Layouts} {
		if len(v) > 0 {
			e[k] = v
		}
	}
	if r.Root != "" {
		e["root"] = r.Root
	}
	return e
}

// codeLenses are a file's lenses: one above each page and handler it
// declares, GET /orgs/{org} · live · layout org.Layout · guard org.Guard,
// running vuka.showRoute with the routes' entries.
func (p *proxy) codeLenses(path string) []any {
	routes, modRoot := p.routes()
	byLine := map[int][]*folderRoute{}
	var lines []int
	for _, r := range routes {
		if !samePath(r.File, path) || r.Line < 1 {
			continue
		}
		if byLine[r.Line] == nil {
			lines = append(lines, r.Line)
		}
		byLine[r.Line] = append(byLine[r.Line], r)
	}
	if !strings.HasSuffix(path, ".vuka.html") {
		for line, group := range typeLenses(p.sourceText(path), p.dirRoutes(path)) {
			if byLine[line] == nil {
				lines = append(lines, line)
				byLine[line] = group
			}
		}
	}
	slices.Sort(lines)
	out := []any{}
	for _, line := range lines {
		group := byLine[line]
		var entries []any
		for _, r := range group {
			entries = append(entries, routeEntry(r, modRoot))
		}
		pos := lspPosition{Line: uint32(line - 1)}
		out = append(out, map[string]any{"range": lspRange{pos, pos},
			"command": map[string]any{"title": lensTitle(group), "command": "vuka.showRoute", "arguments": entries}})
	}
	return out
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// lensTitle is a route's line in a lens.
func lensTitle(group []*folderRoute) string {
	var methods []string
	for _, r := range group {
		methods = appendNew(methods, r.Method)
	}
	r := group[0]
	parts := []string{strings.Join(methods, ", ") + " " + r.Path, r.Kind}
	if n := len(r.Layouts); n > 0 {
		parts = append(parts, "layout "+r.Layouts[n-1])
	}
	if len(r.Guards) > 0 {
		parts = append(parts, "guard "+strings.Join(r.Guards, ", "))
	}
	return strings.Join(parts, " · ")
}

// chainSeg is a step of a urls chain: a field, or a method with its
// arguments.
type chainSeg struct {
	name       string
	start, end int
	args       [2]int // its ( and ), -1 when it has none
}

// urlChain is urls.Orgs.Org(7).Edit.Path(): its steps, Root and the final
// Path() left out.
type urlChain struct {
	start, end int
	segs       []chainSeg
}

var urlsRef = regexp.MustCompile(`\burls\.`)

// findChains finds the urls chains in a source.
func findChains(src []byte) []urlChain {
	var out []urlChain
	for _, m := range urlsRef.FindAllIndex(src, -1) {
		if m[0] > 0 && (src[m[0]-1] == '.' || isIdentByte(src[m[0]-1])) {
			continue
		}
		c := urlChain{start: m[0]}
		i := m[1]
		for {
			j := i
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			if j == i {
				break
			}
			seg := chainSeg{name: string(src[i:j]), start: i, end: j, args: [2]int{-1, -1}}
			k := j
			if k < len(src) && src[k] == '(' {
				e := argsEnd(src, k)
				if e < 0 {
					break
				}
				seg.args = [2]int{k, e}
				k = e + 1
			}
			c.segs = append(c.segs, seg)
			c.end = k
			if k+1 < len(src) && src[k] == '.' && isIdentByte(src[k+1]) {
				i = k + 1
				continue
			}
			break
		}
		if len(c.segs) > 0 && c.segs[0].name == "Root" {
			c.segs = c.segs[1:]
		} else if len(c.segs) == 0 {
			continue
		}
		if n := len(c.segs); n > 0 && c.segs[n-1].name == "Path" && c.segs[n-1].args[0] >= 0 {
			c.segs = c.segs[:n-1]
		}
		out = append(out, c)
	}
	return out
}

// argsEnd is the ) closing the ( at i, strings skipped; -1 when none does
// on the line.
func argsEnd(src []byte, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch c := src[i]; c {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		case '"', '\'', '`':
			i = skipQuoted(src, i) - 1
		case '\n':
			return -1
		}
	}
	return -1
}

func (c urlChain) names() []string {
	out := make([]string, len(c.segs))
	for i, s := range c.segs {
		out[i] = s.name
	}
	return out
}

// routeNames is a route's chain in urls.
func routeNames(r *folderRoute) []string {
	return nodeNames(r.node, r.leaf)
}

func nodeNames(node, leaf string) []string {
	var out []string
	for _, seg := range strings.Split(node, "/") {
		if seg != "" {
			out = append(out, (&urlNode{seg: seg}).name())
		}
	}
	if leaf != "" {
		out = append(out, leaf)
	}
	return out
}

// pageFor is the route a chain names: a page's (GET) first.
func pageFor(routes []*folderRoute, names []string) *folderRoute {
	var best *folderRoute
	for _, r := range routes {
		if r.Kind == "handler" || !slices.Equal(routeNames(r), names) {
			continue
		}
		if best == nil || rank(r) < rank(best) {
			best = r
		}
	}
	return best
}

func rank(r *folderRoute) int {
	switch {
	case r.Kind != "endpoint" && r.Method == "GET":
		return 0
	case r.Kind != "endpoint":
		return 1
	case r.Method == "GET":
		return 2
	}
	return 3
}

var hrefLiteral = regexp.MustCompile(`\bhref\s*=\s*(?:"(/[^"]*)"|'(/[^']*)'|\{\s*"(/[^"]*)"\s*\})`)

// hrefLit is a literal path in an href attribute.
type hrefLit struct {
	start, end int // the path, without its quotes
	path       string
}

func findHrefs(src []byte) []hrefLit {
	var out []hrefLit
	for _, m := range hrefLiteral.FindAllSubmatchIndex(src, -1) {
		for g := 2; g < 8; g += 2 {
			if m[g] >= 0 {
				out = append(out, hrefLit{m[g], m[g+1], string(src[m[g]:m[g+1]])})
			}
		}
	}
	return out
}

// pathMatches reports whether a literal path is one a route pattern
// serves: /orgs/7/edit is /orgs/{org}/edit's.
func pathMatches(pattern, lit string) bool {
	if i := strings.IndexAny(lit, "?#"); i >= 0 {
		lit = lit[:i]
	}
	if len(lit) > 1 {
		lit = strings.TrimSuffix(lit, "/")
	}
	ps, ls := strings.Split(pattern, "/"), strings.Split(lit, "/")
	for i, p := range ps {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}") {
			return i < len(ls)
		}
		if i >= len(ls) {
			return false
		}
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			if ls[i] == "" {
				return false
			}
			continue
		}
		if p != ls[i] {
			return false
		}
	}
	return len(ps) == len(ls)
}

// pageForPath is the page a literal path is served by.
func pageForPath(routes []*folderRoute, lit string) *folderRoute {
	var best *folderRoute
	for _, r := range routes {
		if r.Kind == "endpoint" || r.Kind == "handler" || r.Method != "GET" || !pathMatches(r.Path, lit) {
			continue
		}
		// The most literal pattern wins: /orgs/new over /orgs/{org}.
		if best == nil || strings.Count(r.Path, "{") < strings.Count(best.Path, "{") {
			best = r
		}
	}
	return best
}

// routeLocation is where a route is declared: its type's name, else its line.
func (p *proxy) routeLocation(r *folderRoute) map[string]any {
	pos := lspPosition{Line: uint32(max(r.Line-1, 0))}
	rng := lspRange{pos, pos}
	if src := p.sourceText(r.File); src != nil && r.Line >= 1 {
		start := lineStart(src, uint32(r.Line-1))
		line := lineAt(src, min(start, len(src)))
		name := r.typ
		if r.handler != "" {
			name = r.handler
		} else if r.fn != "" {
			name = r.Method
		}
		if c := wordIn(line, name); c >= 0 && name != "" {
			rng = lspRange{positionOf(src, start+c), positionOf(src, start+c+len(name))}
		}
	}
	return map[string]any{"uri": pathToURI(p.display(r.File)), "range": rng}
}

// routeTargetAt is the page a urls chain or a literal href at off names.
func (p *proxy) routeTargetAt(path string, src []byte, off int) (map[string]any, bool) {
	routes, _ := p.routes()
	if len(routes) == 0 {
		return nil, false
	}
	for _, c := range findChains(src) {
		if off >= c.start && off <= c.end {
			if r := pageFor(routes, c.names()); r != nil {
				return p.routeLocation(r), true
			}
		}
	}
	if strings.HasSuffix(path, ".go") {
		return nil, false
	}
	for _, h := range findHrefs(src) {
		if off >= h.start && off <= h.end {
			if r := pageForPath(routes, h.path); r != nil {
				return p.routeLocation(r), true
			}
		}
	}
	return nil, false
}

// withRouteLinks adds to a file's document links its urls chains and
// literal hrefs naming a page.
func (p *proxy) withRouteLinks(v any, vf *vfile) any {
	routes, _ := p.routes()
	if len(routes) == 0 || vf == nil {
		return v
	}
	links, _ := v.([]any)
	src := vf.text()
	add := func(start, end int, r *folderRoute) {
		loc := p.routeLocation(r)
		rng := loc["range"].(lspRange)
		links = append(links, map[string]any{"range": lspRange{positionOf(src, start), positionOf(src, end)},
			"target": loc["uri"].(string) + "#L" + strconv.Itoa(int(rng.Start.Line)+1), "tooltip": r.Method + " " + r.Path + " · " + r.Page})
	}
	for _, c := range findChains(src) {
		if r := pageFor(routes, c.names()); r != nil {
			add(c.start, c.end, r)
		}
	}
	for _, h := range findHrefs(src) {
		if r := pageForPath(routes, h.path); r != nil {
			add(h.start, h.end, r)
		}
	}
	if links == nil {
		return v
	}
	return links
}

// goRouteRequest answers go to definition on a urls chain in a .go file.
func (p *proxy) goRouteRequest(m *rpcMsg, path string) bool {
	if m.Method != "textDocument/definition" {
		return false
	}
	var params struct {
		Position lspPosition `json:"position"`
	}
	if json.Unmarshal(m.Params, &params) != nil {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	loc, ok := p.routeTargetAt(path, src, offsetOf(src, params.Position))
	if !ok {
		return false
	}
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": []any{loc}})
	return true
}

// pageRoutesAt are the routes of the page whose type's name is at off in
// the file declaring it; on the package clause, every page of the file.
func (p *proxy) pageRoutesAt(path string, src []byte, off int) []*folderRoute {
	routes, _ := p.routes()
	s, e := off, off
	for s > 0 && isIdentByte(src[s-1]) {
		s--
	}
	for e < len(src) && isIdentByte(src[e]) {
		e++
	}
	word := string(src[s:e])
	ls := bytes0LineStart(src, off)
	pkgLine := strings.HasPrefix(strings.TrimSpace(string(lineAt(src, ls))), "package ")
	var out []*folderRoute
	for _, r := range routes {
		if r.Kind == "handler" || !samePath(r.File, path) {
			continue
		}
		if pkgLine || r.typ == word && word != "" && strings.Contains(string(lineAt(src, ls)), "type "+word) {
			out = append(out, r)
		}
	}
	return out
}

func bytes0LineStart(src []byte, off int) int {
	off = min(off, len(src))
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}

// moduleSources calls f with each .go, .vuka and .vuka.html file of the
// module, as the editor has it.
func (p *proxy) moduleSources(modRoot string, f func(path string, src []byte)) {
	if modRoot == "" {
		return
	}
	_ = filepath.WalkDir(modRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != modRoot && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata" || name == "dist") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil && path != modRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, "_gen.go") || !(strings.HasSuffix(name, ".go") || isVuka(name)) {
			return nil
		}
		if src := p.sourceText(path); src != nil {
			f(path, src)
		}
		return nil
	})
}

// pageReferences are gopls's references to a page's type, then the urls
// chains and literal hrefs naming the page.
func (p *proxy) pageReferences(raw []byte, path string, pages []*folderRoute) []any {
	out := []any{}
	var params map[string]any
	_ = json.Unmarshal(raw, &params)
	p.view.RLock()
	p.mu.Lock()
	vf := p.virtual[p.bySource[path]]
	genPath := p.bySource[path]
	p.mu.Unlock()
	if vf != nil && params != nil {
		if pos, ok := toPosition(params["position"]); ok {
			params["textDocument"] = map[string]any{"uri": pathToURI(genPath)}
			params["position"] = vf.toGen(pos)
			if res, ok := p.ask("textDocument/references", params); ok {
				var v any
				if json.Unmarshal(res, &v) == nil {
					if l, ok := p.rewrite(v, vf, vf).([]any); ok {
						out = append(out, l...)
					}
				}
			}
		}
	}
	p.view.RUnlock()
	_, modRoot := p.routes()
	p.moduleSources(modRoot, func(file string, src []byte) {
		uri := pathToURI(p.display(file))
		loc := func(s, e int) {
			out = append(out, map[string]any{"uri": uri, "range": lspRange{positionOf(src, s), positionOf(src, e)}})
		}
		for _, c := range findChains(src) {
			for _, r := range pages {
				if slices.Equal(c.names(), routeNames(r)) {
					loc(c.start, c.end)
					break
				}
			}
		}
		if strings.HasSuffix(file, ".go") {
			return
		}
		for _, h := range findHrefs(src) {
			for _, r := range pages {
				if r.Method == "GET" && r.Kind != "endpoint" && pathMatches(r.Path, h.path) {
					loc(h.start, h.end)
					break
				}
			}
		}
	})
	return out
}

// chainRename is how a route's chain changes with a rename.
type chainRename struct {
	from, to []string
	params   []bool // which of to's steps take a parameter
}

// willRenameFiles answers workspace/willRenameFiles: for the pages under
// pages/ a renamed file or folder holds, an edit of every urls chain naming
// them (or a folder above them), applied before the rename.
func (p *proxy) willRenameFiles(m *rpcMsg) {
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": p.renameEdits(m.Params)})
}

// renameEdits is willRenameFiles's WorkspaceEdit, nil for none.
func (p *proxy) renameEdits(raw json.RawMessage) any {
	var params struct {
		Files []struct {
			OldURI string `json:"oldUri"`
			NewURI string `json:"newUri"`
		} `json:"files"`
	}
	_ = json.Unmarshal(raw, &params)
	routes, modRoot := p.routes()
	var renames []chainRename
	for _, f := range params.Files {
		old, new := uriToPath(f.OldURI), uriToPath(f.NewURI)
		for _, r := range routes {
			if r.Kind == "handler" || r.dir == nil || !(samePath(r.File, old) || strings.HasPrefix(r.File, old+string(filepath.Separator))) {
				continue
			}
			pagesDir := strings.TrimSuffix(r.dir.dir, filepath.FromSlash(r.dir.rel))
			pagesDir = strings.TrimSuffix(pagesDir, string(filepath.Separator))
			oldFile := r.File
			newFile := new + strings.TrimPrefix(r.File, old)
			if samePath(r.File, old) {
				oldFile, newFile = old, new
			}
			node, leaf, ok := renamedNode(r, pagesDir, oldFile, newFile)
			if !ok {
				continue
			}
			from, to := routeNames(r), nodeNames(node, leaf)
			if slices.Equal(from, to) {
				continue
			}
			var isParam []bool
			for _, seg := range strings.Split(node, "/") {
				if seg != "" {
					isParam = append(isParam, load.Bracketed(seg))
				}
			}
			if leaf != "" {
				isParam = append(isParam, false)
			}
			renames = append(renames, chainRename{from, to, isParam})
		}
	}
	changes := map[string]any{}
	if len(renames) > 0 {
		p.moduleSources(modRoot, func(file string, src []byte) {
			var edits []any
			for _, c := range findChains(src) {
				if e := renameChain(src, c, renames); e != nil {
					edits = append(edits, e...)
				}
			}
			if len(edits) > 0 {
				changes[pathToURI(p.display(file))] = edits
			}
		})
	}
	if len(changes) == 0 {
		return nil
	}
	return map[string]any{"changes": changes}
}

// renamedNode is a route's folder of urls and its field there once its
// file is newFile: a page named by its .vuka.html file (or [id] file) takes
// the new name, one named by its type keeps it and only its folder moves.
func renamedNode(r *folderRoute, pagesDir, oldFile, newFile string) (string, string, bool) {
	relDir := func(f string) (string, bool) {
		rel, err := filepath.Rel(pagesDir, filepath.Dir(f))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		if rel == "." {
			rel = ""
		}
		return urlRel(filepath.ToSlash(rel)), true
	}
	oldDir, ok1 := relDir(oldFile)
	newDir, ok2 := relDir(newFile)
	if !ok1 || !ok2 {
		return "", "", false
	}
	oldBase, newBase := filepath.Base(oldFile), filepath.Base(newFile)
	oldSeg, newSeg := fileSeg(oldBase), fileSeg(newBase)
	html := strings.HasSuffix(oldBase, ".vuka.html") && strings.HasSuffix(newBase, ".vuka.html")
	fileNode := func(dir, seg, base string) (string, string) {
		switch {
		case seg == "":
			return dir, ""
		case load.Bracketed(seg):
			return joinRel(dir, seg), ""
		}
		return dir, componentName(base)
	}
	if n, l := fileNode(oldDir, oldSeg, oldBase); (html || load.Bracketed(oldSeg)) && n == r.node && l == r.leaf {
		if !html && !load.Bracketed(newSeg) {
			return "", "", false
		}
		n, l := fileNode(newDir, newSeg, newBase)
		return n, l, true
	}
	rest := r.node
	switch {
	case oldDir == "":
	case r.node == oldDir:
		rest = ""
	case strings.HasPrefix(r.node, oldDir+"/"):
		rest = r.node[len(oldDir)+1:]
	default:
		return "", "", false
	}
	if rest == "" {
		return newDir, r.leaf, true
	}
	return joinRel(newDir, rest), r.leaf, true
}

// renameChain is the edits renaming a chain: one naming a renamed page, or
// a folder above it.
func renameChain(src []byte, c urlChain, renames []chainRename) []any {
	names := c.names()
	edit := func(s, e int, text string) any {
		return map[string]any{"range": lspRange{positionOf(src, s), positionOf(src, e)}, "newText": text}
	}
	for _, rn := range renames {
		switch {
		case slices.Equal(names, rn.from):
		case len(names) < len(rn.from) && len(rn.from) == len(rn.to) && slices.Equal(names, rn.from[:len(names)]):
		default:
			continue
		}
		if len(rn.from) == len(rn.to) {
			var edits []any
			for i, s := range c.segs {
				if rn.to[i] != s.name {
					edits = append(edits, edit(s.start, s.end, rn.to[i]))
				}
			}
			return edits
		}
		// The chain's length changes: written again, each parameter's
		// arguments carried over in order.
		var args []string
		for _, s := range c.segs {
			if s.args[0] >= 0 {
				args = append(args, string(src[s.args[0]:s.args[1]+1]))
			}
		}
		var b strings.Builder
		k := 0
		for i, name := range rn.to {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(name)
			if rn.params[i] {
				if k < len(args) {
					b.WriteString(args[k])
					k++
				} else {
					b.WriteString("()")
				}
			}
		}
		last := c.segs[len(c.segs)-1]
		end := last.end
		if last.args[1] >= 0 {
			end = last.args[1] + 1
		}
		return []any{edit(c.segs[0].start, end, b.String())}
	}
	return nil
}
