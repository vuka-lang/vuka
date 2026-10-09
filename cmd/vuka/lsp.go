package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"unicode/utf8"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// lsp runs `vuka lsp`: gopls behind a proxy that keeps each .vuka file's
// generated Go open in gopls as an unsaved buffer, sends requests on .vuka files
// to the matching place in that Go, and maps every answer back.
func lsp(args []string) error {
	fs := flag.NewFlagSet("lsp", flag.ContinueOnError)
	goplsFlag := fs.String("gopls", "", "gopls binary (default: gopls on PATH, then $GOBIN, then $GOPATH/bin)")
	logFile := fs.String("log", "", "append a log to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	gopls, err := findGopls(*goplsFlag)
	if err != nil {
		return err
	}
	logw := io.Discard
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		logw = f
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runLSP(ctx, os.Stdin, os.Stdout, logw, gopls)
}

func findGopls(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if p, err := exec.LookPath("gopls"); err == nil {
		return p, nil
	}
	for _, dir := range []string{os.Getenv("GOBIN"), filepath.Join(goEnv("GOPATH"), "bin")} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, "gopls"); fileExists(p) {
			return p, nil
		}
	}
	return "", errors.New("gopls not found; install it with: go install golang.org/x/tools/gopls@latest")
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runLSP(ctx context.Context, in io.Reader, out io.Writer, logw io.Writer, gopls string) error {
	child := exec.Command(gopls, "serve")
	child.Stderr = logw
	cin, err := child.StdinPipe()
	if err != nil {
		return err
	}
	cout, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	if err := child.Start(); err != nil {
		return fmt.Errorf("start gopls: %w", err)
	}
	tmp, err := os.MkdirTemp("", "vuka-lsp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	p := newProxy(newRPCConn(in, out), newRPCConn(cout, cin), logw, tmp)
	goplsDone, editorDone := make(chan struct{}), make(chan struct{})
	go func() { p.fromGopls(); close(goplsDone) }()
	go func() { p.fromEditor(); close(editorDone) }()
	select {
	case <-editorDone:
	case <-goplsDone:
	case <-ctx.Done():
	}
	p.stop()
	_ = cin.Close()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		<-exited
	}
	return nil
}

// vfile is the generated Go of one .vuka file, open in gopls.
type vfile struct {
	source  string // the .vuka path
	from    []byte // the .vuka text it was generated from
	gen     []byte
	m       *transpile.SourceMap
	version int
}

// toGen maps an editor position in the .vuka file to the generated Go.
func (f *vfile) toGen(pos lspPosition) lspPosition {
	g, _ := f.m.ToGenerated(offsetOf(f.from, pos))
	return positionOf(f.gen, g)
}

// toSource maps a range in the generated Go to the .vuka file.
func (f *vfile) toSource(r lspRange) (lspRange, bool) {
	s, _, ok1 := f.m.ToSource(offsetOf(f.gen, r.Start))
	e, _, ok2 := f.m.ToSource(offsetOf(f.gen, r.End))
	if !ok1 || !ok2 {
		return r, false
	}
	if e < s {
		e = s
	}
	return lspRange{positionOf(f.from, s), positionOf(f.from, e)}, true
}

type vukaRequest struct {
	editorID  json.RawMessage
	method    string
	file      *vfile
	attr      bool     // completion after @, on an attribute or decorator
	typed     lspRange // there: what was typed after the last dot
	full      lspRange // and after the @
	qualifier string   // the package typed before the dot, if any
}

type proxy struct {
	editor, gopls *rpcConn
	log           io.Writer
	tmp           string

	genMu sync.Mutex

	mu        sync.Mutex
	initID    string // the editor's initialize request
	root      string
	nextID    int
	goReqs    map[string]bool         // editor requests on Go files
	vukaReqs  map[string]*vukaRequest // proxy request id → request on a .vuka file
	bufs      map[string][]byte       // .vuka path → editor buffer
	virtual   map[string]*vfile       // generated path → buffer in gopls
	bySource  map[string]string       // .vuka path → generated path
	vukaDiags map[string][]any        // .vuka path → transpiler errors
	goDiags   map[string][]any        // .vuka path → gopls diagnostics mapped from its Go
	shown     map[string]string       // real path → the path the editor uses for it
	decos     []projectDecorator      // the module's decorators, as of the last regenerate
	started   bool
	dirty     bool
	timer     *time.Timer
	stopped   bool
}

