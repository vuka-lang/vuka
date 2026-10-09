package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const lspSource = `package main

import (
	"fmt"
)

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
	return startLSPWith(t, map[string]string{"main.vuka": lspSource})
}

func startLSPWith(t *testing.T, files map[string]string) (*lspClient, string, any) {
	return startLSPOpts(t, files, lspOptions{renameCommands: true})
}

func startLSPOpts(t *testing.T, files map[string]string, opts lspOptions) (*lspClient, string, any) {
	gopls, err := findGopls("")
	if err != nil {
		t.Skip(err)
	}
	repo, _ := filepath.Abs("../..")
	dir := t.TempDir()
	mod := "module lsptest\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	// The runtime's own requirements (templ), as checksums go can trust offline.
	sum, _ := os.ReadFile(filepath.Join(repo, "go.sum"))
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	for name, src := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	t.Setenv("GOWORK", "off")

	toProxyR, toProxyW := io.Pipe()
	fromProxyR, fromProxyW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = runLSP(ctx, toProxyR, fromProxyW, io.Discard, gopls, opts)
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
			"arguments": []any{map[string]any{"URI": pathToURI(filepath.Join(realDir(dir), "main_vuka.go"))}}})
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

	t.Run("completion adds an import to the source's imports", func(t *testing.T) {
		// The source uses Result, so its generated Go imports the runtime on
		// the package line; gopls rewrites that when it adds an import.
		src := strings.Replace(lspSource, "\tfmt.Println(twice(4))", "\tfmt.Println(twice(4))\n\tstrconv.Ito", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 5},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, "strconv.Ito") + len("strconv.Ito")
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), off)})
		items := v
		if list, ok := v.(map[string]any); ok {
			items = list["items"]
		}
		for _, it := range items.([]any) {
			item := it.(map[string]any)
			if item["label"] != "Itoa" {
				continue
			}
			edits, _ := item["additionalTextEdits"].([]any)
			got := applyEdits(src, edits)
			if !strings.Contains(got, "import (\n\t\"fmt\"\n\t\"strconv\"\n)") || strings.Count(got, "import") != 1 {
				t.Fatalf("after the import edits:\n%s", got[:strings.Index(got, "type Circle")])
			}
			return
		}
		t.Fatal("no Itoa completion")
	})

	t.Run("completion while typing a decorator", func(t *testing.T) {
		src := strings.Replace(lspSource, "func twice(", `decorator logged(c) {
	c.Ne
}

