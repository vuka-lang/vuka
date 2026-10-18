package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ormGenPkg mirrors vuka-orm: a QuerySet with plain methods and a generic
// method Values[R] that returns a projection type with its own chain methods.
const ormGenPkg = `package ormgen

import "context"

type QS[T any] struct{ rows []T }

func For[T any]() QS[T] { return QS[T]{} }

func (q QS[T]) Annotate(name string, v any) QS[T] { return q }
func (q QS[T]) Filter(f string) QS[T]             { return q }

func (q QS[T]) Values[R any](fields ...string) VS[T, R] { return VS[T, R]{} }

type VS[T, R any] struct{ q QS[T] }

func (v VS[T, R]) Having(f string) VS[T, R]            { return v }
func (v VS[T, R]) OrderBy(f string) VS[T, R]           { return v }
func (v VS[T, R]) All(ctx context.Context) ([]R, error) { return nil, nil }
`

// genMethodMain uses the generic-method chain: plain methods, then Values[Row],
// then more methods, then .All on a separate line.
const genMethodMain = `package main

import (
	"context"

	"lsptest/ormgen"
)

type Task struct{ ID int }

type Row struct {
	ID   int
	Name string
}

func run(ctx context.Context) {
	letters := ormgen.For[Task]().
		Annotate("employer_name", "x").
		Filter("a").
		Values[Row]("id", "name").
		Having("b").
		OrderBy("c")
	rows, _ := letters.All(ctx)
	_ = rows
}
`

// startLSPGo127 sets up the proxy on a go 1.27 module (generic methods need
// the 1.27 language version), with the vuka runtime replaced locally.
func startLSPGo127(t *testing.T, files map[string]string) (*lspClient, string) {
	if _, err := findGopls(""); err != nil {
		needs(t, "%v", err)
	}
	dir := t.TempDir()
	repo, _ := filepath.Abs("../..")
	mod := "module lsptest\n\ngo 1.27.1\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	ownSum, _ := os.ReadFile("go.sum")
	os.WriteFile(filepath.Join(dir, "go.sum"), ownSum, 0o644)
	for name, src := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	t.Setenv("GOWORK", "off")
	gopls, _ := findGopls("")
	toProxyR, toProxyW := io.Pipe()
	fromProxyR, fromProxyW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = runLSP(ctx, toProxyR, fromProxyW, io.Discard, gopls, lspOptions{renameCommands: true})
		close(done)
	}()
	t.Cleanup(func() { cancel(); toProxyW.Close(); fromProxyR.Close(); <-done })
	c := &lspClient{t: t, conn: newRPCConn(fromProxyR, toProxyW), resps: map[string]chan rpcMsg{}, diags: make(chan map[string]any, 100)}
	go c.loop()
	c.init = c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": map[string]any{}})
	c.notify("initialized", map[string]any{})
	return c, dir
}

// TestLSPGenericMethodChainHover (symptom 1): hover on every method of a chain
// that includes a generic method Values[R] returns a non-empty signature — in
// particular the methods AFTER the generic-method call.
func TestLSPGenericMethodChainHover(t *testing.T) {
	c, dir := startLSPGo127(t, map[string]string{
		"main.vuka":          genMethodMain,
		"ormgen/ormgen.vuka": ormGenPkg,
	})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": genMethodMain}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	hover := func(needle string, delta int) string {
		off := strings.Index(genMethodMain, needle) + delta
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(genMethodMain), off)})
		b, _ := json.Marshal(v)
		return string(b)
	}

	checks := []struct {
		needle, delta, want string
	}{
		{"Annotate(", "", "Annotate"},
		{"Filter(\"a\")", "", "Filter"},
		{"Values[Row]", "", "Values"},
		{"Having(\"b\")", "", "Having"},
		{"OrderBy(\"c\")", "", "OrderBy"},
		{"letters.All(ctx)", "letters.", "All"},
	}
	for _, ck := range checks {
		h := hover(ck.needle, len(ck.delta))
		if !strings.Contains(h, ck.want+"(") {
			t.Errorf("hover on %q (after generic method: %v): got %.300s", ck.want,
				ck.needle == "Having(\"b\")" || ck.needle == "OrderBy(\"c\")" || ck.needle == "letters.All(ctx)", h)
		}
	}
}
