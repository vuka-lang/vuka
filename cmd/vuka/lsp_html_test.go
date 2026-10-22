package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
)

const lspHTMLGo = `package main

func main() {}
`

const lspHTMLTemplate = `package main

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
}

func (s Sales) Total() int { return len(s.Week) }
<section>
  <h2>{Title}</h2>
  for _, d := range Week {
    <i>{d}</i>
  }
  <p>{Total()}</p>
  <Row n={1}/>
</section>
`

const lspHTMLRow = `package main

// Row is a row of a number.
type Row struct{ N int }
<p>{N}</p>
`

// TestLSPTemplate: hover and definition in a .vuka.html land on the Go its
// bare names and holes are, in its header or in a component's file.
func TestLSPTemplate(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate, "row.vuka.html": lspHTMLRow})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspHTMLTemplate}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspHTMLTemplate), strings.Index(lspHTMLTemplate, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("{Title}", 1)))
	if !strings.Contains(string(hover), "Title string") {
		t.Errorf("hover on Title: %s", hover)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at("{Total()}", 1)))
	if !strings.Contains(string(def), "sales.vuka.html") || !strings.Contains(string(def), `"line":8`) {
		t.Errorf("definition of Total: %s", def)
	}
	def, _ = json.Marshal(c.call("textDocument/definition", at("{d}", 1)))
	if !strings.Contains(string(def), "sales.vuka.html") {
		t.Errorf("definition of d: %s", def)
	}
	// A component used as a tag is its file's struct.
	def, _ = json.Marshal(c.call("textDocument/definition", at("<Row", 2)))
	if !strings.Contains(string(def), "row.vuka.html") {
		t.Errorf("definition of <Row>: %s", def)
	}
	hover, _ = json.Marshal(c.call("textDocument/hover", at("<Row", 2)))
	if !strings.Contains(string(hover), "Row") {
		t.Errorf("hover on <Row>: %s", hover)
	}
	def, _ = json.Marshal(c.call("textDocument/definition", at("<Row n", 5)))
	if !strings.Contains(string(def), "row.vuka.html") {
		t.Errorf("definition of <Row n>: %s", def)
	}
}

const lspHTMLTyping = `package main

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
}

func (s Sales) Total() int { return len(s.Week) }
<section>
  <h2>{Title}</h2>
  <Ro
  <Row 
  <inp
  <a tar
  <input type="ch
  <textarea bi
</section>
`

// TestLSPTemplateHTML: completion in a .vuka.html's markup is HTML's: the
// elements after <, an element's attributes in its tag, an attribute's
// values in its quotes.
func TestLSPTemplateHTML(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate, "row.vuka.html": lspHTMLRow})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspHTMLTemplate}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": lspHTMLTyping}}})
	at := func(needle string) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspHTMLTyping), strings.Index(lspHTMLTyping, needle)+len(needle))}
	}
	for needle, want := range map[string]string{"<Ro": `"Row"`, "<Row ": `"n"`, "<inp": `"input"`, "<a tar": `"target"`, `type="ch`: `"checkbox"`, "<textarea bi": `"bind:value"`} {
		b, _ := json.Marshal(c.call("textDocument/completion", at(needle)))
		if !strings.Contains(string(b), want) {
			t.Errorf("completion at %s: no %s in %.300s", needle, want, b)
		}
	}
}

// A package of template files read by their bare names, one of them with a
// syntax error in its markup.
var lspHTMLPackage = map[string]string{
	"layout.go": `package main

const Name = "Shop"

// Item is a menu entry.
type Item struct {
	Label string
	Href  string
}

func main() {}
`,
	"document.vuka.html": `package main

import "lsptest/components"

// Document is the page around every page.
type Document struct {
	Title string
}
<html>
  <head><title>{Title + " · " + Name}</title></head>
  <body>
    <components.Icon glyph={components.Home} size={16} />
    <NavLink item={Item{Label: "Home"}} on={true} />
    <Footer />
  </body>
</html>
`,
	"navlink.vuka.html": `package main

// NavLink is a menu entry; On marks the current one.
type NavLink struct {
	Item Item
	On   bool
}
<a href={Item.Href} class={Item.Label}>{Item.Label}</a>
`,
	"footer.vuka.html": `package main

// Footer is the pages' footer line.
type Footer struct{}
<p>Copyright © 2014</p>
`,
	"broken.vuka.html": `package main

// Broken is a menu entry with an unclosed span.
type Broken struct{ Item Item }
<p><span>{Item.Label}</p>
`,
	"menu.vuka.html": `package main

// Menu lists items.
type Menu struct{ Items []Item }
<ul>
  for _, it := range Items {
    <li>{it.Label}</li>
  }
</ul>
`,
	"components/icon.go": `package components

// Glyph is an icon's drawing.
type Glyph struct{ Path string }

var Home = Glyph{"M0 0"}
`,
	"components/icon.vuka.html": `package components

// Icon draws its glyph.
type Icon struct {
	Glyph Glyph
	Size  int
	Label string
}

func (i Icon) Path() string { return i.Glyph.Path }
<svg width={Size} aria-label={Label}><path d={Path()} /></svg>
`,
}

