package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestRouteFolderTidy: gopls's go.mod complaints about route folders'
// import paths are dropped; others are kept.
func TestRouteFolderTidy(t *testing.T) {
	ds := []any{
		map[string]any{"message": `example.com/app/pages/(admin): malformed import path "example.com/app/pages/(admin)": invalid char '('`},
		map[string]any{"message": `malformed import path "example.com/app/pages/orgs/[org]/permits": invalid char '['`},
		map[string]any{"message": `example.com/x is not used in this module`},
	}
	kept, ok := withoutRouteFolderPaths(ds)
	if !ok || len(kept) != 1 || !strings.Contains(kept[0].(map[string]any)["message"].(string), "not used") {
		t.Errorf("kept %v", kept)
	}
	if _, ok := withoutRouteFolderPaths(ds[2:]); ok {
		t.Error("dropped one about something else")
	}
}
