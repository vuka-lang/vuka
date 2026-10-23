package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Extracting a component: a selection of a shape's markup that is one or
// more whole elements becomes a shape file of its own, in the script form
// and formatted as vuka fmt formats it, and the selection a tag of it.
// What the selection reads from around it becomes the new shape's props,
// exported fields, which the tag passes:
//
//   - a field of the shape, bare or through the receiver: a prop of the
//     field's type, as the script writes it (Children, a ui.Node, is
//     passed as the tag's content);
//   - a method of the shape, called or passed as a handler (onClick={Sort}):
//     a prop of the method value's func type, the method passed as a value,
//     so the call and the handler work as before — on the shape, whose
//     state the method changes, and whose event it stays. A shape live only
//     because the selection sent its events embeds ui.Live, as the
//     compiler made it before;
//   - a variable of the markup around it (a loop's item, its index): a prop
//     of its type.
//
// The selection's names are renamed to the props'. A selection writing a
// field (count++, tab = "x") or taking its address can't move: the new
// shape would change its own copy. In a folder of
// pages the new shape goes to the private folder _parts beside the page (a
// package of its own, which the page imports); when the selection needs the
// page's package (a type, function or variable declared there), it goes in
// the page's own package instead, as _name.vuka.html, which routes nothing.
// Elsewhere it goes in the same package.

// extractActions offers extracting the selection, when it is whole
// elements of a shape's markup.
func extractActions(r actionRequest) []any {
	f := readShape(r.path, r.src)
	if f == nil || r.start == r.end || r.start < f.markup {
		return nil
	}
	if _, _, ok := wholeElements(r.src, f.markup, r.start, r.end); !ok {
		return nil
	}
	rg := lspRange{positionOf(r.src, r.start), positionOf(r.src, r.end)}
	return []any{codeAction{Title: "Extract component…", Kind: "refactor.extract",
		Command: map[string]any{"title": "Extract component…", "command": cmdExtract,
			"arguments": []any{map[string]any{"uri": pathToURI(r.uri(r.path)), "range": rg}}}}}
}

// wholeElements trims [start, end) of space and reports whether what is
// left is one or more whole elements of the markup from markup on,
// siblings with only space between.
func wholeElements(src []byte, markup, start, end int) (int, int, bool) {
	for start < end && isSpace(src[start]) {
		start++
	}
	for end > start && isSpace(src[end-1]) {
		end--
	}
	if start >= end || src[start] != '<' {
		return 0, 0, false
	}
	scan := scanMarkup(src)
	ends := map[int]int{}
	for _, e := range scan.elems {
		if e.closed {
			ends[e.open.start] = e.close.end
		}
	}
	i := start
	for i < end {
		t, ok := markupTag(src, i)
		if !ok || t.close || !t.done || t.name == "" {
			return 0, 0, false
		}
		next := t.end
		if !t.self {
			e, ok := ends[i]
			if !ok {
				return 0, 0, false
			}
			next = e
		}
		if next > end {
			return 0, 0, false
		}
		i = next
		for i < end && isSpace(src[i]) {
			i++
		}
	}
	return start, end, i == end && start >= markup
}

// prop is a prop of the extracted shape: what it stands for and where the
// selection reads it.
type prop struct {
	name     string // the prop's
	from     string // what the tag passes: the name around the selection
	typ      string
	children bool
	uses     [][2]int // where the selection reads it: a name, or recv.Name
	first    int
}

