package load

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/tsprops"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Package is the files of one Go package in a directory that holds Vuka source.
// An external test package (name_test) is its own Package.
type Package struct {
	Dir        string
	ImportPath string
	Name       string
	Files      []transpile.File // .vuka and .go files matching the build context, and the Go of each Templ
	Templ      []*Templ         // .templ files, compiled with templ's generator
	Imports    []string
	Embeds     []string // directories below Dir of .templ files its .vuka files may name (vuka.File)
	NoLink     bool     // a file's header says //vuka:nolink: auto-link leaves the package alone
	// GoDir is where the go command sees the package when it isn't Dir: a
	// folder route's parameter folder, pages/pets/[id], is pages/pets/param_id
	// (GoDir). The generated files go there; the sources stay in Dir.
	GoDir string
	// After are packages transpiled before it, though it doesn't import
	// them: those of the context processors whose variables it reads by
	// name, unless they import it. One that would still close a cycle is
	// an error (ProcessorCycle).
	After []string
	// Links are packages the package's generated Go imports though its
	// sources don't: pages/route.go's folder-route page packages, which a
	// main reaches through pages.Routes(). Contexts follows them.
	Links []string
}

// BuildDir is the directory the go command sees p in: GoDir, else Dir.
func (p *Package) BuildDir() string {
	if p.GoDir != "" {
		return p.GoDir
	}
	return p.Dir
}

// A folder route names a path parameter with a bracketed folder,
// pages/pets/[id] or pages/files/[path...], and a route group with a
// parenthesized one, pages/(admin), neither of which an import path can
// hold; a private folder of pages/, pages/_parts, is no route, and the go
// command's ./... passes over it. The go command sees such a folder, and
// the folders below it, by its Go name: param_id, param_path, group_admin,
// private_parts. vuka's discovery passes over a folder named so beside its
// source folder: the generated files vuka gen -inplace writes there are
// the source folder's.

// Bracketed reports whether a file or folder name (its extension removed)
// is a path parameter's: [id], [path...].
func Bracketed(name string) bool {
	return len(name) > 2 && name[0] == '[' && name[len(name)-1] == ']'
}

// Grouped reports whether a folder name is a route group's: (admin).
func Grouped(name string) bool {
	return len(name) > 2 && name[0] == '(' && name[len(name)-1] == ')'
}

// Private reports whether a folder name is a private folder's, below
// pages/: _parts.
func Private(name string) bool { return len(name) > 1 && name[0] == '_' }

// PagesDir is the name of a folder routes' directory: a private folder is
// one below it.
const PagesDir = "pages"

// GoElem is a folder's name as the go command sees it: [id] is param_id,
// [path...] param_path, (admin) group_admin; any other name is itself.
// A private folder's Go name, private_parts for _parts, is GoRel's: only a
// folder below pages/ is one.
func GoElem(name string) string {
	switch {
	case Bracketed(name):
		return goPrefix + strings.TrimSuffix(name[1:len(name)-1], "...")
	case Grouped(name):
		return groupPrefix + name[1:len(name)-1]
	}
	return name
}

// The prefixes of the Go names: a bracketed folder's, a route group's and
// a private folder's.
const (
	goPrefix      = "param_"
	groupPrefix   = "group_"
	privatePrefix = "private_"
)

// sourceNames are the folder names whose Go name is elem: [id] and [id...]
// for param_id, (admin) for group_admin, and below pages/ _parts for
// private_parts; none for another name.
func sourceNames(elem string, inPages bool) []string {
	if name, ok := strings.CutPrefix(elem, goPrefix); ok && name != "" {
		return []string{"[" + name + "]", "[" + name + "...]"}
	}
	if name, ok := strings.CutPrefix(elem, groupPrefix); ok && name != "" {
		return []string{"(" + name + ")"}
	}
	if name, ok := strings.CutPrefix(elem, privatePrefix); ok && name != "" && inPages {
		return []string{"_" + name}
	}
	return nil
}

// InPages reports whether the directory dir, a path from the module's root
// (or an absolute path), is below a pages/ directory.
func InPages(dir string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(filepath.Dir(dir)), "/"), PagesDir)
}

// inPages is InPages for dir below root.
func inPages(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && !strings.HasPrefix(rel, "..") && InPages(rel)
}

