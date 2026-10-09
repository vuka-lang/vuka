package transpile_test

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden transpiles each testdata/golden/*.vuka and compares the Go it
// produces with name.golden, or its errors with name.err. A case with a
// name.out file is also compiled and run, and its output compared.
func TestGolden(t *testing.T) {
	cases, _ := filepath.Glob("testdata/golden/*.vuka")
	if len(cases) == 0 {
		t.Fatal("no golden cases")
	}
	for _, path := range cases {
		name := strings.TrimSuffix(filepath.Base(path), ".vuka")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(path)
			res, err := transpile.Package([]transpile.File{{Name: name + ".vuka", Src: src}},
				transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
			if err != nil {
				if !strings.HasPrefix(name, "err_") {
					t.Fatalf("only err_ cases may fail:\n%v", err)
				}
				compare(t, filepath.Join(dir, name+".err"), []byte(err.Error()+"\n"))
				return
			}
			if strings.HasPrefix(name, "err_") {
				t.Fatal("an err_ case transpiled without errors")
			}
			out := res.Files[0].Src
			compare(t, filepath.Join(dir, name+".golden"), out)

			want, err := os.ReadFile(filepath.Join(dir, name+".out"))
			if err != nil {
				return
			}
			if got := run(t, out); got != string(want) {
				t.Errorf("output:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func compare(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)\ngot:\n%s", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func run(t *testing.T, src []byte) string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles and runs the program")
	}
	dir := t.TempDir()
	root, _ := filepath.Abs("..")
	mod := "module golden\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/a-h/templ v0.3.1020 // indirect\n\tgithub.com/vuka-lang/vuka v0.0.0\n)\n\nreplace github.com/vuka-lang/vuka => " + root + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644)
	// Files a case embeds (vuka.File) are in testdata/golden/assets.
	os.CopyFS(filepath.Join(dir, "assets"), os.DirFS("testdata/golden/assets"))
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}