func newProxy(editor, gopls *rpcConn, log io.Writer, tmp string) *proxy {
	return &proxy{
		editor: editor, gopls: gopls, log: log, tmp: tmp,
		goReqs: map[string]bool{}, vukaReqs: map[string]*vukaRequest{},
		bufs: map[string][]byte{}, virtual: map[string]*vfile{}, bySource: map[string]string{},
		vukaDiags: map[string][]any{}, goDiags: map[string][]any{}, shown: map[string]string{},
	}
}

func (p *proxy) logf(format string, args ...any) {
	fmt.Fprintf(p.log, "vuka lsp: "+format+"\n", args...)
}

func (p *proxy) stop() {
	p.mu.Lock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
	}
	p.mu.Unlock()
}

// vukaMethods are the .vuka requests answered by gopls on the generated Go.
var vukaMethods = map[string]bool{
	"textDocument/hover":             true,
	"textDocument/completion":        true,
	"textDocument/signatureHelp":     true,
	"textDocument/definition":        true,
	"textDocument/declaration":       true,
	"textDocument/typeDefinition":    true,
	"textDocument/implementation":    true,
	"textDocument/references":        true,
	"textDocument/documentHighlight": true,
	"textDocument/documentSymbol":    true,
	"textDocument/rename":            true,
	"textDocument/prepareRename":     true,
	"textDocument/codeAction":        true,
	"textDocument/inlayHint":         true,
}

func isVuka(path string) bool { return strings.HasSuffix(path, ".vuka") }

// real resolves an editor path through symlinks, as package discovery does
// (macOS's /var is /private/var), remembering the editor's spelling.
func (p *proxy) real(path string) string {
	r := path
	if e, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		r = filepath.Join(e, filepath.Base(path))
	}
	p.mu.Lock()
	p.shown[r] = path
	p.mu.Unlock()
	return r
}

// display is the path the editor knows a real path by.
func (p *proxy) display(real string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.shown[real]; ok {
		return s
	}
	return real
}

func (p *proxy) fromEditor() {
	for {
		body, err := p.editor.read()
		if err != nil {
			return
		}
		var m rpcMsg
		if err := json.Unmarshal(body, &m); err != nil {
			p.logf("bad message from editor: %v", err)
			continue
		}
		if m.isResponse() {
			_ = p.gopls.write(body)
			continue
		}
		var doc struct {
			TextDocument textDocumentID `json:"textDocument"`
		}
		_ = json.Unmarshal(m.Params, &doc)
		path := uriToPath(doc.TextDocument.URI)

		switch m.Method {
		case "initialize":
			p.initialize(m.Params)
			p.mu.Lock()
			p.initID = string(m.ID)
			p.mu.Unlock()
			// gopls sees the generated files at their real paths; give it the
			// workspace by the same paths, or it thinks they're outside it.
			m.Params = realRoots(m.Params)
			_ = p.gopls.send(&m)
			continue
		case "workspace/executeCommand":
			// Commands reach the editor under vuka.; gopls knows them without.
			var params map[string]any
			if json.Unmarshal(m.Params, &params) == nil {
				if cmd, ok := params["command"].(string); ok {
					params["command"] = strings.TrimPrefix(cmd, commandPrefix)
					m.Params, _ = json.Marshal(params)
				}
			}
			p.mu.Lock()
			p.goReqs[string(m.ID)] = true
			p.mu.Unlock()
			_ = p.gopls.send(&m)
			continue
		case "initialized":
			_ = p.gopls.write(body)
			p.mu.Lock()
			p.started = true
			p.mu.Unlock()
			p.regenerate()
			continue
		case "exit":
			_ = p.gopls.write(body)
			return
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose", "textDocument/didSave":
			if isVuka(path) {
				p.document(m.Method, m.Params, p.real(path))
				continue
			}
			if p.isVirtual(path) {
				continue // gopls keeps the proxy's version of a generated file
			}
			_ = p.gopls.write(body)
			if m.Method == "textDocument/didSave" && strings.HasSuffix(path, ".go") {
				p.schedule()
			}
			continue
		case "workspace/didChangeWatchedFiles":
			_ = p.gopls.write(body)
			var params struct {
				Changes []struct {
					URI string `json:"uri"`
				} `json:"changes"`
			}
			_ = json.Unmarshal(m.Params, &params)
			for _, c := range params.Changes {
				if path := uriToPath(c.URI); (strings.HasSuffix(path, ".go") || isVuka(path)) && !p.isVirtual(path) {
					p.schedule()
					break
				}
			}
			continue
		}
		if isVuka(path) {
			if m.isRequest() {
				p.vukaRequest(&m, p.real(path))
			}
			continue
		}
		if m.isRequest() {
			p.mu.Lock()
			p.goReqs[string(m.ID)] = true
			p.mu.Unlock()
		}
		_ = p.gopls.write(body)
	}
}

