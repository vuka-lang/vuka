package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspAttrsSource = `package main

import "fmt"

// Path binds a parameter from the URL.
@vuka.Targets(vuka.OnParam)
type Path string

func Get(path string) func(*vuka.Decl) { return func(d *vuka.Decl) {} }

decorator timed(c) { c.Next() }

func Transactional(opts ...string) func(*vuka.Call) { return func(c *vuka.Call) { c.Next() } }

decorator ApiRoute(path string) = @Get(path) @timed

@ApiRoute("/pets/{id}")
@Transactional
func show(@Path("id") id string) string { return fmt.Sprint(id) }

func main() { show("1") }
`

// TestLSPParamAttrs covers the newer attribute spots: hover on a parameter
// attribute and on a composed decorator, and completion while one is typed.
func TestLSPParamAttrs(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": lspAttrsSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": lspAttrsSource}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	hover := func(src, needle string, delta int) string {
		pos := positionOf([]byte(src), strings.Index(src, needle)+delta)
		b, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}))
		return string(b)
	}
	if got := hover(lspAttrsSource, `@Path("id")`, 2); !strings.Contains(got, "type Path string") {
		t.Fatalf("hover on a parameter attribute: %s", got)
	}
	if got := hover(lspAttrsSource, `@ApiRoute(`, 2); !strings.Contains(got, "func ApiRoute(path string)") {
		t.Fatalf("hover on a composed decorator: %s", got)
	}
	if got := hover(lspAttrsSource, `@Transactional`+"\n", 2); !strings.Contains(got, "func Transactional(opts ...string)") {
		t.Fatalf("hover on an optional-argument decorator: %s", got)
	}

	// Typing a parameter's attribute: the rest of the file keeps working, and
	// the attribute's type is offered.
	typing := strings.Replace(lspAttrsSource, `func show(@Path("id") id string)`, `func show(@Pa id string)`, 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": typing}}})
	if text := completionText(c, uri, typing, "show(@Pa", "Path"); text != "Path" {
		t.Fatalf("at show(@Pa, picking Path inserts %q", text)
	}
	bare := strings.Replace(lspAttrsSource, `func show(@Path("id") id string)`, `func show(@ id string)`, 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": bare}}})
	if text := completionText(c, uri, bare, "show(@", "Path"); text != "Path" {
		t.Fatalf("at show(@, picking Path inserts %q", text)
	}
	// And a composed decorator's next element.
	elem := strings.Replace(lspAttrsSource, `@Get(path) @timed`, `@Get(path) @ti`, 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 4},
		"contentChanges": []any{map[string]any{"text": elem}}})
	if text := completionText(c, uri, elem, "@Get(path) @ti", "timed"); text != "timed" {
		t.Fatalf("in a composed decorator, picking timed inserts %q", text)
	}
}
