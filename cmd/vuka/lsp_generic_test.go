package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// genericShape is a generic live component with a hook and @ui.URL fields.
const genericShape = `package components

import "context"

type Table[T any] struct {
	Rows  []T
	Sort  string @ui.URL("sort")
	page  int    @ui.URL("page").Default(1)
	shown int
}

@ui.Mount
func (t *Table[T]) mount(ctx context.Context) error {
	t.shown = len(t.Rows)
	return nil
}

func (t *Table[T]) Next() { t.page++ }
<p onClick={Next}>{shown} rows, page {page}, sort {Sort}</p>
`

// TestGenericShapeEditor: on a generic shape, the fields carrying
// attributes still get their tokens and hover, and @ui.URL its own hover.
func TestGenericShapeEditor(t *testing.T) {
	const base, decl = 30, 1
	s := scanShape([]byte(genericShape))
	toks := shapeTokens(s, base, decl)
	typ := func(k refKind) uint32 { return base + uint32(k-refProp) }
	for _, c := range []struct {
		needle string
		kind   refKind
		mods   uint32
	}{
		{"Sort  string", refProp, decl},
		{"page  int", refState, decl},
		{"shown int", refState, decl},
		{"page++", refState, 0},
		{"page}", refState, 0},
		{"Sort}", refProp, 0},
		{"shown}", refState, 0},
		{"Next}>", refEvent, 0},
	} {
		got, ok := tokenAt(t, toks, genericShape, c.needle, 1)
		if !ok || got.typ != typ(c.kind) || got.mods != c.mods {
			t.Errorf("%q: token %+v (found %v), want type %d mods %d", c.needle, got, ok, typ(c.kind), c.mods)
		}
	}
	h := shapeHover("/m/components/table.vuka.html", s, strings.Index(genericShape, "page}"))
	if h == nil || !strings.Contains(h.(map[string]any)["contents"].(map[string]any)["value"].(string), "**server state** `page int`") {
		t.Errorf("hover on page: %v", h)
	}
	h = shapeHover("/m/components/table.vuka.html", s, strings.Index(genericShape, "shown}"))
	if h == nil || !strings.Contains(h.(map[string]any)["contents"].(map[string]any)["value"].(string), "set in `mount` (@ui.Mount)") {
		t.Errorf("hover on shown: %v", h)
	}
	as := urlAttrs([]byte(genericShape))
	if len(as) != 2 || as[1].field != "page" || as[1].def != "1" || !strings.Contains(urlHover(as[1]), "`?page=`") {
		t.Fatalf("%+v", as)
	}
}

// TestLSPGenericShape drives the proxy and gopls on a generic component
// with a hook and @ui.URL fields: no diagnostics, the fields' tokens, and
// hover on a hole and on @ui.URL.
func TestLSPGenericShape(t *testing.T) {
	app := routesModule(t, map[string]string{"components/table.vuka.html": genericShape})
	c := startLSPAt(t, app, lspOptions{})
	legend := semTokLegendFrom(t, c.init)
	path := filepath.Join(app, "components", "table.vuka.html")
	uri := pathToURI(path)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": genericShape}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	toks := decodeTokens(t, c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}}).(map[string]any))
	for _, c := range []struct{ needle, typ string }{{"page}", "vukaState"}, {"Sort}", "vukaProp"}, {"shown}", "vukaState"}, {"Next}>", "vukaEvent"}} {
		if tk, ok := tokenAt(t, toks, genericShape, c.needle, 1); !ok || tk.typ != legend[c.typ] {
			t.Errorf("%s: %+v (found %v), want %s", c.needle, tk, ok, c.typ)
		}
	}
	hover := func(needle string, delta int) string {
		t.Helper()
		pos := positionOf([]byte(genericShape), strings.Index(genericShape, needle)+delta)
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
		if h == nil {
			return ""
		}
		return h["contents"].(map[string]any)["value"].(string)
	}
	if h := hover("shown}", 0); !strings.Contains(h, "set in `mount` (@ui.Mount)") {
		t.Errorf("hover on {shown}: %q", h)
	}
	if h := hover(`@ui.URL("page")`, 2); !strings.Contains(h, "`?page=`") {
		t.Errorf("hover on @ui.URL: %q", h)
	}
}