func (p *proxy) fromGopls() {
	for {
		body, err := p.gopls.read()
		if err != nil {
			return
		}
		var m rpcMsg
		if err := json.Unmarshal(body, &m); err != nil {
			p.logf("bad message from gopls: %v", err)
			continue
		}
		if bytes.Contains(body, []byte(`"gopls.`)) {
			m = p.renameCommands(m)
			body, _ = json.Marshal(&m)
		}
		switch {
		case m.isResponse():
			id := string(m.ID)
			p.mu.Lock()
			vr := p.vukaReqs[id]
			delete(p.vukaReqs, id)
			goReq := p.goReqs[id]
			delete(p.goReqs, id)
			p.mu.Unlock()
			if vr != nil {
				p.answer(vr, &m)
				continue
			}
			if goReq && len(m.Error) == 0 && p.mentionsVirtual(m.Result) {
				m.Result = p.rewriteJSON(m.Result, nil)
				_ = p.editor.send(&m)
				continue
			}
			_ = p.editor.write(body)
		case m.Method == "textDocument/publishDiagnostics":
			p.goplsDiagnostics(m.Params)
		default:
			// gopls's own requests, such as workspace/applyEdit, may name
			// generated files.
			if p.mentionsVirtual(m.Params) {
				m.Params = p.rewriteJSON(m.Params, nil)
				_ = p.editor.send(&m)
				continue
			}
			_ = p.editor.write(body)
		}
	}
}

// realRoots resolves symlinks in the workspace folders of initialize params.
func realRoots(raw json.RawMessage) json.RawMessage {
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return raw
	}
	fix := func(uri string) string {
		if path := uriToPath(uri); path != "" {
			if r, err := filepath.EvalSymlinks(path); err == nil {
				return pathToURI(r)
			}
		}
		return uri
	}
	if u, ok := params["rootUri"].(string); ok {
		params["rootUri"] = fix(u)
	}
	if rp, ok := params["rootPath"].(string); ok {
		if r, err := filepath.EvalSymlinks(rp); err == nil {
			params["rootPath"] = r
		}
	}
	if folders, ok := params["workspaceFolders"].([]any); ok {
		for _, f := range folders {
			if m, ok := f.(map[string]any); ok {
				if u, ok := m["uri"].(string); ok {
					m["uri"] = fix(u)
				}
			}
		}
	}
	out, err := json.Marshal(params)
	if err != nil {
		return raw
	}
	return out
}

// commandPrefix namespaces gopls's commands. The editor's Go extension has
// registered gopls.* already; a second registration of the same names makes
// VS Code refuse this server.
const commandPrefix = "vuka."

// renameCommands puts gopls's commands under commandPrefix in a message for
// the editor: the commands advertised at initialize or registered later, and
// every command a code action, code lens or similar refers to.
func (p *proxy) renameCommands(m rpcMsg) rpcMsg {
	p.mu.Lock()
	isInit := m.isResponse() && string(m.ID) == p.initID
	p.mu.Unlock()
	field := &m.Params
	if m.isResponse() {
		field = &m.Result
	}
	var v any
	if json.Unmarshal(*field, &v) != nil {
		return m
	}
	var walk func(v any, inCommands bool) any
	walk = func(v any, inCommands bool) any {
		switch x := v.(type) {
		case []any:
			for i := range x {
				x[i] = walk(x[i], inCommands)
			}
		case map[string]any:
			for k, val := range x {
				switch {
				case k == "arguments":
				case k == "commands" && (isInit || m.Method == "client/registerCapability"):
					x[k] = walk(val, true)
				case k == "command":
					if s, ok := val.(string); ok && strings.HasPrefix(s, "gopls.") {
						x[k] = commandPrefix + s
					} else {
						x[k] = walk(val, false)
					}
				default:
					x[k] = walk(val, inCommands)
				}
			}
		case string:
			if inCommands && strings.HasPrefix(x, "gopls.") {
				return commandPrefix + x
			}
		}
		return v
	}
	out := walk(v, false)
	if isInit {
		addTrigger(out, "@")
	}
	if b, err := json.Marshal(out); err == nil {
		*field = b
	}
	return m
}

