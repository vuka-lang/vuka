package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const lspSource = `package main

import "fmt"

type Circle struct{ R float64 }
type Rect struct{ W, H float64 }

func area(c Circle) float64 { return 3 * c.R * c.R }
func area(r Rect) float64   { return r.W * r.H }

func find(id int) Result[int] {
	if id == 0 {
		return Err(fmt.Errorf("none"))
	}
	return Ok(id)
}

func twice(id int) (int, error) {
	n := find(id)?
	return n * 2, nil
}

func main() {
	fmt.Println(area(Circle{R: 1}), area(Rect{W: 2, H: 3}))
	fmt.Println(twice(4))
}
`

// lspClient speaks LSP to the proxy over pipes.
type lspClient struct {
	t     *testing.T
	conn  *rpcConn
	mu    sync.Mutex
	next  int
	resps map[string]chan rpcMsg
	diags chan map[string]any
}

func (c *lspClient) loop() {
	for {
		body, err := c.conn.read()
		if err != nil {
			return
		}
		var m rpcMsg
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.isResponse():
			c.mu.Lock()
			ch := c.resps[string(m.ID)]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case m.Method == "textDocument/publishDiagnostics":
			var p map[string]any
			_ = json.Unmarshal(m.Params, &p)
			c.diags <- p
		case m.isRequest():
			_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
		}
	}
}

func (c *lspClient) call(method string, params any) any {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id, _ := json.Marshal(c.next)
	ch := make(chan rpcMsg, 1)
	c.resps[string(id)] = ch
	c.mu.Unlock()
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	select {
	case m := <-ch:
		if len(m.Error) > 0 {
			c.t.Fatalf("%s: %s", method, m.Error)
		}
		var v any
		_ = json.Unmarshal(m.Result, &v)
		return v
	case <-time.After(60 * time.Second):
		c.t.Fatalf("%s: no answer", method)
		return nil
	}
}

func (c *lspClient) notify(method string, params any) {
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// waitDiags returns the next diagnostics published for uri that satisfy ok.
func (c *lspClient) waitDiags(uri string, ok func([]any) bool) []any {
	c.t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case p := <-c.diags:
			if p["uri"] != uri {
				continue
			}
			ds, _ := p["diagnostics"].([]any)
			if ok(ds) {
				return ds
			}
		case <-deadline:
			c.t.Fatalf("no matching diagnostics for %s", uri)
			return nil
		}
	}
}

func startLSP(t *testing.T) (*lspClient, string, any) {
	gopls, err := findGopls("")
	if err != nil {
		t.Skip(err)
	}
	repo, _ := filepath.Abs("../..")
	dir := t.TempDir()
	mod := "module lsptest\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	os.WriteFile(filepath.Join(dir, "main.vuka"), []byte(lspSource), 0o644)
	t.Setenv("GOWORK", "off")

	toProxyR, toProxyW := io.Pipe()
	fromProxyR, fromProxyW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = runLSP(ctx, toProxyR, fromProxyW, io.Discard, gopls)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		toProxyW.Close()
		fromProxyR.Close()
		<-done
	})
	c := &lspClient{t: t, conn: newRPCConn(fromProxyR, toProxyW), resps: map[string]chan rpcMsg{}, diags: make(chan map[string]any, 100)}
	go c.loop()
	init := c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": map[string]any{}})
	c.notify("initialized", map[string]any{})
	return c, dir, init
}

func TestLSP(t *testing.T) {
	c, dir, init := startLSP(t)
	path := filepath.Join(dir, "main.vuka")
	uri := pathToURI(path)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": lspSource}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	at := func(needle string, delta int) map[string]any {
		off := strings.Index(lspSource, needle) + delta
		pos := positionOf([]byte(lspSource), off)
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}
	}

	t.Run("gopls commands are namespaced", func(t *testing.T) {
		caps := init.(map[string]any)["capabilities"].(map[string]any)
		cmds := caps["executeCommandProvider"].(map[string]any)["commands"].([]any)
		if len(cmds) == 0 {
			t.Fatal("no commands advertised")
		}
		for _, c := range cmds {
			if !strings.HasPrefix(c.(string), "vuka.gopls.") {
				t.Fatalf("command %v isn't namespaced; VS Code's Go extension registers gopls.* already", c)
			}
		}
		v := c.call("workspace/executeCommand", map[string]any{"command": "vuka.gopls.list_known_packages",
			"arguments": []any{map[string]any{"URI": pathToURI(filepath.Join(dir, "main_vuka.go"))}}})
		if b, _ := json.Marshal(v); !strings.Contains(string(b), "Packages") {
			t.Fatalf("executeCommand: %s", b)
		}
	})

	t.Run("hover on an overloaded call", func(t *testing.T) {
		v := c.call("textDocument/hover", at("area(Rect", 1))
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "func area(r Rect) float64") {
			t.Fatalf("hover: %s", b)
		}
	})

	t.Run("definition of an overload", func(t *testing.T) {
		v := c.call("textDocument/definition", at("area(Rect", 1))
		b, _ := json.Marshal(v)
		want := positionOf([]byte(lspSource), strings.Index(lspSource, "area(r Rect)"))
		if !strings.Contains(string(b), uri) || !strings.Contains(string(b), `"line":`+itoaTest(want.Line)) {
			t.Fatalf("definition: %s, want line %d of %s", b, want.Line, uri)
		}
	})

	t.Run("hover through ?", func(t *testing.T) {
		v := c.call("textDocument/hover", at("n * 2", 0))
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "var n int") {
			t.Fatalf("hover: %s", b)
		}
	})

	t.Run("rename", func(t *testing.T) {
		params := at("n * 2", 0)
		params["newName"] = "total"
		v := c.call("textDocument/rename", params)
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), uri) || strings.Contains(string(b), "_vuka.go") || strings.Count(string(b), `"newText":"total"`) != 2 {
			t.Fatalf("rename: %s", b)
		}
	})

	t.Run("outline", func(t *testing.T) {
		v := c.call("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": uri}})
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), `"name":"twice"`) || strings.Contains(string(b), "area__") {
			t.Fatalf("symbols: %s", b)
		}
	})

	t.Run("completion", func(t *testing.T) {
		src := strings.Replace(lspSource, "fmt.Println(twice(4))", "fmt.Println(twice(4))\n\tfmt.Pri", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, "fmt.Pri\n") + len("fmt.Pri")
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), off)})
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), `"label":"Println"`) {
			t.Fatalf("completion: %.400s", b)
		}
	})

	t.Run("type error lands on the .vuka line", func(t *testing.T) {
		src := strings.Replace(lspSource, "return n * 2, nil", `return n * "x", nil`, 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
			"contentChanges": []any{map[string]any{"text": src}}})
		want := positionOf([]byte(src), strings.Index(src, `n * "x"`)).Line
		c.waitDiags(uri, func(ds []any) bool {
			for _, d := range ds {
				r, _ := toRange(d.(map[string]any)["range"])
				if r.Start.Line == want {
					return true
				}
			}
			return false
		})
	})

	t.Run("vuka error as a diagnostic", func(t *testing.T) {
		src := strings.Replace(lspSource, "n := find(id)?", "n := find(id)?\n\t_ = area(true)", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 4},
			"contentChanges": []any{map[string]any{"text": src}}})
		c.waitDiags(uri, func(ds []any) bool {
			b, _ := json.Marshal(ds)
			return strings.Contains(string(b), "no overload of area accepts")
		})
	})
}

func itoaTest(n uint32) string {
	b, _ := json.Marshal(n)
	return string(b)
}
