package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const authoringData = `package main

type Todo struct {
	ID    int
	Title string
}

type Tag struct{ Label string }

func main() {}
`

const authoringShape = `package main

import "github.com/vuka-lang/ui/v2"

// List is the todos.
type List struct {
	Todos []Todo
	Tags  []Tag
	Open  bool
}

// Count is how many todos there are.
func (l List) Count() int { return len(l.Todos) }
<section class="list wide" id="todos">
  <h2>{Count()}</h2>
  <ul>
    for _, t := range Todos {
      <li key={t.ID}>{t.Title}</li>
    }
  </ul>
  <nav ref="menu" hidden>
    <button onClick={ui.JS.Toggle(ui.Ref("menu"))}>Menu</button>
  </nav>
</section>
`

// authoring opens list.vuka.html in a server taking snippets and an outline
// with children.
type authoring struct {
	t   *testing.T
	c   *lspClient
	dir string
	uri string
	v   int
}

func startAuthoring(t *testing.T) *authoring {
	if _, err := findGopls(""); err != nil {
		needs(t, "%v", err)
	}
	dir := t.TempDir()
	mod, sum := testModule(t, "lsptest")
	for name, src := range map[string]string{"go.mod": mod, "go.sum": sum, "data.go": authoringData, "list.vuka.html": authoringShape} {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	t.Setenv("GOWORK", "off")
	c := startLSPCaps(t, dir, lspOptions{renameCommands: true}, map[string]any{
		"completion":     map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
		"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
	})
	a := &authoring{t: t, c: c, dir: dir, uri: pathToURI(filepath.Join(dir, "list.vuka.html")), v: 1}
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": a.uri, "languageId": "vuka-html", "version": 1, "text": authoringShape}})
	c.waitDiags(a.uri, func([]any) bool { return true })
	return a
}

// complete puts new for old (| the cursor) and asks for a completion there.
func (a *authoring) complete(old, new string) (string, []map[string]any) {
	a.t.Helper()
	if !strings.Contains(authoringShape, old) {
		a.t.Fatalf("no %q", old)
	}
	text := strings.Replace(authoringShape, old, new, 1)
	cur := strings.Index(text, "|")
	text = text[:cur] + text[cur+1:]
	a.v++
	a.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": a.uri, "version": a.v},
		"contentChanges": []any{map[string]any{"text": text}}})
	got := a.c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": a.uri},
		"position": positionOf([]byte(text), cur)})
	return text, items(got)
}

func itemNamed(list []map[string]any, label string) map[string]any {
	for _, it := range list {
		if it["label"] == label {
			return it
		}
	}
	return nil
}

func TestLSPShapeOutline(t *testing.T) {
	a := startAuthoring(t)
	got := a.c.call("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": a.uri}})
	var outline []string
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		for _, s := range asList(v) {
			m := s.(map[string]any)
			r, ok := toRange(m["range"])
			if !ok || m["selectionRange"] == nil {
				t.Errorf("a symbol without ranges: %v", m)
			}
			outline = append(outline, strings.Repeat("  ", depth)+m["name"].(string)+" "+itoaTest(r.Start.Line)+"-"+itoaTest(r.End.Line))
			walk(m["children"], depth+1)
		}
	}
	walk(got, 0)
	src := authoringShape
	end := lineOf(src, strings.Index(src, "</section>"))
	want := []string{
		"List " + itoaTest(lineOf(src, strings.Index(src, "type List"))) + "-" + itoaTest(end),
		"  Todos", "  Tags", "  Open",
		"  (List).Count",
		"  section#todos.list.wide " + itoaTest(lineOf(src, strings.Index(src, "<section"))) + "-" + itoaTest(end),
		"    h2", "    ul", "      li", "    nav", "      button",
	}
	if len(outline) != len(want) {
		t.Fatalf("outline\n%s\nwant\n%s", strings.Join(outline, "\n"), strings.Join(want, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(outline[i], w) {
			t.Errorf("outline[%d] = %q, want %q", i, outline[i], w)
		}
	}

	folds := a.c.call("textDocument/foldingRange", map[string]any{"textDocument": map[string]any{"uri": a.uri}})
	head := map[string]any{"startLine": float64(0), "endLine": float64(lineOf(src, strings.Index(src, "func (l List)")))}
	b, _ := json.Marshal(folds)
	hb, _ := json.Marshal(head)
	if !strings.Contains(string(b), string(hb)) {
		t.Errorf("no header fold %s in %s", hb, b)
	}
}

func TestLSPShapeRangeFormatting(t *testing.T) {
	a := startAuthoring(t)
	if caps, _ := a.c.init.(map[string]any)["capabilities"].(map[string]any); caps["documentRangeFormattingProvider"] != true {
		t.Errorf("rangeFormatting not advertised: %v", caps["documentRangeFormattingProvider"])
	}
	messy := strings.Replace(authoringShape, "  <h2>{Count()}</h2>", "  <h2>{Count( )}</h2>   ", 1)
	messy = strings.Replace(messy, "{t.Title}</li>", "{t . Title}</li>  ", 1)
	a.v++
	a.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": a.uri, "version": a.v},
		"contentChanges": []any{map[string]any{"text": messy}}})
	line := lineOf(messy, strings.Index(messy, "<h2>"))
	got := a.c.call("textDocument/rangeFormatting", map[string]any{"textDocument": map[string]any{"uri": a.uri},
		"range": lspRange{lspPosition{Line: line}, lspPosition{Line: line, Character: 10}}, "options": map[string]any{"tabSize": 2}})
	out := applyEdits(messy, asList(got))
	if !strings.Contains(out, "\n  <h2>{Count()}</h2>\n") {
		t.Errorf("the selected line isn't formatted:\n%s", out)
	}
	if !strings.Contains(out, "{t . Title}</li>  \n") {
		t.Errorf("a line outside the selection was formatted:\n%s", out)
	}
}

