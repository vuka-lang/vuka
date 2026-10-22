package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// Script blocks of .vuka.html files go to a TypeScript language server
// (typescript-language-server: $VUKA_TSLS, PATH, or the module's or its
// frontend's node_modules/.bin), started on the first question about one.
// Each script is a virtual file beside its template, x.vuka.html.<n>.ts, its
// text the script's after a line referencing .vuka/vuka-component.d.ts: the
// types of its component, ctx and live. Hover, completion, signature help
// and definition inside a script are the server's, mapped back, and so are
// its diagnostics.

var tsMethods = map[string]bool{
	"textDocument/hover":         true,
	"textDocument/completion":    true,
	"textDocument/signatureHelp": true,
	"textDocument/definition":    true,
}

type tsServer struct {
	p    *proxy
	conn *rpcConn
	cmd  *exec.Cmd

	mu      sync.Mutex
	next    int
	pending map[string]chan rpcMsg
	docs    map[string]*tsDoc    // virtual path → its script
	typesAt map[string]time.Time // package dir → when its types were last written
}

// tsDoc is a script as the server has it.
type tsDoc struct {
	html    string
	first   int // the template file's line (0-based) the script's text starts on
	text    string
	version int
	diags   []any
}

const tsPrelude = 1 // the reference line before a script's text

func findTSLS(dirs ...string) string {
	if p := os.Getenv("VUKA_TSLS"); p != "" {
		return p
	}
	if p, err := exec.LookPath("typescript-language-server"); err == nil {
		return p
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if p := filepath.Join(d, "node_modules", ".bin", "typescript-language-server"); fileExists(p) {
			return p
		}
	}
	return ""
}

// typescript is the proxy's TypeScript server, started the first time;
// nil when there is none.
func (p *proxy) typescript() *tsServer {
	p.tsOnce.Do(func() {
		p.mu.Lock()
		root := p.modRoot
		if root == "" {
			root = p.root
		}
		p.mu.Unlock()
		bin := findTSLS(root, cachedFrontendRoot(root))
		if bin == "" {
			p.logf("no typescript-language-server: script blocks get no TypeScript help")
			return
		}
		cmd := exec.Command(bin, "--stdio")
		cmd.Dir = root
		in, err1 := cmd.StdinPipe()
		out, err2 := cmd.StdoutPipe()
		if err1 != nil || err2 != nil || cmd.Start() != nil {
			p.logf("typescript-language-server didn't start")
			return
		}
		ts := &tsServer{p: p, conn: newRPCConn(out, in), cmd: cmd, pending: map[string]chan rpcMsg{}, docs: map[string]*tsDoc{}, typesAt: map[string]time.Time{}}
		go ts.loop()
		if _, ok := ts.call("initialize", map[string]any{"processId": os.Getpid(), "rootUri": pathToURI(root),
			"capabilities": map[string]any{"textDocument": map[string]any{
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"completion":         map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
				"publishDiagnostics": map[string]any{},
			}}}); !ok {
			p.logf("typescript-language-server didn't initialize")
			cmd.Process.Kill()
			return
		}
		_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
		p.ts.Store(ts)
	})
	return p.ts.Load()
}

func (ts *tsServer) stop() {
	if ts != nil && ts.cmd.Process != nil {
		ts.cmd.Process.Kill()
		ts.cmd.Wait()
	}
}

func (ts *tsServer) loop() {
	for {
		body, err := ts.conn.read()
		if err != nil {
			return
		}
		var m rpcMsg
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.isResponse():
			ts.mu.Lock()
			ch := ts.pending[string(m.ID)]
			delete(ts.pending, string(m.ID))
			ts.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case m.isRequest():
			// workspace/configuration, window/workDoneProgress/create: none.
			var res any
			if m.Method == "workspace/configuration" {
				res = []any{}
			}
			_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": res})
		case m.Method == "textDocument/publishDiagnostics":
			ts.diagnostics(m.Params)
		}
	}
}

