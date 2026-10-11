package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/transpile"
)

// A Session transpiles one module again and again, as vuka run -watch does
// on every change, without repeating what the last run already did:
//
//   - The module's own packages that Vuka packages import are type-checked
//     from source in memory (Vuka packages from the Go just generated for
//     them), so no go command runs to compile them for their export data.
//     The standard library's and dependencies' export data is looked up
//     once per session.
//   - A package whose sources, and the API of the module's packages it
//     imports, are as the last run saw them isn't transpiled again: its
//     generated files are the last run's.
//
// Everything is keyed by content, never by what a watcher reports: a file
// edited, added or removed changes a key whatever noticed it. go.mod, go.sum,
// go.work, the go command and the build environment key the whole session;
// a change to any starts it over. Dependencies the module cache doesn't hold
// (directory replacements, workspace modules) can change unseen, so they are
// listed again on every run, and a change in their export data starts the
// session over too.
//
// Errors in Vuka source, and imports the go command can't resolve, are
// reported as Transpile reports them. A run that fails in any other way (a
// Go package of the module that doesn't type-check, cgo, an import cycle) is
// done again by Transpile, so errors read exactly as Transpile reports them.
// A session is not safe for concurrent use.
type Session struct {
	root, modPath string
	goVersion     string
	sizes         types.Sizes
	key           string    // cacheKey(root) the session's state was built under
	imp           *Importer // the standard library's and dependencies' export data
	checked       map[string]*checkedPkg
	outs          map[string]*pkgOut

	// One run's state.
	gen     map[string][]byte      // generated file → its Go, as the overlay has it
	now     map[string]*checkedPkg // the module's packages checked this run
	broken  map[string]bool        // Vuka packages that didn't transpile
	failed  error                  // why the session gave up on this run; nil when it didn't
	walking map[string]bool        // packages being checked, for cycles
	ran     runStats
}

// runStats counts what a run did, for tests.
type runStats struct {
	transpiled, reused, checked int
	fellBack                    bool
}

// checkedPkg is a package of the module, type-checked from source.
type checkedPkg struct {
	key string // its files' names and contents, and its module imports' keys
	pkg *types.Package
	api string // its API, and its module imports' APIs
}

// pkgOut is what a run generated for a package.
type pkgOut struct {
	key   string // inputKey
	gens  []Generated
	stats string // the vuka.File paths' existence, as fileStats saw it
}

// NewSession starts a session for the module at root.
func NewSession(root string) (*Session, error) {
	root, modPath, err := ModuleRoot(root)
	if err != nil {
		return nil, err
	}
	root = realPath(root)
	s := &Session{root: root, modPath: modPath, sizes: types.SizesFor("gc", runtime.GOARCH)}
	if arch := os.Getenv("GOARCH"); arch != "" {
		if sz := types.SizesFor("gc", arch); sz != nil {
			s.sizes = sz
		}
	}
	return s, nil
}

// Root is the directory of the session's module, symlinks resolved.
func (s *Session) Root() string { return s.root }

func (s *Session) reset() {
	s.key = cacheKey(s.root)
	s.goVersion = goDirective(s.root)
	s.imp = newCachedImporter(s.root, "")
	s.imp.volatile = map[string]string{}
	s.checked = map[string]*checkedPkg{}
	s.outs = map[string]*pkgOut{}
}

// Transpile is Transpile, for the session's module: pkgs as Discover found
// them this time.
func (s *Session) Transpile(pkgs []*Package, tmp string, opts Options) ([]Generated, string, error) {
	if s.imp == nil || cacheKey(s.root) != s.key || !s.imp.refresh() {
		s.reset()
	}
	defer s.imp.cache.save()
	s.gen, s.now, s.broken, s.failed, s.walking = map[string][]byte{}, map[string]*checkedPkg{}, map[string]bool{}, nil, map[string]bool{}
	s.ran = runStats{}
	gens, overlay, err := transpileRun(pkgs, tmp, opts, s)
	if errors.Is(err, ErrStopped) {
		s.gen, s.now, s.walking = nil, nil, nil
		return nil, "", err
	}
	var list transpile.ErrorList
	if s.failed == nil && err != nil && !errors.As(err, &list) {
		s.failed = err
	}
	if s.failed == nil {
		for path := range s.imp.exports {
			if s.inModule(path) {
				// A dependency imports the module's own package: its export
				// data and the session's type-checked package would be two.
				s.failed = errors.New(path + " is imported by a dependency")
				break
			}
		}
	}
	s.gen, s.now, s.walking = nil, nil, nil
	if s.failed != nil {
		s.ran.fellBack = true
		return transpileRun(pkgs, tmp, opts, nil)
	}
	return gens, overlay, err
}

