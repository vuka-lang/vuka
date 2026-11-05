package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const modUsage = `usage: vuka mod tidy [-check] [-e] [-v] [-go=version] [-compat=version]
       vuka mod why|vendor|graph|download|… [args]

vuka mod tidy is go mod tidy for a module with Vuka sources. go mod tidy
reads only .go files, so it drops what only .vuka and .vuka.html files
import, and what only the Go vuka generates without writing it needs (the
runtime, the orm's typed fields, route tables, URL helpers, call
adapters). vuka mod tidy transpiles the module as vuka build does, runs go
mod tidy in a temporary copy of the module holding that Go as files, and
copies back go.mod and go.sum alone: nothing generated is written to the
module. It prints the requirements it added and removed.

	-check   change nothing: exit 1 when go.mod or go.sum isn't tidy
	         (for CI), saying what vuka mod tidy would change
	-e, -v, -go, -compat
	         go mod tidy's own flags (go help mod tidy)

The other go mod commands run in the module, with a file of blank imports
standing for each package's .vuka imports for the command's duration.
`

// modTidy is vuka mod tidy.
func modTidy(args []string, stdout, stderr io.Writer) error {
	fl := flag.NewFlagSet("vuka mod tidy", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, modUsage) }
	check := fl.Bool("check", false, "")
	e := fl.Bool("e", false, "")
	v := fl.Bool("v", false, "")
	goVersion := fl.String("go", "", "")
	compat := fl.String("compat", "", "")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitStatus(2)
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "vuka mod tidy: no arguments are taken, %q given\n\n%s", fl.Arg(0), modUsage)
		return exitStatus(2)
	}
	tidyArgs := []string{"mod", "tidy"}
	if *e {
		tidyArgs = append(tidyArgs, "-e")
	}
	if *v {
		tidyArgs = append(tidyArgs, "-v")
	}
	if *goVersion != "" {
		tidyArgs = append(tidyArgs, "-go="+*goVersion)
	}
	if *compat != "" {
		tidyArgs = append(tidyArgs, "-compat="+*compat)
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	res, err := tidyPass(root, modPath, tidyArgs, stderr)
	if err != nil {
		return err
	}
	// Sources that don't transpile for want of their dependencies (a new
	// app, before its first tidy): once their imports are required, the
	// whole module is transpiled again.
	if res.transpileErr != nil && !*check && res.changed() {
		if err := res.write(); err != nil {
			return err
		}
		orig := res.origMod
		if res, err = tidyPass(root, modPath, tidyArgs, stderr); err != nil {
			return err
		}
		res.origMod = orig
	}
	if res.transpileErr != nil {
		fmt.Fprintln(stderr, "vuka mod tidy: warning: the module doesn't all transpile, so of what doesn't only the imports were read:")
		ds, rest := diag.From(res.transpileErr)
		writeDiags(stderr, ds, colorFor(stderr))
		for _, e := range rest {
			fmt.Fprintln(stderr, e)
		}
	}
	if err := res.needed(); err != nil {
		return err
	}
	changes := res.changes()
	if res.ormByGoRun() {
		fmt.Fprintln(stderr, "vuka mod tidy: note: the build runs the orm's tool (for the models' typed fields) with go run, whose go.sum entries tidy drops;\n  go get -tool "+ormModule+"/cmd/orm declares it, and they are kept")
	}
	if *check {
		if !res.changed() {
			return nil
		}
		what := "go.mod and go.sum aren't"
		switch {
		case bytes.Equal(res.oldMod, res.newMod):
			what = "go.sum isn't"
		case bytes.Equal(res.oldSum, res.newSum):
			what = "go.mod isn't"
		}
		fmt.Fprintf(stderr, "vuka mod tidy -check: %s tidy\n", what)
		for _, c := range changes {
			fmt.Fprintln(stderr, "\t"+c)
		}
		return exitStatus(1)
	}
	if err := res.write(); err != nil {
		return err
	}
	for _, c := range changes {
		fmt.Fprintln(stdout, c)
	}
	return nil
}

// tidyResult is a go mod tidy run in a copy of the module.
type tidyResult struct {
	root           string
	origMod        []byte // go.mod before the first pass: what changes and needed compare with
	oldMod, newMod []byte
	oldSum, newSum []byte
	hadSum, hasSum bool
	gens           []load.Generated
	transpileErr   error // the module didn't all transpile: tidy read the imports of what didn't
}

