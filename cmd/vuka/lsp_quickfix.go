package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Quick fixes: the diagnostics of a .vuka.html file with one obvious fix,
// offered where the diagnostic is (or, for the ones vukavet reports, where
// the file has what it reports, whether or not the editor shows vukavet's
// diagnostics):
//
//   - W0102, a live list's item with no key: key its first element by the
//     item's ID-like field (ID, Id, Key, UUID, Slug, Code, then any field
//     ending in ID), else by the loop's index, naming an index written _;
//   - W0703, a layout reading the request: a ui.PageTitle or ui.PageCrumbs
//     read in Load and kept in a field moves into the markup, and a link
//     marked current by the request's path becomes a ui.Link marked in the
//     browser (active="prefix", or "exact" for a path compared with ==);
//     a field left unread goes, with what set it;
//   - E0142, a template file with no package clause: its directory's;
//   - E0329, ui.Live embedded by pointer: embedded by value;
//   - W0701, a pages/ tree no app serves (on its route.go): its Routes()
//     passed to the web.New of the main that owns it, as vuka fix routes
//     does (pagesRoutesEdit); disabled, with what to add, when no one main
//     can be edited.
//
// E0155 (tracked state in a .go file) moves the method to the shape's
// logic file (goFileFixes, lsp_refactor_move.go); W0107 (the Go-header
// form) is withShapeScriptAction's (lsp_shapescript.go).

// templateFixes are the quick fixes at the request's range of a .vuka.html
// file.
func templateFixes(r actionRequest) []any {
	var out []any
	add := func(a any) {
		if a != nil {
			out = append(out, a)
		}
	}
	if a := packageFix(r); a != nil {
		add(*a)
	}
	f := readShape(r.path, r.src)
	if f == nil {
		return out
	}
	if a := liveValueFix(r, f); a != nil {
		add(*a)
	}
	for _, sp := range keySpots(f) {
		if !sp.touches(r) {
			continue
		}
		d := r.diag("W0102")
		if !f.shape.Live && d == nil {
			continue
		}
		a := codeAction{Title: "Give the item a key", Kind: "quickfix", IsPreferred: true}
		if d != nil {
			a.Diagnostics = []map[string]any{d}
		}
		add(lazyAction(r, a, map[string]any{"action": "key", "at": float64(sp.tag.start)}))
	}
	if filepath.Base(r.path) == "_layout.vuka.html" {
		for _, a := range layoutFixes(r, f) {
			add(a)
		}
	}
	return out
}

// touches reports whether the request's range touches [start, end).
func (r actionRequest) touches(start, end int) bool {
	return r.start <= end && r.end >= start
}

// packageFix adds a template file's missing package clause (E0142).
func packageFix(r actionRequest) *codeAction {
	if transpile.ParseTemplateHead(r.src).Package != "" || len(bytes.TrimSpace(r.src)) == 0 {
		return nil
	}
	pkg := format.DirPackage(filepath.Dir(r.path))
	out := format.TemplatePackage(r.src, pkg)
	if bytes.Equal(out, r.src) {
		return nil
	}
	// One insertion: the text the clause goes in front of is the file's.
	at := 0
	for at < len(r.src) && r.src[at] == out[at] {
		at++
	}
	tail := len(r.src) - at
	ins := string(out[at : len(out)-tail])
	if string(out[len(out)-tail:]) != string(r.src[at:]) {
		return nil
	}
	a := codeAction{Title: "Add package " + pkg, Kind: "quickfix", IsPreferred: true,
		Edit: change{{path: r.path, old: r.src, edits: []edit{{at, at, ins}}}}.workspaceEdit(r.uri)}
	if d := r.diag("E0142"); d != nil {
		a.Diagnostics = []map[string]any{d}
	}
	return &a
}

var livePointer = regexp.MustCompile(`(?m)^[ \t]*(\*)[A-Za-z_]\w*\.Live[ \t]*(?://.*)?$`)

// liveValueFix embeds a pointer-embedded ui.Live by value (E0329).
func liveValueFix(r actionRequest, f *shapeFile) *codeAction {
	view := transpile.HeaderView(f.src)
	from := lineOffset(f.src, f.shape.TypeLine-1)
	m := livePointer.FindSubmatchIndex(view[from:f.goEnd])
	if m == nil {
		return nil
	}
	star := from + m[2]
	d := r.diag("E0329")
	if d == nil && !r.touches(lineOffset(f.src, f.shape.TypeLine-1), from+m[1]) {
		return nil
	}
	a := codeAction{Title: "Embed ui.Live by value", Kind: "quickfix", IsPreferred: true,
		Edit: change{{path: r.path, old: r.src, edits: []edit{{star, star + 1, ""}}}}.workspaceEdit(r.uri)}
	if d != nil {
		a.Diagnostics = []map[string]any{d}
	}
	return &a
}

