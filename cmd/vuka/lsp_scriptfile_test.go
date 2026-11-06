package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const lspOrdersGo = `package main

import "strconv"

// Orders lists numbers.
type Orders struct {
	Title string
	ns    []int
}

// Label is a number's label.
func (p Orders) Label(n int) string { return "#" + strconv.Itoa(n) }

func main() {}
`

const lspOrdersMarkup = `<main>
  <h1>{Title}</h1>
  for _, n := range ns {
    <p>{Label(n)}</p>
  }
</main>
`

// TestLSPScriptFile: in a template of markup alone, hover and definition
// reach the struct its script file declares (vuka#356).
func TestLSPScriptFile(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"orders.go": lspOrdersGo, "orders.vuka.html": lspOrdersMarkup})
	uri := pathToURI(filepath.Join(dir, "orders.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspOrdersMarkup}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(lspOrdersMarkup), strings.Index(lspOrdersMarkup, "Label(n)")+2)}
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if h, _ := c.call("textDocument/hover", at).(map[string]any); h != nil {
			if md = h["contents"].(map[string]any)["value"].(string); strings.Contains(md, "Label") {
				break
			}
		}
	}
	if !strings.Contains(md, "func (p Orders) Label(n int) string") {
		t.Errorf("hover: %s", md)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at))
	if !strings.Contains(string(def), "orders.go") || !strings.Contains(string(def), `"line":11`) {
		t.Errorf("definition: %s", def)
	}
}

const inlineOrders = `<script lang="go">
package pages

import (
	"strconv"

	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

// Orders counts.
type Orders struct {
	ui.Live
	n int
}

// Add counts one.
func (p *Orders) Add() { p.n++ }

func (p *Orders) text() string { return strconv.Itoa(p.n) }
</script>

<main>
  <button onClick={Add}>{text()}</button>
  <components.Badge label="x" />
</main>
`

// TestScriptMoveActions: the script moves out to orders.go, the imports
// only the markup uses staying in the template, and back in.
func TestScriptMoveActions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orders.vuka.html")
	writeFile(t, path, inlineOrders)
	acts := actionsAt(t, path, strings.Index(inlineOrders, "type Orders"), strings.Index(inlineOrders, "type Orders"))
	out, ok := acts["Move the script to orders.go"]
	if !ok {
		t.Fatalf("no action to move the script out: %v", acts)
	}
	applyWorkspace(t, out.Edit)
	tmpl, script := readFile(t, path), readFile(t, filepath.Join(dir, "orders.go"))
	if want := "import \"example.com/app/components\"\n\n<main>"; !strings.HasPrefix(tmpl, want) {
		t.Errorf("the template:\n%s", tmpl)
	}
	for _, want := range []string{"package pages", "\"strconv\"", "\"github.com/vuka-lang/ui/v2\"", "func (p *Orders) Add()"} {
		if !strings.Contains(script, want) || strings.Contains(script, "components") {
			t.Errorf("the script file lacks %s, or has components:\n%s", want, script)
		}
	}
	back, ok := actionsAt(t, path, 0, 0)["Move the script from orders.go into this file"]
	if !ok {
		t.Fatal("no action to move the script back")
	}
	applyWorkspace(t, back.Edit)
	if _, err := os.Stat(filepath.Join(dir, "orders.go")); err == nil {
		t.Error("orders.go is still there")
	}
	got := readFile(t, path)
	for _, want := range []string{"<script lang=\"go\">\npackage pages\n", "\"example.com/app/components\"", "func (p *Orders) text() string", "</script>\n\n<main>"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	decorated := strings.Replace(inlineOrders, "// Add counts one.\n", "@ui.Mount\nfunc (p *Orders) mount() error { return nil }\n\n// Add counts one.\n", 1)
	writeFile(t, path, decorated)
	if _, ok := actionsAt(t, path, strings.Index(decorated, "type Orders"), strings.Index(decorated, "type Orders"))["Move the script to orders.vuka"]; !ok {
		t.Error("a script with a decorator doesn't move to orders.vuka")
	}
}
