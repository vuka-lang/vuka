package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const slotsBoard = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Board lists tasks through its caller's slots.
type Board[T any] struct {
	Items []T
	// Row renders one task.
	Row     func(T, int) ui.Node ` + "`vuka:\"required\"`" + `
	Columns []Column[T]
	Actions ui.Node
	Title   string
}
</script>

<section>{Title}</section>
`

const slotsColumn = `package components

import "github.com/vuka-lang/ui/v2"

// Column is one of a Board's columns.
type Column[T any] struct {
	Header ui.Node
	Cell   func(T) ui.Node
}
`

const slotsPageSrc = `<script lang="go">
package pages

import "example.com/app/components"

type Index struct{ Tasks []Task }
</script>

<main>
  <components.Board items={Tasks}>
    <slot:row let={t, i}>{i}: {t.Title}</slot:row>
    <slot:
  </components.Board>
  <p>{t}</p>
</main>
`

func slotsProxy(t *testing.T) (*proxy, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "components", "board.vuka.html"), slotsBoard)
	writeFile(t, filepath.Join(root, "components", "column.go"), slotsColumn)
	p := &proxy{bufs: map[string][]byte{}, importDirs: map[string]string{}, modRoot: root, modPath: "example.com/app"}
	return p, filepath.Join(root, "pages", "index.vuka.html")
}

// slotItems completes <slot: at the first "<slot:\n" of page.
func slotItems(t *testing.T, p *proxy, path, page string) map[string]map[string]any {
	t.Helper()
	at := strings.Index(page, "<slot:\n") + len("<slot:")
	items, ok := p.slotCompletion(path, []byte(page), at)
	if !ok {
		t.Fatal("no completion after <slot:")
	}
	got := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		got[m["label"].(string)] = m
	}
	return got
}

// TestSlotCompletion: <slot: inside a component's tag offers its slots —
// a Node, a func returning one (with a let for its values) and a slice of
// structs (in the singular) — with their docs, but those the tag already
// fills, and nothing outside one.
func TestSlotCompletion(t *testing.T) {
	p, page := slotsProxy(t)
	got := slotItems(t, p, page, slotsPageSrc)
	if len(got) != 2 || got["slot:row"] != nil || got["slot:column"] == nil || got["slot:actions"] == nil {
		t.Fatalf("Board's slots but row, which the tag fills: %v", keys(got))
	}
	unfilled := strings.Replace(slotsPageSrc, "    <slot:row let={t, i}>{i}: {t.Title}</slot:row>\n", "    <slot:column header=\"a\" let={t}>{t}</slot:column>\n", 1)
	got = slotItems(t, p, page, unfilled)
	if len(got) != 3 || got["slot:title"] != nil || got["slot:items"] != nil {
		t.Fatalf("Board's slots: %v", got)
	}
	row := got["slot:row"]
	if row["detail"] != "func(T, int) ui.Node" || row["labelDetails"].(map[string]any)["description"] != "required scoped slot" {
		t.Errorf("the row slot: %v", row)
	}
	if doc, _ := row["documentation"].(map[string]any); doc["value"] != "Row renders one task." {
		t.Errorf("the row slot's doc: %v", row["documentation"])
	}
	if text := row["textEdit"].(map[string]any)["newText"]; text != "slot:row let={${1:t, i}}>$0</slot:row>" {
		t.Errorf("the row slot's text: %v", text)
	}
	col := got["slot:column"]
	if col == nil || col["labelDetails"].(map[string]any)["description"] != "repeated slot" || col["textEdit"].(map[string]any)["newText"] != "slot:column let={${1:t}}>$0</slot:column>" {
		t.Errorf("the column slot: %v", col)
	}
	if a := got["slot:actions"]; a == nil || a["textEdit"].(map[string]any)["newText"] != "slot:actions>$0</slot:actions>" {
		t.Errorf("the actions slot: %v", a)
	}
	outside := strings.Replace(unfilled, "<p>{t}</p>", "<slot:", 1)
	if _, ok := p.slotCompletion(page, []byte(outside), strings.LastIndex(outside, "<slot:")+len("<slot:")); ok {
		t.Error("a slot offered outside a component's tag")
	}
}