// addTrigger makes ch a completion trigger character in initialize's result.
func addTrigger(result any, ch string) {
	r, _ := result.(map[string]any)
	caps, _ := r["capabilities"].(map[string]any)
	cp, _ := caps["completionProvider"].(map[string]any)
	if cp == nil {
		return
	}
	triggers, _ := cp["triggerCharacters"].([]any)
	cp["triggerCharacters"] = append(triggers, ch)
}

func (p *proxy) initialize(params json.RawMessage) {
	var ip struct {
		RootURI          string `json:"rootUri"`
		RootPath         string `json:"rootPath"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	_ = json.Unmarshal(params, &ip)
	root := uriToPath(ip.RootURI)
	if root == "" && len(ip.WorkspaceFolders) > 0 {
		root = uriToPath(ip.WorkspaceFolders[0].URI)
	}
	if root == "" {
		root = ip.RootPath
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	p.mu.Lock()
	p.root = root
	p.mu.Unlock()
	p.logf("root %s", root)
}

func (p *proxy) isVirtual(path string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.virtual[path]
	return ok
}

func (p *proxy) mentionsVirtual(raw json.RawMessage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for path := range p.virtual {
		if bytes.Contains(raw, []byte(pathToURI(path))) {
			return true
		}
	}
	return false
}

// schedule regenerates after a short quiet period.
func (p *proxy) schedule() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dirty = true
	if p.stopped || !p.started {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(150*time.Millisecond, p.regenerate)
}

// document tracks the editor's .vuka buffers.
func (p *proxy) document(method string, params json.RawMessage, path string) {
	switch method {
	case "textDocument/didOpen":
		var dp struct {
			TextDocument struct {
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(params, &dp)
		p.mu.Lock()
		p.bufs[path] = []byte(dp.TextDocument.Text)
		p.mu.Unlock()
	case "textDocument/didChange":
		var dp struct {
			ContentChanges []struct {
				Range *lspRange `json:"range"`
				Text  string    `json:"text"`
			} `json:"contentChanges"`
		}
		_ = json.Unmarshal(params, &dp)
		p.mu.Lock()
		buf := p.bufs[path]
		for _, c := range dp.ContentChanges {
			if c.Range == nil {
				buf = []byte(c.Text)
				continue
			}
			start, end := offsetOf(buf, c.Range.Start), offsetOf(buf, c.Range.End)
			if end < start {
				end = start
			}
			next := make([]byte, 0, len(buf)-(end-start)+len(c.Text))
			buf = append(append(append(next, buf[:start]...), c.Text...), buf[end:]...)
		}
		p.bufs[path] = buf
		p.mu.Unlock()
	case "textDocument/didClose":
		p.mu.Lock()
		delete(p.bufs, path)
		p.mu.Unlock()
	}
	p.schedule()
}

// regenerate transpiles the module's Vuka packages, with the editor's unsaved
// buffers, and syncs gopls and the diagnostics with the result. A package that
// fails keeps its previous generated files, so the rest keeps type-checking.
func (p *proxy) regenerate() {
	p.genMu.Lock()
	defer p.genMu.Unlock()
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.dirty = false
	root := p.root
	bufs := make(map[string][]byte, len(p.bufs))
	for k, v := range p.bufs {
		bufs[k] = v
	}
	p.mu.Unlock()

	modRoot, modPath, err := load.ModuleRoot(root)
	if err != nil {
		p.logf("%v", err)
		return
	}
	read := func(path string) ([]byte, error) {
		if b, ok := bufs[path]; ok {
			return completable(b), nil
		}
		return os.ReadFile(path)
	}
	pkgs, err := load.Discover(modRoot, modPath, modRoot, true, read)
	if err != nil {
		p.logf("discover: %v", err)
		return
	}
	gens, _, terr := load.Transpile(pkgs, p.tmp, load.Options{Bare: true})
	decos := findDecorators(modRoot, modPath, read)
	var list transpile.ErrorList
	if terr != nil && !errors.As(terr, &list) {
		p.logf("transpile: %v", terr)
	}
	sources := map[string][]byte{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			if f.IsVuka() {
				sources[filepath.Join(pkg.Dir, f.Name)] = f.Src
			}
		}
	}
	diags := map[string][]any{}
	for _, e := range list {
		src, ok := sources[e.Pos.Filename]
		if !ok {
			continue
		}
		pos := positionOf(src, e.Pos.Offset)
		diags[e.Pos.Filename] = append(diags[e.Pos.Filename], lspDiagnostic{
			Range: lspRange{pos, pos}, Severity: 1, Source: "vuka", Message: e.Msg})
	}

	want := map[string]*vfile{}
	for _, g := range gens {
		want[g.Target] = &vfile{source: g.Source, from: g.From, gen: g.Src, m: g.Map}
	}

	type op struct {
		method string
		params any
	}
	var ops []op
	p.mu.Lock()
	for path, old := range p.virtual {
		if _, ok := want[path]; !ok && sources[old.source] != nil && len(diags[old.source]) > 0 {
			want[path] = old // keep the last good version while the source has errors
		}
	}
	for path, vf := range want {
		old := p.virtual[path]
		uri := pathToURI(path)
		switch {
		case old == nil:
			vf.version = 1
			ops = append(ops, op{"textDocument/didOpen", map[string]any{"textDocument": map[string]any{
				"uri": uri, "languageId": "go", "version": vf.version, "text": string(vf.gen)}}})
		case !bytes.Equal(old.gen, vf.gen):
			vf.version = old.version + 1
			ops = append(ops, op{"textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": uri, "version": vf.version},
				"contentChanges": []any{map[string]any{"text": string(vf.gen)}}}})
		default:
			vf.version = old.version
		}
	}
	for path := range p.virtual {
		if _, ok := want[path]; !ok {
			ops = append(ops, op{"textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}})
		}
	}
	p.virtual = want
	p.decos = decos
	p.bySource = map[string]string{}
	for path, vf := range want {
		p.bySource[vf.source] = path
	}
	touched := map[string]bool{}
	for path := range p.vukaDiags {
		touched[path] = true
	}
	for path := range diags {
		touched[path] = true
	}
	p.vukaDiags = diags
	p.mu.Unlock()

	for _, o := range ops {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": o.method, "params": o.params})
	}
	for path := range touched {
		p.publish(path)
	}
}

// publish sends a .vuka file's diagnostics: the transpiler's, or when it has
// none, gopls's on the generated Go.
func (p *proxy) publish(path string) {
	p.mu.Lock()
	ds := append([]any{}, p.vukaDiags[path]...)
	if len(ds) == 0 {
		ds = append(ds, p.goDiags[path]...)
	}
	p.mu.Unlock()
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
		"params": map[string]any{"uri": pathToURI(p.display(path)), "diagnostics": ds}})
}

// goplsDiagnostics routes gopls's diagnostics: a generated file's onto its
// .vuka file, any other file's straight to the editor.
func (p *proxy) goplsDiagnostics(params json.RawMessage) {
	var dp struct {
		URI         string `json:"uri"`
		Diagnostics []any  `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &dp); err != nil {
		return
	}
	p.mu.Lock()
	vf := p.virtual[uriToPath(dp.URI)]
	if vf == nil {
		p.mu.Unlock()
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": params})
		return
	}
	var mapped []any
	for _, d := range dp.Diagnostics {
		obj, ok := d.(map[string]any)
		if !ok {
			continue
		}
		r, ok := toRange(obj["range"])
		if !ok {
			continue
		}
		if r, ok = vf.toSource(r); !ok {
			continue
		}
		obj["range"] = r
		if msg, ok := obj["message"].(string); ok {
			if strings.Contains(msg, placeholder) {
				continue
			}
			obj["message"] = demangle(msg)
		}
		delete(obj, "relatedInformation")
		delete(obj, "data")
		mapped = append(mapped, obj)
	}
	p.goDiags[vf.source] = mapped
	p.mu.Unlock()
	p.publish(vf.source)
}

