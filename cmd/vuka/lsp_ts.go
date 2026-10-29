package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Script blocks of .vuka.html files go to a TypeScript language server
// (typescript-language-server: -tsls, $VUKA_TSLS, PATH, or the module's or
// its frontend's node_modules/.bin), started on the first question about one.
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
	docs    map[string]*tsDoc      // virtual path → its script
	typesAt map[string]time.Time   // package dir → when its types were last written
	locs    map[string][]dtsLoc    // a types file → the Go methods its lines declare
	syncs   map[string]bool        // template path → its syncLatest worker runs; true: the text changed since it took it
	files   map[string]*sync.Mutex // template path → held while sync gives the server its scripts
}

// tsDoc is a script as the server has it.
type tsDoc struct {
	refs    []scriptRef
	html    string
	first   int // the template file's line (0-based) the script's text starts on
	text    string
	version int
	diags   []any
	ready   chan struct{} // closed by its first diagnostics: the server has its project
}

const tsPrelude = 1 // the reference line before a script's text

// tsOptions are vuka lsp's -tsls, -tsls-node and -tsserver: the
// TypeScript server an editor hands it (the VS Code extension its bundled
// one, run by VS Code's own Node, with VS Code's TypeScript).
type tsOptions struct {
	server   string // typescript-language-server: an executable, or a .js/.mjs/.cjs file run by node
	node     string // the Node running a JS server; "" for node on PATH
	tsserver string // tsserver.js, or a typescript lib directory: the TypeScript when the module has none
}

// withEnv is o with $VUKA_TSLS, $VUKA_TSLS_NODE and $VUKA_TSSERVER for what
// its flags leave unset.
func (o tsOptions) withEnv() tsOptions {
	for _, f := range []struct {
		v   *string
		env string
	}{{&o.server, "VUKA_TSLS"}, {&o.node, "VUKA_TSLS_NODE"}, {&o.tsserver, "VUKA_TSSERVER"}} {
		if *f.v == "" {
			*f.v = os.Getenv(f.env)
		}
	}
	return o
}

// isJS reports whether path is a script Node runs, not an executable.
func isJS(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

// tsCommand is the command running the server at bin: bin itself, or a
// JavaScript file run by node — an Electron binary (VS Code's) as Node,
// through ELECTRON_RUN_AS_NODE, which plain Node ignores.
func (o tsOptions) tsCommand(bin string) (*exec.Cmd, error) {
	if !isJS(bin) {
		return exec.Command(bin, "--stdio"), nil
	}
	node := o.node
	if node == "" {
		p, err := exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("%s needs Node: none on PATH, and no -tsls-node", bin)
		}
		node = p
	}
	cmd := exec.Command(node, bin, "--stdio")
	cmd.Env = append(os.Environ(), "ELECTRON_RUN_AS_NODE=1")
	return cmd, nil
}

// tsserverOptions are the initialization options choosing the server's
// TypeScript: a module's own — its root's node_modules, which the server
// finds itself, else its frontend's — and o.tsserver when it has none.
func (o tsOptions) tsserverOptions(root, frontend string) map[string]any {
	ts := map[string]any{}
	if !moduleHasTS(root) && frontend != "" && moduleHasTS(frontend) {
		ts["path"] = filepath.Join(frontend, "node_modules", "typescript", "lib")
	}
	if o.tsserver != "" {
		ts["fallbackPath"] = o.tsserver
	}
	if len(ts) == 0 {
		return nil
	}
	return map[string]any{"tsserver": ts}
}

