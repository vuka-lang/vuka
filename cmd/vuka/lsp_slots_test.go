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
	Items   []T
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

// TestSlotCompletion: <slot: inside a component's tag offers its slots —
// a Node, a func returning one (with a let for its values) and a slice of
// structs (in the singular) — and nothing outside one.
func TestSlotCompletion(t *testing.T) {
	p, page := slotsProxy(t)
	src := []byte(slotsPageSrc)
	at := strings.Index(slotsPageSrc, "<slot:\n") + len("<slot:")
	items, ok := p.slotCompletion(page, src, at)
	if !ok {
		t.Fatal("no completion after <slot:")
	}
	got := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		got[m["label"].(string)] = m
	}
	if len(got) != 3 || got["slot:title"] != nil || got["slot:items"] != nil {
		t.Fatalf("Board's slots: %v", got)
	}
	row := got["slot:row"]
	if row["detail"] != "func(T, int) ui.Node" || row["labelDetails"].(map[string]any)["description"] != "required scoped slot" {
		t.Errorf("the row slot: %v", row)
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
	outside := strings.Replace(slotsPageSrc, "<p>{t}</p>", "<slot:", 1)
	if _, ok := p.slotCompletion(page, []byte(outside), strings.LastIndex(outside, "<slot:")+len("<slot:")); ok {
		t.Error("a slot offered outside a component's tag")
	}
}

// TestLetScope: a slot's let declares locals for its content only, tokens
// and hover included.
func TestLetScope(t *testing.T) {
	p, page := slotsProxy(t)
	src := []byte(slotsPageSrc)
	s := scanShape(src)
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