// TestLetScope: a slot's let declares locals for its content only, tokens
// and hover included.
func TestLetScope(t *testing.T) {
	p, page := slotsProxy(t)
	src := []byte(slotsPageSrc)
	s := scanShape("", src)
	decl, ok := s.refAt(strings.Index(slotsPageSrc, "t, i}"))
	if !ok || decl.kind != refLocal || !decl.decl || decl.local.slot != "slot:row" || decl.local.comp != "components.Board" {
		t.Fatalf("the let's t: %+v (%v)", decl, ok)
	}
	use, ok := s.refAt(strings.Index(slotsPageSrc, "t.Title"))
	if !ok || use.kind != refLocal || use.local != decl.local {
		t.Errorf("t in the slot's content: %+v", use)
	}
	if r, ok := s.refAt(strings.Index(slotsPageSrc, "{t}</p>") + 1); ok && r.kind == refLocal {
		t.Errorf("t after the slot is still the let's: %+v", r)
	}
	if note := p.letNote(page, src, use); note != "**slot value** `t T` · `let` of `<slot:row>`: value 1 of `func(T, int) ui.Node`, what `<components.Board>` passes" {
		t.Errorf("note on t: %s", note)
	}
	i, _ := s.refAt(strings.Index(slotsPageSrc, "{i}") + 1)
	if note := p.letNote(page, src, i); !strings.Contains(note, "`i int`") {
		t.Errorf("note on i: %s", note)
	}
	toks := shapeTokens(s, 100, 1)
	pos := positionOf(src, decl.start)
	found := false
	for _, tk := range toks {
		if tk.line == pos.Line && tk.char == pos.Character {
			found = tk.typ == 100+uint32(refLocal-refProp) && tk.mods == 1
		}
	}
	if !found {
		t.Errorf("no local declaration token on the let's t: %v", toks)
	}
}

// TestRequiredProp: a field tagged vuka:"required" is a required prop.
func TestRequiredProp(t *testing.T) {
	p, page := slotsProxy(t)
	info := p.resolveComponent(page, []byte(slotsPageSrc), "components.Board")
	if info == nil {
		t.Fatal("Board not found")
	}
	for _, pr := range info.props {
		if (pr.attr == "row") != pr.required {
			t.Errorf("%s required: %v", pr.attr, pr.required)
		}
	}
	if md := info.markdown("components.Board"); !strings.Contains(md, "`<slot:column>`") || !strings.Contains(md, "`<slot:row>`") {
		t.Errorf("hover's slots: %s", md)
	}
}

const slotsDiagCard = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Card has an actions slot.
type Card struct {
	Actions  ui.Node
	Children ui.Node
}
</script>

<div>{Actions}{Children}</div>
`

const slotsDiagPage = `<script lang="go">
package main

// Page misspells Card's slot.
type Page struct{}
</script>

<Card>
  <slot:actoins>x</slot:actoins>
</Card>
`

// TestLSPSlotDiagnostics: a slot the component has no field for is
// reported as the file is typed, with the nearest slot's name.
func TestLSPSlotDiagnostics(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "card.vuka.html": slotsDiagCard, "page.vuka.html": slotsDiagPage})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": slotsDiagPage}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "E0106")
	})
	line := strings.Count(slotsDiagPage[:strings.Index(slotsDiagPage, "<slot:")], "\n")
	for _, want := range []string{`"code":"E0106"`, `\u003cCard\u003e has no slot actoins; did you mean \u003cslot:actions\u003e?`, `"start":{"character":2,"line":` + itoaTest(uint32(line)) + `}`} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics lack %s: %s", want, got)
		}
	}
}

const slotsHoverList = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// List renders names through its caller's Row.
type List struct {
	Names []string
	Row   func(string, int) ui.Node
}
</script>

<ul>
  for i, n := range Names {
    <li>{Row(n, i)}</li>
  }
</ul>
`

const slotsHoverPage = `<script lang="go">
package main

// Page fills List's Row.
type Page struct{ Names []string }
</script>

<List names={Names}>
  <slot:row let={name, i}><b>{name}</b>{i}</slot:row>
</List>
`

// TestLSPLetHover: hover on a name a slot's let declares says what it is
// and gopls gives its type, through the function the slot compiles to.
func TestLSPLetHover(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "list.vuka.html": slotsHoverList, "page.vuka.html": slotsHoverPage})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": slotsHoverPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		pos := positionOf([]byte(slotsHoverPage), strings.Index(slotsHoverPage, "{name}")+2)
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
		if h != nil {
			md = h["contents"].(map[string]any)["value"].(string)
			if strings.Contains(md, "var name string") {
				break
			}
		}
	}
	for _, want := range []string{"**slot value** `name string` · `let` of `<slot:row>`", "var name string"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover on name lacks %q: %s", want, md)
		}
	}
}

