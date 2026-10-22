package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestTemplateRepairs: the Go at the cursor of a template being typed is
// completed so the template compiles; | is the cursor.
func TestTemplateRepairs(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<a>{item.La|</a>\n", "<a>{item.La}</a>\n"},
		{"<a>{item.|</a>\n", "<a>{item.__vuka_complete}</a>\n"},
		{"<a>{item.La|}</a>\n", ""},
		{"<a>{|}</a>\n", "<a>{__vuka_complete}</a>\n"},
		{"<p>{fmt.Sprint(Li|</p>\n", "<p>{fmt.Sprint(Li)}</p>\n"},
		{"<p>{Shout(Title, |}</p>\n", "<p>{Shout(Title, )}</p>\n"},
		{"<p>{Title + |}</p>\n", "<p>{Title + __vuka_complete}</p>\n"},
		{`<a href={item.H|>x</a>` + "\n", `<a href={item.H}>x</a>` + "\n"},
		{"  if item.|\n  <p/>\n", "  if item. {}\n  <p/>\n"}, // the dot's placeholder is danglingDots'
		{"  if Op| {\n", ""},
		{"  if strings.HasPrefix(Title, |\n", "  if strings.HasPrefix(Title, ) {}\n"},
		{"  for _, x := range |\n", "  for _, x := range __vuka_complete {}\n"},
		{"  } else if |\n", "  } else if __vuka_complete {}\n"},
		{"  case |\n", "  case __vuka_complete:\n"},
		{"  x := |\n", "  x := __vuka_complete\n"},
		{"<p>{x}</p> and |more\n", ""},
		{"<p>{f(a[|)</p>\n", "<p>{f(a[])}</p>\n"},
		{"<script lang=\"ts\">\nconst a = {b|\n</script>\n", ""},
		{"import \"str|\"\n", ""},
		{"func navLink(item It|, on bool)\n", ""},
	} {
		cur := strings.Index(tc.in, "|")
		src := []byte(tc.in[:cur] + tc.in[cur+1:])
		ins := templateRepairs(src, cur)
		got := ""
		if ins != nil {
			out, _ := completableWith(src, ins)
			got = string(out)
			got = strings.ReplaceAll(got, "item.__vuka_complete {}", "item. {}")
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A package of a page and a component, read by bare names.
var lspGoTemplate = map[string]string{
	"sales.go": `package main

import "strings"

// Item is a menu entry.
type Item struct {
	Label string
	Href  string
}

// Shout is s, n times.
func Shout(s string, n int) string { return strings.Repeat(s, n) }

const Limit = 3

func main() {}
`,
	"navlink.vuka.html": `package main

// NavLink is a menu entry; On marks the current one.
type NavLink struct {
	Item Item
	On   bool
}
<a href={Item.Href}>{Item.Label}</a>
`,
	"sales.vuka.html": `package main

import "fmt"

// Sales is a week of sales.
type Sales struct {
	Title string
	Week  []int
	Items []Item
	Open  bool
}

// Total is the week's sales.
func (s Sales) Total() int { return len(s.Week) }
<section>
  <h2>{Title}</h2>
  for _, it := range Items {
    <NavLink item={it} on={Open}/>
  }
  <p>{Total()}</p>
  <p>{fmt.Sprint(Limit)}</p>
  for _, item := range Items {
    <a href={item.Href}>{item.Label}</a>
  }
</section>
<script lang="ts">
const n = 1
</script>
`,
}

// goTemplate opens the template of lspGoTemplate, and types: edit puts
// new for old in it, the | in new the cursor, its last character typed
// after the rest, as an editor sends it.
type goTemplate struct {
	t   *testing.T
	c   *lspClient
	uri string
	v   int
}

func startGoTemplate(t *testing.T) *goTemplate {
	if _, err := findGopls(""); err != nil {
		needs(t, "%v", err)
	}
	dir := t.TempDir()
	mod, sum := testModule(t, "lsptest")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte(sum), 0o644)
	for name, src := range lspGoTemplate {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	t.Setenv("GOWORK", "off")
	c := startLSPCaps(t, dir, lspOptions{renameCommands: true}, map[string]any{
		"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
	})
	g := &goTemplate{t: t, c: c, uri: pathToURI(filepath.Join(dir, "sales.vuka.html")), v: 1}
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": g.uri, "languageId": "vuka-html", "version": 1, "text": lspGoTemplate["sales.vuka.html"]}})
	c.waitDiags(g.uri, func(ds []any) bool { return true })
	return g
}

// edit types new for old and returns the text and the cursor.
func (g *goTemplate) edit(old, new string) (string, lspPosition) {
	g.t.Helper()
	orig := lspGoTemplate["sales.vuka.html"]
	if !strings.Contains(orig, old) {
		g.t.Fatalf("no %q", old)
	}
	text := strings.Replace(orig, old, new, 1)
	cur := strings.Index(text, "|")
	text = text[:cur] + text[cur+1:]
	send := func(change map[string]any) {
		g.v++
		g.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": g.uri, "version": g.v},
			"contentChanges": []any{change}})
	}
	send(map[string]any{"text": text[:cur-1] + text[cur:]})
	at := positionOf([]byte(text), cur-1)
	send(map[string]any{"range": lspRange{at, at}, "text": text[cur-1 : cur]})
	return text, positionOf([]byte(text), cur)
}

func (g *goTemplate) ask(method string, pos lspPosition) any {
	return g.c.call(method, map[string]any{"textDocument": map[string]any{"uri": g.uri}, "position": pos})
}

// items are a completion answer's items.
func items(v any) []map[string]any {
	if m, ok := v.(map[string]any); ok {
		v = m["items"]
	}
	var out []map[string]any
	for _, it := range asList(v) {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asList(v any) []any { l, _ := v.([]any); return l }

// applyItem is text with the completion item accepted.
func applyItem(t *testing.T, text string, item map[string]any) string {
	t.Helper()
	type ed struct {
		s, e int
		text string
	}
	var eds []ed
	add := func(v any) {
		m, _ := v.(map[string]any)
		r, ok := toRange(m["range"])
		if !ok {
			t.Fatalf("an edit without a range: %v", v)
		}
		nt, _ := m["newText"].(string)
		eds = append(eds, ed{offsetOf([]byte(text), r.Start), offsetOf([]byte(text), r.End), nt})
	}
	add(item["textEdit"])
	for _, e := range asList(item["additionalTextEdits"]) {
		add(e)
	}
	sort.Slice(eds, func(i, j int) bool { return eds[i].s > eds[j].s })
	for _, e := range eds {
		text = text[:e.s] + e.text + text[e.e:]
	}
	return text
}

// TestLSPTemplateGoCompletion: completion in a template's Go is gopls's in a
// Go file of the package: bare names are the component's fields and methods
// first, then locals, package names and builtins; after x. and pkg. their
// members; an unimported package's, adding its import to the template; and
// so in call arguments, literals, statements, headers and imports, with the
// line being typed not Go yet. Accepting an item writes the right span.
func TestLSPTemplateGoCompletion(t *testing.T) {
	g := startGoTemplate(t)
	for _, tc := range []struct {
		name, old, new string
		label          string // the item to accept
		want           string // the line it leaves, trimmed
		not            []string
	}{
		{"bare field", "<h2>{Title}</h2>", "<h2>{Ti|}</h2>", "Title", "<h2>{Title}</h2>", []string{`"s"`}},
		{"bare, nothing typed", "<h2>{Title}</h2>", "<h2>{|}</h2>", "Items", "<h2>{Items}</h2>", []string{`"s"`, `"context"`, `"io"`, `"s.Title"`}},
		{"bare method, a snippet", "<p>{Total()}</p>", "<p>{To|}</p>", "Total", "<p>{Total()}</p>", nil},
		{"a statement's bare name", "range Items {", "range It| {", "Items", "for _, it := range Items {", []string{`"s.Items"`}},
		{"a bool for if", "<p>{Total()}</p>", "if Op| {\n  <p>{Total()}</p>\n  }", "Open", "if Open {", nil},
		{"range, nothing typed", "range Items {", "range |", "Items", "for _, it := range Items", nil},
		{"a field half-typed", "{item.Label}</a>", "{item.La|</a>", "Label", `<a href={item.Href}>{item.Label</a>`, nil},
		{"an unclosed statement", "<a href={item.Href}>", "if item.|\n<a href={item.Href}>", "Href", "if item.Href", nil},
		{"an attribute's value", "href={item.Href}", "href={item.H|}", "Href", `<a href={item.Href}>{item.Label}</a>`, nil},
		{"an attribute's value half-typed", "href={item.Href}>", "href={item.H|>", "Href", `<a href={item.Href>{item.Label}</a>`, nil},
		{"a header's type", "Items []Item", "Items []It|", "Item", "Items []Item", nil},
		{"an imported package", "{fmt.Sprint(Limit)}", "{fmt.Spr|}", "Sprintf", "<p>{fmt.Sprintf()}</p>", nil},
		{"a call's argument", "{fmt.Sprint(Limit)}", "{fmt.Sprint(Li|)}", "Limit", "<p>{fmt.Sprint(Limit)}</p>", []string{`"io.LimitReader"`}},
		{"a call's argument, unclosed", "{fmt.Sprint(Limit)}</p>", "{fmt.Sprint(Li|</p>", "Limit", "<p>{fmt.Sprint(Limit</p>", nil},
		{"a composite literal", "<NavLink item={it}", "<NavLink item={Item{La|}}", "Label", "<NavLink item={Item{Label: }} on={Open}/>", nil},
		{"an index", "<p>{Total()}</p>", "<p>{Week[Li|]}</p>", "Limit", "<p>{Week[Limit]}</p>", nil},
		{"an assignment", "<p>{Total()}</p>", "x := Sh|\n  <p>{Total()}</p>", "Shout", "x := Shout()", nil},
		{"the package's import", "{fmt.Sprint(Limit)}", "{strings.ToU|}", "ToUpper", "<p>{strings.ToUpper()}</p>", nil},
	} {
		text, pos := g.edit(tc.old, tc.new)
		res := g.ask("textDocument/completion", pos)
		b, _ := json.Marshal(res)
		for _, n := range tc.not {
			if strings.Contains(string(b), `"label":`+n) {
				t.Errorf("%s: %s offered: %.600s", tc.name, n, b)
			}
		}
		var item map[string]any
		for _, it := range items(res) {
			if it["label"] == tc.label {
				item = it
				break
			}
		}
		if item == nil {
			t.Errorf("%s: no %s in %.600s", tc.name, tc.label, b)
			continue
		}
		got := applyItem(t, text, item)
		line := got[strings.LastIndexByte(got[:offsetOf([]byte(got), pos)], '\n')+1:]
		line = strings.TrimSpace(line[:strings.IndexByte(line, '\n')])
		line = snippet.ReplaceAllString(line, "")
		if line != tc.want {
			t.Errorf("%s: accepting %s leaves %q, want %q", tc.name, tc.label, line, tc.want)
		}
	}
}

// snippet matches a snippet's tab stops and placeholders.
var snippet = regexp.MustCompile(`\$\{\d+(:[^}]*)?\}|\$\d+`)

// TestLSPTemplateGoImports: an unimported package's completion adds its
// import line to the template; import paths complete in its imports; a
// script block's text is no Go.
func TestLSPTemplateGoImports(t *testing.T) {
	g := startGoTemplate(t)
	text, pos := g.edit("{fmt.Sprint(Limit)}", "{strconv.Ito|}")
	var item map[string]any
	for _, it := range items(g.ask("textDocument/completion", pos)) {
		if it["label"] == "Itoa" {
			item = it
		}
	}
	if item == nil {
		t.Fatal("no strconv.Itoa")
	}
	got := applyItem(t, text, item)
	if got = snippet.ReplaceAllString(got, ""); !strings.Contains(got, "import \"fmt\"\nimport \"strconv\"\n") || !strings.Contains(got, "<p>{strconv.Itoa()}</p>") {
		t.Errorf("accepting strconv.Itoa:\n%s", got)
	}

	// Import paths: gopls lists them once it has scanned the standard
	// library, which it starts on the first ask.
	_, pos = g.edit(`import "fmt"`, `import "strc|"`)
	deadline := time.Now().Add(30 * time.Second)
	for {
		b, _ := json.Marshal(g.ask("textDocument/completion", pos))
		if strings.Contains(string(b), `"label":"strconv"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("no import path strconv: %.400s", b)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	_, pos = g.edit("const n = 1", "const n = Ti|")
	b, _ := json.Marshal(g.ask("textDocument/completion", pos))
	if strings.Contains(string(b), `"label":"Title"`) {
		t.Errorf("Go offered in a script: %.300s", b)
	}
}

// TestLSPTemplateGoSignature: signature help in a hole's call, a
// statement's, an attribute's and a call still being typed.
func TestLSPTemplateGoSignature(t *testing.T) {
	g := startGoTemplate(t)
	for _, tc := range []struct{ name, old, new, want string }{
		{"a hole", "{fmt.Sprint(Limit)}", "{Shout(Title, |)}", "Shout(s string, n int) string"},
		{"a hole being typed", "{fmt.Sprint(Limit)}</p>", "{Shout(Title, |</p>", "Shout(s string, n int) string"},
		{"a statement", "<p>{Total()}</p>", "if Shout(Title, |) != \"\" {\n  <p>{Total()}</p>\n  }", "Shout(s string, n int) string"},
		{"a statement being typed", "<p>{Total()}</p>", "if fmt.Sprint(|\n  <p>{Total()}</p>", "Sprint(a ...any) string"},
		{"an attribute", "href={item.Href}", "href={Shout(item.Href, |)}", "Shout(s string, n int) string"},
	} {
		_, pos := g.edit(tc.old, tc.new)
		b, _ := json.Marshal(g.ask("textDocument/signatureHelp", pos))
		if !strings.Contains(string(b), tc.want) || tc.want[0] == 'S' && tc.want[1] == 'h' && !strings.Contains(string(b), `"activeParameter":1`) {
			t.Errorf("%s: %.400s", tc.name, b)
		}
	}
}

// TestLSPTemplateGoDiagnostics: gopls's errors in a hole, a statement, an
// element's attribute and a component's attribute land on the template's
// text.
func TestLSPTemplateGoDiagnostics(t *testing.T) {
	g := startGoTemplate(t)
	src := strings.NewReplacer("<h2>{Title}</h2>", "<h2>{Title + 1}</h2>",
		"it := range Items {", "it := range Limits {", "href={item.Href}", "href={item.Ref}", "on={Open}", "on={Opened}").Replace(lspGoTemplate["sales.vuka.html"])
	g.v++
	g.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": g.uri, "version": g.v},
		"contentChanges": []any{map[string]any{"text": src}}})
	want := map[string]string{"Title + 1": "mismatched types", "Limits": "undefined: Limits", "Ref": "item.Ref undefined", "Opened": "undefined: Opened"}
	ds := g.c.waitDiags(g.uri, func(ds []any) bool { return len(ds) >= len(want) })
	for needle, msg := range want {
		at := positionOf([]byte(src), strings.Index(src, needle))
		found := false
		for _, d := range ds {
			m, _ := d.(map[string]any)
			r, _ := toRange(m["range"])
			text, _ := m["message"].(string)
			if strings.Contains(text, msg) && r.Start.Line == at.Line && r.Start.Character <= at.Character+uint32(len(needle)) && r.End.Character >= at.Character {
				found = true
			}
		}
		if !found {
			b, _ := json.Marshal(ds)
			t.Errorf("no %q at %v: %s", msg, at, b)
		}
	}
}

// TestTemplateImportEdits: a template file takes an added import as a line
// of its own, after its imports or its package clause; into its group when
// it groups them.
func TestTemplateImportEdits(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"package main\n\n<p/>\n", "package main\n\nimport \"strconv\"\n\n<p/>\n"},
		{"package main\n\nimport \"fmt\"\n\n<p/>\n", "package main\n\nimport \"fmt\"\nimport \"strconv\"\n\n<p/>\n"},
		{"package main\n\nimport (\n\t\"fmt\"\n)\n\n<p/>\n", "package main\n\nimport (\n\t\"fmt\"\n\t\"strconv\"\n)\n\n<p/>\n"},
	} {
		f := &vfile{source: "x.vuka.html", from: []byte(tc.in)}
		if got := applyImportEdits(tc.in, sourceImportEdits(f, []importSpec{{path: "strconv"}})); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}