func TestLSPMarkupAuthoringCompletion(t *testing.T) {
	a := startAuthoring(t)

	// ref names declared in the shape, in ui.Ref("…").
	_, list := a.complete(`ui.Ref("menu")`, `ui.Ref("m|")`)
	if itemNamed(list, "menu") == nil {
		t.Errorf("no ref menu: %v", list)
	}

	// The first element of a loop over structs with an ID: keyed.
	text, list := a.complete("      <li key={t.ID}>{t.Title}</li>", "      <l|")
	li := itemNamed(list, "li")
	if li == nil {
		t.Fatalf("no li: %d items", len(list))
	}
	if got := applyItem(t, text, li); !strings.Contains(got, "<li key={t.ID}") {
		t.Errorf("li isn't keyed:\n%s", got)
	}
	// Not past the first element.
	_, list = a.complete("      <li key={t.ID}>{t.Title}</li>", "      <li key={t.ID}>{t.Title}</li>\n      <l|")
	if li := itemNamed(list, "li"); li == nil || strings.Contains(li["textEdit"].(map[string]any)["newText"].(string), "key") {
		t.Errorf("a second element is keyed: %v", li)
	}
	// A loop over items with no ID-like field: no key.
	_, list = a.complete("for _, t := range Todos {\n      <li key={t.ID}>{t.Title}</li>", "for _, t := range Tags {\n      <l|")
	if li := itemNamed(list, "li"); li == nil || strings.Contains(li["textEdit"].(map[string]any)["newText"].(string), "key") {
		t.Errorf("a loop with no ID keys: %v", li)
	}

	// for on a markup line: the keyed loop snippet.
	_, list = a.complete("  <ul>\n", "  <ul>\n  fo|\n")
	if f := itemNamed(list, "for"); f == nil || !strings.Contains(f["textEdit"].(map[string]any)["newText"].(string), "key={") {
		t.Errorf("no keyed for snippet: %v", list)
	}

	// Attributes: ARIA, roles, events per element, ref.
	_, list = a.complete(`<nav ref="menu" hidden>`, `<nav ref="menu" hidden a|>`)
	for _, want := range []string{"aria-pressed", "aria-current", "aria-level", "aria-atomic", "onClick", "onKeyDown", "onPointerMove"} {
		if itemNamed(list, want) == nil {
			t.Errorf("no %s on <nav>", want)
		}
	}
	for _, not := range []string{"onSubmit", "onPlay", "onClose", "ref"} {
		if itemNamed(list, not) != nil {
			t.Errorf("%s offered on <nav>", not)
		}
	}
	if d, _ := itemNamed(list, "onClick")["documentation"].(map[string]any); d == nil || !strings.Contains(d["value"].(string), "{open = !open}") {
		t.Errorf("onClick doesn't say the handler forms: %v", d)
	}
	_, list = a.complete(`<button onClick`, `<button |onClick`)
	if itemNamed(list, "ref") == nil {
		t.Error("no ref on <button>")
	}
	_, list = a.complete(`<ul>`, `<form |>`)
	if itemNamed(list, "onSubmit") == nil {
		t.Error("no onSubmit on <form>")
	}
	for _, tc := range []struct{ attr, want string }{
		{"aria-pressed", "mixed"}, {"aria-current", "step"}, {"role", "toolbar"}, {"aria-relevant", "additions"},
	} {
		_, list = a.complete(`<nav ref="menu" hidden>`, `<nav `+tc.attr+`="|" hidden>`)
		if itemNamed(list, tc.want) == nil {
			t.Errorf("%s: no value %s", tc.attr, tc.want)
		}
	}
}
