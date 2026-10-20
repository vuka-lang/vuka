package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	templparser "github.com/a-h/templ/parser/v2"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// How the language server keeps the generated Go current:
//
//   - On every change to a .vuka buffer, the file alone is checked for
//     syntax errors (transpile.Syntax), which are shown at once.
//   - After a short quiet period a build transpiles the module through a
//     load.Session, which transpiles again only what changed. A build first
//     transpiles the packages of the buffers edited since the last one (and
//     what they import, mostly reused), syncs gopls and the diagnostics with
//     them, and only then the rest of the module: packages importing them,
//     whose Go may follow a changed API. A newer edit, or a request on a file
//     edited since, stops that second stage between two packages; the next
//     build does it.
//   - A request on a .vuka file waits for the first stage of the build of
//     its last edit, never for the rest of the module. A request on a file
//     not edited since its last build is answered at once.

// buildDelay is the quiet period after an edit before a build starts.
const buildDelay = 50 * time.Millisecond

// awaitLimit bounds how long a request waits for its file's build; past it,
// the request is answered from the last generation.
const awaitLimit = 3 * time.Second

// schedule builds after a short quiet period.
func (p *proxy) schedule() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || !p.started {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(buildDelay, p.kick)
}

// kick starts a build now, or, while one runs, asks it to finish its first
// stage and leave the rest to another build right after it.
func (p *proxy) kick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kickLocked()
}

func (p *proxy) kickLocked() {
	if p.stopped || !p.started {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	if p.building {
		p.again = true
		p.preempt.Store(true)
		return
	}
	p.building = true
	go p.builds()
}

// builds runs builds until none is wanted.
func (p *proxy) builds() {
	for {
		p.build()
		p.mu.Lock()
		if p.again && !p.stopped {
			p.again = false
			p.mu.Unlock()
			continue
		}
		p.building = false
		p.built.Broadcast()
		p.mu.Unlock()
		return
	}
}

// await waits until the build of the editor's last change to the .vuka file
// at path has transpiled it, starting that build now when it waits for the
// quiet period. It waits at most awaitLimit.
func (p *proxy) await(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changed[path] <= p.done[path] || !p.started {
		return
	}
	p.kickLocked()
	expired := false
	t := time.AfterFunc(awaitLimit, func() {
		p.mu.Lock()
		expired = true
		p.built.Broadcast()
		p.mu.Unlock()
	})
	defer t.Stop()
	for p.changed[path] > p.done[path] && !p.stopped && !expired {
		p.built.Wait()
	}
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
		delete(p.cursors, path)
		p.edits++
		p.changed[path] = p.edits
		p.mu.Unlock()
		go p.checkSyntax(path)
		if strings.HasSuffix(path, ".vuka.html") && strings.Contains(dp.TextDocument.Text, "<script") {
			go func() {
				if ts := p.typescript(); ts != nil {
					ts.sync(path, []byte(dp.TextDocument.Text))
				}
			}()
		}
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
		cursor := -1
		for _, c := range dp.ContentChanges {
			if c.Range == nil {
				cursor = changeEnd(buf, []byte(c.Text))
				buf = []byte(c.Text)
				continue
			}
			start, end := offsetOf(buf, c.Range.Start), offsetOf(buf, c.Range.End)
			if end < start {
				end = start
			}
			next := make([]byte, 0, len(buf)-(end-start)+len(c.Text))
			buf = append(append(append(next, buf[:start]...), c.Text...), buf[end:]...)
			cursor = start + len(c.Text)
		}
		p.bufs[path] = buf
		p.cursors[path] = cursor
		p.edits++
		p.changed[path] = p.edits
		p.mu.Unlock()
		go p.checkSyntax(path)
		if ts := p.ts.Load(); ts != nil && strings.HasSuffix(path, ".vuka.html") {
			go ts.sync(path, buf)
		}
	case "textDocument/didClose":
		p.mu.Lock()
		delete(p.bufs, path)
		delete(p.cursors, path)
		delete(p.changed, path)
		delete(p.done, path)
		delete(p.syntaxDiags, path)
		p.mu.Unlock()
	}
	p.schedule()
}

