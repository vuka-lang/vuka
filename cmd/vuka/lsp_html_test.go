package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspHTMLGo = `package main

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
}

func (s Sales) Total() int { return len(s.Week) }

func main() {}
`

const lspHTMLTemplate = `package main

<section>
  <h2>{Title}</h2>
  for _, d := range Week {
    <i>{d}</i>
  }
  <p>{Total()}</p>
</section>

func row(n int)
<p>{n}</p>
`

// TestLSPTemplate: hover and definition in a .vuka.html land on the Go its
// bare names and holes are.
func TestLSPTemplate(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspHTMLTemplate}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspHTMLTemplate), strings.Index(lspHTMLTemplate, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("{Title}", 1)))
	if !strings.Contains(string(hover), "Title string") {
		t.Errorf("hover on Title: %s", hover)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at("{Total()}", 1)))
	if !strings.Contains(string(def), "sales.go") {
		t.Errorf("definition of Total: %s", def)
	}
	def, _ = json.Marshal(c.call("textDocument/definition", at("{n}", 1)))
	if !strings.Contains(string(def), "sales.vuka.html") {
		t.Errorf("definition of n: %s", def)
	}
}

const lspHTMLTyping = `<section>
  <h2>{Title}</h2>
  <inp
  <a tar
  <input type="ch
  <textarea bi
</section>
`

// TestLSPTemplateHTML: completion in a .vuka.html's markup is HTML's: the
// elements after <, an element's attributes in its tag, an attribute's
// values in its quotes.
func TestLSPTemplateHTML(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspHTMLTemplate}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": lspHTMLTyping}}})
	at := func(needle string) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspHTMLTyping), strings.Index(lspHTMLTyping, needle)+len(needle))}
	}
	for needle, want := range map[string]string{"<inp": `"input"`, "<a tar": `"target"`, `type="ch`: `"checkbox"`, "<textarea bi": `"bind:value"`} {
		b, _ := json.Marshal(c.call("textDocument/completion", at(needle)))
		if !strings.Contains(string(b), want) {
			t.Errorf("completion at %s: no %s in %.300s", needle, want, b)
		}
	}
}