// A loop of the markup whose first element is an HTML element without a
// key (W0102).

type keySpot struct {
	head        [2]int // the loop's head, its line
	index, item string // the loop's variables: for index, item := range; item "" for one
	indexAt     int    // where the index is written
	itemAt      int
	tag         jsxTag
}

func (sp keySpot) touches(r actionRequest) bool {
	return r.touches(sp.head[0], sp.head[1]) || r.touches(sp.tag.start, sp.tag.end) || r.diag("W0102") != nil
}

var loopVars = regexp.MustCompile(`^(\s*\{?for\s+)([A-Za-z_]\w*)(?:(\s*,\s*)([A-Za-z_]\w*))?\s*:?=\s*range\b.*\{\s*$`)

// keySpots are the markup's loops whose first element has no key.
func keySpots(f *shapeFile) []keySpot {
	var out []keySpot
	src := f.src
	for off := f.markup; off < len(src); {
		end := bytes.IndexByte(src[off:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += off
		}
		m := loopVars.FindSubmatchIndex(src[off:end])
		if m != nil {
			sp := keySpot{head: [2]int{off, end}, index: string(src[off+m[4] : off+m[5]]), indexAt: off + m[4]}
			if m[8] >= 0 {
				sp.item, sp.itemAt = string(src[off+m[8]:off+m[9]]), off+m[8]
			}
			i := end
			for i < len(src) && isSpace(src[i]) {
				i++
			}
			if t, ok := markupTag(src, i); ok && !t.close && t.name != "" && !isComponentName(t.name) && !hasAttr(t, "key") {
				sp.tag = t
				out = append(out, sp)
			}
		}
		off = end + 1
	}
	return out
}

func isComponentName(tag string) bool {
	return tag[0] >= 'A' && tag[0] <= 'Z' || strings.Contains(tag, ".")
}

func hasAttr(t jsxTag, name string) bool {
	for _, a := range t.attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

// keyFix keys the first element of the loop whose first element's tag
// starts at at.
func keyFix(path string, src []byte, at int, read load.ReadFunc) (change, error) {
	f := readShape(path, src)
	if f == nil {
		return nil, errors.New("not a shape file")
	}
	var sp *keySpot
	for _, s := range keySpots(f) {
		if s.tag.start == at {
			sp = &s
		}
	}
	if sp == nil {
		return nil, errors.New("no item without a key there")
	}
	key := ""
	var edits []edit
	if sp.item != "" && sp.item != "_" {
		if field := idField(path, src, sp.itemAt, read); field != "" {
			key = sp.item + "." + field
		}
	}
	if key == "" {
		switch {
		case sp.item == "" && sp.index != "_":
			key = sp.index // for k := range: the index, a map's key
		case sp.index != "_":
			key = sp.index
		default:
			name := freeName(src, "i", "idx", "n")
			edits = append(edits, edit{sp.indexAt, sp.indexAt + 1, name})
			key = name
		}
	}
	at = sp.tag.nameEnd
	edits = append(edits, edit{at, at, " key={" + key + "}"})
	return change{{path: path, old: src, edits: edits}}, nil
}

// freeName is the first of names the file doesn't use as a word.
func freeName(src []byte, names ...string) string {
	for _, n := range names {
		if wordIn(src, n) < 0 {
			return n
		}
	}
	for i := 2; ; i++ {
		if n := "i" + strconv.Itoa(i); wordIn(src, n) < 0 {
			return n
		}
	}
}

// idField is the field of the loop item declared at off that identifies
// it, by the item's type: "" when it has none, or can't be typed.
func idField(path string, src []byte, off int, read load.ReadFunc) string {
	tp, err := loadTyped(path, read)
	if err != nil {
		return ""
	}
	tf := tp.files[path]
	if tf == nil {
		return ""
	}
	for _, id := range tp.idents(tf, off, off+1) {
		t := id.obj.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok {
			return ""
		}
		fields := map[string]bool{}
		var suffixed []string
		for i := 0; i < st.NumFields(); i++ {
			if fl := st.Field(i); fl.Exported() && !fl.Embedded() {
				fields[fl.Name()] = true
				if strings.HasSuffix(fl.Name(), "ID") {
					suffixed = append(suffixed, fl.Name())
				}
			}
		}
		for _, n := range idFields {
			if fields[n] {
				return n
			}
		}
		if len(suffixed) > 0 {
			return suffixed[0]
		}
	}
	return ""
}

// The layout fixes (W0703).

// loadRead is a statement of a layout's Load setting a field to what it
// reads: recv.field = expr.
type loadRead struct {
	field     string
	expr      string // the call's text, as the markup writes it
	stmt      [2]int // the statement's lines
	pageRead  bool   // ui.PageTitle or ui.PageCrumbs
	reqRead   bool   // the request
	loadDecl  headerDecl
	onlyStmts int // the statements of Load's body
}

// loadReads are the field assignments of the layout's Load.
func loadReads(f *shapeFile) []loadRead {
	var load *headerDecl
	for _, d := range f.methods() {
		if d.name == "Load" {
			d := d
			load = &d
		}
	}
	if load == nil {
		return nil
	}
	const pre = "package p\n"
	text := pre + string(f.src[load.declStart:load.end])
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "", text, 0)
	if err != nil || len(af.Decls) != 1 {
		return nil
	}
	fd, ok := af.Decls[0].(*ast.FuncDecl)
	if !ok || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List[0].Names) != 1 {
		return nil
	}
	recv := fd.Recv.List[0].Names[0].Name
	ctxName := ""
	for _, p := range fd.Type.Params.List {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Context" && len(p.Names) == 1 {
			ctxName = p.Names[0].Name
		}
	}
	uiName := uiLocalName(f)
	off := func(p token.Pos) int { return fset.Position(p).Offset - len(pre) + load.declStart }
	var out []loadRead
	for _, st := range fd.Body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Tok != token.ASSIGN {
			continue
		}
		lhs, ok := as.Lhs[0].(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if x, ok := lhs.X.(*ast.Ident); !ok || x.Name != recv {
			continue
		}
		lr := loadRead{field: lhs.Sel.Name, loadDecl: *load, onlyStmts: len(fd.Body.List)}
		s, e := off(st.Pos()), off(st.End())
		lr.stmt = [2]int{bytes.LastIndexByte(f.src[:s], '\n') + 1, e}
		if nl := bytes.IndexByte(f.src[e:], '\n'); nl >= 0 {
			lr.stmt[1] = e + nl + 1
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && len(call.Args) == 1 {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == uiName && (sel.Sel.Name == "PageTitle" || sel.Sel.Name == "PageCrumbs") {
					if a, ok := call.Args[0].(*ast.Ident); ok && a.Name == ctxName {
						lr.pageRead, lr.expr = true, uiName+"."+sel.Sel.Name+"(ctx)"
					}
				}
			}
		}
		rhs := string(f.src[off(as.Rhs[0].Pos()):off(as.Rhs[0].End())])
		lr.reqRead = !lr.pageRead && regexp.MustCompile(`\b(?:Request|PageQuery)\(|\.URL\b`).MatchString(rhs)
		out = append(out, lr)
	}
	return out
}

