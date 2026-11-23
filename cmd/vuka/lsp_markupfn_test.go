package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const markupFnPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Order is one of the orders reviewed.
type Order struct {
	ID     int
	Number string
}

// Review lists its orders through a markup func.
type Review struct {
	ui.Live
	Orders []Order
}

// Approve approves an order.
func (p *Review) Approve(id int) error { return nil }
</script>

row := func(o Order) {
  <li>{o.Number} <button onClick={Aprove(o.ID)}>approve</button></li>
}

<ul>
  for _, o := range Orders {
    {row(o)}
  }
</ul>
`

// TestLSPMarkupFuncs: a markup func's body is markup as the main markup is
// to the editor: a diagnostic inside it covers its place in the body, and
// hover and definition there answer as they do in the main markup.
func TestLSPMarkupFuncs(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "review.vuka.html": markupFnPage})
	uri := pathToURI(filepath.Join(dir, "review.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": markupFnPage}})
	ds := c.waitDiags(uri, func(ds []any) bool { return len(ds) > 0 })
	at := positionOf([]byte(markupFnPage), strings.Index(markupFnPage, "Aprove"))
	got := jsonOf(ds)
	want := `"start":{"character":` + itoaTest(at.Character) + `,"line":` + itoaTest(at.Line) + `}`
	if !strings.Contains(got, `"code":"E0172"`) || !strings.Contains(got, want) {
		t.Fatalf("diagnostics %s, want E0172 at %s", got, want)
	}

	fixed := strings.Replace(markupFnPage, "Aprove", "Approve", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": fixed}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	pos := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(fixed), strings.Index(fixed, needle)+delta)}
	}
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if h, _ := c.call("textDocument/hover", pos("Approve(o.ID)", 2)).(map[string]any); h != nil {
			if md = h["contents"].(map[string]any)["value"].(string); strings.Contains(md, "Approve(id int)") {
				break
			}
		}
	}
	if !strings.Contains(md, "func (p *Review) Approve(id int) error") {
		t.Errorf("hover on the method inside the body: %s", md)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", pos("o.Number}", 0)))
	if line := `"line":` + itoaTest(positionOf([]byte(fixed), strings.Index(fixed, "o Order) {")).Line); !strings.Contains(string(def), line) {
		t.Errorf("definition of the parameter: %s, want %s", def, line)
	}
}

const markupFnBadge = `<script lang="go">
package main

// Tone is a Badge's tone.
type Tone string

const (
	ToneCalm Tone = "calm"
	ToneLoud Tone = "loud"
)

// Badge is a labelled tone.
type Badge struct {
	Tone  Tone
	Label string
}
</script>

<span>{Label}</span>
`

const markupFnSales = `<script lang="go">
package main

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
}
</script>

row := func(d int) {
  <li>{d} {Title} <Badge tone="calm" label={Title}/></li>
}

<section>
  for _, d := range Week {
    {row(d)}
  }
</section>
`

// TestLSPMarkupFuncAuthoring: inside a markup func's body the editor
// completes as in the main markup — tags, attributes, an attribute's
// values, a component prop's constants and the shape's names — goes from a
// call to the binding, and the outline shows the binding holding its body.
func TestLSPMarkupFuncAuthoring(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "sales.vuka.html": markupFnSales, "badge.vuka.html": markupFnBadge})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": markupFnSales}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(text, needle string, delta int) map[string]any {
		t.Helper()
		i := strings.Index(text, needle)
		if i < 0 {
			t.Fatalf("no %q", needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(text), i+delta)}
	}

	def, _ := json.Marshal(c.call("textDocument/definition", at(markupFnSales, "{row(d)}", 1)))
	if line := `"line":` + itoaTest(positionOf([]byte(markupFnSales), strings.Index(markupFnSales, "row :=")).Line); !strings.Contains(string(def), line) {
		t.Errorf("definition of row from its call: %s, want %s", def, line)
	}
	if h := jsonOf(c.call("textDocument/hover", at(markupFnSales, "row :=", 1))); !strings.Contains(h, "func(d int) ui.Node") {
		t.Errorf("hover on the binding: %s", h)
	}
	if d := jsonOf(c.call("textDocument/definition", at(markupFnSales, "<Badge", 2))); !strings.Contains(d, "badge.vuka.html") {
		t.Errorf("definition of <Badge> in the body: %s", d)
	}
	var names []string
	for _, s := range asList(c.call("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": uri}})) {
		m := s.(map[string]any)
		names = append(names, m["name"].(string)+":"+jsonOf(m["kind"]))
	}
	if got := strings.Join(names, " "); !strings.Contains(got, "row:12") {
		t.Errorf("outline %s: no row", got)
	}

	typing := strings.Replace(markupFnSales, `<li>{d} {Title} <Badge tone="calm" label={Title}/></li>`, `<li>
    <Bad
    <Badge to
    <Badge tone="
    <input type="ch
    {Tit
  </li>`, 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": typing}}})
	for needle, want := range map[string]string{"<Bad": `"Badge"`, "<Badge to": `"tone"`, `tone="`: `"loud"`, `type="ch`: `"checkbox"`, "{Tit": `"Title"`} {
		if got := jsonOf(c.call("textDocument/completion", at(typing, needle, len(needle)))); !strings.Contains(got, want) {
			t.Errorf("completion at %s in the body: no %s in %.300s", needle, want, got)
		}
	}
}

// TestMarkupFuncOutline: in the outline a markup func's binding holds its
// body's elements, beside the markup's own.
func TestMarkupFuncOutline(t *testing.T) {
	var got []string
	var walk func([]map[string]any, string)
	walk = func(syms []map[string]any, ind string) {
		for _, s := range syms {
			got = append(got, ind+s["name"].(string))
			var kids []map[string]any
			for _, k := range asList(s["children"]) {
				kids = append(kids, k.(map[string]any))
			}
			walk(kids, ind+"  ")
		}
	}
	walk(markupRoots([]byte(markupFnSales)), "")
	if s := strings.Join(got, "|"); s != "script|row|  li|section" {
		t.Errorf("outline %s", s)
	}
}
