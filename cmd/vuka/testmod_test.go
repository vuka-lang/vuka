package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// testModule is a test module's go.mod and go.sum: it requires the local
// vuka (this repository), the local github.com/vuka-lang/ui (a sibling
// checkout, ../vuka-ui, or $VUKA_UI) and templ at the version this command
// builds with, with checksums go can trust offline.
func testModule(t *testing.T, path string) (gomod, gosum string) {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if _, err := os.Stat(filepath.Join(ui, "go.mod")); err != nil {
		t.Fatalf("the tests need github.com/vuka-lang/ui checked out at %s (or $VUKA_UI): %v", ui, err)
	}
	mod, _ := os.ReadFile("go.mod")
	var templ []string // templ and its requirements, as this command requires them
	for _, line := range strings.Split(string(mod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && strings.HasPrefix(f[0], "github.com/a-h/") {
			templ = append(templ, f[0]+" "+f[1])
		}
	}
	// The runtime ui requires, when newer than the generated code's: the
	// module's requirement must be the version the build selects.
	runtime := transpile.RuntimeVersion
	uiMod, _ := os.ReadFile(filepath.Join(ui, "go.mod"))
	for _, line := range strings.Split(string(uiMod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "github.com/vuka-lang/vuka" && semverLess(runtime, f[1]) {
			runtime = f[1]
		}
	}
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	sum := append(uiSum, ownSum...)
	return "module " + path + "\n\ngo 1.25.0\n\nrequire (\n\t" + strings.Join(templ, "\n\t") +
		"\n\tgithub.com/vuka-lang/ui v0.0.0\n\tgithub.com/vuka-lang/vuka " + runtime + "\n)\n\n" +
		"replace github.com/vuka-lang/vuka => " + repo + "\n\nreplace github.com/vuka-lang/ui => " + ui + "\n", string(sum)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// semverLess compares vMAJOR.MINOR.PATCH[-pre] versions, a pre-release
// before its release.
func semverLess(a, b string) bool {
	num := func(v string) ([3]int, string) {
		var n [3]int
		core, pre, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
		for i, p := range strings.SplitN(core, ".", 3) {
			fmt.Sscan(p, &n[i])
		}
		return n, pre
	}
	x, xp := num(a)
	y, yp := num(b)
	if x != y {
		for i := range x {
			if x[i] != y[i] {
				return x[i] < y[i]
			}
		}
	}
	return xp != "" && (yp == "" || xp < yp)
}