// uiLocalName is the name the shape imports package ui by.
func uiLocalName(f *shapeFile) string {
	for _, sp := range f.imports.specs {
		if sp.path == uiPath {
			if sp.name != "" {
				return sp.name
			}
			return "ui"
		}
	}
	return "ui"
}

// layoutFixes are the W0703 fixes at the request's range of a layout.
func layoutFixes(r actionRequest, f *shapeFile) []any {
	reads := loadReads(f)
	d := r.diag("W0703")
	typeLine := lineOffset(f.src, f.shape.TypeLine-1)
	var out []any
	for _, lr := range reads {
		if !lr.pageRead || !(d != nil || r.touches(lr.stmt[0], lr.stmt[1]) || r.touches(typeLine, typeLine+1) || r.start >= f.markup) {
			continue
		}
		a := codeAction{Title: "Read " + strings.TrimSuffix(lr.expr, "(ctx)") + " in the markup", Kind: "quickfix", IsPreferred: true}
		if d != nil {
			a.Diagnostics = []map[string]any{d}
		}
		out = append(out, lazyAction(r, a, map[string]any{"action": "pageRead", "field": lr.field}))
	}
	requestRead := false
	for _, lr := range reads {
		requestRead = requestRead || lr.reqRead
	}
	if requestRead || d != nil {
		for _, l := range activeLinks(f) {
			if !r.touches(l.span[0], l.span[1]) {
				continue
			}
			a := codeAction{Title: "Mark the current link in the browser: ui.Link active=\"" + l.active + "\"", Kind: "quickfix", IsPreferred: l.ifElse}
			if d != nil {
				a.Diagnostics = []map[string]any{d}
			}
			out = append(out, lazyAction(r, a, map[string]any{"action": "activeLink", "at": float64(l.span[0])}))
		}
	}
	return out
}

