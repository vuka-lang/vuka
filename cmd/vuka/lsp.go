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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"unicode/utf8"

	templparser "github.com/a-h/templ/parser/v2"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// lsp runs `vuka lsp`: gopls behind a proxy that keeps each .vuka file's
// generated Go open in gopls as an unsaved buffer, sends requests on .vuka files
// to the matching place in that Go, and maps every answer back.
func lsp(args []string) error {
	fs := flag.NewFlagSet("lsp", flag.ContinueOnError)
	goplsFlag := fs.String("gopls", "", "gopls binary (default: gopls on PATH, then $GOBIN, then $GOPATH/bin)")
	logFile := fs.String("log", "", "append a log to this file")
	shared := fs.Bool("shared", true, "use gopls's shared daemon (-remote=auto), so every editor session shares one gopls")
	var ts tsOptions
	fs.StringVar(&ts.server, "tsls", "", "typescript-language-server for <script lang=\"ts\"> blocks: its executable, or its lib/cli.mjs run with -tsls-node (default: $VUKA_TSLS, PATH, node_modules/.bin)")
	fs.StringVar(&ts.node, "tsls-node", "", "the Node that runs a .js/.mjs -tsls (default: $VUKA_TSLS_NODE, then node on PATH); an Electron binary runs as Node")
	fs.StringVar(&ts.tsserver, "tsserver", "", "the TypeScript (tsserver.js or its lib directory) used when the module has none of its own (default: $VUKA_TSSERVER)")
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
	return serveLSP(gopls, *logFile, lspOptions{goplsArgs: goplsArgs, renameCommands: true, ts: ts})
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return runLSP(ctx, os.Stdin, os.Stdout, logw, gopls, opts)
}

// lspOptions are how vuka lsp runs gopls and talks to the editor.
type lspOptions struct {
	goplsArgs      []string // before serve, e.g. -remote=auto
	renameCommands bool     // namespace gopls's commands (vuka.gopls.*) for a second client
	dropIn         bool     // serving as gopls: .templ files are templ's language server's
	ts             tsOptions
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

	p := newProxy(newRPCConn(in, out), newRPCConn(cout, cin).queued(), logw, tmp)
	p.renameCommands, p.dropIn, p.tsOpts = opts.renameCommands, opts.dropIn, opts.ts
	goplsDone, editorDone := make(chan struct{}), make(chan struct{})
	go func() { p.fromGopls(); close(goplsDone) }()
	go func() { p.fromEditor(); close(editorDone) }()
	select {
	case <-editorDone:
	case <-goplsDone:
	case <-ctx.Done():
	}
	p.stop()
	p.gopls.flush(time.Second) // the editor's shutdown and exit, if it sent them
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
	source   string // the .vuka or .templ path
	from     []byte // the source text it was generated from
	cur      []byte // the editor's text, when it differs from from
	hunks    []hunk // where cur and from differ
	stale    bool   // cur was edited since from: the hunks are a diff
	gen      []byte
	m        *transpile.SourceMap    // a .vuka file's map
	templ    *templparser.SourceMap  // a .templ file's map (templ's own)
	files    []transpile.FileRef     // a .vuka file's vuka.File literals, in from
	names    map[string]string       // the package's overloads: Go name → the source's
	vars     []transpile.VarRead     // the context processors' variables it reads by name
	avail    []transpile.VarRead     // and those it may read
	unwraps  []transpile.Unwrap      // the calls in its markup returning (T, error)
	implicit []transpile.ImplicitCtx // the calls in its markup given the render's ctx
	fixes    []edit                  // the W0402 rewrites on from (ctxVarFixes)
	version  int
	probed   int  // the version holding the last probe (see probe)
	pending  bool // not sent to gopls yet (lsp_open.go)
}

// namesOf is the package's overloads, Go name → the source's; nil for no
// file.
func (f *vfile) namesOf() map[string]string {
	if f == nil {
		return nil
	}
	return f.names
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
	o := f.fromOff(offsetOf(f.text(), pos))
	g, _ := f.m.ToGenerated(o)
	// At a name's first byte, the name: not text Vuka wrote before it (a
	// bare field's receiver).
	if o < len(f.from) && isIdentByte(f.from[o]) && (o == 0 || !isIdentByte(f.from[o-1])) {
		if next, exact := f.m.ToGenerated(o + 1); exact && next-1 > g {
			g = next - 1
		}
	}
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
	if exact && s == e && r.Start != r.End {
		return r, false // a name Vuka wrote: the receiver type of a method it generates
	}
	return lspRange{positionOf(f.text(), f.curOff(s)), positionOf(f.text(), f.curOff(e))}, true
}