func (ts *tsServer) call(method string, params any) (rpcMsg, bool) {
	ts.mu.Lock()
	ts.next++
	id := strconv.Itoa(ts.next)
	ch := make(chan rpcMsg, 1)
	ts.pending[id] = ch
	ts.mu.Unlock()
	if ts.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}) != nil {
		return rpcMsg{}, false
	}
	select {
	case m := <-ch:
		return m, len(m.Error) == 0
	case <-time.After(30 * time.Second):
		ts.mu.Lock()
		delete(ts.pending, id)
		ts.mu.Unlock()
		return rpcMsg{}, false
	}
}

// types writes the types of the scripts of the package in dir, at most
// every few seconds, to .vuka/ts/<dir>/vuka-component.d.ts at the module
// root, and returns the file.
func (ts *tsServer) types(dir string) string {
	ts.p.mu.Lock()
	root, modPath := ts.p.modRoot, ts.p.modPath
	ts.p.mu.Unlock()
	if root == "" {
		root = ts.p.root
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = "_"
	}
	path := filepath.Join(root, ".vuka", "ts", rel, componentDTSName)
	ts.mu.Lock()
	stale := time.Since(ts.typesAt[dir]) > 3*time.Second
	if stale {
		ts.typesAt[dir] = time.Now()
	}
	ts.mu.Unlock()
	if !stale {
		return path
	}
	var comps []*assetComp
	files, _ := filepath.Glob(filepath.Join(dir, "*.vuka.html"))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, a := range transpile.TemplateAssets(f, src) {
			if a.Script && a.Decl != "" {
				comps = append(comps, &assetComp{dir: dir, name: a.Decl, scripts: []assetSrc{{inline: a.Text}}})
			}
		}
	}
	if dts := componentDTS(root, modPath, comps); dts != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		writeIfChanged(filepath.Join(root, ".vuka", ".gitignore"), []byte("*\n"))
		writeIfChanged(path, dts)
	}
	return path
}

// sync gives the server the scripts of the template file at path as they
// are, and returns them by virtual path.
func (ts *tsServer) sync(path string, src []byte) map[string]*tsDoc {
	dts := ts.types(filepath.Dir(path))
	assets := transpile.TemplateAssets(path, src)
	prelude := "/// <reference path=" + strconv.Quote(filepath.ToSlash(dts)) + " />\n"
	out := map[string]*tsDoc{}
	n := 0
	for _, a := range assets {
		if !a.Script {
			continue
		}
		virtual := path + "." + strconv.Itoa(n) + ".ts"
		n++
		text := prelude + a.Text
		ts.mu.Lock()
		d := ts.docs[virtual]
		switch {
		case d == nil:
			d = &tsDoc{html: path, first: a.Line - 1, text: text, version: 1}
			ts.docs[virtual] = d
			ts.mu.Unlock()
			_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
				"textDocument": map[string]any{"uri": pathToURI(virtual), "languageId": "typescript", "version": 1, "text": text}}})
		case d.text != text:
			d.text, d.first = text, a.Line-1
			d.version++
			v := d.version
			ts.mu.Unlock()
			_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
				"textDocument":   map[string]any{"uri": pathToURI(virtual), "version": v},
				"contentChanges": []any{map[string]any{"text": text}}}})
		default:
			d.first = a.Line - 1
			ts.mu.Unlock()
		}
		out[virtual] = d
	}
	ts.mu.Lock()
	for v, d := range ts.docs {
		if d.html == path && out[v] == nil {
			delete(ts.docs, v)
			defer ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{
				"textDocument": map[string]any{"uri": pathToURI(v)}}})
		}
	}
	ts.mu.Unlock()
	return out
}

