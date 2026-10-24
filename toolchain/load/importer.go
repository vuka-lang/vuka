// Package load finds Vuka packages in a module and supplies what transpiling
// them needs from the go command: build-context file selection, export data for
// imports, and the overlay that puts generated files in front of the compiler.
//
// It is for the vuka command's use only; the compatibility promise doesn't
// cover it.
package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// Importer reads imported packages' export data from `go list -export`, run by
// the installed go command (through the overlay, so Vuka packages import each
// other).
type Importer struct {
	Dir     string // where go list runs
	Overlay string // overlay JSON file; "" for none

	exports map[string]string
	errs    map[string]string
	mods    map[string]*goModule // import path → its module, as go list reported it
	stale   map[string]bool      // main-module packages that failed: listed again once the overlay changes
	broken  map[string]bool      // Vuka packages that didn't transpile
	gc      types.ImporterFrom
	cache   *exportCache // packages outside the main module, across runs; nil for none
	// volatile, when not nil, records the export data file of each listed
	// package the cache doesn't hold: those that may change (refresh).
	volatile map[string]string
}

func NewImporter(dir, overlay string) *Importer {
	im := &Importer{Dir: dir, Overlay: overlay, exports: map[string]string{}, errs: map[string]string{},
		mods: map[string]*goModule{}, stale: map[string]bool{}, broken: map[string]bool{}}
	im.gc = importer.ForCompiler(token.NewFileSet(), "gc", im.lookup).(types.ImporterFrom)
	return im
}

// newCachedImporter is an importer for the module at root that remembers the
// export data of the standard library and dependencies across runs.
func newCachedImporter(root, overlay string) *Importer {
	im := NewImporter(root, overlay)
	im.cache = cacheFor(root)
	return im
}

// NewModuleImporter is an importer for the module at root, without an
// overlay, that remembers the standard library's and dependencies' export
// data across runs and processes.
func NewModuleImporter(root string) *Importer { return newCachedImporter(root, "") }

// Prefetch looks up the export data of paths, and everything they depend
// on, with one go command: the imports of all the module's Vuka packages,
// asked once rather than package by package.
func (im *Importer) Prefetch(paths []string) error { return im.prefetch(paths, nil) }

// prefetch is Prefetch, asking the go command about more too when it asks
// about any of paths: what the run may look up later, which costs nothing
// more in the same go command and a go command each when asked one by one.
func (im *Importer) prefetch(paths, more []string) error {
	missing := im.missing(paths)
	if len(missing) == 0 {
		return nil
	}
	return im.list(append(missing, im.missing(more)...)...)
}

// missing are the paths whose export data the importer hasn't.
func (im *Importer) missing(paths []string) []string {
	var missing []string
	for _, p := range paths {
		if _, ok := im.exports[p]; ok || p == "unsafe" || p == "C" || build.IsLocalImport(p) {
			continue
		}
		if im.cache != nil {
			if file, ok := im.cache.get(p); ok {
				im.exports[p] = file
				continue
			}
		}
		missing = append(missing, p)
	}
	return missing
}

func (im *Importer) Import(path string) (*types.Package, error) {
	return im.ImportFrom(path, im.Dir, 0)
}

func (im *Importer) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	return im.gc.ImportFrom(path, dir, mode)
}

// Broken records that the Vuka package path didn't transpile, so importing
// it points at its own errors.
func (im *Importer) Broken(path string) { im.broken[path] = true }

func (im *Importer) lookup(path string) (io.ReadCloser, error) {
	if im.broken[path] {
		return nil, fmt.Errorf("%s has errors", path)
	}
	if build.IsLocalImport(path) {
		// A component file (../web/islands/Chart.vue), which no go
		// command need be asked about.
		return nil, fmt.Errorf("%s is not a package", path)
	}
	if _, ok := im.exports[path]; !ok && im.cache != nil {
		if file, ok := im.cache.get(path); ok {
			im.exports[path] = file
		}
	}
	if _, ok := im.exports[path]; !ok {
		if err := im.list(path); err != nil {
			return nil, err
		}
	}
	if msg := im.errs[path]; msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	file := im.exports[path]
	if file == "" {
		return nil, fmt.Errorf("no export data for %q", path)
	}
	return os.Open(file)
}

