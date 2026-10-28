package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// A .vuka.html file is one struct and its markup: a Go header declaring the
// struct (htmlshape.go), then, from the first line starting with <, the
// markup that struct renders, its fields and methods read by their bare
// names. It compiles as the .vuka source writing the markup as the struct's
// View (a static struct, which then renders it) or Render (a live one), on
// the markup's own lines:
//
//	type Card struct{ Title string }    type Card struct{ Title string }
//	<div>{Title}</div>            →     func (c Card) View() ui.Node { return <>
//	                                    <div>{c.Title}</div></> }

// IsTemplate reports whether f is a .vuka.html template file.
func (f File) IsTemplate() bool { return strings.HasSuffix(f.Name, ".vuka.html") }

// htmlFile is what a template file's compilation needs to know about it.
type htmlFile struct {
	island     *islandPage // island-only page before its import lowers to a decorator
	recv       string      // the markup's receiver: s
	recvType   string      // and its type: Sales
	start      int         // where the markup is
	end        int
	src        []byte      // the file as written
	ins        []insertion // what its compiled text inserts, in order
	lines      lineIndex   // src's
	shape      *Shape      // the struct it declares (htmlshape.go)
	markup     int         // where its markup starts in src: what is above is its Go header
	contextual bool        // the markup's function has the render's ctx
	ownCtx     bool        // the markup declares a ctx of its own
}

var funcLine = regexp.MustCompile(`^func\s`)

// TemplateHead is the top of a template file: its package clause, then its
// imports, single or grouped.
type TemplateHead struct {
	Package string // "" when the file has no package clause
	Line    int    // the clause's line, from 0; -1 without one
	Col     int    // the column its name starts at, from 1
	Imports []TemplateImport
	Top     int // the line the markup, or a doc comment above a header, starts on
}

// TemplateImport is an import of a template file.
type TemplateImport struct {
	Name, Path string // Name is "" when the path's last element names it
	Line       int
}

func (i TemplateImport) name() string {
	if i.Name != "" {
		return i.Name
	}
	return ImportName(i.Path)
}

var (
	packageClause  = regexp.MustCompile(`^(\s*package\s+)(\w+)\s*(?://.*)?$`)
	htmlImportSpec = regexp.MustCompile(`^(?:([\w.]+)\s+)?("[^"]*"|` + "`[^`]*`" + `)\s*;?\s*(?://.*)?$`)
	importKeyword  = regexp.MustCompile(`^import(?:\s|\(|"|$)`)
)

// ParseTemplateHead reads a template file's package clause and imports;
// blank lines and // comments may come between them. A script-form file's
// are in its <script lang="go">.
func ParseTemplateHead(src []byte) TemplateHead { return parseTemplateHead(HeaderView(src)) }

func parseTemplateHead(src []byte) TemplateHead {
	lines := strings.SplitAfter(string(src), "\n")
	h := TemplateHead{Line: -1}
	last, group, i := -1, false, 0
	spec := func(text string, line int) bool {
		m := htmlImportSpec.FindStringSubmatch(text)
		if m == nil {
			return false
		}
		path, err := strconv.Unquote(m[2])
		if err != nil {
			return false
		}
		h.Imports = append(h.Imports, TemplateImport{Name: m[1], Path: path, Line: line})
		last = line
		return true
	}
scan:
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		switch {
		case group:
			if l == ")" {
				group, last = false, i
			} else if l != "" && !strings.HasPrefix(l, "//") && !spec(l, i) {
				break scan
			}
		case l == "" || strings.HasPrefix(l, "//"):
		case h.Line < 0 && len(h.Imports) == 0 && packageClause.MatchString(strings.TrimRight(lines[i], "\r\n")):
			m := packageClause.FindStringSubmatch(strings.TrimRight(lines[i], "\r\n"))
			h.Package, h.Line, h.Col, last = m[2], i, len(m[1])+1, i
		case importKeyword.MatchString(l):
			rest := strings.TrimSpace(l[len("import"):])
			switch {
			case rest == "(":
				group, last = true, i
			case strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")"):
				for _, s := range strings.Split(strings.TrimSuffix(rest[1:], ")"), ";") {
					if s = strings.TrimSpace(s); s != "" && !spec(s, i) {
						break scan
					}
				}
				last = i
			case !spec(rest, i):
				break scan
			}
		default:
			break scan
		}
	}
	for i > last+1 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "//") {
		i-- // a doc comment of what follows
	}
	h.Top = i
	return h
}

