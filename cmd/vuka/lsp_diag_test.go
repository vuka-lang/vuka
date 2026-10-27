package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/diag"
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
	for _, want := range []string{`"code":"E0203"`, `"codeDescription":{"href":"` + diag.DocsURL + `E0203"}`,
		`"message":"an attribute must start its own line\nhelp: put the attribute on its own line, above the declaration"`, `"severity":1`} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %s: %s", want, got)
		}
	}
}

// TestLSPUnimportedGrouped: the attributes of a package the file doesn't
// import are one diagnostic, at the first, the others its related
// information.
func TestLSPUnimportedGrouped(t *testing.T) {
	src := "package main\n\n@shop.Tracked\nfunc a() {}\n\n@shop.Logged\nfunc b() {}\n\n@shop.Tracked\nfunc c() {}\n\nfunc main() {}\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": src})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "E0215")
	})
	if n := strings.Count(got, `"code":"E0215"`); n != 1 {
		t.Errorf("%d E0215 diagnostics, want 1: %s", n, got)
	}
	for _, want := range []string{`"message":"@shop.Tracked and @shop.Logged: this file imports no package named shop`,
		`"range":{"end":{"character":0,"line":2},"start":{"character":0,"line":2}}`,
		`"relatedInformation":[{"location":{"range":{"end":{"character":0,"line":5},"start":{"character":0,"line":5}}`,
		`{"location":{"range":{"end":{"character":0,"line":8},"start":{"character":0,"line":8}}`} {
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
	for _, want := range []string{`"code":"E0324"`, `"message":"Detail.CV is deferred, and the group Profile has no loader\nhelp: mark its loader @ui.Loads(\"profile\")"`,
		`"range":{"end":{"character":3,"line":` + itoaTest(uint32(line)) + `},"start":{"character":1,"line":` + itoaTest(uint32(line)) + `}}`, `"code":"E0322"`} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %s: %s", want, got)
		}
	}
}

// TestLSPBracketFolder: a [id] folder's package is seen by the go command
// by its Go name, _id; an error in its Vuka source is reported at the
// source, in its folder, and nothing about the folder's name.
func TestLSPBracketFolder(t *testing.T) {
	src := "package item\n\nfunc Name() string {\n\tvar n int = \"x\"\n\treturn \"\"\n}\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "views/[id]/item.vuka": src})
	uri := pathToURI(filepath.Join(dir, "views", "[id]", "item.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "cannot use")
	})
	if !strings.Contains(got, `"line":3`) || strings.Contains(got, "malformed") || strings.Contains(got, "invalid char") {
		t.Errorf("diagnostics: %s", got)
	}
}

// TestLSPDiagnosticsDuringEdit: gopls's diagnostics of a generated file
// that arrive while the editor's buffer of its source is newer than the
// build are shown once a build of the buffer leaves the file's Go as it
// was: gopls doesn't send the same diagnostics again.
func TestLSPDiagnosticsDuringEdit(t *testing.T) {
	src := "package item\n\nfunc Name() string {\n\tvar n int = \"x\"\n\treturn \"\"\n}\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "views/[id]/item.vuka": src})
	waitFor(t, func() bool { return strings.Contains(c.log.String(), "built the module") })
	// gopls is sent the generated Go; while it diagnoses it, the editor
	// opens the source and keeps changing it, back to what it was.
	uri := pathToURI(filepath.Join(dir, "views", "[id]", "item.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
	for v, end := 2, time.Now().Add(3*time.Second); time.Now().Before(end); v++ {
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": v},
			"contentChanges": []any{map[string]any{"text": src}}})
		time.Sleep(10 * time.Millisecond)
	}
	c.waitDiags(uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), "cannot use") })
}

// TestLSPGroupFolder: a route group's package, (admin), is seen by the go
// command by its Go name, group_admin; an error in its Vuka source is
// reported at the source, in its folder, and nothing about the folder's
// name.
func TestLSPGroupFolder(t *testing.T) {
	src := "package item\n\nfunc Name() string {\n\tvar n int = \"x\"\n\treturn \"\"\n}\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "views/(admin)/item.vuka": src})
	uri := pathToURI(filepath.Join(dir, "views", "(admin)", "item.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "cannot use")
	})
	if !strings.Contains(got, `"line":3`) || strings.Contains(got, "malformed") || strings.Contains(got, "invalid char") {
		t.Errorf("diagnostics: %s", got)
	}
}

// TestLSPGoInRouteFolders: a .go file in a route group or a [id] folder is
// type-checked with its package, seen by the go command by the folder's Go
// name: its errors are reported at it, and a definition in another of the
// folder's .go files lands in that file, not its copy.
func TestLSPGoInRouteFolders(t *testing.T) {
	for _, folder := range []string{"(admin)", "[id]"} {
		t.Run(folder, func(t *testing.T) {
			label := "package pets\n\n// Label is a pet's label.\nfunc Label() string { return \"rex\" }\n"
			src := "package pets\n\nfunc Name() string {\n\tvar n int = Label()\n\treturn \"\"\n}\n"
			c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n",
				"pages/pets/" + folder + "/label.go": label, "pages/pets/" + folder + "/name.go": src})
			path := filepath.Join(dir, "pages", "pets", folder, "name.go")
			uri := pathToURI(path)
			doc := map[string]any{"uri": uri}
			c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": src}})
			var got string
			c.waitDiags(uri, func(ds []any) bool {
				got = jsonOf(ds)
				return strings.Contains(got, "cannot use")
			})
			if !strings.Contains(got, `"line":3`) || strings.Contains(got, "malformed") || strings.Contains(got, "invalid char") {
				t.Errorf("diagnostics: %s", got)
			}
			v, _ := c.call("textDocument/definition", map[string]any{"textDocument": doc,
				"position": positionOf([]byte(src), strings.Index(src, "Label()")+2)}).([]any)
			if len(v) != 1 || v[0].(map[string]any)["uri"] != pathToURI(filepath.Join(dir, "pages", "pets", folder, "label.go")) ||
				!strings.Contains(jsonOf(v[0].(map[string]any)["range"]), `"line":3`) {
				t.Errorf("definition of Label: %s", jsonOf(v))
			}
		})
	}
}
