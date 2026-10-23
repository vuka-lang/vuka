package load

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoDir: a bracketed folder of folder routes is seen by the go command
// by its Go name, param_id, a route group (admin) by group_admin, and the
// folders below them under those names, as is a private folder below
// pages/, _parts by private_parts; the Go name maps back to the
// folder while it isn't on disk.
func TestGoDir(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"pages/pets/[id]/photos", "pages/files/[path...]", "pages/(admin)/users/[id]", "pages/_parts/buttons", "pages/(admin)/_nav", "lib/_x"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	for rel, want := range map[string]string{
		"pages/pets":               "pages/pets",
		"pages/pets/[id]":          "pages/pets/param_id",
		"pages/pets/[id]/photos":   "pages/pets/param_id/photos",
		"pages/files/[path...]":    "pages/files/param_path",
		"pages/not[a]bracket/here": "pages/not[a]bracket/here",
		"pages/(admin)":            "pages/group_admin",
		"pages/(admin)/users/[id]": "pages/group_admin/users/param_id",
		"pages/not(a)group":        "pages/not(a)group",
		"pages/_parts/buttons":     "pages/private_parts/buttons",
		"pages/(admin)/_nav":       "pages/group_admin/private_nav",
		"lib/_x":                   "lib/_x",
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
	if !IsGoName(filepath.Join(root, "pages/pets/param_id")) || !IsGoName(filepath.Join(root, "pages/files/param_path")) || IsGoName(filepath.Join(root, "pages/pets/param_x")) ||
		!IsGoName(filepath.Join(root, "pages/group_admin")) || IsGoName(filepath.Join(root, "pages/group_x")) ||
		!IsGoName(filepath.Join(root, "pages/private_parts")) || IsGoName(filepath.Join(root, "lib/private_x")) {
		t.Error("IsGoName")
	}
	os.MkdirAll(filepath.Join(root, "pages/pets/param_id"), 0o755)
	if _, ok := SourceDir(root, filepath.Join(root, "pages/pets/param_id")); ok {
		t.Error("SourceDir of a Go name on disk")
	}
}
