package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFixCtxVars: vuka fix ctx-vars rewrites each testdata/fixctxvars
// name.in, in a package of an app listing web.RequestContext and
// auth.Context, to name.golden, dropping the imports left unused; the app
// still vets, and a second run has nothing to do.
func TestFixCtxVars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	cases, _ := filepath.Glob("testdata/fixctxvars/*.in")
	for i, c := range cases {
		cases[i], _ = filepath.Abs(c)
	}
	main, _ := os.ReadFile("testdata/fixctxvars/main.go")
	files := map[string]string{"main.go": string(main)}
	for _, c := range cases {
		src, _ := os.ReadFile(c)
		files["orders/"+strings.TrimSuffix(filepath.Base(c), ".in")] = string(src)
	}
	dir := refactorModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out bytes.Buffer
	if err := fix([]string{"ctx-vars"}, &out); err != nil {
		t.Fatalf("vuka fix: %v\n%s", err, out.String())
	}
	for _, c := range cases {
		name := strings.TrimSuffix(filepath.Base(c), ".in")
		got := readFile(t, filepath.Join(dir, "orders", name))
		golden := strings.TrimSuffix(c, ".in") + ".golden"
		if *updateFiles {
			os.WriteFile(golden, []byte(got), 0o644)
			continue
		}
		if want, _ := os.ReadFile(golden); got != string(want) {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
	out.Reset()
	if err := fix([]string{"ctx-vars"}, &out); err != nil || !strings.Contains(out.String(), "nothing to fix") {
		t.Errorf("again: %v\n%s", err, out.String())
	}
	vets(t, dir)
}

// TestLSPCtxVarsFix: on auth.Can in a page's markup, the editor offers
// vuka fix ctx-vars' rewrite, the import left unused dropped with it.
func TestLSPCtxVarsFix(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	main, _ := os.ReadFile("testdata/fixctxvars/main.go")
	const board = `<script lang="go">
package orders

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2/auth"
)

// Board shows its control by a permission.
type Board struct{ ui.Live }

func NewBoard() *Board { return &Board{} }
</script>

<section>
  if auth.Can(ctx, "orders.approve") {
    <p>approver</p>
  }
</section>
`
	dir := refactorModule(t, map[string]string{"main.go": string(main), "orders/board.vuka.html": board})
	t.Setenv("GOWORK", "off")
	c := startLSPAt(t, dir, lspOptions{})
	path := filepath.Join(dir, "orders", "board.vuka.html")
	uri := pathToURI(path)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": board}})
	at := strings.Index(board, "auth.Can(") + 2
	var fix map[string]any
	for deadline := time.Now().Add(60 * time.Second); fix == nil && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		got := c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
			"range": lspRange{positionOf([]byte(board), at), positionOf([]byte(board), at)}, "context": map[string]any{"diagnostics": []any{}}})
		list, _ := got.([]any)
		for _, a := range list {
			if m := a.(map[string]any); m["title"] == `Write perms.Has("orders.approve")` {
				fix = m
			}
		}
	}
	if fix == nil {
		t.Fatal("no quick fix on auth.Can")
	}
	applyWorkspace(t, fix["edit"])
	got := readFile(t, path)
	if !strings.Contains(got, `if perms.Has("orders.approve") {`) || strings.Contains(got, "web/v2/auth") {
		t.Errorf("after the fix:\n%s", got)
	}
	vets(t, dir)
}
