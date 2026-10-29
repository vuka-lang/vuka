package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// splitImportEdits separates the edits on a generated file that touch its
// imports. Those can't be mapped one by one: the generated import section
// holds text the .vuka file doesn't (the runtime's import), and gopls rewrites
// it when it adds an import. So they are applied to the generated Go, and the
// imports that appear come back as edits adding them to the .vuka file's own
// imports. The other edits are returned for mapping as usual.
func splitImportEdits(f *vfile, edits []any) (rest, src []any) {
	end := importsEnd(f.gen)
	type ed struct {
		start, end int
		text       string
	}
	var imps []ed
	for _, e := range edits {
		m, ok := e.(map[string]any)
		r, okr := toRange(m["range"])
		text, okt := m["newText"].(string)
		if !ok || !okr || !okt {
			rest = append(rest, e)
			continue
		}
		s, en := offsetOf(f.gen, r.Start), offsetOf(f.gen, r.End)
		if s > end {
			rest = append(rest, e)
			continue
		}
		imps = append(imps, ed{s, en, text})
	}
	if len(imps) == 0 {
		return edits, nil
	}
	sort.Slice(imps, func(i, j int) bool { return imps[i].start > imps[j].start })
	gen := string(f.gen)
	for _, e := range imps {
		if e.start <= e.end && e.end <= len(gen) {
			gen = gen[:e.start] + e.text + gen[e.end:]
		}
	}
	before := importSet([]byte(f.gen))
	var added []importSpec
	for _, s := range importList([]byte(gen)) {
		if !before[s] && !importSet(f.text())[s] {
			added = append(added, s)
		}
	}
	if len(added) == 0 {
		return rest, nil
	}
	return rest, importEditsJSON(f.text(), sourceImportEdits(f, added))
}

// importEditsJSON turns import edits (src offsets) into LSP text edits.
func importEditsJSON(src []byte, eds []importEdit) []any {
	out := make([]any, 0, len(eds))
	for _, e := range eds {
		out = append(out, map[string]any{
			"range":   lspRange{positionOf(src, e.start), positionOf(src, e.end)},
			"newText": e.text,
		})
	}
	return out
}

type importSpec struct{ name, path string }

func (s importSpec) line() string {
	if s.name != "" {
		return s.name + " " + strconv.Quote(s.path)
	}
	return strconv.Quote(s.path)
}

// parseImports reads a file's imports; it tolerates errors after them, so it
// works on Vuka source.
func parseImports(src []byte) (*token.FileSet, *ast.File) {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	return fset, f
}

func importList(src []byte) []importSpec {
	_, f := parseImports(src)
	if f == nil {
		return nil
	}
	var out []importSpec
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		s := importSpec{path: path}
		if imp.Name != nil {
			s.name = imp.Name.Name
		}
		out = append(out, s)
	}
	return out
}

func importSet(src []byte) map[importSpec]bool {
	set := map[importSpec]bool{}
	for _, s := range importList(src) {
		set[s] = true
	}
	return set
}

// importsEnd is the offset where a file's imports end (its package clause's
// end when it has none).
func importsEnd(src []byte) int {
	fset, f := parseImports(src)
	if f == nil {
		return 0
	}
	end := f.Name.End()
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			end = g.End()
		}
	}
	return fset.Position(end).Offset
}

// importEdit replaces src[start:end] with text (start == end inserts).
type importEdit struct {
	start, end int
	text       string
}

// addImports returns the edits that add specs to src's imports, those it
// imports already left out. When an import ( … ) block already exists,
// each spec goes into it in its group's sorted place (importGroup).
// Otherwise the specs are consolidated with any existing lone imports into a
// single sorted block (so repeated auto-imports never pile up as separate
// `import "x"` lines); with no imports at all, a lone import (one spec) or a
// block (several) is written after the package clause.
func addImports(src []byte, specs []importSpec) []importEdit {
	return addImportsIn(src, specs, "")
}

