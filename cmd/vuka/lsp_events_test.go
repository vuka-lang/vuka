package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const eventsPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Orders picks a status.
type Orders struct {
	ui.Live
	status string
}

// Choose keeps the status picked.
func (p *Orders) Choose(s string) { p.status = s }
</script>

<main>
  <select data-group="a" onChange={Choose(this.value)}><option>a</option></select>
  <p>{status}</p>
</main>
`

// TestLSPEventCalls: in an on… attribute calling a method, hover and
// definition on the method go to it, completion after this. offers the
// element's reads and its attributes, and hover on a read says what it
// reads (vuka#353).
func TestLSPEventCalls(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "orders.vuka.html": eventsPage})
	uri := pathToURI(filepath.Join(dir, "orders.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": eventsPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(src, needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), strings.Index(src, needle)+delta)}
	}

	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if h, _ := c.call("textDocument/hover", at(eventsPage, "Choose(this", 2)).(map[string]any); h != nil {
			md = h["contents"].(map[string]any)["value"].(string)
			if strings.Contains(md, "Choose(s string)") {
				break
			}
		}
	}
	if !strings.Contains(md, "func (p *Orders) Choose(s string)") {
		t.Errorf("hover on the method: %s", md)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at(eventsPage, "Choose(this", 2)))
	if want := `"line":12`; !strings.Contains(string(def), "orders.vuka.html") || !strings.Contains(string(def), want) {
		t.Errorf("definition of the method: %s", def)
	}

	h, _ := json.Marshal(c.call("textDocument/hover", at(eventsPage, "this.value", len("this.v"))))
	if !strings.Contains(string(h), "The element's value when the event fires") {
		t.Errorf("hover on this.value: %s", h)
	}
	h, _ = json.Marshal(c.call("textDocument/hover", at(eventsPage, "this.value", 1)))
	if !strings.Contains(string(h), "is the element the event is on") {
		t.Errorf("hover on this: %s", h)
	}

	typing := strings.Replace(eventsPage, "Choose(this.value)", "Choose(this.)", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": typing}}})
	items := completionLabels(t, c.call("textDocument/completion", at(typing, "this.)", len("this."))))
	for _, want := range []string{"value", "checked", "dataGroup"} {
		if items[want] == nil {
			t.Errorf("no %s among %v", want, items)
		}
	}
}
