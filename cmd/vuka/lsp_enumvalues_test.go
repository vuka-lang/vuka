package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lspEnumButton = `<script lang="go">
package components

import (
	"example.com/app/design"
	"github.com/vuka-lang/ui/v2"
)

// Variant is a Button's look.
type Variant string

const (
	VariantDefault Variant = "" // the primary colour
	// VariantOutline is a bordered button.
	VariantOutline Variant = "outline"
	VariantGhost   Variant = "ghost"
)

type Button struct {
	Variant  Variant
	Tone     design.Tone
	Icon     string
	Children ui.Node
}
</script>

<button>{Children}</button>
`

const lspEnumDesign = `package design

type Tone string

const (
	ToneInfo   Tone = "info"
	ToneDanger Tone = "danger" // a destructive action
	toneHidden Tone = "hidden"
)
`

const lspEnumPage = `<script lang="go">
package pages

import "example.com/app/components"

type Index struct{}
</script>

<main>
  <components.Button variant="" tone="" icon="">Save</components.Button>
</main>
`

// A component tag's attribute setting a prop of a defined string type
// completes with its constants' values, the constant and its doc beside
// each.
func TestLSPEnumValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"components/button.vuka.html": lspEnumButton,
		"design/design.go":            lspEnumDesign,
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	page := filepath.Join(dir, "pages", "index.vuka.html")
	src := []byte(lspEnumPage)
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	vf := &vfile{source: page, from: src}
	items := func(needle string) []map[string]any {
		t.Helper()
		off := strings.Index(lspEnumPage, needle) + len(needle)
		spot, ok := completionSpot(src, off)
		if !ok || spot.kind != "value" {
			t.Fatalf("%s: spot %+v %v", needle, spot, ok)
		}
		var out []map[string]any
		for _, it := range p.completeJSX(vf, "", spot, off).(map[string]any)["items"].([]any) {
			out = append(out, it.(map[string]any))
		}
		return out
	}
	show := func(its []map[string]any) string {
		var out []string
		for _, it := range its {
			out = append(out, it["label"].(string)+"="+it["detail"].(string))
		}
		return strings.Join(out, ",")
	}
	for needle, want := range map[string]string{
		`variant="`: `""=the primary colour,outline=VariantOutline is a bordered button.,ghost=VariantGhost`,
		`tone="`:    `info=ToneInfo,danger=a destructive action`,
		`icon="`:    ``,
	} {
		if got := show(items(needle)); got != want {
			t.Errorf("%s: %q, want %q", needle, got, want)
		}
	}
	if it := items(`variant="`)[1]; it["textEdit"].(map[string]any)["newText"] != "outline" {
		t.Errorf("inserts %v", it["textEdit"])
	}
}
