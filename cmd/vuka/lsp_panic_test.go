package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestSyntaxPanic: a compiler panic while checking a file is the file's
// E0001 diagnostic, and the server goes on.
func TestSyntaxPanic(t *testing.T) {
	orig := syntaxCheck
	t.Cleanup(func() { syntaxCheck = orig })
	syntaxCheck = func(*syntaxChecker, string, []byte, []transpile.File) transpile.ErrorList { panic("boom") }
	var log strings.Builder
	p := &proxy{bufs: map[string][]byte{}, log: &log}
	d := p.syntaxOf(t.TempDir()+"/page.vuka.html", []byte("<script lang=\"go\">\npackage main\n</script>\n"))
	b, _ := json.Marshal(d.list)
	if len(d.list) != 1 || !strings.Contains(string(b), "E0001") || !strings.Contains(string(b), "boom") {
		t.Errorf("diagnostics %s", b)
	}
	if !strings.Contains(log.String(), "panic checking") || !strings.Contains(log.String(), "goroutine") {
		t.Errorf("log: %s", log.String())
	}
}

// TestSyntaxMidEdit: a page being written, its struct and a tag half typed,
// in both forms, is checked without a panic.
func TestSyntaxMidEdit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	var log strings.Builder
	p := &proxy{bufs: map[string][]byte{}, log: &log}
	head := "package main\n\ntype Page struct {\n\tTitle string\n}\n"
	for _, src := range []string{head, head + "  <di", "<script lang=\"go\">\n" + head + "</script>\n", "<script lang=\"go\">\n" + head + "</script>\n\n  <di"} {
		p.syntaxOf(filepath.Join(dir, "page.vuka.html"), []byte(src))
	}
	if strings.Contains(log.String(), "panic") {
		t.Errorf("log: %s", log.String())
	}
}

// TestRouteFolderTidy: gopls's go.mod complaints about the module's route
// folders' import paths are dropped; others, and the rest of a message that
// also reports something else, are kept.
func TestRouteFolderTidy(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "go.mod")
	os.WriteFile(mod, []byte("module example.com/shop\n\ngo 1.25\n"), 0o644)
	ds := []any{
		map[string]any{"message": `example.com/shop/pages/(admin): malformed import path "example.com/shop/pages/(admin)": invalid char '('`},
		map[string]any{"message": `malformed import path "example.com/shop/pages/orders/[order]/items": invalid char '['`},
		map[string]any{"message": `example.com/x is not used in this module`},
		map[string]any{"message": `malformed import path "example.com/other/pages/[id]": invalid char '['`},
		map[string]any{"message": "go: example.com/shop/pages/products/[id]: malformed import path \"example.com/shop/pages/products/[id]\": invalid char '['\ngo: example.com/shop imports\n\texample.org/missing: cannot find module"},
	}
	kept, ok := withoutRouteFolderPaths(mod, ds)
	var msgs []string
	for _, d := range kept {
		msgs = append(msgs, d.(map[string]any)["message"].(string))
	}
	want := []string{`example.com/x is not used in this module`, `malformed import path "example.com/other/pages/[id]": invalid char '['`,
		"go: example.com/shop imports\n\texample.org/missing: cannot find module"}
	if !ok || !slices.Equal(msgs, want) {
		t.Errorf("kept %q", msgs)
	}
	if _, ok := withoutRouteFolderPaths(mod, ds[2:4]); ok {
		t.Error("dropped one about something else")
	}
	if _, ok := withoutRouteFolderPaths(filepath.Join(dir, "main.go"), ds); ok {
		t.Error("dropped one on a file other than go.mod")
	}

	logged := func(stderr string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"type": 1, "message": "2026/11/08 10:00:00 tidy: diagnosing " + pathToURI(mod) + ": err: exit status 1: stderr: " + stderr})
		return b
	}
	route := "go: example.com/shop/pages/products/[id]: malformed import path \"example.com/shop/pages/products/[id]\": invalid char '['\n"
	if _, drop := withoutRouteTidy(logged(route + "\n")); !drop {
		t.Error("a tidy run failing only on route folders is still shown")
	}
	other := "go: example.com/shop imports\n\texample.org/missing: cannot find module\n"
	out, drop := withoutRouteTidy(logged(route + other))
	var got struct{ Message string }
	json.Unmarshal(out, &got)
	if drop || strings.Contains(got.Message, "[id]") || !strings.HasSuffix(got.Message, "stderr: "+other) {
		t.Errorf("mixed failure: drop %v, message %q", drop, got.Message)
	}
	for _, params := range []json.RawMessage{logged(other), []byte(`{"type":3,"message":"Finished loading packages."}`)} {
		if out, drop := withoutRouteTidy(params); drop || string(out) != string(params) {
			t.Errorf("changed %s: %s", params, out)
		}
	}
}

// TestLSPRouteFolderTidy: in an app with a [param] folder, gopls's failed
// tidy run never reaches the editor and go.mod has no diagnostic, while a
// genuine go.mod problem is still reported.
func TestLSPRouteFolderTidy(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{
		"main.go":                        "package main\n\nfunc main() {}\n",
		"pages/products/[id]/product.go": "package id\n\nconst Name = \"product\"\n",
	})
	mod := filepath.Join(dir, "go.mod")
	uri := pathToURI(mod)
	src, _ := os.ReadFile(mod)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go.mod", "version": 1, "text": string(src)}})
	if ds := c.waitDiags(uri, func([]any) bool { return true }); len(ds) != 0 {
		t.Errorf("go.mod diagnostics: %v", ds)
	}
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": string(src) + "\nbogus directive\n"}}})
	c.waitDiagsAt(uri, 2, func(ds []any) bool { return len(ds) > 0 })
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.shown {
		if strings.Contains(m, "malformed import path") {
			t.Errorf("shown: %s", m)
		}
	}
}

// TestGoVersionDiagnostics: a module built by a newer Go than vuka's is a
// diagnostic on each open file, or on the first file when none is open.
func TestGoVersionDiagnostics(t *testing.T) {
	e := &load.GoVersionError{Vuka: "go1.25.3", Module: "go1.27.1"}
	pkgs := []*load.Package{
		{Dir: "/app", Files: []transpile.File{{Name: "main.go"}, {Name: "main.vuka"}}},
		{Dir: "/app/views", Files: []transpile.File{{Name: "page.vuka.html"}}},
	}
	files := func(list transpile.ErrorList) []string {
		var out []string
		for _, d := range list {
			if !strings.Contains(d.Msg, "built with go1.25.3, but this module builds with go1.27.1") {
				t.Errorf("message %q", d.Msg)
			}
			out = append(out, d.Pos.Filename)
		}
		return out
	}
	if got := files(goVersionDiagnostics(pkgs, nil, e)); strings.Join(got, " ") != "/app/main.vuka" {
		t.Errorf("none open: %v", got)
	}
	bufs := map[string][]byte{"/app/views/page.vuka.html": nil}
	if got := files(goVersionDiagnostics(pkgs, bufs, e)); strings.Join(got, " ") != "/app/views/page.vuka.html" {
		t.Errorf("one open: %v", got)
	}
}
