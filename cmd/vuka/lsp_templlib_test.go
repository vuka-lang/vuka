package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lspTemplLibPage = `<script lang="go">
package pages

import "lsptest/components/widget"

type Index struct{}
</script>

<main>
  <widget.Button variant="primary" >Go</widget.Button>
</main>
`

// TestLSPTemplLibrary: a templ library component's tag completes its props
// as attributes are written (id, class, attributes), and hover on one shows
// its field.
func TestLSPTemplLibrary(t *testing.T) {
	files := map[string]string{"main.go": "package main\n\nimport _ \"lsptest/pages\"\n\nfunc main() {}\n", "pages/index.vuka.html": lspTemplLibPage}
	for _, name := range []string{"widget.go", "widget.templ", "widget.js"} {
		b, _ := os.ReadFile(filepath.Join("testdata", "templlib", name))
		files["components/widget/"+name] = string(b)
	}
	c, dir, _ := startLSPWith(t, templFiles(t, files))
	uri := pathToURI(filepath.Join(dir, "pages", "index.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspTemplLibPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	items := completionItems(c, uri, lspTemplLibPage, `variant="primary" `)
	for _, want := range []string{"id", "class", "attributes", "onClick", "aria-label", "data-"} {
		if _, ok := items[want]; !ok {
			t.Errorf("no %s among %v", want, keys(items))
		}
	}
	for _, not := range []string{"iD", "variant"} {
		if _, ok := items[not]; ok {
			t.Errorf("%s is offered: %v", not, keys(items))
		}
	}
	pos := positionOf([]byte(lspTemplLibPage), strings.Index(lspTemplLibPage, `variant=`)+2)
	b, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}))
	if !strings.Contains(string(b), "Variant string") {
		t.Errorf("hover on variant: %s", b)
	}
}