// build transpiles the module's Vuka packages, with the editor's unsaved
// buffers, and syncs gopls and the diagnostics with the result, in two
// stages: the packages of the buffers edited since the last build, then the
// whole module. A package that fails keeps its previous generated files, so
// the rest keeps type-checking.
func (p *proxy) build() {
	p.buildMu.Lock()
	defer p.buildMu.Unlock()
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	root, seq, since := p.root, p.edits, p.snapped
	p.snapped = seq
	bufs := make(map[string][]byte, len(p.bufs))
	cursors := make(map[string]int, len(p.cursors))
	edited := map[string]bool{}
	for k, v := range p.bufs {
		bufs[k] = v
		cursors[k] = -1
		if c, ok := p.cursors[k]; ok {
			cursors[k] = c
		}
		if p.changed[k] > since {
			edited[k] = true
		}
	}
	p.preempt.Store(false)
	p.mu.Unlock()
	// Whatever happens, the buffers taken are done with: a request on them
	// waits no longer.
	defer p.finished(bufs, seq)

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
			src, hs := completableAt(path, b, cursors[path])
			hunks[path] = hs
			return src, nil
		}
		return os.ReadFile(path)
	}
	t0 := time.Now()
	pkgs, err := load.Discover(modRoot, modPath, modRoot, true, read)
	if err != nil {
		p.logf("discover: %v", err)
		return
	}
	decos := findDecorators(modRoot, modPath, read)
	p.mu.Lock()
	p.decos = decos
	p.modRoot, p.modPath = modRoot, modPath
	p.mu.Unlock()
	if p.sess == nil || p.sess.Root() != modRoot {
		if p.sess, err = load.NewSession(modRoot); err != nil {
			p.logf("session: %v", err)
			p.sess = nil
		}
	}
	transpileAll := load.Transpile
	if p.sess != nil {
		transpileAll = p.sess.Transpile
	}
	run := func(pkgs []*load.Package, opts load.Options) ([]load.Generated, transpile.ErrorList, error) {
		gens, _, err := transpileAll(pkgs, p.tmp, opts)
		var list transpile.ErrorList
		if err != nil && !errors.As(err, &list) {
			if !errors.Is(err, load.ErrStopped) {
				p.logf("transpile: %v", err)
			}
			return gens, nil, err
		}
		return gens, list, nil
	}

	// First the packages of the edited buffers.
	var roots []*load.Package
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			if edited[filepath.Join(pkg.Dir, f.Name)] {
				roots = append(roots, pkg)
				break
			}
		}
	}
	first := load.Closure(pkgs, roots)
	if len(first) > 0 && len(first) < len(pkgs) {
		gens, list, err := run(first, load.Options{Bare: true})
		if err != nil {
			return
		}
		p.apply(first, gens, list, bufs, hunks, edited, seq, false)
		p.logf("built %d of %d packages in %v", len(first), len(pkgs), time.Since(t0).Round(time.Millisecond))
	} else {
		first = nil
	}

	// Then the module. Once the edited packages are done, it gives way to
	// newer edits.
	opts := load.Options{Bare: true}
	if first != nil || len(edited) == 0 {
		opts.Stop = p.preempt.Load
	}
	gens, list, err := run(pkgs, opts)
	if err != nil {
		if errors.Is(err, load.ErrStopped) {
			p.logf("stopped for a newer edit")
		}
		return
	}
	all := map[string]bool{}
	for path := range bufs {
		all[path] = true
	}
	p.apply(pkgs, gens, list, bufs, hunks, all, seq, true)
	p.logf("built the module (%d packages) in %v", len(pkgs), time.Since(t0).Round(time.Millisecond))
}

// changeEnd is where the editor's text was changed from old to cur, in cur:
// the end of what differs, for a change that sends the whole text.
func changeEnd(old, cur []byte) int {
	pre := 0
	for pre < len(old) && pre < len(cur) && old[pre] == cur[pre] {
		pre++
	}
	suf := 0
	for suf < len(old)-pre && suf < len(cur)-pre && old[len(old)-1-suf] == cur[len(cur)-1-suf] {
		suf++
	}
	return len(cur) - suf
}

