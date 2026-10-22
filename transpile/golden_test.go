package transpile_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/diag"
	"github.com/vuka-lang/vuka/docs"
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
			if strings.HasPrefix(name, "err_markup") {
				// What an app's .vuka file is held to: no markup.
				defer transpile.SetJSXInVuka(transpile.SetJSXInVuka(false))
			}
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
				var list transpile.ErrorList
				if errors.As(err, &list) {
					var b bytes.Buffer
					diag.Render(&b, docs.Fill(list.Diagnostics()), diag.Options{Source: func(string) ([]byte, bool) { return src, true }})
					compare(t, filepath.Join(dir, name+".diag"), b.Bytes())
				}
				return
			}
			if strings.HasPrefix(name, "err_") {
				t.Fatal("an err_ case transpiled without errors")
			}
			out := res.Files[0].Src
			compare(t, filepath.Join(dir, name+".golden"), out)
			callsPath := filepath.Join(dir, name+".calls.golden")
			if res.Calls != nil {
				compare(t, callsPath, res.Calls)
			} else if _, err := os.Stat(callsPath); err == nil {
				t.Errorf("%s: no call adapters any more: remove it", callsPath)
			}

			outPath := filepath.Join(dir, name+".out")
			want, err := os.ReadFile(outPath)
			if err != nil {
				return
			}
			if res.Calls != nil {
				// The adapters are a speed-up: the program says the same
				// through reflection.
				if got := run(t, out, res.Calls); got != string(want) {
					t.Errorf("output with call adapters:\n%s\nwant:\n%s", got, want)
				}
			}
			got := run(t, out)
			if *update {
				if got != string(want) {
					os.WriteFile(outPath, []byte(got), 0o644)
				}
				return
			}
			if got != string(want) {
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

func run(t *testing.T, src []byte, calls ...[]byte) string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles and runs the program")
	}
	dir := t.TempDir()
	mod, sum := goldenModule(t)
	os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644)
	for _, c := range calls {
		os.WriteFile(filepath.Join(dir, transpile.CallsGoName), c, 0o644)
	}
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

// goldenModule is testdata/golden's go.mod, its replaced paths made
// absolute, and its go.sum.
func goldenModule(t *testing.T) (mod, sum []byte) {
	t.Helper()
	data, err := os.ReadFile("testdata/golden/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 4 && f[0] == "replace" && f[2] == "=>" {
			p, _ := filepath.Abs(filepath.Join("testdata/golden", f[3]))
			line = "replace " + f[1] + " => " + p + "\n"
		}
		b.WriteString(line)
	}
	sum, _ = os.ReadFile("testdata/golden/go.sum")
	return []byte(b.String()), sum
}

// TestSyntax checks Syntax against Package on the golden cases: a case it
// finds errors in is one Package reports exactly those for, and a case that
// transpiles has none.
func TestSyntax(t *testing.T) {
	cases, _ := filepath.Glob("testdata/golden/*.vuka")
	found := 0
	for _, path := range cases {
		name := strings.TrimSuffix(filepath.Base(path), ".vuka")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(path)
		errs := transpile.Syntax(transpile.File{Name: name + ".vuka", Src: src}, transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
		if !strings.HasPrefix(name, "err_") {
			if len(errs) > 0 {
				t.Errorf("%s transpiles, but Syntax reports %v", name, errs)
			}
			continue
		}
		if len(errs) == 0 {
			continue
		}
		found++
		want, _ := os.ReadFile(filepath.Join(dir, name+".err"))
		if got := errs.Error() + "\n"; got != string(want) {
			t.Errorf("%s: Syntax reports\n%s\nPackage\n%s", name, got, want)
		}
	}
	if found == 0 {
		t.Error("Syntax found no errors in the err_ cases")
	}
	t.Logf("%d err_ cases are syntax errors", found)
}