// check is the error of a head whose package clause is missing, or names
// another package than pkg, the directory's ("" when it has no other files).
func (h TemplateHead) check(pkg string) *Error {
	switch {
	case h.Package == "":
		e := &Error{Code: "E0142", Pos: token.Position{Line: 1, Column: 1}, Msg: "a template file starts with its package clause"}
		if pkg != "" {
			return e.help("add \"package %s\"", pkg)
		}
		return e.help("add \"package <name>\", its directory's package")
	case pkg == "" || h.Package == pkg || h.Package == pkg+"_test" || h.Package+"_test" == pkg:
		return nil
	}
	return (&Error{Code: "E0142", Pos: token.Position{Line: h.Line + 1, Column: h.Col}, Len: len(h.Package),
		Msg: "the template file is in package " + h.Package + ", and its directory's package is " + pkg}).help("write \"package %s\"", pkg)
}

// componentName is the type a template file is named after: sales.vuka.html
// is Sales, order_row.vuka.html OrderRow. A file with no struct above its
// markup (E0152) names it so.
func componentName(file string) string {
	base := strings.TrimSuffix(file, ".vuka.html")
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	var b strings.Builder
	up := true
	for _, r := range base {
		switch {
		case r == '_' || r == '-' || r == '.' || r == '[' || r == ']':
			up = true
		case up:
			b.WriteRune(unicode.ToUpper(r))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// template makes the template file f the .vuka source it compiles as, or
// reports why it can't. files are the package's.
func (e *engine) template(f *fileState, files []File) bool {
	q := "ui."
	if e.selfJSX {
		q = ""
	}
	src, h, _, err := htmlParts(f.name, f.src, files, q, false, e.templateNames(), e.forceCtx[f.name])
	if err != nil {
		line, col := max(err.Pos.Line, 1), max(err.Pos.Column, 1)
		err.Pos = f.lines.pos(f.path, min(f.lines[min(line, len(f.lines))-1]+col-1, len(f.src)))
		*e.errs = append(*e.errs, err)
		return false
	}
	if h.island != nil {
		im := h.island.imp
		// HeaderView keeps offsets in the original shape, including script wrappers.
		for _, tok := range scanTokens(HeaderView(f.src)) {
			if tok.tok != token.STRING || f.at(tok.off).Line != im.Line+1 {
				continue
			}
			if path, _ := strconv.Unquote(tok.lit); path == im.Path {
				if _, _, ok := e.readIslandComponent(f, path, tok.off, e.errs); !ok {
					return false
				}
				break
			}
		}
	}
	f.src, f.html, f.lines = src, h, newLineIndex(src)
	return true
}

// htmlSource is a template file as the .vuka source it compiles as. others
// are the package's other files: its package's name and imports come from
// them.
func htmlSource(name string, src []byte, others []File, ui string, vars map[string]bool) ([]byte, *htmlFile, *Error) {
	out, h, _, err := htmlParts(name, src, others, ui, false, vars, false)
	return out, h, err
}

// TemplateAsset is a template's script or stylesheet: a <script> or
// <style> after its markup, which it is cut from.
type TemplateAsset struct {
	Decl   string // the struct the markup is of; a func's name for a second template (E0153)
	Script bool   // a script, else a stylesheet
	Scoped bool   // <style scoped>
	Text   string
	Line   int // the line Text starts on
}

// TemplateAssets are the scripts and stylesheets of a template file's
// markup.
func TemplateAssets(name string, src []byte) []TemplateAsset {
	_, _, assets, _ := htmlParts(name, HeaderView(src), nil, "ui.", true, nil, false)
	return assets
}

var (
	recvDecl = regexp.MustCompile(`^func\s*\(\s*\w*\s*\*?\s*(\w+)`)
	funcDecl = regexp.MustCompile(`^func\s+(\w+)`)
)

// cutAssets cuts the scripts and stylesheets after the markup of lines[from:to],
// blanking their lines, and returns them, of decl.
func cutAssets(lines []string, from, to int, decl string) []TemplateAsset {
	var out []TemplateAsset
	for {
		last := to - 1
		for last >= from && strings.TrimSpace(lines[last]) == "" {
			last--
		}
		if last < from {
			return out
		}
		end := strings.TrimSpace(lines[last])
		kind := ""
		switch {
		case strings.HasSuffix(end, "</script>"):
			kind = "script"
		case strings.HasSuffix(end, "</style>"):
			kind = "style"
		default:
			return out
		}
		open := -1
		for j := last; j >= from; j-- {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "<"+kind) {
				open = j
				break
			}
		}
		if open < 0 {
			return out
		}
		text := strings.Join(lines[open:last+1], "")
		gt := strings.IndexByte(text, '>')
		closeAt := strings.LastIndex(text, "</"+kind+">")
		tag := text[:max(gt, 0)]
		if gt < 0 || closeAt < gt || strings.Contains(tag, "is:inline") {
			return out
		}
		body := text[gt+1 : closeAt]
		line := open + 1 + strings.Count(text[:gt+1], "\n")
		if strings.HasPrefix(body, "\n") {
			body, line = body[1:], line+1
		}
		out = append([]TemplateAsset{{Decl: decl, Script: kind == "script", Scoped: strings.Contains(tag, " scoped"), Text: body, Line: line}}, out...)
		for j := open; j <= last; j++ {
			lines[j] = blank(lines[j])
		}
		to = open
	}
}

// cssHole is a {…} in a stylesheet that is no CSS block: a Go name or call,
// a hole written where a stylesheet has none.
var cssHole = regexp.MustCompile(`\{\s*[A-Za-z_][A-Za-z0-9_.]*(?:\([^{}:;]*\))?\s*\}`)

// blank is l with its text spaces: offsets stay those of the file.
func blank(l string) string {
	t := strings.TrimRight(l, "\r\n")
	return strings.Repeat(" ", len(t)) + l[len(t):]
}

// templateDecl is the declaration a template header names.
func templateDecl(header string) string {
	if m := recvDecl.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	if m := funcDecl.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

// htmlParts is htmlSource, and the markup's scripts and stylesheets;
// assetsOnly reads only those. withCtx gives the markup the render's ctx
// though it names none: a call in it takes ctx implicitly (implicit.go).
func htmlParts(name string, src []byte, others []File, ui string, assetsOnly bool, vars map[string]bool, withCtx bool) ([]byte, *htmlFile, []TemplateAsset, *Error) {
	lines := strings.SplitAfter(string(src), "\n")
	off := make([]int, len(lines)+1)
	for i, l := range lines {
		off[i+1] = off[i] + len(l)
	}
	head := ParseTemplateHead(src)
	top := head.Top
	if !assetsOnly {
		if err := head.check(packageNameOf(others)); err != nil {
			return nil, nil, nil, err
		}
	}
	// The file declares its struct above its markup: the markup starts
	// there. A file without one is E0152 (oneTemplate); its assets are still
	// read, for the language server.
	var shape *Shape
	if isShape(lines, top) {
		sh, err := parseShape(src, lines, head)
		if err != nil {
			if assetsOnly {
				return nil, nil, nil, nil
			}
			return nil, nil, nil, err
		}
		shape, top = sh, sh.Markup
	}
	own := map[string]bool{}
	for _, imp := range head.Imports {
		own[imp.name()] = true
	}
	var heads []int
	for i := top; i < len(lines); i++ {
		if funcLine.MatchString(lines[i]) {
			heads = append(heads, i)
		}
	}
	docStart := func(h int) int {
		for h > top && strings.HasPrefix(lines[h-1], "//") {
			h--
		}
		return h
	}
	h := &htmlFile{}
	var ins []insertion
	add := func(at int, text string) { ins = append(ins, insertion{at, text}) }
	contentEnd := func(i int) int { return off[i] + len(strings.TrimRight(lines[i], " \t\r\n")) }
	contextual := false
	closeBody := "</> }"
	openBody := func(withContext bool) string {
		closeBody = "</> }"
		if !withContext {
			return " { return <>"
		}
		contextual = true
		closeBody = "</> }) }"
		return " { return " + ui + "Ctx(func(ctx context.Context) " + ui + "Node { return <>"
	}
	nodeMethod := false
	firstEnd := len(lines)
	if len(heads) > 0 {
		firstEnd = docStart(heads[0])
	}
	first := -1
	for i := top; i < firstEnd; i++ {
		if strings.TrimSpace(lines[i]) != "" && !strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			first = i
			break
		}
	}
	var assets []TemplateAsset
	typ := componentName(name)
	if shape != nil {
		typ = shape.Type
	}
	if first >= 0 {
		assets = append(assets, cutAssets(lines, first, firstEnd, typ)...)
	}
	for k, hd := range heads {
		end := len(lines)
		if k+1 < len(heads) {
			end = docStart(heads[k+1])
		}
		assets = append(assets, cutAssets(lines, hd+1, end, templateDecl(lines[hd]))...)
	}
	if assetsOnly {
		return nil, nil, assets, nil
	}
	if err := oneTemplate(name, src, lines, heads, shape, others); err != nil {
		return nil, nil, nil, err
	}
	seen := map[[2]string]bool{}
	for _, a := range assets {
		kind := [2]string{a.Decl, "a script"}
		if m := cssHole.FindStringIndex(a.Text); !a.Script && m != nil {
			hole := strings.TrimSpace(a.Text[m[0]+1 : m[1]-1])
			return nil, nil, nil, &Error{Code: "E0128", Pos: token.Position{Line: a.Line + strings.Count(a.Text[:m[0]], "\n"), Column: 1},
				Msg:  "a stylesheet has no holes: {" + hole + "} would be CSS text as it is",
				Help: "set it on an element as a CSS variable, style:--x={" + hole + "}, and read var(--x) in the stylesheet"}
		}
		if !a.Script {
			kind[1] = "a scoped stylesheet"
			if !a.Scoped {
				continue
			}
		}
		if seen[kind] {
			return nil, nil, nil, &Error{Code: "E0141", Pos: token.Position{Line: a.Line - 1, Column: 1}, Msg: "the template of " + a.Decl + " has " + kind[1] + " already",
				Help: "a template has one script and one scoped stylesheet: join them"}
		}
		seen[kind] = true
	}
	// An island page's markup is its component: the struct is the island,
	// and the compiler writes its Render.
	if isl, err := islandOnly(shape, head, lines, first, firstEnd); err != nil {
		return nil, nil, nil, err
	} else if isl != nil {
		h.island = isl
		lines[isl.imp.Line] = blank(lines[isl.imp.Line])
		for i := first; i < firstEnd; i++ {
			lines[i] = blank(lines[i])
		}
		add(off[shape.TypeLine-1], isl.decorator(ui))
		first = -1
	}
	// The struct's markup: past oneTemplate, the file has its struct and no
	// other template.
	if first >= 0 {
		ptr, live := shape.Ptr, shape.Live
		recv := receiverName(typ, string(src[off[first]:off[firstEnd]]))
		star := ""
		if ptr {
			star = "*"
		}
		*h = htmlFile{recv: recv, recvType: typ, start: off[first], end: off[firstEnd], shape: shape, markup: off[shape.Markup]}
		// The markup reads the struct as its methods do.
		if shape.Recv != "" {
			recv, h.recv = shape.Recv, shape.Recv
		}
		targs := shape.TypeArgs
		// A struct live by its hooks or events is a live component.
		if shape.Live && !shape.EmbedsLive {
			add(shape.open, ui+"Live; ")
		}
		// A stateful component's markup is its Render, a struct
		// component's its View.
		method := "View"
		if live {
			method = "Render"
		}
		markupText := []byte(strings.Join(lines[first:firstEnd], ""))
		h.ownCtx = ownCtx.Match(markupText)
		opening := openBody(withCtx && !h.ownCtx || templateReadsContext(markupText, "", vars))
		h.contextual = contextual
		if !live && shape.hasRole(BrowserState) {
			// A static struct's states, before its View renders (a live
			// one's get theirs before it mounts): the engine names the
			// call (statesCall).
			amp := "&"
			if ptr {
				amp = ""
			}
			opening = " { " + statesCall + "(" + amp + recv + "); " + strings.TrimPrefix(opening, " { ")
		}
		open := "func (" + recv + " " + star + typ + targs + ") " + method + "() " + ui + "Node" + opening
		if t := strings.TrimSpace(lines[first]); !strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "{") {
			open += "\n" // a statement starts its line
		}
		open = "\n\n" + open // after the header's last declaration
		add(off[first], open)
		// The markup's function ends after its last line.
		last := first
		for i := first + 1; i < firstEnd; i++ {
			if strings.TrimSpace(lines[i]) != "" {
				last = i
			}
		}
		end := contentEnd(last)
		for _, a := range assets {
			if a.Decl == typ && a.Script {
				// A component script reads its props from its root: the
				// first element, or, when the markup starts with a
				// component's tag or a slot, which render no element of
				// its own, a <vk-c> around the markup.
				at, tag := propsAt(lines, off, first, firstEnd)
				if !isComponent(tag) && !strings.HasPrefix(tag, "slot:") {
					add(at, scriptAttrs(ui, recv, a.Text))
					break
				}
				wrap := "<vk-c style=\"display:contents\"" + scriptAttrs(ui, recv, a.Text) + ">"
				if t := strings.TrimSpace(lines[first]); !strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "{") {
					wrap += "\n"
				}
				add(off[first], wrap)
				add(end, "</vk-c>")
				break
			}
		}
		add(end, closeBody)
		if !live {
			// A static struct's View is its markup; the struct is a ui.Node,
			// rendering its View.
			w := "w"
			if recv == "w" {
				w = "out"
			}
			add(end, "\n\nfunc ("+recv+" "+star+typ+targs+") Render(ctx context.Context, "+w+" io.Writer) error { return "+recv+".View().Render(ctx, "+w+") }")
			nodeMethod = true
		}
	}
	var imps []string
	if ui != "" && !own[strings.TrimSuffix(ui, ".")] {
		target := DefaultJSXTarget // or the package's own, imported as ui
		for _, imp := range packageImports(others) {
			if imp.name+"." == ui {
				target = imp.path
			}
		}
		imps = append(imps, strings.TrimSuffix(ui, ".")+" "+strconv.Quote(target))
	}
	roots := selectorRoots(goParts(src))
	named := map[string]bool{}
	for _, imp := range packageImports(others) {
		if !own[imp.name] && imp.name != strings.TrimSuffix(ui, ".") && roots[imp.name] {
			imps = append(imps, imp.name+" "+strconv.Quote(imp.path))
			named[imp.name] = true
		}
	}
	if nodeMethod || contextual {
		stds := []string{"context"}
		if nodeMethod {
			stds = append(stds, "io")
		}
		for _, std := range stds {
			if !own[std] && !named[std] {
				imps = append(imps, std+" "+strconv.Quote(std))
			}
		}
	}
	// The imports go after the package clause, on its line.
	prefix := ""
	for _, imp := range imps {
		prefix += "; import " + imp
	}
	ins = append([]insertion{{off[head.Line] + head.Col - 1 + len(head.Package), prefix}}, ins...)
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at < ins[j].at })
	h.ins, h.src, h.lines = ins, src, newLineIndex(src)
	if h.recv != "" {
		h.start, h.end = h.toSynth(h.start), h.toSynth(h.end)
	}
	var b strings.Builder
	last := 0
	blanked := strings.Join(lines, "")
	for _, in := range ins {
		b.WriteString(blanked[last:in.at])
		b.WriteString(in.text)
		last = in.at
	}
	b.WriteString(blanked[last:])
	return []byte(b.String()), h, assets, nil
}

