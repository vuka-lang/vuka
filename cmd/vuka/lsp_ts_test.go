package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspTSGo = `package main

import "github.com/vuka-lang/ui/v2"

// Chart is a component with a script.
type Chart struct {
	Title  string
	Points []int
	open   ui.State[bool]
}

func main() {}
`

const lspTSTemplate = `package main

<figure>{Title}</figure>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Chart>((el, props, ctx) => {
  el.title = props.Title + props.Missing
  ctx.state("open").set(true)
})
</script>
`

// TestLSPScript: a template's script is TypeScript with its component's
// types: completion and hover from the TypeScript server, its errors at
// their lines in the .vuka.html.
func TestLSPScript(t *testing.T) {
	if findTSLS() == "" {
		nm := fixtureModules("VUKA_TS_NM", "islands")
		if nm == "" || !fileExists(filepath.Join(nm, ".bin", "typescript-language-server")) {
			needs(t, "no typescript-language-server ($VUKA_TSLS, or npm ci --prefix testdata/node/islands)")
		}
		t.Setenv("VUKA_TSLS", filepath.Join(nm, ".bin", "typescript-language-server"))
	}
	c, dir, _ := startLSPWith(t, map[string]string{"chart.go": lspTSGo, "chart.vuka.html": lspTSTemplate})
	uri := pathToURI(filepath.Join(dir, "chart.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspTSTemplate}})
	line := uint32(strings.Count(lspTSTemplate[:strings.Index(lspTSTemplate, "props.Missing")], "\n"))
	c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "Missing") && strings.Contains(string(b), `"line":`+itoaTest(line))
	})
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspTSTemplate), strings.Index(lspTSTemplate, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("props.Title", 7)))
	if !strings.Contains(string(hover), "Title") || !strings.Contains(string(hover), "string") {
		t.Errorf("hover on props.Title: %s", hover)
	}
	comp, _ := json.Marshal(c.call("textDocument/completion", at(`ctx.state`, 4)))
	for _, want := range []string{`"state"`, `"navigate"`, `"live"`} {
		if !strings.Contains(string(comp), want) {
			t.Errorf("completion after ctx. lacks %s: %.400s", want, comp)
		}
	}
}
