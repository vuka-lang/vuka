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

// A prefix typed inside the quotes still offers every value, replacing the
// prefix; a quote just typed after = (no closing quote yet) completes too.
func TestLSPEnumValuesTyping(t *testing.T) {
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
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	labels := func(text, needle, attr string) []string {
		t.Helper()
		src := []byte(text)
		off := strings.Index(text, needle) + len(needle)
		spot, ok := completionSpot(src, off)
		if !ok || spot.kind != "value" {
			t.Fatalf("%s: spot %+v %v", needle, spot, ok)
		}
		quote := strings.Index(text, attr+`="`) + len(attr) + 2
		from := positionOf(src, quote)
		var out []string
		for _, it := range p.completeJSX(&vfile{source: page, from: src}, "", spot, off).(map[string]any)["items"].([]any) {
			m := it.(map[string]any)
			if edit := m["textEdit"].(map[string]any)["range"].(lspRange); edit.Start != from {
				t.Errorf("%s: %s replaces from %+v, want %+v (the quote)", needle, m["label"], edit.Start, from)
			}
			out = append(out, m["label"].(string))
		}
		return out
	}
	prefix := strings.Replace(lspEnumPage, `variant=""`, `variant="out"`, 1)
	if got := labels(prefix, `variant="out`, "variant"); strings.Join(got, ",") != `"",outline,ghost` {
		t.Errorf("with a prefix typed: %v", got)
	}
	open := strings.Replace(lspEnumPage, `tone="" icon="">Save</components.Button>`, `tone="`, 1)
	if got := labels(open, `tone="`, "tone"); strings.Join(got, ",") != "info,danger" {
		t.Errorf("after a quote not closed yet: %v", got)
	}
}

const lspEnumSibling = `<script lang="go">
package components

type Panel struct{}
</script>

<div>
  <Button variant="">x</Button>
</div>
`

// A bare tag naming a component of the file's own package completes its
// enum props' values too.
func TestLSPEnumValuesBareTag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"components/button.vuka.html": lspEnumButton,
		"components/panel.vuka.html":  lspEnumSibling,
		"design/design.go":            lspEnumDesign,
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	page := filepath.Join(dir, "components", "panel.vuka.html")
	src := []byte(lspEnumSibling)
	off := strings.Index(lspEnumSibling, `variant="`) + len(`variant="`)
	spot, ok := completionSpot(src, off)
	if !ok || spot.kind != "value" {
		t.Fatalf("spot %+v %v", spot, ok)
	}
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	var got []string
	for _, it := range p.completeJSX(&vfile{source: page, from: src}, "", spot, off).(map[string]any)["items"].([]any) {
		got = append(got, it.(map[string]any)["label"].(string))
	}
	if strings.Join(got, ",") != `"",outline,ghost` {
		t.Errorf("bare <Button variant=: %v", got)
	}
}

// Completing an attribute name on a component tag: an enum prop inserts a
// quoted value spot and re-triggers completion there; other props are
// unchanged.
func TestLSPEnumPropAttr(t *testing.T) {
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
	text := strings.Replace(lspEnumPage, `variant="" tone="" icon=""`, ``, 1)
	src := []byte(text)
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	off := strings.Index(text, `<components.Button `) + len(`<components.Button `)
	items, ok := p.propCompletion(page, src, off)
	if !ok {
		t.Fatal("no prop completion in <components.Button>")
	}
	got := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		got[m["label"].(string)] = m
	}
	for attr, want := range map[string]string{"variant": `variant="$1"`, "tone": `tone="$1"`, "icon": `icon="$1"`} {
		if text := got[attr]["textEdit"].(map[string]any)["newText"]; text != want {
			t.Errorf("%s inserts %v, want %q", attr, text, want)
		}
	}
	for attr, want := range map[string]bool{"variant": true, "tone": true, "icon": false} {
		if _, has := got[attr]["command"]; has != want {
			t.Errorf("%s re-triggers completion: %v, want %v", attr, has, want)
		}
	}
	enums := p.tagEnumAttrs(page, src, "components.Button")
	if !enums["variant"] || !enums["tone"] || enums["icon"] {
		t.Errorf("tagEnumAttrs: %v", enums)
	}
}

// A component tag whose props have an Attrs field completes attr:, the
// attribute it passes on; one without doesn't. Both new forms leave the
// spots after them attributes.
func TestLSPAttrPassthrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	button := strings.Replace(lspEnumButton, "\tChildren ui.Node\n", "\tAttrs    []ui.Attr\n\tChildren ui.Node\n", 1)
	card := strings.Replace(strings.Replace(lspEnumButton, "type Button struct", "type Card struct", 1), "<button>", "<section>", 1)
	card = strings.Replace(card, "</button>", "</section>", 1)
	for name, src := range map[string]string{
		"components/button.vuka.html": button,
		"components/card.vuka.html":   card,
		"design/design.go":            lspEnumDesign,
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	page := strings.Replace(lspEnumPage, `<components.Button variant="" tone="" icon="">Save</components.Button>`,
		`<components.Button attr:form={id} icon?={x} >Save</components.Button>
  <components.Card icon?={x} >x</components.Card>`, 1)
	src := []byte(page)
	p := &proxy{modRoot: dir, modPath: "example.com/app", importDirs: map[string]string{}}
	vf := &vfile{source: filepath.Join(dir, "pages", "index.vuka.html"), from: src}
	labels := func(needle string) map[string]bool {
		t.Helper()
		off := strings.Index(page, needle) + len(needle)
		spot, ok := completionSpot(src, off)
		if !ok || spot.kind != "attr" {
			t.Fatalf("%s: spot %+v %v", needle, spot, ok)
		}
		out := map[string]bool{}
		for _, it := range p.completeJSX(vf, "", spot, off).(map[string]any)["items"].([]any) {
			out[it.(map[string]any)["label"].(string)] = true
		}
		return out
	}
	if !labels(`icon?={x} `)["attr:"] {
		t.Error("no attr: on a component with Attrs")
	}
	if labels(`<components.Card icon?={x} `)["attr:"] {
		t.Error("attr: on a component without Attrs")
	}
	for _, needle := range []string{`attr:form={id}`, `icon?={x}`} {
		off := strings.Index(page, needle) + len(needle) - 1
		if spot, ok := completionSpot(src, off); ok && spot.kind != "" && spot.kind != "value" {
			t.Errorf("%s: the value is a %s spot", needle, spot.kind)
		}
	}
}
