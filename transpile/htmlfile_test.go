package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// TestGoldenTemplates transpiles each package of testdata/golden/templates —
// .go, .vuka and .vuka.html files — comparing each generated file with
// name.golden (x_html.go.golden) or the errors with errors.err, and runs
// it against out when there is one.
func TestGoldenTemplates(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/golden/templates/*")
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			entries, _ := os.ReadDir(dir)
			var files []transpile.File
			for _, e := range entries {
				n := e.Name()
				if strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html") {
					src, _ := os.ReadFile(filepath.Join(dir, n))
					files = append(files, transpile.File{Name: n, Src: src})
				}
			}
			res, err := transpile.Package(files, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: dir})
			if err != nil {
				compare(t, filepath.Join(dir, "errors.err"), []byte(err.Error()+"\n"))
				return
			}
			run := t.TempDir()
			for _, f := range files {
				if strings.HasSuffix(f.Name, ".go") {
					os.WriteFile(filepath.Join(run, f.Name), f.Src, 0o644)
				}
			}
			for _, o := range res.Files {
				compare(t, filepath.Join(dir, o.GoName+".golden"), o.Src)
				os.WriteFile(filepath.Join(run, o.GoName), o.Src, 0o644)
			}
			want, err := os.ReadFile(filepath.Join(dir, "out"))
			if err != nil || testing.Short() {
				return
			}
			mod, sum := goldenModule(t)
			os.WriteFile(filepath.Join(run, "go.mod"), mod, 0o644)
			os.WriteFile(filepath.Join(run, "go.sum"), sum, 0o644)
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = run
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go run: %v\n%s", err, out)
			}
			if *update {
				os.WriteFile(filepath.Join(dir, "out"), out, 0o644)
			} else if string(out) != string(want) {
				t.Errorf("output:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}