// refresh lists again the packages whose export data may have changed since
// they were listed (volatile), and reports whether all of them are as they
// were: the same export data, or, for one that had none, none still, for the
// same reason (an import the go command can't resolve, a dependency that
// doesn't compile). Types already imported stay valid only when they are.
func (im *Importer) refresh() bool {
	if len(im.volatile) == 0 {
		return true
	}
	old := make(map[string]string, len(im.volatile))
	oldErrs := make(map[string]string, len(im.volatile))
	paths := make([]string, 0, len(im.volatile))
	for path, file := range im.volatile {
		old[path], oldErrs[path] = file, im.errs[path]
		paths = append(paths, path)
		delete(im.exports, path)
		delete(im.errs, path)
	}
	if err := im.list(paths...); err != nil {
		return false
	}
	for path, file := range old {
		if im.exports[path] != file || file == "" && im.errs[path] != oldErrs[path] {
			return false
		}
	}
	return true
}

// OverlayChanged tells the importer the overlay has new files: what failed
// in the main module may compile now.
func (im *Importer) OverlayChanged() {
	for path := range im.stale {
		delete(im.exports, path)
		delete(im.errs, path)
	}
	clear(im.stale)
}

// goLists counts the go list commands importers run, for tests.
var goLists atomic.Int64

type goModule struct {
	Path, Version string
	Main          bool
	Replace       *struct{ Version string } // no version: a directory, which may change
}

type goError struct {
	ImportStack []string
	Pos, Err    string
}

// list records export data for paths and everything they depend on. With
// -e, packages that compile get their export data even when others don't.
func (im *Importer) list(paths ...string) error {
	goLists.Add(1)
	args := []string{"list", "-e", "-export", "-deps", "-json=ImportPath,Export,Error,DepsErrors,Standard,Module"}
	if im.Overlay != "" {
		args = append(args, "-overlay="+im.Overlay)
	}
	cmd := exec.Command("go", append(args, paths...)...)
	cmd.Dir = im.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return fmt.Errorf("go list %s: %v\n%s", strings.Join(paths, " "), err, strings.TrimSpace(stderr.String()))
	}
	// Older go commands print compile errors to stderr instead of Error.
	compile := compileErrors(stderr.String())
	type pkg struct {
		ImportPath, Export string
		Standard           bool
		Module             *goModule
		Error              *goError
		DepsErrors         []*goError
	}
	var failed []*pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		p := new(pkg)
		if err := dec.Decode(p); err != nil {
			return err
		}
		im.exports[p.ImportPath] = p.Export
		im.mods[p.ImportPath] = p.Module
		if p.Error == nil && p.Export == "" && compile[p.ImportPath] != "" {
			p.Error = &goError{Err: compile[p.ImportPath]}
		}
		switch {
		case p.Error != nil || len(p.DepsErrors) > 0 || p.Export == "":
			failed = append(failed, p)
			if p.Module != nil && p.Module.Main {
				im.stale[p.ImportPath] = true
			}
		case im.cache != nil && (p.Standard || p.Module != nil && !p.Module.Main && (p.Module.Replace == nil || p.Module.Replace.Version != "")):
			im.cache.put(p.ImportPath, p.Export)
			continue
		}
		if im.volatile != nil && !p.Standard {
			im.volatile[p.ImportPath] = p.Export
		}
	}
	for _, p := range failed {
		if msg := im.failure(p.ImportPath, p.Error, p.DepsErrors); msg != "" {
			im.errs[p.ImportPath] = msg
		}
	}
	for _, path := range paths {
		if _, ok := im.exports[path]; !ok {
			im.exports[path] = ""
		}
	}
	return nil
}