// ownCtx matches markup declaring a ctx of its own.
var ownCtx = regexp.MustCompile(`(?m)^\s*(?:ctx\s*:=|var\s+ctx\b)`)

// templateReadsContext identifies an implicit ctx read in Go expressions,
// ignoring text, string literals, selectors and explicitly bound contexts:
// ctx, or a name of vars, a context processor's variable read with it.
func templateReadsContext(src []byte, header string, vars map[string]bool) bool {
	if header != "" {
		f, _ := parser.ParseFile(token.NewFileSet(), "", "package p\n"+header+" {}", 0)
		if f != nil {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					for _, fields := range []*ast.FieldList{fn.Recv, fn.Type.Params} {
						if fields == nil {
							continue
						}
						for _, field := range fields.List {
							for _, name := range field.Names {
								if name.Name == "ctx" {
									return false
								}
							}
						}
					}
				}
			}
		}
	}
	body := goParts(src)
	if ownCtx.Match(src) {
		return false
	}
	var scan scanner.Scanner
	fs := token.NewFileSet()
	scan.Init(fs.AddFile("", -1, len(body)), []byte(body), nil, 0)
	prev := token.ILLEGAL
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			return false
		}
		if tok == token.IDENT && (lit == "ctx" || vars[lit]) && prev != token.PERIOD {
			return true
		}
		prev = tok
	}
}