// extractComponent works out the change extracting [start, end) of the
// shape file path (text src) into a component name ("" for a default).
func extractComponent(path string, src []byte, start, end int, name string, read load.ReadFunc) (change, error) {
	if d, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(d, filepath.Base(path))
	}
	f := readShape(path, src)
	if f == nil {
		return nil, errors.New("not a shape file: no struct above the markup")
	}
	s, e, ok := wholeElements(src, f.markup, start, end)
	if !ok {
		return nil, errors.New("select one or more whole elements of the markup")
	}
	tp, err := loadTyped(path, read)
	if err != nil {
		return nil, err
	}
	tf := tp.files[path]
	if tf == nil || string(tf.from) != string(src) {
		return nil, errors.New(filepath.Base(path) + " doesn't transpile as it is")
	}
	shape := tp.shapeType(f.shape.Type)
	if shape == nil {
		return nil, errors.New("can't type-check " + f.shape.Type)
	}
	recvs := tp.renderRecvs(tf, shape)
	sels := selectorsOf(tf.ast)

	header := map[string]refImport{} // the header's imports by path
	root, modPath := tp.root, tp.modPath
	pkgName := pathPackageNames(root, modPath)
	byName := map[string]refImport{}
	for _, sp := range f.imports.specs {
		header[sp.path] = sp
		byName[sp.localName(pkgName)] = sp
	}
	imports := map[string]wantImport{} // the new file's, by path
	needSame := false
	qualify := func(p *types.Package) string {
		if p == tp.pkg {
			needSame = true
			return ""
		}
		if sp, ok := header[p.Path()]; ok {
			n := sp.localName(pkgName)
			imports[p.Path()] = wantImport{sp.name, p.Path()}
			return n
		}
		imports[p.Path()] = wantImport{"", p.Path()}
		return p.Name()
	}
	// typeText resolves the qualifiers of a type as the header writes it.
	typeText := func(t string) string {
		for n := range usedNames([]byte(t)) {
			if sp, ok := byName[n]; ok {
				imports[sp.path] = wantImport{sp.name, sp.path}
			}
		}
		for _, w := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '.' }) {
			if !strings.Contains(w, ".") && tp.pkg.Scope().Lookup(w) != nil {
				needSame = true
			}
		}
		return t
	}

	props := map[types.Object]*prop{}
	var order []*prop
	add := func(obj types.Object, from, typ string, use [2]int) {
		pr := props[obj]
		if pr == nil {
			pr = &prop{from: from, typ: typ, first: use[0]}
			props[obj] = pr
			order = append(order, pr)
		}
		pr.uses = append(pr.uses, use)
	}
	// The receiver written out, p.Remove: the selector reads as the bare
	// name.
	recvAt := map[*ast.Ident]int{}
	for _, id := range tp.idents(tf, s, e) {
		if !recvs[id.obj] {
			continue
		}
		var sel *ast.SelectorExpr
		for _, x := range sels {
			if x.X == id.id {
				sel = x
			}
		}
		if sel == nil {
			return nil, fmt.Errorf("the selection names the receiver %s, not one of its fields or methods", id.id.Name)
		}
		recvAt[sel.Sel] = id.off
	}
	fieldTypes := map[string]string{}
	for _, fl := range f.shape.Fields {
		fieldTypes[fl.Name] = fl.Type
	}
	for _, id := range tp.idents(tf, s, e) {
		obj := id.obj
		if recvs[obj] {
			continue
		}
		if sel := sels[id.id]; sel != nil {
			x, _ := sel.X.(*ast.Ident)
			if x == nil || !recvs[tp.info.Uses[x]] {
				continue // a selector of something else
			}
			use := [2]int{id.off, id.off + len(id.id.Name)}
			if at, ok := recvAt[id.id]; ok {
				if string(src[at:use[1]]) != x.Name+"."+id.id.Name {
					return nil, fmt.Errorf("can't read %s.%s as written", x.Name, id.id.Name)
				}
				use[0] = at
			}
			switch o := obj.(type) {
			case *types.Var:
				if err := readOnly(src, use[0], use[1]-use[0]); err != nil {
					return nil, err
				}
				add(obj, o.Name(), typeText(fieldTypes[o.Name()]), use)
			case *types.Func:
				sig := o.Type().(*types.Signature)
				fn := types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
				add(obj, o.Name(), types.TypeString(fn, qualify), use)
			}
			continue
		}
		switch o := obj.(type) {
		case *types.PkgName:
			qualify(o.Imported())
			if sp, ok := header[o.Imported().Path()]; ok {
				imports[sp.path] = wantImport{sp.name, sp.path}
			}
		case *types.Var:
			if o.IsField() {
				continue
			}
			if o.Parent() == tp.pkg.Scope() {
				needSame = true
				continue
			}
			decl, ok := tp.declSource(tf, o)
			switch {
			case ok && decl >= s && decl < e:
			case ok:
				add(obj, o.Name(), types.TypeString(o.Type(), qualify), [2]int{id.off, id.off + len(o.Name())})
			case o.Name() == "ctx":
			default:
				return nil, fmt.Errorf("the selection uses %s, which only its place declares", o.Name())
			}
		case *types.Const, *types.Func, *types.TypeName:
			if o.Pkg() == tp.pkg && o.Parent() == tp.pkg.Scope() {
				needSame = true
			}
		}
	}

	// Placement.
	dir := filepath.Dir(path)
	rel, _ := filepath.Rel(root, dir)
	elems := strings.Split(filepath.ToSlash(rel), "/")
	inPages, private := false, false
	for _, el := range elems {
		if inPages && load.Private(el) {
			private = true
		}
		inPages = inPages || el == load.PagesDir
	}
	targetDir, targetPkg, fileName := dir, f.imports.pkg, ""
	parts := inPages && !private && !needSame
	if parts {
		targetDir = filepath.Join(dir, "_parts")
		targetPkg = "parts"
		if p := dirPackage(targetDir, read); p != "" {
			targetPkg = p
		}
	}
	taken := func(n string) bool {
		if parts {
			return declaredIn(targetDir, n, read)
		}
		return tp.pkg.Scope().Lookup(n) != nil || declaredIn(dir, n, read)
	}
	if name == "" {
		name = "Part"
		for i := 2; taken(name); i++ {
			name = "Part" + strconv.Itoa(i)
		}
	} else if !token.IsIdentifier(name) || !token.IsExported(name) {
		name = exportName(name)
		if !token.IsIdentifier(name) {
			return nil, fmt.Errorf("%q isn't a Go name", name)
		}
	}
	if taken(name) {
		return nil, fmt.Errorf("%s is declared already", name)
	}
	fileName = snakeName(name) + ".vuka.html"
	if inPages && !private && !parts {
		fileName = "_" + fileName
	}
	newPath := filepath.Join(targetDir, fileName)
	if _, err := read(newPath); err == nil {
		return nil, errors.New(newPath + " exists")
	}

	// Props' names.
	used := map[string]bool{name: true}
	sort.SliceStable(order, func(i, j int) bool { return order[i].first < order[j].first })
	for obj, pr := range props {
		if v, ok := obj.(*types.Var); ok && v.IsField() && v.Name() == "Children" && strings.HasSuffix(pr.typ, "Node") {
			pr.children = true
		}
	}
	for _, pr := range order {
		pr.name = exportName(pr.from)
		if pr.children {
			pr.name = "Children"
		} else if len(pr.from) <= 2 {
			if n := namedOf(pr.typ); n != "" {
				pr.name = n
			}
		}
		base := pr.name
		for i := 2; used[pr.name] || pr.name == "Children" && !pr.children; i++ {
			pr.name = base + strconv.Itoa(i)
		}
		used[pr.name] = true
	}

	// The new shape's markup: the selection, its names renamed, dedented.
	var renames []edit
	for _, pr := range order {
		for _, u := range pr.uses {
			if pr.name == pr.from && u[1]-u[0] == len(pr.from) {
				continue
			}
			renames = append(renames, edit{u[0] - s, u[1] - s, pr.name})
		}
	}

	markup := dedent(string(applySpans(src[s:e], renames)), indentBefore(src, s))
	var fields []string
	for _, pr := range order {
		fields = append(fields, pr.name+" "+pr.typ)
	}
	var imps []wantImport
	for _, w := range imports {
		imps = append(imps, w)
	}
	sortImports(imps)
	text := shapeFileText(newPath, targetPkg, imps, name, fields, markup)

	// The tag in the selection's place.
	tagName := name
	if parts {
		tagName = targetPkg + "." + name
	}
	var tag strings.Builder
	tag.WriteString("<" + tagName)
	children := ""
	for _, pr := range order {
		if pr.children {
			children = "{" + pr.from + "}"
			continue
		}
		tag.WriteString(" " + pr.name + "={" + pr.from + "}")
	}
	if children != "" {
		tag.WriteString(">" + children + "</" + tagName + ">")
	} else {
		tag.WriteString(" />")
	}
	pageEdits := []edit{{s, e, tag.String()}}
	after := applySpans(src, pageEdits)
	drop := unusedImports(after, after[readImports(after).end:], pkgName)
	var addImps []wantImport
	if parts {
		relParts, _ := filepath.Rel(root, targetDir)
		addImps = append(addImps, wantImport{"", modPath + "/" + load.GoRel(filepath.ToSlash(relParts))})
	}
	// A shape live only because its markup sends an event stays live when
	// the sends move: it embeds ui.Live, as the compiler would have.
	if f.shape.Live && !f.shape.EmbedsLive {
		if sh, _ := transpile.ParseShape(after); sh == nil || !sh.Live {
			uiName := "ui"
			if sp, ok := header[uiPath]; ok {
				uiName = sp.localName(pkgName)
			} else {
				addImps = append(addImps, wantImport{"", uiPath})
			}
			e, err := embedLive(src, f.shape, uiName)
			if err != nil {
				return nil, err
			}
			pageEdits = append(pageEdits, e)
		}
	}
	pageEdits = append(pageEdits, importEdits(src, drop, addImps)...)
	return change{created(newPath, text), {path: path, old: src, edits: pageEdits}}, nil
}