// vukaRequest asks gopls the editor's question at the matching place in the
// generated Go.
func (p *proxy) vukaRequest(m *rpcMsg, path string) {
	reply := func(result any) {
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
	if !vukaMethods[m.Method] {
		reply(nil)
		return
	}
	p.mu.Lock()
	dirty := p.dirty
	p.mu.Unlock()
	if dirty {
		p.regenerate()
	}
	p.mu.Lock()
	vf := p.virtual[p.bySource[path]]
	if vf == nil {
		p.mu.Unlock()
		reply(nil)
		return
	}
	var params map[string]any
	_ = json.Unmarshal(m.Params, &params)
	params["textDocument"] = map[string]any{"uri": pathToURI(p.bySource[path])}
	srcPos, _ := toPosition(params["position"])
	if pos, ok := toPosition(params["position"]); ok {
		params["position"] = vf.toGen(pos)
	}
	if r, ok := toRange(params["range"]); ok {
		start, end := vf.toGen(r.Start), vf.toGen(r.End)
		// An attribute is in the generated Go twice (a comment in place, a copy
		// after the code), so a range's ends can map apart; never send gopls
		// one that ends before it starts.
		if offsetOf(vf.gen, end) < offsetOf(vf.gen, start) {
			start = end
		}
		params["range"] = lspRange{start, end}
	}
	p.nextID++
	id := strconv.Quote("vuka-lsp:" + strconv.Itoa(p.nextID))
	vr := &vukaRequest{editorID: m.ID, method: m.Method, file: vf,
		attr: m.Method == "textDocument/completion" && afterAt(vf.from, srcPos)}
	if vr.attr {
		off := offsetOf(vf.from, srcPos)
		start := off
		for start > 0 && isIdentByte(vf.from[start-1]) {
			start--
		}
		full := start
		for full > 0 && (isIdentByte(vf.from[full-1]) || vf.from[full-1] == '.') {
			full--
		}
		vr.typed = lspRange{positionOf(vf.from, start), srcPos}
		vr.full = lspRange{positionOf(vf.from, full), srcPos}
		if q := string(vf.from[full:start]); q != "" {
			vr.qualifier = strings.TrimSuffix(q, ".")
		}
		// Typing @ triggers completion; gopls only knows its own triggers.
		params["context"] = map[string]any{"triggerKind": 1}
	}
	p.vukaReqs[id] = vr
	p.mu.Unlock()
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": m.Method, "params": params})
}

// answer hands gopls's answer to a .vuka request back, mapped onto the source.
func (p *proxy) answer(vr *vukaRequest, m *rpcMsg) {
	reply := map[string]any{"jsonrpc": "2.0", "id": vr.editorID}
	if len(m.Error) > 0 {
		reply["error"] = m.Error
		_ = p.editor.send(reply)
		return
	}
	var v any
	_ = json.Unmarshal(m.Result, &v)
	v = p.rewrite(v, vr.file, vr.file)
	switch vr.method {
	case "textDocument/completion":
		v = cleanCompletion(v)
		if vr.attr {
			v = attrCompletion(v, vr.typed)
			p.mu.Lock()
			decos := p.decos
			p.mu.Unlock()
			v = withItems(v, decoratorItems(vr.file, decos, vr.full, vr.qualifier))
		}
	case "textDocument/hover":
		v = staticHover(demangleStrings(v))
	case "textDocument/signatureHelp", "textDocument/documentSymbol", "textDocument/inlayHint":
		v = demangleStrings(v)
	}
	reply["result"] = v
	_ = p.editor.send(reply)
}

func (p *proxy) rewriteJSON(raw json.RawMessage, ctx *vfile) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	out, err := json.Marshal(p.rewrite(v, ctx, nil))
	if err != nil {
		return raw
	}
	return out
}

