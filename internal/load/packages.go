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
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	templparser "github.com/a-h/templ/parser/v2"

	"github.com/vuka-lang/vuka/transpile"
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
}

// ModuleRoot finds the module enclosing dir.
func ModuleRoot(dir string) (root, modPath string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
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
		read = os.ReadFile
	}
	// The go command matches overlay paths against real paths, so a module
	// reached through a symlink (macOS's /var is /private/var) is resolved.
	root, dir = realPath(root), realPath(dir)
	var pkgs []*Package
	seen := map[string]bool{}
	var visit func(d string) error
	visit = func(d string) error {
		if seen[d] {
			return nil
		}
		seen[d] = true
		found, err := readDir(root, modPath, d, read)
		pkgs = append(pkgs, found...)
		if err != nil || recursive {
			return err
		}
		for _, p := range found {
			for _, e := range p.Embeds {
				if _, err := os.Stat(filepath.Join(e, "go.mod")); err != nil {
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
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || !e.IsDir() {
			return err
		}
		name := e.Name()
		if path != dir {
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		return visit(path)
	})
	return pkgs, err
}

func readDir(root, modPath, dir string, read ReadFunc) ([]*Package, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	generated := map[string]bool{}
	hasSource := false
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir():
		case strings.HasSuffix(name, ".vuka"):
			hasSource = true
			generated[transpile.GoName(name)] = true
		case isTempl(name):
			hasSource = true
			generated[TemplGoName(name)] = true // a `templ generate` output; the fresh one replaces it
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
		importPath += "/" + filepath.ToSlash(rel)
	}
	byName := map[string]*Package{}
	var order []string
	pkg := func(name string) *Package {
		p := byName[name]
		if p == nil {
			p = &Package{Dir: dir, ImportPath: importPath, Name: name}
			if strings.HasSuffix(name, "_test") {
				p.ImportPath += "_test"
			}
			byName[name] = p
			order = append(order, name)
		}
		return p
	}
	var broken []*Templ // .templ files templ can't compile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || generated[name] || !(strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go") || isTempl(name)) {
			continue
		}
		src, err := read(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var t *Templ
		if isTempl(name) {
			// Judged, parsed and type-checked as the Go file it becomes.
			if t = compileTempl(filepath.Join(dir, name), name, src); t.Err != nil {
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
		if strings.HasSuffix(name, ".vuka") {
			p.Embeds = append(p.Embeds, templDirs(dir, src)...)
		}
		if t != nil {
			p.Templ = append(p.Templ, t)
		}
		for _, imp := range f.Imports {
			if path, err := strconv.Unquote(imp.Path.Value); err == nil {
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
	var pkgs []*Package
	for _, n := range order {
		if p := byName[n]; p.hasVuka() || len(p.Templ) > 0 {
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
	var out []*Package
	state := map[*Package]int{} // 1 visiting, 2 done
	var visit func(p *Package, stack []string) error
	visit = func(p *Package, stack []string) error {
		switch state[p] {
		case 1:
			return fmt.Errorf("import cycle: %s", strings.Join(append(stack, p.ImportPath), " -> "))
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
			if q := byPath[d]; q != nil && q != p {
				if err := visit(q, append(stack, p.ImportPath)); err != nil {
					return err
				}
			}
		}
		// A .templ file's package the source may name comes first too, unless
		// that is a cycle: the literal may not be a vuka.File at all.
		for _, d := range p.Embeds {
			if q := byDir[d]; q != nil && q != p && state[q] == 0 {
				if err := visit(q, append(stack, p.ImportPath)); err != nil {
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

// Generated is one transpiled file: where it belongs and what it holds. It is
// the Go of a .vuka file, or of a .templ file (Map nil, TemplMap set when bare).
type Generated struct {
	Target string // the .go path beside the source file
	Source string // the .vuka or .templ file
	Src    []byte
	From   []byte // the source text it was generated from
	Map    *transpile.SourceMap
	// TemplMap is templ's source map for a .templ file's Go, when Src is
	// templ's raw output (Options.Bare); nil otherwise.
	TemplMap *templparser.SourceMap
	Files    []transpile.FileRef // a .vuka file's vuka.File literals, in From
}

// Options configure Transpile.
type Options struct {
	Bare bool // no header or line directives (the language server maps positions itself)
}

// Transpile transpiles pkgs in import order. Each package's output is added to
// an overlay in tmp before the next is transpiled, so imports of Vuka packages
// type-check. It returns the generated files and the overlay file; when some
// packages fail, the others' files come back with the error.
func Transpile(pkgs []*Package, tmp string, opts Options) ([]Generated, string, error) {
	pkgs, err := Order(pkgs)
	if err != nil {
		return nil, "", err
	}
	overlay := filepath.Join(tmp, "overlay.json")
	replace := map[string]string{}
	var out []Generated
	if err := writeOverlay(overlay, replace); err != nil {
		return nil, "", err
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
			if !vukaPkgs[path] && !seen[path] {
				seen[path] = true
				imports = append(imports, path)
			}
		}
	}
	var imp *Importer
	if len(pkgs) > 0 {
		root, _, err := ModuleRoot(pkgs[0].Dir)
		if err != nil {
			root = pkgs[0].Dir
		}
		imp = newCachedImporter(root, overlay)
		defer imp.cache.save()
		_ = imp.Prefetch(imports) // a failure shows up where the import is used
	}
	templ := TemplFinder(pkgs)
	var errs transpile.ErrorList
	for i, p := range pkgs {
		add := func(goName string, src []byte) error {
			target := filepath.Join(p.Dir, goName)
			file := filepath.Join(tmp, strconv.Itoa(i)+"_"+goName)
			if err := os.WriteFile(file, src, 0o644); err != nil {
				return err
			}
			replace[target] = file
			return nil
		}
		templOK := true
		for _, t := range p.Templ {
			if t.Err != nil {
				errs = append(errs, t.Err)
				templOK = false
				continue
			}
			g := Generated{Target: filepath.Join(p.Dir, t.GoName), Source: filepath.Join(p.Dir, t.Name), From: t.Src}
			if opts.Bare {
				g.Src, g.TemplMap = t.Go, t.Map
			} else {
				g.Src = t.formatted()
			}
			if err := add(t.GoName, g.Src); err != nil {
				return nil, "", err
			}
			out = append(out, g)
		}
		if !templOK {
			// The package's Go is incomplete without it; type-checking its
			// .vuka files would only report what the .templ error explains.
			if err := writeOverlay(overlay, replace); err != nil {
				return nil, "", err
			}
			continue
		}
		res, err := transpile.Package(p.Files, transpile.Options{
			Templ:      templ,
			Importer:   imp,
			Path:       func(name string) string { return filepath.Join(p.Dir, name) },
			Bare:       opts.Bare,
			Dir:        p.Dir,
			ImportPath: p.ImportPath,
		})
		if err != nil {
			var list transpile.ErrorList
			if errors.As(err, &list) {
				errs = append(errs, list...)
				continue
			}
			return nil, "", fmt.Errorf("%s: %w", p.ImportPath, err)
		}
		for _, f := range res.Files {
			target := filepath.Join(p.Dir, f.GoName)
			if err := add(f.GoName, f.Src); err != nil {
				return nil, "", err
			}
			var from []byte
			for _, in := range p.Files {
				if in.Name == f.Name {
					from = in.Src
				}
			}
			out = append(out, Generated{Target: target, Source: filepath.Join(p.Dir, f.Name), Src: f.Src, From: from, Map: f.Map, Files: f.Files})
		}
		if err := writeOverlay(overlay, replace); err != nil {
			return nil, "", err
		}
	}
	if len(errs) > 0 {
		return out, overlay, errs
	}
	return out, overlay, nil
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
	entries, _ := os.ReadDir(dir)
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

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
