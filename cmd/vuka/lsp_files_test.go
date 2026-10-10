package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const filesSource = `package main

import "strings"

func Template(f vuka.File) func(*vuka.Decl) { return func(*vuka.Decl) {} }

@Template("views/page.html")
func page() {}

@Template("views/pets.templ")
func pets() {}

@Template("hello.templ")
func hello() {}

func main() { _ = strings.ToUpper("x") }
`

// TestLSPFileLinks: vuka.File literals are document links, their definition
// is the file (a .templ file's one component), their hover the path and the
// components, and they keep working mid-edit.
func TestLSPFileLinks(t *testing.T) {
	c, dir, init := startLSPWith(t, templFiles(t, map[string]string{
		"main.vuka":        filesSource,
		"views/page.html":  "<h1>page</h1>\n",
		"views/pets.templ": "package views\n\ntempl PetList(names []string, n int) {\n\t<p>{ names[n] }</p>\n}\n",
		"hello.templ":      templSource,
	}))
	if caps := init.(map[string]any)["capabilities"].(map[string]any); caps["documentLinkProvider"] == nil {
		t.Fatal("no documentLinkProvider")
	}
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": filesSource}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	doc := map[string]any{"uri": uri}
	target := func(name string) string { return pathToURI(filepath.Join(dir, name)) }

	checkLinks := func(src string) {
		t.Helper()
		v := c.call("textDocument/documentLink", map[string]any{"textDocument": doc})
		got := map[string]lspRange{}
		for _, l := range v.([]any) {
			m := l.(map[string]any)
			r, _ := toRange(m["range"])
			got[m["target"].(string)] = r
		}
		if r, ok := got["https://pkg.go.dev/strings"]; !ok || r.Start != positionOf([]byte(src), strings.Index(src, `"strings"`)+1) {
			t.Fatalf("gopls's import link: %v (all: %s)", r, jsonOf(v))
		}
		for _, name := range []string{"views/page.html", "views/pets.templ", "hello.templ"} {
			off := strings.Index(src, `"`+name+`"`) + 1
			want := lspRange{positionOf([]byte(src), off), positionOf([]byte(src), off+len(name))}
			if r, ok := got[target(name)]; !ok || r != want {
				b, _ := json.Marshal(v)
				t.Fatalf("link to %s: %v, want %v (all: %s)", name, r, want, b)
			}
		}
	}
	checkLinks(filesSource)

	at := func(src, needle string) map[string]any {
		return map[string]any{"textDocument": doc, "position": positionOf([]byte(src), strings.Index(src, needle)+3)}
	}
	def := func(src, needle string) map[string]any {
		t.Helper()
		v, _ := c.call("textDocument/definition", at(src, needle)).([]any)
		if len(v) != 1 {
			t.Fatalf("definition at %s: %v", needle, v)
		}
		return v[0].(map[string]any)
	}
	if d := def(filesSource, `"views/page.html"`); d["uri"] != target("views/page.html") || !strings.Contains(jsonOf(d["range"]), `"start":{"character":0,"line":0}`) {
		t.Fatalf("definition of page.html: %s", jsonOf(d))
	}
	if d := def(filesSource, `"views/pets.templ"`); d["uri"] != target("views/pets.templ") ||
		jsonOf(d["range"]) != `{"end":{"character":13,"line":2},"start":{"character":6,"line":2}}` {
		t.Fatalf("definition of pets.templ: %s", jsonOf(d))
	}

	h := jsonOf(c.call("textDocument/hover", at(filesSource, `"views/pets.templ"`)))
	if !strings.Contains(h, "`views/pets.templ`") || !strings.Contains(h, "templ PetList(names, n)") {
		t.Fatalf("hover: %s", h)
	}

	// A line added above, then an error below: the links follow the text.
	edited := strings.Replace(filesSource, "package main\n", "package main\n\n// pages\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": edited}}})
	checkLinks(edited)
	broken := strings.Replace(edited, "func main() {", "func main() { x := ", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": broken}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) > 0 })
	checkLinks(broken)
	if d := def(broken, `"hello.templ"`); d["uri"] != target("hello.templ") {
		t.Fatalf("definition mid-edit: %s", jsonOf(d))
	}
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
