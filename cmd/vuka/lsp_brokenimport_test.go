package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// brokenImportCompletion opens main (main.vuka of files), types a member
// access on the cross-package static chain and returns the completion reply.
func brokenImportCompletion(t *testing.T, files map[string]string, main string) string {
	t.Helper()
	c, dir, _ := startLSPWith(t, files)
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": main}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	src := strings.Replace(main, "\t_ = rows", "\tentities.Task.Objects.\n\t_ = rows", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	off := strings.Index(src, "entities.Task.Objects.\n") + len("entities.Task.Objects.")
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), off)})
	b, _ := json.Marshal(v)
	return string(b)
}

// TestLSPCompletionBrokenImport: an error in a package the file imports — in
// a plain Go file of it — must not stop completions in the file: the package
// still type-checks to its declarations, and what is typed keeps reaching
// gopls instead of freezing at the last good generation.
func TestLSPCompletionBrokenImport(t *testing.T) {
	got := brokenImportCompletion(t, map[string]string{
		"main.vuka":              crossMain,
		"entities/entities.vuka": entitiesPkg,
		"entities/extra.go":      "package entities\n\nfunc bad() { alsoUndefined() }\n",
	}, crossMain)
	if !strings.Contains(got, `"All"`) || !strings.Contains(got, `"Count"`) {
		t.Errorf("completion with a broken import missing All/Count: %.600s", got)
	}
}

// TestLSPCompletionErrorsSameFile: a type error elsewhere in the same file
// doesn't stop completions either.
func TestLSPCompletionErrorsSameFile(t *testing.T) {
	broken := strings.Replace(crossMain, "func parse(s string) (int, error) {",
		"func oops() { undefinedName() }\n\nfunc parse(s string) (int, error) {", 1)
	got := brokenImportCompletion(t, map[string]string{
		"main.vuka":              broken,
		"entities/entities.vuka": entitiesPkg,
	}, broken)
	if !strings.Contains(got, `"All"`) || !strings.Contains(got, `"Count"`) {
		t.Errorf("completion with a same-file error missing All/Count: %.600s", got)
	}
}
