package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLSPDiagnosticCodes: the editor's diagnostics carry their code, a link
// to its page and the help, for the compiler's errors and for what vuka vet
// reports of an island page.
func TestLSPDiagnosticCodes(t *testing.T) {
	src := "package main\n\nfunc helper() {} @doc(\"x\")\nfunc main() {}\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": src})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "E0203")
	})
	for _, want := range []string{`"code":"E0203"`, `"codeDescription":{"href":"https://vuka-lang.github.io/vuka/errors/E0203"}`,
		`"message":"an attribute must start its own line\nhelp: put the attribute on its own line, above the declaration"`, `"severity":1`} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %s: %s", want, got)
		}
	}
}

func TestLSPIslandVet(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"detail.vuka": vetPage, "islands/Detail.vue": "<template><p/></template>"})
	uri := pathToURI(filepath.Join(dir, "detail.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": vetPage}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "E0324")
	})
	line := strings.Count(vetPage[:strings.Index(vetPage, "CV ")], "\n")
	for _, want := range []string{`"code":"E0324"`, `"message":"Detail.CV is deferred, and the group Profile has no loader\nhelp: add the method\nfunc (d *Detail) LoadProfile(ctx context.Context) error"`,
		`"range":{"end":{"character":3,"line":` + itoaTest(uint32(line)) + `},"start":{"character":1,"line":` + itoaTest(uint32(line)) + `}}`, `"code":"E0322"`} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %s: %s", want, got)
		}
	}
}