// addImportsIn is addImports in the module modPath, whose packages make
// the last group of a block.
func addImportsIn(src []byte, specs []importSpec, modPath string) []importEdit {
	fset, f := parseImports(src)
	if f == nil {
		return nil
	}
	have := map[string]bool{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		have[p] = true
	}
	var add []importSpec
	for _, s := range specs {
		if !have[s.path] {
			have[s.path] = true
			add = append(add, s)
		}
	}
	if len(add) == 0 {
		return nil
	}
	specs = add
	sort.Slice(specs, func(i, j int) bool { return specs[i].path < specs[j].path })
	var blocks []*ast.GenDecl
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			blocks = append(blocks, g)
		}
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }

	// An existing import ( … ) block: each spec into its group.
	for _, g := range blocks {
		if !g.Lparen.IsValid() {
			continue
		}
		start, end := off(g.Pos()), off(g.End())
		cur := src
		for _, s := range specs {
			cur = insertGrouped(cur, s, modPath)
		}
		return []importEdit{{start, end, string(cur[start : end+len(cur)-len(src)])}}
	}

	// No paren block but lone imports exist: fold them and the new specs into
	// one sorted block, replacing every lone import decl.
	if len(blocks) > 0 {
		all := append([]importSpec(nil), specs...)
		for _, g := range blocks {
			for _, s := range g.Specs {
				is := s.(*ast.ImportSpec)
				path, _ := strconv.Unquote(is.Path.Value)
				spec := importSpec{path: path}
				if is.Name != nil {
					spec.name = is.Name.Name
				}
				all = append(all, spec)
			}
		}
		return []importEdit{{off(blocks[0].Pos()), off(blocks[len(blocks)-1].End()), groupedBlock(all, modPath)}}
	}

	// No imports at all: a lone import (one) or a block (several), after the
	// package clause.
	at := off(f.Name.End())
	if at <= 0 { // the parser lost the position (a decorator confused it)
		if m := packageClause.FindIndex(src); m != nil {
			at = m[1]
		} else {
			return nil
		}
	}
	var text string
	if len(specs) == 1 {
		text = "\n\nimport " + specs[0].line()
	} else {
		text = "\n\n" + groupedBlock(specs, modPath)
	}
	return []importEdit{{at, at, text}}
}

// groupedBlock is specs as a block: grouped as organizing imports writes
// them when the module is known, else in one sorted group.
func groupedBlock(specs []importSpec, modPath string) string {
	if modPath == "" {
		return importBlock(specs)
	}
	var org []*orgSpec
	seen := map[importSpec]bool{}
	for _, s := range specs {
		if !seen[s] {
			seen[s] = true
			org = append(org, &orgSpec{name: s.name, path: s.path})
		}
	}
	if len(org) == 1 {
		return "import (\n\t" + org[0].line() + "\n)"
	}
	return renderImports(org, nil, modPath, "\n")
}

func (s *orgSpec) line() string { return importSpec{s.name, s.path}.line() }

// importGroup is the group of a block an import path is in, as organizing
// imports sorts them: the standard library, other modules, the module's own.
func importGroup(path, modPath string) int {
	switch {
	case modPath != "" && (path == modPath || strings.HasPrefix(path, modPath+"/")):
		return 2
	case transpile.IsStd(path):
		return 0
	}
	return 1
}

