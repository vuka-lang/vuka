package main

import (
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vetScope is what vuka vet was asked about: the packages its directory
// patterns name (./entities/, ./modules/...), and what they import of the
// module, through Go packages and Vuka packages alike. vuka vet transpiles
// the whole module, as a build does; it reports the warnings of the asked
// packages only, and the errors of those and of the packages they import —
// the errors that stop them building.
type vetScope struct {
	root, modPath string
	patterns      []string        // the directory patterns, as given
	deps          map[string]bool // import paths of the module's packages they import, directly or not
}

// newVetScope is the scope of vuka vet's package arguments args; nil when
// they cover the whole module or name packages some other way (by import
// path, "all"), which leaves the report as it was.
func newVetScope(root, modPath string, pkgs []*load.Package, args []string) *vetScope {
	var patterns []string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
		case a == "." || a == ".." || strings.HasPrefix(a, "./") || strings.HasPrefix(a, "../") || filepath.IsAbs(a) ||
			strings.HasPrefix(a, `.\`) || strings.HasPrefix(a, `..\`):
			dir := strings.TrimSuffix(a, "/...")
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				continue // a flag's value: -vettool /path/to/tool
			}
			if moduleWide(root, a) {
				return nil
			}
			patterns = append(patterns, a)
		default:
			return nil
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	s := &vetScope{root: root, modPath: modPath, patterns: patterns, deps: map[string]bool{}}
	byPath := map[string]*load.Package{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
	}
	var visit func(path string)
	visit = func(path string) {
		if s.deps[path] || path != modPath && !strings.HasPrefix(path, modPath+"/") {
			return
		}
		s.deps[path] = true
		var imports []string
		if p := byPath[path]; p != nil {
			imports = p.Imports
		} else if bp, err := build.Default.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, modPath), "/"))), 0); err == nil {
			imports = bp.Imports
		}
		for _, imp := range imports {
			visit(imp)
		}
	}
	for _, p := range pkgs {
		if s.asksDir(p.Dir) {
			visit(p.ImportPath)
		}
	}
	// The Go packages it asks about: every directory the patterns match.
	for _, pat := range patterns {
		base, recursive := strings.CutSuffix(pat, "/...")
		abs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		filepath.WalkDir(abs, func(dir string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if dir != abs && (!recursive || strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "testdata" || hasGoModFile(dir)) {
				return filepath.SkipDir
			}
			if rel, err := filepath.Rel(realPathOf(root), realPathOf(dir)); err == nil && !strings.HasPrefix(rel, "..") {
				path := modPath
				if rel != "." {
					path += "/" + load.GoRel(filepath.ToSlash(rel))
				}
				if _, err := build.Default.ImportDir(dir, 0); err == nil {
					visit(path)
				}
			}
			return nil
		})
	}
	return s
}

func hasGoModFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// asksDir reports whether the patterns name the package in dir.
func (s *vetScope) asksDir(dir string) bool {
	return slices.ContainsFunc(s.patterns, func(p string) bool { return inPattern(dir, p) })
}

// asks reports whether d is about a file of a package the patterns name;
// one about no file is.
func (s *vetScope) asks(d diag.Diagnostic) bool {
	return d.Span.File == "" || s.asksDir(filepath.Dir(d.Span.File))
}

// warnings are the diagnostics of ds about the asked packages.
func (s *vetScope) warnings(ds []diag.Diagnostic) []diag.Diagnostic {
	if s == nil {
		return ds
	}
	return slices.DeleteFunc(ds, func(d diag.Diagnostic) bool { return !s.asks(d) })
}

// report narrows a failed transpile's report to the asked packages and the
// packages they import, returning how many errors of other packages it
// left out.
func (s *vetScope) report(r *buildReport, in buildInput) int {
	if s == nil || r == nil {
		return 0
	}
	left := 0
	r.groups = slices.DeleteFunc(r.groups, func(g errGroup) bool {
		if s.deps[g.path] || slices.ContainsFunc(g.diags, s.asks) {
			return false
		}
		left += len(g.diags)
		return true
	})
	r.importers = slices.DeleteFunc(r.importers, func(d diag.Diagnostic) bool { return !s.asks(d) })
	return left
}

// writeLeftOut says how many errors of packages it wasn't asked about vuka
// vet left out.
func writeLeftOut(w io.Writer, n int) {
	if n > 0 {
		fmt.Fprintf(w, "(%d %s in packages not asked about, nor imported by them: vuka vet ./... lists %s)\n", n, countWord(n, "error", "errors"), countWord(n, "it", "them"))
	}
}
