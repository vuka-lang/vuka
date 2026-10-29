package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const ambigMain = `package main

import "lsptest/views"

var _ views.List

func main() {}
`

const ambigList = `package views

import "context"

// List's Items has an overload taking the render's ctx and one taking
// nothing: the markup's Items() compiles both ways.
type List struct{}

func (l List) Items() string { return "every item" }

func (l List) Items(ctx context.Context) string { return "items for the request" }
<p>{Items()}</p>
`

// TestLSPAmbiguousCtx: a markup call both with and without the render's
// ctx fits is a W0108 warning in the editor, whose quick fixes write the
// ctx or the marker keeping the call without it.
func TestLSPAmbiguousCtx(t *testing.T) {
	c, dir, _ := startLSPWithMod(t, map[string]string{"main.go": ambigMain, "views/list.vuka.html": ambigList}, nil)
	uri := pathToURI(filepath.Join(dir, "views", "list.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": ambigList}})
	var warning map[string]any
	c.waitDiags(uri, func(ds []any) bool {
		for _, d := range ds {
			if m, ok := d.(map[string]any); ok && m["code"] == "W0108" {
				warning = m
				return true
			}
		}
		return false
	})
	if warning["severity"] != float64(2) || !strings.Contains(warning["message"].(string), "Items() compiles as written while Items(ctx) also fits") {
		t.Fatalf("warning: %v", warning)
	}
	at := strings.Index(ambigList, "Items()}")
	v := c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
		"range":   lspRange{positionOf([]byte(ambigList), at), positionOf([]byte(ambigList), at)},
		"context": map[string]any{"diagnostics": []any{warning}, "only": []string{"quickfix"}}})
	b, _ := json.Marshal(v)
	var actions []struct {
		Title string `json:"title"`
		Edit  struct {
			DocumentChanges []struct {
				Edits []any `json:"edits"`
			} `json:"documentChanges"`
		} `json:"edit"`
	}
	json.Unmarshal(b, &actions)
	got := map[string]string{}
	for _, a := range actions {
		if strings.Contains(a.Title, "ctx") && len(a.Edit.DocumentChanges) == 1 {
			got[a.Title] = applyEdits(ambigList, a.Edit.DocumentChanges[0].Edits)
		}
	}
	want := map[string]string{
		"Pass the render's ctx: Items(ctx)":                "<p>{Items(ctx)}</p>",
		"Keep the call without ctx: Items(/*vuka:noctx*/)": "<p>{Items(/*vuka:noctx*/)}</p>",
	}
	for title, line := range want {
		text, ok := got[title]
		if !ok {
			t.Errorf("no action %q: %s", title, b)
			continue
		}
		if !strings.Contains(text, line+"\n") {
			t.Errorf("%s:\n%s", title, text)
		}
	}
}
