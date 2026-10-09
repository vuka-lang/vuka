// Package load finds Vuka packages in a module and supplies what transpiling
// them needs from the go command: build-context file selection, export data for
// imports, and the overlay that puts generated files in front of the compiler.
package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Importer reads imported packages' export data from `go list -export`, run by
// the installed go command (through the overlay, so Vuka packages import each
// other).
type Importer struct {
	Dir     string // where go list runs
	Overlay string // overlay JSON file; "" for none

	exports map[string]string
	errs    map[string]string
	gc      types.ImporterFrom
	cache   *exportCache // packages outside the main module, across runs; nil for none
}

func NewImporter(dir, overlay string) *Importer {
	im := &Importer{Dir: dir, Overlay: overlay, exports: map[string]string{}, errs: map[string]string{}}
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

// Prefetch looks up the export data of paths, and everything they depend
// on, with one go command: the imports of all the module's Vuka packages,
// asked once rather than package by package.
func (im *Importer) Prefetch(paths []string) error {
	var missing []string
	for _, p := range paths {
		if _, ok := im.exports[p]; ok || p == "unsafe" || p == "C" {
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
	if len(missing) == 0 {
		return nil
	}
	return im.list(missing...)
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

func (im *Importer) lookup(path string) (io.ReadCloser, error) {
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

// list records export data for paths and everything they depend on.
func (im *Importer) list(paths ...string) error {
	args := []string{"list", "-e", "-export", "-deps", "-json=ImportPath,Export,Error,Standard,Module"}
	if im.Overlay != "" {
		args = append(args, "-overlay="+im.Overlay)
	}
	cmd := exec.Command("go", append(args, paths...)...)
	cmd.Dir = im.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list %s: %v\n%s", strings.Join(paths, " "), err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath, Export string
			Standard           bool
			Module             *struct{ Main bool }
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); err != nil {
			return err
		}
		im.exports[p.ImportPath] = p.Export
		if p.Error != nil {
			im.errs[p.ImportPath] = p.Error.Err
		} else if im.cache != nil && p.Export != "" && (p.Standard || p.Module != nil && !p.Module.Main) {
			im.cache.put(p.ImportPath, p.Export)
		}
	}
	for _, path := range paths {
		if _, ok := im.exports[path]; !ok {
			im.exports[path] = ""
		}
	}
	return nil
}
