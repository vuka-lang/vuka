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
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
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
			name = transpile.ImportName(spec.path)
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
	typed             bool // a model's field reference, read as its typed field
}

// staticsFile is one parsed Go file of a package: a generated file's Go, or
// a .go file on disk.
type staticsFile struct {
	path string
	ast  *ast.File
	fset *token.FileSet
	src  []byte
	// generator: another generator's output than Vuka's from a source —
	// the orm's typed fields, link files, call adapters — whose names
	// (UserColumns, UserFieldsAt) are no statics.
	generator bool
}

// vukaOutput starts the Go Vuka writes for a source file.
var vukaOutput = []byte("// Code generated by vuka from ")

// declaring are the files that can declare statics: hand-written Go and
// Vuka's from its sources.
func declaring(files []*staticsFile) []*staticsFile {
	return slices.DeleteFunc(slices.Clone(files), func(f *staticsFile) bool { return f.generator })
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
				name = transpile.ImportName(spec.path)
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
		statics   bool   // collect Name_* declarations at this level
		qual      string // the package as the level above names it, of another package's type
	}
	queue := []level{{dir, spot.typeName, true, ""}}
	seen := map[string]bool{}
	taken := map[string]bool{}
	var out []staticMember
	var typed map[string]staticMember // the model's typed fields, by name
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
			if depth == 0 {
				qual := ""
				if l.dir != srcDir {
					qual = spot.qual
				}
				typed = p.typedFieldMembers(l.dir, files, l.name, qual)
			}
			if l.statics {
				own := declaring(files)
				decls := append(enumDecls(own, l.name, l.dir == srcDir, spot.written), staticDecls(own, l.name, l.dir == srcDir, spot.written)...)
				if len(decls) == 0 && l.dir != srcDir {
					decls = getterStatics(own, ts, spot.written, l.qual)
				}
				for _, m := range decls {
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
						next = append(next, level{d2, n2, true, selectorQual(field.Type)})
					}
					continue
				}
				for _, n := range field.Names {
					if taken[n.Name] || !isExported(n.Name) && l.dir != srcDir {
						continue
					}
					taken[n.Name] = true
					m := staticMember{name: n.Name, kind: 5,
						detail: substituteSelf(exprText(field.Type), spot.written),
						doc:    fieldDoc(field)}
					if t, ok := typed[n.Name]; ok {
						m.detail, m.typed = t.detail, true
					}
					out = append(out, m)
				}
			}
		}
		queue = next
	}
	for _, name := range modelMethodNames {
		if m, ok := typed[name]; ok && !taken[name] {
			out = append(out, m)
		}
	}
	return out, missing
}

// modelMethodNames are the methods of a model's typed fields its type reads
// as its own (transpile's modelMethods): Post.As("p") is PostColumns.As("p").
var modelMethodNames = []string{"As", "On"}

// typedFieldMembers is what the orm generated for the model typeName of the
// package at dir (files, its Go): its typed fields, <Model>Fields, as the
// field references Model.Field read them — columns by their typed field's
// type, relations by the path they lead to — and the methods of
// modelMethodNames. Nil when the type has none. qual qualifies the
// package's types, for a model of another package.
func (p *proxy) typedFieldMembers(dir string, files []*staticsFile, typeName, qual string) map[string]staticMember {
	path := typeName + "Fields"
	ts, file := findTypeSpec(files, path)
	if ts == nil {
		return nil
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	local := typeNames(files)
	text := func(e ast.Expr) string { return qualifyText(exprText(e), qual, local) }
	out := map[string]staticMember{}
	for _, field := range st.Fields.List {
		for _, n := range field.Names {
			if n.IsExported() {
				out[n.Name] = staticMember{name: n.Name, kind: 5, detail: text(field.Type)}
			}
		}
	}
	isModelMethod := map[string]bool{}
	for _, name := range modelMethodNames {
		isModelMethod[name] = true
	}
	methods := func(files []*staticsFile, recv string, text func(ast.Expr) string, embedded bool) {
		for _, f := range files {
			for _, d := range f.ast.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || !fd.Name.IsExported() || !recvNamed(fd.Recv.List[0].Type, recv) {
					continue
				}
				name := fd.Name.Name
				if _, ok := out[name]; ok {
					continue
				}
				switch {
				case isModelMethod[name]:
					out[name] = staticMember{name: name, kind: 2, detail: text(fd.Type), doc: docText(fd.Doc)}
				case !embedded && fd.Type.Params.NumFields() == 0 && fd.Type.Results.NumFields() == 1 && name != "String":
					// A relation: Post.Author is PostColumns.Author().
					out[name] = staticMember{name: name, kind: 5, detail: text(fd.Type.Results.List[0].Type), doc: docText(fd.Doc)}
				}
			}
		}
	}
	methods(files, path, text, false)
	// The methods it promotes: orm.RelationPath's On.
	for _, field := range st.Fields.List {
		if len(field.Names) != 0 {
			continue
		}
		d2, n2, ok := p.namedTarget(dir, file, field.Type)
		if !ok {
			continue
		}
		pkg := selectorQual(field.Type)
		files2 := p.packageFiles(d2)
		local2 := typeNames(files2)
		methods(files2, n2, func(e ast.Expr) string { return qualifyText(exprText(e), pkg, local2) }, true)
	}
	return out
}

// selectorQual is the package qualifying a type expression: orm of
// *orm.Model[T]; "" when it has none.
func selectorQual(t ast.Expr) string {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.SelectorExpr:
			return exprText(x.X)
		default:
			return ""
		}
	}
}

// typeNames are the types a package's files declare.
func typeNames(files []*staticsFile) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		for _, d := range f.ast.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
				for _, s := range g.Specs {
					out[s.(*ast.TypeSpec).Name.Name] = true
				}
			}
		}
	}
	return out
}