// insertion is text a template file compiles with, inserted at a src
// offset.
type insertion struct {
	at   int
	text string
}

// toSynth is where src offset off is in the synthesized text: after what is
// inserted before it.
func (h *htmlFile) toSynth(off int) int {
	d := 0
	for _, in := range h.ins {
		if in.at > off {
			break
		}
		d += len(in.text)
	}
	return off + d
}

// toSrc is the src offset of the synthesized text's off: an offset inside
// an insertion is the insertion's place.
func (h *htmlFile) toSrc(off int) int {
	d := 0
	for _, in := range h.ins {
		if in.at+d > off {
			break
		}
		if off < in.at+d+len(in.text) {
			return in.at
		}
		d += len(in.text)
	}
	return off - d
}

// scriptAttrs are the attributes a component script reads on its root:
// its props, and its states when it calls ctx.state (data-vk-st: data-vk-s
// is its scope, which the component loader finds the instance by).
func scriptAttrs(ui, v, script string) string {
	out := " data-vk-p={" + ui + "Props(" + v + ")}"
	if strings.Contains(script, ".state(") {
		out += " data-vk-st={" + ui + "States(" + v + ")}"
	}
	return out
}

// propsAt is where data-vk-p goes: after the first tag's name in
// lines[from:to], and that name.
func propsAt(lines []string, off []int, from, to int) (int, string) {
	for i := from; i < to; i++ {
		if loc := firstTag.FindStringIndex(lines[i]); loc != nil {
			return off[i] + loc[1], lines[i][loc[0]+1 : loc[1]]
		}
	}
	return off[from], ""
}