// TestSlotHover: hover on a slot's tag, opening or closing, shows the field
// it fills, its doc and the types of the values its let names.
func TestSlotHover(t *testing.T) {
	t.Parallel()
	p, page := slotsProxy(t)
	src := []byte(slotsPageSrc)
	s := scanShape(page, src)
	for _, at := range []int{strings.Index(slotsPageSrc, "slot:row let") + 2, strings.Index(slotsPageSrc, "</slot:row>") + 4} {
		h, _ := p.slotHover(page, src, s, at).(map[string]any)
		if h == nil {
			t.Fatalf("no hover at %d", at)
		}
		md := h["contents"].(map[string]any)["value"].(string)
		for _, want := range []string{"**required scoped slot** `<slot:row>` · `Row func(T, int) ui.Node` of `<components.Board>`", "[board.vuka.html](", "#L10)", "Row renders one task.", "`let={t, i}` names `t T`, `i int`"} {
			if !strings.Contains(md, want) {
				t.Errorf("hover at %d lacks %q: %s", at, want, md)
			}
		}
	}
	if h := p.slotHover(page, src, s, strings.Index(slotsPageSrc, "{t.Title}")+2); h != nil {
		t.Errorf("hover on a slot's content: %v", h)
	}
}

// TestSlotDefinition: definition on a slot's name goes to the field it
// fills, a repeated slot's to its slice.
func TestSlotDefinition(t *testing.T) {
	t.Parallel()
	p, page := slotsProxy(t)
	board := filepath.Join(p.modRoot, "components", "board.vuka.html")
	for slot, want := range map[string]lspRange{
		"<slot:row let={t, i}>{i}: {t.Title}</slot:row>": {lspPosition{9, 1}, lspPosition{9, 4}},
		"<slot:column header=\"a\">x</slot:column>":      {lspPosition{10, 1}, lspPosition{10, 8}},
		"<slot:actions>x</slot:actions>":                 {lspPosition{11, 1}, lspPosition{11, 8}},
	} {
		page2 := strings.Replace(slotsPageSrc, "<slot:row let={t, i}>{i}: {t.Title}</slot:row>", slot, 1)
		loc, ok := p.slotDefinition(page, []byte(page2), scanShape(page, []byte(page2)), strings.Index(page2, slot)+3)
		if !ok || loc["uri"] != pathToURI(board) || loc["range"] != want {
			t.Errorf("definition of %s: %v, want %v", slot, loc, want)
		}
	}
}

const slotsPanelTempl = `package panel

type PanelProps struct {
	Title string
	// Header is the panel's top bar.
	Header templ.Component
	// Footer sits under the body.
	Footer templ.Component
}

templ Panel(p PanelProps) {
	<section>
		if p.Header != nil {
			@p.Header
		}
		{ children... }
		if p.Footer != nil {
			@p.Footer
		}
	</section>
}
`

const slotsLibList = `<script lang="go">
package lists

import "github.com/vuka-lang/ui/v2"

// List renders names through its caller's Row.
type List struct {
	Names []string
	// Row is one name's line.
	Row func(string, int) ui.Node
}
</script>

<ul>
  for i, n := range Names {
    <li>{Row(n, i)}</li>
  }
</ul>
`

const slotsLibPage = `<script lang="go">
package pages

import (
	"lsptest/components/lists"
	"lsptest/components/panel"
)

type Index struct{ Names []string }
</script>

<main>
  <panel.Panel title="Orders">
    <slot:header><b>Orders</b></slot:header>
    <slot:footr>more</slot:footr>
  </panel.Panel>
  <lists.List names={Names}>
    <slot:row let={name, i}><b>{name}</b>{i}</slot:row>
  </lists.List>
</main>
`

