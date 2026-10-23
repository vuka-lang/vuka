package main

import (
	"strings"
	"testing"
)

const semShape = `package pages

import (
	"context"

	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

type Board struct {
	ui.Live
	Title string
	stats Stats
	rows  []Row
	open  ui.State[bool]
}

@ui.Mount
func (p *Board) mount(ctx context.Context) error {
	p.stats = load()
	return nil
}

func (p *Board) Add() { p.rows = append(p.rows, Row{}) }

func (p *Board) label() string { return "x" }
<main>
  <h1>{Title} {stats.Posts}</h1>
  <button onClick={Add}>Add</button>
  <button onClick={open = !open}>Toggle</button>
  <button onClick={ui.JS.Toggle("#m")}>Menu</button>
  for _, r := range rows {
    <components.Card title={r.Name}/>
  }
  <p>{label()}</p>
</main>
`

// tokenAt finds the decoded token starting at the nth occurrence of needle in src.
func tokenAt(t *testing.T, toks []semToken, src, needle string, nth int) (semToken, bool) {
	t.Helper()
	off := -1
	for i := 0; nth > 0; nth-- {
		j := strings.Index(src[i:], needle)
		if j < 0 {
			t.Fatalf("%q not in source", needle)
		}
		off = i + j
		i = off + 1
	}
	pos := positionOf([]byte(src), off)
	for _, tk := range toks {
		if tk.line == pos.Line && tk.char == pos.Character {
			return tk, true
		}
	}
	return semToken{}, false
}

// TestShapeTokens: the markup's names come out as the Vuka token types, a
// field's declaration with the declaration modifier, after gopls's legend.
func TestShapeTokens(t *testing.T) {
	const base, decl = 30, 1
	toks := shapeTokens(scanShape([]byte(semShape)), base, decl)
	typ := func(k refKind) uint32 { return base + uint32(k-refProp) }
	for _, c := range []struct {
		needle string
		nth    int
		kind   refKind
		mods   uint32
	}{
		{"Title string", 1, refProp, decl},
		{"stats Stats", 1, refState, decl},
		{"open  ui.State", 1, refBrowser, decl},
		{"stats = load", 1, refState, 0},
		{"{Title}", 1, refNone, 0},
		{"Title} {stats", 1, refProp, 0},
		{"stats.Posts", 1, refState, 0},
		{"Add}>", 1, refEvent, 0},
		{"Add() {", 1, refEvent, decl},
		{"open = !open", 1, refBrowser, 0},
		{"open}>Toggle", 1, refBrowser, 0},
		{"r := range", 1, refLocal, decl},
		{"r.Name", 1, refLocal, 0},
		{"Card title", 1, refComponent, 0},
		{"rows {", 1, refState, 0},
	} {
		got, ok := tokenAt(t, toks, semShape, c.needle, c.nth)
		if c.kind == refNone {
			if ok {
				t.Errorf("%q: token %+v, want none", c.needle, got)
			}
			continue
		}
		if !ok || got.typ != typ(c.kind) || got.mods != c.mods {
			t.Errorf("%q: token %+v (found %v), want type %d mods %d", c.needle, got, ok, typ(c.kind), c.mods)
		}
	}
	if _, ok := tokenAt(t, toks, semShape, "label()}", 1); ok {
		t.Errorf("an unexported method call is gopls's to colour")
	}
}

