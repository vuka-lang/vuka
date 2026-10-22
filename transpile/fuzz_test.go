package transpile_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// FuzzPackage transpiles arbitrary source: it never panics, and the Go it
// produces parses. Seeded with the golden cases.
func FuzzPackage(f *testing.F) {
	const dir = "testdata/golden"
	cases, _ := filepath.Glob(dir + "/*.vuka")
	for _, p := range cases {
		src, _ := os.ReadFile(p)
		f.Add(src)
	}
	imp := load.NewImporter(dir, "")
	f.Fuzz(func(t *testing.T, src []byte) {
		res, err := transpile.Package([]transpile.File{{Name: "f.vuka", Src: src}},
			transpile.Options{Importer: imp, Dir: dir})
		if err != nil {
			return
		}
		for _, out := range res.Files {
			if _, err := parser.ParseFile(token.NewFileSet(), out.Name, out.Src, parser.ParseComments); err != nil {
				t.Fatalf("the generated Go doesn't parse: %v\nsource:\n%s\ngenerated:\n%s", err, src, out.Src)
			}
		}
	})
}

// FuzzSyntax checks a file's syntax alone, the editor's first pass: it never
// panics.
func FuzzSyntax(f *testing.F) {
	cases, _ := filepath.Glob("testdata/golden/*.vuka")
	for _, p := range cases {
		src, _ := os.ReadFile(p)
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		transpile.Syntax(transpile.File{Name: "f.vuka", Src: src}, transpile.Options{})
	})
}
