package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// driftSource has a JSX construct above a plain generic method-chain line, to
// probe whether positions after a vuka construct still map correctly.
const driftSource = `package main

import "github.com/vuka-lang/ui"

type Task struct{ ID int }

type Manager[T any] struct{ items []T }

func (m *Manager[T]) All() []T { return m.items }

func NewManager[T any]() *Manager[T] { return &Manager[T]{} }

func widget() ui.Node {
	return <div className="card">
		<h3>hello</h3>
	</div>
}

func run() {
	mgr := NewManager[Task]()
	_ = mgr.All()
}
`

func driftItems(c *lspClient, uri, src string, needle string, delta int) []map[string]any {
	off := strings.Index(src, needle) + delta
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), off)})
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	var out []map[string]any
	arr, _ := items.([]any)
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func hasLabel(items []map[string]any, label string) bool {
	for _, it := range items {
		if it["label"] == label {
			return true
		}
	}
	return false
}

// TestLSPDriftCompletion (BUG B): completion on a plain method chain below a
// JSX construct returns the right members.
func TestLSPDriftCompletion(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": driftSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": driftSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	// Complete right after "mgr." in "_ = mgr.All()".
	src := strings.Replace(driftSource, "_ = mgr.All()", "_ = mgr.\n\t_ = mgr.All()", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	items := driftItems(c, uri, src, "_ = mgr.\n", len("_ = mgr."))
	if !hasLabel(items, "All") {
		var labels []string
		for _, it := range items {
			labels = append(labels, it["label"].(string))
		}
		t.Fatalf("completion after mgr. below JSX missing All; got %v", labels)
	}
}

// TestLSPDriftHover (BUG D): hover on a method and a type arg below a JSX
// construct returns a non-empty signature.
func TestLSPDriftHover(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": driftSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": driftSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	hover := func(needle string, delta int) string {
		off := strings.Index(driftSource, needle) + delta
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(driftSource), off)})
		b, _ := json.Marshal(v)
		return string(b)
	}
	if h := hover("mgr.All()", 4); !strings.Contains(h, "All()") {
		t.Fatalf("hover on All below JSX: %s", h)
	}
	if h := hover("NewManager[Task]()", len("NewManager[")); !strings.Contains(h, "type Task struct") {
		t.Fatalf("hover on type arg Task below JSX: %s", h)
	}
}

// TestLSPDriftDiagnostic (BUG C): a type error below a JSX construct lands on
// its own line in the .vuka file.
func TestLSPDriftDiagnostic(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": driftSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": driftSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	// Introduce an undefined identifier on the line below the chain.
	src := strings.Replace(driftSource, "_ = mgr.All()", "_ = mgr.All()\n\t_ = undefinedThing", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	want := positionOf([]byte(src), strings.Index(src, "undefinedThing")).Line
	ds := c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "undefinedThing") || strings.Contains(string(b), "undefined")
	})
	var lines []uint32
	landed := false
	for _, d := range ds {
		r, _ := toRange(d.(map[string]any)["range"])
		lines = append(lines, r.Start.Line)
		if r.Start.Line == want {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("diagnostic for undefinedThing on line %d; got lines %v", want, lines)
	}
}
