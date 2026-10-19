package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// TestSplit splits each golden case that runs into templates and the rest,
// as vuka eject does: the .vuka.html is name.vuka.html.golden, and the two
// files together compile to a program printing the case's output.
func TestSplit(t *testing.T) {
	cases, _ := filepath.Glob("testdata/golden/*.out")
	for _, out := range cases {
		name := strings.TrimSuffix(filepath.Base(out), ".out")
		t.Run(name, func(t *testing.T) {
			dir := "testdata/golden"
			src, err := os.ReadFile(filepath.Join(dir, name+".vuka"))
			if err != nil {
				t.Skip("not a .vuka case")
			}
			opts := transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir}
			split := opts
			split.Split = true
			res, err := transpile.Package([]transpile.File{{Name: name + ".vuka", Src: src}}, split)
			if err != nil {
				t.Fatal(err)
			}
			s := res.Files[0].Split
			golden := filepath.Join(dir, "split", name+".vuka.html.golden")
			if s == nil {
				if _, err := os.Stat(golden); err == nil && !*update {
					t.Fatalf("%s moves no template now", name)
				}
				return
			}
			compare(t, golden, s.HTML)
			files := []transpile.File{{Name: name + ".vuka", Src: s.Vuka}, {Name: name + ".vuka.html", Src: s.HTML}}
			res, err = transpile.Package(files, opts)
			if err != nil {
				t.Fatalf("the split doesn't compile:\n%v\n--- %s.vuka\n%s\n--- %s.vuka.html\n%s", err, name, s.Vuka, name, s.HTML)
			}
			if testing.Short() {
				return
			}
			run := t.TempDir()
			mod, sum := goldenModule(t)
			os.WriteFile(filepath.Join(run, "go.mod"), mod, 0o644)
			os.WriteFile(filepath.Join(run, "go.sum"), sum, 0o644)
			os.CopyFS(filepath.Join(run, "assets"), os.DirFS("testdata/golden/assets"))
			for _, o := range res.Files {
				os.WriteFile(filepath.Join(run, o.GoName), o.Src, 0o644)
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = run
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			got, err := cmd.CombinedOutput()
			want, _ := os.ReadFile(out)
			// State and component sites are lines of the file the code is in.
			site := regexp.MustCompile(`[\w]+\.vuka(\.html)?:\d+`)
			if err != nil || site.ReplaceAllString(string(got), "site") != site.ReplaceAllString(string(want), "site") {
				t.Errorf("the split prints\n%s%v\nwant\n%s", got, err, want)
			}
		})
	}
}
