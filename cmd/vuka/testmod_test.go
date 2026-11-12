package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// testModule is a test module's go.mod and go.sum: it requires the local
// vuka (this repository), the local github.com/vuka-lang/ui/v2 (a sibling
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
		t.Fatalf("the tests need github.com/vuka-lang/ui/v2 checked out at %s (or $VUKA_UI): %v", ui, err)
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
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "github.com/vuka-lang/vuka/v2" && semverLess(runtime, f[1]) {
			runtime = f[1]
		}
	}
	// ui's own Go version and every module it requires (orm and what orm
	// needs among them): the test module must satisfy them.
	goVersion, extra := "1.25.0", ""
	inRequire := false
	for _, line := range strings.Split(string(uiMod), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "go" && semverLess("v"+goVersion, "v"+f[1]):
			goVersion = f[1]
		case len(f) == 2 && f[0] == "require" && f[1] == "(":
			inRequire = true
		case len(f) == 1 && f[0] == ")":
			inRequire = false
		case inRequire && len(f) >= 2 && f[0] != "github.com/vuka-lang/vuka/v2" && f[0] != "github.com/vuka-lang/ui/v2" && !strings.HasPrefix(f[0], "github.com/a-h/"):
			extra += "\n\t" + f[0] + " " + f[1]
		}
	}
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	sum := append(uiSum, ownSum...)
	return "module " + path + "\n\ngo " + goVersion + "\n\nrequire (\n\t" + strings.Join(templ, "\n\t") +
		"\n\tgithub.com/vuka-lang/ui/v2 " + familyVersion(t, "ui") + "\n\tgithub.com/vuka-lang/vuka/v2 " + runtime + extra + "\n)\n\n" +
		"replace github.com/vuka-lang/vuka/v2 => " + repo + "\n\nreplace github.com/vuka-lang/ui/v2 => " + ui + "\n", string(sum)
}

// ormFamily is the versions of the vuka family (ui, vuka, web) that the orm
// the ui checkout requires itself requires: the test module's own
// requirements, though replaced by local checkouts, must not be lower, or
// go would have to rewrite its go.mod.
func ormFamily(t *testing.T, ormVersion string) map[string]string {
	t.Helper()
	fam := map[string]string{}
	for _, line := range strings.Split(ormGoMod(t, ormVersion), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[0], "github.com/vuka-lang/") {
			fam[f[0]] = f[1]
		}
	}
	return fam
}

// ormGoMod is the go.mod of the orm version in the module cache, empty when
// it isn't there.
func ormGoMod(t *testing.T, ormVersion string) string {
	t.Helper()
	if ormVersion == "" {
		return ""
	}
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	mod, _ := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "cache", "download", "github.com", "vuka-lang", "orm", "v2", "@v", ormVersion+".mod"))
	return string(mod)
}

// uiORMVersion is the orm version the ui checkout requires.
func uiORMVersion(uiMod string) string {
	for _, line := range strings.Split(uiMod, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "github.com/vuka-lang/orm/v2" {
			return f[1]
		}
	}
	return ""
}

// familyVersion is the version a test module requires a vuka family module
// at: v2.0.0, or what the orm the ui checkout requires needs.
func familyVersion(t *testing.T, module string) string {
	t.Helper()
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		repo, _ := filepath.Abs("../..")
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	uiMod, _ := os.ReadFile(filepath.Join(ui, "go.mod"))
	orm := ""
	for _, line := range strings.Split(string(uiMod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "github.com/vuka-lang/orm/v2" {
			orm = f[1]
		}
	}
	if v := ormFamily(t, orm)["github.com/vuka-lang/"+module+"/v2"]; v != "" {
		return v
	}
	return "v2.0.0"
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