// pageReadFix moves Load's read of the page's title or breadcrumbs into
// the field's place in the markup.
func pageReadFix(path string, src []byte, field string, read load.ReadFunc) (change, error) {
	f := readShape(path, src)
	if f == nil {
		return nil, errors.New("not a shape file")
	}
	for _, lr := range loadReads(f) {
		if lr.field != field || !lr.pageRead {
			continue
		}
		tp, err := loadTyped(path, read)
		if err != nil {
			return nil, err
		}
		uses, err := fieldUses(tp, path, src, f, field)
		if err != nil {
			return nil, err
		}
		var edits []edit
		for _, u := range uses {
			switch {
			case u[0] >= f.markup:
				edits = append(edits, edit{u[0], u[1], lr.expr})
			case u[0] >= lr.stmt[0] && u[0] < lr.stmt[1]:
			default:
				return nil, errors.New(field + " is read in the Go too: rewrite it by hand")
			}
		}
		return dropRead(path, src, f, lr, edits, read)
	}
	return nil, errors.New("Load reads no page title into " + field)
}

// dropRead is the change edits makes with the field of lr, and the
// statement setting it, removed: Load too, when it does nothing else;
// imports left unused go.
func dropRead(path string, src []byte, f *shapeFile, lr loadRead, edits []edit, read load.ReadFunc) (change, error) {
	if lr.onlyStmts == 2 && strings.Contains(string(src[lr.stmt[1]:lr.loadDecl.end]), "return nil") {
		s := lr.loadDecl.start
		if prev := bytes.LastIndexByte(src[:max(s-1, 0)], '\n') + 1; s > 0 && strings.TrimSpace(string(src[prev:s])) == "" {
			s = prev
		}
		edits = append(edits, edit{s, lr.loadDecl.end, ""})
	} else {
		edits = append(edits, edit{lr.stmt[0], lr.stmt[1], ""})
	}
	for _, fl := range f.shape.Fields {
		if fl.Name == lr.field {
			ls := lineOffset(src, fl.Line-1)
			le := ls + bytes.IndexByte(src[ls:], '\n') + 1
			if strings.Count(string(src[ls:le]), ",") == 0 {
				edits = append(edits, edit{ls, le, ""})
			}
		}
	}
	after := applySpans(src, edits)
	root, modPath, _ := load.ModuleRoot(filepath.Dir(path))
	drop := unusedImports(after, after[readImports(after).end:], pathPackageNames(root, modPath))
	edits = append(edits, importEdits(src, drop, nil)...)
	return change{{path: path, old: src, edits: edits}}, nil
}

// fieldUses are where the file reads the shape's field name, bare or
// through the receiver.
func fieldUses(tp *typedPkg, path string, src []byte, f *shapeFile, name string) ([][2]int, error) {
	tf := tp.files[path]
	if tf == nil || !bytes.Equal(tf.from, src) {
		return nil, errors.New(filepath.Base(path) + " doesn't transpile as it is")
	}
	shape := tp.shapeType(f.shape.Type)
	if shape == nil {
		return nil, errors.New("can't type-check " + f.shape.Type)
	}
	st, _ := shape.Underlying().(*types.Struct)
	var field types.Object
	for i := 0; st != nil && i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			field = st.Field(i)
		}
	}
	if field == nil {
		return nil, errors.New(f.shape.Type + " has no field " + name)
	}
	var out [][2]int
	for _, id := range tp.idents(tf, 0, len(src)) {
		if id.obj == field && tp.info.Defs[id.id] == nil {
			out = append(out, [2]int{id.off, id.off + len(name)})
		}
	}
	return out, nil
}

