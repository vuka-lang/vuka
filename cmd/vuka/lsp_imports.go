package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	return rest, importEditsJSON(f.text(), addImports(f.text(), added))
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

// addImports returns the edits that add specs to src's imports. When an
// import ( … ) block already exists, the specs go into it in sorted place.
// Otherwise the specs are consolidated with any existing lone imports into a
// single sorted grouped block (so repeated auto-imports never pile up as
// separate `import "x"` lines); with no imports at all, a lone import (one
// spec) or a grouped block (several) is written after the package clause.
func addImports(src []byte, specs []importSpec) []importEdit {
	sort.Slice(specs, func(i, j int) bool { return specs[i].path < specs[j].path })
	fset, f := parseImports(src)
	if f == nil {
		return nil
	}
	var blocks []*ast.GenDecl
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			blocks = append(blocks, g)
		}
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	lineStart := func(o int) int { return strings.LastIndexByte(string(src[:o]), '\n') + 1 }

	// An existing import ( … ) block: insert into it in sorted place.
	for _, g := range blocks {
		if !g.Lparen.IsValid() {
			continue
		}
		at := lineStart(off(g.Rparen))
		for _, s := range g.Specs {
			path, _ := strconv.Unquote(s.(*ast.ImportSpec).Path.Value)
			if path > specs[0].path {
				at = lineStart(off(s.Pos()))
				break
			}
		}
		var b strings.Builder
		for _, s := range specs {
			b.WriteString("\t" + s.line() + "\n")
		}
		return []importEdit{{at, at, b.String()}}
	}

	// No paren block but lone imports exist: fold them and the new specs into
	// one sorted grouped block, replacing every lone import decl.
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
		return []importEdit{{off(blocks[0].Pos()), off(blocks[len(blocks)-1].End()), importBlock(all)}}
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
		text = "\n\n" + importBlock(specs)
	}
	return []importEdit{{at, at, text}}
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
