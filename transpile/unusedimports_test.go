package transpile

import (
	"bytes"
	"testing"
)

// TestDropUnusedImports: an import the generated Go adds and nothing names
// is blanked out, its bytes spaces so every offset holds; one it names, and
// the source's own, stay.
func TestDropUnusedImports(t *testing.T) {
	src := []byte("package p" + autoImport("uigen", "example.com/ui/uigen") + autoImport("ui", "example.com/ui") + "\n\nimport \"os\"\n\nvar _ = ui.X\n")
	f := &fileState{autoImports: map[string]string{"example.com/ui/uigen": "uigen", "example.com/ui": "ui"}}
	got := f.dropUnusedImports(src)
	if len(got) != len(src) {
		t.Fatalf("length %d, want %d", len(got), len(src))
	}
	if bytes.Contains(got, []byte("uigen")) || !bytes.Contains(got, []byte(autoImport("ui", "example.com/ui"))) || !bytes.Contains(got, []byte(`import "os"`)) {
		t.Errorf("got:\n%s", got)
	}
}