// fail records why the session gives up on this run.
func (s *Session) fail(err error) {
	if s.failed == nil {
		s.failed = err
	}
}

// inModule reports whether path is a package of the session's module (not
// of a module nested in it), which the session type-checks itself.
func (s *Session) inModule(path string) bool {
	_, ok := s.dirOf(path)
	return ok
}

func (s *Session) dirOf(path string) (string, bool) {
	if strings.HasSuffix(path, "_test") {
		return "", false
	}
	var dir string
	switch {
	case path == s.modPath:
		return s.root, true
	case strings.HasPrefix(path, s.modPath+"/"):
		dir = filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(path, s.modPath+"/")))
	default:
		return "", false
	}
	for d := dir; d != s.root && len(d) > len(s.root); d = filepath.Dir(d) {
		if fileExists(filepath.Join(d, "go.mod")) {
			return "", false
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", false
	}
	return dir, true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Session) Import(path string) (*types.Package, error) {
	return s.ImportFrom(path, s.root, 0)
}

func (s *Session) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if s.broken[path] {
		return nil, fmt.Errorf("%s has errors", path) // as the Importer says it
	}
	if s.inModule(path) {
		c, err := s.check(path)
		if err != nil {
			s.fail(err)
			return nil, err
		}
		return c.pkg, nil
	}
	// What the go command says of an import it can't resolve is what
	// Transpile's importer, the same code, says too.
	return s.imp.ImportFrom(path, dir, mode)
}

func (s *Session) Broken(path string) { s.broken[path] = true }

func (s *Session) OverlayChanged() {}

