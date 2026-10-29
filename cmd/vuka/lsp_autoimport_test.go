package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const autoButton = `<script lang="go">
package design

// Button runs an action.
type Button struct {
	Label   string
	Primary bool
}
</script>

<button class="btn">{Label}</button>
`

const autoCard = `<script lang="go">
package design

type Card struct {
	Title    string
	Children Node
}
</script>

<div class="card"><h2>{Title}</h2>{Children}</div>
`

const autoChip = "package widgets\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Chip is a small label.\nfunc Chip(text string) ui.Node { return nil }\n"

// autoModule writes a module with component packages: components/ui
// declaring package design, widgets named as its directory, and a pages
// tree, whose routes are no components to import.
func autoModule(t *testing.T) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	mod, _ := testModule(t, "example.com/shop")
	writeFile(t, filepath.Join(root, "go.mod"), mod)
	writeFile(t, filepath.Join(root, "components", "ui", "button.vuka.html"), autoButton)
	writeFile(t, filepath.Join(root, "components", "ui", "card.vuka.html"), autoCard)
	writeFile(t, filepath.Join(root, "widgets", "chip.go"), autoChip)
	writeFile(t, filepath.Join(root, "pages", "about", "about.vuka.html"), "<script lang=\"go\">\npackage about\n\ntype About struct{}\n</script>\n\n<main></main>\n")
	return root
}

// tagItems completes the tag at the end of before in the file at path,
// whose text is before+after, as JSON round-trips the items.
func tagItems(t *testing.T, path, before, after string) (string, []map[string]any) {
	t.Helper()
	src := before + after
	vf := &vfile{source: path, from: []byte(src)}
	spot, ok := completionSpot([]byte(src), len(before))
	if !ok || spot.kind != "tag" {
		t.Fatalf("no tag spot at %q", before)
	}
	typed := lspRange{positionOf([]byte(src), spot.from), positionOf([]byte(src), len(before))}
	var items []map[string]any
	b, _ := json.Marshal(unimportedTagItems(vf, spot, typed))
	_ = json.Unmarshal(b, &items)
	return src, items
}

func accept(src string, it map[string]any) string {
	edits := []any{it["textEdit"]}
	if extra, ok := it["additionalTextEdits"].([]any); ok {
		edits = append(edits, extra...)
	}
	return applyEdits(src, edits)
}

func TestUnimportedComponentPackages(t *testing.T) {
	root := autoModule(t)
	page := filepath.Join(root, "pages", "home", "home.vuka.html")
	head := "<script lang=\"go\">\npackage home\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n)\n\ntype Home struct{ ui.Live }\n\nfunc (h *Home) title() string { return fmt.Sprint(\"x\") }\n</script>\n\n<main>\n  "
	src, items := tagItems(t, page, head+"<des", "\n</main>\n")
	design := itemNamed(items, "design")
	if design == nil {
		t.Fatalf("<des offers no design: %v", items)
	}
	if design["detail"] != "example.com/shop/components/ui" || design["textEdit"].(map[string]any)["newText"] != "design." ||
		design["command"].(map[string]any)["command"] != "editor.action.triggerSuggest" || design["additionalTextEdits"] != nil {
		t.Errorf("the package item: %v", design)
	}
	if got := accept(src, design); !strings.Contains(got, "<design.\n") {
		t.Errorf("accepting the package: %q", got)
	}
	if w := itemNamed(items, "widgets"); w == nil || w["detail"] != "example.com/shop/widgets" {
		t.Errorf("no widgets: %v", items)
	}
	if itemNamed(items, "about") != nil || itemNamed(items, "home") != nil || itemNamed(items, "ui") != nil {
		t.Errorf("a route's package, the file's own or one it imports is offered: %v", items)
	}

	src, items = tagItems(t, page, head+"<design.", "\n</main>\n")
	btn := itemNamed(items, "Button")
	if btn == nil || itemNamed(items, "Card") == nil {
		t.Fatalf("<design. offers no components: %v", items)
	}
	if d, _ := btn["detail"].(string); !strings.Contains(d, "label string") {
		t.Errorf("Button's detail: %q", d)
	}
	if doc := btn["documentation"].(map[string]any)["value"].(string); !strings.Contains(doc, "Button runs an action.") {
		t.Errorf("Button's documentation: %q", doc)
	}
	got := accept(src, btn)
	want := "import (\n\t\"fmt\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n\n\tdesign \"example.com/shop/components/ui\"\n)"
	if !strings.Contains(got, want) || !strings.Contains(got, "<design.Button\n") {
		t.Errorf("accepting Button:\n%s\nwant the import in the module's group:\n%s", got, want)
	}

	// Imported already: the components are gopls's, with no edit.
	imported := strings.Replace(head, "\t\"github.com/vuka-lang/ui/v2\"\n", "\t\"github.com/vuka-lang/ui/v2\"\n\n\tdesign \"example.com/shop/components/ui\"\n", 1)
	if _, items := tagItems(t, page, imported+"<design.", "\n</main>\n"); len(items) != 0 {
		t.Errorf("an imported package's components: %v", items)
	}
	if _, items := tagItems(t, page, imported+"<des", "\n</main>\n"); itemNamed(items, "design") != nil {
		t.Errorf("an imported package offered again: %v", items)
	}
}