// activeLink is a link of a layout's markup marked current by the
// request's path: an if/else whose branches are the same <a> but for its
// class, on a condition comparing the path with the link's href, or a
// lone <a> with a path for href.
type activeLink struct {
	span     [2]int // what the ui.Link replaces: the if/else, or the element
	active   string // prefix or exact
	ifElse   bool
	link     string // the ui.Link's text
	pathRead string // the bare field the condition reads, "" for none
}

var (
	ifHead    = regexp.MustCompile(`^([ \t]*)if\s+(.+?)\s*\{[ \t]*$`)
	elseLine  = regexp.MustCompile(`^[ \t]*\}\s*else\s*\{[ \t]*$`)
	closeLine = regexp.MustCompile(`^[ \t]*\}[ \t]*$`)
	prefixIf  = regexp.MustCompile(`^strings\.HasPrefix\(\s*([A-Za-z_][\w.]*)\s*,\s*"([^"\\]*)"\s*\)$`)
	exactIf   = regexp.MustCompile(`^([A-Za-z_][\w.]*)\s*==\s*"([^"\\]*)"$|^"([^"\\]*)"\s*==\s*([A-Za-z_][\w.]*)$`)
)

// activeLinks are the layout's links a ui.Link marks in the browser.
func activeLinks(f *shapeFile) []activeLink {
	src := f.src
	type line struct{ start, end int }
	var lines []line
	for off := f.markup; off < len(src); {
		end := bytes.IndexByte(src[off:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += off
		}
		lines = append(lines, line{off, end})
		off = end + 1
	}
	text := func(l line) string { return string(src[l.start:l.end]) }
	var out []activeLink
	inIf := map[int]bool{}
	for i := 0; i < len(lines); i++ {
		m := ifHead.FindStringSubmatch(text(lines[i]))
		if m == nil {
			continue
		}
		indent, cond := m[1], m[2]
		active, x, href := "", "", ""
		if g := prefixIf.FindStringSubmatch(cond); g != nil {
			active, x, href = "prefix", g[1], g[2]
		} else if g := exactIf.FindStringSubmatch(cond); g != nil {
			active, x, href = "exact", g[1]+g[4], g[2]+g[3]
		} else {
			continue
		}
		// if … { <a …>…</a> } else { <a …>…</a> }, one element a branch.
		j := i + 1
		for j < len(lines) && !elseLine.MatchString(text(lines[j])) {
			j++
		}
		k := j + 1
		for k < len(lines) && !closeLine.MatchString(text(lines[k])) {
			k++
		}
		if k >= len(lines) {
			continue
		}
		on, ok1 := oneLink(src, lines[i].end+1, lines[j].start)
		off, ok2 := oneLink(src, lines[j].end+1, lines[k].start)
		if !ok1 || !ok2 || on.href != href || off.href != href || on.kids != off.kids || on.rest != off.rest {
			continue
		}
		var extra []string
		base := strings.Fields(off.class)
		for _, c := range strings.Fields(on.class) {
			if !slicesContains(base, c) {
				extra = append(extra, c)
			}
		}
		attrs := " href=" + strconv.Quote(href) + " active=\"" + active + "\""
		if len(base) > 0 {
			attrs += " class=" + strconv.Quote(strings.Join(base, " "))
		}
		if len(extra) > 0 {
			attrs += " activeClass=" + strconv.Quote(strings.Join(extra, " "))
		}
		l := activeLink{span: [2]int{lines[i].start, min(lines[k].end+1, len(src))}, active: active, ifElse: true,
			link: indent + "<" + uiLocalName(f) + ".Link" + attrs + on.rest + ">" + on.kids + "</" + uiLocalName(f) + ".Link>\n"}
		if token.IsIdentifier(x) {
			l.pathRead = x
		}
		for n := i; n <= k; n++ {
			inIf[n] = true
		}
		out = append(out, l)
		i = k
	}
	// A lone <a href="/path">: marked while the address is below it.
	for n, l := range lines {
		if inIf[n] {
			continue
		}
		for at := l.start; at < l.end; at++ {
			if src[at] != '<' {
				continue
			}
			a, ok := oneLinkAt(src, at)
			if !ok || !strings.HasPrefix(a.href, "/") {
				continue
			}
			attrs := " href=" + strconv.Quote(a.href) + " active=\"prefix\""
			if a.class != "" {
				attrs += " class=" + strconv.Quote(a.class)
			}
			ui := uiLocalName(f)
			out = append(out, activeLink{span: [2]int{at, a.end}, active: "prefix",
				link: "<" + ui + ".Link" + attrs + a.rest + ">" + a.kids + "</" + ui + ".Link>"})
		}
	}
	return out
}

// anchor is an <a> element with a static href.
type anchor struct {
	href, class string
	rest        string // its other attributes, as written, each after a space
	kids        string
	end         int
}

// oneLink is the one <a> element of src[from:to], space around it.
func oneLink(src []byte, from, to int) (anchor, bool) {
	for from < to && isSpace(src[from]) {
		from++
	}
	a, ok := oneLinkAt(src, from)
	if !ok || strings.TrimSpace(string(src[a.end:to])) != "" {
		return anchor{}, false
	}
	return a, true
}

// oneLinkAt is the <a> element at at.
func oneLinkAt(src []byte, at int) (anchor, bool) {
	t, ok := markupTag(src, at)
	if !ok || t.name != "a" || t.self || !t.done {
		return anchor{}, false
	}
	var a anchor
	for _, at := range t.attrs {
		val := ""
		if at.valStart > 0 {
			val = string(src[at.valStart:at.valEnd])
		}
		switch at.name {
		case "href", "class":
			s, err := strconv.Unquote(val)
			if err != nil || at.expr {
				return anchor{}, false
			}
			if at.name == "href" {
				a.href = s
			} else {
				a.class = s
			}
		default:
			end := at.end
			if at.valStart > 0 {
				end = at.valEnd
			}
			a.rest += " " + string(src[at.start:end])
		}
	}
	close := bytes.Index(src[t.end:], []byte("</a>"))
	if a.href == "" || close < 0 || bytes.Contains(src[t.end:t.end+close], []byte("<a")) {
		return anchor{}, false
	}
	a.kids = string(src[t.end : t.end+close])
	a.end = t.end + close + len("</a>")
	return a, true
}

// activeLinkFix replaces the link at at with a ui.Link marked in the
// browser; a field the condition read that nothing reads any more goes,
// with the statement of Load setting it.
func activeLinkFix(path string, src []byte, at int, read load.ReadFunc) (change, error) {
	f := readShape(path, src)
	if f == nil {
		return nil, errors.New("not a shape file")
	}
	for _, l := range activeLinks(f) {
		if l.span[0] != at {
			continue
		}
		edits := []edit{{l.span[0], l.span[1], l.link}}
		if l.pathRead == "" {
			return change{{path: path, old: src, edits: edits}}, nil
		}
		for _, lr := range loadReads(f) {
			if lr.field != l.pathRead || !lr.reqRead {
				continue
			}
			tp, err := loadTyped(path, read)
			if err != nil {
				break
			}
			uses, err := fieldUses(tp, path, src, f, lr.field)
			if err != nil {
				break
			}
			others := false
			for _, u := range uses {
				in := func(s [2]int) bool { return u[0] >= s[0] && u[0] < s[1] }
				others = others || !in(l.span) && !in(lr.stmt)
			}
			if !others {
				return dropRead(path, src, f, lr, edits, read)
			}
		}
		return change{{path: path, old: src, edits: edits}}, nil
	}
	return nil, errors.New("no link to mark there")
}

// routesFix is W0701's fix on a pages/ tree's route.go: the owning main's
// file written again with the tree's Routes() passed to its web.New.
func routesFix(r actionRequest) *codeAction {
	d := r.diag("W0701")
	if d == nil || filepath.Base(r.path) != routeGoName {
		return nil
	}
	root, modPath, err := load.ModuleRoot(filepath.Dir(r.path))
	if err != nil {
		return nil
	}
	e := pagesRoutesEdit(root, modPath, filepath.Dir(r.path))
	a := codeAction{Title: "Serve " + filepath.Base(filepath.Dir(r.path)) + "/ from the app: pass its Routes() to web.New", Kind: "quickfix",
		Diagnostics: []map[string]any{d}}
	switch {
	case e.Src != nil && e.File != "":
		path := filepath.Join(root, filepath.FromSlash(e.File))
		old, err := r.read(path)
		if err != nil {
			return nil
		}
		a.Title = "Pass " + filepath.Base(filepath.Dir(r.path)) + ".Routes() to web.New in " + e.File
		a.IsPreferred = true
		a.Edit = change{{path: path, old: old, edits: []edit{{0, len(old), string(e.Src)}}}}.workspaceEdit(r.uri)
	case e.Hint != "":
		a.Disabled = map[string]any{"reason": e.Hint}
	default:
		return nil
	}
	return &a
}
