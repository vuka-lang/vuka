package load

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// coldModule writes an app of n folder-route pages, pages/(admin)/itemN/[id],
// each a Vuka package importing store, a plain Go package whose imports
// (encoding/csv) no Vuka package names, and a library module beside it.
func coldModule(t *testing.T, n int) string {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=readonly")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "dep", "go.mod"), "module example.com/dep\n\ngo 1.23\n")
	write(t, filepath.Join(dir, "dep", "dep.go"), "package dep\n\nimport \"strings\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n")
	root := filepath.Join(dir, "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/vuka-lang/vuka/v2 v2.0.0\n\texample.com/dep v0.0.0\n)\n\n"+
		"replace github.com/vuka-lang/vuka/v2 => "+repo+"\n\nreplace example.com/dep => ../dep\n")
	write(t, filepath.Join(root, "store", "store.go"), `package store

import (
	"encoding/csv"
	"strings"

	"example.com/dep"
)

func Rows(s string) int {
	r, _ := csv.NewReader(strings.NewReader(s)).ReadAll()
	return len(r) + len(dep.Up(s))
}
`)
	var imports, calls []string
	for i := range n {
		d := filepath.Join(root, "pages", "(admin)", fmt.Sprintf("item%d", i), "[id]")
		write(t, filepath.Join(d, "page.vuka"), fmt.Sprintf(`package param_id

import "example.com/app/store"

func Count%d() Result[int] {
	return Ok(store.Rows("a,b"))
}
`, i))
		imports = append(imports, fmt.Sprintf("\t_ \"example.com/app/pages/group_admin/item%d/param_id\"", i))
	}
	write(t, filepath.Join(root, "main.go"), "package main\n\nimport (\n"+strings.Join(imports, "\n")+"\n)\n\nfunc main() {"+strings.Join(calls, "")+"}\n")
	// Directories changed this second aren't kept between reads: the app
	// was written a while ago.
	old := time.Now().Add(-time.Hour)
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			os.Chtimes(p, old, old)
		}
		return nil
	})
	return root
}

// A cold build — nothing in vuka's cache — reads each directory of the
// module once, however many steps walk it, and asks the go command for the
// export data of what its packages import once, not once more for each
// import type-checking reaches.
func TestColdBuildReadsEachDirectoryOnce(t *testing.T) {
	const n = 12
	root := coldModule(t, n)
	t.Setenv("VUKA_CACHE", t.TempDir())
	dirs := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs++
		}
		return nil
	})

	reads, lists := DirReads(), goLists.Load()
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != n {
		t.Fatalf("%d packages, want %d", len(pkgs), n)
	}
	s, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	s.UseCache("")
	gens, _, err := s.Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Mirror(root, filepath.Join(t.TempDir(), "build"), gens, true); err != nil {
		t.Fatal(err)
	}
	if got := DirReads() - reads; got > int64(dirs) {
		t.Errorf("a cold build read directories %d times; the module has %d", got, dirs)
	}
	if got := goLists.Load() - lists; got != 1 {
		t.Errorf("a cold build ran go list %d times, want 1", got)
	}

	// Discovery again, as the next step of a build does: no directory is
	// read again.
	reads = DirReads()
	if _, err := Discover(root, "example.com/app", root, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := DirReads() - reads; got != 0 {
		t.Errorf("discovering again read directories %d times", got)
	}
}

// A component file named by a relative path is no package: the go command
// is never asked about it.
func TestImporterLocalPath(t *testing.T) {
	im := NewImporter(t.TempDir(), "")
	lists := goLists.Load()
	if _, err := im.Import("../web/islands/Chart.vue"); err == nil {
		t.Fatal("imported a component file")
	}
	if err := im.Prefetch([]string{"./Chart.vue"}); err != nil {
		t.Fatal(err)
	}
	if got := goLists.Load() - lists; got != 0 {
		t.Errorf("ran go list %d times for component files", got)
	}
}
