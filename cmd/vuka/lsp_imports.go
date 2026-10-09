package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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
		if !before[s] && !importSet(f.from)[s] {
			added = append(added, s)
		}
	}
	if len(added) == 0 {
		return rest, nil
	}
	at, text := addImports(f.from, added)
	pos := positionOf(f.from, at)
	return rest, []any{map[string]any{"range": lspRange{pos, pos}, "newText": text}}
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

// addImports is an insertion adding specs to src's imports: into its last
// import block, in sorted place; after a lone import; or after the package
// clause.
func addImports(src []byte, specs []importSpec) (int, string) {
	sort.Slice(specs, func(i, j int) bool { return specs[i].path < specs[j].path })
	fset, f := parseImports(src)
	if f == nil {
		return 0, ""
	}
	var last *ast.GenDecl
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			last = g
		}
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	lineStart := func(o int) int { return strings.LastIndexByte(string(src[:o]), '\n') + 1 }
	switch {
	case last != nil && last.Lparen.IsValid():
		// Before the first spec that sorts after the new ones, else at the end.
		at := lineStart(off(last.Rparen))
		for _, s := range last.Specs {
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
		return at, b.String()
	case last != nil:
		var b strings.Builder
		for _, s := range specs {
			b.WriteString("\nimport " + s.line())
		}
		return off(last.End()), b.String()
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, s := range specs {
		b.WriteString("\nimport " + s.line())
	}
	return off(f.Name.End()), b.String()
}
