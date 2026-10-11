package main

import (
	"strings"
	"testing"
)

func TestAddImportsWithDecoratorNoBlock(t *testing.T) {
	src := "package adminapplicant\n\n@admin.AdminPage()\ntype Applicants struct {\n}\n"
	at, text := addImports([]byte(src), []importSpec{{path: "portal/pages/admin"}})
	out := src[:at] + text + src[at:]
	if !strings.HasPrefix(out, "package adminapplicant\n\nimport \"portal/pages/admin\"\n") {
		t.Fatalf("import landed wrong:\n%s", out)
	}
}
