package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Organizing a .vuka or .vuka.html file's imports, as goimports does a Go
// file's: the imports nothing in the file uses go, the packages it uses
// without an import come in when only one package can be meant, and the
// imports are sorted in groups — the standard library, other modules, the
// module's own. What the file uses is read from the code Vuka lowers it to
// (transpile.Uses), so markup, attributes and component tags count; which
// package a name means is transpile.ImportScope's, as the build's hints
// find it. vuka lsp offers it as source.organizeImports; vuka fmt -imports
// applies it.

// organized is a file's imports organized.
type organized struct {
	edit      *importEdit         // the import section rewritten; nil when it stays
	ambiguous map[string][]string // names used with several packages they can mean
}

// organizeImports organizes the imports of the file at path, whose text is
// src; read reads the package's other files. vars gives the names of the
// context processors' variables the file may read (user, request), which
// are never a package to import, asked only when a name is to be added; a
// nil func or map is not knowing them, and then only the packages of
// attributes (@orm.PK) are added. A file whose code doesn't parse is left
// as it is (the error).
func organizeImports(path string, src []byte, read load.ReadFunc, vars func() map[string]bool) (organized, error) {
	var out organized
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	scope, others := packageScope(path, read)
	uses, err := transpile.Uses(transpile.File{Name: base, Src: src}, transpile.Options{Dir: dir, ModuleRoot: scope.ModuleRoot}, others...)
	if err != nil {
		return out, err
	}

	view := transpile.HeaderView(src)
	fset := token.NewFileSet()
	af, _ := parser.ParseFile(fset, "", view, parser.ImportsOnly|parser.ParseComments)
	if af == nil || af.Name == nil || af.Name.Name == "" {
		return out, nil // no package clause: nothing to place imports after
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }

	// The file's imports, with the comments on them.
	var decls []*ast.GenDecl
	for _, d := range af.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			decls = append(decls, g)
		}
	}
	var specs []*orgSpec
	start, end := -1, -1
	if len(decls) > 0 {
		start, end = off(decls[0].Pos()), off(decls[len(decls)-1].End())
	}
	for _, g := range decls {
		for _, s := range g.Specs {
			is := s.(*ast.ImportSpec)
			p, err := strconv.Unquote(is.Path.Value)
			if err != nil {
				return out, nil
			}
			sp := &orgSpec{path: p, start: off(is.Pos()), end: off(is.End())}
			if is.Name != nil {
				sp.name = is.Name.Name
			}
			specs = append(specs, sp)
		}
	}
	var tail []string // comments after the last import
	for _, cg := range af.Comments {
		at := off(cg.Pos())
		if at < start || at >= end {
			continue
		}
		text := string(src[at:off(cg.End())])
		line := fset.Position(cg.Pos()).Line
		var next *orgSpec
		for _, sp := range specs {
			if fset.Position(token.Pos(sp.end+1)).Line == line && sp.end <= at {
				sp.comment = text
				next = nil
				break
			}
			if sp.start > at && (next == nil || sp.start < next.start) {
				next = sp
			}
		}
		switch {
		case next != nil:
			next.doc = append(next.doc, text)
		case !specHasComment(specs, text):
			tail = append(tail, text)
		}
	}

	// What stays, what comes.
	have := map[string]bool{} // the names the kept imports give, or may
	var kept []*orgSpec
	seen := map[string]bool{}
	for _, sp := range specs {
		key := sp.name + " " + sp.path
		if seen[key] {
			continue
		}
		seen[key] = true
		local := sp.name
		if local == "" {
			local = scope.PackageName(sp.path)
		}
		switch {
		case sp.name == "_" || sp.name == ".":
		case local == "":
			// Its package's name can't be told: it stays, and gives the
			// names it may go by.
			for _, g := range nameGuesses(sp.path) {
				have[g] = true
			}
		case !usedAt(uses, local):
			continue
		}
		if local != "" {
			have[local] = true
		}
		kept = append(kept, sp)
	}
	declared := packageNames(others)
	var names []string
	for n := range uses.Qualifiers {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return uses.Qualifiers[names[i]] < uses.Qualifiers[names[j]] })
	var known map[string]bool // the variables', once asked
	varsAsked := false
	for _, n := range names {
		if have[n] || declared[n] || types.Universe.Lookup(n) != nil {
			continue
		}
		if !uses.Attrs[n] {
			if !varsAsked {
				varsAsked = true
				if vars != nil {
					known = vars()
				}
			}
			if known == nil || known[n] {
				continue
			}
		}
		cands := scope.Candidates(n)
		self := ownImportPath(scope, dir)
		cands = removeString(cands, self)
		switch {
		case len(cands) == 1:
			kept = append(kept, &orgSpec{path: cands[0]})
			have[n] = true
		case len(cands) > 1:
			if out.ambiguous == nil {
				out.ambiguous = map[string][]string{}
			}
			out.ambiguous[n] = cands
		}
	}

	nl := "\n"
	if strings.Contains(string(src), "\r\n") {
		nl = "\r\n"
	}
	text := renderImports(kept, tail, scope.ModulePath, nl)
	if start < 0 {
		if text == "" {
			return out, nil
		}
		at := off(af.Name.End())
		out.edit = &importEdit{at, at, nl + nl + text}
		return out, nil
	}
	if text == "" {
		// Every import goes: so do the blank lines before them.
		for start > 0 && (src[start-1] == '\n' || src[start-1] == '\r' || src[start-1] == ' ' || src[start-1] == '\t') {
			start--
		}
	}
	if string(src[start:end]) == text {
		return out, nil
	}
	out.edit = &importEdit{start, end, text}
	return out, nil
}

