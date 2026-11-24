package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const mvUsage = `usage: vuka mv [-n] <from> <to>

Mv moves a folder under pages/, a [param] folder or one page file, to a
new place under pages/, and rewrites what names a route below it, so the
app builds and serves the same pages at their new paths:

  - route("…") and routePath("…") in .vuka and .vuka.html files, and
    web.Route, web.RouteURL and web.RoutePath in .go files, naming a route
    whose name the move changes: a moved route's, or one renamed by it, as
    orders and orders.order are when two paths differ only by parameters;
  - the urls chains naming them, written as the new chain (vuka fix routes
    writes a chain as route(…));
  - the import paths of the moved packages.

It prints the routes whose middleware, guards or layouts change with the
move. It writes nothing when the target exists, when either side is
outside pages/, or when the routes of the moved tree would clash or fail
their checks. In a git work tree a tracked source is moved with git mv.
The generated route.go, urls_gen.go and vuka_route_gen.go are written
again by the next build.

  -n, -dry-run  print the plan and write nothing
`

// mvCmd is vuka mv.
func mvCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(fs.Output(), mvUsage) }
	var dry bool
	fs.BoolVar(&dry, "n", false, "print the plan, write nothing")
	fs.BoolVar(&dry, "dry-run", false, "print the plan, write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return exitStatus(2)
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	from, err1 := filepath.Abs(fs.Arg(0))
	to, err2 := filepath.Abs(fs.Arg(1))
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	plan, err := planMove(root, modPath, from, to)
	if err != nil {
		var refused *moveRefusal
		if errors.As(err, &refused) {
			fmt.Fprintf(stderr, "vuka mv: %s\n", refused.msg)
			if len(refused.diags) > 0 {
				writeDiags(stderr, refused.diags, colorFor(stderr))
			}
			return exitStatus(1)
		}
		return err
	}
	plan.print(stdout, dry)
	if dry {
		return nil
	}
	return plan.apply()
}

// moveRefusal is why vuka mv writes nothing.
type moveRefusal struct {
	msg   string
	diags []diag.Diagnostic
}

func (r *moveRefusal) Error() string { return r.msg }

func refuse(format string, args ...any) error {
	return &moveRefusal{msg: fmt.Sprintf(format, args...)}
}

// movePlan is a move and every edit it takes, worked out before anything
// is written.
type movePlan struct {
	root, from, to string
	git            bool
	edits          []fileEdit
	routes         []movedRoute
	wraps          []string
	notes          []string
}

// fileEdit is a source rewritten: at path now, written at dest (path, or
// where the move takes it).
type fileEdit struct {
	path, dest string
	src, out   []byte
	splices    []spliceEdit // out is src with these, gofmt'ed for a .go file
	perm       fs.FileMode
}

// movedRoute is a route's name and path before and after.
type movedRoute struct {
	oldName, newName, oldPath, newPath string
}

