package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const trySource = `package main

import "strconv"

func exists(email string) (bool, error) { return email == "", nil }

func price(item string) (int, error) { return len(item), nil }

func register(email, item string) (int, error) {
	if exists(email)? {
		return 0, nil
	}
	total := price(item)? * strconv.Atoi(email)?("parsing %q", email)
	return total, nil
}
`

// TestLSPTryInExpression: hover and definition on calls a ? moves out of
// an if's condition and an expression still answer about the call, and
// the file has no diagnostics.
func TestLSPTryInExpression(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": trySource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": trySource}})
	ds := c.waitDiags(uri, func(ds []any) bool { return true })
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %v", ds)
	}
	ask := func(method, needle string, delta int) string {
		off := strings.Index(trySource, needle) + delta
		v := c.call(method, map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(trySource), off)})
		b, _ := json.Marshal(v)
		return string(b)
	}
	if h := ask("textDocument/hover", "exists(email)?", 2); !strings.Contains(h, "func exists(email string) (bool, error)") {
		t.Errorf("hover on exists in an if's condition: %s", h)
	}
	if h := ask("textDocument/hover", "price(item)?", 2); !strings.Contains(h, "func price(item string) (int, error)") {
		t.Errorf("hover on price in an expression: %s", h)
	}
	if h := ask("textDocument/hover", "Atoi(email)", 1); !strings.Contains(h, "func strconv.Atoi(s string) (int, error)") {
		t.Errorf("hover on Atoi with ?(…): %s", h)
	}
	def := ask("textDocument/definition", "exists(email)?", 2)
	if !strings.Contains(def, `"line":4`) || !strings.Contains(def, "main.vuka") {
		t.Errorf("definition of exists: %s", def)
	}
}