// insertGrouped is src with s in its first import ( … ) block: in sorted
// place among the imports of its group, else a group of its own between
// the groups before and after it.
func insertGrouped(src []byte, s importSpec, modPath string) []byte {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if f == nil {
		return src
	}
	var g *ast.GenDecl
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT && gd.Lparen.IsValid() {
			g = gd
			break
		}
	}
	if g == nil {
		return src
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	lineStartAt := func(o int) int { return strings.LastIndexByte(string(src[:o]), '\n') + 1 }
	lineAfter := func(o int) int {
		if i := strings.IndexByte(string(src[o:]), '\n'); i >= 0 {
			return o + i + 1
		}
		return len(src)
	}
	type at struct {
		group      int
		path       string
		start, end int
	}
	var specs []at
	for _, sp := range g.Specs {
		is := sp.(*ast.ImportSpec)
		p, _ := strconv.Unquote(is.Path.Value)
		first := is.Pos()
		if is.Doc != nil {
			first = is.Doc.Pos()
		}
		specs = append(specs, at{importGroup(p, modPath), p, lineStartAt(off(first)), lineAfter(off(is.End()))})
	}
	want := importGroup(s.path, modPath)
	line := "\t" + s.line() + "\n"
	pos, text := lineStartAt(off(g.Rparen)), line
	switch last := -1; {
	case len(specs) == 0:
	default:
		for i, sp := range specs {
			if sp.group == want {
				last = i
			}
		}
		if last >= 0 {
			pos = specs[last].end
			for i := last; i >= 0 && specs[i].group == want; i-- {
				if specs[i].path > s.path {
					pos = specs[i].start
				}
			}
			break
		}
		next := slices.IndexFunc(specs, func(sp at) bool { return sp.group > want })
		if next >= 0 {
			pos, text = specs[next].start, line+"\n"
		} else {
			pos, text = specs[len(specs)-1].end, "\n"+line
		}
	}
	return []byte(string(src[:pos]) + text + string(src[pos:]))
}

// sourceImportEdits are the edits adding specs to the imports of f's source. A
// template file takes them as it writes imports, a line each, unless it
// groups its imports already.
func sourceImportEdits(f *vfile, specs []importSpec) []importEdit {
	src := f.text()
	modPath := ""
	if _, mp, err := load.ModuleRoot(filepath.Dir(f.source)); err == nil {
		modPath = mp
	}
	if !strings.HasSuffix(f.source, ".vuka.html") {
		return addImportsIn(src, specs, modPath)
	}
	head := transpile.ParseTemplateHead(src)
	lines := strings.SplitAfter(string(src), "\n")
	for _, imp := range head.Imports {
		if !strings.HasPrefix(strings.TrimSpace(lines[imp.Line]), "import") {
			return addImportsIn(transpile.HeaderView(src), specs, modPath) // in an import ( … ) group
		}
	}
	var add []importSpec
	for _, s := range specs {
		if !slices.ContainsFunc(head.Imports, func(im transpile.TemplateImport) bool { return im.Path == s.path }) &&
			!slices.Contains(add, s) {
			add = append(add, s)
		}
	}
	if len(add) == 0 {
		return nil
	}
	specs = add
	sort.Slice(specs, func(i, j int) bool { return specs[i].path < specs[j].path })
	var b strings.Builder
	at := -1
	switch {
	case len(head.Imports) > 0:
		at = lineStart(src, uint32(head.Imports[len(head.Imports)-1].Line)) + len(strings.TrimRight(lines[head.Imports[len(head.Imports)-1].Line], "\r\n"))
	case head.Line >= 0:
		at = lineStart(src, uint32(head.Line)) + len(strings.TrimRight(lines[head.Line], "\r\n"))
		b.WriteString("\n")
	default:
		return nil
	}
	for _, s := range specs {
		b.WriteString("\nimport " + s.line())
	}
	return []importEdit{{at, at, b.String()}}
}

// importBlock renders specs (deduplicated, sorted by path) as a grouped
// import ( … ) block.
func importBlock(specs []importSpec) string {
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].path != specs[j].path {
			return specs[i].path < specs[j].path
		}
		return specs[i].name < specs[j].name
	})
	var b strings.Builder
	b.WriteString("import (\n")
	var last importSpec
	for i, s := range specs {
		if i > 0 && s == last {
			continue
		}
		b.WriteString("\t" + s.line() + "\n")
		last = s
	}
	b.WriteString(")")
	return b.String()
}

// packageClause finds the package line, for a file the parser can't place.
var packageClause = regexp.MustCompile(`(?m)^package[ \t]+\w+`)
