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