// TestLSPTemplatePackage: in template files read by their bare names, with
// components of the package and of another package, hover, definition and
// completion land on the Go, while another template file of the package has
// a syntax error.
func TestLSPTemplatePackage(t *testing.T) {
	c, dir, _ := startLSPWith(t, lspHTMLPackage)
	doc := lspHTMLPackage["document.vuka.html"]
	uri := pathToURI(filepath.Join(dir, "document.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": doc}})
	// The first diagnostics are the syntax check's, of the file as a template.
	first, _ := json.Marshal(c.waitDiags(uri, func(ds []any) bool { return true }))
	if string(first) != "[]" {
		t.Errorf("syntax errors in a template with none: %s", first)
	}
	at := func(uri, text, needle string, delta int) map[string]any {
		i := strings.Index(text, needle)
		if i < 0 {
			t.Fatalf("no %q", needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(text), i+delta)}
	}
	check := func(uri, text, method, needle string, delta int, want ...string) {
		t.Helper()
		b, _ := json.Marshal(c.call(method, at(uri, text, needle, delta)))
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s at %q+%d: no %s in %.400s", method, needle, delta, w, b)
			}
		}
	}
	check(uri, doc, "textDocument/hover", "{Title +", 2, "Title string")
	check(uri, doc, "textDocument/definition", "{Title +", 2, "document.vuka.html")
	check(uri, doc, "textDocument/hover", "+ Name}", 3, "Name")
	check(uri, doc, "textDocument/definition", "+ Name}", 3, "layout.go")
	check(uri, doc, "textDocument/completion", "{Title +", 3, `"Title"`)
	// A component of the package: its tag is its struct, its attributes its fields.
	check(uri, doc, "textDocument/hover", "<NavLink", 2, "NavLink")
	check(uri, doc, "textDocument/definition", "<NavLink", 2, "navlink.vuka.html")
	check(uri, doc, "textDocument/hover", "<NavLink item", 10, "Item Item")
	check(uri, doc, "textDocument/definition", "<NavLink item", 10, "navlink.vuka.html")
	check(uri, doc, "textDocument/completion", "<NavLink ", 9, `"item"`)
	check(uri, doc, "textDocument/definition", "<Footer", 2, "footer.vuka.html")
	// A struct component of another package: its fields as attributes.
	check(uri, doc, "textDocument/hover", "glyph={", 1, "Glyph")
	check(uri, doc, "textDocument/definition", "glyph={", 1, "icon.vuka.html")
	check(uri, doc, "textDocument/completion", "size={16} ", 10, `"label"`)

	// In a component's own markup: its fields, and theirs.
	nav := lspHTMLPackage["navlink.vuka.html"]
	nuri := pathToURI(filepath.Join(dir, "navlink.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": nuri, "languageId": "vuka-html", "version": 1, "text": nav}})
	c.waitDiags(nuri, func(ds []any) bool { return true })
	check(nuri, nav, "textDocument/hover", "class={Item.Label}", 12, "Label string")
	check(nuri, nav, "textDocument/definition", "class={Item.Label}", 12, "layout.go")
	check(nuri, nav, "textDocument/completion", "class={Item.Label}", 12, `"Label"`, `"Href"`)

	broken := lspHTMLPackage["broken.vuka.html"]
	buri := pathToURI(filepath.Join(dir, "broken.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": buri, "languageId": "vuka-html", "version": 1, "text": broken}})
	c.waitDiags(buri, func(ds []any) bool { b, _ := json.Marshal(ds); return strings.Contains(string(b), "E0101") })
	menu := lspHTMLPackage["menu.vuka.html"]
	muri := pathToURI(filepath.Join(dir, "menu.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": muri, "languageId": "vuka-html", "version": 1, "text": menu}})
	c.waitDiags(muri, func(ds []any) bool { return true })
	check(muri, menu, "textDocument/hover", "{it.Label}", 5, "Label string")
}

