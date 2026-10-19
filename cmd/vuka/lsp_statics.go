package main

// Completion and hover for statics: Type.Objects (or pkg.Type.Objects), the
// members Vuka lowers to package-level TypeMember declarations. gopls can't
// offer them after `Type.` — in the generated Go they live beside the type,
// not on it — so the proxy finds them itself: it resolves the type's
// declaration in the generated (or real) Go of its package and walks it the
// way the transpiler's findStatic does, statics first, then the types it
// embeds, collecting field references along the way. The items carry the
// declarations' doc comments.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// staticSpot is a possible statics member use at the cursor: Type.member or
// pkg.Type.member, read from the source text alone; whether Type really is a
// type is settled later, against the package's Go.
type staticSpot struct {
	qual     string // the package written before the type, "" when none
	typeName string
	written  string   // the type as the source spells it: "entities.Letter"
	member   string   // what is typed after the dot so far (may be "")
	typed    lspRange // the member's range, replaced by a completion
}

// completionStaticSpot reads the spot at a completion offset: the cursor is
// after `Type.` or `pkg.Type.`, possibly inside a partly-typed member.
func completionStaticSpot(src []byte, off int) (staticSpot, bool) {
	start := off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	end := off
	for end < len(src) && isIdentByte(src[end]) {
		end++
	}
	s, ok := chainBefore(src, start)
	if !ok {
		return staticSpot{}, false
	}
	s.member = string(src[start:off])
	s.typed = lspRange{positionOf(src, start), positionOf(src, end)}
	return s, true
}

// hoverStaticSpot reads the spot at a hover offset: the cursor is on the
// member itself.
func hoverStaticSpot(src []byte, off int) (staticSpot, bool) {
	start, end := off, off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	for end < len(src) && isIdentByte(src[end]) {
		end++
	}
	if start == end {
		return staticSpot{}, false
	}
	s, ok := chainBefore(src, start)
	if !ok {
		return staticSpot{}, false
	}
	s.member = string(src[start:end])
	s.typed = lspRange{positionOf(src, start), positionOf(src, end)}
	return s, true
}

// chainBefore reads the `Type.` or `pkg.Type.` ending just before start.
// A longer chain (a.b.C., f().C.) is not a plain type reference and doesn't
// match.
func chainBefore(src []byte, start int) (staticSpot, bool) {
	if start == 0 || src[start-1] != '.' {
		return staticSpot{}, false
	}
	tEnd := start - 1
	tStart := tEnd
	for tStart > 0 && isIdentByte(src[tStart-1]) {
		tStart--
	}
	if tStart == tEnd || src[tStart] >= '0' && src[tStart] <= '9' {
		return staticSpot{}, false
	}
	s := staticSpot{typeName: string(src[tStart:tEnd])}
	before := tStart
	if tStart > 0 && src[tStart-1] == '.' {
		qEnd := tStart - 1
		qStart := qEnd
		for qStart > 0 && isIdentByte(src[qStart-1]) {
			qStart--
		}
		if qStart == qEnd || src[qStart] >= '0' && src[qStart] <= '9' {
			return staticSpot{}, false
		}
		s.qual = string(src[qStart:qEnd])
		before = qStart
	}
	if before > 0 {
		switch c := src[before-1]; {
		case c == '.' || c == ')' || c == ']' || isIdentByte(c):
			return staticSpot{}, false
		}
	}
	s.written = s.typeName
	if s.qual != "" {
		s.written = s.qual + "." + s.typeName
	}
	return s, true
}

// staticSpotAt is the spot for a request at off, when it could be a statics
// member: not a decorator (@pkg.Name), and the name before the dot isn't an
// imported package (then the dot selects the package's members, gopls's job).
func staticSpotAt(method string, src []byte, off int) *staticSpot {
	var s staticSpot
	var ok bool
	switch method {
	case "textDocument/completion":
		s, ok = completionStaticSpot(src, off)
	case "textDocument/hover":
		s, ok = hoverStaticSpot(src, off)
	}
	if !ok || afterAt(src, positionOf(src, off)) {
		return nil
	}
	imported := map[string]bool{}
	for _, spec := range importList(src) {
		name := spec.name
		if name == "" {
			name = spec.path[strings.LastIndex(spec.path, "/")+1:]
		}
		imported[name] = true
	}
	if s.qual == "" && imported[s.typeName] {
		return nil // pkg.member — gopls completes it
	}
	return &s
}

// staticMember is one thing reachable as Type.Member: a static (var, const,
// accessor or method) or a field reference.
type staticMember struct {
	name, detail, doc string
	kind              float64 // LSP CompletionItemKind
	static            bool
}

