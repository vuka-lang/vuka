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
}

func NewImporter(dir, overlay string) *Importer {
	im := &Importer{Dir: dir, Overlay: overlay, exports: map[string]string{}, errs: map[string]string{}}
	im.gc = importer.ForCompiler(token.NewFileSet(), "gc", im.lookup).(types.ImporterFrom)
	return im
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

// list records export data for path and everything it depends on.
func (im *Importer) list(path string) error {
	args := []string{"list", "-e", "-export", "-deps", "-json=ImportPath,Export,Error"}
	if im.Overlay != "" {
		args = append(args, "-overlay="+im.Overlay)
	}
	cmd := exec.Command("go", append(args, path)...)
	cmd.Dir = im.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list %s: %v\n%s", path, err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath, Export string
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); err != nil {
			return err
		}
		im.exports[p.ImportPath] = p.Export
		if p.Error != nil {
			im.errs[p.ImportPath] = p.Error.Err
		}
	}
	if _, ok := im.exports[path]; !ok {
		im.exports[path] = ""
	}
	return nil
}