// tsRequest answers a question about a script block of the template file
// at path with the TypeScript server's answer; false when pos is in none.
func (p *proxy) tsRequest(m *rpcMsg, path string) bool {
	if !tsMethods[m.Method] || !strings.HasSuffix(path, ".vuka.html") {
		return false
	}
	var params map[string]any
	if json.Unmarshal(m.Params, &params) != nil {
		return false
	}
	pos, ok := toPosition(params["position"])
	if !ok {
		return false
	}
	p.mu.Lock()
	src, open := p.bufs[path]
	p.mu.Unlock()
	if !open {
		src, _ = os.ReadFile(path)
	}
	in := false
	for _, a := range transpile.TemplateAssets(path, src) {
		lines := uint32(strings.Count(a.Text, "\n"))
		if a.Script && pos.Line >= uint32(a.Line-1) && pos.Line <= uint32(a.Line-1)+lines {
			in = true
		}
	}
	if !in {
		return false
	}
	go func() {
		reply := func(result any) {
			_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
		ts := p.typescript()
		if ts == nil {
			reply(nil)
			return
		}
		for virtual, d := range ts.sync(path, src) {
			lines := uint32(strings.Count(d.text, "\n")) - tsPrelude
			if pos.Line < uint32(d.first) || pos.Line > uint32(d.first)+lines {
				continue
			}
			params["textDocument"] = map[string]any{"uri": pathToURI(virtual)}
			params["position"] = lspPosition{Line: pos.Line - uint32(d.first) + tsPrelude, Character: pos.Character}
			res, ok := ts.call(m.Method, params)
			if !ok {
				reply(nil)
				return
			}
			var v any
			_ = json.Unmarshal(res.Result, &v)
			reply(ts.toTemplate(v, virtual, d, true))
			return
		}
		reply(nil)
	}()
	return true
}

// toTemplate maps the server's answer about virtual onto its template:
// its URI, and the ranges in it (inDoc: the object is about virtual).
func (ts *tsServer) toTemplate(v any, virtual string, d *tsDoc, inDoc bool) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = ts.toTemplate(x[i], virtual, d, inDoc)
		}
	case map[string]any:
		for _, k := range []string{"uri", "targetUri"} {
			if u, ok := x[k].(string); ok {
				inDoc = uriToPath(u) == virtual
				if inDoc {
					x[k] = pathToURI(ts.p.display(d.html))
				}
			}
		}
		if inDoc {
			if r, ok := toRange(x); ok && len(x) == 2 {
				return lspRange{ts.line(r.Start, d), ts.line(r.End, d)}
			}
		}
		for k, e := range x {
			x[k] = ts.toTemplate(e, virtual, d, inDoc)
		}
	}
	return v
}

func (ts *tsServer) line(p lspPosition, d *tsDoc) lspPosition {
	if p.Line < tsPrelude {
		p.Line = tsPrelude
	}
	p.Line = p.Line - tsPrelude + uint32(d.first)
	return p
}

// diagnostics are the server's for a script: its template's, published with
// the rest of them.
func (ts *tsServer) diagnostics(params json.RawMessage) {
	var dp struct {
		URI         string `json:"uri"`
		Diagnostics []any  `json:"diagnostics"`
	}
	if json.Unmarshal(params, &dp) != nil {
		return
	}
	virtual := uriToPath(dp.URI)
	ts.mu.Lock()
	d := ts.docs[virtual]
	if d == nil {
		ts.mu.Unlock()
		return
	}
	var out []any
	for _, x := range dp.Diagnostics {
		m, ok := x.(map[string]any)
		r, rok := toRange(m["range"])
		if !ok || !rok || r.Start.Line < tsPrelude {
			continue
		}
		m["range"] = lspRange{ts.line(r.Start, d), ts.line(r.End, d)}
		if m["source"] == nil {
			m["source"] = "ts"
		}
		delete(m, "relatedInformation")
		out = append(out, m)
	}
	d.diags = out
	html := d.html
	ts.mu.Unlock()
	ts.p.publish(html)
}

// diagsFor are the server's diagnostics for the scripts of the template
// file at path.
func (ts *tsServer) diagsFor(path string) []any {
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	var out []any
	for _, d := range ts.docs {
		if d.html == path {
			out = append(out, d.diags...)
		}
	}
	return out
}