// IsGoName reports whether the folder dir is the Go name of a bracketed,
// parenthesized or private folder beside it: param_id beside [id] or
// [id...], group_admin beside (admin), private_parts beside _parts.
func IsGoName(dir string) bool {
	parent := filepath.Dir(dir)
	for _, b := range sourceNames(filepath.Base(dir), InPages(dir)) {
		if fi, err := os.Stat(filepath.Join(parent, b)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// GoRel is a slash-separated path with each element its Go name: GoElem's,
// and private_parts for a private folder _parts below pages/.
func GoRel(rel string) string {
	if !strings.ContainsAny(rel, "[(_") {
		return rel
	}
	parts := strings.Split(rel, "/")
	inPages := false
	for i, p := range parts {
		switch {
		case inPages && Private(p):
			parts[i] = privatePrefix + p[1:]
		default:
			parts[i] = GoElem(p)
		}
		inPages = inPages || p == PagesDir
	}
	return strings.Join(parts, "/")
}

// RouteMapped reports whether the directory dir, below root, is in a
// bracketed folder or a route group, whose .go files the go command can't
// build where they are: vuka copies them into the folder's GoDir.
func RouteMapped(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, p := range strings.Split(filepath.ToSlash(rel), "/") {
		if Bracketed(p) || Grouped(p) {
			return true
		}
	}
	return false
}

// GoDir is the directory the go command sees the source directory dir,
// below root, as: dir itself unless a folder below root is bracketed, a
// route group, or private below pages/.
func GoDir(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return dir
	}
	if g := GoRel(filepath.ToSlash(rel)); g != filepath.ToSlash(rel) {
		return filepath.Join(root, filepath.FromSlash(g))
	}
	return dir
}

// SourceDir is the source directory whose GoDir is goDir, below root, when
// one of its folders is bracketed, a route group or private and goDir isn't
// on disk: param_id is [id], or [id...], whichever is there; group_admin is
// (admin); private_parts is _parts.
func SourceDir(root, goDir string) (string, bool) {
	rel, err := filepath.Rel(root, goDir)
	if err != nil || strings.HasPrefix(rel, "..") || !strings.Contains(rel, goPrefix) && !strings.Contains(rel, groupPrefix) && !strings.Contains(rel, privatePrefix) {
		return "", false
	}
	if _, err := os.Stat(goDir); err == nil {
		return "", false
	}
	dir, found, inPages := root, false, false
	for _, p := range strings.Split(filepath.ToSlash(rel), "/") {
		next := filepath.Join(dir, p)
		for _, b := range sourceNames(p, inPages) {
			if fi, err := os.Stat(filepath.Join(dir, b)); err == nil && fi.IsDir() {
				next, found = filepath.Join(dir, b), true
				break
			}
		}
		inPages = inPages || p == PagesDir
		dir = next
	}
	return dir, found
}

// ModuleRoot finds the module enclosing dir.
func ModuleRoot(dir string) (root, modPath string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if data, err := readGoMod(d); err == nil {
			sc := bufio.NewScanner(bytes.NewReader(data))
			for sc.Scan() {
				if f := strings.Fields(sc.Text()); len(f) >= 2 && f[0] == "module" {
					p, err := strconv.Unquote(f[1])
					if err != nil {
						p = f[1]
					}
					return d, p, nil
				}
			}
			return "", "", fmt.Errorf("%s: no module line", filepath.Join(d, "go.mod"))
		}
		if filepath.Dir(d) == d {
			return "", "", errors.New("no go.mod found in " + dir + " or above")
		}
	}
}

// ReadFunc reads a source file; the language server passes one that prefers
// the editor's unsaved buffers.
type ReadFunc func(path string) ([]byte, error)

// Discover returns the packages with Vuka source under dir (recursively when
// recursive), in a module rooted at root. A nil read reads the disk.
func Discover(root, modPath, dir string, recursive bool, read ReadFunc) ([]*Package, error) {
	if read == nil {
		read = ReadSource
	}
	// The go command matches overlay paths against real paths, so a module
	// reached through a symlink (macOS's /var is /private/var) is resolved.
	root, dir = realPath(root), realPath(dir)
	var pkgs []*Package
	seen := map[string]bool{}
	templ := TemplActive(root)
	live := TemplTrees(root)
	var visit func(d string) error
	visit = func(d string) error {
		if seen[d] {
			return nil
		}
		seen[d] = true
		found, err := readDir(root, modPath, d, read, templ, live)
		pkgs = append(pkgs, found...)
		if err != nil || recursive {
			return err
		}
		for _, p := range found {
			for _, e := range p.Embeds {
				if !hasGoMod(e) {
					if err := visit(e); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if !recursive {
		return pkgs, visit(dir)
	}
	err := WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || !e.IsDir() {
			return err
		}
		name := e.Name()
		if path != dir {
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") && !inPages(root, path) || name == "testdata" || name == "vendor" || name == "node_modules" || IsGoName(path) {
				return filepath.SkipDir
			}
			if hasGoMod(path) {
				return filepath.SkipDir
			}
		}
		return visit(path)
	})
	return pkgs, err
}

func readDir(root, modPath, dir string, read ReadFunc, templ, live bool) ([]*Package, error) {
	entries, err := ReadDir(dir)
	if err != nil {
		return nil, err
	}
	generated := map[string]bool{}
	hasSource := false
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir():
		case strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".vuka.html"):
			hasSource = true
			generated[transpile.GoName(name)] = true
			generated[transpile.CallsGoName] = true
		case templ && isTempl(name):
			hasSource = true
			generated[TemplGoName(name)] = true // a `templ generate` output; the fresh one replaces it
		}
	}
	if !hasSource && RouteMapped(root, dir) {
		// A route folder's Go, which the go command sees in its GoDir.
		for _, e := range entries {
			hasSource = hasSource || !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !generated[e.Name()]
		}
	}
	if !hasSource {
		return nil, nil
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	importPath := modPath
	if rel != "." {
		importPath += "/" + GoRel(filepath.ToSlash(rel))
	}
	goDir := ""
	if g := GoDir(root, dir); g != dir {
		goDir = g
	}
	byName := map[string]*Package{}
	var order []string
	pkg := func(name string) *Package {
		p := byName[name]
		if p == nil {
			p = &Package{Dir: dir, ImportPath: importPath, Name: name, GoDir: goDir}
			if strings.HasSuffix(name, "_test") {
				p.ImportPath += "_test"
			}
			byName[name] = p
			order = append(order, name)
		}
		return p
	}
	var broken []*Templ        // .templ files templ can't compile
	var htmls []transpile.File // .vuka.html files: the package's whose other files name it
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".vuka.html") {
			src, err := read(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			htmls = append(htmls, transpile.File{Name: name, Src: src})
			continue
		}
		if e.IsDir() || generated[name] || !(strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go") || templ && isTempl(name)) {
			continue
		}
		src, err := read(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var t *Templ
		if isTempl(name) {
			// Judged, parsed and type-checked as the Go file it becomes.
			if t = compileTempl(filepath.Join(dir, name), name, src, live, nil); t.Err != nil {
				broken = append(broken, t)
				continue
			}
			name, src = t.GoName, t.Go
		}
		if ok, err := matches(dir, name, src); err != nil || !ok {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil && strings.HasSuffix(name, ".vuka") && f != nil && f.Name != nil && f.Name.Name != "_" {
			err = nil // the imports are read; what follows them is Vuka (@attributes), which Go can't scan
		}
		if err != nil {
			if strings.HasSuffix(name, ".vuka") {
				// Mid-edit: let the transpiler report it with the rest.
				f, err = parser.ParseFile(token.NewFileSet(), name, src, parser.PackageClauseOnly)
			}
			if err != nil {
				continue
			}
		}
		p := pkg(f.Name.Name)
		p.Files = append(p.Files, transpile.File{Name: name, Src: src})
		if noLink(src) {
			p.NoLink = true
		}
		if strings.HasSuffix(name, ".vuka") {
			p.Embeds = append(p.Embeds, templDirs(dir, src)...)
		}
		if t != nil {
			p.Templ = append(p.Templ, t)
			if t.Live != nil {
				p.Imports = append(p.Imports, UIPath+"/templx") // the recorder, in what builds compile
			}
		}
		for _, imp := range f.Imports {
			if path, err := strconv.Unquote(imp.Path.Value); err == nil && !tsprops.IsComponent(path) {
				p.Imports = append(p.Imports, path)
			}
		}
	}
	for _, t := range broken {
		name := templPackageName(t.Src)
		if name == "" && len(order) > 0 {
			name = order[0]
		}
		if name == "" {
			name = filepath.Base(dir)
		}
		p := pkg(name)
		p.Templ = append(p.Templ, t)
	}
	if len(htmls) > 0 {
		name := filepath.Base(dir)
		for _, n := range order {
			if !strings.HasSuffix(n, "_test") {
				name = n
				break
			}
		}
		for _, h := range htmls {
			head := transpile.ParseTemplateHead(h.Src)
			p := pkg(name)
			if head.Package == name+"_test" {
				p = pkg(head.Package) // the external test package's
			}
			p.Files = append(p.Files, h)
			for _, imp := range head.Imports {
				if !strings.HasPrefix(imp.Path, ".") && !tsprops.IsComponent(imp.Path) {
					p.Imports = append(p.Imports, imp.Path) // a component file (./chart.vue) is no package
				}
			}
		}
	}
	var pkgs []*Package
	for _, n := range order {
		if p := byName[n]; p.hasVuka() || len(p.Templ) > 0 || p.GoDir != "" && len(p.Files) > 0 {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, nil
}

func (p *Package) hasVuka() bool {
	for _, f := range p.Files {
		if f.IsVuka() {
			return true
		}
	}
	return false
}

// matches applies the build context (GOOS/GOARCH file suffixes, //go:build lines)
// to a file; a .vuka file is judged as the .go file it becomes. A .templ
// file's Go, which isn't on disk, is judged by src.
func matches(dir, name string, src []byte) (bool, error) {
	ctx := build.Default
	if strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, "_templ.go") {
		name = strings.TrimSuffix(name, ".vuka")
		name = strings.TrimSuffix(name, ".go") + ".go"
		full := filepath.Join(dir, name)
		ctx.OpenFile = func(path string) (io.ReadCloser, error) {
			if path == full {
				return io.NopCloser(bytes.NewReader(src)), nil
			}
			return os.Open(path)
		}
	}
	return ctx.MatchFile(dir, name)
}

// templDirs are the subdirectories of dir named by string literals in src
// that are paths of .templ files: packages a vuka.File may refer to.
func templDirs(dir string, src []byte) []string {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(src)), src, func(token.Position, string) {}, 0)
	var dirs []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return dirs
		}
		if tok != token.STRING || !strings.HasSuffix(lit, `.templ"`) && !strings.HasSuffix(lit, ".templ`") {
			continue
		}
		p, err := strconv.Unquote(lit)
		if err != nil {
			continue
		}
		if d := path.Dir(path.Clean(filepath.ToSlash(p))); d != "." && d != ".." && !strings.HasPrefix(d, "../") && !path.IsAbs(d) {
			dirs = append(dirs, filepath.Join(dir, filepath.FromSlash(d)))
		}
	}
}