// TestLSPAfterPackage: the Go a package needs beyond its own, the ORM's typed
// fields the vuka command adds after each package (load.AfterPackage), is
// gopls's too: hover on it.
func TestLSPAfterPackage(t *testing.T) {
	after := load.AfterPackage
	t.Cleanup(func() { load.AfterPackage = after })
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		for _, g := range gens {
			if strings.Contains(string(g.Src), "PetFields.") {
				return []load.Generated{{Target: filepath.Join(dir, "pet_gen.go"),
					Src: []byte("package main\n\n// PetFields are a pet's typed fields.\nvar PetFields = struct{ Name string }{}\n")}}
			}
		}
		return nil
	}
	src := "package main\n\ntype Pets struct{}\n<p>{PetFields.Name}</p>\n"
	c, dir, _ := startLSPWith(t, map[string]string{"pets.go": "package main\n\nfunc main() {}\n", "pets.vuka.html": src})
	uri := pathToURI(filepath.Join(dir, "pets.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	b, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), strings.Index(src, "PetFields")+1)}))
	if !strings.Contains(string(b), "typed fields") {
		t.Errorf("hover on the package's added Go: %s", b)
	}
}

// Implicit ctx is the render context in a page's markup and in a
// component's, and remains visible to gopls through the generated closure's
// source map.
func TestLSPTemplateContext(t *testing.T) {
	const source = `package main

type contextKey struct{}

func main() {}
`
	const template = `package main

type Sales struct{ Title string }
<section>
  value := ctx.Value(contextKey{})
  <h1>{Title}</h1><p>{value}</p><ContextBadge/>
</section>
`
	const badge = `package main

type ContextBadge struct{}
<i>{ctx.Err()}</i>
`
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": source, "sales.vuka.html": template, "badge.vuka.html": badge})
	for name, text := range map[string]string{"sales.vuka.html": template, "badge.vuka.html": badge} {
		uri := pathToURI(filepath.Join(dir, name))
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": text}})
		c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
		needle := "ctx.Value"
		if name == "badge.vuka.html" {
			needle = "ctx.Err"
		}
		hover, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(text), strings.Index(text, needle))}))
		if !strings.Contains(string(hover), "context.Context") {
			t.Errorf("hover on %s: %s", needle, hover)
		}
	}
}

// TestLSPOneWay: markup only in a .vuka.html file, below the struct it
// renders, one template a file: markup in a .vuka file or in a header is
// E0151 at its first tag, markup with no struct above it E0152, a func
// after the markup E0153 on its line.
func TestLSPOneWay(t *testing.T) {
	files := map[string]string{
		"main.go":            "package main\n\nfunc main() {}\n",
		"logic.vuka":         "package main\n\nimport \"github.com/vuka-lang/ui\"\n\nfunc card(title string) ui.Node {\n\treturn <div class=\"card\">{title}</div>\n}\n",
		"header.vuka.html":   "package main\n\nimport \"github.com/vuka-lang/ui\"\n\ntype Header struct{ Title string }\n\nfunc (h Header) Badge() ui.Node { return <b>{h.Title}</b> }\n<h1>{Title}</h1>\n",
		"nostruct.vuka.html": "package main\n\n<p>no struct</p>\n",
		"two.vuka.html":      "package main\n\ntype Two struct{ N int }\n<p>{N}</p>\n\nfunc item(n int)\n<li>{n}</li>\n",
	}
	c, dir, _ := startLSPWith(t, files)
	for _, tc := range []struct{ file, code, at string }{
		{"logic.vuka", "E0151", `<div class="card">`},
		{"header.vuka.html", "E0151", "<b>{h.Title}"},
		{"nostruct.vuka.html", "E0152", "<p>no struct"},
		{"two.vuka.html", "E0153", "func item"},
	} {
		src := files[tc.file]
		uri := pathToURI(filepath.Join(dir, tc.file))
		lang := "vuka-html"
		if strings.HasSuffix(tc.file, ".vuka") {
			lang = "vuka"
		}
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": lang, "version": 1, "text": src}})
		ds := c.waitDiags(uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), `"code":"`+tc.code+`"`) })
		want := positionOf([]byte(src), strings.Index(src, tc.at))
		found := false
		for _, d := range ds {
			m, _ := d.(map[string]any)
			r, _ := toRange(m["range"])
			if m["code"] == tc.code && r.Start == want {
				found = true
			}
		}
		if !found || len(ds) != 1 {
			t.Errorf("%s: want only %s at %v: %s", tc.file, tc.code, want, jsonOf(ds))
		}
	}
}