// finished records that a build took the buffers bufs, as of the edits count
// seq, and wakes the requests waiting for them.
func (p *proxy) finished(bufs map[string][]byte, seq int) {
	p.mu.Lock()
	for path := range bufs {
		p.done[path] = max(p.done[path], seq)
	}
	p.built.Broadcast()
	p.mu.Unlock()
}

// apply syncs gopls and the diagnostics with what a build generated for
// pkgs (the whole module when all is set, which also closes generated files
// whose source is gone), and records that the buffers at paths are built as
// of the edits count seq.
func (p *proxy) apply(pkgs []*load.Package, gens []load.Generated, list transpile.ErrorList, bufs map[string][]byte, hunks map[string][]hunk,
	paths map[string]bool, seq int, all bool) {
	p.genMu.Lock()
	defer p.genMu.Unlock()
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
	// The errors, in the text the editor has (the transpiled text has
	// placeholders where the editor's is mid-edit).
	diags := map[string]*diagSet{}
	for _, e := range list {
		src, ok := sources[e.Pos.Filename]
		if !ok {
			continue
		}
		text, off := src, e.Pos.Offset
		if b, ok := bufs[e.Pos.Filename]; ok && !strings.HasSuffix(e.Pos.Filename, ".templ") {
			m := vfile{from: src, cur: b, hunks: hunks[e.Pos.Filename]}
			text, off = b, m.curOff(off)
		}
		d := diags[e.Pos.Filename]
		if d == nil {
			d = &diagSet{text: text}
			diags[e.Pos.Filename] = d
		}
		pos := positionOf(text, off)
		d.list = append(d.list, lspFromError(lspRange{pos, positionOf(text, off+e.Len)}, e))
	}
	// Which of them are syntax errors, that the editor's next change may
	// have mended: the file's own syntax check says so.
	for path, d := range diags {
		if _, open := bufs[path]; !open || !isVuka(path) {
			continue
		}
		syn := p.syntaxOf(path, d.text)
		for i, x := range d.list {
			for _, s := range syn.list {
				if equalDiag(x, s) {
					d.syntax = append(d.syntax, i)
					break
				}
			}
		}
	}

	want := map[string]*vfile{}
	for _, g := range gens {
		tm, _ := g.TemplMap.(*templparser.SourceMap)
		vf := &vfile{source: g.Source, from: g.From, gen: g.Src, m: g.Map, templ: tm, files: g.Files, names: g.Names}
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
	p.view.Lock()
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
	virtual := make(map[string]*vfile, len(p.virtual))
	for path, vf := range p.virtual {
		if _, ok := want[path]; ok {
			continue
		}
		if all || sources[vf.source] != nil {
			// Gone: its source is, or no longer generates it.
			if !p.owned[path] {
				ops = append(ops, op{"textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": pathToURI(path)}}})
			}
			continue
		}
		virtual[path] = vf
	}
	for path, vf := range want {
		virtual[path] = vf
	}
	p.virtual = virtual
	p.bySource = map[string]string{}
	for path, vf := range virtual {
		if vf.source != "" {
			p.bySource[vf.source] = path
		}
	}
	touched := map[string]bool{}
	for path := range p.vukaDiags {
		if all || sources[path] != nil {
			touched[path] = true
			delete(p.vukaDiags, path)
		}
	}
	for path, d := range diags {
		touched[path] = true
		p.vukaDiags[path] = d
	}
	for path := range paths {
		if p.done[path] < seq {
			p.done[path] = seq
			touched[path] = true
		}
	}
	p.built.Broadcast()
	p.mu.Unlock()

	for _, o := range ops {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": o.method, "params": o.params})
	}
	p.view.Unlock()
	for path := range touched {
		p.publish(path)
	}
}

// diagSet is diagnostics of a file and the text of the file they are of.
type diagSet struct {
	text   []byte
	list   []any // lspDiagnostic or gopls's map
	syntax []int // the transpiler's: those that are syntax errors
}

