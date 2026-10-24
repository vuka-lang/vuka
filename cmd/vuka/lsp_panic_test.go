package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
