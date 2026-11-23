package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lateButton = `<script lang="go">
package components

// Variant is a Button's look.
type Variant string

const (
	VariantGhost   Variant = "ghost"
	VariantOutline Variant = "outline"
)

type Button struct {
	Variant Variant
	Title   string
}
</script>

<button>x</button>
`

const latePage = `<script lang="go">
package pages

import "lsptest/components"

type Index struct{}
</script>

<main>
  <components.Button variant="gh" title="He" />
</main>
`

// lateCompletion answers a value spot on a component tag from the sources
// alone — no build, the file read from disk when its buffer isn't open —
// and stays quiet everywhere else, so the watchdog's empty answer remains
// for what it can't know.
func TestLateCompletionFromSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"components/button.vuka.html": lateButton,
		"pages/index.vuka.html":       latePage,
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	page := filepath.Join(dir, "pages", "index.vuka.html")
	params := func(path, needle string) json.RawMessage {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		off := strings.Index(string(src), needle) + len(needle)
		pos := positionOf(src, off)
		b, _ := json.Marshal(map[string]any{
			"textDocument": map[string]any{"uri": pathToURI(path)},
			"position":     pos,
		})
		return b
	}
	p := &proxy{modRoot: dir, modPath: "lsptest", importDirs: map[string]string{}, shown: map[string]string{}}

	res := p.lateCompletion(params(page, `variant="gh`))
	m, _ := res.(map[string]any)
	if m == nil {
		t.Fatal("no answer for a value spot")
	}
	var labels []string
	for _, it := range m["items"].([]any) {
		labels = append(labels, it.(map[string]any)["label"].(string))
	}
	if got := strings.Join(labels, ","); got != "ghost,outline" {
		t.Errorf("items %q, want ghost,outline", got)
	}

	if res := p.lateCompletion(params(page, `title="He`)); res != nil {
		t.Errorf("free attribute text answered: %v", res)
	}
	if res := p.lateCompletion(params(page, `<main>`)); res != nil {
		t.Errorf("markup text answered: %v", res)
	}

	bare := &proxy{importDirs: map[string]string{}, shown: map[string]string{}}
	if res := bare.lateCompletion(params(page, `variant="gh`)); res != nil {
		t.Errorf("answered without a module root: %v", res)
	}
}

// The first completion right after didOpen — before the first build settles —
// answers a component prop's constants rather than the empty list the editor
// would cache for the typed prefix.
func TestLSPFirstCompletionBeforeBuild(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{
		"components/button.vuka.html": lateButton,
		"pages/index.vuka.html":       latePage,
	})
	uri := pathToURI(filepath.Join(dir, "pages", "index.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": latePage}})
	off := strings.Index(latePage, `variant="gh`) + len(`variant="gh`)
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(latePage), off)})
	m, _ := v.(map[string]any)
	items, _ := m["items"].([]any)
	var labels []string
	for _, it := range items {
		labels = append(labels, fmt.Sprint(it.(map[string]any)["label"]))
	}
	if got := strings.Join(labels, ","); got != "ghost,outline" {
		t.Errorf("first completion: %q, want ghost,outline", got)
	}
}
