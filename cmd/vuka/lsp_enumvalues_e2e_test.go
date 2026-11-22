package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const enumSpotsButton = `<script lang="go">
package components

// Size is a Button's size.
type Size string

const (
	SizeSm Size = "sm"
	SizeLg Size = "lg"
)

type Variant string

const (
	VariantGhost Variant = "ghost"
)

type Kind string

const (
	KindPlain Kind = "plain"
	KindFancy Kind = "fancy"
)

type Button struct {
	Variant Variant
	Size    Size
	Type    Kind
	Title   Kind
}
</script>

<button>x</button>
`

const enumSpotsPage = `<script lang="go">
package pages

import "lsptest/components"

type Index struct{}
</script>

<main>
  <components.Button variant="ghost" size="sm" type="plain" title="plain" />
  <button type="submit">Go</button>
  <input placeholder="Hello" />
</main>
`

const enumSpotsPanel = `<script lang="go">
package components

type Panel struct{}
</script>

<div>
  <Button variant="ghost" size="sm" type="plain" title="plain" />
</div>
`

// Completion inside an attribute's quotes answers through the whole server,
// prefix typed or not: a component prop of a defined string type offers its
// constants even when the attribute spells an HTML one (size, type, title),
// an element's enumerated attribute keeps its HTML values, and free attribute
// text stays quiet.
func TestLSPEnumValueSpots(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{
		"components/button.vuka.html": enumSpotsButton,
		"components/panel.vuka.html":  enumSpotsPanel,
		"pages/index.vuka.html":       enumSpotsPage,
	})
	complete := func(path, text, needle string) []map[string]any {
		t.Helper()
		uri := pathToURI(filepath.Join(dir, path))
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": "vuka", "version": 1, "text": text}})
		c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
		off := strings.Index(text, needle) + len(needle)
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(text), off)})
		m, _ := v.(map[string]any)
		items, _ := m["items"].([]any)
		var out []map[string]any
		for _, it := range items {
			out = append(out, it.(map[string]any))
		}
		return out
	}
	labels := func(its []map[string]any) string {
		var out []string
		for _, it := range its {
			out = append(out, it["label"].(string))
		}
		return strings.Join(out, ",")
	}

	for needle, want := range map[string]string{
		`variant="gh`: "ghost",
		`size="s`:     "sm,lg",
		`type="`:      "plain,fancy",
		`title="`:     "plain,fancy",
	} {
		if got := labels(complete("pages/index.vuka.html", enumSpotsPage, needle)); got != want {
			t.Errorf("%s: %q, want %q", needle, got, want)
		}
	}

	// The typed prefix is what the picked value replaces.
	items := complete("pages/index.vuka.html", enumSpotsPage, `variant="gh`)
	if len(items) == 0 {
		t.Fatal("no items for variant=\"gh")
	}
	b, _ := json.Marshal(items[0]["textEdit"])
	quote := strings.Index(enumSpotsPage, `variant="`) + len(`variant="`)
	from := positionOf([]byte(enumSpotsPage), quote)
	if want := `"start":{"character":` + jsonInt(from.Character); !strings.Contains(string(b), want) {
		t.Errorf("textEdit %s replaces from the quote (%s)", b, want)
	}

	if got := labels(complete("components/panel.vuka.html", enumSpotsPanel, `size="s`)); got != "sm,lg" {
		t.Errorf("bare <Button size=: %q", got)
	}

	if got := labels(complete("pages/index.vuka.html", enumSpotsPage, `<button type="su`)); !strings.Contains(","+got+",", ",submit,") {
		t.Errorf("<button type=: %q, want the HTML values", got)
	}
	if got := labels(complete("pages/index.vuka.html", enumSpotsPage, `placeholder="He`)); got != "" {
		t.Errorf("free attribute text completes: %q", got)
	}
}

func jsonInt(v uint32) string {
	b, _ := json.Marshal(v)
	return string(b)
}
