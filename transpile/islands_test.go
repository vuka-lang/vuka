package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A wrong value for an imported island's prop is Go's type error, at the
// attribute in the .vuka file.
func TestIslandPropTypeErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the program")
	}
	dir := "testdata/golden"
	src, err := os.ReadFile(filepath.Join(dir, "islands_typeerr.vuka"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := transpile.Package([]transpile.File{{Name: "islands_typeerr.vuka", Src: src}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	mod, sum := goldenModule(t)
	os.WriteFile(filepath.Join(tmp, "go.mod"), mod, 0o644)
	os.WriteFile(filepath.Join(tmp, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(tmp, "main.go"), append(res.Files[0].Src, "\nfunc main() {}\n"...), 0o644)
	cmd := exec.Command("go", "build", "-gcflags=-e", "-o", os.DevNull, ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, _ := cmd.CombinedOutput()
	for _, want := range []string{
		`islands_typeerr.vuka:9:`, `cannot use 3 (untyped int constant) as string value`,
		`islands_typeerr.vuka:10:`, `cannot use "today" (untyped string constant) as "time".Time value`,
		`islands_typeerr.vuka:11:`, `cannot use "3" (untyped string constant) as float64 value`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("go build doesn't report %s:\n%s", want, out)
		}
	}
}
