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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"unicode/utf8"

	templparser "github.com/a-h/templ/parser/v2"

	"github.com/vuka-lang/vuka/internal/format"
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
	shared := fs.Bool("shared", true, "use gopls's shared daemon (-remote=auto), so every editor session shares one gopls")
	if err := fs.Parse(args); err != nil {
		return err
	}
	gopls, err := findGopls(*goplsFlag)
	if err != nil {
		return err
	}
	var goplsArgs []string
	if *shared {
		goplsArgs = append(goplsArgs, "-remote=auto")
	}
	return serveLSP(gopls, *logFile, lspOptions{goplsArgs: goplsArgs, renameCommands: true})
}

// asGopls runs vuka as a drop-in gopls, when invoked under that name (the
// VS Code Go extension's go.alternateTools.gopls points at a link to vuka):
// gopls's subcommands other than serve go straight to the real gopls; serving
// runs the proxy, so .go files see the Go generated from .vuka files too. The
// commands keep gopls's names, which the Go extension registers itself.
func asGopls(args []string) error {
	gopls, err := findGopls(os.Getenv("VUKA_GOPLS"))
	if err != nil {
		return err
	}
	if sub := goplsSubcommand(args); sub != "" && sub != "serve" {
		c := exec.Command(gopls, args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}
	goplsArgs := args
	if os.Getenv("VUKA_GOPLS_SHARED") != "0" && !hasFlag(args, "remote") {
		goplsArgs = append([]string{"-remote=auto"}, args...)
	}
	return serveLSP(gopls, os.Getenv("VUKA_LSP_LOG"), lspOptions{goplsArgs: goplsArgs, dropIn: true})
}

// goplsBoolFlags are gopls's flags that take no value; any other flag given
// without = takes the next argument (templ's language server runs gopls with
// -logfile <file> -rpc.trace -remote <addr>).
var goplsBoolFlags = map[string]bool{"rpc.trace": true, "v": true, "verbose": true, "vv": true, "veryverbose": true, "h": true, "help": true}

// goplsSubcommand is the subcommand in gopls's arguments, "" for none (serve).
func goplsSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if a == "--" {
			break
		}
		if name := strings.TrimLeft(a, "-"); !strings.Contains(name, "=") && !goplsBoolFlags[name] {
			i++
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func serveLSP(gopls, logFile string, opts lspOptions) error {
	logw := io.Discard
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		logw = f
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runLSP(ctx, os.Stdin, os.Stdout, logw, gopls, opts)
}

// lspOptions are how vuka lsp runs gopls and talks to the editor.
type lspOptions struct {
	goplsArgs      []string // before serve, e.g. -remote=auto
	renameCommands bool     // namespace gopls's commands (vuka.gopls.*) for a second client
	dropIn         bool     // serving as gopls: .templ files are templ's language server's
}

func findGopls(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	isSelf := func(p string) bool {
		r, err := filepath.EvalSymlinks(p)
		return err == nil && r == self
	}
	if p, err := exec.LookPath("gopls"); err == nil && !isSelf(p) {
		return p, nil
	}
	for _, dir := range append(filepath.SplitList(os.Getenv("PATH")), os.Getenv("GOBIN"), filepath.Join(goEnv("GOPATH"), "bin")) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, "gopls"); fileExists(p) && !isSelf(p) {
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

func runLSP(ctx context.Context, in io.Reader, out io.Writer, logw io.Writer, gopls string, opts lspOptions) error {
	child := exec.Command(gopls, append(append([]string{}, opts.goplsArgs...), "serve")...)
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
	p.renameCommands, p.dropIn = opts.renameCommands, opts.dropIn
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

// vfile is the generated Go of one .vuka or .templ file, open in gopls.
type vfile struct {
	source  string // the .vuka or .templ path
	from    []byte // the source text it was generated from
	cur     []byte // the editor's text, when it differs from from
	hunks   []hunk // where cur and from differ
	stale   bool   // cur was edited since from: the hunks are a diff
	gen     []byte
	m       *transpile.SourceMap   // a .vuka file's map
	templ   *templparser.SourceMap // a .templ file's map (templ's own)
	files   []transpile.FileRef    // a .vuka file's vuka.File literals, in from
	version int
	probed  int // the version holding the last probe (see probe)
}

// text is the source as the editor has it.
func (f *vfile) text() []byte {
	if f.cur != nil {
		return f.cur
	}
	return f.from
}

// hunk is a stretch where the editor's text and the text the Go was generated
// from differ: a placeholder put in mid-edit, or an edit made since the last
// version that transpiled.
type hunk struct{ cur, curEnd, from, fromEnd int }

// withText sets the editor's text cur, and the hunks between it and from:
// the given ones, or else a diff of the two, which leaves f stale.
func (f *vfile) withText(cur []byte, hunks []hunk) {
	f.cur, f.hunks, f.stale = nil, nil, false
	if cur == nil || bytes.Equal(cur, f.from) {
		return
	}
	f.cur, f.hunks = cur, hunks
	if hunks == nil {
		f.hunks, f.stale = diffHunks(f.from, cur), true
	}
}

// changedAt reports whether off in the editor's text is in a stretch edited
// since the Go was generated, where nothing in the Go matches it.
func (f *vfile) changedAt(off int) bool {
	if !f.stale {
		return false
	}
	for _, h := range f.hunks {
		if off >= h.cur && off < h.curEnd {
			return true
		}
	}
	return false
}

// goneAt reports whether off in the text the Go came from is inside a stretch
// edited since, so it has no place in the editor's text.
func (f *vfile) goneAt(off int) bool {
	if !f.stale {
		return false
	}
	for _, h := range f.hunks {
		if off > h.from && off < h.fromEnd {
			return true
		}
	}
	return false
}

// fromOff maps an offset in the editor's text to the text the Go came from.
func (f *vfile) fromOff(off int) int {
	d := 0
	for _, h := range f.hunks {
		if off <= h.cur {
			break
		}
		if off < h.curEnd {
			return h.from
		}
		d = h.fromEnd - h.curEnd
	}
	return off + d
}

// curOff maps an offset in the text the Go came from to the editor's text.
func (f *vfile) curOff(off int) int {
	d := 0
	for _, h := range f.hunks {
		if off <= h.from {
			break
		}
		if off < h.fromEnd {
			return h.cur
		}
		d = h.curEnd - h.fromEnd
	}
	return off + d
}

// isTempl reports whether f is a .templ file's Go.
func (f *vfile) isTempl() bool { return f.m == nil }

// toGen maps an editor position in the source file to the generated Go.
func (f *vfile) toGen(pos lspPosition) lspPosition {
	if f.m == nil {
		if f.templ != nil {
			line, col := byteCol(f.from, pos)
			if g, ok := f.templ.TargetPositionFromSource(line, col); ok {
				return atByteCol(f.gen, g.Line, g.Col)
			}
		}
		return pos
	}
	g, _ := f.m.ToGenerated(f.fromOff(offsetOf(f.text(), pos)))
	return positionOf(f.gen, g)
}

// toSource maps a range in the generated Go to the source file. In a .templ
// file's Go only templ's mapped expressions and symbols map. While f is
// stale, an exact range with an end in text since edited doesn't map; an
// inexact one (a diagnostic's) moves to the edit's start.
func (f *vfile) toSource(r lspRange, exact bool) (lspRange, bool) {
	if f.m == nil {
		return f.templSource(r)
	}
	s, e, ok := f.fromRange(r)
	if !ok || exact && (f.goneAt(s) || f.goneAt(e)) {
		return r, false
	}
	return lspRange{positionOf(f.text(), f.curOff(s)), positionOf(f.text(), f.curOff(e))}, true
}

// fromRange is a range in the generated Go as offsets in the text it came from.
func (f *vfile) fromRange(r lspRange) (s, e int, ok bool) {
	s, _, ok1 := f.m.ToSource(offsetOf(f.gen, r.Start))
	e, _, ok2 := f.m.ToSource(offsetOf(f.gen, r.End))
	return s, max(s, e), ok1 && ok2
}

// staleEdit reports whether an edit to f's generated Go touches text edited
// since the Go was generated: made against text that is gone, it must be dropped.
func (f *vfile) staleEdit(v any) bool {
	if !f.stale || f.m == nil {
		return false
	}
	obj, _ := v.(map[string]any)
	r, ok := toRange(obj["range"])
	if !ok {
		return false
	}
	s, e, ok := f.fromRange(r)
	if !ok {
		return false
	}
	for _, h := range f.hunks {
		if s < h.fromEnd && e > h.from {
			return true
		}
	}
	return false
}

// templSource maps a range through templ's source map, which counts columns
// in bytes where LSP counts UTF-16 units.
func (f *vfile) templSource(r lspRange) (lspRange, bool) {
	if f.templ == nil {
		return r, false
	}
	line, col := byteCol(f.gen, r.Start)
	start, ok := f.templ.SourcePositionFromTarget(line, col)
	if !ok {
		sym, ok := f.templ.SymbolSourceRangeFromTarget(line, col)
		if !ok {
			return r, false
		}
		return lspRange{atByteCol(f.from, sym.From.Line, sym.From.Col), atByteCol(f.from, sym.To.Line, sym.To.Col)}, true
	}
	// Expressions are copied verbatim, so a one-line range keeps its width.
	from := lineStart(f.from, start.Line) + int(start.Col)
	to := from
	if r.End.Line == r.Start.Line {
		if endLine, endCol := byteCol(f.gen, r.End); endLine == line && endCol > col {
			to += int(endCol - col)
		}
	}
	return lspRange{positionOf(f.from, from), positionOf(f.from, to)}, true
}

// byteCol is an LSP position's line and byte column.
func byteCol(buf []byte, pos lspPosition) (line, col uint32) {
	return pos.Line, uint32(offsetOf(buf, pos) - lineStart(buf, pos.Line))
}

// atByteCol is the LSP position of a line and byte column.
func atByteCol(buf []byte, line, col uint32) lspPosition {
	return positionOf(buf, lineStart(buf, line)+int(col))
}

func lineStart(buf []byte, line uint32) int { return offsetOf(buf, lspPosition{Line: line}) }

type vukaRequest struct {
	editorID  json.RawMessage
	method    string
	file      *vfile
	attr      bool      // completion after @, on an attribute or decorator
	typed     lspRange  // there: what was typed after the last dot
	full      lspRange  // and after the @
	qualifier string    // the package typed before the dot, if any
	closeTag  *lspRange // asked on a closing tag's name, sent to its opening tag's
	blocks    *lspRange // completion after a markup child's {: the word typed, for block snippets
}

type proxy struct {
	editor, gopls *rpcConn
	log           io.Writer
	tmp           string

	genMu sync.Mutex

	mu               sync.Mutex
	initID           string // the editor's initialize request
	renameCommands   bool
	dropIn           bool
	root             string
	nextID           int
	goReqs           map[string]bool         // editor requests on Go files
	vukaReqs         map[string]*vukaRequest // proxy request id → request on a .vuka file
	asks             map[string]chan rpcMsg  // proxy request id → the proxy's own question to gopls
	bufs             map[string][]byte       // .vuka path → editor buffer
	virtual          map[string]*vfile       // generated path → buffer in gopls
	owned            map[string]bool         // Go paths the editor has open itself; its text wins
	bySource         map[string]string       // .vuka path → generated path
	vukaDiags        map[string][]any        // .vuka path → transpiler errors
	goDiags          map[string][]any        // .vuka path → gopls diagnostics mapped from its Go
	shown            map[string]string       // real path → the path the editor uses for it
	decos            []projectDecorator      // the module's decorators, as of the last regenerate
	modRoot, modPath string                  // the module, as of the last regenerate
	started          bool
	dirty            bool
	timer            *time.Timer
	stopped          bool
}

func newProxy(editor, gopls *rpcConn, log io.Writer, tmp string) *proxy {
	return &proxy{
		editor: editor, gopls: gopls, log: log, tmp: tmp,
		goReqs: map[string]bool{}, vukaReqs: map[string]*vukaRequest{}, asks: map[string]chan rpcMsg{},
		bufs: map[string][]byte{}, virtual: map[string]*vfile{}, bySource: map[string]string{}, owned: map[string]bool{},
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
	"textDocument/hover":              true,
	"textDocument/completion":         true,
	"textDocument/signatureHelp":      true,
	"textDocument/definition":         true,
	"textDocument/declaration":        true,
	"textDocument/typeDefinition":     true,
	"textDocument/implementation":     true,
	"textDocument/references":         true,
	"textDocument/documentHighlight":  true,
	"textDocument/documentSymbol":     true,
	"textDocument/rename":             true,
	"textDocument/prepareRename":      true,
	"textDocument/codeAction":         true,
	"textDocument/inlayHint":          true,
	"textDocument/documentLink":       true,
	"textDocument/foldingRange":       true,
	"textDocument/linkedEditingRange": true,
}

func isVuka(path string) bool { return strings.HasSuffix(path, ".vuka") }

// quietMethods are requests whose failure is no answer, not an error to show.
var quietMethods = map[string]bool{
	"textDocument/hover": true, "textDocument/definition": true, "textDocument/declaration": true,
	"textDocument/typeDefinition": true, "textDocument/implementation": true, "textDocument/references": true,
	"textDocument/documentHighlight": true, "textDocument/signatureHelp": true, "textDocument/completion": true,
	"textDocument/inlayHint": true, "textDocument/codeAction": true, "textDocument/documentSymbol": true,
	"textDocument/documentLink": true, "textDocument/foldingRange": true,
}

var unresolvedImport = regexp.MustCompile(`no required module provides package "([^"]+)"`)

// importHint adds, to Go's message for an import nothing provides, the path
// that would work when the module has a directory of that name: a package of
// your own project is imported by the module path and the directory.
func importHint(msg, modRoot, modPath string) string {
	g := unresolvedImport.FindStringSubmatch(msg)
	if g == nil || modRoot == "" || strings.Contains(strings.SplitN(g[1], "/", 2)[0], ".") {
		return msg
	}
	if fi, err := os.Stat(filepath.Join(modRoot, filepath.FromSlash(g[1]))); err == nil && fi.IsDir() {
		return msg + `; it's in this module: import "` + modPath + "/" + g[1] + `"`
	}
	return msg
}

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
			if !p.renameCommands {
				break
			}
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
			if strings.HasSuffix(path, ".go") && (m.Method == "textDocument/didOpen" || m.Method == "textDocument/didClose") {
				p.own(path, m.Method == "textDocument/didOpen", body)
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
				if path := uriToPath(c.URI); (strings.HasSuffix(path, ".go") || isVuka(path) || strings.HasSuffix(path, ".templ")) && !p.isVirtual(path) {
					p.schedule()
					break
				}
			}
			continue
		}
		if isVuka(path) {
			switch {
			case m.Method == "textDocument/formatting":
				p.formatting(&m, p.real(path))
			case m.isRequest():
				p.vukaRequest(&m, p.real(path))
			}
			continue
		}
		if m.Method == "textDocument/completion" && jsxTriggered(m.Params) {
			_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"isIncomplete": false, "items": []any{}}})
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
		if p.renameCommands && bytes.Contains(body, []byte(`"gopls.`)) {
			m = p.namespaceCommands(m)
			body, _ = json.Marshal(&m)
		} else if m.isResponse() && p.isInit(m) {
			m = p.namespaceCommands(m) // still adds the @ trigger
			body, _ = json.Marshal(&m)
		}
		switch {
		case m.isResponse():
			id := string(m.ID)
			p.mu.Lock()
			if ch := p.asks[id]; ch != nil {
				delete(p.asks, id)
				p.mu.Unlock()
				ch <- m
				continue
			}
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
func (p *proxy) isInit(m rpcMsg) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(m.ID) == p.initID
}

func (p *proxy) namespaceCommands(m rpcMsg) rpcMsg {
	isInit := m.isResponse() && p.isInit(m)
	rename := p.renameCommands
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
				case k == "commands" && rename && (isInit || m.Method == "client/registerCapability"):
					x[k] = walk(val, true)
				case k == "command" && rename:
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
	if isInit && !p.dropIn {
		for _, ch := range []string{"@", "<", "/", " ", `"`} {
			addTrigger(out, ch)
		}
		if r, ok := out.(map[string]any); ok {
			if caps, ok := r["capabilities"].(map[string]any); ok {
				caps["documentFormattingProvider"] = true // .vuka files by vuka fmt, .go files by gopls
				caps["linkedEditingRangeProvider"] = true // .vuka tag names
				caps["foldingRangeProvider"] = true
				if caps["documentLinkProvider"] == nil {
					caps["documentLinkProvider"] = map[string]any{} // vuka.File literals
				}
			}
		}
	}
	if b, err := json.Marshal(out); err == nil {
		*field = b
	}
	return m
}

// formatting answers textDocument/formatting for a .vuka file with vuka fmt:
// one edit replacing the whole document, or none when it doesn't parse.
func (p *proxy) formatting(m *rpcMsg, path string) {
	p.mu.Lock()
	src, ok := p.bufs[path]
	p.mu.Unlock()
	if !ok {
		src, _ = os.ReadFile(path)
	}
	edits := []any{}
	if out, err := format.Source(src); err != nil {
		p.logf("format %s: %v", path, err)
	} else if !bytes.Equal(out, src) {
		edits = append(edits, map[string]any{
			"range":   lspRange{Start: lspPosition{}, End: positionOf(src, len(src))},
			"newText": string(out),
		})
	}
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": edits})
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

	// A workspace reached through a symlink (macOS's /tmp and /var are
	// /private/…): gopls, the go command and the generated files all use real
	// paths, so every message is translated, editor paths to real on the way
	// in, real to editor paths on the way out.
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		// URIs only: a real path contains the editor's as a substring
		// (/private/var/x holds /var/x), file:// URIs don't.
		from, to := []byte(strings.TrimSuffix(pathToURI(root), "/")+"/"), []byte(strings.TrimSuffix(pathToURI(real), "/")+"/")
		p.editor.rewriting(
			func(b []byte) []byte { return bytes.ReplaceAll(b, from, to) },
			func(b []byte) []byte { return bytes.ReplaceAll(b, to, from) })
	}
}

func (p *proxy) isVirtual(path string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vfileOf(path) != nil
}

// vfileOf is the proxy's generated file at path, nil when there is none or the
// editor has that path open itself. p.mu must be held.
func (p *proxy) vfileOf(path string) *vfile {
	if p.owned[path] {
		return nil
	}
	return p.virtual[path]
}

// own hands a Go path to the editor while the editor has it open. templ's
// language server, using vuka as its gopls, opens a .templ file's Go (x_templ.go)
// itself and maps positions by its own text: gopls gets the editor's buffer in
// place of the proxy's, and the proxy's again once the editor closes it.
func (p *proxy) own(path string, open bool, body []byte) {
	p.genMu.Lock()
	defer p.genMu.Unlock()
	p.mu.Lock()
	vf, was := p.virtual[path], p.owned[path]
	if open {
		p.owned[path] = true
	} else {
		delete(p.owned, path)
	}
	var gen string
	if vf != nil && !open {
		vf.version, gen = 1, string(vf.gen)
	}
	p.mu.Unlock()
	uri := pathToURI(path)
	if open && vf != nil && !was {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose",
			"params": map[string]any{"textDocument": map[string]any{"uri": uri}}})
	}
	_ = p.gopls.write(body)
	if !open && vf != nil {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": gen}}})
	}
}

