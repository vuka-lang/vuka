package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const lspTSGo = `package main

import "github.com/vuka-lang/ui/v2"

// Chart is a component with a script.
type Chart struct {
	Title  string
	Points []int
	open   ui.State[bool]
}

func main() {}
`

const lspTSTemplate = `package main

<figure>{Title}</figure>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Chart>((el, props, ctx) => {
  el.title = props.Title + props.Missing
  ctx.state("open").set(true)
})
</script>
`

// TestLSPScript: a template's script is TypeScript with its component's
// types: completion and hover from the TypeScript server, its errors at
// their lines in the .vuka.html.
func TestLSPScript(t *testing.T) {
	if findTSLS() == "" {
		nm := fixtureModules("VUKA_TS_NM", "islands")
		if nm == "" || !fileExists(filepath.Join(nm, ".bin", "typescript-language-server")) {
			needs(t, "no typescript-language-server ($VUKA_TSLS, or npm ci --prefix testdata/node/islands)")
		}
		t.Setenv("VUKA_TSLS", filepath.Join(nm, ".bin", "typescript-language-server"))
	}
	c, dir, _ := startLSPWith(t, map[string]string{"chart.go": lspTSGo, "chart.vuka.html": lspTSTemplate})
	uri := pathToURI(filepath.Join(dir, "chart.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspTSTemplate}})
	line := uint32(strings.Count(lspTSTemplate[:strings.Index(lspTSTemplate, "props.Missing")], "\n"))
	c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "Missing") && strings.Contains(string(b), `"line":`+itoaTest(line))
	})
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(lspTSTemplate), strings.Index(lspTSTemplate, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("props.Title", 7)))
	if !strings.Contains(string(hover), "Title") || !strings.Contains(string(hover), "string") {
		t.Errorf("hover on props.Title: %s", hover)
	}
	comp, _ := json.Marshal(c.call("textDocument/completion", at(`ctx.state`, 4)))
	for _, want := range []string{`"state"`, `"navigate"`, `"live"`} {
		if !strings.Contains(string(comp), want) {
			t.Errorf("completion after ctx. lacks %s: %.400s", want, comp)
		}
	}
}

const lspTSPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Board is a live page.
type Board struct {
	ui.Live
	Q    string
	open ui.State[bool]
}

// Reorder puts the cards in the order of ids.
func (b *Board) Reorder(ids []int) {}
</script>

<main><input ref="box" /></main>