func (r *tidyResult) changed() bool {
	return !bytes.Equal(r.oldMod, r.newMod) || !bytes.Equal(r.oldSum, r.newSum) || r.hadSum != r.hasSum
}

// write copies go.mod and go.sum back to the module, where they changed.
func (r *tidyResult) write() error {
	mod := filepath.Join(r.root, "go.mod")
	if !bytes.Equal(r.oldMod, r.newMod) {
		if err := writeKeepingMode(mod, r.newMod); err != nil {
			return err
		}
		r.oldMod = r.newMod
	}
	sum := filepath.Join(r.root, "go.sum")
	switch {
	case r.hasSum && (!r.hadSum || !bytes.Equal(r.oldSum, r.newSum)):
		if err := writeKeepingMode(sum, r.newSum); err != nil {
			return err
		}
	case r.hadSum && !r.hasSum:
		if err := os.Remove(sum); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	r.oldSum, r.hadSum = r.newSum, r.hasSum
	return nil
}

func writeKeepingMode(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

// changes are the requirements tidy added, removed and moved, one line each.
func (r *tidyResult) changes() []string {
	before, after := requirements(r.origMod), requirements(r.newMod)
	var out []string
	for _, p := range slices.Sorted(maps.Keys(after)) {
		if old, ok := before[p]; !ok {
			out = append(out, "added   "+p+" "+after[p])
		} else if old != after[p] {
			out = append(out, "changed "+p+" "+old+" => "+after[p])
		}
	}
	for _, p := range slices.Sorted(maps.Keys(before)) {
		if _, ok := after[p]; !ok {
			out = append(out, "removed "+p+" "+before[p])
		}
	}
	return out
}

// requirements are go.mod's require directives: module path → version.
func requirements(mod []byte) map[string]string {
	out := map[string]string{}
	f, err := modfile.Parse("go.mod", mod, nil)
	if err != nil {
		return out
	}
	for _, r := range f.Require {
		out[r.Mod.Path] = r.Mod.Version
	}
	return out
}

// ormByGoRun reports whether the build reads the orm's typed fields, which
// it generates with go run of the orm's tool, go.mod declaring no tool.
func (r *tidyResult) ormByGoRun() bool {
	if requirements(r.newMod)[ormModule] == "" || bytes.Contains(r.newMod, []byte("tool "+ormModule+"/cmd/orm")) {
		return false
	}
	return slices.ContainsFunc(r.gens, func(g load.Generated) bool { return typedFieldsUse.Match(g.Src) })
}

// needed checks tidy kept the requirement of every module the generated Go
// imports from, and of every tool: the copy holds that Go, so a miss is
// vuka's bug, and go.mod is left as it was rather than broken.
func (r *tidyResult) needed() error {
	before, after := requirements(r.origMod), requirements(r.newMod)
	f, _ := modfile.Parse("go.mod", r.origMod, nil)
	if f == nil || f.Module == nil {
		return nil
	}
	main := f.Module.Mod.Path
	type use struct{ pkg, by string }
	var uses []use
	for _, t := range f.Tool {
		uses = append(uses, use{t.Path, "a tool directive"})
	}
	for _, g := range r.gens {
		file, _ := parser.ParseFile(token.NewFileSet(), g.Target, g.Src, parser.ImportsOnly)
		if file == nil {
			continue
		}
		by, _ := filepath.Rel(r.root, g.Target)
		if g.Source != "" {
			by, _ = filepath.Rel(r.root, g.Source)
		}
		for _, imp := range file.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil {
				uses = append(uses, use{p, by})
			}
		}
	}
	for _, u := range uses {
		if u.pkg == main || strings.HasPrefix(u.pkg, main+"/") {
			continue
		}
		mod := ""
		for p := range before {
			if (u.pkg == p || strings.HasPrefix(u.pkg, p+"/")) && len(p) > len(mod) {
				mod = p
			}
		}
		if mod == "" {
			continue
		}
		if _, kept := after[mod]; !kept {
			return fmt.Errorf("vuka mod tidy: go mod tidy would remove %s, which %s still needs (%s); go.mod and go.sum are left as they were. This is a bug in vuka mod tidy: please report it", mod, u.by, u.pkg)
		}
	}
	return nil
}

// tidyPass transpiles the module at root as vuka build does and runs go mod
// tidy in a copy of it holding the generated Go as files.
func tidyPass(root, modPath string, tidyArgs []string, stderr io.Writer) (*tidyResult, error) {
	res := &tidyResult{root: root}
	var err error
	if res.oldMod, err = os.ReadFile(filepath.Join(root, "go.mod")); err != nil {
		return nil, err
	}
	res.origMod = res.oldMod
	if res.oldSum, err = os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		res.hadSum = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "vuka-tidy-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return nil, err
	}
	gens, overlay, terr := tidyTranspile(root, modPath, pkgs, filepath.Join(tmp, "gen"))
	res.gens, res.transpileErr = gens, terr

	copyDir, err := tidyCopyDir(tmp, res.oldMod)
	if err != nil {
		return nil, err
	}
	if err := copyModule(root, copyDir, res.oldMod, overlay); err != nil {
		return nil, err
	}
	// The imports of each package's .vuka and .templ files, as files of
	// blank imports: what doesn't transpile is still read.
	for path, src := range importStubs(root, pkgs) {
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		dst := filepath.Join(copyDir, rel)
		if fileExists(dst) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			return nil, err
		}
	}

	if tidyCopyHook != nil {
		tidyCopyHook(copyDir)
	}
	c := exec.Command("go", tidyArgs...)
	c.Dir = copyDir
	c.Env = append(os.Environ(), "PWD="+copyDir)
	if os.Getenv("GOWORK") == "" {
		c.Env = append(c.Env, "GOWORK=off")
	}
	out := &pathRewriter{w: stderr, from: copyDir, to: root}
	c.Stdout, c.Stderr = out, out
	err = c.Run()
	out.flush()
	if err != nil {
		return nil, fmt.Errorf("go mod tidy: %v", err)
	}
	if res.newMod, err = os.ReadFile(filepath.Join(copyDir, "go.mod")); err != nil {
		return nil, err
	}
	if res.newSum, err = os.ReadFile(filepath.Join(copyDir, "go.sum")); err == nil {
		res.hasSum = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return res, nil
}

