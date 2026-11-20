package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGenOverlayOut: vuka gen -overlay-out writes the same generated Go the
// mirror holds, as shadow files named by the source tree's paths, and a
// build with the overlay compiles at the module's own path.
func TestGenOverlayOut(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	src, err := os.ReadFile("../../examples/users/main.vuka")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "users", "main.vuka"), string(src))
	t.Chdir(root)

	if err := gen([]string{"-o", "mirror", "-code-only"}, io.Discard); err != nil {
		t.Fatalf("gen -o: %v", err)
	}
	ovFile := filepath.Join(t.TempDir(), "overlay.json")
	var out bytes.Buffer
	if err := gen([]string{"-code-only", "-overlay-out", ovFile}, &out); err != nil {
		t.Fatalf("gen -overlay-out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "build")); err == nil {
		t.Errorf("-overlay-out mirrored the module")
	}
	raw, err := os.ReadFile(ovFile)
	if err != nil {
		t.Fatal(err)
	}
	var ov struct{ Replace map[string]string }
	if err := json.Unmarshal(raw, &ov); err != nil || len(ov.Replace) == 0 {
		t.Fatalf("overlay %s: %v", raw, err)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	for target, shadow := range ov.Replace {
		rel, err := filepath.Rel(realRoot, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel, err = filepath.Rel(root, target)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("target %s is outside the module %s", target, root)
				continue
			}
		}
		want, err := os.ReadFile(filepath.Join(root, "mirror", rel))
		if err != nil {
			t.Errorf("mirror has no %s: %v", rel, err)
			continue
		}
		got, _ := os.ReadFile(shadow)
		// The mirror's //line directives are relative to itself; the overlay's
		// are the source tree's own paths.
		if lines(got) != lines(want) {
			t.Errorf("%s: overlay and mirror differ in length", rel)
		}
		if stripLines(got) != stripLines(want) {
			t.Errorf("%s: overlay and mirror differ beyond //line directives", rel)
		}
	}
	c := exec.Command("go", "build", "-overlay", ovFile, "-o", filepath.Join(t.TempDir(), "app"), "./users")
	c.Dir = realRoot
	if b, err := c.CombinedOutput(); err != nil {
		t.Errorf("go build -overlay: %v\n%s", err, b)
	}
}

func lines(b []byte) int { return bytes.Count(b, []byte("\n")) }

func stripLines(b []byte) string {
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "//line ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// TestGenOverlayOutNarrowed: packages named after -overlay-out make the
// overlay of those and what they import in the module, through Go packages
// too; one reaching a package main is the whole module.
func TestGenOverlayOutNarrowed(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	writeFile(t, filepath.Join(root, "b", "b.vuka"), "package b\n\nfunc B() int { return 1 }\n")
	writeFile(t, filepath.Join(root, "a", "a.vuka"), "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n")
	writeFile(t, filepath.Join(root, "c", "c.vuka"), "package c\n\nfunc C() int { return 3 }\n")
	writeFile(t, filepath.Join(root, "g", "g.go"), "package g\n\nimport \"example.com/app/b\"\n\nfunc G() int { return b.B() }\n")
	writeFile(t, filepath.Join(root, "app", "main.vuka"), "package main\n\nimport (\n\t\"example.com/app/a\"\n\t\"example.com/app/c\"\n)\n\nfunc main() { println(a.A() + c.C()) }\n")
	t.Chdir(root)

	dirs := func(args ...string) (string, []string) {
		t.Helper()
		ovFile := filepath.Join(t.TempDir(), "overlay.json")
		var out bytes.Buffer
		if err := gen(append([]string{"-code-only", "-overlay-out", ovFile}, args...), &out); err != nil {
			t.Fatalf("gen %v: %v", args, err)
		}
		raw, err := os.ReadFile(ovFile)
		if err != nil {
			t.Fatal(err)
		}
		var ov struct{ Replace map[string]string }
		if err := json.Unmarshal(raw, &ov); err != nil {
			t.Fatal(err)
		}
		var got []string
		for target := range ov.Replace {
			d := filepath.Base(filepath.Dir(target))
			if !slices.Contains(got, d) {
				got = append(got, d)
			}
		}
		slices.Sort(got)
		return out.String(), got
	}
	for _, c := range []struct {
		args     []string
		want     []string
		narrowed bool
	}{
		{[]string{"./a"}, []string{"a", "b"}, true},
		{[]string{"./b"}, []string{"b"}, true},
		{[]string{"./g"}, []string{"b"}, true},
		{[]string{"./a", "./c"}, []string{"a", "b", "c"}, true},
		{[]string{"./..."}, []string{"a", "app", "b", "c"}, false},
		{[]string{"./app"}, []string{"a", "app", "b", "c"}, false},
		{nil, []string{"a", "app", "b", "c"}, false},
	} {
		out, got := dirs(c.args...)
		if !slices.Equal(got, c.want) {
			t.Errorf("gen %v: packages %v, want %v", c.args, got, c.want)
		}
		if narrowed := strings.Contains(out, " packages)"); narrowed != c.narrowed {
			t.Errorf("gen %v: narrowed %v, want %v (%s)", c.args, narrowed, c.narrowed, out)
		}
	}
	if err := gen([]string{"./a"}, io.Discard); err == nil {
		t.Errorf("gen ./a without -overlay-out: no error")
	}
	if err := gen([]string{"./..."}, io.Discard); err != nil {
		t.Errorf("gen ./...: %v", err)
	}
}
