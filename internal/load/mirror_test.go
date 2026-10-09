package load

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMirror mirrors a module mixing .vuka and .go files, an embedded asset,
// testdata, a nested module and a hidden directory, then builds and tests the
// result with plain go.
func TestMirror(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	// The module is written by hand; let go fill in what the runtime requires.
	t.Setenv("GOFLAGS", "-mod=mod")
	repo, _ := filepath.Abs("../..")
	root := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\n"+
		"replace github.com/vuka-lang/vuka => "+repo+"\n\nreplace example.com/lib => ../lib\n")
	write(t, filepath.Join(root, "main.vuka"), `package main

import (
	"fmt"

	"example.com/app/shapes"
)

func parse(s string) Result[int] {
	if s == "" {
		return Err(fmt.Errorf("empty"))
	}
	return Ok(len(s))
}

func main() {
	match parse(greeting) {
	case Ok(n):
		fmt.Println(greeting, n, shapes.Describe())
	case Err(e):
		fmt.Println(e)
	}
}
`)
	write(t, filepath.Join(root, "embed.go"), "package main\n\nimport _ \"embed\"\n\n//go:embed hello.txt\nvar greeting string\n")
	write(t, filepath.Join(root, "hello.txt"), "hi")
	write(t, filepath.Join(root, "shapes", "area.vuka"), `package shapes

import "fmt"

func area(w int) int       { return w * w }
func area(w, h int) int    { return w * h }

func Describe() string { return fmt.Sprint(area(2), area(2, 3), helper()) }
`)
	write(t, filepath.Join(root, "shapes", "helper.go"), "package shapes\n\nfunc helper() string { return \"ok\" }\n")
	write(t, filepath.Join(root, "shapes", "area_test.go"), `package shapes

import (
	"os"
	"testing"
)

func TestDescribe(t *testing.T) {
	data, _ := os.ReadFile("testdata/want.txt")
	if got := Describe(); got != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}
`)
	write(t, filepath.Join(root, "shapes", "testdata", "want.txt"), "4 6ok")
	write(t, filepath.Join(root, "tools", "go.mod"), "module example.com/tools\n")
	write(t, filepath.Join(root, "tools", "x.go"), "package tools\n")
	write(t, filepath.Join(root, ".cache", "junk.go"), "junk")
	out := filepath.Join(root, "build")
	write(t, filepath.Join(out, Marker), "")
	write(t, filepath.Join(out, "stale.go"), "package main\n")

	mirror := func() MirrorResult {
		t.Helper()
		pkgs, err := Discover(root, "example.com/app", root, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		gens, _, err := Transpile(pkgs, t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		res, err := Mirror(root, out, gens, false)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := mirror()
	if strings.Join(res.Removed, ",") != "stale.go" {
		t.Errorf("removed %v, want stale.go", res.Removed)
	}
	for _, missing := range []string{"main.vuka", "shapes/area.vuka", "tools/x.go", ".cache/junk.go"} {
		if _, err := os.Stat(filepath.Join(out, missing)); err == nil {
			t.Errorf("%s was mirrored", missing)
		}
	}
	mod, _ := os.ReadFile(filepath.Join(out, "go.mod"))
	if !strings.Contains(string(mod), "example.com/lib => ../../lib") {
		t.Errorf("relative replace not adjusted:\n%s", mod)
	}

	goCmd := func(args ...string) string {
		t.Helper()
		c := exec.Command("go", args...)
		c.Dir = out
		c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s in build/: %v\n%s", strings.Join(args, " "), err, b)
		}
		return string(b)
	}
	if got := goCmd("run", "."); got != "hi 2 4 6ok\n" {
		t.Errorf("go run: %q", got)
	}
	goCmd("test", "./...")
	goCmd("vet", "./...")

	if res := mirror(); len(res.Written)+len(res.Removed) != 0 {
		t.Errorf("second mirror changed %v %v", res.Written, res.Removed)
	}

	// A directory vuka didn't make is never synced over.
	other := filepath.Join(root, "dist")
	write(t, filepath.Join(other, "keep.go"), "package dist\n")
	if _, err := Mirror(root, other, nil, false); err == nil {
		t.Error("mirrored over a directory without the marker")
	}
}
