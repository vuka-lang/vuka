package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const e2eCounter = `package pages

import "github.com/vuka-lang/ui/v2"

// Counter counts.
type Counter struct {
	ui.Live
	n    int
	open ui.State[bool]
}

func (p *Counter) Inc() { p.n++ }
<section>
  <button onClick={Inc}>{n}</button>
  <button onClick={open = !open}>Toggle</button>
</section>
`

// TestLSPShapeSemantics drives the proxy and gopls on the routes fixture:
// the legend carries the Vuka types, a .vuka.html file's tokens hold them,
// a page has its code lens once the module is built, hover reads a hole,
// and renaming a page file edits the urls chains naming it.
func TestLSPShapeSemantics(t *testing.T) {
	app := routesModule(t, map[string]string{"pages/counter.vuka.html": e2eCounter})
	c := startLSPAt(t, app, lspOptions{renameCommands: true})
	legend := semTokLegendFrom(t, c.init)
	event, ok := legend["vukaEvent"]
	if !ok {
		t.Fatalf("legend lacks the Vuka types: %v", legend)
	}
	caps := c.init.(map[string]any)["capabilities"].(map[string]any)
	if caps["workspace"].(map[string]any)["fileOperations"].(map[string]any)["willRename"] == nil {
		t.Fatal("willRename not advertised")
	}

	path := filepath.Join(app, "pages", "counter.vuka.html")
	uri := pathToURI(path)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": e2eCounter}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	toks := decodeTokens(t, c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}}).(map[string]any))
	if tk, ok := tokenAt(t, toks, e2eCounter, "Inc}>", 1); !ok || tk.typ != event {
		t.Errorf("onClick={Inc}: %+v (found %v), want type %d", tk, ok, event)
	}
	if tk, ok := tokenAt(t, toks, e2eCounter, "open = !open", 1); !ok || tk.typ != legend["vukaBrowserState"] {
		t.Errorf("open: %+v (found %v)", tk, ok)
	}

	pos := positionOf([]byte(e2eCounter), strings.Index(e2eCounter, "{n}")+1)
	h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
	if h == nil || !strings.Contains(h["contents"].(map[string]any)["value"].(string), "**server state** `n int` · set in `Inc`") {
		t.Errorf("hover on {n}: %v", h)
	}

	var lenses []any
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if lenses, _ = c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": uri}}).([]any); len(lenses) > 0 {
			break
		}
	}
	if len(lenses) != 1 || lenses[0].(map[string]any)["command"].(map[string]any)["title"] != "GET /counter · live · layout pages.Layout" {
		t.Fatalf("code lenses: %v", lenses)
	}

	edit, _ := c.call("workspace/willRenameFiles", map[string]any{"files": []any{map[string]any{
		"oldUri": pathToURI(filepath.Join(app, "pages", "about.vuka.html")),
		"newUri": pathToURI(filepath.Join(app, "pages", "info.vuka.html"))}}}).(map[string]any)
	changes, _ := edit["changes"].(map[string]any)
	if len(changes) != 1 {
		t.Fatalf("rename edits: %v", edit)
	}
	for u, edits := range changes {
		src := string(p0(uriToPath(u)))
		if got := applyEdits(src, edits.([]any)); !strings.Contains(got, "urls.Info.Path()") {
			t.Errorf("%s after the rename:\n%s", u, got)
		}
	}
}

func p0(path string) []byte { return (&proxy{}).sourceText(path) }
