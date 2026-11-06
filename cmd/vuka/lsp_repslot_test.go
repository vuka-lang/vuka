package main

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// repTable is a generic table whose columns are a repeated slot: an
// element struct mixing a Node field, a func of two values and plain fields.
const repTable = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Column is one of a DataTable's columns.
type Column[T any] struct {
	// Header is the column's heading.
	Header ui.Node
	// Cell renders the column for one row.
	Cell func(T, int) ui.Node
	// Sort is the key the column sorts by.
	Sort   string
	Align  string
	Width  string
	Hidden bool
	Mobile string
}

// DataTable lists rows in columns.
type DataTable[T any] struct {
	Rows    []T
	Columns []Column[T]
	// Actions is a row's menu.
	Actions     func(T) ui.Node
	Title       string
	Toolbar     ui.Node
	Bulk        ui.Node
	Empty       ui.Node
	Note        ui.Node
	Description ui.Node
	Index       bool
}
</script>

<table>
  <tr>
    for _, c := range Columns {
      <th>{c.Header}</th>
    }
  </tr>
  for i, r := range Rows {
    <tr>
      for _, c := range Columns {
        <td>{c.Cell(r, i)}</td>
      }
      <td>{Actions(r)}</td>
    </tr>
  }
</table>
`

const repPage = `<script lang="go">
package pages

import "lsptest/components"

// Order is a row.
type Order struct{ Number string }

type Index struct{ Orders []Order }
</script>

<main>
  <components.DataTable rows={Orders} index>
    <slot:column header="Number" sort="number" let={t, n}><b>{t.Number}</b>{n}</slot:column>
  </components.DataTable>