// fromRange is a range in the generated Go as offsets in the text it came from.
func (f *vfile) fromRange(r lspRange) (s, e int, ok bool) {
	gs, ge := offsetOf(f.gen, r.Start), offsetOf(f.gen, r.End)
	s, _, ok1 := f.m.ToSource(gs)
	e, _, ok2 := f.m.ToSource(ge)
	if ge > gs && (!ok2 || e < s) {
		// The end of a statement's copy is where the next one starts, and
		// that may be elsewhere: the range ends after its last byte.
		if last, _, ok := f.m.ToSource(ge - 1); ok && last >= s {
			e, ok2 = last+1, true
		}
	}
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
	attr      bool         // completion after @, on an attribute or decorator
	field     bool         // and the attribute is a struct field's
	typed     lspRange     // there: what was typed after the last dot
	full      lspRange     // and after the @
	qualifier string       // the package typed before the dot, if any
	closeTag  *lspRange    // asked on a closing tag's name, sent to its opening tag's
	blocks    *lspRange    // completion after a markup child's {: the word typed, for block snippets
	note      string       // hover: what to add to gopls's answer (an island page's lifecycle hook)
	spot      *staticSpot  // a possible Type.member use at the cursor (statics, field references)
	tmpl      *tmplSpot    // completion in a template file's Go
	varTyped  string       // completion of a bare name: what is typed of it
	varRange  *lspRange    // and where
	timer     *time.Timer  // answers it without gopls at goplsDeadline
	scripts   []any        // references: the scripts' calls of the method at the cursor (live.reorder)
	prop      *lspPosition // references on a shape's prop: its declaration in the generated Go
	propURI   string       // and the generated file's uri
}

type proxy struct {
	editor, gopls *rpcConn
	log           io.Writer
	tmp           string

	genMu genLock
	// view is held to read the generated files and ask gopls about them
	// (shared), or to change them and tell gopls (alone): a request never
	// reaches gopls mapped onto a generation gopls hasn't been sent yet.
	view sync.RWMutex

	mu               sync.Mutex
	initID           string // the editor's initialize request
	noCtxHints       bool   // initializationOptions.vuka.implicitContextHints is false
	renameCommands   bool
	dropIn           bool
	root             string
	nextID           int
	goReqs           map[string]bool           // editor requests on Go files
	goAfter          map[string]func(any) any  // and what to add to gopls's answer to them
	vukaReqs         map[string]*vukaRequest   // proxy request id → request on a .vuka file
	asks             map[string]chan rpcMsg    // proxy request id → the proxy's own question to gopls
	dropped          map[string]bool           // proxy request ids given up on: gopls's late answer goes nowhere
	replies          replies                   // the editor's requests not answered yet (lsp_answers.go)
	bufs             map[string][]byte         // .vuka path → editor buffer
	cursors          map[string]int            // .vuka path → where the editor's last change to it ended
	virtual          map[string]*vfile         // generated path → buffer in gopls
	owned            map[string]bool           // Go paths the editor has open itself; its text wins
	bySource         map[string]string         // .vuka path → generated path
	vukaDiags        map[string]*diagSet       // .vuka path → the last build's transpiler errors
	syntaxDiags      map[string]*diagSet       // .vuka path → its syntax errors, as the editor has it now
	goDiags          map[string]*diagSet       // .vuka path → gopls diagnostics mapped from its Go
	goHeld           map[string]heldDiags      // generated path → gopls's diagnostics of it, kept back while its source was edited
	shown            map[string]string         // real path → the path the editor uses for it
	decos            []projectDecorator        // the module's decorators, as of the last regenerate
	modRoot, modPath string                    // the module, as of the last regenerate
	staticsPkgs      map[string][]*staticsFile // dependency dir → its parsed Go, for statics
	importDirs       map[string]string         // import path → package directory
	opening          opening                   // generated files gopls hasn't been sent (lsp_open.go)
	started          bool
	timer            *time.Timer
	stopped          bool

	// Builds: one runs at a time, on its own goroutine; see build.
	buildMu  sync.Mutex     // held by the build that runs
	pubMu    sync.Mutex     // held while diagnostics are worked out and sent
	synMu    sync.Mutex     // held while a buffer's syntax is checked
	sess     *load.Session  // what the module's last builds transpiled; nil before the first
	building bool           // a build runs
	again    bool           // and another is wanted once it ends
	preempt  atomic.Bool    // the build that runs should leave the rest of the module for later
	built    *sync.Cond     // on mu: a build finished a stage
	edits    int            // the editor's changes to .vuka buffers so far
	changed  map[string]int // .vuka path → edits when its buffer last changed
	done     map[string]int // .vuka path → edits as of the build that last transpiled its buffer
	gaveUp   map[string]int // .vuka path → the edits count a request stopped waiting for (await)
	snapped  int            // edits when the last build took the buffers
	syntax   syntaxChecker

	tsOpts tsOptions
	tsOnce sync.Once
	ts     atomic.Pointer[tsServer] // the TypeScript server for script blocks, once started

	sem semState // markup tokens, code lenses and route links (lsp_semantic_html.go, lsp_routes.go)
}