// staticsFile is one parsed Go file of a package: a generated file's Go, or
// a .go file on disk.
type staticsFile struct {
	path string
	ast  *ast.File
}

// staticMembers resolves spot against the Go of its type's package: nil when
// the name doesn't declare a type there. vf is the .vuka file asked in.
// missing is the import path the file doesn't have yet, when the qualifier
// names a module package the file never imported.
func (p *proxy) staticMembers(vf *vfile, spot *staticSpot) (members []staticMember, missing string) {
	srcDir := filepath.Dir(vf.source)
	dir := srcDir
	if spot.qual != "" {
		path := ""
		for _, spec := range importList(vf.text()) {
			name := spec.name
			if name == "" {
				name = spec.path[strings.LastIndex(spec.path, "/")+1:]
			}
			if name == spot.qual {
				path = spec.path
				break
			}
		}
		switch {
		case path != "":
			dir = p.importDir(path)
		default:
			// Not imported (typed fresh, or the import was dropped as
			// unused): the module package of that name declaring the type.
			dir, missing = p.modulePackage(spot.qual, spot.typeName)
		}
		if dir == "" {
			return nil, ""
		}
	}

	type level struct {
		dir, name string
		statics   bool // collect Name_* declarations at this level
	}
	queue := []level{{dir, spot.typeName, true}}
	seen := map[string]bool{}
	taken := map[string]bool{}
	var out []staticMember
	for depth := 0; len(queue) > 0 && depth < 8; depth++ {
		var next []level
		for _, l := range queue {
			key := l.dir + "\x00" + l.name
			if seen[key] {
				continue
			}
			seen[key] = true
			files := p.packageFiles(l.dir)
			ts, declFile := findTypeSpec(files, l.name)
			if ts == nil {
				if len(out) == 0 && len(next) == 0 && depth == 0 {
					return nil, "" // not a type
				}
				continue
			}
			if l.statics {
				for _, m := range append(enumDecls(files, l.name, l.dir == srcDir, spot.written), staticDecls(files, l.name, l.dir == srcDir, spot.written)...) {
					if taken[m.name] {
						continue
					}
					taken[m.name] = true
					out = append(out, m)
				}
			}
			st := ts.Type
			for i := 0; i < 4; i++ { // a defined type's struct: type Base[Self] Model[Self]
				if _, ok := st.(*ast.StructType); ok {
					break
				}
				d2, n2, ok := p.namedTarget(l.dir, declFile, st)
				if !ok {
					st = nil
					break
				}
				files2 := p.packageFiles(d2)
				ts2, df2 := findTypeSpec(files2, n2)
				if ts2 == nil {
					st = nil
					break
				}
				st, declFile, l.dir = ts2.Type, df2, d2
			}
			strct, _ := st.(*ast.StructType)
			if strct == nil {
				continue
			}
			for _, field := range strct.Fields.List {
				if len(field.Names) == 0 {
					if d2, n2, ok := p.namedTarget(l.dir, declFile, field.Type); ok {
						next = append(next, level{d2, n2, true})
					}
					continue
				}
				for _, n := range field.Names {
					if taken[n.Name] || !isExported(n.Name) && l.dir != srcDir {
						continue
					}
					taken[n.Name] = true
					out = append(out, staticMember{name: n.Name, kind: 5,
						detail: substituteSelf(exprText(field.Type), spot.written),
						doc:    fieldDoc(field)})
				}
			}
		}
		queue = next
	}
	return out, missing
}

// namedTarget resolves a field or definition type expression to the package
// directory and name of the named type it refers to.
func (p *proxy) namedTarget(dir string, file *staticsFile, t ast.Expr) (string, string, bool) {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return dir, x.Name, true
		case *ast.SelectorExpr:
			pkg, ok := x.X.(*ast.Ident)
			if !ok {
				return "", "", false
			}
			for _, imp := range file.ast.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				name := path[strings.LastIndex(path, "/")+1:]
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == pkg.Name {
					if d := p.importDir(path); d != "" {
						return d, x.Sel.Name, true
					}
					return "", "", false
				}
			}
			return "", "", false
		default:
			return "", "", false
		}
	}
}

// findTypeSpec finds `type name` among a package's files.
func findTypeSpec(files []*staticsFile, name string) (*ast.TypeSpec, *staticsFile) {
	for _, f := range files {
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, s := range g.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name {
					return ts, f
				}
			}
		}
	}
	return nil, nil
}

