package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// ctx-vars: a vuka file reads what a context processor gives through its
// variable — perms.Has("x"), user, request — not auth.Can(ctx, "x"),
// auth.Current(ctx) or web.Request(ctx) (W0402). The compiler works out
// each rewrite (transpile.Error.Fix); the fixer and the editor's quick fix
// apply it, and drop the imports of web and web/auth it leaves unused.

// ctxVarFixes are the W0402 rewrites of the warnings of one file, as
// edits on its text.
func ctxVarFixes(ws transpile.ErrorList) []edit {
	var out []edit
	for _, w := range ws {
		if w.Code == "W0402" && w.Fix != "" && w.Len > 0 {
			out = append(out, edit{w.Pos.Offset, w.Pos.Offset + w.Len, w.Fix})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	// One inside another (a read in an argument of auth.Can) waits for the
	// next run: the outer's text has it as written.
	keep := out[:0]
	for _, e := range out {
		if len(keep) == 0 || e.start >= keep[len(keep)-1].end {
			keep = append(keep, e)
		}
	}
	return keep
}

// ctxVarEdits are fixes on src and the edits removing the imports of web
// and web/auth src no longer uses once they are applied.
func ctxVarEdits(src []byte, fixes []edit) []edit {
	for _, e := range fixes {
		if e.start < 0 || e.end > len(src) || e.start > e.end {
			return nil
		}
	}
	after := applySpans(src, fixes)
	return append(slices.Clone(fixes), unusedImportEdits(src, after, transpile.WebPath, transpile.WebPath+"/auth")...)
}

// unusedImportEdits remove src's imports of paths that after, src as it
// will be, doesn't name: their lines, a blank line left between groups,
// and an import declaration left empty.
func unusedImportEdits(src, after []byte, paths ...string) []edit {
	fset, f := parseImports(transpile.HeaderView(src))
	if f == nil {
		return nil
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var out []edit
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var gone []*ast.ImportSpec
		for _, s := range gd.Specs {
			imp := s.(*ast.ImportSpec)
			path, _ := strconv.Unquote(imp.Path.Value)
			if !slices.Contains(paths, path) {
				continue
			}
			name := transpile.ImportName(path)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "_" || name == "." || regexp.MustCompile(`(^|[^\w.])`+regexp.QuoteMeta(name)+`\.\w`).Match(after) {
				continue
			}
			gone = append(gone, imp)
		}
		switch {
		case len(gone) == 0:
		case len(gone) == len(gd.Specs):
			out = append(out, lineSpan(src, off(gd.Pos()), off(gd.End()), true))
		default:
			for _, imp := range gone {
				out = append(out, lineSpan(src, off(imp.Pos()), off(imp.End()), false))
			}
		}
	}
	return out
}

// lineSpan is the edit removing the lines from..to cover, with the blank
// line before them when a blank line or a ) follows, or the one after
// when ( precedes; whole, for a declaration, with the blank line after.
func lineSpan(src []byte, from, to int, decl bool) edit {
	start := bytes.LastIndexByte(src[:from], '\n') + 1
	end := len(src)
	if i := bytes.IndexByte(src[to:], '\n'); i >= 0 {
		end = to + i + 1
	}
	blank := func(a, b int) bool { return len(bytes.TrimSpace(src[a:b])) == 0 }
	prevStart := 0
	if start > 0 {
		prevStart = bytes.LastIndexByte(src[:start-1], '\n') + 1
	}
	nextEnd := len(src)
	if i := bytes.IndexByte(src[end:], '\n'); i >= 0 {
		nextEnd = end + i + 1
	}
	next := bytes.TrimSpace(src[end:nextEnd])
	switch {
	case decl && end < len(src) && blank(end, nextEnd):
		end = nextEnd
	case !decl && start > 0 && blank(prevStart, start) && (len(next) == 0 || next[0] == ')'):
		start = prevStart
	case !decl && start > 0 && bytes.HasSuffix(bytes.TrimSpace(src[prevStart:start]), []byte("(")) && end < len(src) && blank(end, nextEnd):
		end = nextEnd
	}
	return edit{start, end, ""}
}

// fixCtxVars rewrites the direct reads of the module's vuka files.
func fixCtxVars(m *fixModule) ([]fixChange, error) {
	byFile := map[string]transpile.ErrorList{}
	var files []string
	for _, g := range m.gens {
		for _, w := range g.Warnings {
			file := w.Pos.Filename
			if !filepath.IsAbs(file) {
				file = filepath.Join(filepath.Dir(g.Source), filepath.Base(file))
			}
			if _, ok := byFile[file]; !ok {
				files = append(files, file)
			}
			byFile[file] = append(byFile[file], w)
		}
	}
	sort.Strings(files)
	var out []fixChange
	for _, file := range files {
		fixes := ctxVarFixes(byFile[file])
		if len(fixes) == 0 {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		edits := ctxVarEdits(src, fixes)
		if edits == nil {
			continue
		}
		rel, _ := filepath.Rel(m.root, file)
		what := fmt.Sprintf("%s: %d read(s) through perms, user or request", filepath.ToSlash(rel), len(fixes))
		out = append(out, fixChange{what, func() error {
			return os.WriteFile(file, applySpans(src, edits), 0o644)
		}})
	}
	return out, nil
}

// ctxVarActions are the quick fixes rewriting the direct reads the
// request's range touches (W0402), as vuka fix ctx-vars does.
func ctxVarActions(r actionRequest) []any {
	var out []any
	for _, fx := range r.ctxFixes {
		if !r.touches(fx.start, fx.end) {
			continue
		}
		edits := ctxVarEdits(r.src, []edit{fx})
		if edits == nil {
			continue
		}
		a := codeAction{Title: "Write " + fx.text, Kind: "quickfix", IsPreferred: true,
			Edit: change{{path: r.path, old: r.src, edits: edits}}.workspaceEdit(r.uri)}
		if d := r.diag("W0402"); d != nil {
			a.Diagnostics = []map[string]any{d}
		}
		out = append(out, a)
	}
	return out
}