func newProxy(editor, gopls *rpcConn, log io.Writer, tmp string) *proxy {
	p := &proxy{
		editor: editor, gopls: gopls, log: log, tmp: tmp,
		goReqs: map[string]bool{}, vukaReqs: map[string]*vukaRequest{}, asks: map[string]chan rpcMsg{}, dropped: map[string]bool{},
		bufs: map[string][]byte{}, cursors: map[string]int{}, virtual: map[string]*vfile{}, bySource: map[string]string{}, owned: map[string]bool{},
		vukaDiags: map[string]*diagSet{}, syntaxDiags: map[string]*diagSet{}, goDiags: map[string]*diagSet{},
		shown:       map[string]string{},
		staticsPkgs: map[string][]*staticsFile{}, importDirs: map[string]string{},
		changed: map[string]int{}, done: map[string]int{},
	}
	p.built = sync.NewCond(&p.mu)
	editor.dropping(p.dropReply)
	return p
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
	p.built.Broadcast()
	p.mu.Unlock()
	p.buildMu.Lock() // the build that runs has stopped
	p.buildMu.Unlock()
	p.ts.Load().stop()
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

// semanticTokensMethods are the .vuka requests whose result is a token array
// over the generated Go, remapped onto the source.
var semanticTokensMethods = map[string]bool{
	"textDocument/semanticTokens/full":  true,
	"textDocument/semanticTokens/range": true,
}

func isVuka(path string) bool {
	return strings.HasSuffix(path, ".vuka") || strings.HasSuffix(path, ".vuka.html")
}

// routeGo reports whether path is a .go file in a bracketed folder or a
// route group, which gopls sees as a copy in the folder's Go name
// (load.RouteMapped): its requests are answered there, as a .vuka file's.
func (p *proxy) routeGo(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	p.mu.Lock()
	root := p.modRoot
	p.mu.Unlock()
	if root == "" {
		root = string(filepath.Separator)
	}
	return load.RouteMapped(root, filepath.Dir(path))
}

// moduleFile reports whether path is a go.mod, go.sum or go.work file.
func moduleFile(path string) bool {
	switch filepath.Base(path) {
	case "go.mod", "go.sum", "go.work":
		return true
	}
	return false
}

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
	msgs := make(chan editorMsg, 256)
	go p.readEditor(msgs)
	for em := range msgs {
		body, m := em.body, em.m
		if m.isRequest() && !p.reach(m.ID) {
			continue // answered while it waited
		}
		var doc struct {
			TextDocument textDocumentID `json:"textDocument"`
		}
		_ = json.Unmarshal(m.Params, &doc)
		path := uriToPath(doc.TextDocument.URI)
		if p.refactorRequest(&m, path) {
			continue // vuka's own refactors and quick fixes (lsp_refactor.go)
		}

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
			p.untrack(m.ID)
			_ = p.gopls.send(&m)
			continue
		case "initialized":
			_ = p.gopls.write(body)
			p.mu.Lock()
			p.started, p.building = true, true
			p.mu.Unlock()
			p.builds() // the first, before anything else reaches gopls
			continue
		case "$/cancelRequest":
			if p.cancelVuka(m.Params) {
				continue
			}
		case "exit":
			_ = p.gopls.write(body)
			return
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose", "textDocument/didSave":
			if isVuka(path) || p.routeGo(path) {
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
		case "workspace/willRenameFiles":
			go p.willRenameFiles(&m)
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
				if path := uriToPath(c.URI); (strings.HasSuffix(path, ".go") || isVuka(path) || strings.HasSuffix(path, ".templ") || moduleFile(path)) && !p.isVirtual(path) {
					p.schedule()
					break
				}
			}
			continue
		}
		if isVuka(path) || p.routeGo(path) {
			switch {
			case m.Method == "textDocument/formatting" && isVuka(path):
				p.formatting(&m, p.real(path))
			case m.Method == "textDocument/rangeFormatting" && isVuka(path):
				p.rangeFormatting(&m, p.real(path))
			case m.isRequest():
				p.vukaRequest(&m, p.real(path))
			}
			continue
		}
		if strings.HasSuffix(path, ".go") && p.goRouteRequest(&m, path) {
			continue
		}
		if m.Method == "textDocument/completion" && jsxTriggered(m.Params) {
			_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"isIncomplete": false, "items": []any{}}})
			continue
		}
		if m.isRequest() {
			if strings.HasSuffix(path, ".go") {
				p.goRouteAfter(&m, path)
			}
			p.mu.Lock()
			p.goReqs[string(m.ID)] = true
			p.mu.Unlock()
			p.untrack(m.ID)
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
			after := p.goAfter[id]
			delete(p.goAfter, id)
			dropped := p.dropped[id]
			delete(p.dropped, id)
			p.mu.Unlock()
			if vr != nil {
				vr.timer.Stop()
				p.answer(vr, &m)
				continue
			}
			if dropped {
				continue // answered already, without gopls
			}
			if withGoAfter(&m, after) {
				body, _ = json.Marshal(&m)
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

// ctxHintsOn reports whether inlay hints show the ctx markup's calls get
// (initializationOptions {"vuka": {"implicitContextHints": false}} turns
// them off).
func (p *proxy) ctxHintsOn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.noCtxHints
}

// realRoots resolves symlinks in the workspace folders of initialize params.
func realRoots(raw json.RawMessage) json.RawMessage {
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return raw
	}
	if o, ok := params["initializationOptions"].(map[string]any); ok {
		delete(o, "vuka") // vuka's own options, not gopls's settings
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
				caps["documentRangeFormattingProvider"] = true
				addRefactorKinds(caps)
				if caps["documentLinkProvider"] == nil {
					caps["documentLinkProvider"] = map[string]any{} // vuka.File literals
				}
				// Semantic tokens: gopls's, remapped onto the .vuka source, with
				// gopls's own legend so the token-type indices carry through. full
				// and range; no delta (the editor asks for a full set each time).
				if stp, ok := caps["semanticTokensProvider"].(map[string]any); ok {
					if legend, ok := stp["legend"]; ok {
						caps["semanticTokensProvider"] = map[string]any{"legend": legend, "full": true, "range": true}
					}
				}
				p.extendCaps(caps)
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
	fmtSrc := format.Source
	if strings.HasSuffix(path, ".vuka.html") {
		fmtSrc = func(src []byte) ([]byte, error) { return formatTemplateFile(path, src, false) }
	}
	if out, err := fmtSrc(src); err != nil {
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
		Options struct {
			Vuka struct {
				ImplicitContextHints *bool `json:"implicitContextHints"`
			} `json:"vuka"`
		} `json:"initializationOptions"`
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
	p.noCtxHints = ip.Options.Vuka.ImplicitContextHints != nil && !*ip.Options.Vuka.ImplicitContextHints
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
	pending := vf != nil && vf.pending
	if pending {
		vf.pending = false // gopls gets the editor's buffer, then the proxy's
		p.dropOpen(path)
	}
	p.mu.Unlock()
	uri := pathToURI(path)
	if open && vf != nil && !was && !pending {
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

// vukaRequest asks gopls the editor's question at the matching place in the
// generated Go.
func (p *proxy) vukaRequest(m *rpcMsg, path string) {
	reply := func(result any) {
		if pendingRefactors(m.ID) {
			go func() {
				_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": withRefactors(result, m.ID)})
			}()
			return
		}
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
	if p.tsRequest(m, path) {
		return
	}
	done, shapeNote := p.shapeRequest(m, path)
	if done {
		return
	}
	if !vukaMethods[m.Method] && !semanticTokensMethods[m.Method] {
		reply(nil)
		return
	}
	p.await(path)
	p.mu.Lock()
	gen := p.bySource[path]
	p.mu.Unlock()
	if spanning[m.Method] {
		p.awaitAllOpen(awaitLimit)
	} else if gen != "" {
		p.awaitOpen(gen, awaitLimit)
	}
	p.view.RLock()
	defer p.view.RUnlock()
	p.mu.Lock()
	vf := p.virtual[p.bySource[path]]
	if vf == nil {
		p.mu.Unlock()
		reply(nil)
		return
	}
	if buf, ok := p.bufs[path]; ok && !vf.isTempl() && !bytes.Equal(vf.text(), buf) {
		// Edited since its last build (which took too long to wait for):
		// the last generation, mapped onto the text as it is now.
		cur := *vf
		cur.withText(buf, nil)
		vf = &cur
	}
	if semanticTokensMethods[m.Method] && (vf.isTempl() || vf.stale) {
		// Mid-edit the generated Go is stale against the buffer; whole-file
		// tokens remapped onto it would be wrong. Empty is better than garbage,
		// like the diagnostic gating; the next settled build re-highlights.
		p.mu.Unlock()
		reply(p.markupOnlyTokens(vf))
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
		if h := p.islandRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			reply(h())
			return
		}
		if h := p.urlRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			reply(h())
			return
		}
		if h := p.fileRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			reply(h())
			return
		}
		if h := p.contextVarRequest(m.Method, vf, off); h != nil {
			p.mu.Unlock()
			go func() { reply(h()) }()
			return
		}
		if h := p.authoringRequest(m.Method, vf, genPath, off); h != nil {
			p.mu.Unlock()
			go func() { reply(h()) }()
			return
		}
		if m.Method == "textDocument/completion" && strings.HasSuffix(vf.source, ".vuka.html") && off > 0 && markupText(src)(off-1) {
			// Markup text is no Go: nothing completes there, and an item
			// would import a package for it.
			p.mu.Unlock()
			reply(map[string]any{"isIncomplete": false, "items": []any{}})
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
	vr.field = vr.attr && fieldAttrSpot(vf.text(), offsetOf(vf.text(), srcPos))
	if m.Method == "textDocument/references" && hasPos {
		vr.scripts = p.scriptCallsAt(vf, offsetOf(vf.text(), srcPos))
		vr.prop, vr.propURI = propDeclAt(vf, offsetOf(vf.text(), srcPos)), pathToURI(genPath)
	}
	if m.Method == "textDocument/completion" && !strings.HasSuffix(vf.source, ".vuka.html") {
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
	if hasPos && m.Method == "textDocument/hover" {
		vr.note = islandHookNote(p.modRoot, vf.source, vf.text(), offsetOf(vf.text(), srcPos))
		if u, ok := vf.unwrapAt(offsetOf(vf.text(), srcPos)); ok {
			vr.note = strings.TrimPrefix(vr.note+"\n\n---\n\n"+unwrapNote(u), "\n\n---\n\n")
		}
		if _, ok := vf.implicitAt(offsetOf(vf.text(), srcPos)); ok {
			vr.note = strings.TrimPrefix(vr.note+"\n\n---\n\n"+implicitNote, "\n\n---\n\n")
		}
		if shapeNote != "" {
			vr.note = strings.TrimPrefix(vr.note+"\n\n---\n\n"+shapeNote, "\n\n---\n\n")
		}
	}
	if hasPos && !vr.attr && (m.Method == "textDocument/completion" || m.Method == "textDocument/hover") {
		vr.spot = staticSpotAt(m.Method, vf.text(), offsetOf(vf.text(), srcPos))
	}
	if hasPos && m.Method == "textDocument/completion" && !vr.attr && len(vf.avail) > 0 {
		src := vf.text()
		off := offsetOf(src, srcPos)
		if start := varPrefix(src, off); start >= 0 {
			vr.varTyped, vr.varRange = string(src[start:off]), &lspRange{positionOf(src, start), srcPos}
		}
	}
	if hasPos && m.Method == "textDocument/completion" && !vr.attr && vr.blocks == nil && closeTag == nil {
		vr.tmpl = templateSpot(vf, genPath, offsetOf(vf.text(), srcPos))
	}
	vr.timer = time.AfterFunc(goplsDeadline, func() { p.expire(id) })
	p.vukaReqs[id] = vr
	p.mu.Unlock()
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": m.Method, "params": params})
}

// editorMsg is a message from the editor, read ahead of the loop handling
// it.
type editorMsg struct {
	body []byte
	m    rpcMsg
}

// readEditor reads the editor's messages for fromEditor, which can be busy
// with a build — the first one runs before anything else. Responses go
// on at once (gopls asks the editor for its configuration meanwhile).
// Requests are tracked until answered, and a cancelled one is answered at
// once (lsp_answers.go).
func (p *proxy) readEditor(msgs chan<- editorMsg) {
	defer close(msgs)
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
			if !p.refactorReply(&m) {
				_ = p.gopls.write(body)
			}
			continue
		}
		switch {
		case m.isRequest():
			p.track(&m)
		case m.Method == "$/cancelRequest":
			p.cancelled(m.Params)
		}
		msgs <- editorMsg{body, m}
	}
}

// goplsDeadline is how long a .vuka request waits for gopls. A busy gopls
// (a shared daemon loading another workspace) can take minutes; the editor
// gets what vuka knows without it instead — the markup's own semantic
// tokens, no hover — rather than nothing at all.
var goplsDeadline = 15 * time.Second

// expire answers the .vuka request id without gopls, if gopls hasn't.
func (p *proxy) expire(id string) {
	p.mu.Lock()
	vr := p.vukaReqs[id]
	if vr == nil {
		p.mu.Unlock()
		return
	}
	delete(p.vukaReqs, id)
	p.cancelGopls(id)
	p.mu.Unlock()
	p.logf("%s: no answer from gopls in %v", vr.method, goplsDeadline)
	p.answer(vr, &rpcMsg{Error: json.RawMessage(`{"code":-32803,"message":"gopls didn't answer in time"}`)})
}

// cancelVuka takes the editor's $/cancelRequest of a .vuka request: gopls
// knows it by the proxy's id. It reports whether the request was one.
func (p *proxy) cancelVuka(params json.RawMessage) bool {
	var cp struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(params, &cp) != nil || len(cp.ID) == 0 {
		return false
	}
	p.mu.Lock()
	for id, vr := range p.vukaReqs {
		if bytes.Equal(vr.editorID, cp.ID) {
			delete(p.vukaReqs, id)
			vr.timer.Stop()
			p.cancelGopls(id)
			p.mu.Unlock()
			_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": vr.editorID, "error": map[string]any{"code": -32800, "message": "cancelled"}})
			return true
		}
	}
	p.mu.Unlock()
	return false
}

// cancelGopls tells gopls the proxy no longer wants its answer to id, and
// drops that answer when it comes. p.mu is held.
func (p *proxy) cancelGopls(id string) {
	p.dropped[id] = true
	go p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": json.RawMessage(id)}})
}

// answer hands gopls's answer to a .vuka request back, mapped onto the source.
func (p *proxy) answer(vr *vukaRequest, m *rpcMsg) {
	reply := map[string]any{"jsonrpc": "2.0", "id": vr.editorID}
	if len(m.Error) > 0 {
		// gopls can't answer mid-edit or with a broken import; like gopls
		// under the Go extension, that's no answer rather than an error.
		switch {
		case vr.method == "textDocument/documentLink":
			reply["result"] = p.withRouteLinks(p.documentLinks(vr.file), vr.file)
		case vr.method == "textDocument/foldingRange":
			reply["result"] = foldingRanges(vr.file, nil)
		case vr.method == "textDocument/inlayHint" && p.ctxHintsOn() && vr.file != nil:
			reply["result"] = withImplicitHints(nil, vr.file)
		case vr.method == "textDocument/codeAction":
			go func() {
				reply["result"] = withRefactors(p.withShapeScriptAction(vr.file, nil), vr.editorID)
				_ = p.editor.send(reply)
			}()
			return
		case vr.blocks != nil:
			reply["result"] = map[string]any{"isIncomplete": false, "items": blockItems(*vr.blocks)}
		case vr.field:
			go func() {
				if items, ok := p.fieldAttrItems(vr.file, vr.qualifier, vr.typed); ok {
					reply["result"] = withFieldAttrItems(nil, items, vr.qualifier, vr.typed)
				}
				_ = p.editor.send(reply)
			}()
			return
		case vr.spot != nil && vr.method == "textDocument/completion":
			go func() {
				if items := p.staticCompletionItems(vr.file, vr.spot); items != nil {
					reply["result"] = map[string]any{"isIncomplete": false, "items": items}
				}
				_ = p.editor.send(reply)
			}()
			return
		case vr.spot != nil && vr.method == "textDocument/hover":
			go func() {
				if h := p.staticHoverItem(vr.file, vr.spot); h != nil {
					reply["result"] = h
				}
				_ = p.editor.send(reply)
			}()
			return
		case semanticTokensMethods[vr.method]:
			reply["result"] = p.markupOnlyTokens(vr.file)
		case vr.varRange != nil:
			reply["result"] = map[string]any{"isIncomplete": false, "items": varItems(vr.file, vr.varTyped, *vr.varRange)}
		case quietMethods[vr.method]:
			p.logf("%s: %s", vr.method, m.Error)
			reply["result"] = nil
		default:
			reply["error"] = m.Error
		}
		_ = p.editor.send(reply)
		return
	}
	if semanticTokensMethods[vr.method] {
		reply["result"] = p.semanticTokens(vr.file, m.Result)
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
		v = cleanCompletion(v, vr.file.namesOf())
		if vr.varRange != nil {
			v = withItems(v, newItems(v, varItems(vr.file, vr.varTyped, *vr.varRange)))
		}
		if vr.blocks != nil {
			v = withBlockItems(v, *vr.blocks)
		}
		if vr.field {
			got := v
			go func() {
				if items, ok := p.fieldAttrItems(vr.file, vr.qualifier, vr.typed); ok {
					reply["result"] = withFieldAttrItems(got, items, vr.qualifier, vr.typed)
				} else {
					reply["result"] = completeList(attrCompletion(got, vr.typed))
				}
				_ = p.editor.send(reply)
			}()
			return
		}
		if vr.attr {
			v = attrCompletion(v, vr.typed)
			p.mu.Lock()
			decos := p.decos
			p.mu.Unlock()
			v = completeList(withItems(v, decoratorItems(vr.file, decos, vr.full, vr.qualifier)))
		}
		if vr.spot != nil {
			merged := v
			go func() {
				merged = withItems(merged, newItems(merged, p.staticCompletionItems(vr.file, vr.spot)))
				if vr.tmpl != nil {
					vr.tmpl.members = false
					merged = p.templateCompletion(vr.file, vr.tmpl, merged)
				}
				reply["result"] = merged
				_ = p.editor.send(reply)
			}()
			return
		}
		if vr.tmpl != nil {
			got := v
			go func() {
				reply["result"] = p.templateCompletion(vr.file, vr.tmpl, got)
				_ = p.editor.send(reply)
			}()
			return
		}
	case "textDocument/hover":
		v = demangleStrings(v, vr.file.namesOf())
		if vr.note != "" {
			v = withNote(v, vr.note)
		}
		if h, ok := v.(map[string]any); ok && vr.closeTag != nil {
			h["range"] = *vr.closeTag
		}
		if vr.spot != nil {
			usual := v
			go func() {
				if h := p.staticHoverItem(vr.file, vr.spot); h != nil {
					reply["result"] = h
				} else {
					reply["result"] = usual
				}
				_ = p.editor.send(reply)
			}()
			return
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
		if vr.method == "textDocument/documentHighlight" {
			v = withRanges(v)
		}
		v = p.withCloseTwins(vr.method, v, vr.file)
		if len(vr.scripts) > 0 {
			list, _ := v.([]any)
			v = append(list, vr.scripts...)
		}
		if vr.prop != nil {
			list, _ := v.([]any)
			go func() {
				reply["result"] = withLocations(list, p.fieldTagRefs(vr.propURI, *vr.prop, vr.file)...)
				_ = p.editor.send(reply)
			}()
			return
		}
	case "textDocument/signatureHelp", "textDocument/documentSymbol", "textDocument/inlayHint":
		v = demangleStrings(v, vr.file.namesOf())
		if vr.method == "textDocument/inlayHint" && p.ctxHintsOn() && vr.file != nil {
			v = withImplicitHints(v, vr.file)
		}
		if vr.method == "textDocument/documentSymbol" && isShape(vr.file) {
			v = shapeSymbols(vr.file, v)
		}
	case "textDocument/documentLink":
		v = p.withRouteLinks(p.withFileLinks(v, vr.file), vr.file)
	case "textDocument/foldingRange":
		v = foldingRanges(vr.file, v)
	case "textDocument/codeAction":
		got := p.withShapeScriptAction(vr.file, withoutGoplsOrganize(v))
		go func() {
			reply["result"] = withRefactors(got, vr.editorID)
			_ = p.editor.send(reply)
		}()
		return
	}
	reply["result"] = v
	_ = p.editor.send(reply)
}

// ask sends gopls a request of the proxy's own and waits for the answer.
func (p *proxy) ask(method string, params any) (json.RawMessage, bool) {
	return p.askUntil(10*time.Second, nil, method, params)
}

// askUntil is ask waiting at most d, and no longer once stop is closed.
func (p *proxy) askUntil(d time.Duration, stop <-chan struct{}, method string, params any) (json.RawMessage, bool) {
	m, ok := p.askWithinStop(d, stop, method, params)
	if !ok {
		return nil, false
	}
	if len(m.Error) > 0 {
		p.logf("%s: %s", method, m.Error)
		return nil, false
	}
	return m.Result, true
}

// askWithin sends gopls a request of the proxy's own and waits, at most d,
// for the answer, which may be an error; ok is false when none came.
func (p *proxy) askWithin(d time.Duration, method string, params any) (m rpcMsg, ok bool) {
	return p.askWithinStop(d, nil, method, params)
}

// askWithinStop is askWithin giving up early once stop is closed.
func (p *proxy) askWithinStop(d time.Duration, stop <-chan struct{}, method string, params any) (m rpcMsg, ok bool) {
	p.mu.Lock()
	p.nextID++
	id := strconv.Quote("vuka-ask:" + strconv.Itoa(p.nextID))
	ch := make(chan rpcMsg, 1)
	p.asks[id] = ch
	p.mu.Unlock()
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case m := <-ch:
		return m, true
	case <-t.C:
	case <-stop:
	}
	p.mu.Lock()
	if p.asks[id] != nil {
		delete(p.asks, id)
		p.cancelGopls(id)
	}
	p.mu.Unlock()
	return rpcMsg{}, false
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
					_, isLocation := x["uri"]
					if _, isEdit := x["newText"]; isLocation || isEdit || x["targetUri"] != nil {
						return nil
					}
					delete(x, key)
					continue
				}
				x[key] = r
			default:
				if _, nested := val.(map[string]any); nested {
					if r := p.rewrite(val, ctx, origin); r != nil {
						x[key] = r
					} else {
						delete(x, key)
					}
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

// mangled matches an overload's name as older vuka commands wrote it
// (area__Circle) and Vuka's temporaries (__e1), which the editor should
// never show.
var mangled = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_]*?)__[A-Za-z0-9_]+\b`)

var ident = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)

// demangle is s with each overload's Go name (names: areaCircle) spelled
// as the source writes it (area).
func demangle(s string, names map[string]string) string {
	s = mangled.ReplaceAllString(s, "$1")
	if len(names) == 0 {
		return s
	}
	return ident.ReplaceAllStringFunc(s, func(w string) string {
		if n, ok := names[w]; ok {
			return n
		}
		return w
	})
}

func demangleStrings(v any, names map[string]string) any {
	switch x := v.(type) {
	case string:
		return demangle(x, names)
	case []any:
		for i := range x {
			x[i] = demangleStrings(x[i], names)
		}
	case map[string]any:
		for k := range x {
			x[k] = demangleStrings(x[k], names)
		}
	}
	return v
}

// cleanCompletion drops Vuka's temporaries and shows each overload under the
// name the source uses (its detail still tells them apart).
func cleanCompletion(v any, names map[string]string) any {
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
		kept = append(kept, demangleStrings(item, names))
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
func completable(src []byte) ([]byte, []hunk) { return completableWith(src, nil) }

// completableAt is completable for the buffer of the file at path, the
// editor's cursor at cursor (-1: not known): in a template file, the Go
// being typed there is completed too (see templateRepairs).
func completableAt(path string, src []byte, cursor int) ([]byte, []hunk) {
	if !strings.HasSuffix(path, ".vuka.html") {
		return completable(src)
	}
	var extra []insert
	if cursor >= 0 {
		extra = templateRepairs(src, cursor)
	}
	return completableIn(src, extra, true)
}

// insert is text put into a buffer at an offset.
type insert struct {
	at   int
	text string
}

func completableWith(src []byte, extra []insert) ([]byte, []hunk) {
	return completableIn(src, extra, false)
}

// completableIn is completableWith; in a template file (html) a dot ending
// a sentence of its markup's text is no selector being typed: text never
// takes a placeholder, which would make it Go a completion imports for.
func completableIn(src []byte, extra []insert, html bool) ([]byte, []hunk) {
	var ins []insert
	for _, re := range []*regexp.Regexp{unfinishedAttr, unfinishedParamAttr, unfinishedElem} {
		for _, m := range re.FindAllSubmatchIndex(src, -1) {
			ins = append(ins, insert{m[3], placeholder + "()"})
		}
	}
	var text func(int) bool
	if html {
		text = markupText(src)
	}
	for _, at := range danglingDots(src) {
		if text == nil || !text(at-1) {
			ins = append(ins, insert{at, placeholder})
		}
	}
	for _, at := range fieldPlaceholders(src) {
		ins = append(ins, insert{at, placeholder})
	}
	for _, e := range extra {
		if !slices.Contains(ins, e) {
			ins = append(ins, e)
		}
	}
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at < ins[j].at })
	blank := blankUnfinishedTags(src)
	if len(extra) > 0 {
		blank = blankRepaired(src, extra)
	}
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

// blankRepaired is blankUnfinishedTags of src, its tags read with the
// repairs extra made: a hole a repair closes no longer runs on over its
// tag's >.
func blankRepaired(src []byte, extra []insert) []byte {
	sorted := slices.Clone(extra)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].at < sorted[j].at })
	var rep []byte
	keep := make([]int, 0, len(src)) // rep offset of each src byte
	last := 0
	for _, in := range sorted {
		for i := last; i < in.at; i++ {
			keep = append(keep, len(rep)+i-last)
		}
		rep = append(append(rep, src[last:in.at]...), in.text...)
		last = in.at
	}
	for i := last; i < len(src); i++ {
		keep = append(keep, len(rep)+i-last)
	}
	rep = append(rep, src[last:]...)
	blank := blankUnfinishedTags(rep)
	out := make([]byte, len(src))
	for i, at := range keep {
		out[i] = blank[at]
	}
	return out
}

// afterAt reports whether pos is in an attribute's name: the line so far is
// @ and a (possibly qualified) name.
func afterAt(src []byte, pos lspPosition) bool {
	off := offsetOf(src, pos)
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return attrPrefix.Match(src[start:off]) || attrSpot.Match(src[start:off]) || fieldAttrSpot(src, off)
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