// uiPath is the import path of package ui.
const uiPath = "github.com/vuka-lang/ui/v2"

// embedLive is the edit embedding ui.Live (as uiName.Live) first in the
// shape's struct.
func embedLive(src []byte, sh *transpile.Shape, uiName string) (edit, error) {
	ls := lineOffset(src, sh.TypeLine-1)
	le := ls + bytes.IndexByte(src[ls:], '\n')
	if le < ls {
		le = len(src)
	}
	line := string(src[ls:le])
	if strings.HasSuffix(strings.TrimSpace(line), "{") {
		return edit{le + 1, le + 1, "\t" + uiName + ".Live\n"}, nil
	}
	if i := strings.Index(line, "struct{}"); i >= 0 {
		return edit{ls + i, ls + i + len("struct{}"), "struct {\n\t" + uiName + ".Live\n}"}, nil
	}
	return edit{}, errors.New("can't embed ui.Live in " + sh.Type + ": write it, then extract")
}

// lineOffset is where the line (from 0) starts.
func lineOffset(src []byte, line int) int {
	off := 0
	for l := 0; l < line && off < len(src); l++ {
		i := bytes.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return off
}

// shapeFileText is a shape file at path in the script form: a
// <script lang="go"> with its package clause and imports and the struct
// name with fields, then its markup, formatted as vuka fmt formats it.
func shapeFileText(path, pkg string, imports []wantImport, name string, fields []string, markup string) string {
	var b strings.Builder
	b.WriteString("<script lang=\"go\">\npackage " + pkg + "\n\n")
	switch len(imports) {
	case 0:
	case 1:
		b.WriteString("import " + wantLine(imports[0]) + "\n\n")
	default:
		b.WriteString("import (\n")
		for i, w := range imports {
			if i > 0 && isStd(imports[i-1].path) && !isStd(w.path) {
				b.WriteString("\n")
			}
			b.WriteString("\t" + wantLine(w) + "\n")
		}
		b.WriteString(")\n\n")
	}
	decl := "type " + name + " struct {\n"
	for _, fl := range fields {
		decl += "\t" + fl + "\n"
	}
	decl += "}\n"
	if len(fields) == 0 {
		decl = "type " + name + " struct{}\n"
	}
	b.WriteString(decl)
	b.WriteString("</script>\n\n")
	b.WriteString(strings.TrimRight(markup, "\n") + "\n")
	if out, err := formatTemplateFile(path, []byte(b.String()), false); err == nil {
		return string(out)
	}
	return b.String()
}

// readOnly reports an error when the field at off, n bytes long, is
// written or has its address taken there.
func readOnly(src []byte, off, n int) error {
	name := string(src[off : off+n])
	i := off + n
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	rest := string(src[i:min(i+3, len(src))])
	if strings.HasPrefix(rest, "++") || strings.HasPrefix(rest, "--") ||
		strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, "==") ||
		len(rest) >= 2 && rest[1] == '=' && strings.ContainsRune("+-*/%&|^", rune(rest[0])) {
		return fmt.Errorf("the selection writes %s: move the write into a method, and the method's use moves with the selection", name)
	}
	j := off - 1
	for j >= 0 && (src[j] == ' ' || src[j] == '\t') {
		j--
	}
	if j >= 0 && src[j] == '&' && (j == 0 || src[j-1] != '&') {
		return fmt.Errorf("the selection takes %s's address", name)
	}
	return nil
}

