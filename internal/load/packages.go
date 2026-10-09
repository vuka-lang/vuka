package load

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

// Package is the files of one Go package in a directory that holds Vuka source.
// An external test package (name_test) is its own Package.
type Package struct {
	Dir        string
	ImportPath string
	Name       string
	Files      []transpile.File // .vuka and .go files matching the build context
	Imports    []string
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
	visit := func(d string) error {
		found, err := readDir(root, modPath, d, read)
		pkgs = append(pkgs, found...)
		return err
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
	hasVuka := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".vuka") {
			hasVuka = true
			generated[transpile.GoName(e.Name())] = true
		}
	}
	if !hasVuka {
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
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || generated[name] || !(strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go")) {
			continue
		}
		src, err := read(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if ok, err := matches(dir, name, src); err != nil || !ok {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			if strings.HasSuffix(name, ".vuka") {
				// Mid-edit: let the transpiler report it with the rest.
				f, err = parser.ParseFile(token.NewFileSet(), name, src, parser.PackageClauseOnly)
			}
			if err != nil {
				continue
			}
		}
		pkgName := f.Name.Name
		p := byName[pkgName]
		if p == nil {
			p = &Package{Dir: dir, ImportPath: importPath, Name: pkgName}
			if strings.HasSuffix(pkgName, "_test") {
				p.ImportPath += "_test"
			}
			byName[pkgName] = p
			order = append(order, pkgName)
		}
		p.Files = append(p.Files, transpile.File{Name: name, Src: src})
		for _, imp := range f.Imports {
			if path, err := strconv.Unquote(imp.Path.Value); err == nil {
				p.Imports = append(p.Imports, path)
			}
		}
	}
	var pkgs []*Package
	for _, n := range order {
		if p := byName[n]; p.hasVuka() {
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
// to a file; a .vuka file is judged as the .go file it becomes.
func matches(dir, name string, src []byte) (bool, error) {
	ctx := build.Default
	if strings.HasSuffix(name, ".vuka") {
		fake := strings.TrimSuffix(name, ".vuka") + ".go"
		full := filepath.Join(dir, fake)
		ctx.OpenFile = func(path string) (io.ReadCloser, error) {
			if path == full {
				return io.NopCloser(bytes.NewReader(src)), nil
			}
			return os.Open(path)
		}
		name = fake
	}
	return ctx.MatchFile(dir, name)
}

// Order sorts packages so each comes after the Vuka packages it imports.
func Order(pkgs []*Package) ([]*Package, error) {
	byPath := map[string]*Package{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
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

// Generated is one transpiled file: where it belongs and what it holds.
type Generated struct {
	Target string // the .go path beside the .vuka file
	Source string // the .vuka file
	Src    []byte
	From   []byte // the .vuka text it was generated from
	Map    *transpile.SourceMap
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
	var errs transpile.ErrorList
	for i, p := range pkgs {
		res, err := transpile.Package(p.Files, transpile.Options{
			Importer: imp,
			Path:     func(name string) string { return filepath.Join(p.Dir, name) },
			Bare:     opts.Bare,
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
			file := filepath.Join(tmp, strconv.Itoa(i)+"_"+f.GoName)
			if err := os.WriteFile(file, f.Src, 0o644); err != nil {
				return nil, "", err
			}
			replace[target] = file
			var from []byte
			for _, in := range p.Files {
				if in.Name == f.Name {
					from = in.Src
				}
			}
			out = append(out, Generated{Target: target, Source: filepath.Join(p.Dir, f.Name), Src: f.Src, From: from, Map: f.Map})
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