// Order sorts packages so each comes after the Vuka packages it imports.
func Order(pkgs []*Package) ([]*Package, error) {
	byPath, byDir := map[string]*Package{}, map[string]*Package{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
		if !strings.HasSuffix(p.Name, "_test") {
			byDir[p.Dir] = p
		}
	}
	via := throughGo(pkgs, byPath)
	var out []*Package
	state := map[*Package]int{} // 1 visiting, 2 done
	// frame is a package being ordered, and the processor package whose
	// variables it reads that the next frame was entered for ("" for an
	// import).
	type frame struct{ path, after string }
	var visit func(p *Package, stack []frame) error
	visit = func(p *Package, stack []frame) error {
		switch state[p] {
		case 1:
			i := slices.IndexFunc(stack, func(f frame) bool { return f.path == p.ImportPath })
			var paths []string
			for _, f := range stack[max(i, 0):] {
				paths = append(paths, f.path)
			}
			paths = append(paths, p.ImportPath)
			if i >= 0 {
				for _, f := range stack[i:] {
					if f.after != "" {
						return &ProcessorCycle{Package: f.path, Processor: f.after, Stack: paths}
					}
				}
			}
			return fmt.Errorf("import cycle: %s", strings.Join(paths, " -> "))
		case 2:
			return nil
		}
		state[p] = 1
		deps := append([]string(nil), p.Imports...)
		if strings.HasSuffix(p.ImportPath, "_test") {
			deps = append(deps, strings.TrimSuffix(p.ImportPath, "_test"))
		}
		sort.Strings(deps)
		for _, d := range deps {
			for _, q := range via(d) {
				if q != p {
					if err := visit(q, append(stack, frame{path: p.ImportPath})); err != nil {
						return err
					}
				}
			}
		}
		// The packages of the context processors it reads come first. None
		// imports it (Contexts leaves those out), but one may need it first
		// through the processors another package reads: a ProcessorCycle.
		for _, d := range p.After {
			for _, q := range via(d) {
				if q != p {
					if err := visit(q, append(stack, frame{path: p.ImportPath, after: d})); err != nil {
						return err
					}
				}
			}
		}
		// A .templ file's package the source may name comes first too, unless
		// that is a cycle: the literal may not be a vuka.File at all.
		for _, d := range p.Embeds {
			if q := byDir[d]; q != nil && q != p && state[q] == 0 {
				if err := visit(q, append(stack, frame{path: p.ImportPath})); err != nil {
					return err
				}
			}
		}
		state[p] = 2
		out = append(out, p)
		return nil
	}
	for _, p := range pkgs {
		if err := visit(p, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ProcessorCycle is an order Order can't find: Package reads the
// variables of the context processors of the package Processor by name,
// so it is transpiled after it, and Processor's package needs Package
// transpiled first, through the packages of Stack and other packages'
// processors.
type ProcessorCycle struct {
	Package, Processor string
	Stack              []string // the cycle's packages, its first again last
}

func (e *ProcessorCycle) Error() string {
	return fmt.Sprintf("%s reads the variables of %s's context processors by name, and %s needs it transpiled first: %s",
		e.Package, e.Processor, e.Processor, strings.Join(e.Stack, " -> "))
}

// throughGo is what importing a path brings of pkgs: the package itself, or
// for a Go package of the module the packages of pkgs it imports, directly
// or through other Go packages of the module.
func throughGo(pkgs []*Package, byPath map[string]*Package) func(string) []*Package {
	var root, modPath string
	if len(pkgs) > 0 {
		root, modPath, _ = ModuleRoot(pkgs[0].Dir)
		root = realPath(root)
	}
	memo := map[string][]*Package{}
	var via func(string) []*Package
	via = func(path string) []*Package {
		if q := byPath[path]; q != nil {
			return []*Package{q}
		}
		if got, ok := memo[path]; ok {
			return got
		}
		memo[path] = nil // an import cycle is the go command's to report
		rel, ok := strings.CutPrefix(path, modPath+"/")
		if root == "" || !ok && path != modPath {
			return nil
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		for d := dir; d != root && len(d) > len(root); d = filepath.Dir(d) {
			if hasGoMod(d) {
				return nil
			}
		}
		bp, _ := build.Default.ImportDir(dir, 0)
		if bp == nil {
			return nil
		}
		var out []*Package
		for _, imp := range bp.Imports {
			for _, q := range via(imp) {
				if !slices.Contains(out, q) {
					out = append(out, q)
				}
			}
		}
		memo[path] = out
		return out
	}
	return via
}

// Generated is one transpiled file: where it belongs and what it holds. It is
// the Go of a .vuka file, or of a .templ file (Map nil, TemplMap set when bare).
type Generated struct {
	Target string // the .go path beside the source file
	Source string // the .vuka or .templ file; empty for the package's call adapters
	Src    []byte
	From   []byte // the source text it was generated from
	Map    *transpile.SourceMap
	// Body is where the source's own lines end in Src, for a .vuka file
	// (transpile.Output.Body); 0 for a .templ file's Go.
	Body int
	// Declares says the file registers declarations at init — routes, crons,
	// commands — through a declarer, a type decorator or a composed decorator
	// (transpile.Output.Declares). AutoLink links its package into main.
	Declares bool
	// TemplMap is templ's source map (a *parser.SourceMap) for a .templ
	// file's Go, when Src is templ's raw output (Options.Bare); nil otherwise.
	TemplMap any
	Files    []transpile.FileRef // a .vuka file's vuka.File literals, in From
	Names    map[string]string   // the package's overloads (transpile.Output.Names)
	// Vars and Available are the context processors' variables a .vuka
	// file reads by name and may read (transpile.Output).
	Vars, Available []transpile.VarRead
	// Warnings are what vuka vet reports about the file.
	Warnings transpile.ErrorList
	// Unwrapped are the calls in its markup returning (T, error)
	// (transpile.Output.Unwrapped).
	Unwrapped []transpile.Unwrap
	// Implicit are the calls in its markup given the render's ctx
	// (transpile.Output.Implicit).
	Implicit []transpile.ImplicitCtx
}

// Options configure Transpile.
type Options struct {
	Bare bool // no header or line directives (the language server maps positions itself)
	// Tolerant keeps a module package with type errors importable, partially
	// type-checked: the language server, where an error anywhere must not
	// stop the rest of the module's requests.
	Tolerant bool
	// Scopes names each package's scoped declarations, by its directory
	// (transpile.Options.Scopes); a .templ file declaring one is compiled
	// again with its elements marked.
	Scopes map[string]map[string]string
	// Stop, when set, is asked before each package is transpiled: true
	// abandons the run, which returns ErrStopped. A session keeps what the
	// packages transpiled before it did.
	Stop func() bool
	// Contexts are the context processors each package reads by name, by
	// import path (transpile.Options.Contexts); nil finds them in the
	// module's package mains (Contexts).
	Contexts map[string][]transpile.Context
}

// AfterPackage, when set, is asked after each Vuka package transpiles, with
// the overlay holding its files, for more Go of that package the packages
// importing it need: the vuka command gives the ORM's generated typed
// fields of a package of models. Its files are added to the overlay and
// the run's output, bare or not: the language server shows them to gopls.
var AfterPackage func(dir, overlay string, gens []Generated) []Generated

// ErrStopped is the error of a run Options.Stop abandoned.
var ErrStopped = errors.New("stopped")

// Transpile transpiles pkgs in import order. Each package's output is added to
// an overlay in tmp before the next is transpiled, so imports of Vuka packages
// type-check. It returns the generated files and the overlay file; when some
// packages fail, the others' files come back with the error.
func Transpile(pkgs []*Package, tmp string, opts Options) ([]Generated, string, error) {
	return transpileRun(pkgs, tmp, opts, nil)
}

// pkgImporter resolves the imports of the packages a run transpiles.
type pkgImporter interface {
	types.Importer
	// Broken records that the Vuka package path didn't transpile.
	Broken(path string)
	// OverlayChanged says the overlay has a package's generated files.
	OverlayChanged()
}

// transpileRun is Transpile; with a session, packages whose inputs are as the
// session last transpiled them are taken from it, and imports of the
// module's own packages are type-checked from source in memory.
func transpileRun(pkgs []*Package, tmp string, opts Options, sess *Session) ([]Generated, string, error) {
	// The module's packages, read once: for the context processors, and
	// for the imports of the module's own Go packages, which type-checking
	// asks for too.
	var mod map[string]*modPkg
	modPath := ""
	moduleRoot := ""
	if len(pkgs) > 0 {
		if root, mp, err := ModuleRoot(pkgs[0].Dir); err == nil {
			// A newer Go than vuka's writes export data vuka may not read.
			if err := CheckGoVersion(realPath(root)); err != nil {
				return nil, "", err
			}
			moduleRoot = root
			mod, modPath = scanModule(realPath(root), mp, ReadSource), mp
			if opts.Contexts == nil {
				opts.Contexts = contexts(mod, pkgs, ReadSource)
			}
		}
	}
	for _, p := range pkgs {
		p.After = nil
		for _, path := range processorPaths(opts.Contexts[p.ImportPath]) {
			if path != p.ImportPath {
				p.After = append(p.After, path)
			}
		}
	}
	pkgs, err := Order(pkgs)
	if err != nil {
		return nil, "", processorCycleError(err, opts.Contexts)
	}
	compiles := compileSettings{}
	overlay := filepath.Join(tmp, "overlay.json")
	replace := map[string]string{}
	var out []Generated
	if sess == nil {
		if err := writeOverlay(overlay, replace); err != nil {
			return nil, "", err
		}
	}
	// One importer for the whole run: every package's imports looked up with
	// one go command, the standard library and dependencies cached across runs.
	vukaPkgs := map[string]bool{}
	for _, p := range pkgs {
		vukaPkgs[p.ImportPath] = true
	}
	var imports []string
	seen := map[string]bool{}
	for _, p := range pkgs {
		for _, path := range p.Imports {
			if !vukaPkgs[path] && !seen[path] && (sess == nil || !sess.inModule(path)) {
				seen[path] = true
				imports = append(imports, path)
			}
		}
	}
	var roots []string
	for _, p := range pkgs {
		roots = append(roots, p.Imports...)
	}
	imports = append(imports, moduleImports(mod, modPath, roots, seen)...)
	more := generatedImports(modPath, seen)
	var imp pkgImporter
	switch {
	case sess != nil:
		imp = sess
		_ = sess.imp.prefetch(imports, more)
	case len(pkgs) > 0:
		root, _, err := ModuleRoot(pkgs[0].Dir)
		if err != nil {
			root = pkgs[0].Dir
		}
		im := newCachedImporter(root, overlay)
		defer im.cache.save()
		_ = im.prefetch(imports, more) // a failure shows up where the import is used
		imp = im
	}
	flush := func() error {
		if sess != nil {
			return nil // written once, at the end: the session reads generated files from memory
		}
		if err := writeOverlay(overlay, replace); err != nil {
			return err
		}
		imp.OverlayChanged()
		return nil
	}
	templ := TemplFinder(pkgs)
	registry := ""
	if len(pkgs) > 0 {
		if root, _, err := ModuleRoot(pkgs[0].Dir); err == nil {
			registry = TemplRegistry(root)
		}
	}
	var errs transpile.ErrorList
	for i, p := range pkgs {
		if opts.Stop != nil && opts.Stop() {
			return nil, "", ErrStopped
		}
		add := func(goName string, src []byte) error {
			target := filepath.Join(p.BuildDir(), goName)
			file := filepath.Join(tmp, strconv.Itoa(i)+"_"+goName)
			if err := os.WriteFile(file, src, 0o644); err != nil {
				return err
			}
			replace[target] = file
			if sess != nil {
				sess.gen[target] = src
			}
			return nil
		}
		if p.GoDir != "" {
			// The folder's own .go files: copied where the go command sees
			// the package, a line directive naming the source, and gone
			// from the folder it can't build.
			for _, f := range p.Files {
				if f.IsVuka() || !strings.HasSuffix(f.Name, ".go") || slices.ContainsFunc(p.Templ, func(t *Templ) bool { return t.GoName == f.Name }) {
					continue
				}
				source := filepath.Join(p.Dir, f.Name)
				g := Generated{Target: filepath.Join(p.GoDir, f.Name), Source: source, From: f.Src}
				line := ""
				if !opts.Bare {
					line = "//line " + filepath.ToSlash(source) + ":1:1\n"
				}
				g.Src = append([]byte(line), f.Src...)
				g.Map = transpile.CopyMap(len(line), len(f.Src))
				if err := add(f.Name, g.Src); err != nil {
					return nil, "", err
				}
				replace[source] = ""
				out = append(out, g)
			}
		}
		if sc := opts.Scopes[p.Dir]; len(sc) > 0 {
			p.scopeTempl(sc)
		}
		cc, err := compiles.of(p.Dir)
		if err != nil {
			return nil, "", err
		}
		key := ""
		if sess != nil {
			if key = sess.inputKey(p, pkgs, registry, opts, cc); key == "" {
				return nil, "", sess.failed // the session gives up on this run
			}
			if gens, ok := sess.reuse(p, key); ok {
				for _, g := range gens {
					if err := add(filepath.Base(g.Target), g.Src); err != nil {
						return nil, "", err
					}
				}
				out = append(out, gens...)
				continue
			}
		}
		first := len(out)
		templOK := true
		for _, t := range p.Templ {
			if t.Err != nil {
				errs = append(errs, t.Err)
				templOK = false
				continue
			}
			g := Generated{Target: filepath.Join(p.BuildDir(), t.GoName), Source: filepath.Join(p.Dir, t.Name), From: t.Src}
			if opts.Bare {
				g.Src, g.TemplMap = t.Go, t.Map
			} else {
				g.Src = t.Build()
			}
			if err := add(t.GoName, g.Src); err != nil {
				return nil, "", err
			}
			out = append(out, g)
		}
		if !templOK {
			// The package's Go is incomplete without it; type-checking its
			// .vuka files would only report what the .templ error explains.
			imp.Broken(p.ImportPath)
			if err := flush(); err != nil {
				return nil, "", err
			}
			continue
		}
		topts := transpile.Options{
			Templ:         templ,
			TemplRegistry: registry,
			Importer:      imp,
			Path:          func(name string) string { return filepath.Join(p.Dir, name) },
			Bare:          opts.Bare,
			Dir:           p.Dir,
			ModuleRoot:    moduleRoot,
			ImportPath:    p.ImportPath,
			Scopes:        opts.Scopes[p.Dir],
			Contexts:      opts.Contexts[p.ImportPath],
			NoImplicitCtx: cc.NoImplicitCtx,
		}
		res, err := transpile.Package(p.Files, topts)
		if e := unreadable(imp); e != nil {
			return nil, "", e // not the type errors of each import it fails
		}
		partial := false
		if err != nil {
			var list transpile.ErrorList
			if !errors.As(err, &list) {
				return nil, "", fmt.Errorf("%s: %w", p.ImportPath, err)
			}
			errs = append(errs, list...)
			if opts.Bare {
				// The language server: the package's other files still
				// type-check without those with errors.
				for _, d := range list {
					if d.Code == "E0162" {
						recovery := topts
						recovery.IncompleteCalls = true
						res, _ = transpile.Package(p.Files, recovery)
						break
					}
				}
				if res == nil {
					res = transpileWithout(p, topts, list)
				}
			}
			if res == nil {
				imp.Broken(p.ImportPath)
				continue
			}
			partial = true
		}
		for _, f := range res.Files {
			target := filepath.Join(p.BuildDir(), f.GoName)
			if err := add(f.GoName, f.Src); err != nil {
				return nil, "", err
			}
			var from []byte
			for _, in := range p.Files {
				if in.Name == f.Name {
					from = in.Src
				}
			}
			out = append(out, Generated{Target: target, Source: filepath.Join(p.Dir, f.Name), Src: f.Src, From: from, Map: f.Map, Body: f.Body, Files: f.Files, Names: f.Names, Declares: f.Declares, Vars: f.Vars, Available: f.Available, Warnings: f.Warnings, Unwrapped: f.Unwrapped, Implicit: f.Implicit})
		}
		if res.Calls != nil {
			// The call adapters: a file of the package's, no source's.
			if err := add(transpile.CallsGoName, res.Calls); err != nil {
				return nil, "", err
			}
			out = append(out, Generated{Target: filepath.Join(p.BuildDir(), transpile.CallsGoName), Src: res.Calls})
		}
		if AfterPackage != nil {
			if err := writeOverlay(overlay, replace); err != nil {
				return nil, "", err
			}
			for _, g := range AfterPackage(p.BuildDir(), overlay, out[first:]) {
				if err := add(filepath.Base(g.Target), g.Src); err != nil {
					return nil, "", err
				}
				out = append(out, g)
			}
		}
		if sess != nil && !partial {
			sess.store(p, key, out[first:])
		}
		if err := flush(); err != nil {
			return nil, "", err
		}
	}
	if sess != nil {
		if err := writeOverlay(overlay, replace); err != nil {
			return nil, "", err
		}
	}
	if len(errs) > 0 {
		return out, overlay, onceEach(errs)
	}
	return out, overlay, nil
}

// unreadable is the export data imp couldn't read because the module's Go
// is newer than vuka's; nil when there was none.
func unreadable(imp pkgImporter) error {
	switch im := imp.(type) {
	case *Importer:
		return im.Unreadable()
	case *Session:
		return im.imp.Unreadable()
	}
	return nil
}

// transpileWithout transpiles p without what has errors, those of list and
// any the rest then has: a template file's template holding one is left
// blank, any other .vuka file left out. nil when no .vuka file is left or
// the errors are elsewhere.
func transpileWithout(p *Package, opts transpile.Options, list transpile.ErrorList) *transpile.Result {
	files := p.Files
	for {
		changed := false
		next := slices.Clone(files)
		drop := map[string]bool{}
		for _, e := range list {
			dir, name := filepath.Split(e.Pos.Filename)
			if filepath.Clean(dir) != p.Dir || !isVukaName(name) || drop[name] {
				continue
			}
			i := slices.IndexFunc(next, func(f transpile.File) bool { return f.Name == name })
			if i < 0 {
				continue
			}
			if next[i].IsTemplate() {
				if b := transpile.BlankTemplate(next[i].Src, e.Pos.Offset); b != nil && !bytes.Equal(b, next[i].Src) {
					next[i].Src, changed = b, true
					continue
				}
			}
			drop[name], changed = true, true
		}
		next = slices.DeleteFunc(next, func(f transpile.File) bool { return drop[f.Name] })
		if !changed || !slices.ContainsFunc(next, transpile.File.IsVuka) {
			return nil
		}
		res, err := transpile.Package(next, opts)
		if err == nil {
			return res
		}
		if !errors.As(err, &list) {
			return nil
		}
		files = next
	}
}

func isVukaName(name string) bool {
	return strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".vuka.html")
}

// TemplFinder finds .templ files of pkgs for vuka.File literals naming one
// in another package's directory.
func TemplFinder(pkgs []*Package) func(string) (transpile.TemplFile, error) {
	type found struct {
		p *Package
		t *Templ
	}
	byPath := map[string]found{}
	for _, p := range pkgs {
		for _, t := range p.Templ {
			byPath[filepath.Join(p.Dir, t.Name)] = found{p, t}
		}
	}
	return func(file string) (transpile.TemplFile, error) {
		file = realPath(file)
		dir := filepath.Dir(file)
		f, ok := byPath[file]
		switch {
		case !ok:
			if root, _, err := ModuleRoot(dir); err == nil && len(pkgs) > 0 && !strings.HasPrefix(pkgs[0].Dir+string(filepath.Separator), root+string(filepath.Separator)) {
				return transpile.TemplFile{}, fmt.Errorf("%s is in another module (%s)", dir, filepath.Join(root, "go.mod"))
			}
			return transpile.TemplFile{}, fmt.Errorf("%s is not a package of this module", dir)
		case f.t.Err != nil:
			return transpile.TemplFile{}, f.t.Err
		}
		if other := goPackage(dir, f.p.Name); other != "" {
			return transpile.TemplFile{}, fmt.Errorf("it is package %s; the directory's Go files are package %s", f.p.Name, other)
		}
		return transpile.TemplFile{ImportPath: f.p.ImportPath, Go: f.t.Go}, nil
	}
}

// goPackage is the package of a Go file in dir other than name, or "".
func goPackage(dir, name string) string {
	entries, _ := ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_templ.go") {
			continue
		}
		if ok, err := matches(dir, n, nil); err != nil || !ok {
			continue
		}
		if f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly); err == nil && f.Name.Name != name {
			return f.Name.Name
		}
	}
	return ""
}

