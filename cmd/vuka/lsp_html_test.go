package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
)

const lspHTMLGo = `package main

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
}

func (s Sales) Total() int { return len(s.Week) }

func main() {}
`

const lspHTMLTemplate = `package main

<section>
  <h2>{Title}</h2>
  for _, d := range Week {
    <i>{d}</i>
  }
  <p>{Total()}</p>
  <row n={1}/>
</section>

func row(n int)
<p>{n}</p>
`

// TestLSPTemplate: hover and definition in a .vuka.html land on the Go its
// bare names and holes are.
func TestLSPTemplate(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate})
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
	if !strings.Contains(string(def), "sales.go") {
		t.Errorf("definition of Total: %s", def)
	}
	def, _ = json.Marshal(c.call("textDocument/definition", at("{n}", 1)))
	if !strings.Contains(string(def), "sales.vuka.html") {
		t.Errorf("definition of n: %s", def)
	}
	// A partial used as a tag is its template.
	def, _ = json.Marshal(c.call("textDocument/definition", at("<row", 2)))
	if !strings.Contains(string(def), "sales.vuka.html") {
		t.Errorf("definition of <row>: %s", def)
	}
	hover, _ = json.Marshal(c.call("textDocument/hover", at("<row", 2)))
	if !strings.Contains(string(hover), "func row(n int)") {
		t.Errorf("hover on <row>: %s", hover)
	}
}

const lspHTMLTyping = `package main

<section>
  <h2>{Title}</h2>
  <ro
  <row 
  <inp
  <a tar
  <input type="ch
  <textarea bi
</section>

func row(n int)
<p>{n}</p>
`

// TestLSPTemplateHTML: completion in a .vuka.html's markup is HTML's: the
// elements after <, an element's attributes in its tag, an attribute's
// values in its quotes.
func TestLSPTemplateHTML(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": lspHTMLGo, "sales.vuka.html": lspHTMLTemplate})
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
	for needle, want := range map[string]string{"<ro": `"row"`, "<row ": `"n"`, "<inp": `"input"`, "<a tar": `"target"`, `type="ch`: `"checkbox"`, "<textarea bi": `"bind:value"`} {
		b, _ := json.Marshal(c.call("textDocument/completion", at(needle)))
		if !strings.Contains(string(b), want) {
			t.Errorf("completion at %s: no %s in %.300s", needle, want, b)
		}
	}
}

// A package of template files read by their bare names, one of them with a
// syntax error in one of its templates.
var lspHTMLPackage = map[string]string{
	"layout.go": `package main

const Name = "Shop"

// Document is the page around every page.
type Document struct {
	Title string
}

// Item is a menu entry.
type Item struct {
	Label string
	Href  string
}

func main() {}
`,
	"document.vuka.html": `package main

import "lsptest/components"

<html>
  <head><title>{Title + " · " + Name}</title></head>
  <body>
    <components.Icon glyph={components.Home} size={16} />
    <navLink item={Item{Label: "Home"}} on={true} />
    <Footer />
  </body>
</html>

// navLink is a menu entry; on marks the current one.
func navLink(item Item, on bool)
<a href={item.Href} class={item.Label}>{item.Label}</a>

// Footer is the pages' footer line.
func Footer()
<p>Copyright © 2014</p>
`,
	"menu.vuka.html": `package main

func broken(item Item)
<p><span>{item.Label}</p>

func Menu(items []Item)
<ul>
  for _, it := range items {
    <li>{it.Label}</li>
  }
</ul>
`,
	"components/icon.go": `package components

// Glyph is an icon's drawing.
type Glyph struct{ Path string }

var Home = Glyph{"M0 0"}

// Icon draws its glyph.
type Icon struct {
	Glyph Glyph
	Size  int
	Label string
}

func (i Icon) Path() string { return i.Glyph.Path }
`,
	"components/icon.vuka.html": `package components

<svg width={Size} aria-label={Label}><path d={Path()} /></svg>
`,
}

// TestLSPTemplatePackage: in template files read by their bare names, with
// a package clause, partials and components of another package, hover,
// definition and completion land on the Go, while another template file of
// the package has a syntax error; and in that file, outside the template
// with the error.
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
	check := func(method, needle string, delta int, want ...string) {
		t.Helper()
		b, _ := json.Marshal(c.call(method, at(uri, doc, needle, delta)))
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s at %q+%d: no %s in %.400s", method, needle, delta, w, b)
			}
		}
	}
	check("textDocument/hover", "{Title +", 2, "Title string")
	check("textDocument/definition", "{Title +", 2, "layout.go")
	check("textDocument/hover", "+ Name}", 3, "Name")
	check("textDocument/completion", "{Title +", 3, `"Title"`)
	// A partial: its parameters, and their fields.
	check("textDocument/hover", "class={item.Label}", 14, "Label string")
	check("textDocument/definition", "class={item.Label}", 14, "layout.go")
	check("textDocument/completion", "class={item.Label}", 12, `"Label"`, `"Href"`)
	// Its tag: the template, and its parameters as attributes.
	check("textDocument/hover", "<navLink", 2, "func navLink(item Item, on bool)")
	check("textDocument/definition", "<navLink", 2, "document.vuka.html")
	check("textDocument/hover", "<navLink item", 10, "item Item")
	check("textDocument/definition", "<navLink item", 10, "document.vuka.html")
	check("textDocument/completion", "<navLink ", 9, `"item"`) // on is given
	// A struct component of another package: its fields as attributes.
	check("textDocument/hover", "glyph={", 1, "Glyph")
	check("textDocument/definition", "glyph={", 1, "icon.go")
	check("textDocument/completion", "size={16} ", 10, `"label"`)

	menu := lspHTMLPackage["menu.vuka.html"]
	muri := pathToURI(filepath.Join(dir, "menu.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": muri, "languageId": "vuka-html", "version": 1, "text": menu}})
	c.waitDiags(muri, func(ds []any) bool { b, _ := json.Marshal(ds); return strings.Contains(string(b), "E0101") })
	b, _ := json.Marshal(c.call("textDocument/hover", at(muri, menu, "{it.Label}", 5)))
	if !strings.Contains(string(b), "Label string") {
		t.Errorf("hover in the template without the error: %s", b)
	}
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
	src := "package main\n\n<p>{PetFields.Name}</p>\n"
	c, dir, _ := startLSPWith(t, map[string]string{"pets.go": "package main\n\ntype Pets struct{}\n\nfunc main() {}\n", "pets.vuka.html": src})
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

// Implicit ctx is the render context in main markup and named helpers, and
// remains visible to gopls through the generated closure's source map.
func TestLSPTemplateContext(t *testing.T) {
	const source = `package main

type Sales struct { Title string }
type contextKey struct{}
func main() {}
`
	const template = `package main

value := ctx.Value(contextKey{})
<section><h1>{Title}</h1><p>{value}</p><contextBadge/></section>

func contextBadge()
<i>{ctx.Err()}</i>
`
	c, dir, _ := startLSPWith(t, map[string]string{"sales.go": source, "sales.vuka.html": template})
	uri := pathToURI(filepath.Join(dir, "sales.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": template}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	for _, needle := range []string{"ctx.Value", "ctx.Err"} {
		hover, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(template), strings.Index(template, needle))}))
		if !strings.Contains(string(hover), "context.Context") {
			t.Errorf("hover on %s: %s", needle, hover)
		}
	}
}