// TestLSPSlotsLibrary: a templ library's component and another package's
// component take the same slot support as the page's own: an unknown slot
// is E0106 with the nearest name, <slot: completes the slots not filled
// yet with their docs, hover on a slot's tag shows its field, definition
// goes to it, and a let's names complete in the slot's content.
func TestLSPSlotsLibrary(t *testing.T) {
	t.Parallel()
	files := map[string]string{"main.go": "package main\n\nimport _ \"lsptest/pages\"\n\nfunc main() {}\n",
		"components/panel/panel.templ": slotsPanelTempl, "components/lists/list.vuka.html": slotsLibList, "pages/index.vuka.html": slotsLibPage}
	c, dir, _ := startLSPWith(t, templFiles(t, files))
	uri := pathToURI(filepath.Join(dir, "pages", "index.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": slotsLibPage}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(jsonOf(ds))
		return strings.Contains(got, "E0106")
	})
	if !strings.Contains(got, `has no slot footr; did you mean <slot:footer>?`) {
		t.Errorf("the unknown slot's diagnostic: %s", got)
	}

	typing := strings.Replace(slotsLibPage, "<slot:footr>more</slot:footr>", "<slot:", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": typing}}})
	items := completionItems(c, uri, typing, "</slot:header>\n    <slot:")
	if _, ok := items["slot:header"]; ok || items["slot:footer"] == nil {
		t.Fatalf("Panel's slots but header: %v", keys(items))
	}
	if doc := itemDoc(items["slot:footer"]); doc != "Footer sits under the body." {
		t.Errorf("footer's doc: %q", doc)
	}
	valid := strings.Replace(slotsLibPage, "<slot:footr>more</slot:footr>", "<slot:footer>more</slot:footer>", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []any{map[string]any{"text": valid}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	pos := positionOf([]byte(valid), strings.Index(valid, "slot:header")+6)
	h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
	if md := slotHoverText(h); !strings.Contains(md, "`Header templ.Component` of `<panel.Panel>`") || !strings.Contains(md, "the panel's top bar") {
		t.Errorf("hover on the header slot: %s", md)
	}
	defs, _ := c.call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).([]any)
	if len(defs) != 1 || !strings.HasSuffix(defs[0].(map[string]any)["uri"].(string), "panel.templ") || jsonOf(defs[0].(map[string]any)["range"]) != `{"end":{"character":7,"line":5},"start":{"character":1,"line":5}}` {
		t.Errorf("definition of the header slot: %v", defs)
	}
	pos = positionOf([]byte(valid), strings.Index(valid, "slot:row")+6)
	h, _ = c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
	if md := slotHoverText(h); !strings.Contains(md, "`let={name, i}` names `name string`, `i int`") || !strings.Contains(md, "one name's line") {
		t.Errorf("hover on the row slot: %s", md)
	}

	var names map[string]map[string]any
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if names = completionItems(c, uri, valid, "<b>{na"); names["name"] != nil {
			break
		}
	}
	if names["name"] == nil || !strings.Contains(jsonOf(names["name"]), "string") {
		t.Errorf("the let's name doesn't complete in the slot: %v", keys(names))
	}
}

func slotHoverText(h map[string]any) string {
	c, _ := h["contents"].(map[string]any)
	v, _ := c["value"].(string)
	return v
}

const slotsGenericList = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Order is a row.
type Order struct{ Number string }

// List renders its items through its caller's Row.
type List[T any] struct {
	Items []T
	Row   func(T, int) ui.Node
}
</script>

<ul>
  for i, it := range Items {
    <li>{Row(it, i)}</li>
  }
</ul>
`

const slotsGenericPage = `<script lang="go">
package main

// Page fills List's Row; List's T is inferred from items.
type Page struct{ Orders []Order }
</script>

<List items={Orders}>
  <slot:row let={o, i}><b>{o.Number}</b>{i}</slot:row>
</List>
`

// TestLSPInferredLetHover: hover on a slot's let of a generic component
// whose type arguments are inferred names the inferred type, not T.
func TestLSPInferredLetHover(t *testing.T) {
	t.Parallel()
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "list.vuka.html": slotsGenericList, "page.vuka.html": slotsGenericPage})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": slotsGenericPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		pos := positionOf([]byte(slotsGenericPage), strings.Index(slotsGenericPage, "{o.Number}")+1)
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
		if h != nil {
			md = h["contents"].(map[string]any)["value"].(string)
			if strings.Contains(md, "var o Order") {
				break
			}
		}
	}
	for _, want := range []string{"**slot value** `o Order` · `let` of `<slot:row>`: value 1 of `func(T, int) ui.Node`", "var o Order"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover on o lacks %q: %s", want, md)
		}
	}
	pos := positionOf([]byte(slotsGenericPage), strings.Index(slotsGenericPage, "slot:row")+6)
	h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
	if md := slotHoverText(h); !strings.Contains(md, "`let={o, i}` names `o Order`, `i int`") {
		t.Errorf("hover on the row slot lacks the inferred type: %s", md)
	}
}
