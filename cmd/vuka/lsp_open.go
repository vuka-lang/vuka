package main

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// How a build's new generated files reach gopls.
//
// The generated Go is on no disk, so gopls knows nothing of a generated
// file until it is opened, and each didOpen of a file it doesn't know
// starts a go list of that file's package. Every such load first writes
// each open buffer to a temporary directory (gocommand.WriteOverlays,
// which can't be cancelled), so n files opened at once cost n loads each
// writing up to n files, and a load cut short by the next didOpen is done
// again for the next snapshot: a module of a few hundred files kept gopls
// busy for minutes. A build therefore sends gopls its new files a package
// at a time, in dependency order, and before the next package waits for
// gopls to have loaded the last one (it asks gopls for the imports of one
// of its files, which takes that load and nothing more). Each package is
// then loaded with the files of the packages before it known, and nothing
// is loaded twice. The packages the editor asks about go first, after the
// packages they import. A few files are sent at once, as before.
//
// Files gopls has open get a didChange only when their Go changed, and a
// didClose when no source generates them any more; a file not yet sent
// gets neither, and is sent with its latest Go.

// openAtOnce is how many new generated files a build sends gopls without
// waiting between packages.
var openAtOnce = 48

// openWait bounds how long the next package waits for gopls to load the
// one sent before it.
var openWait = 5 * time.Second

// opening is the generated files a build made that gopls hasn't been sent.
type opening struct {
	queue   []string            // package directories with files to send, in order
	files   map[string][]string // package directory → its files to send
	deps    map[string][]string // package directory → the directories of the module packages it imports
	running bool                // openPackages runs
	sent    chan struct{}       // closed, and replaced, when a package is sent
	started time.Time
	count   int // files sent since the queue was last empty
	pkgs    int
}

// queueOpen records that the generated file at path, in p.virtual and
// pending, is to be sent to gopls. p.mu is held.
func (p *proxy) queueOpen(path string) {
	o := &p.opening
	if o.files == nil {
		o.files, o.deps, o.sent = map[string][]string{}, map[string][]string{}, make(chan struct{})
	}
	dir := filepath.Dir(path)
	if slices.Contains(o.files[dir], path) {
		return
	}
	if len(o.files[dir]) == 0 && !slices.Contains(o.queue, dir) {
		o.queue = append(o.queue, dir)
	}
	o.files[dir] = append(o.files[dir], path)
}

// dropOpen forgets a file not yet sent. p.mu is held.
func (p *proxy) dropOpen(path string) {
	o := &p.opening
	dir := filepath.Dir(path)
	if i := slices.Index(o.files[dir], path); i >= 0 {
		o.files[dir] = slices.Delete(o.files[dir], i, i+1)
	}
}

// recordDeps records which module packages each of pkgs imports, by
// directory, for ordering what is sent. p.mu is held.
func (p *proxy) recordDeps(pkgs []*load.Package) {
	o := &p.opening
	if o.deps == nil {
		o.deps = map[string][]string{}
	}
	dirOf := map[string]string{}
	for _, pkg := range pkgs {
		dirOf[pkg.ImportPath] = pkg.BuildDir()
	}
	for _, pkg := range pkgs {
		var deps []string
		for _, imp := range pkg.Imports {
			if d, ok := dirOf[imp]; ok && d != pkg.BuildDir() {
				deps = append(deps, d)
			}
		}
		o.deps[pkg.BuildDir()] = deps
	}
}

// sendOpens sends gopls the queued files: at once when there are few, else
// a package at a time from openPackages. p.mu is held, and p.view by the
// caller, who sends nothing to gopls for these files before.
func (p *proxy) sendOpens() {
	o := &p.opening
	n := 0
	for _, dir := range o.queue {
		n += len(o.files[dir])
	}
	if n == 0 || o.running {
		return
	}
	o.started, o.count, o.pkgs = time.Now(), 0, 0
	if n <= openAtOnce {
		for len(o.queue) > 0 {
			p.sendPackageLocked()
		}
		return
	}
	o.running = true
	go p.openPackages()
}