var firstTag = regexp.MustCompile(`<[A-Za-z][\w.:-]*`)

// goParts is the Go of a template file: its holes, and its lines of Go
// (headers, statements), not its markup's text — where "@web.Live" names
// no package.
func goParts(src []byte) string {
	var b strings.Builder
	for _, l := range strings.Split(string(src), "\n") {
		if goLine.MatchString(l) {
			b.WriteString(l + "\n")
		}
	}
	text := string(src)
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		if i+1 < len(text) && text[i+1] == '{' {
			i++
			continue
		}
		end := holeEnd(text, i+1)
		if end < 0 {
			break
		}
		b.WriteString(text[i+1:end] + "\n")
		i = end
	}
	return b.String()
}

// selectorRoots are the names Go code selects from, the time of
// time.Now: an identifier before a dot that doesn't follow one. Strings and
// comments name none.
func selectorRoots(code string) map[string]bool {
	roots := map[string]bool{}
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(code)), []byte(code), func(token.Position, string) {}, 0)
	prev, name := token.ILLEGAL, ""
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return roots
		}
		if tok == token.PERIOD && name != "" {
			roots[name] = true
		}
		name = ""
		if tok == token.IDENT && prev != token.PERIOD {
			name = lit
		}
		prev = tok
	}
}

var goLine = regexp.MustCompile(`^\s*(func\s|if\s|for\s|switch\s|case\s|default:|} else|[\w, ]+:=)`)