<script lang="ts">
import { page } from 'vuka:component'
export default page<Board>((el, live, ctx) => {
  live.reorder([1])
  ctx.state("open")
})
</script>
`

// tsFixture is the typescript-language-server's lib/cli.mjs and the
// typescript lib of testdata/node/islands, as the VS Code extension hands
// them to vuka lsp; the test skips without them.
func tsFixture(t *testing.T) (cli, lib string) {
	nm := fixtureModules("VUKA_TS_NM", "islands")
	cli = filepath.Join(nm, "typescript-language-server", "lib", "cli.mjs")
	lib = filepath.Join(nm, "typescript", "lib")
	if nm == "" || !fileExists(cli) || !fileExists(filepath.Join(lib, "tsserver.js")) {
		needs(t, "no typescript-language-server and typescript (npm ci --prefix testdata/node/islands)")
	}
	return cli, lib
}

// TestLSPScriptLauncher: -tsls a typescript-language-server's cli.mjs run
// by -tsls-node, -tsserver the TypeScript of a module without one: what
// the VS Code extension passes, with no server on PATH. A page script gets
// its live methods after live., a component script ctx's members.
func TestLSPScriptLauncher(t *testing.T) {
	cli, lib := tsFixture(t)
	node, err := exec.LookPath("node")
	if err != nil {
		needs(t, "no node on PATH")
	}
	t.Setenv("VUKA_TSLS", "")
	opts := lspOptions{renameCommands: true, ts: tsOptions{server: cli, node: node, tsserver: lib}}
	c, dir, _ := startLSPOpts(t, map[string]string{"chart.go": lspTSGo, "chart.vuka.html": lspTSTemplate, "board.vuka.html": lspTSPage}, opts)
	complete := func(file, src, needle string, delta int) string {
		uri := pathToURI(filepath.Join(dir, file))
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
		got, _ := json.Marshal(c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), strings.Index(src, needle)+delta)}))
		return string(got)
	}
	if got := complete("board.vuka.html", lspTSPage, "live.reorder", 5); !strings.Contains(got, `"reorder"`) || !strings.Contains(got, `"q"`) {
		t.Errorf("completion after live. lacks reorder and q: %.600s", got)
	}
	got := complete("chart.vuka.html", lspTSTemplate, "ctx.state", 4)
	for _, want := range []string{`"state"`, `"ref"`, `"on"`, `"emit"`, `"live"`} {
		if !strings.Contains(got, want) {
			t.Errorf("completion after ctx. lacks %s: %.600s", want, got)
		}
	}
}

// TestTSOptions: a JavaScript server runs under the given Node as Node
// (ELECTRON_RUN_AS_NODE, for VS Code's Electron), an executable as itself;
// the module's own TypeScript, else its frontend's, is preferred to
// -tsserver's.
func TestTSOptions(t *testing.T) {
	o := tsOptions{node: "/app/Code Helper", tsserver: "/vscode/typescript/lib"}
	cmd, err := o.tsCommand("/ext/node_modules/typescript-language-server/lib/cli.mjs")
	if err != nil || cmd.Path != "/app/Code Helper" || strings.Join(cmd.Args[1:], " ") != "/ext/node_modules/typescript-language-server/lib/cli.mjs --stdio" || !slices.Contains(cmd.Env, "ELECTRON_RUN_AS_NODE=1") {
		t.Errorf("JS server: %v %q %v", err, cmd.Args, err)
	}
	if cmd, _ := o.tsCommand("/bin/tsls"); strings.Join(cmd.Args, " ") != "/bin/tsls --stdio" || cmd.Env != nil {
		t.Errorf("executable: %q", cmd.Args)
	}
	root, web := t.TempDir(), t.TempDir()
	got := func() string { b, _ := json.Marshal(o.tsserverOptions(root, web)); return string(b) }
	if g := got(); g != `{"tsserver":{"fallbackPath":"/vscode/typescript/lib"}}` {
		t.Errorf("no TypeScript of its own: %s", g)
	}
	os.MkdirAll(filepath.Join(web, "node_modules", "typescript", "lib"), 0o755)
	os.WriteFile(filepath.Join(web, "node_modules", "typescript", "lib", "tsserver.js"), nil, 0o644)
	if g := got(); !strings.Contains(g, `"path":`) || !strings.Contains(g, "node_modules") {
		t.Errorf("the frontend's TypeScript: %s", g)
	}
	os.MkdirAll(filepath.Join(root, "node_modules", "typescript", "lib"), 0o755)
	os.WriteFile(filepath.Join(root, "node_modules", "typescript", "lib", "tsserver.js"), nil, 0o644)
	if g := got(); strings.Contains(g, `"path":`) {
		t.Errorf("the module's TypeScript is the server's to find: %s", g)
	}
	t.Setenv("VUKA_TSLS", "/env/tsls")
	t.Setenv("VUKA_TSSERVER", "")
	if e := (tsOptions{server: "/flag/tsls"}).withEnv(); e.server != "/flag/tsls" {
		t.Errorf("the flag loses to the environment: %+v", e)
	}
	if e := (tsOptions{}).withEnv(); e.server != "/env/tsls" {
		t.Errorf("no environment: %+v", e)
	}
}

// TestLSPScriptNames: in a script, the strings ctx takes complete with
// the names the markup and the Go give: refs, PushEvent's events, the
// root's emitted events, a page's @ui.URL keys and the folder routes; a
// live method's hover shows its Go doc.
func TestLSPScriptNames(t *testing.T) {
	cli, lib := tsFixture(t)
	t.Setenv("VUKA_TSLS", "")
	page := strings.Replace(namesIndex, "export default component<Index>((el, props, ctx) => {})", `export default component<Index>((el, props, ctx) => {
  ctx.ref("search")
  ctx.on("saved", (p) => {})
  ctx.patch({ q: "x" })
  ctx.navigate("/")
  ctx.live.reorder([1])
})`, 1)
	card := strings.Replace(namesCard, "export default component<Card>((el, props, ctx) => {})", `export default component<Card>((el, props, ctx) => {
  ctx.emit("picked")
})`, 1)
	opts := lspOptions{renameCommands: true, ts: tsOptions{server: cli, tsserver: lib}}
	c, dir, _ := startLSPOpts(t, map[string]string{"pages/index.vuka.html": page, "pages/point.go": "package pages\n\ntype Point struct{ X, Y int }\n",
		"pages/users/[id].vuka.html": namesUser, "components/card.vuka.html": card}, opts)
	open := func(file, src string) string {
		uri := pathToURI(filepath.Join(dir, file))
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
		return uri
	}
	at := func(uri, src, needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), strings.Index(src, needle)+delta)}
	}
	labels := func(v any) []string {
		b, _ := json.Marshal(v)
		var r struct {
			Items []struct{ Label string }
		}
		if json.Unmarshal(b, &r) != nil {
			json.Unmarshal(b, &r.Items)
		}
		var out []string
		for _, i := range r.Items {
			out = append(out, i.Label)
		}
		slices.Sort(out)
		return out
	}
	pu, cu := open("pages/index.vuka.html", page), open("components/card.vuka.html", card)
	for _, x := range []struct {
		uri, src, needle string
		delta            int
		want             []string
	}{
		{pu, page, `ctx.ref("`, 9, []string{"card", "chart", "search"}},
		{pu, page, `ctx.on("`, 8, []string{"count", "named", "saved"}},
		{pu, page, `ctx.patch({ `, 12, []string{"page?", "q?"}},
		{pu, page, `ctx.navigate("`, 14, []string{"/"}},
		{cu, card, `ctx.emit("`, 10, []string{"close", "picked"}},
	} {
		got := labels(c.call("textDocument/completion", at(x.uri, x.src, x.needle, x.delta)))
		for _, w := range x.want {
			if !slices.Contains(got, w) {
				t.Errorf("completion in %s lacks %q: %q", x.needle, w, got)
			}
		}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at(pu, page, "reorder([1])", 2)))
	if !strings.Contains(string(hover), "puts the cards in the order of ids") {
		t.Errorf("hover on live.reorder lacks its Go doc: %s", hover)
	}
}