</main>
`

// repProxy is a module holding repTable, and the page using it.
func repProxy(t *testing.T) (*proxy, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(root, "go.mod"), "module lsptest\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "components", "data_table.vuka.html"), repTable)
	p := &proxy{bufs: map[string][]byte{}, importDirs: map[string]string{}, modRoot: root, modPath: "lsptest"}
	return p, filepath.Join(root, "pages", "index.vuka.html")
}

func itemLabels(items []any) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.(map[string]any)["label"].(string))
	}
	sort.Strings(out)
	return out
}

// TestRepeatedSlotCompletion: a repeated slot's tag completes its element's
// fields but the func its content fills, and let, never an element's
// attributes; the parent lists the slot once beside its Node and func
// slots; inside it, <slot: lists the element's slots its tag doesn't set.
func TestRepeatedSlotCompletion(t *testing.T) {
	p, page := repProxy(t)
	for _, c := range []struct {
		tag  string
		want []string
	}{
		{`<slot:column >`, []string{"align", "header", "hidden", "let", "mobile", "sort", "width"}},
		{`<slot:column header="Number" sort="number" let={t, n} >`, []string{"align", "hidden", "mobile", "width"}},
	} {
		text := strings.Replace(repPage, `<slot:column header="Number" sort="number" let={t, n}>`, c.tag, 1)
		items, ok := p.slotAttrCompletion(page, []byte(text), strings.Index(text, c.tag)+len(c.tag)-1)
		if got := itemLabels(items); !ok || !slices.Equal(got, c.want) {
			t.Errorf("attributes in %s: %v, want %v", c.tag, got, c.want)
		}
	}

	parent := strings.Replace(repPage, "</slot:column>\n", "</slot:column>\n    <slot:\n", 1)
	items, ok := p.slotCompletion(page, []byte(parent), strings.Index(parent, "    <slot:\n")+len("    <slot:"))
	if got := itemLabels(items); !ok || !slices.Equal(got, []string{"slot:actions", "slot:bulk", "slot:column", "slot:description", "slot:empty", "slot:note", "slot:toolbar"}) {
		t.Errorf("DataTable's slots: %v", got)
	}
	for _, c := range []struct {
		tag  string
		want []string
	}{
		{`<slot:column header="Number" sort="number" let={t, n}>`, []string{"slot:cell"}},
		{`<slot:column sort="number">`, []string{"slot:cell", "slot:header"}},
	} {
		text := strings.Replace(repPage, `<slot:column header="Number" sort="number" let={t, n}><b>{t.Number}</b>{n}`, c.tag+"<slot:\n", 1)
		items, ok := p.slotCompletion(page, []byte(text), strings.Index(text, "<slot:\n")+len("<slot:"))
		if got := itemLabels(items); !ok || !slices.Equal(got, c.want) {
			t.Errorf("slots inside %s: %v, want %v", c.tag, got, c.want)
		}
	}
}

// TestRepeatedSlotHover: hover on a repeated slot's tag names its slice, its
// element's attributes and its let's values; on an attribute, the element
// field; on let, the values of the element's func.
func TestRepeatedSlotHover(t *testing.T) {
	p, page := repProxy(t)
	src := []byte(repPage)
	s := scanShape(page, src)
	hover := func(needle string, d int, h func(string, []byte, *shapeScan, int) any) string {
		m, _ := h(page, src, s, strings.Index(repPage, needle)+d).(map[string]any)
		return slotHoverText(m)
	}
	for _, c := range []struct {
		needle string
		h      func(string, []byte, *shapeScan, int) any
		want   []string
	}{
		{"slot:column header", p.slotHover, []string{"**repeated slot** `<slot:column>` · `Columns []Column[T]` of `<components.DataTable>`", "its attributes `header`, `sort`, `align`, `width`, `hidden`, `mobile`.", "`let={t, n}` names `t T`, `n int`"}},
		{`header="Number"`, p.slotAttrHover, []string{"**field** `Header ui.Node` of `<slot:column>`'s element", "#L9)", "Header is the column's heading."}},
		{`sort="number"`, p.slotAttrHover, []string{"**field** `Sort string`", "Sort is the key the column sorts by."}},
		{"let={t, n}", p.slotAttrHover, []string{"names the values its content takes, in order: `T`, `int`, of `Cell func(T, int) ui.Node`"}},
	} {
		md := hover(c.needle, 2, c.h)
		for _, w := range c.want {
			if !strings.Contains(md, w) {
				t.Errorf("hover on %s lacks %q: %s", c.needle, w, md)
			}
		}
	}
}

// TestLSPRepeatedSlotInferred: through the server, a generic table's type
// argument inferred from rows: a repeated slot's tag completes exactly its
// element's attributes, and its let's values hover as the inferred type.
func TestLSPRepeatedSlotInferred(t *testing.T) {
	files := map[string]string{"main.go": "package main\n\nimport _ \"lsptest/pages\"\n\nfunc main() {}\n", "components/data_table.vuka.html": repTable, "pages/index.vuka.html": repPage}
	c, dir, _ := startLSPWith(t, files)
	uri := pathToURI(filepath.Join(dir, "pages", "index.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": repPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	hover := func(needle string, d int) string {
		pos := positionOf([]byte(repPage), strings.Index(repPage, needle)+d)
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
		return slotHoverText(h)
	}
	var md string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if md = hover("{t.Number}", 1); strings.Contains(md, "var t Order") {
			break
		}
	}
	if !strings.Contains(md, "**slot value** `t Order` · `let` of `<slot:column>`: value 1 of `Cell func(T, int) ui.Node`") {
		t.Errorf("hover on t: %s", md)
	}
	if md := hover("{n}", 1); !strings.Contains(md, "`n int`") {
		t.Errorf("hover on n: %s", md)
	}
	if md := hover("slot:column header", 6); !strings.Contains(md, "`let={t, n}` names `t Order`, `n int`") {
		t.Errorf("hover on the column slot: %s", md)
	}
	typing := strings.Replace(repPage, `let={t, n}>`, `let={t, n} >`, 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": typing}}})
	got := keys(completionItems(c, uri, typing, `let={t, n} `))
	sort.Strings(got)
	if !slices.Equal(got, []string{"align", "hidden", "mobile", "width"}) {
		t.Errorf("attributes in the column slot's tag: %v", got)
	}
}