// staticDecls collects the package-level declarations that are typeName's
// statics: TypeMember, and typeMember inside the type's own package.
func staticDecls(files []*staticsFile, typeName string, samePkg bool, written string) []staticMember {
	upperAt := func(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }
	member := func(name string) (string, bool) {
		if m, ok := strings.CutPrefix(name, typeName); ok && upperAt(m) {
			return m, true
		}
		lower := strings.ToLower(typeName[:1]) + typeName[1:]
		if !samePkg || lower == typeName {
			return "", false
		}
		if m, ok := strings.CutPrefix(name, lower); ok && upperAt(m) {
			return strings.ToLower(m[:1]) + m[1:], true
		}
		return "", false
	}
	var out []staticMember
	for _, f := range files {
		for _, d := range f.ast.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Recv != nil {
					continue
				}
				m, ok := member(decl.Name.Name)
				if !ok {
					continue
				}
				doc := staticDocText(decl.Doc, decl.Name.Name, written+"."+m)
				if v, ok := accessorValue(decl); ok {
					out = append(out, staticMember{name: m, kind: 6, static: true, doc: doc,
						detail: "static " + m + " " + substituteSelf(v, written)})
					continue
				}
				out = append(out, staticMember{name: m, kind: 3, static: true, doc: doc,
					detail: "static " + substituteSelf(exprText(decl.Type), written)})
			case *ast.GenDecl:
				if decl.Tok != token.VAR && decl.Tok != token.CONST {
					continue
				}
				for _, s := range decl.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, n := range vs.Names {
						m, ok := member(n.Name)
						if !ok {
							continue
						}
						detail := "static " + m
						if decl.Tok == token.CONST {
							detail = "static const " + m
						}
						if vs.Type != nil {
							detail += " " + substituteSelf(exprText(vs.Type), written)
						}
						kind := 6.0
						if decl.Tok == token.CONST {
							kind = 21
						}
						doc := vs.Doc
						if doc == nil {
							doc = decl.Doc
						}
						out = append(out, staticMember{name: m, kind: kind, static: true,
							detail: detail, doc: staticDocText(doc, n.Name, written+"."+m)})
					}
				}
			}
		}
	}
	return out
}

// accessorValue reads a generic static's accessor — func TypeM[…]() *rt.Static[V]
// — and returns V's text.
func accessorValue(decl *ast.FuncDecl) (string, bool) {
	ft := decl.Type
	if ft.Params.NumFields() != 0 || ft.Results.NumFields() != 1 {
		return "", false
	}
	ptr, ok := ft.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	idx, ok := ptr.X.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Static" {
		return "", false
	}
	return exprText(idx.Index), true
}

var selfWord = regexp.MustCompile(`\bSelf\b`)

// substituteSelf writes a base type's Self parameter as the type the source
// named, the way the transpiler instantiates it.
func substituteSelf(text, written string) string {
	return selfWord.ReplaceAllString(text, written)
}

// staticDocText is a declaration's doc with its mangled Go name spelled the
// way the source writes the static.
func staticDocText(doc *ast.CommentGroup, goName, spelled string) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(doc.Text(), goName, spelled))
}

func fieldDoc(field *ast.Field) string {
	if field.Doc != nil {
		return strings.TrimSpace(field.Doc.Text())
	}
	if field.Comment != nil {
		return strings.TrimSpace(field.Comment.Text())
	}
	return ""
}

func exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	return types.ExprString(e)
}

// packageFiles parses the Go of the package at dir: the proxy's generated
// files for its .vuka sources, and the .go files on disk. Directories outside
// the module (dependencies, immutable) are cached.
func (p *proxy) packageFiles(dir string) []*staticsFile {
	p.mu.Lock()
	inModule := p.modRoot != "" && strings.HasPrefix(dir, p.modRoot+string(filepath.Separator)) || dir == p.modRoot
	if !inModule {
		if cached, ok := p.staticsPkgs[dir]; ok {
			p.mu.Unlock()
			return cached
		}
	}
	var gens []struct {
		path string
		src  []byte
	}
	for source, gen := range p.bySource {
		if filepath.Dir(source) != dir {
			continue
		}
		if vf := p.virtual[gen]; vf != nil {
			gens = append(gens, struct {
				path string
				src  []byte
			}{gen, vf.gen})
		}
	}
	p.mu.Unlock()

	var out []*staticsFile
	fset := token.NewFileSet()
	add := func(path string, src []byte) {
		f, _ := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if f != nil {
			out = append(out, &staticsFile{path: path, ast: f})
		}
	}
	for _, g := range gens {
		add(g.path, g.src)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		add(filepath.Join(dir, name), src)
	}
	if !inModule {
		p.mu.Lock()
		p.staticsPkgs[dir] = out
		p.mu.Unlock()
	}
	return out
}

