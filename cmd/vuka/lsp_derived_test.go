package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspDerivedTemplate = `package main

import "github.com/vuka-lang/ui/v2"

// Cart derives its total.
type Cart struct {
	ui.Live
	count ui.State[int]
	price ui.State[int]
	total ui.Derived[int] @ui.From(count * price)
	tab   ui.State[string] @ui.Notify(OnTab)
}

// OnTab hears the tab.
func (c *Cart) OnTab(tab string) {}
<p>{total}</p>
`

// TestLSPDerived: a Derived's @ui.From reads the struct's fields by their
// bare names, which hover and definition reach; the editor completes them
// there, and a wrong one is the compiler's diagnostic at its place; a
// @ui.Notify's handler is reached the same way.
func TestLSPDerived(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": lspHTMLGo, "cart.vuka.html": lspDerivedTemplate})
	uri := pathToURI(filepath.Join(dir, "cart.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspDerivedTemplate}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspDerivedTemplate), strings.Index(lspDerivedTemplate, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("{total}", 1)))
	if !strings.Contains(string(hover), "Derived[int]") {
		t.Errorf("hover on total: %s", hover)
	}
	hover, _ = json.Marshal(c.call("textDocument/hover", at("price)", 0)))
	if !strings.Contains(string(hover), "State[int]") {
		t.Errorf("hover on price in @ui.From: %s", hover)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at("count *", 0)))
	if !strings.Contains(string(def), "cart.vuka.html") || !strings.Contains(string(def), `"line":7`) {
		t.Errorf("definition of count in @ui.From: %s", def)
	}
	def, _ = json.Marshal(c.call("textDocument/definition", at("OnTab)", 0)))
	if !strings.Contains(string(def), "cart.vuka.html") || !strings.Contains(string(def), `"line":14`) {
		t.Errorf("definition of OnTab in @ui.Notify: %s", def)
	}

	version := 1
	edit := func(from, to string) string {
		src := strings.Replace(lspDerivedTemplate, from, to, 1)
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
		return src
	}
	src := edit("@ui.From(count * price)", "@ui.From(count * pri)")
	items := completionItems(c, uri, src, "count * pri")
	if _, ok := items["price"]; !ok {
		t.Errorf("completing in @ui.From: no price among %v", keys(items))
	}
	edit("@ui.From(count * price)", "@ui.From(cout * price)")
	c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "E0176") && strings.Contains(string(b), "reads cout") && strings.Contains(string(b), "did you mean count")
	})
}