// openPackages sends the queued files a package at a time, waiting for
// gopls to load each.
func (p *proxy) openPackages() {
	for {
		p.view.Lock()
		p.mu.Lock()
		last := ""
		for last == "" && len(p.opening.queue) > 0 && !p.stopped {
			last = p.sendPackageLocked()
		}
		more := len(p.opening.queue) > 0 && !p.stopped
		if !more {
			o := &p.opening
			o.running = false
			if o.count > 0 {
				p.logf("sent gopls %d generated files of %d packages in %v", o.count, o.pkgs, time.Since(o.started).Round(time.Millisecond))
			}
		}
		p.mu.Unlock()
		p.view.Unlock()
		if !more {
			return
		}
		if last != "" {
			p.loaded(last)
		}
	}
}

// sendPackageLocked sends gopls the queued files of the first package in
// the queue and returns the last one sent; "" when none was. p.mu is held.
func (p *proxy) sendPackageLocked() string {
	o := &p.opening
	dir := o.queue[0]
	o.queue = o.queue[1:]
	paths := o.files[dir]
	delete(o.files, dir)
	last := ""
	for _, path := range paths {
		vf := p.virtual[path]
		if vf == nil || !vf.pending {
			continue
		}
		vf.pending = false
		if p.owned[path] {
			continue // the editor's buffer is gopls's
		}
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{
			"uri": pathToURI(path), "languageId": "go", "version": vf.version, "text": string(vf.gen)}}})
		last = path
		o.count++
	}
	if last != "" {
		o.pkgs++
	}
	close(o.sent)
	o.sent = make(chan struct{})
	return last
}

// loaded waits, at most openWait, until gopls has loaded the package of
// the generated file at path: gopls answers a request on the file once it
// has, and gopls.list_imports needs nothing more.
func (p *proxy) loaded(path string) {
	if _, ok := p.askWithin(openWait, "workspace/executeCommand", map[string]any{
		"command": "gopls.list_imports", "arguments": []any{map[string]any{"URI": pathToURI(path)}}}); !ok {
		p.logf("gopls hasn't loaded %s in %v; sending the next package", path, openWait)
	}
}

// prioritize moves the package of the generated file at path, after the
// packages it imports that are still to be sent, to the front of the
// queue. p.mu is held.
func (p *proxy) prioritize(path string) {
	o := &p.opening
	if len(o.queue) == 0 {
		return
	}
	queued := map[string]bool{}
	for _, d := range o.queue {
		queued[d] = true
	}
	var first []string
	seen := map[string]bool{}
	var visit func(dir string)
	visit = func(dir string) {
		if seen[dir] {
			return
		}
		seen[dir] = true
		for _, d := range o.deps[dir] {
			visit(d)
		}
		if queued[dir] {
			first = append(first, dir)
		}
	}
	visit(filepath.Dir(path))
	if len(first) == 0 {
		return
	}
	rest := slices.DeleteFunc(slices.Clone(o.queue), func(d string) bool { return seen[d] && queued[d] })
	o.queue = append(first, rest...)
}

// awaitOpen waits, at most limit, until gopls has been sent the generated
// file at path, putting its package first.
func (p *proxy) awaitOpen(path string, limit time.Duration) {
	deadline := time.After(limit)
	p.mu.Lock()
	p.prioritize(path)
	for {
		vf := p.virtual[path]
		if vf == nil || !vf.pending || p.stopped {
			p.mu.Unlock()
			return
		}
		sent := p.opening.sent
		p.mu.Unlock()
		select {
		case <-sent:
		case <-deadline:
			return
		}
		p.mu.Lock()
	}
}

// awaitAllOpen waits, at most limit, until gopls has been sent every
// generated file: for requests whose answers span packages.
func (p *proxy) awaitAllOpen(limit time.Duration) {
	deadline := time.After(limit)
	p.mu.Lock()
	for len(p.opening.queue) > 0 && !p.stopped {
		sent := p.opening.sent
		p.mu.Unlock()
		select {
		case <-sent:
		case <-deadline:
			return
		}
		p.mu.Lock()
	}
	p.mu.Unlock()
}

// spanning are the .vuka requests whose answers span the module's
// packages: they wait for gopls to have every generated file.
var spanning = map[string]bool{
	"textDocument/references":     true,
	"textDocument/rename":         true,
	"textDocument/implementation": true,
}