// importDir is the directory of the package at an import path: inside the
// module by its path, elsewhere from `go list`, cached per session.
func (p *proxy) importDir(path string) string {
	p.mu.Lock()
	modRoot, modPath := p.modRoot, p.modPath
	if d, ok := p.importDirs[path]; ok {
		p.mu.Unlock()
		return d
	}
	p.mu.Unlock()
	if modRoot == "" {
		return ""
	}
	dir := ""
	switch {
	case path == modPath:
		dir = modRoot
	case strings.HasPrefix(path, modPath+"/"):
		dir = filepath.Join(modRoot, filepath.FromSlash(strings.TrimPrefix(path, modPath+"/")))
	default:
		cmd := exec.Command("go", "list", "-f", "{{.Dir}}", path)
		cmd.Dir = modRoot
		if out, err := cmd.Output(); err == nil {
			dir = strings.TrimSpace(string(out))
		}
	}
	p.mu.Lock()
	p.importDirs[path] = dir
	p.mu.Unlock()
	return dir
}

// modulePackage finds the module package named qual that declares typeName:
// its directory and import path. The candidates are the directories of the
// proxy's generated files whose package clause names qual.
func (p *proxy) modulePackage(qual, typeName string) (string, string) {
	p.mu.Lock()
	modRoot, modPath := p.modRoot, p.modPath
	var dirs []string
	seen := map[string]bool{}
	for source, gen := range p.bySource {
		dir := filepath.Dir(source)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		vf := p.virtual[gen]
		if vf == nil {
			continue
		}
		if m := pkgClause.FindSubmatch(vf.gen); m != nil && string(m[1]) == qual {
			dirs = append(dirs, dir)
		}
	}
	p.mu.Unlock()
	sort.Strings(dirs)
	for _, dir := range dirs {
		if ts, _ := findTypeSpec(p.packageFiles(dir), typeName); ts == nil {
			continue
		}
		path := modPath
		if rel, err := filepath.Rel(modRoot, dir); err == nil && rel != "." {
			path += "/" + filepath.ToSlash(rel)
		}
		return dir, path
	}
	return "", ""
}

// staticCompletionItems are the completion items for spot's statics and
// field references; nil when spot's name turns out not to be a type. When
// the type's package isn't imported yet, each item's edits add the import.
func (p *proxy) staticCompletionItems(vf *vfile, spot *staticSpot) []any {
	members, missing := p.staticMembers(vf, spot)
	var importEdits []any
	if missing != "" {
		importEdits = importEditsJSON(vf.text(), addImports(vf.text(), []importSpec{{path: missing}}))
	}
	var items []any
	for _, m := range members {
		item := map[string]any{
			"label":      m.name,
			"kind":       m.kind,
			"detail":     m.detail,
			"sortText":   "0" + m.name,
			"filterText": m.name,
			"textEdit":   map[string]any{"range": spot.typed, "newText": m.name},
		}
		if m.doc != "" {
			item["documentation"] = map[string]any{"kind": "markdown", "value": m.doc}
		}
		if importEdits != nil {
			item["additionalTextEdits"] = importEdits
		}
		items = append(items, item)
	}
	return items
}

// newItems drops the synthesized items gopls already offered by label, so a
// field the generated Go completes isn't listed twice.
func newItems(v any, items []any) []any {
	have := map[string]bool{}
	existing, _ := v.([]any)
	if list, ok := v.(map[string]any); ok {
		existing, _ = list["items"].([]any)
	}
	for _, it := range existing {
		if m, ok := it.(map[string]any); ok {
			if label, ok := m["label"].(string); ok {
				have[label] = true
			}
		}
	}
	var out []any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if label, ok := m["label"].(string); ok && have[label] {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// staticHoverItem is the hover for spot when its member is a static or field
// reference of the type; nil otherwise, and the usual answer stands.
func (p *proxy) staticHoverItem(vf *vfile, spot *staticSpot) map[string]any {
	members, _ := p.staticMembers(vf, spot)
	for _, m := range members {
		if m.name != spot.member || !m.static {
			// A field reference's names are copied into the generated Go,
			// so gopls's own hover already answers on them.
			continue
		}
		head := strings.Replace(m.detail, "static "+m.name, "static "+spot.written+"."+m.name, 1)
		if rest, ok := strings.CutPrefix(m.detail, "static const "+m.name); ok {
			head = "static const " + spot.written + "." + m.name + rest
		}
		if strings.HasPrefix(m.detail, "static func") {
			head = "static " + spot.written + "." + m.name + strings.TrimPrefix(m.detail, "static func")
		}
		value := "```go\n" + head + "\n```"
		if m.doc != "" {
			value += "\n\n" + m.doc
		}
		return map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": value},
			"range":    spot.typed,
		}
	}
	return nil
}
