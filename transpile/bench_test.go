package transpile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// BenchmarkGoldens transpiles every golden case that transpiles, as the
// build does (readable output) and as the language server does (bare), one
// importer shared across runs as a build's is.
func BenchmarkGoldens(b *testing.B) {
	const dir = "testdata/golden"
	paths, _ := filepath.Glob(dir + "/*.vuka")
	var files []transpile.File
	for _, p := range paths {
		if strings.HasPrefix(filepath.Base(p), "err_") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			b.Fatal(err)
		}
		files = append(files, transpile.File{Name: filepath.Base(p), Src: src})
	}
	imp := load.NewImporter(dir, "")
	for _, mode := range []struct {
		name string
		bare bool
	}{{"readable", false}, {"bare", true}} {
		b.Run(mode.name, func(b *testing.B) {
			for range b.N {
				for _, f := range files {
					if _, err := transpile.Package([]transpile.File{f}, transpile.Options{Importer: imp, Dir: dir, Bare: mode.bare}); err != nil {
						b.Fatal(f.Name, err)
					}
				}
			}
		})
	}
}
