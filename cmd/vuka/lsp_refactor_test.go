package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refactorModule is a module of files requiring the local vuka, ui and web,
// for refactors applied and built.
func refactorModule(t *testing.T, files map[string]string) string {
	t.Helper()
	gomod, gosum := testModule(t, "example.com/app")
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if _, err := os.Stat(filepath.Join(web, "go.mod")); err != nil {
		t.Skipf("the refactor tests need github.com/vuka-lang/web/v2 checked out at %s (or $VUKA_WEB)", web)
	}
	webSum, _ := os.ReadFile(filepath.Join(web, "go.sum"))
	uiMod, _ := os.ReadFile(filepath.Join(os.Getenv("VUKA_UI"), "go.mod"))
	if os.Getenv("VUKA_UI") == "" {
		uiMod, _ = os.ReadFile(filepath.Join(filepath.Dir(repo), "vuka-ui", "go.mod"))
	}
	webMod, _ := os.ReadFile(filepath.Join(web, "go.mod"))
	// Every module the build loads, at the highest version any of them
	// requires: the go.mod go accepts as it is.
	reqs := map[string]string{"github.com/vuka-lang/web/v2": "v2.0.0"}
	var order []string
	for _, text := range []string{gomod, string(uiMod), string(webMod)} {
		for _, line := range strings.Split(text, "\n") {
			f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
			if len(f) < 2 || !strings.Contains(f[0], ".") || !strings.HasPrefix(f[1], "v") {
				continue
			}
			if v, ok := reqs[f[0]]; !ok || semverLess(v, f[1]) {
				if !ok {
					order = append(order, f[0])
				}
				reqs[f[0]] = f[1]
			}
		}
	}
	var req strings.Builder
	req.WriteString("require (\n\tgithub.com/vuka-lang/web/v2 v2.0.0\n")
	for _, m := range order {
		req.WriteString("\t" + m + " " + reqs[m] + "\n")
	}
	req.WriteString(")\n")
	start, end := strings.Index(gomod, "require ("), strings.Index(gomod, "\n)\n")+3
	gomod = gomod[:start] + req.String() + gomod[end:] + "\nreplace github.com/vuka-lang/web/v2 => " + web + "\n"
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum+string(webSum))
	for name, src := range files {
		writeFile(t, filepath.Join(dir, name), src)
	}
	return dir
}

// apply writes a refactor's change to disk.
func apply(t *testing.T, c change) {
	t.Helper()
	for path, text := range c.result() {
		if text == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, path, string(text))
	}
}

// vets runs vuka vet over the module in dir: the refactored code
// transpiles, compiles and vets.
func vets(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("vet", []string{"./..."}, &out, &errOut); err != nil {
		var files strings.Builder
		filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && (strings.HasSuffix(p, ".vuka.html") || strings.HasSuffix(p, ".vuka") || strings.HasSuffix(p, ".go")) {
				b, _ := os.ReadFile(p)
				files.WriteString("== " + strings.TrimPrefix(p, dir+"/") + "\n" + string(b) + "\n")
			}
			return nil
		})
		t.Fatalf("vuka vet: %v\n%s%s\n%s", err, out.Bytes(), errOut.Bytes(), files.String())
	}
}

// applyWorkspace applies a WorkspaceEdit of document changes to disk, as
// the editor gets it: through JSON.
func applyWorkspace(t *testing.T, edit any) {
	t.Helper()
	b, _ := json.Marshal(edit)
	var we struct {
		DocumentChanges []map[string]any `json:"documentChanges"`
	}
	if err := json.Unmarshal(b, &we); err != nil || len(we.DocumentChanges) == 0 {
		t.Fatalf("not a WorkspaceEdit of document changes: %s", b)
	}
	for _, dc := range we.DocumentChanges {
		switch dc["kind"] {
		case "create":
			path := uriToPath(dc["uri"].(string))
			if _, err := os.Stat(path); err == nil {
				t.Fatalf("creates %s, which exists", path)
			}
			writeFile(t, path, "")
		case "delete":
			if err := os.Remove(uriToPath(dc["uri"].(string))); err != nil {
				t.Fatal(err)
			}
		default:
			path := uriToPath(dc["textDocument"].(map[string]any)["uri"].(string))
			old, _ := os.ReadFile(path)
			writeFile(t, path, applyEdits(string(old), dc["edits"].([]any)))
		}
	}
}