func TestUnimportedComponentNoImports(t *testing.T) {
	root := autoModule(t)
	page := filepath.Join(root, "pages", "home", "home.vuka.html")
	head := "<script lang=\"go\">\npackage home\n\ntype Home struct{}\n</script>\n\n<main>\n  "
	src, items := tagItems(t, page, head+"<widgets.", "\n</main>\n")
	chip := itemNamed(items, "Chip")
	if chip == nil {
		t.Fatalf("<widgets. offers no Chip: %v", items)
	}
	if chip["kind"].(float64) != 3 {
		t.Errorf("a function component's kind: %v", chip["kind"])
	}
	got := accept(src, chip)
	if !strings.Contains(got, "package home\n\nimport \"example.com/shop/widgets\"\n\ntype Home") || !strings.Contains(got, "<widgets.Chip\n") {
		t.Errorf("accepting Chip with no imports:\n%s", got)
	}

	// The ui kit, a module go.mod requires.
	src, items = tagItems(t, page, head+"<ui.", "\n</main>\n")
	badge := itemNamed(items, "Badge")
	if badge == nil {
		t.Fatalf("<ui. offers no kit components: %v", items)
	}
	if got := accept(src, badge); !strings.Contains(got, "import \"github.com/vuka-lang/ui/v2\"\n") {
		t.Errorf("accepting ui.Badge:\n%s", got)
	}
	_, items = tagItems(t, page, head+"<", "\n</main>\n")
	var order []string
	for _, it := range items {
		order = append(order, it["sortText"].(string)+" "+it["label"].(string))
	}
	if d, u := itemNamed(items, "design"), itemNamed(items, "ui"); d == nil || u == nil || d["sortText"].(string) > u["sortText"].(string) {
		t.Errorf("the module's packages rank before the kit's: %v", order)
	}
}

// TestUnimportedComponentVuka: a .vuka file's markup takes the import into
// its import block.
func TestUnimportedComponentVuka(t *testing.T) {
	root := autoModule(t)
	file := filepath.Join(root, "admin", "admin.vuka")
	head := "package admin\n\nimport \"strings\"\n\nfunc Panel(name string) ui.Node {\n\treturn <section>{strings.ToUpper(name)}\n\t\t"
	src, items := tagItems(t, file, head+"<design.Ca", "\n\t</section>\n}\n")
	card := itemNamed(items, "Card")
	if card == nil {
		t.Fatalf("no Card: %v", items)
	}
	got := accept(src, card)
	if !strings.Contains(got, "import (\n\t\"strings\"\n\n\tdesign \"example.com/shop/components/ui\"\n)") {
		t.Errorf("accepting Card in a .vuka file:\n%s", got)
	}
}

func TestInsertGrouped(t *testing.T) {
	for _, tc := range []struct{ in, path, want string }{
		{"package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/x/y\"\n)\n", "example.com/m/a",
			"package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/x/y\"\n\n\t\"example.com/m/a\"\n)\n"},
		{"package p\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/m/b\"\n)\n", "example.com/m/a",
			"package p\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/m/a\"\n\t\"example.com/m/b\"\n)\n"},
		{"package p\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/m/b\"\n)\n", "github.com/x/y",
			"package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/x/y\"\n\n\t\"example.com/m/b\"\n)\n"},
		{"package p\n\nimport (\n\t\"github.com/x/y\"\n)\n", "bytes",
			"package p\n\nimport (\n\t\"bytes\"\n\n\t\"github.com/x/y\"\n)\n"},
		{"package p\n\nimport (\n\t\"fmt\"\n\t\"example.com/m/a\"\n)\n", "example.com/m/a", "package p\n\nimport (\n\t\"fmt\"\n\t\"example.com/m/a\"\n)\n"},
	} {
		got := applyImportEdits(tc.in, addImportsIn([]byte(tc.in), []importSpec{{path: tc.path}}, "example.com/m"))
		if got != tc.want {
			t.Errorf("adding %s to %q:\ngot  %q\nwant %q", tc.path, tc.in, got, tc.want)
		}
	}
}

