package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLSPGoImportsVuka: a .go file importing a package of only .vuka
// files type-checks in the editor — in a package of plain Go and in a
// package with .vuka files of its own — and its errors are its own.
func TestLSPGoImportsVuka(t *testing.T) {
	entities := "package entities\n\ntype Category struct {\n\tID   int64\n\tName string\n}\n\nfunc Find(id int64) Result[Category] { return Ok(Category{ID: id}) }\n"
	for _, tc := range []struct{ name, file, pkg string }{
		{"plain Go package", "dtos/advert.go", "dtos"},
		{"mixed package", "views/count.go", "views"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package " + tc.pkg + "\n\nimport \"lsptest/entities\"\n\ntype CategoryCount struct {\n\tCategory entities.Category\n\tCount    int\n}\n\nfunc Name(c CategoryCount) int {\n\treturn c.Category.Name\n}\n"
			files := map[string]string{
				"main.go":              "package main\n\nfunc main() {}\n",
				"entities/models.vuka": entities,
				"views/title.vuka":     "package views\n\nfunc Title() Result[string] { return Ok(\"t\") }\n",
				tc.file:                src,
			}
			c, dir, _ := startLSPWith(t, files)
			uri := pathToURI(filepath.Join(dir, tc.file))
			c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": src}})
			var got string
			c.waitDiags(uri, func(ds []any) bool {
				got = jsonOf(ds)
				return strings.Contains(got, "cannot use") || strings.Contains(got, "could not import") || strings.Contains(got, "not in std")
			})
			if strings.Count(got, `"message"`) != 1 || !strings.Contains(got, `"line":10`) {
				t.Errorf("diagnostics: %s", got)
			}
		})
	}
}