// actionsAt are vuka's code actions for the range [from, to) of the
// file at path, as the editor would get them.
func actionsAt(t *testing.T, path string, from, to int, diags ...map[string]any) map[string]codeAction {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := actionRequest{path: path, src: src, start: from, end: to, diags: diags, read: os.ReadFile}
	out := map[string]codeAction{}
	for _, a := range codeActions(r) {
		ca, ok := a.(codeAction)
		if !ok {
			t.Fatalf("not a code action: %#v", a)
		}
		out[ca.Title] = ca
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// selection is the offsets of the text from the line holding from to the
// end of the line holding to.
func selection(src, from, to string) (int, int) {
	s := strings.Index(src, from)
	s = strings.LastIndexByte(src[:s], '\n') + 1
	e := strings.Index(src[s:], to) + s + len(to)
	if nl := strings.IndexByte(src[e:], '\n'); nl >= 0 {
		e += nl
	}
	return s, e
}

const refactorMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { _ = web.New(pages.Routes()) }
`

const refactorItem = `package pages

type Item struct {
	ID   int
	Name string
}
`

const listPage = `<script lang="go">
package pages

import (
	"strings"

	"github.com/vuka-lang/ui/v2"
)

type Index struct {
	ui.Live
	items []Item
	count int
	title string
}

func (p *Index) Add() { p.count++ }

func (p *Index) Remove(id int) {}
</script>

<main>
  <h1>{strings.ToUpper(title)}</h1>
  <ul>
    for _, it := range items {
      <li key={it.ID}>
        <span>{it.Name}</span>
        <button onClick={func() { p.Remove(it.ID) }}>Remove</button>
      </li>
    }
  </ul>
  <p>{count} items</p>
  <button onClick={Add}>Add one</button>
</main>
`

// TestExtractComponent: a selection of a page's markup becomes a shape in
// _parts, in the script form, and the page passes it what it reads.
func TestExtractComponent(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/item.go": refactorItem, "pages/index.vuka.html": listPage})
	path := filepath.Join(dir, "pages/index.vuka.html")
	s, e := selection(listPage, "<p>{count}", "Add one</button>")
	c, err := extractComponent(path, []byte(listPage), s, e, "Counter", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, c)
	part := readFile(t, filepath.Join(dir, "pages/_parts/counter.vuka.html"))
	page := readFile(t, path)
	t.Logf("part:\n%s\npage:\n%s", part, page)
	if !strings.HasPrefix(part, "<script lang=\"go\">\npackage parts\n") {
		t.Errorf("not the script form")
	}
	vets(t, dir)
}

// TestExtractRow: a loop's item extracted from a page in the Go-header
// form: the item, a method called through the receiver and the key; the
// row needs the page's type, so it stays in the page's package. The key
// stays on the element the row renders, as the compiler asks of a struct
// component.
func TestExtractRow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	header := strings.Replace(strings.Replace(listPage, "<script lang=\"go\">\n", "", 1), "</script>\n\n", "", 1)
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/item.go": refactorItem, "pages/index.vuka.html": header})
	path := filepath.Join(dir, "pages/index.vuka.html")
	s, e := selection(header, "<li key", "</li>")
	c, err := extractComponent(path, []byte(header), s, e, "row", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, c)
	part := readFile(t, filepath.Join(dir, "pages/_row.vuka.html"))
	page := readFile(t, path)
	for _, want := range []string{"type Row struct {\n\tItem   Item\n\tRemove func(id int)\n}", "<li key={Item.ID}>", "onClick={func() { Remove(Item.ID) }}"} {
		if !strings.Contains(part, want) {
			t.Errorf("the row has no %q:\n%s", want, part)
		}
	}
	if !strings.Contains(page, "<Row Item={it} Remove={Remove} />") || strings.Contains(page, "private_parts") {
		t.Errorf("page:\n%s", page)
	}
	vets(t, dir)
}

const sendsCounter = `<script lang="go">
package components

type Counter struct {
	count int
}

func (c *Counter) Add() { c.count++ }
</script>

<div>
  <p>{count}</p>
  <button onClick={Add}>Add one</button>
</div>
`

// TestExtractSends: outside pages/ the component goes in the same package;
// a shape live only because the selection sends its event embeds ui.Live,
// and the event reaches the shape through the method value it passes.
func TestExtractSends(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	dir := refactorModule(t, map[string]string{"components/counter.vuka.html": sendsCounter})
	path := filepath.Join(dir, "components/counter.vuka.html")
	s, e := selection(sendsCounter, "<button", "</button>")
	c, err := extractComponent(path, []byte(sendsCounter), s, e, "AddButton", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, c)
	part := readFile(t, filepath.Join(dir, "components/add_button.vuka.html"))
	page := readFile(t, path)
	if !strings.Contains(part, "type AddButton struct {\n\tAdd func()\n}") {
		t.Errorf("part:\n%s", part)
	}
	for _, want := range []string{"import \"github.com/vuka-lang/ui/v2\"", "type Counter struct {\n\tui.Live\n\tcount int", "<AddButton Add={Add} />"} {
		if !strings.Contains(page, want) {
			t.Errorf("page has no %q:\n%s", want, page)
		}
	}
	vets(t, dir)
}

const hooksPage = `<script lang="go">
package pages

import (
	"context"
	"strconv"
	"strings"

	"github.com/vuka-lang/ui/v2"
)

type Board struct {
	ui.Live
	count int
	note  string
}

func (b *Board) Title() string { return "Board " + strconv.Itoa(b.count) }

// Mount starts the count.
@ui.Mount
func (b *Board) Mount(ctx context.Context) error {
	b.count = 1
	return nil
}

// Add counts one more.
func (b *Board) Add() { b.count++ }

func (b *Board) Shout() { b.note = strings.ToUpper(b.note) + "!" }
</script>

<main>
  <p>{count} {note}</p>
  <button onClick={Add}>Add</button>
  <button onClick={Shout}>Shout</button>
</main>
`

// TestMoveMethods: the methods of a shape's Go, but Title and its hooks,
// move to the logic file beside it, created with its package clause and
// the imports they need; the shape keeps only the imports it still uses.
// One method moves into a logic file that exists. Both forms.
func TestMoveMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	header := strings.Replace(strings.Replace(hooksPage, "<script lang=\"go\">\n", "", 1), "</script>\n\n", "", 1)
	for _, form := range []struct{ name, src string }{{"script", hooksPage}, {"header", header}} {
		t.Run(form.name, func(t *testing.T) {
			dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/board.vuka.html": form.src})
			path := filepath.Join(dir, "pages/board.vuka.html")
			at := strings.Index(form.src, "func (b *Board) Add")
			acts := actionsAt(t, path, at, at)
			all, ok := acts["Move the methods of Board to board.vuka"]
			if !ok || acts["Move Add to board.vuka"].Edit == nil {
				t.Fatalf("actions: %v", acts)
			}
			applyWorkspace(t, all.Edit)
			shape, logic := readFile(t, path), readFile(t, filepath.Join(dir, "pages/board.vuka"))
			for _, want := range []string{"func (b *Board) Title()", "@ui.Mount\nfunc (b *Board) Mount", "\"strconv\"", "\"context\""} {
				if !strings.Contains(shape, want) {
					t.Errorf("the shape lost %q:\n%s", want, shape)
				}
			}
			if strings.Contains(shape, "Add()") || strings.Contains(shape, "strings") {
				t.Errorf("the shape kept a moved method or its import:\n%s", shape)
			}
			if !strings.HasPrefix(logic, "package pages\n\nimport \"strings\"\n\n// Add counts one more.\nfunc (b *Board) Add()") || !strings.Contains(logic, "func (b *Board) Shout()") {
				t.Errorf("logic file:\n%s", logic)
			}
			vets(t, dir)
		})
	}
}

// TestMoveFromGo: a .go file's method using a shape's tracked state
// (E0155) moves to the shape's logic file, which exists: appended, its
// imports joined; the .go file, left empty, goes.
func TestMoveFromGo(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	goFile := "package pages\n\nimport \"strings\"\n\nfunc (b *Board) Reset() {\n\tb.count = 0\n\tb.note = strings.TrimSpace(b.note)\n}\n"
	logic := "package pages\n\nimport \"fmt\"\n\nfunc (b *Board) Show() string { return fmt.Sprint(b.count) }\n"
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/board.vuka.html": hooksPage, "pages/reset.go": goFile, "pages/board.vuka": logic})
	path := filepath.Join(dir, "pages/reset.go")
	at := strings.Index(goFile, "b.count")
	diag := map[string]any{"code": "E0155", "range": map[string]any{"start": positionOf([]byte(goFile), at), "end": positionOf([]byte(goFile), at+7)}}
	acts := actionsAt(t, path, at, at, diag)
	fix, ok := acts["Move Reset to board.vuka"]
	if !ok || fix.Kind != "quickfix" || len(fix.Diagnostics) != 1 {
		t.Fatalf("actions: %v", acts)
	}
	applyWorkspace(t, fix.Edit)
	if _, err := os.Stat(path); err == nil {
		t.Errorf("reset.go is left")
	}
	got := readFile(t, filepath.Join(dir, "pages/board.vuka"))
	if !strings.Contains(got, "import (\n\t\"fmt\"\n\t\"strings\"\n)") || !strings.HasSuffix(got, "func (b *Board) Reset() {\n\tb.count = 0\n\tb.note = strings.TrimSpace(b.note)\n}\n") {
		t.Errorf("board.vuka:\n%s", got)
	}
	vets(t, dir)
}

const unkeyedPage = `<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

type Index struct {
	ui.Live
	items []Item
	names []string
}
</script>

<main>
  <ul>
    for _, it := range items {
      <li>{it.Name}</li>
    }
  </ul>
  <ol>
    for _, name := range names {
      <li class="name">{name}</li>
    }
  </ol>
</main>
`

// TestKeyFix: a live list's item gets a key (W0102): the item's ID, or the
// index, named when the loop writes it _.
func TestKeyFix(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/item.go": refactorItem, "pages/index.vuka.html": unkeyedPage})
	path := filepath.Join(dir, "pages/index.vuka.html")
	for _, c := range []struct{ at, want string }{
		{"<li>{it.Name}", "<li key={it.ID}>{it.Name}</li>"},
		{"<li class=\"name\">", "for i, name := range names {\n      <li key={i} class=\"name\">"},
	} {
		src := readFile(t, path)
		at := strings.Index(src, c.at)
		fix, ok := actionsAt(t, path, at, at)["Give the item a key"]
		if !ok {
			t.Fatalf("no key fix at %q", c.at)
		}
		applyWorkspace(t, fix.Edit)
		if got := readFile(t, path); !strings.Contains(got, c.want) {
			t.Errorf("no %q:\n%s", c.want, got)
		}
	}
	vets(t, dir)
}

const titledLayout = `<script lang="go">
package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Layout struct {
	Children ui.Node
	title    string
}

func (l *Layout) Load(ctx context.Context) error {
	l.title = ui.PageTitle(ctx)
	return nil
}
</script>

<div>
  <h1>{title}</h1>
  <main>{Children}</main>
</div>
`

const pathLayout = `package pages

import (
	"context"
	"strings"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

type Layout struct {
	Children ui.Node
	path     string
}

func (l *Layout) Load(ctx context.Context) error {
	l.path = web.Request(ctx).URL.Path
	return nil
}
<div>
  <nav>
    if strings.HasPrefix(path, "/orders") {
      <a href="/orders" class="tab on">Orders</a>
    } else {
      <a href="/orders" class="tab">Orders</a>
    }
    <a href="/about">About</a>
  </nav>
  <main>{Children}</main>
</div>
`

const simplePage = "<script lang=\"go\">\npackage pages\n\ntype Index struct{}\n</script>\n\n<p>Home</p>\n"

// TestLayoutFixes: a layout reading the page's title in Load reads it in
// its markup, and one marking its current link by the request's path marks
// it with ui.Link in the browser (W0703); what they read with goes.
func TestLayoutFixes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	t.Run("title", func(t *testing.T) {
		dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/index.vuka.html": simplePage, "pages/_layout.vuka.html": titledLayout})
		path := filepath.Join(dir, "pages/_layout.vuka.html")
		at := strings.Index(titledLayout, "type Layout")
		fix, ok := actionsAt(t, path, at, at)["Read ui.PageTitle in the markup"]
		if !ok {
			t.Fatalf("no fix: %v", actionsAt(t, path, at, at))
		}
		applyWorkspace(t, fix.Edit)
		got := readFile(t, path)
		want := "<script lang=\"go\">\npackage pages\n\nimport (\n\t\"github.com/vuka-lang/ui/v2\"\n)\n\ntype Layout struct {\n\tChildren ui.Node\n}\n</script>\n\n<div>\n  <h1>{ui.PageTitle(ctx)}</h1>"
		if !strings.HasPrefix(got, want) {
			t.Errorf("layout:\n%s\nwant:\n%s", got, want)
		}
		vets(t, dir)
	})
	t.Run("links", func(t *testing.T) {
		dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/index.vuka.html": simplePage, "pages/_layout.vuka.html": pathLayout})
		path := filepath.Join(dir, "pages/_layout.vuka.html")
		at := strings.Index(pathLayout, "<a href=\"/about\"")
		fix, ok := actionsAt(t, path, at, at)["Mark the current link in the browser: ui.Link active=\"prefix\""]
		if !ok {
			t.Fatalf("no fix for the lone link")
		}
		applyWorkspace(t, fix.Edit)
		src := readFile(t, path)
		at = strings.Index(src, "if strings.HasPrefix")
		fix, ok = actionsAt(t, path, at, at)["Mark the current link in the browser: ui.Link active=\"prefix\""]
		if !ok {
			t.Fatalf("no fix for the if")
		}
		applyWorkspace(t, fix.Edit)
		got := readFile(t, path)
		for _, want := range []string{
			"    <ui.Link href=\"/orders\" active=\"prefix\" class=\"tab\" activeClass=\"on\">Orders</ui.Link>\n    <ui.Link href=\"/about\" active=\"prefix\">About</ui.Link>",
			"type Layout struct {\n\tChildren ui.Node\n}\n<div>",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("no %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "web") || strings.Contains(got, "strings") || strings.Contains(got, "context") {
			t.Errorf("imports left:\n%s", got)
		}
		vets(t, dir)
	})
}

const keyedCounter = `<script lang="go">
package components

type Counter struct {
	count int
	names []string
}

func (c *Counter) Add() { c.count++ }
</script>

<div>
  <p>{count}</p>
  <button onClick={Add}>Add one</button>
  <ul>
    for _, n := range names {
      <li>{n}</li>
    }
  </ul>
</div>
`

// TestLSPRefactors: vuka lsp offers its actions beside gopls's; Extract
// component runs vuka.extractComponent, which has the editor apply the
// edit, and the key fix's edit comes on codeAction/resolve.
func TestLSPRefactors(t *testing.T) {
	if _, err := findGopls(""); err != nil {
		needs(t, "%v", err)
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	mod, sum := testModule(t, "lsptest")
	path := filepath.Join(dir, "components/counter.vuka.html")
	for name, src := range map[string]string{"go.mod": mod, "go.sum": sum, "components/counter.vuka.html": keyedCounter} {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	c := startLSPCaps(t, dir, lspOptions{renameCommands: true}, map[string]any{
		"codeAction": map[string]any{"resolveSupport": map[string]any{"properties": []any{"edit"}},
			"codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []any{"quickfix", "refactor", "refactor.extract", "refactor.move", "refactor.rewrite", "source"}}}},
	})
	c.applyEdit = func(params json.RawMessage) any {
		var p struct {
			Edit any `json:"edit"`
		}
		_ = json.Unmarshal(params, &p)
		applyWorkspace(t, p.Edit)
		return map[string]any{"applied": true}
	}
	caps := c.init.(map[string]any)["capabilities"].(map[string]any)
	if cap, _ := caps["codeActionProvider"].(map[string]any); cap["resolveProvider"] != true || !strings.Contains(fmt.Sprint(cap["codeActionKinds"]), "refactor.move") {
		t.Errorf("code actions: %v", caps["codeActionProvider"])
	}
	uri := pathToURI(path)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": keyedCounter}})
	c.waitDiags(uri, func([]any) bool { return true })
	actions := func(s, e int) map[string]map[string]any {
		got := c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
			"range": lspRange{positionOf([]byte(keyedCounter), s), positionOf([]byte(keyedCounter), e)}, "context": map[string]any{"diagnostics": []any{}}})
		out := map[string]map[string]any{}
		list, _ := got.([]any)
		for _, a := range list {
			m := a.(map[string]any)
			out[m["title"].(string)] = m
		}
		return out
	}

	at := strings.Index(keyedCounter, "<li>")
	key := actions(at, at)["Give the item a key"]
	if key == nil || key["edit"] != nil || key["data"] == nil {
		t.Fatalf("the key fix, its edit left for resolve: %v", key)
	}
	resolved := c.call("codeAction/resolve", key).(map[string]any)
	applyWorkspace(t, resolved["edit"])
	if got := readFile(t, path); !strings.Contains(got, "for i, n := range names {\n      <li key={i}>{n}</li>") {
		t.Errorf("keyed:\n%s", got)
	}

	src := readFile(t, path)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	s, e := selection(src, "<button", "</button>")
	got := c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
		"range": lspRange{positionOf([]byte(src), s), positionOf([]byte(src), e)}, "context": map[string]any{"diagnostics": []any{}}})
	var extract map[string]any
	for _, a := range got.([]any) {
		if m := a.(map[string]any); m["title"] == "Extract component…" {
			extract = m
		}
	}
	if extract == nil {
		t.Fatalf("no Extract component…: %v", got)
	}
	cmd := extract["command"].(map[string]any)
	arg := cmd["arguments"].([]any)[0].(map[string]any)
	arg["name"] = "AddButton"
	c.call("workspace/executeCommand", map[string]any{"command": cmd["command"], "arguments": []any{arg}})
	if got := readFile(t, path); !strings.Contains(got, "<AddButton Add={Add} />") || !strings.Contains(got, "ui.Live") {
		t.Errorf("after extracting:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "components/add_button.vuka.html")); err != nil {
		t.Error(err)
	}
	vets(t, dir)
}

// TestSmallFixes: a template file without its package clause gets its
// directory's (E0142); ui.Live embedded by pointer is embedded by value
// (E0329).
func TestSmallFixes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	noPackage := strings.Replace(sendsCounter, "package components\n\n", "", 1)
	pointer := strings.Replace(strings.Replace(sendsCounter, "type Counter struct {\n", "type Counter struct {\n\t*ui.Live\n", 1),
		"package components\n", "package components\n\nimport \"github.com/vuka-lang/ui/v2\"\n", 1)
	pointer = strings.Replace(pointer, "Counter", "Board", -1)
	dir := refactorModule(t, map[string]string{"components/counter.vuka.html": noPackage, "components/board.vuka.html": pointer, "components/doc.go": "package components\n"})
	path := filepath.Join(dir, "components/counter.vuka.html")
	fix, ok := actionsAt(t, path, 0, 0)["Add package components"]
	if !ok {
		t.Fatalf("no package fix")
	}
	applyWorkspace(t, fix.Edit)
	if got := readFile(t, path); got != sendsCounter {
		t.Errorf("got:\n%s\nwant:\n%s", got, sendsCounter)
	}
	path = filepath.Join(dir, "components/board.vuka.html")
	at := strings.Index(pointer, "*ui.Live")
	fix, ok = actionsAt(t, path, at, at)["Embed ui.Live by value"]
	if !ok {
		t.Fatalf("no ui.Live fix")
	}
	applyWorkspace(t, fix.Edit)
	if got := readFile(t, path); !strings.Contains(got, "type Board struct {\n\tui.Live\n") {
		t.Errorf("got:\n%s", got)
	}
	vets(t, dir)
}
