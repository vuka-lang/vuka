package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const markupFnPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Order is one of the orders reviewed.
type Order struct {
	ID     int
	Number string
}

// Review lists its orders through a markup func.
type Review struct {
	ui.Live
	Orders []Order
}

// Approve approves an order.
func (p *Review) Approve(id int) error { return nil }
</script>

row := func(o Order) {
  <li>{o.Number} <button onClick={Aprove(o.ID)}>approve</button></li>
}

<ul>
  for _, o := range Orders {
    {row(o)}
  }
</ul>
`

// TestLSPMarkupFuncs: a markup func's body is markup as the main markup is
// to the editor: a diagnostic inside it covers its place in the body, and
// hover and definition there answer as they do in the main markup.
func TestLSPMarkupFuncs(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "review.vuka.html": markupFnPage})
	uri := pathToURI(filepath.Join(dir, "review.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": markupFnPage}})
	ds := c.waitDiags(uri, func(ds []any) bool { return len(ds) > 0 })
	at := positionOf([]byte(markupFnPage), strings.Index(markupFnPage, "Aprove"))
	got := jsonOf(ds)
	want := `"start":{"character":` + itoaTest(at.Character) + `,"line":` + itoaTest(at.Line) + `}`
	if !strings.Contains(got, `"code":"E0172"`) || !strings.Contains(got, want) {
		t.Fatalf("diagnostics %s, want E0172 at %s", got, want)
	}

	fixed := strings.Replace(markupFnPage, "Aprove", "Approve", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": fixed}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	pos := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(fixed), strings.Index(fixed, needle)+delta)}
	}
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if h, _ := c.call("textDocument/hover", pos("Approve(o.ID)", 2)).(map[string]any); h != nil {
			if md = h["contents"].(map[string]any)["value"].(string); strings.Contains(md, "Approve(id int)") {
				break
			}
		}
	}
	if !strings.Contains(md, "func (p *Review) Approve(id int) error") {
		t.Errorf("hover on the method inside the body: %s", md)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", pos("o.Number}", 0)))
	if line := `"line":` + itoaTest(positionOf([]byte(fixed), strings.Index(fixed, "o Order) {")).Line); !strings.Contains(string(def), line) {
		t.Errorf("definition of the parameter: %s, want %s", def, line)
	}
}
