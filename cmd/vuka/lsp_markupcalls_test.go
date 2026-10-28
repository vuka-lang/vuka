package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const callsStore = `package main

import "context"

type Category struct{ Name string }

type Store struct{}

func (Store) Categories(ctx context.Context) ([]Category, error) { return nil, nil }

func main() {}
`

const callsPage = `package main

// Page lists the store's categories.
type Page struct{ Store Store }
<section>
  categories := Store.Categories()
  <p>{len(categories)}</p>
  for _, c := range Store.Categories() {
    <b>{c.Name}</b>
  }
</section>
`

func openCalls(t *testing.T) (*lspClient, string) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": callsStore, "page.vuka.html": callsPage})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": callsPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	return c, uri
}

// TestLSPImplicitCtxHover: hover on a call given the render's ctx shows
// gopls's signature and says the page supplies ctx.
func TestLSPImplicitCtxHover(t *testing.T) {
	c, uri := openCalls(t)
	at := strings.Index(callsPage, "for _, c := range Store.Categories()") + len("for _, c := range Store.Cat")
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(callsPage), at)}).(map[string]any)
		if h != nil {
			md = h["contents"].(map[string]any)["value"].(string)
			if strings.Contains(md, "Categories(ctx context.Context)") {
				break
			}
		}
	}
	for _, want := range []string{"Categories(ctx context.Context) ([]Category, error)", "ctx is supplied by the page"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover lacks %q: %s", want, md)
		}
	}
}

// TestLSPImplicitCtxHints: an inlay hint shows the ctx where each call
// gets it.
func TestLSPImplicitCtxHints(t *testing.T) {
	c, uri := openCalls(t)
	end := positionOf([]byte(callsPage), len(callsPage))
	got := c.call("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": end}})
	b, _ := json.Marshal(got)
	for _, call := range []string{"categories := Store.Categories(", "range Store.Categories("} {
		pos := positionOf([]byte(callsPage), strings.Index(callsPage, call)+len(call))
		want, _ := json.Marshal(map[string]any{"line": pos.Line, "character": pos.Character})
		if !strings.Contains(string(b), `"label":"ctx"`) || !strings.Contains(string(b), string(want)) {
			t.Errorf("hints %s lack ctx at %s", b, want)
		}
	}
}

// TestLSPMarkupCallsDefinition: definition works through a call given
// ctx, and on a := name's use, which is lazy in the Go.
func TestLSPMarkupCallsDefinition(t *testing.T) {
	c, uri := openCalls(t)
	def := func(at int) string {
		var got string
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			v := c.call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(callsPage), at)})
			b, _ := json.Marshal(v)
			if got = string(b); got != "null" && got != "[]" {
				break
			}
		}
		return got
	}
	got := def(strings.Index(callsPage, "range Store.Categories") + len("range Store.Cat"))
	if !strings.Contains(got, "main.go") {
		t.Errorf("definition of Categories: %s, want main.go", got)
	}
	got = def(strings.Index(callsPage, "{len(categories)}") + len("{len(cat"))
	decl := positionOf([]byte(callsPage), strings.Index(callsPage, "categories :="))
	want, _ := json.Marshal(map[string]any{"line": decl.Line, "character": decl.Character})
	if !strings.Contains(got, "page.vuka.html") || !strings.Contains(got, string(want)) {
		t.Errorf("definition of categories: %s, want its statement at %s", got, want)
	}
}
