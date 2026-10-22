package load

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoDir: a bracketed folder of folder routes is seen by the go command
// by its Go name, param_id, and the folders below it under that name; the
// Go name maps back to the folder while it isn't on disk.
func TestGoDir(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"pages/pets/[id]/photos", "pages/files/[path...]"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	for rel, want := range map[string]string{
		"pages/pets":               "pages/pets",
		"pages/pets/[id]":          "pages/pets/param_id",
		"pages/pets/[id]/photos":   "pages/pets/param_id/photos",
		"pages/files/[path...]":    "pages/files/param_path",
		"pages/not[a]bracket/here": "pages/not[a]bracket/here",
	} {
		got := GoDir(root, filepath.Join(root, rel))
		if got != filepath.Join(root, want) {
			t.Errorf("GoDir(%s) = %s, want %s", rel, got, want)
		}
		if rel == want {
			continue
		}
		if src, ok := SourceDir(root, got); !ok || src != filepath.Join(root, rel) {
			t.Errorf("SourceDir(%s) = %s, %v, want %s", want, src, ok, rel)
		}
	}
	if !IsGoName(filepath.Join(root, "pages/pets/param_id")) || !IsGoName(filepath.Join(root, "pages/files/param_path")) || IsGoName(filepath.Join(root, "pages/pets/param_x")) {
		t.Error("IsGoName")
	}
	os.MkdirAll(filepath.Join(root, "pages/pets/param_id"), 0o755)
	if _, ok := SourceDir(root, filepath.Join(root, "pages/pets/param_id")); ok {
		t.Error("SourceDir of a Go name on disk")
	}
}