// tidyCopyHook, for tests, sees the copy before go mod tidy runs in it.
var tidyCopyHook func(dir string)

// tidyTranspile is vuka build's transpile of the module, with the route
// tables and the Go generated for the module as a whole, its tests' too:
// the generated Go, and the overlay naming it. Nothing is written to the
// module. An error is the transpile's: what it generated is returned with
// it.
func tidyTranspile(root, modPath string, pkgs []*load.Package, tmp string) ([]load.Generated, string, error) {
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, "", err
	}
	pa, err := findAssets(root, modPath)
	if err != nil {
		return nil, "", err
	}
	routes := scanRoutes(root, modPath, nil)
	if pkgs, err = routes.stub(pkgs); err != nil {
		return nil, "", err
	}
	var gens []load.Generated
	var overlay string
	var terr error
	if len(pkgs) > 0 {
		gens, overlay, terr = cachedTranspile(root)(pkgs, tmp, loadOptions(pa))
	}
	if g, o, err := routes.apply(gens, overlay, tmp); err != nil {
		if g != nil {
			gens, overlay = g, o
		}
		terr = errors.Join(terr, err)
	} else {
		gens, overlay = g, o
	}
	o, g, _, err := moduleOverlay(root, modPath, tmp, overlay, pkgs, gens, true, io.Discard)
	if err != nil {
		return gens, overlay, errors.Join(terr, err)
	}
	return g, o, terr
}