// in is the diagnostics moved onto the text cur, with or without the
// syntax errors.
func (d *diagSet) in(cur []byte, syntax bool) []any {
	if d == nil {
		return nil
	}
	skip := map[int]bool{}
	if !syntax {
		for _, i := range d.syntax {
			skip[i] = true
		}
	}
	same := cur == nil || bytes.Equal(cur, d.text)
	var m vfile
	if !same {
		m = vfile{from: d.text}
		m.withText(cur, nil)
	}
	out := []any{}
	for i, x := range d.list {
		if skip[i] {
			continue
		}
		if same {
			out = append(out, x)
			continue
		}
		move := func(r lspRange) lspRange {
			s, e := m.curOff(offsetOf(d.text, r.Start)), m.curOff(offsetOf(d.text, r.End))
			return lspRange{positionOf(cur, s), positionOf(cur, max(s, e))}
		}
		switch x := x.(type) {
		case lspDiagnostic:
			x.Range = move(x.Range)
			out = append(out, x)
		case map[string]any:
			if r, ok := toRange(x["range"]); ok {
				c := make(map[string]any, len(x))
				for k, v := range x {
					c[k] = v
				}
				c["range"] = move(r)
				out = append(out, c)
			}
		}
	}
	return out
}

func equalDiag(a, b any) bool {
	x, ok1 := a.(lspDiagnostic)
	y, ok2 := b.(lspDiagnostic)
	return ok1 && ok2 && x.Range == y.Range && x.Severity == y.Severity && x.Code == y.Code && x.Message == y.Message
}

// publish sends a .vuka file's diagnostics. While the editor's text is newer than the last build's, its own
// syntax errors stand for the transpiler's; without any, the build's other
// errors stay (moved along with the edits) until the next build; without
// those, gopls's on the generated Go.
func (p *proxy) publish(path string) {
	if p.dropIn && strings.HasSuffix(path, ".templ") {
		return // templ's language server reports on its files
	}
	p.pubMu.Lock()
	defer p.pubMu.Unlock()
	p.mu.Lock()
	islandText, opened := p.bufs[path]
	root := p.modRoot
	p.mu.Unlock()
	if !opened {
		islandText, _ = os.ReadFile(path)
	}
	island := islandDiags(root, path, islandText)
	p.mu.Lock()
	cur, open := p.bufs[path]
	fresh := !open || p.changed[path] <= p.done[path]
	var ds []any
	if syn := p.syntaxDiags[path]; !fresh && syn != nil && bytes.Equal(syn.text, cur) && len(syn.list) > 0 {
		ds = syn.in(cur, true)
	} else {
		ds = p.vukaDiags[path].in(cur, fresh)
	}
	if len(ds) == 0 {
		ds = p.goDiags[path].in(cur, true)
	}
	if open && !bytes.Equal(cur, islandText) {
		island = nil
	}
	ds = append(ds, island...)
	ds = append(ds, p.ts.Load().diagsFor(path)...)
	if ds == nil {
		ds = []any{}
	}
	b, _ := json.Marshal(ds)
	p.mu.Unlock()
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
		"params": map[string]any{"uri": pathToURI(p.display(path)), "diagnostics": json.RawMessage(b)}})
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
	// Drop a diagnostic storm gopls raises on a generated file that no longer
	// matches what we pushed, or that was built from a buffer the editor has
	// since moved past: the Go it checked still holds mid-edit placeholders, so
	// one edit scatters errors across the file that clear a moment later. Keep
	// the last good diagnostics until a build of the current buffer settles.
	if dp.Version != 0 && dp.Version != vf.version {
		p.mu.Unlock()
		return
	}
	if _, open := p.bufs[vf.source]; open && p.changed[vf.source] > p.done[vf.source] {
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
			obj["message"] = importHint(demangle(vf.m.Message(int(r.Start.Line)+1, msg), vf.names), p.modRoot, p.modPath)
		}
		delete(obj, "relatedInformation")
		delete(obj, "data")
		mapped = append(mapped, obj)
	}
	p.goDiags[vf.source] = &diagSet{text: vf.text(), list: mapped}
	p.mu.Unlock()
	p.publish(vf.source)
}

