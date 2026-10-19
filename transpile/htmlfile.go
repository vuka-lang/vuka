package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// A .vuka.html file is templates: markup, and func headers starting the
// templates after the first. What comes before the first header is the
// Render of the type the file is named after (sales.vuka.html: Sales), its
// fields and methods read by their bare names. It compiles as the .vuka
// source writing each template as a function returning its markup, on the
// template's own lines:
//
//	func Card(p CardProps)              func Card(p CardProps) ui.Node { return <>
//	<div>{p.Title}</div>          →     <div>{p.Title}</div></> }

// IsTemplate reports whether f is a .vuka.html template file.
func (f File) IsTemplate() bool { return strings.HasSuffix(f.Name, ".vuka.html") }

// htmlFile is what a template file's compilation needs to know about it.
type htmlFile struct {
	recv     string // the main template's receiver: s
	recvType string // and its type: Sales
	start    int    // where the main template's markup is
	end      int
	src      []byte      // the file as written
	ins      []insertion // what its compiled text inserts, in order
	lines    lineIndex   // src's
}

var (
	importLine = regexp.MustCompile(`^import\s+(\w+\s+)?"[^"]*"\s*$`)
	funcLine   = regexp.MustCompile(`^func\s`)
)

// componentName is the type a template file is the Render of: sales.vuka.html
// is Sales, order_row.vuka.html OrderRow.
func componentName(file string) string {
	base := strings.TrimSuffix(file, ".vuka.html")
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	var b strings.Builder
	up := true
	for _, r := range base {
		switch {
		case r == '_' || r == '-' || r == '.':
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

// htmlSource is a template file as the .vuka source it compiles as. others
// are the package's other files: its name, its imports and the main
// template's type come from them.
func htmlSource(name string, src []byte, others []File, ui string) ([]byte, *htmlFile, *Error) {
	out, h, _, err := htmlParts(name, src, others, ui, false)
	return out, h, err
}

// TemplateAsset is a template's script or stylesheet: a <script> or
// <style> after its markup, which it is cut from.
type TemplateAsset struct {
	Decl   string // the declaration the template is: a type, for its Render or other method, or a function
	Script bool   // a script, else a stylesheet
	Scoped bool   // <style scoped>
	Text   string
	Line   int // the line Text starts on
}

// TemplateAssets are the scripts and stylesheets of a template file's
// templates.
func TemplateAssets(name string, src []byte) []TemplateAsset {
	_, _, assets, _ := htmlParts(name, src, nil, "ui.", true)
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

// htmlParts is htmlSource, and the templates' scripts and stylesheets;
// assetsOnly reads only those.
func htmlParts(name string, src []byte, others []File, ui string, assetsOnly bool) ([]byte, *htmlFile, []TemplateAsset, *Error) {
	lines := strings.SplitAfter(string(src), "\n")
	off := make([]int, len(lines)+1)
	for i, l := range lines {
		off[i+1] = off[i] + len(l)
	}
	// The imports at the top, comments and blank lines among them.
	top := 0
	own := map[string]bool{}
	for ; top < len(lines); top++ {
		l := strings.TrimSpace(lines[top])
		if m := importLine.FindStringSubmatch(l); m != nil {
			own[importName(l, m[1])] = true
			continue
		}
		if l != "" && !strings.HasPrefix(l, "//") {
			break
		}
	}
	for top > 0 && strings.HasPrefix(strings.TrimSpace(lines[top-1]), "//") {
		top-- // a doc comment of what follows
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
	// body closes the template whose markup is lines[from:to], returning
	// where its function ends.
	body := func(from, to int) int {
		last := -1
		for i := from; i < to; i++ {
			if strings.TrimSpace(lines[i]) != "" {
				last = i
			}
		}
		if last < 0 {
			add(contentEnd(from-1), "<></> }")
			return contentEnd(from - 1)
		}
		add(contentEnd(last), "</> }")
		return contentEnd(last)
	}
	// A struct component's View is its markup; the component is a ui.Node,
	// rendering its View.
	nodeMethod := false
	renderMethod := func(at int, recv, typ string) {
		w := "w"
		if recv == "w" {
			w = "out"
		}
		add(at, "\n\nfunc ("+recv+" "+typ+") Render(ctx context.Context, "+w+" io.Writer) error { return "+recv+".View().Render(ctx, "+w+") }")
		nodeMethod = true
	}
	header := func(i int) {
		l := strings.TrimRight(lines[i], " \t\r\n")
		res := " " + ui + "Node"
		if strings.HasSuffix(l, "ui.Node") || strings.HasSuffix(l, " Node") {
			res = ""
		}
		add(contentEnd(i), res+" { return <>")
	}
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
	seen := map[[2]string]bool{}
	for _, a := range assets {
		kind := [2]string{a.Decl, "a script"}
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
	if first >= 0 {
		ptr, live, ok := receiverKind(typ, others)
		if !ok {
			return nil, nil, nil, &Error{Code: "E0140", Msg: "the markup before the first func is the component " + typ + "'s, and the package declares no type " + typ,
				Help: "declare type " + typ + " in a .go or .vuka file of the package, or start the file with func …"}
		}
		recv := receiverName(typ, string(src[off[first]:off[firstEnd]]))
		star := ""
		if ptr {
			star = "*"
		}
		*h = htmlFile{recv: recv, recvType: typ, start: off[first], end: off[firstEnd]}
		// A stateful component's markup is its Render, a struct
		// component's its View.
		method := "View"
		if live {
			method = "Render"
		}
		add(off[first], "func ("+recv+" "+star+typ+") "+method+"() "+ui+"Node { return <>")
		for _, a := range assets {
			if a.Decl == typ && a.Script {
				// A component script reads its props from its root.
				add(propsAt(lines, off, first, firstEnd), " data-vk-p={"+ui+"Props("+recv+")}")
				break
			}
		}
		end := contentEnd(first)
		if strings.TrimSpace(strings.Join(lines[first+1:firstEnd], "")) == "" {
			add(end, "</> }")
		} else {
			end = body(first+1, firstEnd)
		}
		if !live {
			renderMethod(end, recv, star+typ)
		}
	}
	for k, hd := range heads {
		end := len(lines)
		if k+1 < len(heads) {
			end = docStart(heads[k+1])
		}
		header(hd)
		if v := propsOf(lines[hd]); v != "" {
			for _, a := range assets {
				if a.Script && a.Line > hd && a.Line <= end {
					add(propsAt(lines, off, hd+1, end), " data-vk-p={"+ui+"Props("+v+")}")
					break
				}
			}
		}
		at := body(hd+1, end)
		if m := viewHeader.FindStringSubmatch(lines[hd]); m != nil {
			renderMethod(at, m[1], m[2]+m[3])
		}
	}
	pkg := packageName(others)
	var imps []string
	if ui != "" {
		target := DefaultJSXTarget // or the package's own, imported as ui
		for _, imp := range packageImports(others) {
			if imp.name+"." == ui {
				target = imp.path
			}
		}
		imps = append(imps, strings.TrimSuffix(ui, ".")+" "+strconv.Quote(target))
	}
	text := goParts(src)
	named := map[string]bool{}
	for _, imp := range packageImports(others) {
		if !own[imp.name] && imp.name != strings.TrimSuffix(ui, ".") && regexp.MustCompile(`(^|[^\w.])`+regexp.QuoteMeta(imp.name)+`\.`).MatchString(text) {
			imps = append(imps, imp.name+" "+strconv.Quote(imp.path))
			named[imp.name] = true
		}
	}
	if nodeMethod {
		for _, std := range []string{"context", "io"} {
			if !own[std] && !named[std] {
				imps = append(imps, std+" "+strconv.Quote(std))
			}
		}
	}
	prefix := "package " + pkg + ";"
	for _, imp := range imps {
		prefix += " import " + imp + ";"
	}
	// The package clause and imports go on a line of their own before the
	// file's.
	ins = append([]insertion{{0, prefix + "\n"}}, ins...)
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

// propsAt is where data-vk-p goes: after the first element's tag name in
// lines[from:to].
func propsAt(lines []string, off []int, from, to int) int {
	for i := from; i < to; i++ {
		if loc := firstTag.FindStringIndex(lines[i]); loc != nil {
			return off[i] + loc[1]
		}
	}
	return off[from]
}

var (
	viewHeader = regexp.MustCompile(`^func\s*\(\s*(\w+)\s+(\*?)\s*(\w+(?:\[[\w\s,]*\])?)\s*\)\s*View\s*\(\s*\)`)
	firstTag   = regexp.MustCompile(`<[A-Za-z][\w.:-]*`)
	recvName   = regexp.MustCompile(`^func\s*\(\s*(\w+)\s`)
	propParam  = regexp.MustCompile(`^func\s+\w+\s*\(\s*(\w+)\s+[\w.*]+\s*\)`)
)

// propsOf is what a template's script reads as its props: its receiver, or
// its one parameter.
func propsOf(header string) string {
	if m := recvName.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	if m := propParam.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

func importName(line, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	path, _ := strconv.Unquote(line[strings.IndexByte(line, '"'):])
	return pathBase(path)
}

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
			name := pathBase(path)
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

func packageName(files []File) string {
	for _, f := range files {
		if f.IsTemplate() {
			continue
		}
		if a, _ := parser.ParseFile(token.NewFileSet(), "", f.Src, parser.PackageClauseOnly); a != nil && a.Name != nil {
			return a.Name.Name
		}
	}
	return "main"
}

// receiverKind reports whether typ's markup method takes a pointer — it
// embeds ui.Live, holds a ui.State or has a pointer method —, whether it is
// a stateful component (it embeds ui.Live: its markup is its Render), and
// whether the package declares typ.
func receiverKind(typ string, files []File) (ptr, live, ok bool) {
	decl := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(typ) + `\s+struct\s*\{`)
	method := regexp.MustCompile(`func\s*\(\s*\w*\s*\*\s*` + regexp.QuoteMeta(typ) + `\s*\)`)
	for _, f := range files {
		if f.IsTemplate() {
			continue
		}
		src := string(f.Src)
		if method.MatchString(src) {
			ptr = true
		}
		loc := decl.FindStringIndex(src)
		if loc == nil {
			continue
		}
		ok = true
		if end := goEndBrace(src, loc[1]); end > 0 {
			body := src[loc[1]:end]
			live = live || regexp.MustCompile(`(?m)^\s*(\w+\.)?Live\s*$`).MatchString(body)
			ptr = ptr || live || strings.Contains(body, "State[")
		}
	}
	return ptr, live, ok
}

// goEndBrace is the offset of the } closing the brace before i.
func goEndBrace(s string, i int) int {
	for depth := 1; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// receiverName is the main template's receiver: the type's first letter,
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

// resolveBare qualifies the main template's bare names: an identifier
// naming a field or method of the receiver is the receiver's, s.Title,
// unless a local declared in the template names it.
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
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, walk)
			return false
		case *ast.KeyValueExpr:
			if _, field := n.Key.(*ast.Ident); !field {
				ast.Inspect(n.Key, walk)
			}
			ast.Inspect(n.Value, walk)
			return false
		case *ast.Ident:
			e.bareIdent(f, n, recv)
			return false
		}
		return true
	}
	ast.Inspect(f.ast, walk)
}

func (e *engine) bareIdent(f *fileState, id *ast.Ident, recv types.Type) {
	off := f.orig(id.Pos())
	if off < f.html.start || off >= f.html.end || f.done[off] || e.info.Defs[id] != nil {
		return
	}
	// A field or method wins over a package-level name; a local, a
	// parameter or a loop variable over the field.
	if o := e.info.Uses[id]; o != nil {
		if _, typ := o.(*types.TypeName); typ || o.Parent() != e.pkg.Scope() || off > 0 && f.src[off-1] == '<' || off > 1 && string(f.src[off-2:off]) == "</" {
			return
		}
	}
	if o, _, _ := types.LookupFieldOrMethod(recv, true, e.pkg, id.Name); o == nil {
		return
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