func writeOverlay(path string, replace map[string]string) error {
	data, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// realPath is p with symlinks resolved, p when it doesn't resolve. Paths
// resolved are kept: a build asks for the same few many times, and each
// resolution stats every element of the path.
func realPath(p string) string {
	r, _ := RealPath(p)
	return r
}

// RealPath is filepath.EvalSymlinks, remembered; ok is false (and the path
// p) when p doesn't resolve.
func RealPath(p string) (string, bool) {
	if r, ok := realPaths.Load(p); ok {
		return r.(string), true
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p, false
	}
	realPaths.Store(p, r)
	return r, true
}

var realPaths sync.Map // path → its real path

var (
	rootFailure = regexp.MustCompile(`^(\S+) doesn't compile: `)
	depFailure  = regexp.MustCompile(`^(\S+) depends on (\S+), which doesn't compile: `)
)

// onceEach keeps the go command's explanation of a dependency that doesn't
// compile in the first error reporting it; later imports of it, and of
// packages that fail only because of it, say so without repeating it.
func onceEach(errs transpile.ErrorList) transpile.ErrorList {
	first := map[string]token.Position{}
	for _, e := range errs {
		if m := rootFailure.FindStringSubmatch(e.Msg); m != nil {
			if _, ok := first[m[1]]; !ok {
				first[m[1]] = e.Pos
			}
		}
	}
	for _, e := range errs {
		if m := rootFailure.FindStringSubmatch(e.Msg); m != nil && first[m[1]] != e.Pos {
			e.Msg = m[1] + " doesn't compile (see " + relPos(e.Pos, first[m[1]]) + ")"
		} else if m := depFailure.FindStringSubmatch(e.Msg); m != nil {
			if _, ok := first[m[2]]; ok {
				e.Msg = m[1] + " depends on " + m[2] + ", which doesn't compile"
			} else {
				first[m[2]] = e.Pos
			}
		}
	}
	return errs
}

// relPos is pos with its file relative to from's directory.
func relPos(from, pos token.Position) string {
	if rel, err := filepath.Rel(filepath.Dir(from.Filename), pos.Filename); err == nil {
		pos.Filename = rel
	}
	return pos.String()
}

// Closure is roots and what transpiling them needs of pkgs: the packages
// they import, the packages of the .templ files their vuka.File literals may
// name, and for an external test package the package it tests, all the way
// down; in pkgs' order.
func Closure(pkgs, roots []*Package) []*Package {
	byPath, byDir := map[string]*Package{}, map[string][]*Package{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
		byDir[p.Dir] = append(byDir[p.Dir], p)
	}
	via := throughGo(pkgs, byPath)
	in := map[*Package]bool{}
	var visit func(p *Package)
	visit = func(p *Package) {
		if in[p] {
			return
		}
		in[p] = true
		for _, imp := range p.Imports {
			for _, q := range via(imp) {
				visit(q)
			}
		}
		if strings.HasSuffix(p.ImportPath, "_test") {
			if q := byPath[strings.TrimSuffix(p.ImportPath, "_test")]; q != nil {
				visit(q)
			}
		}
		for _, d := range p.Embeds {
			for _, q := range byDir[d] {
				if !strings.HasSuffix(q.Name, "_test") {
					visit(q)
				}
			}
		}
	}
	for _, p := range roots {
		visit(p)
	}
	var out []*Package
	for _, p := range pkgs {
		if in[p] {
			out = append(out, p)
		}
	}
	return out
}