// rangeKeys hold ranges in the document of the enclosing object's uri, or the
// request's document when there is none.
var rangeKeys = map[string]bool{
	"range": true, "selectionRange": true, "targetRange": true, "targetSelectionRange": true,
	"insert": true, "replace": true, "fullRange": true,
}

// rewrite maps every location in an LSP value that points into generated Go
// onto the .vuka source. ctx is the file ranges without a uri belong to (nil:
// a real Go file); origin is the request's document. A location that can't be
// mapped is dropped.
func (p *proxy) rewrite(v any, ctx, origin *vfile) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if r := p.rewrite(e, ctx, origin); r != nil {
				out = append(out, r)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"uri", "targetUri"} {
			if uri, ok := x[key].(string); ok {
				p.mu.Lock()
				ctx = p.virtual[uriToPath(uri)]
				p.mu.Unlock()
				if ctx != nil {
					x[key] = pathToURI(p.display(ctx.source))
				}
			}
		}
		if td, ok := x["textDocument"].(map[string]any); ok {
			if uri, ok := td["uri"].(string); ok {
				p.mu.Lock()
				ctx = p.virtual[uriToPath(uri)]
				p.mu.Unlock()
				if ctx != nil {
					td["uri"], td["version"] = pathToURI(p.display(ctx.source)), nil
				}
			}
		}
		if changes, ok := x["changes"].(map[string]any); ok {
			out := map[string]any{}
			for uri, edits := range changes {
				p.mu.Lock()
				f := p.virtual[uriToPath(uri)]
				p.mu.Unlock()
				if f != nil {
					uri = pathToURI(p.display(f.source))
				}
				out[uri] = p.rewriteEdits(edits, f, origin)
			}
			x["changes"] = out
		}
		for _, key := range []string{"edits", "additionalTextEdits"} {
			if list, ok := x[key].([]any); ok && ctx != nil {
				x[key] = p.rewriteEdits(list, ctx, origin)
			}
		}
		for key, val := range x {
			switch {
			case key == "changes" || key == "textDocument" || key == "arguments" ||
				(key == "edits" || key == "additionalTextEdits") && ctx != nil:
			case key == "originSelectionRange":
				if origin != nil {
					if r, ok := mapRange(origin, val); ok {
						x[key] = r
					} else {
						delete(x, key)
					}
				}
			case rangeKeys[key]:
				if ctx == nil {
					continue
				}
				r, ok := mapRange(ctx, val)
				if !ok {
					if _, isLocation := x["uri"]; isLocation || x["targetUri"] != nil {
						return nil
					}
					delete(x, key)
					continue
				}
				x[key] = r
			default:
				if _, nested := val.(map[string]any); nested {
					x[key] = p.rewrite(val, ctx, origin)
				} else if _, list := val.([]any); list {
					x[key] = p.rewrite(val, ctx, origin)
				}
			}
		}
		return x
	}
	return v
}