// planMove works out moving from to to, both absolute paths under one
// pages/ directory of the module at root: it checks the moved tree's
// routes in a copy of the module, and refuses (a *moveRefusal) a move
// they don't pass.
func planMove(root, modPath, from, to string) (*movePlan, error) {
	root = realPathOf(root)
	from, to = realPathOf(from), realPathOf(to)
	fi, err := os.Lstat(from)
	if err != nil {
		return nil, refuse("%s: no such file or folder", relPath(root, from))
	}
	if _, err := os.Lstat(to); err == nil {
		return nil, refuse("%s exists already", relPath(root, to))
	}
	if from == to || within(to, from) {
		return nil, refuse("can't move %s into itself", relPath(root, from))
	}
	trees := scanRoutes(root, modPath, nil)
	tree := slices.IndexFunc(trees, func(t *routeTree) bool { return within(from, t.pages) })
	if tree < 0 || !within(to, trees[tree].pages) {
		return nil, refuse("both %s and %s must be below a pages/ folder, the same one", relPath(root, from), relPath(root, to))
	}
	if t := trees[tree]; within(from, t.urlsDir()) || within(to, t.urlsDir()) || from == t.urlsDir() || to == t.urlsDir() {
		return nil, refuse("pages/urls is generated: it isn't moved")
	}
	if !fi.IsDir() {
		switch {
		case !isVuka(from) && !strings.HasSuffix(from, ".go"):
			return nil, refuse("%s is no page source: move a folder, or a .vuka, .vuka.html or .go file", relPath(root, from))
		case sourceExt(from) != sourceExt(to):
			return nil, refuse("%s keeps its kind: name the target %s", relPath(root, from), sourceExt(from))
		}
	}
	plan := &movePlan{root: root, from: from, to: to}
	if gitWorkTree(root) {
		if gitFiles(root, to) {
			return nil, refuse("git has files at %s: commit or remove them first", relPath(root, to))
		}
		plan.git = gitFiles(root, from)
	}

	oldTrees, oldRes, err := routeTable(root, modPath)
	if err != nil {
		return nil, err
	}
	if oldRes.err() != nil {
		return nil, &moveRefusal{msg: "the routes have errors now: fix them first", diags: oldRes.diags}
	}
	cp, err := mkTemp("vuka-mv-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(cp)
	copyRoot := filepath.Join(cp, filepath.Base(root))
	if err := copyForMove(root, copyRoot); err != nil {
		return nil, err
	}
	copyRoot = realPathOf(copyRoot)
	inCopy := func(p string) string { return filepath.Join(copyRoot, relPath(root, p)) }
	if err := os.MkdirAll(filepath.Dir(inCopy(to)), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(inCopy(from), inCopy(to)); err != nil {
		return nil, err
	}
	clause, err := plan.packageClause()
	if err != nil {
		return nil, err
	}
	if clause != nil {
		if err := rewriteCopy(inCopy(to), clause.out); err != nil {
			return nil, err
		}
	}
	newTrees, newRes, err := routeTable(copyRoot, modPath)
	if err != nil {
		return nil, refuseIn(copyRoot, root, "the moved tree doesn't build", err)
	}
	if err := newRes.treeErr(); err != nil {
		return nil, &moveRefusal{msg: "the moved tree's routes fail their checks", diags: inRoot(newRes.diags, copyRoot, root)}
	}

	m := &mover{plan: plan, root: root, copyRoot: copyRoot, modPath: modPath, old: oldRes, new: newRes, oldTrees: oldTrees, newTrees: newTrees}
	m.pair()
	if err := m.rewrite(clause); err != nil {
		return nil, err
	}
	if len(plan.edits) > 0 {
		for _, e := range plan.edits {
			if err := rewriteCopy(inCopy(e.dest), e.out); err != nil {
				return nil, err
			}
		}
		if _, newRes, err = routeTable(copyRoot, modPath); err != nil {
			return nil, refuseIn(copyRoot, root, "the moved tree doesn't build", err)
		}
	}
	if newRes.err() != nil {
		return nil, &moveRefusal{msg: "the moved tree's routes fail their checks", diags: inRoot(newRes.diags, copyRoot, root)}
	}
	return plan, nil
}

// rewriteCopy writes a file of the copy anew, never through a link to the
// module's.
func rewriteCopy(path string, data []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// sourceExt is a source's kind: .vuka.html, .vuka or .go.
func sourceExt(p string) string {
	for _, e := range []string{".vuka.html", ".vuka", ".go"} {
		if strings.HasSuffix(p, e) {
			return e
		}
	}
	return filepath.Ext(p)
}

// within reports whether p is below dir.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// refuseIn is a refusal for err, its paths in the copy named as in root.
func refuseIn(copyRoot, root, msg string, err error) error {
	if ds, rest := diag.From(err); len(ds) > 0 && len(rest) == 0 {
		return &moveRefusal{msg: msg, diags: inRoot(ds, copyRoot, root)}
	}
	return refuse("%s: %s", msg, strings.ReplaceAll(err.Error(), copyRoot, root))
}

// inRoot is ds with the copy's paths named as in root.
func inRoot(ds []diag.Diagnostic, copyRoot, root string) []diag.Diagnostic {
	out := slices.Clone(ds)
	fix := func(s *diag.Span) {
		if s.File == copyRoot || within(s.File, copyRoot) {
			rel, _ := filepath.Rel(copyRoot, s.File)
			s.File = filepath.Join(root, rel)
		}
	}
	for i := range out {
		d := &out[i]
		fix(&d.Span)
		d.Related = slices.Clone(d.Related)
		for j := range d.Related {
			fix(&d.Related[j])
		}
		d.Message = strings.ReplaceAll(d.Message, copyRoot, root)
	}
	return out
}

// packageClause is the edit of a page file moved into another folder of
// sources: its package clause names theirs. nil when none is needed.
func (p *movePlan) packageClause() (*fileEdit, error) {
	if !isVuka(p.from) && !strings.HasSuffix(p.from, ".go") || filepath.Dir(p.from) == filepath.Dir(p.to) {
		return nil, nil
	}
	want := ""
	entries, _ := os.ReadDir(filepath.Dir(p.to))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !isVuka(n) && !isSourceFile(n) || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(filepath.Dir(p.to), n)); err == nil {
			if want = packageName(n, src); want != "" {
				break
			}
		}
	}
	if want == "" {
		return nil, nil
	}
	src, err := os.ReadFile(p.from)
	if err != nil {
		return nil, err
	}
	m := packageLine.FindSubmatchIndex(src)
	if m == nil || string(src[m[2]:m[3]]) == want {
		return nil, nil
	}
	e := &fileEdit{path: p.from, dest: p.to, src: src, splices: []spliceEdit{{m[2], m[3], want}}}
	e.out = spliceEdits(src, slices.Clone(e.splices))
	return e, nil
}

var packageLine = regexp.MustCompile(`(?m)^package\s+(\w+)`)

// mover works out the edits of a move from the route tables before and
// after it.
type mover struct {
	plan                    *movePlan
	root, copyRoot, modPath string
	old, new                *routesResult
	oldTrees, newTrees      routeTrees
	pairs                   map[*folderRoute]*folderRoute // an old route → itself after the move
}

// moved is where p is after the move.
func (m *mover) moved(p string) string {
	if p == m.plan.from {
		return m.plan.to
	}
	if within(p, m.plan.from) {
		rel, _ := filepath.Rel(m.plan.from, p)
		return filepath.Join(m.plan.to, rel)
	}
	return p
}

// routeKey is what keeps a route the same route through a move: its
// file, relative to the module, and what it is there.
func routeKey(r *folderRoute, file string) string {
	return strings.Join([]string{filepath.ToSlash(file), r.Method, r.Kind, r.typ, r.handler}, "\x00")
}

// pair pairs each route with itself after the move, and records the
// routes whose name, path, middleware, guards or layouts change.
func (m *mover) pair() {
	byKey := map[string]*folderRoute{}
	for _, r := range m.new.routes {
		byKey[routeKey(r, relPath(m.copyRoot, r.File))] = r
	}
	m.pairs = map[*folderRoute]*folderRoute{}
	seen := map[string]bool{}
	for _, r := range m.old.routes {
		n := byKey[routeKey(r, relPath(m.root, m.moved(r.File)))]
		if n == nil {
			continue
		}
		m.pairs[r] = n
		if (r.Name != n.Name || r.Path != n.Path) && !seen[r.Path] {
			seen[r.Path] = true
			m.plan.routes = append(m.plan.routes, movedRoute{r.Name, n.Name, r.Path, n.Path})
		}
		var changes []string
		for _, w := range []struct {
			what     string
			old, new []string
		}{{"middleware", r.Middleware, n.Middleware}, {"guards", r.Guards, n.Guards}, {"layouts", r.Layouts, n.Layouts}} {
			if !slices.Equal(w.old, w.new) {
				changes = append(changes, w.what+" "+listOrNone(w.old)+" → "+listOrNone(w.new))
			}
		}
		if len(changes) > 0 {
			m.plan.wraps = append(m.plan.wraps, fmt.Sprintf("%s %s: %s", n.Method, n.Path, strings.Join(changes, "; ")))
		}
	}
	slices.SortFunc(m.plan.routes, func(a, b movedRoute) int { return strings.Compare(a.oldPath, b.oldPath) })
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

// renames are the route names the move changes, old → new.
func (m *mover) renames() map[string]string {
	out := map[string]string{}
	for name, nr := range m.old.named {
		var rep *folderRoute
		for _, r := range m.old.routes {
			if r.Path != nr.Path || m.pairs[r] == nil {
				continue
			}
			if rep == nil || r.File == nr.At.File && r.Line == nr.At.Line {
				rep = r
			}
		}
		if rep != nil && m.pairs[rep].Name != name {
			out[name] = m.pairs[rep].Name
		}
	}
	return out
}

// rewrite works out the edits of the module's sources.
func (m *mover) rewrite(clause *fileEdit) error {
	renames := m.renames()
	chains := m.chainMap()
	oldBase, newBase := "", ""
	if fi, err := os.Stat(m.plan.from); err == nil && fi.IsDir() {
		oldBase, newBase = m.importPath(m.plan.from), m.importPath(m.plan.to)
	}
	var problems []string
	err := walkModule(m.root, func(path string) error {
		goFile := strings.HasSuffix(path, ".go")
		if !goFile && !isVuka(path) || strings.HasSuffix(path, "_gen.go") || slices.Contains(strings.Split(filepath.ToSlash(relPath(m.root, path)), "/"), "testdata") {
			return nil
		}
		for _, t := range m.oldTrees {
			if within(path, t.urlsDir()) {
				return nil
			}
		}
		src, err := os.ReadFile(path)
		if err != nil || bytes.HasPrefix(src, []byte("// Code generated")) {
			return nil
		}
		var splices []spliceEdit
		if clause != nil && path == clause.path {
			splices = append(splices, clause.splices...)
		}
		if len(renames) > 0 {
			for _, rc := range findRouteCalls(src) {
				if !rc.nameClosed || goFile && !strings.HasPrefix(rc.fn, "web.") {
					continue
				}
				if n, ok := renames[rc.name]; ok {
					q := string(src[rc.nameStart])
					lit := strconv.Quote(n)
					if q == "`" {
						lit = "`" + n + "`"
					}
					splices = append(splices, spliceEdit{rc.nameStart, rc.nameEnd, lit})
				}
			}
		}
		if bytes.Contains(src, []byte("urls.")) {
			for _, c := range findChains(src) {
				e, problem := chains.rewrite(src, c)
				if problem != "" {
					line, _ := lineCol(src, c.start)
					problems = append(problems, fmt.Sprintf("%s:%d: %s: %s", filepath.ToSlash(relPath(m.root, path)), line, string(src[c.start:c.end]), problem))
				}
				if e != nil {
					splices = append(splices, *e)
				}
			}
		}
		if oldBase != "" && bytes.Contains(src, []byte(`"`+oldBase)) {
			for _, q := range []string{`"`, `/`} {
				lit := []byte(`"` + oldBase + q)
				for at := 0; ; {
					i := bytes.Index(src[at:], lit)
					if i < 0 {
						break
					}
					s := at + i + 1
					splices = append(splices, spliceEdit{s, s + len(oldBase), newBase})
					at = s + len(oldBase)
				}
			}
		}
		splices = disjoint(splices)
		if len(splices) == 0 {
			return nil
		}
		fi, _ := os.Stat(path)
		e := fileEdit{path: path, dest: m.moved(path), src: src, splices: splices, perm: 0o644}
		if fi != nil {
			e.perm = fi.Mode().Perm()
		}
		e.out = spliceEdits(src, slices.Clone(splices))
		if goFile {
			if f, err := format.Source(src); err == nil && bytes.Equal(f, src) {
				if f, err := format.Source(e.out); err == nil {
					e.out = f
				}
			}
		}
		m.plan.edits = append(m.plan.edits, e)
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(m.plan.edits, func(a, b fileEdit) int { return strings.Compare(a.path, b.path) })
	if len(problems) > 0 {
		return refuse("these urls chains can't follow the move:\n\t%s", strings.Join(problems, "\n\t"))
	}
	return nil
}

// disjoint is edits sorted, those inside an earlier one dropped.
func disjoint(edits []spliceEdit) []spliceEdit {
	slices.SortStableFunc(edits, func(a, b spliceEdit) int {
		if a.from != b.from {
			return a.from - b.from
		}
		return b.to - a.to
	})
	var out []spliceEdit
	for _, e := range edits {
		if n := len(out); n > 0 && e.from < out[n-1].to {
			continue
		}
		out = append(out, e)
	}
	return out
}

// importPath is the import path of the package of the folder dir.
func (m *mover) importPath(dir string) string {
	return m.modPath + "/" + load.GoRel(filepath.ToSlash(relPath(m.root, dir)))
}

// chainMap is how the urls chains change with a move.
type chainMap struct {
	routes  []*folderRoute
	pairs   map[*folderRoute]*folderRoute
	queries map[string]string // a page's query struct, old name → new
}

func (m *mover) chainMap() *chainMap {
	cm := &chainMap{routes: m.old.routes, pairs: m.pairs, queries: map[string]string{}}
	after := map[string]string{}
	for _, t := range m.newTrees {
		for name, q := range t.urlQueries() {
			after[filepath.ToSlash(relPath(m.copyRoot, q.dir))+"\x00"+q.typ] = name
		}
	}
	for _, t := range m.oldTrees {
		for name, q := range t.urlQueries() {
			if n, ok := after[filepath.ToSlash(relPath(m.root, m.moved(q.dir)))+"\x00"+q.typ]; ok && n != name {
				cm.queries[name] = n
			}
		}
	}
	return cm
}

// queryPage is the page a query struct of urls is for.
type queryPage struct{ dir, typ string }

// urlQueries are the query structs of urls by name.
func (t *routeTree) urlQueries() map[string]queryPage {
	out := map[string]queryPage{}
	if t.urls == nil {
		return out
	}
	var walk func(n *urlNode)
	walk = func(n *urlNode) {
		for _, q := range n.queries {
			if q.active() && q.name != "" && q.dir != nil {
				out[q.name] = queryPage{q.dir.dir, q.typ}
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.urls)
	return out
}

// chainSteps are a route's steps in urls, each a name and whether it takes
// a parameter.
func chainSteps(r *folderRoute) ([]string, []bool) {
	var names []string
	var params []bool
	for _, seg := range strings.Split(r.node, "/") {
		if seg != "" {
			names = append(names, (&urlNode{seg: seg}).name())
			params = append(params, load.Bracketed(seg))
		}
	}
	if r.leaf != "" {
		names = append(names, r.leaf)
		params = append(params, false)
	}
	return names, params
}

// rewrite is the edit of the chain c of src the move takes, nil for none,
// or why it can't follow the move.
func (cm *chainMap) rewrite(src []byte, c urlChain) (*spliceEdit, string) {
	segs := c.segs
	if n := len(segs); n > 0 && segs[n-1].name == "URL" && segs[n-1].args[0] >= 0 {
		segs = segs[:n-1]
	}
	if len(segs) == 0 {
		return nil, ""
	}
	names := make([]string, len(segs))
	for i, s := range segs {
		names[i] = s.name
	}
	if len(names) == 1 {
		if n, ok := cm.queries[names[0]]; ok {
			return &spliceEdit{segs[0].start, segs[0].end, n}, ""
		}
	}
	var to []string
	var params []bool
	if r := pageFor(cm.routes, names); r != nil {
		n := cm.pairs[r]
		if n == nil {
			return nil, ""
		}
		to, params = chainSteps(n)
	} else {
		// A folder's value: where every route below it goes.
		agree := true
		for _, r := range cm.routes {
			old, _ := chainSteps(r)
			if r.Kind == "handler" || len(old) <= len(names) || !slices.Equal(old[:len(names)], names) || cm.pairs[r] == nil {
				continue
			}
			nn, np := chainSteps(cm.pairs[r])
			rest := old[len(names):]
			if len(nn) < len(rest) || !slices.Equal(nn[len(nn)-len(rest):], rest) {
				agree = false
				break
			}
			cut, cutParams := nn[:len(nn)-len(rest)], np[:len(np)-len(rest)]
			if to != nil && !slices.Equal(to, cut) {
				agree = false
				break
			}
			to, params = cut, cutParams
		}
		if !agree {
			return nil, "it names a folder whose pages don't move together"
		}
		if to == nil {
			return nil, ""
		}
	}
	if slices.Equal(to, names) {
		return nil, ""
	}
	if len(to) == 0 {
		return nil, "the route moves to the root, which has no chain"
	}
	var args []string
	for _, s := range segs {
		if s.args[0] >= 0 {
			args = append(args, string(src[s.args[0]:s.args[1]+1]))
		}
	}
	want := 0
	for _, p := range params {
		if p {
			want++
		}
	}
	if want != len(args) {
		return nil, fmt.Sprintf("it gives %d path %s, and the route takes %d after the move", len(args), countWord(len(args), "parameter", "parameters"), want)
	}
	var b strings.Builder
	k := 0
	for i, name := range to {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(name)
		if params[i] {
			b.WriteString(args[k])
			k++
		}
	}
	last := segs[len(segs)-1]
	end := last.end
	if last.args[0] >= 0 {
		end = last.args[1] + 1
	}
	return &spliceEdit{segs[0].start, end, b.String()}, ""
}

// print writes the plan.
func (p *movePlan) print(w io.Writer, dry bool) {
	how := "move"
	if p.git {
		how = "git mv"
	}
	verb := "moving"
	if dry {
		verb = "would move"
	}
	fmt.Fprintf(w, "%s %s → %s (%s)\n", verb, filepath.ToSlash(relPath(p.root, p.from)), filepath.ToSlash(relPath(p.root, p.to)), how)
	if len(p.routes) > 0 {
		fmt.Fprintln(w, "routes:")
		for _, r := range p.routes {
			fmt.Fprintf(w, "\t%s %s → %s %s\n", r.oldName, r.oldPath, r.newName, r.newPath)
		}
	}
	if len(p.edits) > 0 {
		fmt.Fprintln(w, "rewrites:")
		for _, e := range p.edits {
			for _, s := range e.splices {
				line, _ := lineCol(e.src, s.from)
				fmt.Fprintf(w, "\t%s:%d: %s → %s\n", filepath.ToSlash(relPath(p.root, e.path)), line, e.src[s.from:s.to], s.text)
			}
		}
	}
	if len(p.wraps) > 0 {
		fmt.Fprintln(w, "middleware, guards and layouts that change:")
		for _, l := range p.wraps {
			fmt.Fprintf(w, "\t%s\n", l)
		}
	}
	for _, n := range p.notes {
		fmt.Fprintln(w, n)
	}
}

// apply makes the move and writes the edits; when a step fails, it puts
// back what it had done.
func (p *movePlan) apply() (err error) {
	for _, e := range p.edits {
		if now, err := os.ReadFile(e.path); err != nil || !bytes.Equal(now, e.src) {
			return fmt.Errorf("vuka mv: %s changed while the move was worked out: nothing written, run it again", relPath(p.root, e.path))
		}
	}
	backup, err := mkTemp("vuka-mv-backup-")
	if err != nil {
		return err
	}
	for i, e := range p.edits {
		if err := os.WriteFile(filepath.Join(backup, strconv.Itoa(i)), e.src, 0o644); err != nil {
			os.RemoveAll(backup)
			return err
		}
	}
	var undo []func() error
	defer func() {
		if err == nil {
			os.RemoveAll(backup)
			return
		}
		var failed []error
		for i := len(undo) - 1; i >= 0; i-- {
			if u := undo[i](); u != nil {
				failed = append(failed, u)
			}
		}
		if len(failed) > 0 {
			err = fmt.Errorf("%w; putting it back failed too (%v): the sources as they were are in %s", err, errors.Join(failed...), backup)
			return
		}
		os.RemoveAll(backup)
		err = fmt.Errorf("%w: nothing changed", err)
	}()
	var made []string
	for d := filepath.Dir(p.to); !fileExists(d); d = filepath.Dir(d) {
		made = append(made, d)
	}
	if err := os.MkdirAll(filepath.Dir(p.to), 0o755); err != nil {
		return err
	}
	undo = append(undo, func() error {
		for _, d := range made {
			os.Remove(d)
		}
		return nil
	})
	if err := p.rename(p.from, p.to); err != nil {
		return err
	}
	undo = append(undo, func() error { return p.rename(p.to, p.from) })
	for _, e := range p.edits {
		if err := os.WriteFile(e.dest, e.out, e.perm); err != nil {
			return err
		}
		undo = append(undo, func() error { return os.WriteFile(e.dest, e.src, e.perm) })
	}
	return nil
}

// rename moves from to to, with git mv when the plan says so.
func (p *movePlan) rename(from, to string) error {
	if !p.git {
		return os.Rename(from, to)
	}
	c := exec.Command("git", "mv", "--", from, to)
	c.Dir = p.root
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("git mv: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// gitWorkTree reports whether root is in a git work tree.
func gitWorkTree(root string) bool {
	c := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	c.Dir = root
	out, err := c.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitFiles reports whether git's index has files at path.
func gitFiles(root, path string) bool {
	c := exec.Command("git", "ls-files", "--", path)
	c.Dir = root
	out, err := c.Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

// copyForMove copies the module at root to dst for the move's checks:
// large files hard-linked, the hidden folders, node_modules and nested
// modules left out, and go.mod's and a go.work's directory paths made
// absolute.
func copyForMove(root, dst string) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			if n := d.Name(); path != root && (strings.HasPrefix(n, ".") || n == "node_modules" || fileExists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() || rel == "go.mod" {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Size() > 4<<20 && os.Link(path, out) == nil {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	for _, r := range mf.Replace {
		if r.New.Version == "" && !filepath.IsAbs(r.New.Path) {
			_ = mf.AddReplace(r.Old.Path, r.Old.Version, filepath.Join(root, r.New.Path), "")
		}
	}
	data, err = mf.Format()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dst, "go.mod"), data, 0o644); err != nil {
		return err
	}
	return copyWork(root, dst)
}

// copyWork writes the go.work above root, if one rules it, in dst: the
// copy in place of the module, every other path absolute.
func copyWork(root, dst string) error {
	if os.Getenv("GOWORK") != "" {
		return nil
	}
	for d := root; ; d = filepath.Dir(d) {
		file := filepath.Join(d, "go.work")
		data, err := os.ReadFile(file)
		if err == nil {
			wf, err := modfile.ParseWork(file, data, nil)
			if err != nil {
				return err
			}
			abs := func(p string) string {
				if filepath.IsAbs(p) {
					return p
				}
				return filepath.Join(d, p)
			}
			for _, u := range slices.Clone(wf.Use) {
				p := abs(u.Path)
				_ = wf.DropUse(u.Path)
				if realPathOf(p) == root {
					p = "."
				}
				_ = wf.AddUse(p, u.ModulePath)
			}
			for _, r := range wf.Replace {
				if r.New.Version == "" {
					_ = wf.AddReplace(r.Old.Path, r.Old.Version, abs(r.New.Path), "")
				}
			}
			wf.Cleanup()
			return os.WriteFile(filepath.Join(dst, "go.work"), modfile.Format(wf.Syntax), 0o644)
		}
		if filepath.Dir(d) == d {
			return nil
		}
	}
}
