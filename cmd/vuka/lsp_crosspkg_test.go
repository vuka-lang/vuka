package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// entitiesPkg declares an ORM-style model with a static Objects member in
// another package.
const entitiesPkg = `package entities

import "context"

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
`

// crossMain uses the cross-package static chain below a JSX construct.
const crossMain = `package main

import (
	"context"

	"github.com/vuka-lang/ui"
	"lsptest/entities"
)

func widget() ui.Node {
	return <div className="card">
		<h3>hi</h3>
	</div>
}

func run(ctx context.Context) {
	rows := entities.Task.Objects.All(ctx)
	_ = rows
}
`

// TestLSPCrossPkgStaticChain (BUG B/C/D, cross-package): hover, completion and
// diagnostics on a cross-package static chain below a JSX construct map right.
func TestLSPCrossPkgStaticChain(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{
		"main.vuka":              crossMain,
		"entities/entities.vuka": entitiesPkg,
	})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": crossMain}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	// BUG D: hover on the method after the cross-package static chain.
	off := strings.Index(crossMain, "Objects.All(ctx)") + len("Objects.")
	v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(crossMain), off)})
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), "All(ctx") {
		t.Fatalf("hover on All after cross-package static chain: %s", b)
	}
	var h map[string]any
	_ = json.Unmarshal(b, &h)
	r, _ := toRange(h["range"])
	wantLine := positionOf([]byte(crossMain), strings.Index(crossMain, "Objects.All(ctx)")).Line
	if r.Start.Line != wantLine {
		t.Fatalf("hover range line %d, want %d: %s", r.Start.Line, wantLine, b)
	}

	// BUG B: completion after the chain's dot.
	src := strings.Replace(crossMain, "\t_ = rows", "\tentities.Task.Objects.\n\t_ = rows", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": src}}})
	coff := strings.Index(src, "entities.Task.Objects.\n") + len("entities.Task.Objects.")
	cv := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), coff)})
	cb, _ := json.Marshal(cv)
	if !strings.Contains(string(cb), `"All"`) || !strings.Contains(string(cb), `"Count"`) {
		t.Fatalf("completion after cross-package static chain missing All/Count: %.600s", cb)
	}

	// BUG C: a type error below the chain lands on its own line.
	src2 := strings.Replace(crossMain, "\t_ = rows", "\t_ = rows\n\t_ = undefinedThing", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": src2}}})
	want := positionOf([]byte(src2), strings.Index(src2, "undefinedThing")).Line
	ds := c.waitDiags(uri, func(ds []any) bool {
		db, _ := json.Marshal(ds)
		return strings.Contains(string(db), "undefined")
	})
	for _, d := range ds {
		rr, _ := toRange(d.(map[string]any)["range"])
		if rr.Start.Line == want {
			return
		}
	}
	dsb, _ := json.Marshal(ds)
	t.Fatalf("diagnostic for undefinedThing expected on line %d: %s", want, dsb)
}
