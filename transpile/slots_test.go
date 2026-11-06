package transpile_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestUnknownSlotCode: a slot a component doesn't have is E0106 for every
// component: an app's, a library's, a templ one's, <ui.Await> and
// <ui.Suspense>.
func TestUnknownSlotCode(t *testing.T) {
	for _, c := range []struct{ dir, want string }{
		{"slotunknown", "has no slot"},
		{"suspenseslots", "has no slot"},
		{"awaiterrs", "has no slot"},
	} {
		dir := filepath.Join("testdata/golden/templates", c.dir)
		entries, _ := os.ReadDir(dir)
		var files []transpile.File
		for _, e := range entries {
			if n := e.Name(); strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html") {
				src, _ := os.ReadFile(filepath.Join(dir, n))
				files = append(files, transpile.File{Name: n, Src: src})
			}
		}
		_, err := transpile.Package(files, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: dir})
		var list transpile.ErrorList
		if !errors.As(err, &list) {
			t.Fatalf("%s: %v", c.dir, err)
		}
		n := 0
		for _, e := range list {
			if strings.Contains(e.Msg, c.want) {
				n++
				if e.Code != "E0106" {
					t.Errorf("%s: %s is %s, want E0106", c.dir, e.Msg, e.Code)
				}
			}
		}
		if n == 0 {
			t.Errorf("%s: no unknown slot among %v", c.dir, list)
		}
	}
}
