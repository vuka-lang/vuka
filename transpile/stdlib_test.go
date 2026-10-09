package transpile_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// TestStdlibPassesThrough feeds every Go file of the installed Go's source tree
// through Vuka as if it were Vuka source. Each must come out byte-identical: Go
// is a subset of Vuka, and new Go syntax must never be touched. Running this on
// a new Go release is how Vuka finds out it has to change.
func TestStdlibPassesThrough(t *testing.T) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(strings.TrimSpace(string(out)), "src")
	limit := -1
	if testing.Short() {
		limit = 500
	}
	n := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || limit >= 0 && n >= limit {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution); err != nil {
			return nil // not valid Go to begin with
		}
		n++
		name := strings.TrimSuffix(filepath.Base(path), ".go") + ".vuka"
		res, err := transpile.Package([]transpile.File{{Name: name, Src: src}}, transpile.Options{Bare: true})
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		if got := res.Files[0].Src; !bytes.Equal(got, src) {
			t.Errorf("%s: output differs from input", path)
		}
		return nil
	})
	t.Logf("%d files", n)
}
