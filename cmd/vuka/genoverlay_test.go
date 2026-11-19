package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
