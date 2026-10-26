package main

import (
	"encoding/json"
	"sync"
	"time"
)

// Every request of the editor's gets one answer, in time. readEditor
// records each request as it reads it; whatever answers it — gopls, vuka's
// own handlers, a goroutine finishing a completion — is the answer, and the
// proxy answers in its place when nothing has:
//
//   - at goplsDeadline, a request the main loop hasn't reached yet (a build
//     or a wait for one holds the loop);
//   - at goplsDeadline+replyGrace after the loop reached it, any other;
//   - at once, a request the editor cancels ($/cancelRequest).
//
// An answer for a request the proxy answered already is dropped (dropReply),
// so the editor never gets two. The loop skips a request answered before it
// got there.

// replyGrace is how long a request the main loop has reached may take past
// goplsDeadline before the proxy answers it: a .vuka request's own deadline
// starts when it is sent to gopls, after its wait for a build.
var replyGrace = 10 * time.Second

// inflight is an editor request not answered yet.
type inflight struct {
	method  string
	timer   *time.Timer
	reached bool // the main loop has taken it
}

// replies is the proxy's record of the editor's requests.
type replies struct {
	mu       sync.Mutex
	inflight map[string]*inflight // editor request id → not answered yet
	answered map[string]time.Time // ids the proxy answered in gopls's or a handler's place: their late answers are dropped
}

// track records the editor's request m, read but not yet taken by the main
// loop, and answers it at goplsDeadline unless something has. initialize and
// shutdown are left to gopls, however long it takes.
func (p *proxy) track(m *rpcMsg) {
	if m.Method == "initialize" || m.Method == "shutdown" {
		return
	}
	id, method := string(m.ID), m.Method
	r := &p.replies
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		r.inflight, r.answered = map[string]*inflight{}, map[string]time.Time{}
	}
	f := &inflight{method: method}
	f.timer = time.AfterFunc(goplsDeadline, func() { p.late(id, f) })
	r.inflight[id] = f
}

// reach tells whether the main loop is to handle the editor's request id: it
// is, unless the proxy has answered it already.
func (p *proxy) reach(id json.RawMessage) bool {
	r := &p.replies
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.inflight[string(id)]; f != nil {
		f.reached = true
		f.timer.Reset(goplsDeadline + replyGrace)
		return true
	}
	if _, ok := r.answered[string(id)]; ok {
		delete(r.answered, string(id)) // nothing else answers it now
		return false
	}
	return true
}

// untrack leaves the request id to gopls: a request on a file of gopls's
// own, which gopls answers however long it takes, as it does under any
// editor.
func (p *proxy) untrack(id json.RawMessage) {
	r := &p.replies
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.inflight[string(id)]; f != nil {
		f.timer.Stop()
		delete(r.inflight, string(id))
	}
}

// late answers the request id in place of the answer it didn't get in time.
func (p *proxy) late(id string, f *inflight) {
	p.replies.mu.Lock()
	state := "not reached"
	if f.reached {
		state = "not answered"
	}
	p.replies.mu.Unlock()
	if p.settle(id, f) {
		p.logf("%s: %s in time; answered without it", f.method, state)
		p.answerFor(id, f.method, nil)
	}
}

// cancelled answers the request the editor cancels, at once.
func (p *proxy) cancelled(params json.RawMessage) {
	var cp struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(params, &cp) != nil || len(cp.ID) == 0 {
		return
	}
	id := string(cp.ID)
	p.replies.mu.Lock()
	f := p.replies.inflight[id]
	p.replies.mu.Unlock()
	if f != nil && p.settle(id, f) {
		pendingActions.Delete(id)
		p.answerFor(id, f.method, map[string]any{"code": -32800, "message": "cancelled"})
	}
}