// TestLSPComponentAutoImport: <design. in a template file of a module
// whose components/ui declares package design lists its components through
// the proxy; the one accepted adds the import, and the file builds.
func TestLSPComponentAutoImport(t *testing.T) {
	page := "<script lang=\"go\">\npackage main\n\nimport \"strings\"\n\ntype Home struct{}\n\nfunc (h Home) title() string { return strings.ToUpper(\"home\") }\n</script>\n\n<main>\n  <h1>{title()}</h1>\n</main>\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n",
		"home.vuka.html": page, "components/ui/button.vuka.html": autoButton})
	uri := pathToURI(filepath.Join(dir, "home.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": page}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	typing := strings.Replace(page, "</h1>\n", "</h1>\n  <design.\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": typing}}})
	off := strings.Index(typing, "<design.") + len("<design.")
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(typing), off), "context": map[string]any{"triggerKind": 2, "triggerCharacter": "."}})
	b, _ := json.Marshal(v)
	var list struct{ Items []map[string]any }
	_ = json.Unmarshal(b, &list)
	btn := itemNamed(list.Items, "Button")
	if btn == nil {
		t.Fatalf("no Button at <design.: %.1500s", b)
	}
	if os.Getenv("VUKA_SHOW_ITEM") != "" {
		pretty, _ := json.MarshalIndent(btn, "", "  ")
		t.Logf("%s", pretty)
	}
	done := accept(typing, btn)
	if !strings.Contains(done, "import \"strings\"\nimport design \"lsptest/components/ui\"\n") {
		t.Fatalf("the import added:\n%s", done)
	}
	errs := func(ds []any) (out []string) {
		for _, d := range ds {
			if m, _ := d.(map[string]any); m["severity"] == float64(1) {
				out = append(out, m["message"].(string))
			}
		}
		return out
	}
	// A wrong prop first, so the clean publication after is of the file
	// accepted.
	bad := strings.Replace(done, "<design.Button\n", "<design.Button label={nope} />\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": bad}}})
	c.waitDiags(uri, func(ds []any) bool { return strings.Contains(strings.Join(errs(ds), "\n"), "nope") })
	done = strings.Replace(done, "<design.Button\n", "<design.Button label=\"Go\" />\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 4},
		"contentChanges": []any{map[string]any{"text": done}}})
	c.waitDiags(uri, func(ds []any) bool { return len(errs(ds)) == 0 })
}

// TestLSPGoUnimportedComponentPackage: in a hole and in the script's Go,
// gopls offers a package of .vuka.html files only, unimported, its import
// edit landing in the template's imports.
func TestLSPGoUnimportedComponentPackage(t *testing.T) {
	page := "<script lang=\"go\">\npackage main\n\nimport \"strings\"\n\ntype Home struct{}\n\nfunc (h Home) title() string { return strings.ToUpper(\"home\") }\n</script>\n\n<main>\n  <h1>{title()}</h1>\n</main>\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n",
		"home.vuka.html": page, "components/ui/button.vuka.html": autoButton})
	uri := pathToURI(filepath.Join(dir, "home.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": page}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	v := 1
	for _, tc := range []struct{ old, new, at string }{
		{"<h1>{title()}</h1>", "<h1>{title()}{design.B}</h1>", "{design.B"},
		{"return strings.ToUpper(\"home\") }", "_ = design.B\n\treturn strings.ToUpper(\"home\") }", "_ = design.B"},
	} {
		src := strings.Replace(page, tc.old, tc.new, 1)
		v++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": v},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, tc.at) + len(tc.at)
		var btn map[string]any
		var b []byte
		// gopls indexes the module's packages in the background.
		for deadline := time.Now().Add(20 * time.Second); btn == nil && time.Now().Before(deadline); {
			b, _ = json.Marshal(c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
				"position": positionOf([]byte(src), off)}))
			var list struct{ Items []map[string]any }
			_ = json.Unmarshal(b, &list)
			if btn = itemNamed(list.Items, "Button"); btn == nil {
				time.Sleep(200 * time.Millisecond)
			}
		}
		if btn == nil {
			t.Errorf("%s: no Button: %.800s", tc.at, b)
			continue
		}
		if got := accept(src, btn); !strings.Contains(got, "import \"strings\"\nimport design \"lsptest/components/ui\"\n") {
			t.Errorf("%s: the import:\n%s", tc.at, got)
		}
	}
}
