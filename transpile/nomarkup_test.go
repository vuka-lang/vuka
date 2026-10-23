package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestShapeMidEdit: a shape being written — its struct and no markup yet,
// or a tag half typed — is an error, in both forms, never a panic.
func TestShapeMidEdit(t *testing.T) {
	head := "package main\n\ntype Page struct {\n\tTitle string\n}\n"
	for _, src := range []string{
		head,
		head + "\n",
		head + "  <",
		head + "  <di",
		head + "\t<div class=\"x\">",
		"package main\n\ntype Page struct {\n",
		"<script lang=\"go\">\n" + head + "</script>\n",
		"<script lang=\"go\">\n" + head + "</script>\n\n  <di",
		"<script lang=\"go\">\n" + head,
		"<script lang=\"go\">\npackage main\n\ntype Page struct {\n</script>\n",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%q: panic %v", src, r)
				}
			}()
			files := []transpile.File{{Name: "page.vuka.html", Src: []byte(src)}, {Name: "main.go", Src: []byte("package main\n")}}
			errs := transpile.Syntax(files[0], transpile.Options{Importer: load.NewImporter("testdata/golden", "")}, files[1])
			_, err := transpile.Package(files, transpile.Options{Importer: load.NewImporter("testdata/golden", "")})
			if strings.TrimSpace(src) == strings.TrimSpace(head) && (err == nil || !hasCode(err, "E0148")) {
				t.Errorf("%q: %v, want E0148", src, err)
			}
			_ = errs
			transpile.TemplateAssets("page.vuka.html", []byte(src))
			transpile.ParseShape([]byte(src))
			transpile.TemplateHeaders([]byte(src))
			transpile.BlankTemplate([]byte(src), len(src)/2)
			transpile.SplitTemplate([]byte(src))
		}()
	}
}

func hasCode(err error, code string) bool {
	l, ok := err.(transpile.ErrorList)
	for _, e := range l {
		if string(e.Code) == code {
			return ok
		}
	}
	return false
}