// TestShapeLegend: the Vuka types follow gopls's in the legend, and the
// declaration modifier is found by name.
func TestShapeLegend(t *testing.T) {
	p := &proxy{}
	caps := map[string]any{"semanticTokensProvider": map[string]any{"legend": map[string]any{
		"tokenTypes": []any{"namespace", "type"}, "tokenModifiers": []any{"definition", "declaration"}}}}
	p.extendCaps(caps)
	legend := caps["semanticTokensProvider"].(map[string]any)["legend"].(map[string]any)
	types := legend["tokenTypes"].([]any)
	if len(types) != 2+len(vukaTokenTypes) || types[2] != "vukaProp" || types[7] != "vukaLocal" {
		t.Fatalf("legend types %v", types)
	}
	if p.sem.base != 2 || p.sem.decl != 2 {
		t.Fatalf("base %d decl %d", p.sem.base, p.sem.decl)
	}
	ops := caps["workspace"].(map[string]any)["fileOperations"].(map[string]any)
	if ops["willRename"] == nil || caps["codeLensProvider"] == nil {
		t.Fatalf("capabilities %v", caps)
	}

	// Merged over gopls's tokens, a markup token wins where they overlap.
	vf := &vfile{source: "/m/pages/board.vuka.html", from: []byte(semShape)}
	mk := p.markupTokens(vf.text())
	tk, _ := tokenAt(t, mk, semShape, "stats.Posts", 1)
	gopls := []semToken{{tk.line, tk.char, 5, 8, 0}, {tk.line, tk.char + 6, 5, 9, 0}}
	merged := mergeTokens(gopls, mk)
	var kept []semToken
	for _, m := range merged {
		if m.line == tk.line && m.char >= tk.char && m.char <= tk.char+6 {
			kept = append(kept, m)
		}
	}
	if len(kept) != 2 || kept[0].typ == 8 && kept[1].typ == 8 {
		t.Fatalf("merged %+v", kept)
	}
	if d := decodeTokens(t, p.markupOnlyTokens(vf)); len(d) != len(mk) {
		t.Fatalf("markup-only tokens %d, want %d", len(d), len(mk))
	}
}

// TestShapeHover: holes say what they read and where it's set; on…
// attributes where their handler runs.
func TestShapeHover(t *testing.T) {
	s := scanShape([]byte(semShape))
	hover := func(needle string, delta int) string {
		t.Helper()
		h := shapeHover("/m/pages/board.vuka.html", s, strings.Index(semShape, needle)+delta)
		if h == nil {
			return ""
		}
		return h.(map[string]any)["contents"].(map[string]any)["value"].(string)
	}
	for _, c := range []struct {
		needle string
		delta  int
		want   []string
	}{
		{"stats.Posts", 1, []string{"**server state** `stats Stats`", "set in `mount` (@ui.Mount)", "Tracked"}},
		{"rows {", 1, []string{"**server state** `rows []Row`", "set in `Add`"}},
		{"Title} {", 1, []string{"**prop** `Title string`", "the `[title]` path segment, else the `title` query parameter"}},
		{"open}>Toggle", 1, []string{"**browser state**", "Set by onClick (line"}},
		{"r.Name", 0, []string{"**local** `r`", "`for _, r := range rows`"}},
		{"onClick={Add}", 2, []string{"`onClick`", "**event** `Add` · runs on the server · patches the page"}},
		{"Add}>", 0, []string{"**event** `Add`", "line 24"}},
		{"onClick={open", 2, []string{"**browser state** `open` · set in the browser"}},
		{`onClick={ui.JS`, 2, []string{"**browser commands** · run in the browser"}},
	} {
		got := hover(c.needle, c.delta)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("hover on %q: %q, want it to hold %q", c.needle, got, w)
			}
		}
	}
	compSrc := strings.Replace(semShape, "package pages", "package components", 1)
	if h := shapeHover("/m/components/board.vuka.html", scanShape([]byte(compSrc)), strings.Index(compSrc, "Title} {")); h == nil ||
		!strings.Contains(h.(map[string]any)["contents"].(map[string]any)["value"].(string), "set by the `title` attribute of a `<Board>` tag") {
		t.Errorf("a component's prop: %v", h)
	}
}

func TestLowerCamel(t *testing.T) {
	for in, want := range map[string]string{"Title": "title", "ID": "id", "URLPath": "urlPath", "OwnerID": "ownerID", "x": "x"} {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%q) = %q, want %q", in, got, want)
		}
	}
}
