package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loneImportSource has two lone `import "x"` lines and uses Result, so its
// generated Go imports the runtime on the package line; completing a symbol
// that needs a new import exercises the auto-import edit path end to end.
const loneImportSource = `package main

import "fmt"
import "strings"

func twice(id int) Result[int] {
	return Ok(id * 2)
}

func main() {
	fmt.Println(strings.ToUpper("x"))
	_ = twice(4)
}
`

// TestLSPConsolidatesImportsOnCompletion (BUG A): accepting an auto-import on a
// file with lone imports yields ONE grouped, sorted import block, not more
// separate `import "x"` lines.
func TestLSPConsolidatesImportsOnCompletion(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": loneImportSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": loneImportSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	src := strings.Replace(loneImportSource, "\t_ = twice(4)", "\t_ = twice(4)\n\tstrconv.Ito", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	off := strings.Index(src, "strconv.Ito") + len("strconv.Ito")
	// gopls offers unimported packages from an index it builds in the
	// background, and within a time budget, so a cold or busy gopls can
	// answer before it knows strconv.
	var item map[string]any
	for deadline := time.Now().Add(20 * time.Second); item == nil && time.Now().Before(deadline); {
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), off)})
		items := v
		if list, ok := v.(map[string]any); ok {
			items = list["items"]
		}
		its, _ := items.([]any)
		for _, it := range its {
			if m := it.(map[string]any); m["label"] == "Itoa" {
				item = m
				break
			}
		}
		if item == nil {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if item == nil {
		t.Fatal("no Itoa completion")
	}
	edits, _ := item["additionalTextEdits"].([]any)
	if len(edits) == 0 {
		t.Fatalf("Itoa completion carried no import edits")
	}
	got := applyEdits(src, edits)
	head := got[:strings.Index(got, "func twice")]
	want := "import (\n\t\"fmt\"\n\t\"strconv\"\n\t\"strings\"\n)"
	if !strings.Contains(head, want) {
		t.Fatalf("imports not consolidated into one sorted block:\n%s", head)
	}
	if strings.Count(head, "import") != 1 {
		t.Fatalf("expected a single import block, got %d `import` keywords:\n%s", strings.Count(head, "import"), head)
	}
}
