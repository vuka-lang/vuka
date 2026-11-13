package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lspAlert = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

var alert = ui.Variants{
	Base: "rounded-lg border px-4 py-3",
	Variants: map[string]map[string]string{
		"tone": {"error": "bg-red-50", "info": "bg-gray-50"},
		"size": {"sm": "px-2 py-1", "md": ""},
	},
	Defaults: map[string]string{"tone": "info"},
}

type Alert struct {
	Tone, Size string
	Class      string
	Children   ui.Node
}
</script>

<div class={alert.Class("tone", Tone, "size", Size, "class", Class)}>{Children}</div>
`

const lspBadge = `package components

import "github.com/vuka-lang/ui/v2"

var badge = ui.Variants{Variants: map[string]map[string]string{"kind": {"new": "bg-blue-50", "sale": "bg-red-50"}}}

type Badge struct{ Kind string }

func (b Badge) View() ui.Node { return ui.HTML(` + "`<b class={c}/>`" + `, badge.Class("kind", b.Kind)) }
`

const lspVariantsPage = `<script lang="go">
package pages

import "example.com/app/components"

type Index struct{}
</script>

<main>
  <components.Alert tone="e" size="">Saved.</components.Alert>
  <components.Badge kind="">new</components.Badge>
  <components.Alert class="x">No variant.</components.Alert>
</main>
`

// A component tag's attribute setting a prop its ui.Variants takes
// completes with the variant's values.
func TestLSPVariantValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "components"), 0o755)
	os.MkdirAll(filepath.Join(dir, "pages"), 0o755)
	os.WriteFile(filepath.Join(dir, "components", "alert.vuka.html"), []byte(lspAlert), 0o644)
	os.WriteFile(filepath.Join(dir, "components", "badge.go"), []byte(lspBadge), 0o644)
	page := filepath.Join(dir, "pages", "index.vuka.html")
	src := []byte(lspVariantsPage)
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	vf := &vfile{source: page, from: src}
	labels := func(needle string) string {
		t.Helper()
		off := strings.Index(lspVariantsPage, needle) + len(needle)
		spot, ok := completionSpot(src, off)
		if !ok || spot.kind != "value" {
			t.Fatalf("%s: spot %+v %v", needle, spot, ok)
		}
		var out []string
		for _, it := range p.completeJSX(vf, "", spot, off).(map[string]any)["items"].([]any) {
			out = append(out, it.(map[string]any)["label"].(string))
		}
		return strings.Join(out, ",")
	}
	for needle, want := range map[string]string{`tone="`: "error,info", `size="`: "sm,md", `kind="`: "new,sale", `class="`: ""} {
		if got := labels(needle); got != want {
			t.Errorf("%s: %q, want %q", needle, got, want)
		}
	}
}