// rewriteEdits maps a list of text edits on f's generated Go to its source;
// edits to the imports are redone on the source's own imports.
func (p *proxy) rewriteEdits(v any, f, origin *vfile) any {
	list, ok := v.([]any)
	if !ok || f == nil {
		return p.rewrite(v, f, origin)
	}
	rest, src := splitImportEdits(f, list)
	mapped, _ := p.rewrite(rest, f, origin).([]any)
	return append(mapped, src...)
}

func mapRange(f *vfile, v any) (lspRange, bool) {
	r, ok := toRange(v)
	if !ok {
		return r, false
	}
	return f.toSource(r)
}

// mangled matches an overload's generated name (area__Circle) and Vuka's
// temporaries (__e1), which the editor should never show.
var mangled = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_]*?)__[A-Za-z0-9_]+\b`)

func demangle(s string) string { return mangled.ReplaceAllString(s, "$1") }

var (
	// A generic static's accessor: func Model_Objects() *vuka.Static[Manager[User]] …
	staticAccessor = regexp.MustCompile(`(?m)func _?([A-Z][A-Za-z0-9]*)_([A-Za-z]\w*)\(\) \*\w+\.Static\[(.*?)\](?: //.*)?$`)
	// A static field, constant or method: var User_Table string, func User_New(…
	staticDecl = regexp.MustCompile(`\b(var|const|func) _?([A-Z][A-Za-z0-9]*)_([A-Za-z]\w*)`)
)

// staticHover shows statics the way the source writes them: static
// User.Table string, func User.New(…).
func staticHover(v any) any {
	h, ok := v.(map[string]any)
	if !ok {
		return v
	}
	c, ok := h["contents"].(map[string]any)
	if !ok {
		return v
	}
	text, ok := c["value"].(string)
	if !ok {
		return v
	}
	text = staticAccessor.ReplaceAllString(text, "static $2 $3 // declared by $1")
	text = staticDecl.ReplaceAllStringFunc(text, func(m string) string {
		g := staticDecl.FindStringSubmatch(m)
		if g[1] == "func" {
			return "func " + g[2] + "." + g[3]
		}
		return "static " + g[2] + "." + g[3]
	})
	c["value"] = text
	return v
}

