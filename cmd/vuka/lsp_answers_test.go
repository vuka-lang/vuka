package main

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// editorPipe is a proxy's editor connection whose writes the test reads.
func editorPipe(t *testing.T) (*rpcConn, <-chan rpcMsg) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close(); pw.Close() })
	out := make(chan rpcMsg, 100)
	go func() {
		r := newRPCConn(pr, nil)
		for {
			body, err := r.read()
			if err != nil {
				return
			}
			var m rpcMsg
			_ = json.Unmarshal(body, &m)
			out <- m
		}
	}()
	inR, _ := io.Pipe()
	return newRPCConn(inR, pw), out
}

// deafGopls is a gopls connection that takes everything and answers
// nothing.
func deafGopls() *rpcConn {
	r, _ := io.Pipe()
	return newRPCConn(r, io.Discard).queued()
}

// TestProbeYieldsToBuild: a completion's probes, waiting on a gopls that
// doesn't answer, give the generated Go up at once to a build that needs
// it; before, each held it for its whole question, and a build behind a
// few of them waited for minutes while requests waited for the build.
func TestProbeYieldsToBuild(t *testing.T) {
	editor, _ := editorPipe(t)
	p := newProxy(editor, deafGopls(), io.Discard, t.TempDir())
	gen := filepath.Join(t.TempDir(), "page_vuka.go")
	p.virtual[gen] = &vfile{source: gen + ".vuka", gen: []byte("package pages\n"), version: 1}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); p.probe(gen, "x.", "") }()
	}
	waitFor(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.asks) > 0 // a probe holds the lock and asks gopls
	})
	start := time.Now()
	p.genMu.Lock() // a build's apply
	took := time.Since(start)
	if took > time.Second {
		t.Errorf("the build waited %v for the probes", took)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * probeLockWait):
		t.Fatal("the probes didn't give up")
	}
	p.genMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.asks) != 0 || len(p.dropped) == 0 {
		t.Errorf("asks %v, dropped %v: the probe's question to gopls should be withdrawn", p.asks, p.dropped)
	}
	if vf := p.virtual[gen]; vf.version != 3 {
		t.Errorf("version %d: one probe should have run (and put the file back)", vf.version)
	}
}

// TestReplyAtDeadline: a request the main loop took and nothing answered
// is answered at goplsDeadline+replyGrace, and the answer that comes after
// is dropped: the editor gets exactly one.
func TestReplyAtDeadline(t *testing.T) {
	defer func(d, g time.Duration) { goplsDeadline, replyGrace = d, g }(goplsDeadline, replyGrace)
	goplsDeadline, replyGrace = 200*time.Millisecond, 200*time.Millisecond
	editor, out := editorPipe(t)
	p := newProxy(editor, deafGopls(), io.Discard, t.TempDir())
	for _, c := range []struct{ id, method, want string }{
		{"1", "textDocument/hover", `"result":null`},
		{"2", "textDocument/formatting", `"result":null`},
		{"3", "textDocument/semanticTokens/full", `"data":[]`},
		{"4", "textDocument/rename", `-32803`},
	} {
		m := &rpcMsg{ID: json.RawMessage(c.id), Method: c.method}
		start := time.Now()
		p.track(m)
		if !p.reach(m.ID) {
			t.Fatalf("%s: not to be handled", c.method)
		}
		var got rpcMsg
		select {
		case got = <-out:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no answer", c.method)
		}
		if took := time.Since(start); took < goplsDeadline+replyGrace {
			t.Errorf("%s: answered after %v, before its deadline", c.method, took)
		}
		b, _ := json.Marshal(got)
		if string(got.ID) != c.id || !strings.Contains(strings.ReplaceAll(string(b), " ", ""), c.want) {
			t.Errorf("%s: answer %s, want %s", c.method, b, c.want)
		}
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(c.id), "result": "late"})
		select {
		case m := <-out:
			t.Errorf("%s: a second answer: %s %s", c.method, m.ID, m.Result)
		case <-time.After(100 * time.Millisecond):
		}
	}
	// An answer in time goes through, and the deadline doesn't answer again.
	m := &rpcMsg{ID: json.RawMessage("5"), Method: "textDocument/hover"}
	p.track(m)
	p.reach(m.ID)
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": "in time"})
	if got := <-out; string(got.Result) != `"in time"` {
		t.Errorf("answer %s", got.Result)
	}
	select {
	case m := <-out:
		t.Errorf("a second answer: %s %s", m.ID, m.Result)
	case <-time.After(goplsDeadline + replyGrace + 200*time.Millisecond):
	}
}