func (p *proxy) mentionsVirtual(raw json.RawMessage) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for path := range p.virtual {
		if !p.owned[path] && bytes.Contains(raw, []byte(pathToURI(path))) {
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
	if r, err := filepath.EvalSymlinks(modRoot); err == nil {
		modRoot = r // the paths the editor's buffers are kept by
	}
	hunks := map[string][]hunk{}
	read := func(path string) ([]byte, error) {
		if b, ok := bufs[path]; ok {
			src, hs := completable(b)
			hunks[path] = hs
			return src, nil
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
		for _, t := range pkg.Templ {
			sources[filepath.Join(pkg.Dir, t.Name)] = t.Src
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
		vf := &vfile{source: g.Source, from: g.From, gen: g.Src, m: g.Map, templ: g.TemplMap, files: g.Files}
		if b, ok := bufs[g.Source]; ok && !vf.isTempl() {
			vf.withText(b, hunks[g.Source])
		}
		want[g.Target] = vf
	}
	failing := map[string]bool{} // directories of packages with errors
	for path := range diags {
		failing[filepath.Dir(path)] = true
	}

	type op struct {
		method string
		params any
	}
	var ops []op
	p.mu.Lock()
	for path, old := range p.virtual {
		if _, ok := want[path]; !ok && sources[old.source] != nil && failing[filepath.Dir(old.source)] {
			// Keep the last good version while the package has errors, mapped
			// onto the text as it is now.
			kept := *old
			now := bufs[old.source]
			if now == nil {
				now = sources[old.source]
			}
			if !kept.isTempl() {
				kept.withText(now, nil)
			}
			want[path] = &kept
		}
	}
	for path, vf := range want {
		old := p.virtual[path]
		uri := pathToURI(path)
		switch {
		case p.owned[path]:
			if old != nil {
				vf.version = old.version
			}
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
		if _, ok := want[path]; !ok && !p.owned[path] {
			ops = append(ops, op{"textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}})
		}
	}
	p.virtual = want
	p.decos = decos
	p.modRoot, p.modPath = modRoot, modPath
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
	if p.dropIn && strings.HasSuffix(path, ".templ") {
		return // templ's language server reports on its files
	}
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
		Version     int    `json:"version"`
		Diagnostics []any  `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &dp); err != nil {
		return
	}
	p.mu.Lock()
	vf := p.vfileOf(uriToPath(dp.URI))
	if vf == nil {
		p.mu.Unlock()
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": params})
		return
	}
	if vf.isTempl() || dp.Version != 0 && dp.Version == vf.probed {
		// templ's own language server reports on .templ files; a probe's
		// diagnostics are of code that was never there.
		p.mu.Unlock()
		return
	}
	lines := uint32(bytes.Count(vf.gen, []byte("\n")))
	var mapped []any
	for _, d := range dp.Diagnostics {
		obj, ok := d.(map[string]any)
		if !ok {
			continue
		}
		r, ok := toRange(obj["range"])
		if !ok || r.Start.Line > lines {
			continue
		}
		if r, ok = vf.toSource(r, false); !ok {
			continue
		}
		obj["range"] = r
		if msg, ok := obj["message"].(string); ok {
			if strings.Contains(msg, placeholder) {
				continue
			}
			obj["message"] = importHint(demangle(vf.m.Message(int(r.Start.Line)+1, msg)), p.modRoot, p.modPath)
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
	genPath := p.bySource[path]
	params["textDocument"] = map[string]any{"uri": pathToURI(genPath)}
	srcPos, hasPos := toPosition(params["position"])
	var closeTag *lspRange
	if hasPos {
		src := vf.text()
		off := offsetOf(src, srcPos)
		if h := p.fileRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			reply(h())
			return
		}
		if h := markupRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			reply(h())
			return
		}
		if m.Method != "textDocument/completion" && vf.changedAt(off) {
			p.mu.Unlock()
			reply(nil)
			return
		}
		if h := p.jsxRequest(m.Method, params, vf, genPath, off); h != nil {
			p.mu.Unlock()
			go func() { reply(h()) }()
			return
		}
		if to, r, ok := closeRedirect(src, off); ok {
			closeTag = &r
			params["position"] = vf.toGen(positionOf(src, to))
		} else if m.Method == "textDocument/completion" && off > 0 && isIdentByte(src[off-1]) && afterAt(src, srcPos) {
			// At the end of an attribute's name: the last letter maps into
			// the attribute's copy after the code; its end would map in place.
			gp := vf.toGen(positionOf(src, off-1))
			gp.Character++
			params["position"] = gp
		} else {
			params["position"] = vf.toGen(srcPos)
		}
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
	vr := &vukaRequest{editorID: m.ID, method: m.Method, file: vf, closeTag: closeTag,
		attr: m.Method == "textDocument/completion" && afterAt(vf.text(), srcPos)}
	if m.Method == "textDocument/completion" {
		if s, ok := blockSpot(vf.text(), offsetOf(vf.text(), srcPos)); ok {
			vr.blocks = &lspRange{positionOf(vf.text(), s), srcPos}
		}
	}
	if vr.attr {
		src := vf.text()
		off := offsetOf(src, srcPos)
		start := off
		for start > 0 && isIdentByte(src[start-1]) {
			start--
		}
		full := start
		for full > 0 && (isIdentByte(src[full-1]) || src[full-1] == '.') {
			full--
		}
		vr.typed = lspRange{positionOf(src, start), srcPos}
		vr.full = lspRange{positionOf(src, full), srcPos}
		if q := string(src[full:start]); q != "" {
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
		// gopls can't answer mid-edit or with a broken import; like gopls
		// under the Go extension, that's no answer rather than an error.
		switch {
		case vr.method == "textDocument/documentLink":
			reply["result"] = p.documentLinks(vr.file)
		case vr.method == "textDocument/foldingRange":
			reply["result"] = foldingRanges(vr.file, nil)
		case vr.blocks != nil:
			reply["result"] = map[string]any{"isIncomplete": false, "items": blockItems(*vr.blocks)}
		case quietMethods[vr.method]:
			p.logf("%s: %s", vr.method, m.Error)
			reply["result"] = nil
		default:
			reply["error"] = m.Error
		}
		_ = p.editor.send(reply)
		return
	}
	var v any
	_ = json.Unmarshal(m.Result, &v)
	if vr.method != "textDocument/foldingRange" {
		v = p.rewrite(v, vr.file, vr.file)
	}
	switch vr.method {
	case "textDocument/completion":
		v = cleanCompletion(v)
		if vr.blocks != nil {
			v = withBlockItems(v, *vr.blocks)
		}
		if vr.attr {
			v = attrCompletion(v, vr.typed)
			p.mu.Lock()
			decos := p.decos
			p.mu.Unlock()
			v = completeList(withItems(v, decoratorItems(vr.file, decos, vr.full, vr.qualifier)))
		}
	case "textDocument/hover":
		v = staticHover(demangleStrings(v))
		if h, ok := v.(map[string]any); ok && vr.closeTag != nil {
			h["range"] = *vr.closeTag
		}
	case "textDocument/prepareRename":
		if vr.closeTag != nil {
			if r, ok := v.(map[string]any); ok && r["range"] != nil {
				r["range"] = *vr.closeTag
			} else if _, ok := toRange(v); ok {
				v = *vr.closeTag
			}
		}
	case "textDocument/rename", "textDocument/references", "textDocument/documentHighlight":
		v = p.withCloseTwins(vr.method, v, vr.file)
	case "textDocument/signatureHelp", "textDocument/documentSymbol", "textDocument/inlayHint":
		v = demangleStrings(v)
	case "textDocument/documentLink":
		v = p.withFileLinks(v, vr.file)
	case "textDocument/foldingRange":
		v = foldingRanges(vr.file, v)
	}
	reply["result"] = v
	_ = p.editor.send(reply)
}

// ask sends gopls a request of the proxy's own and waits for the answer.
func (p *proxy) ask(method string, params any) (json.RawMessage, bool) {
	p.mu.Lock()
	p.nextID++
	id := strconv.Quote("vuka-ask:" + strconv.Itoa(p.nextID))
	ch := make(chan rpcMsg, 1)
	p.asks[id] = ch
	p.mu.Unlock()
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	select {
	case m := <-ch:
		if len(m.Error) > 0 {
			p.logf("%s: %s", method, m.Error)
			return nil, false
		}
		return m.Result, true
	case <-time.After(10 * time.Second):
		p.mu.Lock()
		delete(p.asks, id)
		p.mu.Unlock()
		return nil, false
	}
}

// jsxTriggered reports whether a completion was triggered by a character vuka
// added for markup.
func jsxTriggered(raw json.RawMessage) bool {
	var params struct {
		Context struct {
			TriggerCharacter string `json:"triggerCharacter"`
		} `json:"context"`
	}
	_ = json.Unmarshal(raw, &params)
	return jsxTriggers[params.Context.TriggerCharacter]
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
				ctx = p.vfileOf(uriToPath(uri))
				p.mu.Unlock()
				if ctx != nil {
					x[key] = pathToURI(p.display(ctx.source))
				}
			}
		}
		if td, ok := x["textDocument"].(map[string]any); ok {
			if uri, ok := td["uri"].(string); ok {
				p.mu.Lock()
				ctx = p.vfileOf(uriToPath(uri))
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
				f := p.vfileOf(uriToPath(uri))
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
	if !ok || f == nil || f.isTempl() {
		return p.rewrite(v, f, origin)
	}
	fresh := list[:0:0]
	for _, e := range list {
		if !f.staleEdit(e) {
			fresh = append(fresh, e)
		}
	}
	rest, src := splitImportEdits(f, fresh)
	mapped, _ := p.rewrite(rest, f, origin).([]any)
	return append(mapped, src...)
}

func mapRange(f *vfile, v any) (lspRange, bool) {
	r, ok := toRange(v)
	if !ok {
		return r, false
	}
	return f.toSource(r, true)
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

// attrSpot is an attribute's name being typed after the start of a line:
// before a parameter (after its ( or ,), after = in a composed decorator, or
// after another attribute.
var attrSpot = regexp.MustCompile(`(?:[(,=]\s*|@[A-Za-z0-9_.]+(?:\([^()]*\)|\{[^{}]*\})?\s+)@[A-Za-z0-9_.]*$`)

// unfinishedAttr is an @ line being typed: `@` or `@pkg.` with nothing after.
var unfinishedAttr = regexp.MustCompile(`(?m)^(\s*@(?:[A-Za-z_][A-Za-z0-9_]*\.)?)[ \t]*$`)

// unfinishedParamAttr is an @ being typed before a parameter, and
// unfinishedElem one ending a composed decorator's line.
var (
	unfinishedParamAttr = regexp.MustCompile(`([(,][ \t]*@(?:[A-Za-z_][A-Za-z0-9_]*\.)?)[ \t]+[A-Za-z_*\[.]`)
	unfinishedElem      = regexp.MustCompile(`(?m)^(decorator[ \t].*=[ \t]*(?:@\S+[ \t]+)*@(?:[A-Za-z_][A-Za-z0-9_]*\.)?)[ \t]*$`)
)

// placeholder completes an unfinished attribute so the file still transpiles
// while it is typed; it is never shown or written.
const placeholder = "__vuka_complete"

// completable makes a buffer being typed transpile, so the rest of the file
// keeps its completion, hover and diagnostics mid-edit: the placeholder goes
// after an unfinished @ and after a selector's dot with nothing after it yet,
// and a tag with no > yet is blanked out. The hunks say where placeholders went.
func completable(src []byte) ([]byte, []hunk) {
	type insert struct {
		at   int
		text string
	}
	var ins []insert
	for _, re := range []*regexp.Regexp{unfinishedAttr, unfinishedParamAttr, unfinishedElem} {
		for _, m := range re.FindAllSubmatchIndex(src, -1) {
			ins = append(ins, insert{m[3], placeholder + "()"})
		}
	}
	for _, at := range danglingDots(src) {
		ins = append(ins, insert{at, placeholder})
	}
	sort.Slice(ins, func(i, j int) bool { return ins[i].at < ins[j].at })
	blank := blankUnfinishedTags(src)
	hunks := []hunk{}
	var out []byte
	last, d := 0, 0
	for _, in := range ins {
		out = append(append(out, blank[last:in.at]...), in.text...)
		hunks = append(hunks, hunk{in.at, in.at, in.at + d, in.at + d + len(in.text)})
		d += len(in.text)
		last = in.at
	}
	return append(out, blank[last:]...), hunks
}

// afterAt reports whether pos is in an attribute's name: the line so far is
// @ and a (possibly qualified) name.
func afterAt(src []byte, pos lspPosition) bool {
	off := offsetOf(src, pos)
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return attrPrefix.Match(src[start:off]) || attrSpot.Match(src[start:off])
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
		decorator := strings.Contains(detail, "vuka.Call") || strings.Contains(detail, "vuka.Decorator") || strings.Contains(detail, "vuka.Type") ||
			strings.Contains(detail, "vuka.Decl") || strings.Contains(detail, "vuka.Bundle")
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
			// @logged names the decorator; only a factory is called: @retry(3).
			if decorator {
				te["newText"], item["insertTextFormat"] = attrInsert(label, detail)
				delete(item, "command")
			}
		}
		kept = append(kept, item)
	}
	if list, ok := v.(map[string]any); ok {
		list["items"] = kept
		return list
	}
	return kept
}

// completeList drops duplicate labels (the project scan and gopls can both
// offer a decorator) and marks the list complete, so the editor narrows it as
// more is typed rather than asking again: mid-name, @lo reads as a type to
// gopls, which would leave the decorators out.
func completeList(v any) any {
	list, ok := v.(map[string]any)
	if !ok {
		arr, _ := v.([]any)
		list = map[string]any{"items": arr}
	}
	items, _ := list["items"].([]any)
	seen := map[string]bool{}
	var kept []any
	// The project scan's items come last and win: they insert the decorator
	// as written.
	for i := len(items) - 1; i >= 0; i-- {
		item, _ := items[i].(map[string]any)
		label, _ := item["label"].(string)
		if seen[label] {
			continue
		}
		seen[label] = true
		kept = append(kept, item)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	list["items"], list["isIncomplete"] = kept, false
	return list
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
	if r, ok := v.(lspRange); ok {
		return r, true
	}
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

// attrInsert is what picking a decorator inserts after @: its name, or for a
// factory (returning vuka.Decorator) a call with the cursor in it. The second
// result is the LSP insertTextFormat: 1 plain, 2 snippet.
func attrInsert(name, detail string) (string, int) {
	if (strings.Contains(detail, "vuka.Decorator") || strings.Contains(detail, "vuka.Bundle")) && !optionalArgs(detail) {
		return name + "($1)", 2
	}
	return name, 1
}

// optionalArgs reports whether a function's detail, func(…) …, takes no
// arguments or only a variadic one: written bare, the decorator is called
// with none.
func optionalArgs(detail string) bool {
	rest, ok := strings.CutPrefix(detail, "func(")
	if !ok {
		return false
	}
	params, _, ok := strings.Cut(rest, ")")
	return ok && (params == "" || !strings.Contains(params, ",") && strings.Contains(params, "..."))
}

func isIdentByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}
