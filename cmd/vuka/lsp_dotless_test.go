package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestLSPDotlessModuleStatics: in a module named without a dot (a local
// module like "app"), a .vuka file whose only Vuka construct is a
// cross-package static chain still lowers, and completion after the chain
// answers. Same-module imports used to read as the standard library, so the
// file skipped type-checking and its generated Go kept the raw chain.
func TestLSPDotlessModuleStatics(t *testing.T) {
	main := "package main\n\nimport (\n\t\"context\"\n\n\t\"lsptest/entities\"\n)\n\nfunc run(ctx context.Context) {\n\trows := entities.Task.Objects.All(ctx)\n\t_ = rows\n}\n"
	c, dir, _ := startLSPWith(t, map[string]string{
		"main.vuka":              main,
		"entities/entities.vuka": entitiesPkg,
	})
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
	if !strings.Contains(string(b), `"All"`) || !strings.Contains(string(b), `"Count"`) {
		t.Errorf("completion on a dotless module's static chain missing All/Count: %.600s", b)
	}
}
