package transpile_test

import (
	"go/build"
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
		limit = 60
	}
	pkgs, files := 0, 0
	filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
		if err != nil || limit >= 0 && pkgs >= limit {
			return filepath.SkipAll
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == "testdata" {
			return filepath.SkipDir
		}
		// A package as the build sees it: the files matching this platform,
		// grouped by package clause.
		entries, _ := os.ReadDir(dir)
		byPkg := map[string][]transpile.File{}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") {
				continue
			}
			if ok, _ := build.Default.MatchFile(dir, name); !ok {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.PackageClauseOnly)
			if err != nil {
				continue
			}
			byPkg[f.Name.Name] = append(byPkg[f.Name.Name], transpile.File{Name: strings.TrimSuffix(name, ".go") + ".vuka", Src: src})
		}
		for _, list := range byPkg {
			pkgs++
			files += len(list)
			res, err := transpile.Package(list, transpile.Options{Bare: true})
			if err != nil {
				t.Errorf("%s: %v", dir, err)
				continue
			}
			for i, f := range res.Files {
				if !bytes.Equal(f.Src, list[i].Src) {
					t.Errorf("%s: output differs from input", filepath.Join(dir, f.Name))
				}
			}
		}
		return nil
	})
	t.Logf("%d packages, %d files", pkgs, files)
}