@
func twice(`, 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 6},
			"contentChanges": []any{map[string]any{"text": src}}})
		complete := func(after string) []string {
			off := strings.Index(src, after) + len(after)
			v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
				"position": positionOf([]byte(src), off)})
			items := v
			if list, ok := v.(map[string]any); ok {
				items = list["items"]
			}
			var labels []string
			for _, it := range items.([]any) {
				item := it.(map[string]any)
				labels = append(labels, item["sortText"].(string)+" "+item["label"].(string))
			}
			sort.Strings(labels)
			return labels
		}
		// The unfinished @ below must not stop the rest of the file working.
		if got := strings.Join(complete("c.Ne"), ","); !strings.Contains(got, "Next") {
			t.Fatalf("c.Ne: %s", got)
		}
		if text := completionText(c, uri, src, "\n@", "logged"); text != "logged" {
			t.Fatalf("picking logged inserts %q; a decorator is named, not called", text)
		}
		// Typing on: @lo still offers the decorator, and the list says it is
		// complete, so the editor filters it rather than asking again.
		typing := strings.Replace(src, "\n@\n", "\n@lo\n", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 61},
			"contentChanges": []any{map[string]any{"text": typing}}})
		if text := completionText(c, uri, typing, "\n@lo", "logged"); text != "logged" {
			t.Fatalf("at @lo, picking logged inserts %q", text)
		}
		off := strings.Index(typing, "\n@lo") + 4
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(typing), off)})
		if list, ok := v.(map[string]any); !ok || list["isIncomplete"] != false {
			t.Fatalf("the @ list should be complete, so the editor filters it: %v", v)
		}
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 62},
			"contentChanges": []any{map[string]any{"text": src}}})
		got := complete("\n@")
		if len(got) == 0 || !strings.HasSuffix(got[0], " logged") {
			t.Fatalf("@: decorators should come first: %v", got)
		}
		for _, l := range got {
			if strings.HasSuffix(l, " id") || strings.HasSuffix(l, " nil") {
				t.Fatalf("@ offers %q, which no attribute can name", l)
			}
		}
	})

	t.Run("code actions over an attribute", func(t *testing.T) {
		src := strings.Replace(lspSource, "func twice(", "@Tag{}\nfunc twice(", 1)
		src = strings.Replace(src, "type Circle struct", "type Tag struct{}\n\ntype Circle struct", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 7},
			"contentChanges": []any{map[string]any{"text": src}}})
		at := strings.Index(src, "@Tag{}")
		// From inside the attribute to the line after it: the ends map apart.
		c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
			"range":   lspRange{positionOf([]byte(src), at+2), positionOf([]byte(src), at+12)},
			"context": map[string]any{"diagnostics": []any{}}})
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

// applyEdits applies LSP text edits to src.
func applyEdits(src string, edits []any) string {
	type ed struct {
		s, e int
		text string
	}
	var list []ed
	for _, e := range edits {
		m := e.(map[string]any)
		r, _ := toRange(m["range"])
		list = append(list, ed{offsetOf([]byte(src), r.Start), offsetOf([]byte(src), r.End), m["newText"].(string)})
	}
	for i := len(list) - 1; i >= 0; i-- {
		for j := 0; j < i; j++ {
			if list[j].s < list[j+1].s {
				list[j], list[j+1] = list[j+1], list[j]
			}
		}
	}
	for _, e := range list {
		src = src[:e.s] + e.text + src[e.e:]
	}
	return src
}

func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}

func TestLSPProjectDecorators(t *testing.T) {
	main := `package main

import "fmt"

@
func add(a, b int) int { return a + b }

func main() { fmt.Println(add(1, 2)) }
`
	c, dir, init := startLSPWith(t, map[string]string{
		"main.vuka":               main,
		"decorators/logging.vuka": "package decorators\n\ndecorator Logged(c) { c.Next() }\n",
		"decorators/timing.go": `package decorators

import "github.com/vuka-lang/vuka"

func Timed(c *vuka.Call) { c.Next() }

func Retry(times int) vuka.Decorator { return func(c *vuka.Call) { c.Next() } }

func helper() {}
`,
	})
	caps := init.(map[string]any)["capabilities"].(map[string]any)
	if b, _ := json.Marshal(caps["completionProvider"]); !strings.Contains(string(b), `"@"`) {
		t.Fatalf("@ isn't a completion trigger: %s", b)
	}
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": main}})
	time.Sleep(time.Second)

	off := strings.Index(main, "\n@") + 2
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(main), off), "context": map[string]any{"triggerKind": 2, "triggerCharacter": "@"}})
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	found := map[string]map[string]any{}
	for _, it := range items.([]any) {
		item := it.(map[string]any)
		found[item["label"].(string)] = item
	}
	for _, want := range []string{"decorators.Logged", "decorators.Timed", "decorators.Retry"} {
		item := found[want]
		if item == nil {
			t.Fatalf("no %s among %d items", want, len(found))
		}
		got := applyEdits(main, append([]any{item["textEdit"]}, item["additionalTextEdits"].([]any)...))
		wantText := want
		if want == "decorators.Retry" {
			wantText = want + "($1)" // a factory is called
		}
		if !strings.Contains(got, "\n@"+wantText+"\n") || !strings.Contains(got, `"lsptest/decorators"`) {
			t.Fatalf("after picking %s:\n%s", want, got)
		}
	}
	if found["decorators.helper"] != nil {
		t.Fatal("offered an unexported function from another package")
	}
}

// completionText is what picking label from the completion after needle inserts.
func completionText(c *lspClient, uri, src, needle, label string) string {
	off := strings.Index(src, needle) + len(needle)
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), off)})
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	for _, it := range items.([]any) {
		item := it.(map[string]any)
		if item["label"] == label {
			if te, ok := item["textEdit"].(map[string]any); ok {
				return te["newText"].(string)
			}
			return item["label"].(string)
		}
	}
	return ""
}

// TestDropIn is vuka serving as the Go extension's gopls: a .go file in a
// package with .vuka files sees their code, and gopls's commands keep their
// names (the Go extension registers them itself).
func TestDropIn(t *testing.T) {
	c, dir, init := startLSPOpts(t, map[string]string{
		"main.vuka": "package main\n\nfunc helper() Result[int] { return Ok(1) }\n\nfunc main() { _ = useIt() }\n",
		"util.go":   "package main\n\nfunc useIt() int { return helper().Unwrap() }\n",
	}, lspOptions{})
	caps := init.(map[string]any)["capabilities"].(map[string]any)
	for _, cmd := range caps["executeCommandProvider"].(map[string]any)["commands"].([]any) {
		if !strings.HasPrefix(cmd.(string), "gopls.") {
			t.Fatalf("drop-in renamed %v; the Go extension expects gopls's names", cmd)
		}
	}
	util := filepath.Join(dir, "util.go")
	src, _ := os.ReadFile(util)
	uri := pathToURI(util)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "go", "version": 1, "text": string(src)}})
	var hover string
	for i := 0; i < 30 && !strings.Contains(hover, "func helper()"); i++ {
		time.Sleep(200 * time.Millisecond)
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf(src, strings.Index(string(src), "helper")+1)})
		b, _ := json.Marshal(v)
		hover = string(b)
	}
	if !strings.Contains(hover, "func helper() vuka.Result[int]") {
		t.Fatalf("hover on helper from a .go file: %s", hover)
	}
	// Loaded now; the diagnostics that follow are the real ones.
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": string(src) + "\n"}}})
	ds := c.waitDiags(uri, func([]any) bool { return true })
	if len(ds) != 0 {
		b, _ := json.Marshal(ds)
		t.Fatalf("util.go should see helper from main.vuka: %s", b)
	}
}

// TestGoplsPassthrough runs vuka through a link named gopls: subcommands
// other than serve reach the real gopls.
func TestGoplsPassthrough(t *testing.T) {
	if _, err := findGopls(""); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "vuka")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	link := filepath.Join(dir, "gopls")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(link, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "golang.org/x/tools/gopls") {
		t.Fatalf("gopls version through vuka: %v\n%s", err, out)
	}
}

func TestImportHint(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "data"), 0o755)
	msg := `could not import data (no required module provides package "data")`
	if got := importHint(msg, root, "hello"); !strings.HasSuffix(got, `it's in this module: import "hello/data"`) {
		t.Fatalf("got %q", got)
	}
	if got := importHint(`could not import nope (no required module provides package "nope")`, root, "hello"); strings.Contains(got, "in this module") {
		t.Fatalf("hinted a directory that doesn't exist: %q", got)
	}
}