// settle takes the request id, f, as answered by the proxy; false when it
// was answered already.
func (p *proxy) settle(id string, f *inflight) bool {
	r := &p.replies
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight[id] != f {
		return false
	}
	f.timer.Stop()
	delete(r.inflight, id)
	now := time.Now()
	for k, t := range r.answered {
		if now.Sub(t) > 10*time.Minute {
			delete(r.answered, k) // never answered after all
		}
	}
	r.answered[id] = now
	return true
}

// answerFor sends the proxy's answer to the request id: the error given, or
// else what the method answers when there is nothing to say.
func (p *proxy) answerFor(id, method string, err any) {
	reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	switch {
	case err != nil:
		reply["error"] = err
	case semanticTokensMethods[method]:
		reply["result"] = map[string]any{"data": []any{}}
	case quietMethods[method] || emptyMethods[method]:
		reply["result"] = nil
	default:
		reply["error"] = map[string]any{"code": -32803, "message": "vuka lsp didn't answer in time"}
	}
	b, _ := json.Marshal(reply)
	_ = p.editor.writeChecked(b)
}

// emptyMethods are requests whose empty answer says there is nothing: no
// edits (a save the proxy can't format in time goes on unformatted), no
// lenses.
var emptyMethods = map[string]bool{
	"textDocument/formatting":        true,
	"textDocument/rangeFormatting":   true,
	"textDocument/willSaveWaitUntil": true,
	"textDocument/onTypeFormatting":  true,
	"textDocument/codeLens":          true,
}

// dropReply is the editor connection's check of what it writes: an answer
// to a request the proxy answered already is dropped; any other answer
// settles its request.
func (p *proxy) dropReply(body []byte) bool {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(body, &m) != nil || m.Method != "" || len(m.ID) == 0 {
		return false
	}
	id := string(m.ID)
	r := &p.replies
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.inflight[id]; f != nil {
		f.timer.Stop()
		delete(r.inflight, id)
		return false
	}
	if _, ok := r.answered[id]; ok {
		delete(r.answered, id)
		return true
	}
	return false
}

// genLock guards the generated Go gopls has: a build changing it, an editor
// taking a generated file over, and probes, which put a scratch function
// into a file while they ask gopls about it. Lock goes before probes: a
// probe gives the lock up, its question dropped, as soon as Lock waits.
type genLock struct {
	mu     sync.Mutex
	ch     chan struct{} // holds a token while locked
	urgent int           // Lock calls waiting
	yield  chan struct{} // closed while urgent > 0
}

func (l *genLock) init() {
	if l.ch == nil {
		l.ch, l.yield = make(chan struct{}, 1), make(chan struct{})
	}
}

// Lock locks l, ahead of the probes waiting.
func (l *genLock) Lock() {
	l.mu.Lock()
	l.init()
	l.urgent++
	if l.urgent == 1 {
		close(l.yield)
	}
	ch := l.ch
	l.mu.Unlock()
	ch <- struct{}{}
	l.mu.Lock()
	l.urgent--
	if l.urgent == 0 {
		l.yield = make(chan struct{})
	}
	l.mu.Unlock()
}

func (l *genLock) Unlock() {
	l.mu.Lock()
	ch := l.ch
	l.mu.Unlock()
	<-ch
}

// lockProbe locks l for a probe, waiting at most d and never while a Lock
// waits; it returns a channel closed once one does, when the probe is to
// finish at once.
func (l *genLock) lockProbe(d time.Duration) (<-chan struct{}, bool) {
	l.mu.Lock()
	l.init()
	ch := l.ch
	busy := l.urgent > 0
	l.mu.Unlock()
	if busy {
		return nil, false
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case ch <- struct{}{}:
	case <-t.C:
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.urgent > 0 {
		<-ch
		return nil, false
	}
	return l.yield, true
}

// probeLockWait bounds how long a probe waits for another to finish; past
// it, the completion goes without it. probeAskLimit bounds the probe's
// question to gopls.
var (
	probeLockWait = time.Second
	probeAskLimit = 5 * time.Second
)
