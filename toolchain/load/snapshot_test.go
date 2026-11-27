package load

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestSnapshotReads(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.25\n")
	write("pages/index.vuka.html", "<h1>home</h1>\n")
	write("lib/lib.go", "package lib\n")
	write("nested/go.mod", "module example.com/nested\n")

	s := NewSnapshot()
	for _, rel := range []string{"go.mod", "pages/index.vuka.html", "lib/lib.go"} {
		p := filepath.Join(root, rel)
		want, err := ReadSource(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ReadSource(p)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: snapshot read %q, %v; ReadSource %q", rel, got, err, want)
		}
		got[0] = 'X' // a caller's own copy
		if again, _ := s.ReadSource(p); !bytes.Equal(again, want) {
			t.Errorf("%s: a caller's edit reached the snapshot: %q", rel, again)
		}
	}
	if _, err := s.ReadSource(filepath.Join(root, "missing.go")); !os.IsNotExist(err) {
		t.Errorf("a missing file reads %v", err)
	}

	names := func(dir string, read func(string) ([]os.DirEntry, error)) []string {
		entries, err := read(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	if got, want := names(root, s.ReadDir), names(root, ReadDir); !slices.Equal(got, want) {
		t.Errorf("snapshot lists %q, ReadDir %q", got, want)
	}

	for rel, want := range map[string]bool{"go.mod": true, "lib": true, "missing.go": false, "lib/none": false, "none/none": false} {
		if got := s.Exists(filepath.Join(root, rel)); got != want {
			t.Errorf("Exists(%s) = %v, want %v", rel, got, want)
		}
	}
	if !s.HasGoMod(filepath.Join(root, "nested")) || s.HasGoMod(filepath.Join(root, "lib")) {
		t.Error("HasGoMod answers wrong")
	}

	var walked, want []string
	s.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			fi, err := d.Info()
			if err != nil || fi.Name() != d.Name() {
				t.Errorf("%s: Info %v, %v", p, fi, err)
			}
			walked = append(walked, p)
		}
		return err
	})
	WalkDir(root, func(p string, d os.DirEntry, err error) error {
		want = append(want, p)
		return err
	})
	if !slices.Equal(walked, want) {
		t.Errorf("snapshot walks %q, WalkDir %q", walked, want)
	}

	// What changes after the snapshot read it isn't seen, until it is
	// forgotten.
	lib := filepath.Join(root, "lib", "lib.go")
	write("lib/lib.go", "package lib // changed\n")
	write("lib/more.go", "package lib\n")
	if got, _ := s.ReadSource(lib); string(got) != "package lib\n" {
		t.Errorf("the snapshot read a change: %q", got)
	}
	if s.Exists(filepath.Join(root, "lib", "more.go")) {
		t.Error("the snapshot lists a file added after it listed the directory")
	}
	s.Forget(lib, filepath.Join(root, "lib", "more.go"))
	if got, _ := s.ReadSource(lib); string(got) != "package lib // changed\n" {
		t.Errorf("forgotten, the file reads %q", got)
	}
	if !s.Exists(filepath.Join(root, "lib", "more.go")) {
		t.Error("forgotten, the directory doesn't list the added file")
	}

	// A nil Snapshot reads the disk.
	var none *Snapshot
	if got, _ := none.ReadSource(lib); string(got) != "package lib // changed\n" {
		t.Errorf("a nil snapshot reads %q", got)
	}
	if !none.Exists(lib) || none.Exists(filepath.Join(root, "missing.go")) {
		t.Error("a nil snapshot's Exists answers wrong")
	}
}

func TestSnapshotDiscover(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/app\n\ngo 1.25\n",
		"main.go":           "package main\n\nfunc main() {}\n",
		"pages/index.vuka":  "package pages\n\ntype Index struct{}\n",
		"pages/about.vuka":  "package pages\n\ntype About struct{}\n",
		"lib/lib.go":        "package lib\n",
		"views/card.vuka":   "package views\n\nimport \"example.com/app/lib\"\n\nvar _ = lib.X\n",
		"nested/go.mod":     "module example.com/nested\n",
		"nested/x/x.vuka":   "package x\n",
		"_private/p/p.vuka": "package p\n",
	}
	for rel, src := range files {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewSnapshot().Discover(root, "example.com/app", root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the snapshot discovers %v, Discover %v", got, want)
	}
	if len(want) != 2 {
		t.Errorf("discovered %d packages, want pages and views", len(want))
	}
}
