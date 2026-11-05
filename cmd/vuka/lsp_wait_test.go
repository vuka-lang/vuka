package main

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const waitSource = "package main\n\nimport \"strconv\"\n\nfunc label(n int) string { return strconv.Itoa(n) }\n\nfunc main() {\n\tprintln(label(1))\n}\n"

// heldBuilds makes the builds after hold is set wait until release is closed,
// or the test ends.
func heldBuilds(t *testing.T) (hold *atomic.Bool, release func()) {
	hold = &atomic.Bool{}
	ch := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	after := load.AfterPackage
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		if hold.Load() {
			<-ch
		}
		return after(dir, overlay, gens)
	}
	t.Cleanup(func() { load.AfterPackage = after }) // once the proxy has stopped
	return hold, release
}

// waitModule opens main.vuka of a module with waitSource, once built, and
// holds the builds after it.
func waitModule(t *testing.T) (c *lspClient, uri string, edit func(string), release func()) {
	hold, release := heldBuilds(t)
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": waitSource})
	t.Cleanup(release) // before the proxy stops
	uri = pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": waitSource}})
	if h := hoverText(c, uri, waitSource, "label(1)"); !strings.Contains(h, "func label") {
		t.Fatalf("hover before the edit: %q", h)
	}
	hold.Store(true)
	version := 1
	edit = func(text string) {
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": text}}})
	}
	return c, uri, edit, release
}

// hoverText is the hover at the start of needle in src, as text.
func hoverText(c *lspClient, uri, src, needle string) string {
	c.t.Helper()
	m := c.callWithin(answerDeadline, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), strings.Index(src, needle))})
	return string(m.Result)
}

// TestLSPHoverUntouched: while the build of an edit is held up, a hover on
// code the edit didn't touch is answered at once, from the last build; one
// on the edit waits for the build, as long as awaitLimit.
func TestLSPHoverUntouched(t *testing.T) {
	defer func(a time.Duration) { awaitLimit = a }(awaitLimit)
	awaitLimit = 2 * time.Second
	c, uri, edit, _ := waitModule(t)
	text := strings.Replace(waitSource, "label(1)", "label(12)", 1)
	edit(text)

	start := time.Now()
	if h := hoverText(c, uri, text, "label(12)"); !strings.Contains(h, "func label") {
		t.Errorf("hover on the untouched name: %q", h)
	}
	if took := time.Since(start); took > awaitLimit/2 {
		t.Errorf("the hover on untouched code waited %v for the build", took)
	}
	start = time.Now()
	hoverText(c, uri, text, "12)")
	if took := time.Since(start); took < awaitLimit/2 {
		t.Errorf("the hover on the edit didn't wait for the build (%v)", took)
	}
}

// TestLSPWaitDoesntHoldLoop: a completion waiting for the build of its edit
// doesn't hold up what the editor sends after it: a change, and a hover on
// the changed text, are taken and answered meanwhile.
func TestLSPWaitDoesntHoldLoop(t *testing.T) {
	defer func(a time.Duration) { awaitLimit = a }(awaitLimit)
	awaitLimit = 3 * time.Second
	c, uri, edit, release := waitModule(t)
	text := strings.Replace(waitSource, "println(label(1))", "println(label(1))\n\tstrconv.", 1)
	edit(text)

	completed := make(chan time.Time, 1)
	at := map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(text), strings.Index(text, "strconv.\n")+len("strconv."))}
	go func() {
		c.callWithin(answerDeadline, "textDocument/completion", at)
		completed <- time.Now()
	}()
	time.Sleep(100 * time.Millisecond) // the completion waits
	text = "// Labels.\n" + text
	edit(text)
	if h := hoverText(c, uri, text, "label(1)"); !strings.Contains(h, "func label") {
		t.Errorf("hover on the changed text: %q", h)
	}
	hovered := time.Now()
	select {
	case at := <-completed:
		t.Errorf("the completion was answered %v before the hover after it", hovered.Sub(at))
	default:
	}
	release()
	select {
	case <-completed:
	case <-time.After(answerDeadline):
		t.Fatalf("the completion wasn't answered\n%s", c.log.String())
	}
}