// failure explains why path has no export data: its own compile error, or
// the dependency whose error it inherits. "" leaves the generic message.
func (im *Importer) failure(path string, own *goError, deps []*goError) string {
	e := own
	if e == nil {
		if len(deps) == 0 {
			return ""
		}
		e = deps[0]
	}
	culprit, detail, compiled := im.explain(e)
	if !compiled && e == own {
		return e.Err // what the go command says about the import itself
	}
	if culprit == "" {
		culprit = path
	}
	msg := path + " doesn't compile: " + detail
	if culprit != path {
		msg = path + " depends on " + culprit + ", which doesn't compile: " + detail
	}
	if hint := im.oldVukaHint(culprit, detail); hint != "" {
		msg += "; " + hint
	}
	return msg
}

// explain reads one go list error: the package at fault, its first error
// with an absolute position (and how many more there are), and whether it is
// a compile error.
func (im *Importer) explain(e *goError) (culprit, detail string, compiled bool) {
	if rest, ok := strings.CutPrefix(e.Err, "# "); ok {
		head, body, _ := strings.Cut(rest, "\n")
		culprit, _, _ = strings.Cut(head, " ")
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(body), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "note: ") {
				lines = append(lines, im.absPos(l))
			}
		}
		if len(lines) == 0 {
			return culprit, strings.TrimSpace(body), true
		}
		detail = lines[0]
		if n := len(lines) - 1; n > 0 && !strings.HasSuffix(lines[n], "too many errors") {
			detail += fmt.Sprintf(" (and %d more)", n)
		} else if n > 0 {
			detail += " (and more)"
		}
		return culprit, detail, true
	}
	if n := len(e.ImportStack); n > 0 {
		culprit = e.ImportStack[n-1]
	}
	detail = e.Err
	if e.Pos != "" {
		detail = im.absPos(e.Pos) + ": " + e.Err
	}
	return culprit, detail, false
}

// absPos makes the file of a "file:line:col: …" line absolute; the go
// command writes it relative to where it ran.
func (im *Importer) absPos(line string) string {
	file, rest, ok := strings.Cut(line, ":")
	if !ok || filepath.IsAbs(file) || !strings.HasSuffix(file, ".go") {
		return line
	}
	return filepath.Join(im.Dir, file) + ":" + rest
}

// oldVukaAPI matches a compile error naming runtime API that isn't there.
var oldVukaAPI = regexp.MustCompile(`undefined: vuka\.\w|\(type \*?vuka\.\w+ has no field or method|package github\.com/vuka-lang/vuka/\S+`)

// oldVukaHint suggests the way out when culprit's error is code written
// against a runtime API this module's Vuka no longer has.
func (im *Importer) oldVukaHint(culprit, detail string) string {
	if !oldVukaAPI.MatchString(detail) {
		return ""
	}
	switch m := im.mods[culprit]; {
	case m != nil && m.Main:
		return "it was written for an older Vuka; run vuka fix ui"
	case m != nil && m.Version != "":
		return fmt.Sprintf("%s %s was built for an older Vuka; upgrade it (go get %s@latest)", transpile.ImportName(m.Path), m.Version, m.Path)
	case m != nil:
		return m.Path + " was built for an older Vuka; update it"
	}
	return "it is a dependency built for an older Vuka runtime; upgrade it"
}

// compileErrors splits the go command's stderr into each package's "# pkg"
// block of compiler errors.
func compileErrors(stderr string) map[string]string {
	m := map[string]string{}
	for _, block := range strings.Split("\n"+stderr, "\n# ")[1:] {
		head, _, _ := strings.Cut(block, "\n")
		path, _, _ := strings.Cut(head, " ")
		m[path] = "# " + strings.TrimRight(block, "\n") + "\n"
	}
	return m
}
