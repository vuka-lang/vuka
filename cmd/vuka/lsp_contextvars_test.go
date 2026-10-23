package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const ctxVarsMain = `package main

import (
	"lsptest/views"

	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

var _ views.Header

func main() {
	web.New(web.ContextProcessors(web.RequestContext, auth.Context))
}
`

const ctxVarsHeader = `package views

// Header reads the context processors' variables by name.
type Header struct{ Title string }
<header>
  if user != nil {
    <span>{user.Name}</span>
  }
  <small>{request.Path}</small>
</header>
`

// startContextLSP is the proxy on a module whose main lists web's and
// auth's context processors (the golden module's stand-ins).
func startContextLSP(t *testing.T) (*lspClient, string, string, any) {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	web := filepath.Join(repo, "transpile", "testdata", "golden", "webretired")
	c, dir, init := startLSPWithMod(t, map[string]string{"main.go": ctxVarsMain, "views/header.vuka.html": ctxVarsHeader},
		func(mod string) string {
			return strings.Replace(mod, "require (", "require (\n\tgithub.com/vuka-lang/web/v2 v2.0.0", 1) + "\nreplace github.com/vuka-lang/web/v2 => " + web + "\n"
		})
	uri := pathToURI(filepath.Join(dir, "views", "header.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": ctxVarsHeader}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	return c, dir, uri, init
}

// TestLSPContextVars: a processor variable's hover names its type and
// processor, definition goes to the processor and its entry in main, and
// completion offers the variables.
func TestLSPContextVars(t *testing.T) {
	c, dir, uri, init := startContextLSP(t)
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(ctxVarsHeader), strings.Index(ctxVarsHeader, needle)+delta)}
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at("{user.Name}", 2)))
	if !strings.Contains(string(hover), "var user *auth.Identity") || !strings.Contains(string(hover), "from auth.Context (auth.go)") {
		t.Errorf("hover on user: %s", hover)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at("{request.Path}", 3)))
	if !strings.Contains(string(def), "webretired/vars.go") || !strings.Contains(string(def), pathToURI(filepath.Join(dir, "main.go"))) {
		t.Errorf("definition of request: %s", def)
	}
	// The member's completion is gopls's, on the variable's type.
	got, _ := json.Marshal(c.call("textDocument/completion", at("{request.Path}", 4)))
	if !strings.Contains(string(got), `"label":"request"`) {
		t.Errorf("completion of req…: %s", got)
	}
	// The variable is its own kind of semantic token.
	legend := semTokLegendFrom(t, init)
	kind, ok := legend["vukaContextVar"]
	if !ok {
		t.Fatalf("no vukaContextVar in the legend: %v", legend)
	}
	v := c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}})
	res, _ := v.(map[string]any)
	pos := positionOf([]byte(ctxVarsHeader), strings.Index(ctxVarsHeader, "user != nil"))
	found := false
	for _, tk := range decodeTokens(t, res) {
		found = found || tk.line == pos.Line && tk.char == pos.Character && tk.length == 4 && tk.typ == kind
	}
	if !found {
		t.Errorf("no vukaContextVar token on user: %v", res)
	}
}