// TestLSPScriptDefinition: go to definition on live.reorder in a page's
// script lands on func (b *Board) Reorder in the shape's Go, and the
// references of that method include the script's call.
func TestLSPScriptDefinition(t *testing.T) {
	cli, lib := tsFixture(t)
	t.Setenv("VUKA_TSLS", "")
	opts := lspOptions{renameCommands: true, ts: tsOptions{server: cli, tsserver: lib}}
	c, dir, _ := startLSPOpts(t, map[string]string{"board.vuka.html": lspTSPage}, opts)
	uri := pathToURI(filepath.Join(dir, "board.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspTSPage}})
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(lspTSPage), strings.Index(lspTSPage, needle)+delta),
			"context": map[string]any{"includeDeclaration": true}}
	}
	decl := positionOf([]byte(lspTSPage), strings.Index(lspTSPage, "Reorder(ids"))
	def, _ := json.Marshal(c.call("textDocument/definition", at("live.reorder", 6)))
	if !strings.Contains(string(def), "board.vuka.html") || !strings.Contains(string(def), `"line":`+itoaTest(decl.Line)) || strings.Contains(string(def), componentDTSName) {
		t.Errorf("definition of live.reorder isn't func (b *Board) Reorder (line %d): %s", decl.Line, def)
	}
	call := positionOf([]byte(lspTSPage), strings.Index(lspTSPage, "live.reorder")+len("live."))
	refs, _ := json.Marshal(c.call("textDocument/references", at("Reorder(ids", 2)))
	if !strings.Contains(string(refs), `{"character":`+itoaTest(call.Character)+`,"line":`+itoaTest(call.Line)+`}`) {
		t.Errorf("references of Reorder lack the script's live.reorder at %v: %s", call, refs)
	}
}

// TestLSPScriptImports: an import's specifier completes with vuka:component
// and the module's npm dependencies; a statement's start offers the
// listener snippet that cleans up after itself.
func TestLSPScriptImports(t *testing.T) {
	cli, lib := tsFixture(t)
	t.Setenv("VUKA_TSLS", "")
	src := strings.Replace(lspTSPage, "import { page } from 'vuka:component'", "import { page } from 'vuka:component'\nimport Sortable from 'so'", 1)
	src = strings.Replace(src, "  live.reorder([1])\n", "  live.reorder([1])\n  lis\n", 1)
	opts := lspOptions{renameCommands: true, ts: tsOptions{server: cli, tsserver: lib}}
	c, dir, _ := startLSPOpts(t, map[string]string{"board.vuka.html": src,
		"package.json": `{"dependencies": {"sortablejs": "1.15.0"}, "devDependencies": {"@types/sortablejs": "1.15.0", "chart.js": "4.0.0"}}`}, opts)
	uri := pathToURI(filepath.Join(dir, "board.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
	ask := func(needle string, delta int) string {
		got, _ := json.Marshal(c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), strings.Index(src, needle)+delta)}))
		return string(got)
	}
	got := ask("from 'so'", 8)
	for _, want := range []string{`"label":"sortablejs"`, `"label":"chart.js"`, `"label":"vuka:component"`} {
		if !strings.Contains(got, want) {
			t.Errorf("import specifier completion lacks %s: %.800s", want, got)
		}
	}
	if strings.Contains(got, `"label":"@types/sortablejs"`) {
		t.Errorf("an @types package completes: %.800s", got)
	}
	if got := ask("  lis\n", 5); !strings.Contains(got, `"label":"listen"`) || !strings.Contains(got, "removeEventListener") {
		t.Errorf("no listener snippet: %.800s", got)
	}
}