func moduleHasTS(dir string) bool {
	return dir != "" && fileExists(filepath.Join(dir, "node_modules", "typescript", "lib", "tsserver.js"))
}

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
		if p := nodeBin(d, "typescript-language-server"); fileExists(p) {
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
		frontend := cachedFrontendRoot(root)
		o := p.tsOpts.withEnv()
		bin := o.server
		if bin == "" {
			bin = findTSLS(root, frontend)
		}
		if bin == "" {
			p.logf("no typescript-language-server: script blocks get no TypeScript help")
			return
		}
		cmd, err := o.tsCommand(bin)
		if err != nil {
			p.logf("typescript-language-server: %v", err)
			return
		}
		cmd.Dir = root
		in, err1 := cmd.StdinPipe()
		out, err2 := cmd.StdoutPipe()
		if err1 != nil || err2 != nil || cmd.Start() != nil {
			p.logf("typescript-language-server didn't start")
			return
		}
		ts := &tsServer{p: p, conn: newRPCConn(out, in), cmd: cmd, pending: map[string]chan rpcMsg{}, docs: map[string]*tsDoc{}, typesAt: map[string]time.Time{}, locs: map[string][]dtsLoc{}}
		go ts.loop()
		if _, ok := ts.call("initialize", map[string]any{"processId": os.Getpid(), "rootUri": pathToURI(root),
			"initializationOptions": o.tsserverOptions(root, frontend),
			"capabilities": map[string]any{"textDocument": map[string]any{
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"completion":         map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
				"publishDiagnostics": map[string]any{},
			}}}); !ok {
			p.logf("typescript-language-server didn't initialize")
			killTree(cmd)
			return
		}
		_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
		p.ts.Store(ts)
	})
	return p.ts.Load()
}

func (ts *tsServer) stop() {
	if ts != nil && ts.cmd.Process != nil {
		killTree(ts.cmd)
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
				comps = append(comps, &assetComp{dir: dir, name: a.Decl, scripts: []assetSrc{{inline: a.Text}}, markup: []string{f}})
			}
		}
	}
	dts, locs := componentDTSLocs(root, modPath, comps)
	ts.mu.Lock()
	ts.locs[path] = locs
	ts.mu.Unlock()
	if dts != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		writeIfChanged(filepath.Join(root, ".vuka", ".gitignore"), []byte("*\n"))
		writeIfChanged(path, dts)
	}
	return path
}

// syncLatest gives the server the scripts of the template file at path as
// the editor has it, in the background: one worker a file, which takes the
// editor's text when it gets to it, so edits made meanwhile coalesce and
// the server never gets an older text after a newer one.
func (ts *tsServer) syncLatest(path string) {
	ts.mu.Lock()
	if ts.syncs == nil {
		ts.syncs = map[string]bool{}
	}
	_, running := ts.syncs[path]
	ts.syncs[path] = true
	ts.mu.Unlock()
	if running {
		return // its worker takes this text too
	}
	go func() {
		for {
			ts.mu.Lock()
			if !ts.syncs[path] {
				delete(ts.syncs, path)
				ts.mu.Unlock()
				return
			}
			ts.syncs[path] = false
			ts.mu.Unlock()
			ts.p.mu.Lock()
			src, open := ts.p.bufs[path]
			ts.p.mu.Unlock()
			if open {
				ts.sync(path, src)
			}
		}
	}()
}

// sync gives the server the scripts of the template file at path as they
// are, and returns them by virtual path. A file's syncs run one at a time.
func (ts *tsServer) sync(path string, src []byte) map[string]*tsDoc {
	ts.mu.Lock()
	if ts.files == nil {
		ts.files = map[string]*sync.Mutex{}
	}
	file := ts.files[path]
	if file == nil {
		file = &sync.Mutex{}
		ts.files[path] = file
	}
	ts.mu.Unlock()
	file.Lock()
	defer file.Unlock()
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
		text := prelude + setupScript(a.Text, a.Decl)
		var names scriptNames
		if markup, ok := markupOf([]string{path}, a.Decl); ok {
			names.readMarkupNames(markup)
		}
		ts.mu.Lock()
		d := ts.docs[virtual]
		switch {
		case d == nil:
			d = &tsDoc{refs: names.refs, html: path, first: a.Line - 1, text: text, version: 1, ready: make(chan struct{})}
			ts.docs[virtual] = d
			ts.mu.Unlock()
			_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
				"textDocument": map[string]any{"uri": pathToURI(virtual), "languageId": "typescript", "version": 1, "text": text}}})
		case d.text != text:
			d.text, d.first, d.refs = text, a.Line-1, names.refs
			d.version++
			v := d.version
			ts.mu.Unlock()
			_ = ts.conn.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
				"textDocument":   map[string]any{"uri": pathToURI(virtual), "version": v},
				"contentChanges": []any{map[string]any{"text": text}}}})
		default:
			d.first, d.refs = a.Line-1, names.refs
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
			// A script just opened is answered once the server has loaded
			// its project, which it says with its first diagnostics: asked
			// before, it answers with nothing.
			select {
			case <-d.ready:
			case <-time.After(10 * time.Second):
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
			switch m.Method {
			case "textDocument/definition":
				v = ts.toGo(v)
			case "textDocument/completion":
				v = ts.withScriptItems(v, d, params["position"].(lspPosition))
			}
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
		if note := setupRefNote(d.text, d.refs, offsetOf([]byte(d.text), r.Start)); note != "" {
			if message, ok := m["message"].(string); ok {
				m["message"] = message + " " + note
			}
		}
		m["range"] = lspRange{ts.line(r.Start, d), ts.line(r.End, d)}
		if m["source"] == nil {
			m["source"] = "ts"
		}
		delete(m, "relatedInformation")
		out = append(out, m)
	}
	d.diags = out
	select {
	case <-d.ready:
	default:
		close(d.ready)
	}
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