// TestLSPCancelAnsweredAtOnce: a request the editor cancels while the main
// loop is busy (here with the first build) is answered at once, and once.
func TestLSPCancelAnsweredAtOnce(t *testing.T) {
	muteGoplsLog(t)
	defer func(d time.Duration) { goplsDeadline = d }(goplsDeadline)
	goplsDeadline = 5 * time.Second
	release := make(chan struct{})
	after := load.AfterPackage
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		<-release
		return after(dir, overlay, gens)
	}
	t.Cleanup(func() { load.AfterPackage = after }) // once the proxy has stopped

	app := routesModule(t, map[string]string{"pages/card.vuka.html": deadlineCard})
	c := startMutedLSP(t, app)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) }) // before it stops
	uri := pathToURI(filepath.Join(app, "pages", "card.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": deadlineCard}})
	c.mu.Lock()
	c.next++
	id, _ := json.Marshal(c.next)
	ch := make(chan rpcMsg, 4)
	c.resps[string(id)] = ch
	c.mu.Unlock()
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "textDocument/hover", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(deadlineCard), strings.Index(deadlineCard, "Title}")+1)}})
	start := time.Now()
	c.notify("$/cancelRequest", map[string]any{"id": json.RawMessage(id)})
	select {
	case m := <-ch:
		if !strings.Contains(string(m.Error), "-32800") {
			t.Errorf("cancelled hover: result %s, error %s; want RequestCancelled", m.Result, m.Error)
		}
		if took := time.Since(start); took > goplsDeadline/2 {
			t.Errorf("cancelled hover answered after %v", took)
		}
	case <-time.After(answerDeadline):
		t.Fatal("cancelled hover: no answer")
	}
	once.Do(func() { close(release) })
	// The loop, free again, skips the hover; a later request is answered.
	c.callWithin(answerDeadline, "textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}})
	if n := len(ch); n != 0 {
		t.Errorf("the cancelled hover got %d more answers", n)
	}
}

// TestLSPRequestsDontWaitTwice: while the build of an edit is held up, the
// first request on the file waits awaitLimit for it and the rest answer
// from the last build: a burst of requests and then the formatting of a
// save don't queue awaitLimit after awaitLimit on the main loop, which
// showed as a hover stuck on Loading and a save formatting for long.
func TestLSPRequestsDontWaitTwice(t *testing.T) {
	muteGoplsLog(t)
	defer func(d, a time.Duration) { goplsDeadline, awaitLimit = d, a }(goplsDeadline, awaitLimit)
	goplsDeadline, awaitLimit = 10*time.Second, time.Second
	var hold atomic.Bool
	release := make(chan struct{})
	after := load.AfterPackage
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		if hold.Load() {
			<-release
		}
		return after(dir, overlay, gens)
	}
	t.Cleanup(func() { load.AfterPackage = after })

	app := routesModule(t, map[string]string{"pages/card.vuka.html": deadlineCard})
	c := startMutedLSP(t, app)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	uri := pathToURI(filepath.Join(app, "pages", "card.vuka.html"))
	doc := map[string]any{"uri": uri}
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": deadlineCard}})
	c.callWithin(answerDeadline, "textDocument/foldingRange", map[string]any{"textDocument": doc}) // built

	hold.Store(true) // the next build doesn't finish
	text := strings.Replace(deadlineCard, "<h2>", "<h2 >", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": text}}})
	at := map[string]any{"textDocument": doc, "position": positionOf([]byte(text), strings.Index(text, "Title}")+1)}

	const burst = 8
	var wg sync.WaitGroup
	for range burst {
		wg.Add(1)
		go func() { defer wg.Done(); c.callWithin(answerDeadline, "textDocument/hover", at) }()
		time.Sleep(10 * time.Millisecond) // in order
	}
	start := time.Now()
	m := c.callWithin(answerDeadline, "textDocument/formatting", map[string]any{"textDocument": doc,
		"options": map[string]any{"tabSize": 2, "insertSpaces": true}})
	took := time.Since(start)
	if len(m.Error) > 0 {
		t.Errorf("formatting: %s", m.Error)
	}
	if took > burst*awaitLimit/2 {
		t.Errorf("formatting answered after %v, behind requests each waiting for the build", took)
	}
	wg.Wait()
	once.Do(func() { close(release) })
}