// checkSyntax checks the .vuka buffer at path for syntax errors and shows
// them, unless the buffer changes again first.
func (p *proxy) checkSyntax(path string) {
	p.synMu.Lock() // one check at a time, of the text as it is then
	defer p.synMu.Unlock()
	p.mu.Lock()
	buf, ok := p.bufs[path]
	seq := p.changed[path]
	if old := p.syntaxDiags[path]; !ok || old != nil && bytes.Equal(old.text, buf) {
		p.mu.Unlock()
		return // checked already
	}
	p.mu.Unlock()
	d := p.syntaxOf(path, buf)
	p.mu.Lock()
	if p.changed[path] != seq {
		p.mu.Unlock()
		return // a newer change has its own check
	}
	old := p.syntaxDiags[path]
	p.syntaxDiags[path] = d
	p.mu.Unlock()
	if old == nil || len(old.list) > 0 || len(d.list) > 0 {
		p.publish(path) // no news when the text had and has none
	}
}

// syntaxOf is the syntax errors of the editor's text buf of the .vuka file
// at path, as a build would report them for it.
func (p *proxy) syntaxOf(path string, buf []byte) *diagSet {
	src, hs := completable(buf)
	m := vfile{from: src, cur: buf, hunks: hs}
	d := &diagSet{text: buf}
	var others []transpile.File
	if strings.HasSuffix(path, ".vuka.html") {
		others = p.packageSources(path)
	}
	for _, e := range p.syntax.check(path, src, others) {
		pos := positionOf(buf, m.curOff(e.Pos.Offset))
		d.list = append(d.list, lspFromError(lspRange{pos, positionOf(buf, m.curOff(e.Pos.Offset+e.Len))}, e))
	}
	return d
}

// syntaxChecker checks one file at a time for syntax errors, importing a
// JSX target from its export data. The module's own packages aren't loaded
// (the go command would have to build them): when one is a file's JSX
// target, only the build finds it.
type syntaxChecker struct {
	mu            sync.Mutex
	root, modPath string
	gomod         []byte
	imp           *load.Importer
}

// packageSources are the Go and .vuka files of the package of the template
// at path, as the editor has them: what its syntax check reads its package
// clause, imports and component type from.
func (p *proxy) packageSources(path string) []transpile.File {
	dir := filepath.Dir(path)
	entries, _ := os.ReadDir(dir)
	generated := map[string]bool{}
	for _, e := range entries {
		if isVuka(e.Name()) {
			generated[transpile.GoName(e.Name())] = true
		}
	}
	var out []transpile.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || generated[name] || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.vuka") ||
			!strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".vuka") {
			continue
		}
		full := filepath.Join(dir, name)
		p.mu.Lock()
		src, ok := p.bufs[full]
		p.mu.Unlock()
		if !ok {
			var err error
			if src, err = os.ReadFile(full); err != nil {
				continue
			}
		}
		out = append(out, transpile.File{Name: name, Src: src})
	}
	return out
}

func (c *syntaxChecker) check(path string, src []byte, others []transpile.File) transpile.ErrorList {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := filepath.Dir(path)
	root, modPath, err := load.ModuleRoot(dir)
	if err != nil {
		root, modPath = dir, ""
	}
	gomod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if c.imp == nil || c.root != root || !bytes.Equal(gomod, c.gomod) {
		c.imp, c.root, c.modPath, c.gomod = load.NewModuleImporter(root), root, modPath, gomod
	}
	errs := transpile.Syntax(transpile.File{Name: filepath.Base(path), Src: src}, transpile.Options{
		Importer: outside{c.imp, modPath},
		Path:     func(name string) string { return filepath.Join(dir, name) },
		Bare:     true,
		Dir:      dir,
	}, others...)
	kept := errs[:0]
	for _, e := range errs {
		if !strings.HasPrefix(e.Msg, "JSX needs a target") {
			kept = append(kept, e)
		}
	}
	return kept
}

// outside imports packages outside the module only.
type outside struct {
	imp     types.Importer
	modPath string
}

func (o outside) Import(path string) (*types.Package, error) {
	if o.modPath != "" && (path == o.modPath || strings.HasPrefix(path, o.modPath+"/")) {
		return nil, errors.New(path + " is a package of this module")
	}
	return o.imp.Import(path)
}
