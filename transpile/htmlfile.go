package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
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
	out := make([]string, len(lines))
	copy(out, lines)
	h := &htmlFile{}
	body := func(from, to int) {
		last := -1
		for i := from; i < to; i++ {
			if strings.TrimSpace(lines[i]) != "" {
				last = i
			}
		}
		if last < 0 {
			out[from-1] = strings.TrimRight(out[from-1], "\r\n") + "<></> }\n"
			return
		}
		nl := ""
		if strings.HasSuffix(out[last], "\n") {
			nl = "\n"
		}
		out[last] = strings.TrimRight(out[last], "\r\n") + "</> }" + nl
	}
	header := func(i int) {
		l := strings.TrimRight(lines[i], " \t\r\n")
		res := " " + ui + "Node"
		if strings.HasSuffix(l, "ui.Node") || strings.HasSuffix(l, " Node") {
			res = ""
		}
		out[i] = l + res + " { return <>\n"
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
	if first >= 0 {
		typ := componentName(name)
		ptr, ok := receiverKind(typ, others)
		if !ok {
			return nil, nil, &Error{Code: "E0140", Msg: "the markup before the first func is the Render of " + typ + ", and the package declares no type " + typ,
				Help: "declare type " + typ + " in a .go or .vuka file of the package, or start the file with func …"}
		}
		recv := receiverName(typ, string(src[off[first]:off[firstEnd]]))
		star := ""
		if ptr {
			star = "*"
		}
		*h = htmlFile{recv: recv, recvType: typ}
		out[first] = "func (" + recv + " " + star + typ + ") Render() " + ui + "Node { return <>" + lines[first]
		body(first+1, firstEnd)
		if strings.HasSuffix(out[first], "\n") && first+1 >= firstEnd || strings.TrimSpace(strings.Join(lines[first+1:firstEnd], "")) == "" {
			out[first] = strings.TrimRight(out[first], "\r\n") + "</> }\n"
		}
	}
	for k, hd := range heads {
		header(hd)
		end := len(lines)
		if k+1 < len(heads) {
			end = docStart(heads[k+1])
		}
		body(hd+1, end)
	}
	pkg := packageName(others)
	var imps []string
	if ui != "" {
		imps = append(imps, strings.TrimSuffix(ui, ".")+` "`+DefaultJSXTarget+`"`)
	}
	text := string(src)
	for _, imp := range packageImports(others) {
		if !own[imp.name] && imp.name != strings.TrimSuffix(ui, ".") && regexp.MustCompile(`(^|[^\w.])`+regexp.QuoteMeta(imp.name)+`\.`).MatchString(text) {
			imps = append(imps, imp.name+" "+strconv.Quote(imp.path))
		}
	}
	prefix := "package " + pkg + ";"
	for _, imp := range imps {
		prefix += " import " + imp + ";"
	}
	if len(out) == 0 {
		out = []string{""}
	}
	out[0] = prefix + " " + out[0]
	if first >= 0 {
		at := 0
		for i := range firstEnd {
			if i == first {
				h.start = at + strings.Index(out[i], strings.TrimRight(lines[i], "\r\n"))
			}
			at += len(out[i])
		}
		h.end = at
	}
	return []byte(strings.Join(out, "")), h, nil
}

func importName(line, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	path, _ := strconv.Unquote(line[strings.IndexByte(line, '"'):])
	return pathBase(path)
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

// receiverKind reports whether typ's Render takes a pointer — it embeds
// ui.Live, holds a ui.State or has a pointer method — and whether the
// package declares typ.
func receiverKind(typ string, files []File) (ptr, ok bool) {
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
			ptr = ptr || regexp.MustCompile(`(?m)^\s*(\w+\.)?Live\s*$|State\[`).MatchString(body)
		}
	}
	return ptr, ok
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

// resolveBare qualifies the main template's bare names: an identifier the
// type checker couldn't resolve that names a field or method of the
// receiver is the receiver's, s.Title.
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
	if off < f.html.start || off >= f.html.end || f.done[off] || e.info.Uses[id] != nil || e.info.Defs[id] != nil {
		return
	}
	if o, _, _ := types.LookupFieldOrMethod(recv, true, e.pkg, id.Name); o == nil {
		return
	}
	f.done[off] = true
	f.insert(off, f.html.recv+".", 0)
	e.progress = true
}
