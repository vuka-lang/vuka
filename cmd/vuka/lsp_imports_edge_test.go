package main

import (
	"strings"
	"testing"
)

// applyImportEdits applies import edits (src offsets) to src.
func applyImportEdits(src string, eds []importEdit) string {
	for i := len(eds) - 1; i >= 0; i-- {
		e := eds[i]
		src = src[:e.start] + e.text + src[e.end:]
	}
	return src
}

func TestAddImportsWithDecoratorNoBlock(t *testing.T) {
	src := "package adminapplicant\n\n@admin.AdminPage()\ntype Applicants struct {\n}\n"
	out := applyImportEdits(src, addImports([]byte(src), []importSpec{{path: "portal/pages/admin"}}))
	if !strings.HasPrefix(out, "package adminapplicant\n\nimport \"portal/pages/admin\"\n") {
		t.Fatalf("import landed wrong:\n%s", out)
	}
}

// TestAddImportsConsolidatesLoneImports (BUG A): when a file's imports are
// lone `import "x"` lines, adding a new import folds them all into one sorted
// grouped block rather than appending more lone lines.
func TestAddImportsConsolidatesLoneImports(t *testing.T) {
	src := "package p\n\nimport \"context\"\nimport \"github.com/vuka-lang/ui\"\n\nfunc f() {}\n"
	out := applyImportEdits(src, addImports([]byte(src), []importSpec{{path: "portal/entities"}}))
	want := "package p\n\nimport (\n\t\"context\"\n\t\"github.com/vuka-lang/ui\"\n\t\"portal/entities\"\n)\n\nfunc f() {}\n"
	if out != want {
		t.Fatalf("lone imports not consolidated:\ngot:\n%s\nwant:\n%s", out, want)
	}
	if strings.Count(out, "import") != 1 {
		t.Fatalf("expected one import keyword, got %d:\n%s", strings.Count(out, "import"), out)
	}
}

// TestAddImportsIntoExistingBlock keeps the existing-block path working.
func TestAddImportsIntoExistingBlock(t *testing.T) {
	src := "package p\n\nimport (\n\t\"context\"\n\t\"fmt\"\n)\n\nfunc f() {}\n"
	out := applyImportEdits(src, addImports([]byte(src), []importSpec{{path: "bytes"}}))
	want := "package p\n\nimport (\n\t\"bytes\"\n\t\"context\"\n\t\"fmt\"\n)\n\nfunc f() {}\n"
	if out != want {
		t.Fatalf("existing block insertion wrong:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// TestAddImportsConsolidatesWithAlias preserves an alias when folding.
func TestAddImportsConsolidatesWithAlias(t *testing.T) {
	src := "package p\n\nimport m \"math\"\nimport \"strings\"\n\nfunc f() {}\n"
	out := applyImportEdits(src, addImports([]byte(src), []importSpec{{path: "bytes"}}))
	want := "package p\n\nimport (\n\t\"bytes\"\n\tm \"math\"\n\t\"strings\"\n)\n\nfunc f() {}\n"
	if out != want {
		t.Fatalf("alias not preserved on fold:\ngot:\n%s\nwant:\n%s", out, want)
	}
}