// toGo maps the locations of a definition answer that are a live method's
// declaration in the types file onto the Go method: live.reorder(…) goes
// to func (p *Index) Reorder.
func (ts *tsServer) toGo(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = ts.toGo(x[i])
		}
	case map[string]any:
		key, rangeKey := "uri", "range"
		if _, ok := x["targetUri"]; ok {
			key, rangeKey = "targetUri", "targetRange"
		}
		u, _ := x[key].(string)
		r, ok := toRange(x[rangeKey])
		if u == "" || !ok {
			return v
		}
		ts.mu.Lock()
		locs := ts.locs[uriToPath(u)]
		ts.mu.Unlock()
		for _, l := range locs {
			if l.line != int(r.Start.Line) {
				continue
			}
			at := ts.goMethodRange(l)
			x[key] = pathToURI(ts.p.display(l.file))
			x[rangeKey] = at
			if _, ok := x["targetSelectionRange"]; ok {
				x["targetSelectionRange"] = at
			}
		}
	}
	return v
}

// goMethodRange is where the name of l's method is on its line.
func (ts *tsServer) goMethodRange(l dtsLoc) lspRange {
	line := uint32(l.at - 1)
	src, err := load.ReadSource(l.file)
	if err == nil {
		lines := strings.Split(string(src), "\n")
		if int(line) < len(lines) {
			if i := wordIn([]byte(lines[line]), l.name); i >= 0 {
				return lspRange{lspPosition{Line: line, Character: uint32(i)}, lspPosition{Line: line, Character: uint32(i + len(l.name))}}
			}
		}
	}
	return lspRange{lspPosition{Line: line}, lspPosition{Line: line}}
}

// scriptTypeArg is the type a script is of: component<Index>, page<Index>.
var scriptTypeArg = regexp.MustCompile(`\b(?:component|page)<\s*(\w+)\s*>`)

// scriptCallsAt are the places the scripts of a package call the method
// whose declaration's name is at off in vf — live.reorder(…) for
// func (p *Index) Reorder — as locations; nil elsewhere. p.mu is held.
func (p *proxy) scriptCallsAt(vf *vfile, off int) []any {
	src := vf.text()
	start := off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	ls := bytes0LineStart(src, start)
	m := methodDeclAt.FindSubmatch(src[ls:])
	if m == nil || ls+len(m[0])-len(m[2]) != start {
		return nil
	}
	typ, name := string(m[1]), string(m[2])
	call := regexp.MustCompile(`\blive\.` + regexp.QuoteMeta(strings.ToLower(name[:1])+name[1:]) + `\s*\(`)
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(vf.source), "*.vuka.html"))
	var out []any
	for _, f := range files {
		text, open := p.bufs[f]
		if !open {
			text, _ = os.ReadFile(f)
		}
		for _, a := range transpile.TemplateAssets(f, text) {
			t := a.Decl
			if m := scriptTypeArg.FindStringSubmatch(a.Text); m != nil {
				t = m[1]
			}
			if !a.Script || t != typ {
				continue
			}
			for _, c := range call.FindAllStringIndex(a.Text, -1) {
				at := c[0] + len("live.")
				pos := positionOf([]byte(a.Text), at)
				pos.Line += uint32(a.Line - 1)
				end := pos
				end.Character += uint32(len(name))
				shown := f
				if s, ok := p.shown[f]; ok {
					shown = s
				}
				out = append(out, map[string]any{"uri": pathToURI(shown), "range": lspRange{pos, end}})
			}
		}
	}
	return out
}