func demangleStrings(v any) any {
	switch x := v.(type) {
	case string:
		return demangle(x)
	case []any:
		for i := range x {
			x[i] = demangleStrings(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = demangleStrings(x[k])
		}
	}
	return v
}

// cleanCompletion drops Vuka's temporaries and shows each overload under the
// name the source uses (its detail still tells them apart).
func cleanCompletion(v any) any {
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	arr, ok := items.([]any)
	if !ok {
		return v
	}
	kept := arr[:0]
	for _, it := range arr {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if label, _ := item["label"].(string); strings.HasPrefix(label, "__") {
			continue
		}
		kept = append(kept, demangleStrings(item))
	}
	if list, ok := v.(map[string]any); ok {
		list["items"] = kept
		return list
	}
	return kept
}

var attrPrefix = regexp.MustCompile(`^\s*@[A-Za-z0-9_.]*$`)

// unfinishedAttr is an @ line being typed: `@` or `@pkg.` with nothing after.
var unfinishedAttr = regexp.MustCompile(`(?m)^(\s*@(?:[A-Za-z_][A-Za-z0-9_]*\.)?)[ \t]*$`)

// placeholder completes an unfinished attribute so the file still transpiles
// while it is typed; it is never shown or written.
const placeholder = "__vuka_complete"

// completable puts the placeholder after an unfinished @, so the rest of the
// file keeps its completion, hover and diagnostics mid-edit. It only appends
// to those lines, so every position the editor sends stays valid.
func completable(src []byte) []byte {
	return unfinishedAttr.ReplaceAll(src, []byte("${1}"+placeholder+"()"))
}

// afterAt reports whether pos is in an attribute's name: the line so far is
// @ and a (possibly qualified) name.
func afterAt(src []byte, pos lspPosition) bool {
	off := offsetOf(src, pos)
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return attrPrefix.Match(src[start:off])
}

// attrCompletion orders completion after @: decorators (func(*vuka.Call)
// and factories returning one) first, then the rest; suggestions that would
// import an unrelated package are dropped.
func attrCompletion(v any, typed lspRange) any {
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	arr, ok := items.([]any)
	if !ok {
		return v
	}
	kept := arr[:0]
	for _, it := range arr {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if edits, _ := item["additionalTextEdits"].([]any); len(edits) > 0 {
			continue
		}
		label, _ := item["label"].(string)
		detail, _ := item["detail"].(string)
		decorator := strings.Contains(detail, "vuka.Call") || strings.Contains(detail, "vuka.Decorator") || strings.Contains(detail, "vuka.Type")
		// Only what an attribute can name: a function, a type, a package, or a
		// variable holding a decorator.
		switch kind, _ := item["kind"].(float64); kind {
		case 3, 7, 8, 9, 22: // function, class, interface, module, struct
		default:
			if !decorator {
				continue
			}
		}
		rank := "1"
		if decorator {
			rank = "0"
		}
		item["sortText"] = rank + label
		// Replace just what was typed: the generated text holds a placeholder
		// the editor doesn't have.
		if te, ok := item["textEdit"].(map[string]any); ok {
			delete(te, "insert")
			delete(te, "replace")
			te["range"] = typed
		}
		kept = append(kept, item)
	}
	if list, ok := v.(map[string]any); ok {
		list["items"] = kept
		return list
	}
	return kept
}

// withItems adds items to a completion result.
func withItems(v any, items []any) any {
	if len(items) == 0 {
		return v
	}
	if list, ok := v.(map[string]any); ok {
		existing, _ := list["items"].([]any)
		list["items"] = append(existing, items...)
		return list
	}
	existing, _ := v.([]any)
	return append(existing, items...)
}

func toRange(v any) (lspRange, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return lspRange{}, false
	}
	s, ok1 := toPosition(m["start"])
	e, ok2 := toPosition(m["end"])
	return lspRange{s, e}, ok1 && ok2
}

func toPosition(v any) (lspPosition, bool) {
	switch m := v.(type) {
	case map[string]any:
		l, ok1 := m["line"].(float64)
		c, ok2 := m["character"].(float64)
		return lspPosition{uint32(l), uint32(c)}, ok1 && ok2
	case lspPosition:
		return m, true
	}
	return lspPosition{}, false
}

// offsetOf converts an LSP position (UTF-16 columns) to a byte offset.
func offsetOf(buf []byte, pos lspPosition) int {
	off := 0
	for line := uint32(0); line < pos.Line; line++ {
		i := bytes.IndexByte(buf[off:], '\n')
		if i < 0 {
			return len(buf)
		}
		off += i + 1
	}
	units := uint32(0)
	for off < len(buf) && units < pos.Character && buf[off] != '\n' {
		r, size := utf8.DecodeRune(buf[off:])
		units += uint32(runeLen16(r))
		off += size
	}
	return off
}

// positionOf converts a byte offset to an LSP position.
func positionOf(buf []byte, off int) lspPosition {
	off = min(max(off, 0), len(buf))
	line := bytes.Count(buf[:off], []byte("\n"))
	start := bytes.LastIndexByte(buf[:off], '\n') + 1
	units := 0
	for _, r := range string(buf[start:off]) {
		units += runeLen16(r)
	}
	return lspPosition{Line: uint32(line), Character: uint32(units)}
}

// runeLen16 is the number of UTF-16 code units r takes.
func runeLen16(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func isIdentByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}
