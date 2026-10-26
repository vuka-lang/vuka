package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspSignup = `package lsptest

import "github.com/vuka-lang/ui/v2"

type Signup struct {
	ui.FormState
	Email string
	Name  string
}
`

const lspJoin = `<script lang="go">
package lsptest

import "github.com/vuka-lang/ui/v2"

type Join struct {
	ui.Live
	Form Signup
}
</script>

<main>
  <ui.Form for={Form}>
    <label for={Form.Email}>Email</label>
    <input model={Form.Email}/>
  </ui.Form>
</main>
`

// TestLSPModel: a model attribute's field path is Go to the editor: hover
// names the field, completion offers the form's fields, and a misspelt one
// is a diagnostic.
func TestLSPModel(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"signup.go": lspSignup, "join.vuka.html": lspJoin})
	uri := pathToURI(filepath.Join(dir, "join.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": lspJoin}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	pos := positionOf([]byte(lspJoin), strings.Index(lspJoin, "model={Form.Email}")+len("model={Form.E"))
	b, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}))
	if !strings.Contains(string(b), "Email string") {
		t.Errorf("hover on a model's field: %s", b)
	}
	typing := strings.Replace(lspJoin, "model={Form.Email}", "model={Form.Na}", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": typing}}})
	if text := completionText(c, uri, typing, "model={Form.Na", "Name"); text != "Name" {
		t.Errorf("completing a model's field inserts %q", text)
	}
	wrong := strings.Replace(lspJoin, "model={Form.Email}", "model={Form.Emial}", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": wrong}}})
	ds := c.waitDiags(uri, func(ds []any) bool { b, _ := json.Marshal(ds); return strings.Contains(string(b), "Emial") })
	if b, _ := json.Marshal(ds); !strings.Contains(string(b), "did you mean Email") {
		t.Errorf("a misspelt field: %s", b)
	}
}