// qualifyText writes the type names local declares, unqualified in text,
// as qual.Name; text as it is when qual is "".
func qualifyText(text, qual string, local map[string]bool) string {
	if qual == "" {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		if !isIdentByte(text[i]) {
			b.WriteByte(text[i])
			i++
			continue
		}
		j := i
		for j < len(text) && isIdentByte(text[j]) {
			j++
		}
		word := text[i:j]
		selected := i > 1 && text[i-1] == '.' && isIdentByte(text[i-2]) // x.Name
		if local[word] && !selected {
			b.WriteString(qual + ".")
		}
		b.WriteString(word)
		i = j
	}
	return b.String()
}

// getterStatics are the statics a generic type of another package declares
// as generic functions named after them, the way transpile's lookupStatic
// finds them: of as many type parameters as the type (each any), called with
// none of their own arguments, returning one value. Post.Objects, Post
// embedding orm.Model[Post], is orm.Objects[Post]().
func getterStatics(files []*staticsFile, ts *ast.TypeSpec, written, qual string) []staticMember {
	local := typeNames(files)
	n := fieldCount(ts.TypeParams)
	if n == 0 {
		return nil
	}
	var out []staticMember
	for _, f := range files {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !fd.Name.IsExported() || fieldCount(fd.Type.TypeParams) != n || fieldCount(fd.Type.Results) != 1 {
				continue
			}
			params := fd.Type.Params.List
			if len(params) > 1 || len(params) == 1 && (fieldCount(fd.Type.Params) != 1 || !isEllipsis(params[0].Type)) {
				continue
			}
			var names []string
			anyOnly := true
			for _, tp := range fd.Type.TypeParams.List {
				if id, ok := tp.Type.(*ast.Ident); !ok || id.Name != "any" {
					anyOnly = false
				}
				for _, nm := range tp.Names {
					names = append(names, nm.Name)
				}
			}
			if !anyOnly || n != 1 {
				continue // one type parameter, the model's: written stands for it
			}
			result := qualifyText(exprText(fd.Type.Results.List[0].Type), qual, local)
			result = regexp.MustCompile(`\b`+regexp.QuoteMeta(names[0])+`\b`).ReplaceAllString(result, written)
			out = append(out, staticMember{name: fd.Name.Name, kind: 6, static: true,
				detail: "static " + fd.Name.Name + " " + result, doc: docText(fd.Doc)})
		}
	}
	return out
}

// fieldCount is how many names (or unnamed entries) a field list holds.
func fieldCount(l *ast.FieldList) int {
	if l == nil {
		return 0
	}
	return l.NumFields()
}

func isEllipsis(e ast.Expr) bool {
	_, ok := e.(*ast.Ellipsis)
	return ok
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
				name := transpile.ImportName(path)
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
	// TypeMember (typeMember unexported); Type_Member (_Type_member) from
	// runtimes before TypeMember names.
	member := func(name string) (string, bool) {
		if m, ok := strings.CutPrefix(name, typeName+"_"); ok && m != "" {
			return m, true
		}
		if m, ok := strings.CutPrefix(name, typeName); ok && upperAt(m) {
			return m, true
		}
		lower := strings.ToLower(typeName[:1]) + typeName[1:]
		if !samePkg {
			return "", false
		}
		if m, ok := strings.CutPrefix(name, "_"+typeName+"_"); ok && m != "" {
			return m, true
		}
		if m, ok := strings.CutPrefix(name, lower); ok && lower != typeName && upperAt(m) {
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
	// And the package's Go of no source: the orm's typed fields of its
	// models, the call adapters.
	for path, vf := range p.virtual {
		if vf.source == "" && filepath.Dir(path) == dir {
			gens = append(gens, struct {
				path string
				src  []byte
			}{path, vf.gen})
		}
	}
	p.mu.Unlock()

	var out []*staticsFile
	fset := token.NewFileSet()
	add := func(path string, src []byte) {
		f, _ := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if f != nil {
			out = append(out, &staticsFile{path: path, ast: f, fset: fset, src: src, generator: ast.IsGenerated(f) && !bytes.HasPrefix(src, vukaOutput)})
		}
	}
	virtual := map[string]bool{}
	for _, g := range gens {
		add(g.path, g.src)
		virtual[g.path] = true
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || virtual[filepath.Join(dir, name)] {
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
// typed is whether the type is a model whose field references are its
// typed fields.
func (p *proxy) staticCompletionItems(vf *vfile, spot *staticSpot) (items []any, typed bool) {
	members, missing := p.staticMembers(vf, spot)
	var importEdits []any
	if missing != "" {
		importEdits = importEditsJSON(vf.text(), sourceImportEdits(vf, []importSpec{{path: missing}}))
	}
	for _, m := range members {
		typed = typed || m.typed
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
	return items, typed
}

// withStatics adds spot's items to gopls's answer v. Of a model with typed
// fields, gopls's fields go first: it completes the row's (Post.Title a
// string, its members, the embedded orm.Model) where Vuka reads the typed
// field.
func withStatics(v any, items []any, typed bool) any {
	if typed {
		keep := func(it any) bool {
			m, _ := it.(map[string]any)
			label, _ := m["label"].(string)
			kind, _ := m["kind"].(float64)
			return kind != 5 && !strings.Contains(label, ".")
		}
		if list, ok := v.(map[string]any); ok {
			existing, _ := list["items"].([]any)
			list["items"] = slices.DeleteFunc(slices.Clone(existing), func(it any) bool { return !keep(it) })
		} else if existing, ok := v.([]any); ok {
			v = slices.DeleteFunc(slices.Clone(existing), func(it any) bool { return !keep(it) })
		}
	}
	return withItems(v, newItems(v, items))
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