// tidyCopyDir is where in tmp the module's copy goes: deep enough that
// each local replace directive's relative path, ../x, names a place in tmp
// too (copyModule links it to the real directory), so go.mod is copied as
// it is.
func tidyCopyDir(tmp string, mod []byte) (string, error) {
	depth := 1
	if f, err := modfile.Parse("go.mod", mod, nil); err == nil {
		for _, r := range f.Replace {
			if !modfile.IsDirectoryPath(r.New.Path) || filepath.IsAbs(r.New.Path) {
				continue
			}
			n := 0
			for _, el := range strings.Split(filepath.ToSlash(filepath.Clean(r.New.Path)), "/") {
				if el != ".." {
					break
				}
				n++
			}
			depth = max(depth, n)
		}
	}
	dir := filepath.Join(tmp, "m")
	for range depth - 1 {
		dir = filepath.Join(dir, "m")
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// copyModule copies to dst what go mod tidy reads of the module at root:
// go.mod and go.sum, its packages' .go files (as the overlay has them,
// with the files it adds), in the directories the go command reads — no
// nested module, testdata, or directory whose name starts with . or _ —
// a link to vendor/, and links in place of the directories local replace
// directives name. tidy reads a package's imports alone, so nothing else
// (embedded files, assets) is copied.
func copyModule(root, dst string, mod []byte, overlay string) error {
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			return err
		}
	}
	replace := map[string]string{}
	if overlay != "" {
		data, err := os.ReadFile(overlay)
		if err != nil {
			return err
		}
		var ov struct{ Replace map[string]string }
		if err := json.Unmarshal(data, &ov); err != nil {
			return err
		}
		replace = ov.Replace
	}
	var nested []string
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == root {
				return nil
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
				return filepath.SkipDir
			}
			if fileExists(filepath.Join(path, "go.mod")) {
				nested = append(nested, path)
				return filepath.SkipDir
			}
			if name == "vendor" && filepath.Dir(path) == root {
				return os.Symlink(path, filepath.Join(dst, "vendor"))
			}
			return nil
		}
		if !strings.HasSuffix(e.Name(), ".go") {
			return nil
		}
		if _, ok := replace[path]; ok {
			return nil // the overlay's, below
		}
		return copyFile(path, filepath.Join(dst, strings.TrimPrefix(path, root)))
	})
	if err != nil {
		return err
	}
	for _, target := range slices.Sorted(maps.Keys(replace)) {
		rel, err := filepath.Rel(root, target)
		if err != nil || !filepath.IsLocal(rel) || !strings.HasSuffix(target, ".go") {
			continue
		}
		if slices.ContainsFunc(nested, func(n string) bool { return strings.HasPrefix(target, n+string(filepath.Separator)) }) {
			continue
		}
		if replace[target] == "" {
			continue // removed from the package
		}
		if err := copyFile(replace[target], filepath.Join(dst, rel)); err != nil {
			return err
		}
	}
	return linkReplaced(root, dst, mod)
}

// linkReplaced links, in the copy at dst, each local directory a replace
// directive names by a relative path to the directory it names from root.
func linkReplaced(root, dst string, mod []byte) error {
	f, err := modfile.Parse("go.mod", mod, nil)
	if err != nil {
		return nil // tidy says what is wrong with it
	}
	var rels []string
	for _, r := range f.Replace {
		if modfile.IsDirectoryPath(r.New.Path) && !filepath.IsAbs(r.New.Path) {
			rels = append(rels, filepath.Clean(filepath.FromSlash(r.New.Path)))
		}
	}
	// A directory's link covers those below it.
	sort.Slice(rels, func(i, j int) bool { return len(rels[i]) < len(rels[j]) })
	var linked []string
	for _, rel := range rels {
		at := filepath.Join(dst, rel)
		if slices.ContainsFunc(linked, func(l string) bool { return at == l || strings.HasPrefix(at, l+string(filepath.Separator)) }) {
			continue
		}
		if _, err := os.Lstat(at); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join(root, rel), at); err != nil {
			return err
		}
		linked = append(linked, at)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// pathRewriter writes the go command's output a line at a time, the copy's
// paths given as the module's.
type pathRewriter struct {
	w        io.Writer
	from, to string
	buf      []byte
}

func (p *pathRewriter) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	if i := bytes.LastIndexByte(p.buf, '\n'); i >= 0 {
		p.w.Write(bytes.ReplaceAll(p.buf[:i+1], []byte(p.from), []byte(p.to)))
		p.buf = append(p.buf[:0], p.buf[i+1:]...)
	}
	return len(b), nil
}

func (p *pathRewriter) flush() {
	if len(p.buf) > 0 {
		p.w.Write(bytes.ReplaceAll(p.buf, []byte(p.from), []byte(p.to)))
		p.buf = nil
	}
}