// packageScope is where the names of the file at path can come from, and
// the package's other files.
func packageScope(path string, read load.ReadFunc) (transpile.ImportScope, []transpile.File) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	scope := transpile.ImportScope{}
	if root, modPath, err := load.ModuleRoot(dir); err == nil {
		scope.ModuleRoot, scope.ModulePath = root, modPath
	}
	var others []transpile.File
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == base || strings.HasPrefix(n, ".") || strings.HasSuffix(n, "_test.go") ||
			!strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		b, err := read(filepath.Join(dir, n))
		if err != nil || strings.HasSuffix(n, ".go") && isGenerated(b) {
			continue
		}
		others = append(others, transpile.File{Name: n, Src: b})
		scope.Others = append(scope.Others, b)
	}
	return scope, others
}

// usedAt reports whether uses has name: a use at offset 0 is a use too.
func usedAt(uses *transpile.ImportUses, name string) bool {
	_, ok := uses.Qualifiers[name]
	return ok
}

// nameGuesses are the names a package at path may go by: its last
// element (before a major version), without a go- prefix, a -go suffix or
// a .vN one.
func nameGuesses(path string) []string {
	last := transpile.ImportName(path)
	out := []string{last}
	g := strings.TrimPrefix(strings.TrimSuffix(last, "-go"), "go-")
	if i := strings.LastIndex(g, ".v"); i > 0 {
		g = g[:i]
	}
	g = strings.NewReplacer("-", "", ".", "").Replace(g)
	return append(out, g)
}

// orgSpec is an import of the file being organized.
type orgSpec struct {
	name, path string
	start, end int      // where it is, as written
	doc        []string // the comments above it
	comment    string   // the comment after it on its line
}

func specHasComment(specs []*orgSpec, text string) bool {
	for _, sp := range specs {
		if sp.comment == text {
			return true
		}
	}
	return false
}

// renderImports writes the imports as goimports does: a lone import as
// import "x", several as a block in groups — the standard library, other
// modules, the module's own — each sorted by path, a blank line between.
func renderImports(specs []*orgSpec, tail []string, modPath, nl string) string {
	if len(specs) == 0 && len(tail) == 0 {
		return ""
	}
	group := func(p string) int {
		switch {
		case modPath != "" && (p == modPath || strings.HasPrefix(p, modPath+"/")):
			return 2
		case transpile.IsStd(p):
			return 0
		}
		return 1
	}
	sort.SliceStable(specs, func(i, j int) bool {
		a, b := specs[i], specs[j]
		if ga, gb := group(a.path), group(b.path); ga != gb {
			return ga < gb
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return a.name < b.name
	})
	line := func(s *orgSpec) string {
		l := strconv.Quote(s.path)
		if s.name != "" {
			l = s.name + " " + l
		}
		if s.comment != "" {
			l += " " + s.comment
		}
		return l
	}
	if len(specs) == 1 && len(specs[0].doc) == 0 && len(tail) == 0 {
		return "import " + line(specs[0])
	}
	var b strings.Builder
	b.WriteString("import (" + nl)
	for i, s := range specs {
		if i > 0 && group(s.path) != group(specs[i-1].path) {
			b.WriteString(nl)
		}
		for _, d := range s.doc {
			b.WriteString("\t" + strings.ReplaceAll(d, "\n", nl) + nl)
		}
		b.WriteString("\t" + line(s) + nl)
	}
	for _, c := range tail {
		b.WriteString("\t" + c + nl)
	}
	b.WriteString(")")
	return b.String()
}

// packageNames are the names the files declare at the package level, as
// far as they parse.
func packageNames(files []transpile.File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		af, _ := parser.ParseFile(token.NewFileSet(), "", transpile.HeaderView(f.Src), parser.SkipObjectResolution)
		if af == nil {
			continue
		}
		for _, d := range af.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					out[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						out[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							out[n.Name] = true
						}
					}
				}
			}
		}
	}
	return out
}

// ownImportPath is the import path of the package in dir, "" when unknown.
func ownImportPath(scope transpile.ImportScope, dir string) string {
	if scope.ModuleRoot == "" {
		return ""
	}
	rel, err := filepath.Rel(scope.ModuleRoot, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if rel == "." {
		return scope.ModulePath
	}
	return scope.ModulePath + "/" + filepath.ToSlash(rel)
}

func removeString(list []string, s string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// isGenerated reports whether a Go file says it is generated.
func isGenerated(src []byte) bool {
	head := string(src[:min(len(src), 512)])
	return strings.Contains(head, "// Code generated ") && strings.Contains(head, "DO NOT EDIT")
}