// methodDeclAt is a method declaration at the start of a line: its
// receiver's type and its name.
var methodDeclAt = regexp.MustCompile(`^func\s*\(\s*\w*\s*\*?\s*(\w+)(?:\[[^\]]*\])?\s*\)\s*([A-Z]\w*)`)

// importSpecTyped is an import's module specifier being typed: from '…, or
// import('….
var importSpecTyped = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(?\s*)['"]([^'"]*)$`)

// scriptStatementStart is a line holding nothing but a word being typed.
var scriptStatementStart = regexp.MustCompile(`^\s*\w*$`)

// withScriptItems adds vuka's own items to the server's completion in a
// script, at pos in its virtual file: in an import's specifier
// vuka:component and the packages the module's (or its frontend's)
// package.json depends on; at a statement's start a snippet listening for
// an event, removed when the instance leaves.
func (ts *tsServer) withScriptItems(v any, d *tsDoc, pos lspPosition) any {
	lines := strings.Split(d.text, "\n")
	if int(pos.Line) >= len(lines) || int(pos.Character) > len(lines[pos.Line]) {
		return v
	}
	before := lines[pos.Line][:pos.Character]
	var items []any
	if m := importSpecTyped.FindStringSubmatch(before); m != nil {
		typed := lspRange{lspPosition{pos.Line, pos.Character - uint32(len(m[1]))}, pos}
		for _, name := range ts.importNames() {
			items = append(items, map[string]any{"label": name, "kind": 9, "sortText": "0" + name,
				"textEdit": map[string]any{"range": typed, "newText": name}})
		}
	} else if scriptStatementStart.MatchString(before) {
		items = append(items, map[string]any{"label": "listen", "kind": 15, "insertTextFormat": 2,
			"detail":        "addEventListener, removed on destroy",
			"documentation": "An event listener the instance removes when it leaves the page: the hooks' destroy returns it.",
			"insertText":    "const ${1:onKey} = (e: ${2:KeyboardEvent}) => {\n\t$0\n}\n${3:window}.addEventListener('${4:keydown}', $1)\nreturn { destroy() { $3.removeEventListener('$4', $1) } }"})
	}
	if len(items) == 0 {
		return v
	}
	b, _ := json.Marshal(items)
	var extra []any
	json.Unmarshal(b, &extra)
	switch x := v.(type) {
	case map[string]any:
		list, _ := x["items"].([]any)
		x["items"] = mergeItems(list, extra)
		return x
	case []any:
		return mergeItems(x, extra)
	}
	return map[string]any{"isIncomplete": false, "items": extra}
}

// mergeItems is list with the extra items whose labels it lacks.
func mergeItems(list, extra []any) []any {
	have := map[string]bool{}
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			if l, ok := m["label"].(string); ok {
				have[l] = true
			}
		}
	}
	for _, it := range extra {
		if l, _ := it.(map[string]any)["label"].(string); !have[l] {
			list = append(list, it)
		}
	}
	return list
}

// importNames are the specifiers a shape's script imports from: vuka's
// module, and the dependencies of the module's and its frontend's
// package.json.
func (ts *tsServer) importNames() []string {
	ts.p.mu.Lock()
	root := ts.p.modRoot
	if root == "" {
		root = ts.p.root
	}
	ts.p.mu.Unlock()
	out := []string{"vuka:component"}
	for _, dir := range []string{root, cachedFrontendRoot(root)} {
		if dir == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			continue
		}
		var pkg struct{ Dependencies, DevDependencies map[string]string }
		if json.Unmarshal(b, &pkg) != nil {
			continue
		}
		for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
			for name := range deps {
				if !slices.Contains(out, name) && !strings.HasPrefix(name, "@types/") {
					out = append(out, name)
				}
			}
		}
	}
	slices.Sort(out[1:])
	return out
}