// exportName is name with its first letter upper case.
func exportName(name string) string {
	if name == "" {
		return name
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// snakeName is a Go name as a file name: OrderRow is order_row.
func snakeName(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		if unicode.IsUpper(c) {
			if i > 0 && (unicode.IsLower(r[i-1]) || i+1 < len(r) && unicode.IsLower(r[i+1])) {
				b.WriteByte('_')
			}
			c = unicode.ToLower(c)
		}
		b.WriteRune(c)
	}
	return b.String()
}

// namedOf is the name of the named type a type's text names, "" for another.
func namedOf(typ string) string {
	t := strings.TrimLeft(typ, "*")
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		t = t[i+1:]
	}
	if token.IsIdentifier(t) && token.IsExported(t) {
		return t
	}
	return ""
}

// indentBefore is the space before off on its line.
func indentBefore(src []byte, off int) string {
	ls := off
	for ls > 0 && src[ls-1] != '\n' {
		ls--
	}
	return strings.TrimRight(string(src[ls:off]), "\x00")
}

// dedent takes indent off the lines of text after its first.
func dedent(text, indent string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.TrimPrefix(lines[i], indent)
	}
	return strings.Join(lines, "\n")
}

// dirPackage is the package clause of a directory's Vuka and Go files, ""
// when it has none.
func dirPackage(dir string, read load.ReadFunc) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(isVuka(n) || strings.HasSuffix(n, ".go")) || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if src, err := read(filepath.Join(dir, n)); err == nil {
			if s := readImports(src); s.pkg != "" {
				return s.pkg
			}
		}
	}
	return ""
}

// declaredIn reports whether a file of dir declares name at the top level,
// as far as its text tells.
func declaredIn(dir, name string, read load.ReadFunc) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(isVuka(n) || strings.HasSuffix(n, ".go")) {
			continue
		}
		src, err := read(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		to := len(src)
		if strings.HasSuffix(n, ".vuka.html") {
			if sf := readShape(filepath.Join(dir, n), src); sf != nil {
				if sf.shape.Type == name {
					return true
				}
				to = sf.goEnd
			}
		}
		for _, d := range headerDecls(src, readImports(src).end, to) {
			if d.recvBase == "" && d.name == name {
				return true
			}
		}
	}
	return false
}