// check type-checks the module's package path from its Go files as the
// overlay has them, or returns it as checked before when they and its
// imports are as they were.
func (s *Session) check(path string) (*checkedPkg, error) {
	if c := s.now[path]; c != nil {
		return c, nil
	}
	if s.walking[path] {
		return nil, errors.New("import cycle through " + path)
	}
	s.walking[path] = true
	defer delete(s.walking, path)
	dir, _ := s.dirOf(path)
	bp, err := s.buildContext().ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(bp.CgoFiles) > 0 {
		return nil, errors.New(path + " uses cgo")
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s %s\n", path, s.goVersion)
	srcs := make([][]byte, len(bp.GoFiles))
	for i, name := range bp.GoFiles {
		if srcs[i], err = s.read(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
		writeFile(h, name, srcs[i])
	}
	var imports []*checkedPkg
	for _, imp := range bp.Imports { // sorted
		if s.inModule(imp) {
			c, err := s.check(imp)
			if err != nil {
				return nil, err
			}
			imports = append(imports, c)
			fmt.Fprintf(h, "import %s %s\n", imp, c.key)
		}
	}
	key := hex.EncodeToString(h.Sum(nil))
	if c := s.checked[path]; c != nil && c.key == key {
		s.now[path] = c
		return c, nil
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, len(srcs))
	for i, src := range srcs {
		if files[i], err = parser.ParseFile(fset, filepath.Join(dir, bp.GoFiles[i]), src, parser.SkipObjectResolution); err != nil {
			return nil, err
		}
	}
	var typeErr error
	conf := types.Config{Importer: s, GoVersion: s.goVersion, Sizes: s.sizes, Error: func(err error) {
		if typeErr == nil {
			typeErr = err
		}
	}}
	s.ran.checked++
	pkg, _ := conf.Check(path, fset, files, nil)
	if typeErr != nil {
		return nil, fmt.Errorf("%s doesn't compile: %w", path, typeErr)
	}
	api := sha256.New()
	writeAPI(api, pkg)
	for _, c := range imports {
		api.Write([]byte(c.api))
	}
	c := &checkedPkg{key: key, pkg: pkg, api: hex.EncodeToString(api.Sum(nil))}
	s.checked[path], s.now[path] = c, c
	return c, nil
}

// read reads a file as the overlay has it.
func (s *Session) read(path string) ([]byte, error) {
	if src, ok := s.gen[path]; ok {
		return src, nil
	}
	return os.ReadFile(path)
}

// buildContext is the default build context, seeing the files this run
// generated as if they were on disk.
func (s *Session) buildContext() *build.Context {
	ctx := build.Default
	ctx.ReadDir = func(dir string) ([]fs.FileInfo, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var infos []fs.FileInfo
		have := map[string]bool{}
		for _, e := range entries {
			if fi, err := e.Info(); err == nil {
				if src, ok := s.gen[filepath.Join(dir, e.Name())]; ok {
					fi = genInfo{e.Name(), int64(len(src))}
				}
				infos = append(infos, fi)
				have[e.Name()] = true
			}
		}
		for target, src := range s.gen {
			if filepath.Dir(target) == dir && !have[filepath.Base(target)] {
				infos = append(infos, genInfo{filepath.Base(target), int64(len(src))})
			}
		}
		slices.SortFunc(infos, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
		return infos, nil
	}
	ctx.OpenFile = func(path string) (io.ReadCloser, error) {
		if src, ok := s.gen[path]; ok {
			return io.NopCloser(bytes.NewReader(src)), nil
		}
		return os.Open(path)
	}
	return &ctx
}

// genInfo is a generated file as ReadDir shows it.
type genInfo struct {
	name string
	size int64
}

func (g genInfo) Name() string       { return g.name }
func (g genInfo) Size() int64        { return g.size }
func (g genInfo) Mode() fs.FileMode  { return 0o644 }
func (g genInfo) ModTime() time.Time { return time.Time{} }
func (g genInfo) IsDir() bool        { return false }
func (g genInfo) Sys() any           { return nil }

// inputKey is what transpiling p depends on: its sources, the API of the
// module's packages it imports, the .templ packages its vuka.File literals
// may name, and the options. "" when the session gives up on the run.
func (s *Session) inputKey(p *Package, pkgs []*Package, registry string, opts Options) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s %s %s %s %v\n", p.ImportPath, p.Name, p.Dir, registry, opts.Bare)
	fmt.Fprintf(h, "scopes %v\n", opts.Scopes[p.Dir]) // fmt prints a map sorted
	writePackage(h, p)
	imports := slices.Clone(p.Imports)
	slices.Sort(imports)
	for _, imp := range slices.Compact(imports) {
		if imp == p.ImportPath || !s.inModule(imp) {
			continue
		}
		if s.broken[imp] {
			fmt.Fprintf(h, "import %s broken\n", imp)
			continue
		}
		c, err := s.check(imp)
		if err != nil {
			s.fail(err)
			return ""
		}
		fmt.Fprintf(h, "import %s %s\n", imp, c.api)
	}
	for _, d := range p.Embeds {
		for _, q := range pkgs {
			if q.Dir == d && q != p {
				fmt.Fprintf(h, "embeds %s\n", q.ImportPath)
				writePackage(h, q)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writePackage(h hash.Hash, p *Package) {
	for _, f := range p.Files {
		writeFile(h, f.Name, f.Src)
	}
	for _, t := range p.Templ {
		writeFile(h, t.Name, t.Src)
		writeFile(h, t.GoName, t.Live) // what builds compile, when instrumented
		fmt.Fprintf(h, "templ error %v\n", t.Err != nil)
	}
}

func writeFile(h hash.Hash, name string, src []byte) {
	fmt.Fprintf(h, "file %s %d\n", name, len(src))
	h.Write(src)
}

// reuse returns the files the last run generated for p, when its inputs
// are as they were then.
func (s *Session) reuse(p *Package, key string) ([]Generated, bool) {
	o := s.outs[p.ImportPath]
	if o == nil || o.key != key || o.stats != fileStats(o.gens) {
		return nil, false
	}
	s.ran.reused++
	return o.gens, true
}

func (s *Session) store(p *Package, key string, gens []Generated) {
	s.ran.transpiled++
	s.outs[p.ImportPath] = &pkgOut{key: key, gens: slices.Clone(gens), stats: fileStats(gens)}
}

// fileStats says whether each file the vuka.File literals of gens name
// exists, and is a directory: what the transpiler checked of them.
func fileStats(gens []Generated) string {
	var b strings.Builder
	for _, g := range gens {
		for _, f := range g.Files {
			fi, err := os.Stat(f.Path)
			fmt.Fprintf(&b, "%s %v %v\n", f.Path, err == nil, err == nil && fi.IsDir())
		}
	}
	return b.String()
}

// writeAPI writes what importers of pkg see of it: its package-level
// declarations with their types, constants' values and named types'
// methods. Positions are left out: a change that only moves code doesn't
// reach importers.
func writeAPI(h hash.Hash, pkg *types.Package) {
	q := func(p *types.Package) string { return p.Path() }
	fmt.Fprintf(h, "package %s %s\n", pkg.Path(), pkg.Name())
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		fmt.Fprintf(h, "%s\n", types.ObjectString(obj, q))
		switch o := obj.(type) {
		case *types.Const:
			fmt.Fprintf(h, "= %s\n", o.Val().ExactString())
		case *types.TypeName:
			fmt.Fprintf(h, "underlying %s\n", types.TypeString(o.Type().Underlying(), q))
			if n, ok := o.Type().(*types.Named); ok {
				for i := 0; i < n.NumMethods(); i++ {
					fmt.Fprintf(h, "method %s\n", types.ObjectString(n.Method(i), q))
				}
			}
		}
	}
}

// goDirective is the go version the go.mod at root declares, as go/types
// takes it ("go1.23"); "" when it declares none.
func goDirective(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "go" {
			return "go" + f[1]
		}
	}
	return ""
}