// holeEnd is the index of the } closing the hole whose Go starts at i, past
// nested braces and quoted strings; -1 when nothing closes it.
func holeEnd(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'', '`':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' && c != '`' {
					i++
				}
			}
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

type goImport struct{ name, path string }

// packageImports are the imports of the package's Go and .vuka files.
func packageImports(files []File) []goImport {
	var out []goImport
	seen := map[string]bool{}
	for _, f := range files {
		if f.IsTemplate() {
			continue
		}
		a, err := parser.ParseFile(token.NewFileSet(), "", f.Src, parser.ImportsOnly)
		if a == nil && err != nil {
			continue
		}
		for _, s := range a.Imports {
			path, _ := strconv.Unquote(s.Path.Value)
			name := ImportName(path)
			if s.Name != nil {
				name = s.Name.Name
			}
			if name == "_" || name == "." || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, goImport{name, path})
		}
	}
	return out
}

// packageNameOf is the package of files but the templates, "" when there are none.
func packageNameOf(files []File) string {
	for _, f := range files {
		if f.IsTemplate() {
			continue
		}
		if a, _ := parser.ParseFile(token.NewFileSet(), "", f.Src, parser.PackageClauseOnly); a != nil && a.Name != nil {
			return a.Name.Name
		}
	}
	return ""
}

// receiverName is the markup's receiver: the type's first letter,
// unless the markup names something so.
func receiverName(typ, markup string) string {
	r := strings.ToLower(typ[:1])
	for _, cand := range []string{r, strings.ToLower(typ[:min(2, len(typ))]), "this"} {
		if !regexp.MustCompile(`(^|[^\w.])` + cand + `\b`).MatchString(markup) {
			return cand
		}
	}
	return "this"
}

// resolveBare qualifies the markup's bare names: an identifier
// naming a field or method of the receiver is the receiver's, s.Title,
// unless a local declared in the template names it.
//
// A field named as a package-level type (Glyph Glyph) is the field where
// a value goes — Glyph.Path(), Glyph.Name, f(Glyph), {Glyph} — so a
// selector on it is the field's, not a method expression or field
// reference on the type. Where only a type goes the type stays: Glyph(x),
// Glyph{…}, []Glyph, new(Glyph), F[Glyph].
func (e *engine) resolveBare(f *fileState) {
	h := f.html
	if h == nil || h.recv == "" || e.pkg == nil {
		return
	}
	obj, _ := e.pkg.Scope().Lookup(h.recvType).(*types.TypeName)
	if obj == nil {
		return
	}
	recv := types.NewPointer(obj.Type())
	// value marks the identifiers in a value's place.
	value := map[*ast.Ident]bool{}
	mark := func(x ast.Expr) {
		if id, ok := x.(*ast.Ident); ok {
			value[id] = true
		}
	}
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			mark(n.X)
			ast.Inspect(n.X, walk)
			return false
		case *ast.KeyValueExpr:
			if _, field := n.Key.(*ast.Ident); !field {
				ast.Inspect(n.Key, walk)
			}
			mark(n.Value)
			ast.Inspect(n.Value, walk)
			return false
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); !ok || !e.typeArgs(id) {
				for _, a := range n.Args {
					mark(a)
				}
			}
		case *ast.BinaryExpr:
			mark(n.X)
			mark(n.Y)
		case *ast.UnaryExpr:
			mark(n.X)
		case *ast.CompositeLit:
			for _, el := range n.Elts {
				mark(el)
			}
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				mark(r)
			}
		case *ast.AssignStmt:
			for _, r := range n.Rhs {
				mark(r)
			}
		case *ast.ValueSpec:
			for _, v := range n.Values {
				mark(v)
			}
		case *ast.RangeStmt:
			mark(n.X)
		case *ast.Ident:
			e.bareIdent(f, n, recv, value[n])
			return false
		}
		return true
	}
	ast.Inspect(f.ast, walk)
}

// typeArgs reports whether id is a builtin taking a type: new, make.
func (e *engine) typeArgs(id *ast.Ident) bool {
	b, ok := e.info.Uses[id].(*types.Builtin)
	return ok && (b.Name() == "new" || b.Name() == "make")
}

func (e *engine) bareIdent(f *fileState, id *ast.Ident, recv types.Type, value bool) {
	off := f.orig(id.Pos())
	if off < f.html.start || off >= f.html.end || f.done[off] || e.info.Defs[id] != nil {
		return
	}
	// A field or method wins over a package-level name — a type only where
	// a value goes —; a local, a parameter or a loop variable over the field.
	if o := e.info.Uses[id]; o != nil {
		if _, typ := o.(*types.TypeName); typ && !value || o.Parent() != e.pkg.Scope() || off > 0 && f.src[off-1] == '<' || off > 1 && string(f.src[off-2:off]) == "</" {
			return
		}
	}
	if o, _, _ := types.LookupFieldOrMethod(recv, true, e.pkg, id.Name); o == nil && e.methods[f.html.recvType+"."+id.Name] == nil {
		return // neither its field or method, nor its overloaded method
	}
	f.done[off] = true
	// Title is s.Title, the name still mapping to itself.
	q := f.html.recv + "."
	f.fixed = append(f.fixed, edit{start: off, end: off + len(id.Name), text: q + id.Name,
		segs: []segment{{genLen: len(q), src: off}, {gen: len(q), genLen: len(id.Name), src: off, srcLen: len(id.Name), copy: true}}})
	e.progress = true
}

// remap makes m's source offsets the file's own, not its compiled text's:
// copied text inserted there maps to the insertion's place.
func (h *htmlFile) remap(m *SourceMap) {
	if m == nil {
		return
	}
	var out []segment
	for _, sg := range m.segs {
		if !sg.copy {
			from := h.toSrc(sg.src)
			sg.src, sg.srcLen = from, max(h.toSrc(sg.src+sg.srcLen)-from, 0)
			out = append(out, sg)
			continue
		}
		// A copy split where it crosses an insertion.
		for pos := 0; pos < sg.genLen; {
			at := sg.src + pos
			n := sg.genLen - pos
			d := 0
			inside := false
			for _, in := range h.ins {
				s, e := in.at+d, in.at+d+len(in.text)
				switch {
				case at >= e:
					d += len(in.text)
					continue
				case at >= s:
					inside, n = true, min(n, e-at)
				default:
					n = min(n, s-at)
				}
				break
			}
			piece := segment{gen: sg.gen + pos, genLen: n}
			if inside {
				piece.src = h.toSrc(at)
			} else {
				piece.src, piece.srcLen, piece.copy = at-d, n, true
			}
			out = append(out, piece)
			pos += n
		}
	}
	m.segs = out
}

// TemplateHeader is a func line after a template file's markup: a second
// template (E0153).
type TemplateHeader struct {
	Name   string
	Method bool // it has a receiver
	Off    int  // where its name is
	Line   int  // from 1
	Doc    bool // a // comment is right above it
}

var headerName = regexp.MustCompile(`^func(?:\s*(\([^)]*\))\s*|\s+)(\w+)`)

// TemplateHeaders are the func lines after a template file's markup.
func TemplateHeaders(src []byte) []TemplateHeader {
	src = HeaderView(src)
	var out []TemplateHeader
	off, prev := 0, ""
	top := templatesTop(src)
	for i, l := range strings.SplitAfter(string(src), "\n") {
		if m := headerName.FindStringSubmatchIndex(l); m != nil && i >= top {
			out = append(out, TemplateHeader{Name: l[m[4]:m[5]], Method: m[2] >= 0, Off: off + m[4], Line: i + 1,
				Doc: strings.HasPrefix(strings.TrimSpace(prev), "//")})
		}
		off, prev = off+len(l), l
	}
	return out
}

// BlankTemplate is the template file src with the markup holding the offset
// off written as spaces, its lines kept, its struct still declared; a second
// template (E0153) holding it is blanked whole, doc comment and header too,
// when off is in its header. nil when off is in no markup (the package
// clause, the imports, the Go header). The language server compiles a file
// with an error in its markup so, for the package to type-check.
func BlankTemplate(src []byte, off int) []byte {
	src = HeaderView(src)
	lines := strings.SplitAfter(string(src), "\n")
	top := templatesTop(src)
	type span struct{ doc, head, end int } // a template's lines: its doc comment, header, markup up to end
	spans := []span{{top, -1, len(lines)}}
	for i := top; i < len(lines); i++ {
		if funcLine.MatchString(lines[i]) {
			d := i
			for d > top && strings.HasPrefix(lines[d-1], "//") {
				d--
			}
			spans[len(spans)-1].end = d
			spans = append(spans, span{d, i, len(lines)})
		}
	}
	at, line := 0, 0
	for line < len(lines) && at+len(lines[line]) <= off {
		at += len(lines[line])
		line++
	}
	for _, s := range spans {
		if line < s.doc || line >= s.end {
			continue
		}
		from := s.head + 1 // the markup
		if line <= s.head || s.head < 0 {
			from = s.doc
		}
		out := []byte(strings.Join(lines[:from], ""))
		for _, l := range lines[from:s.end] {
			for _, c := range []byte(l) {
				if c != '\n' {
					c = ' '
				}
				out = append(out, c)
			}
		}
		return append(out, strings.Join(lines[s.end:], "")...)
	}
	return nil
}

// oneTemplate checks that a template file is one struct's markup: the
// struct declared above it (E0152) and no other template after it (E0153).
// heads are the lines of its func templates, from 0.
func oneTemplate(name string, src []byte, lines []string, heads []int, shape *Shape, others []File) *Error {
	if len(heads) > 0 {
		hd := TemplateHeaders(src)
		at, what := heads[0], "a template"
		for _, h := range hd {
			if h.Line == heads[0]+1 {
				what = h.Name
				if h.Method {
					what = "the method template " + h.Name
				}
			}
		}
		e := &Error{Code: "E0153", Pos: token.Position{Line: at + 1, Column: 1}, Len: len(strings.TrimRight(lines[at], " \t\r\n")),
			Msg: what + " is a second template in this file: a .vuka.html file renders one struct, the one above its markup"}
		if shape == nil {
			e.Msg = what + " is a template func: a .vuka.html file renders one struct, declared above its markup"
		}
		return e.help("move it to its own .vuka.html file, a struct whose exported fields are its parameters, used as a tag; or write it as a Go func returning ui.Node in a .go file")
	}
	if shape != nil {
		return nil
	}
	typ := componentName(name)
	first := 0
	for first < len(lines)-1 && (strings.TrimSpace(lines[first]) == "" || !strings.HasPrefix(strings.TrimSpace(lines[first]), "<")) {
		first++
	}
	e := &Error{Code: "E0152", Pos: token.Position{Line: first + 1, Column: 1},
		Msg: "this markup has no struct above it: a .vuka.html file declares the struct it renders in its Go header"}
	if where := typeFile(typ, others); where != "" {
		return e.help("move type %s struct {…} and its methods from %s into this file, between its imports and its markup", typ, where)
	}
	return e.help("declare the struct it renders between the imports and the markup: type %s struct {…}", typ)
}

// typeFile is the file of others declaring the struct typ, "" when none does.
func typeFile(typ string, others []File) string {
	decl := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(typ) + `\b`)
	for _, f := range others {
		if !f.IsTemplate() && decl.Match(f.Src) {
			return f.Name
		}
	}
	return ""
}
