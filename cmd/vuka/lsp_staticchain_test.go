package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// staticChainSource mirrors the real ORM shape: a static `Objects` member and
// a generic static `Find[T]`, used in a method chain below a JSX construct.
const staticChainSource = `package main

import (
	"context"

	"github.com/vuka-lang/ui"
)

type Manager[T any] struct{ rows []*T }

func (m *Manager[T]) All(ctx context.Context) []*T { return m.rows }
func (m *Manager[T]) Count() int                   { return len(m.rows) }

type Model[Self any] struct {
	static Objects = Manager[Self]{}
}

type Task struct {
	Model
	Title string
}

func widget() ui.Node {
	return <div className="card">
		<h3>hello</h3>
	</div>
}

func run(ctx context.Context) {
	rows := Task.Objects.All(ctx)
	_ = rows
	_ = Task.Objects.Count()
}
`

func openChain(t *testing.T) (*lspClient, string) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": staticChainSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": staticChainSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })
	return c, uri
}

// TestLSPStaticChainHover (BUG D): hover on the method after a static chain,
// below a JSX construct, returns a non-empty signature mapped to the .vuka.
func TestLSPStaticChainHover(t *testing.T) {
	c, uri := openChain(t)
	hover := func(needle string, delta int) (string, lspRange) {
		off := strings.Index(staticChainSource, needle) + delta
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(staticChainSource), off)})
		b, _ := json.Marshal(v)
		var h map[string]any
		_ = json.Unmarshal(b, &h)
		r, _ := toRange(h["range"])
		return string(b), r
	}
	h, r := hover("Objects.All(ctx)", len("Objects."))
	if !strings.Contains(h, "All(ctx") {
		t.Fatalf("hover on All after static chain below JSX: %s", h)
	}
	// Range must land on the .vuka's own `All`, not an offset line.
	wantLine := positionOf([]byte(staticChainSource), strings.Index(staticChainSource, "Objects.All(ctx)")).Line
	if r.Start.Line != wantLine {
		t.Fatalf("hover range on line %d, want %d (%s)", r.Start.Line, wantLine, h)
	}
	if h, _ := hover("Objects.Count()", len("Objects.")); !strings.Contains(h, "Count()") {
		t.Fatalf("hover on Count after static chain: %s", h)
	}
}

// TestLSPStaticChainCompletion (BUG B): completion after a static chain's dot
// returns the manager's members.
func TestLSPStaticChainCompletion(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": staticChainSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": staticChainSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	src := strings.Replace(staticChainSource, "\t_ = Task.Objects.Count()", "\tTask.Objects.\n\t_ = Task.Objects.Count()", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	off := strings.Index(src, "Task.Objects.\n") + len("Task.Objects.")
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), off)})
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"All"`) || !strings.Contains(string(b), `"Count"`) {
		t.Fatalf("completion after static chain dot below JSX missing All/Count: %.600s", b)
	}
}

// TestLSPStaticChainDiagnostic (BUG C): a type error below a static chain and
// a JSX construct lands on its own .vuka line.
func TestLSPStaticChainDiagnostic(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": staticChainSource})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": staticChainSource}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	src := strings.Replace(staticChainSource, "\t_ = Task.Objects.Count()", "\t_ = Task.Objects.Count()\n\t_ = undefinedThing", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	want := positionOf([]byte(src), strings.Index(src, "undefinedThing")).Line
	ds := c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "undefined")
	})
	var lines []uint32
	for _, d := range ds {
		r, _ := toRange(d.(map[string]any)["range"])
		lines = append(lines, r.Start.Line)
		if r.Start.Line == want {
			return
		}
	}
	t.Fatalf("diagnostic for undefinedThing expected on line %d; got %v", want, lines)
}
