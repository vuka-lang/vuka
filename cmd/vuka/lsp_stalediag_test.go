package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// captureWriter collects the bytes an rpcConn writes so a test can read the
// messages the proxy sent to the editor.
type captureWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// publishedDiags returns the diagnostics of the last publishDiagnostics for uri
// among the captured messages.
func (w *captureWriter) publishedDiags(t *testing.T, uri string) ([]any, bool) {
	t.Helper()
	w.mu.Lock()
	raw := w.buf.String()
	w.mu.Unlock()
	var found []any
	ok := false
	for {
		i := strings.Index(raw, "{")
		if i < 0 {
			break
		}
		dec := json.NewDecoder(strings.NewReader(raw[i:]))
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			raw = raw[i+1:]
			continue
		}
		raw = raw[i+int(dec.InputOffset()):]
		if m["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := m["params"].(map[string]any)
		if params == nil || params["uri"] != uri {
			continue
		}
		ds, _ := params["diagnostics"].([]any)
		found, ok = ds, true
	}
	return found, ok
}

// newProxyFixture builds a proxy with one generated .vuka file in gopls and the
// editor's buffer differing from the built text (the knobs let a test set
// whether the buffer is newer than the last settled build).
func newProxyFixture(t *testing.T, src string) (*proxy, *captureWriter, string, string) {
	t.Helper()
	res, err := transpile.Package([]transpile.File{{Name: "main.vuka", Src: []byte(src)}}, transpile.Options{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	out := res.Files[0]
	source := "/proj/main.vuka"
	gen := "/proj/main_vuka.go"
	cap := &captureWriter{}
	p := &proxy{
		editor:      newRPCConn(nil, cap),
		virtual:     map[string]*vfile{},
		bySource:    map[string]string{},
		bufs:        map[string][]byte{},
		changed:     map[string]int{},
		done:        map[string]int{},
		vukaDiags:   map[string]*diagSet{},
		syntaxDiags: map[string]*diagSet{},
		goDiags:     map[string]*diagSet{},
		shown:       map[string]string{},
		owned:       map[string]bool{},
	}
	vf := &vfile{source: source, from: []byte(src), gen: out.Src, m: out.Map, version: 2}
	p.virtual[gen] = vf
	p.bySource[source] = gen
	p.bufs[source] = []byte(src)
	return p, cap, source, gen
}

// storm is a gopls publishDiagnostics payload with errors spread over several
// lines of the generated Go.
func storm(uri string, version int, lines ...int) json.RawMessage {
	var ds []any
	for _, ln := range lines {
		ds = append(ds, map[string]any{
			"range":    map[string]any{"start": map[string]any{"line": ln, "character": 1}, "end": map[string]any{"line": ln, "character": 2}},
			"severity": 1.0,
			"message":  "undefined: x",
		})
	}
	b, _ := json.Marshal(map[string]any{"uri": uri, "version": version, "diagnostics": ds})
	return b
}

const stormSrc = `package main

import "fmt"

func a() int { return 1 }
func b() int { return 2 }
func c() int { return 3 }

func main() {
	fmt.Println(a() + b() + c())
}
`

// TestLSPStaleDiagnosticsSuppressed (symptom 2): while the editor buffer is
// newer than the last settled build, a gopls diagnostic storm on the
// (mid-edit) generated Go must not be published onto the current buffer.
func TestLSPStaleDiagnosticsSuppressed(t *testing.T) {
	p, cap, source, gen := newProxyFixture(t, stormSrc)
	genURI, srcURI := pathToURI(gen), pathToURI(source)

	// The buffer is newer than the last settled build (changed > done) — the
	// generated Go gopls holds is from a mid-edit snapshot.
	p.changed[source] = 7
	p.done[source] = 4

	p.goplsDiagnostics(storm(genURI, 2 /* == vf.version */, 2, 4, 6, 8))

	ds, published := cap.publishedDiags(t, srcURI)
	if published && len(ds) > 0 {
		t.Fatalf("stale gopls storm was published while the buffer was mid-edit: %d diagnostics", len(ds))
	}
}

// TestLSPFreshDiagnosticsPublished: once a build of the current buffer has
// settled (changed <= done), gopls's diagnostics are published as before.
func TestLSPFreshDiagnosticsPublished(t *testing.T) {
	p, cap, source, gen := newProxyFixture(t, stormSrc)
	genURI, srcURI := pathToURI(gen), pathToURI(source)

	p.changed[source] = 4
	p.done[source] = 4 // settled

	p.goplsDiagnostics(storm(genURI, 2, 4))

	ds, published := cap.publishedDiags(t, srcURI)
	if !published || len(ds) != 1 {
		t.Fatalf("a settled gopls diagnostic was not published: published=%v len=%d", published, len(ds))
	}
}

// TestLSPStaleVersionDiagnosticsDropped: gopls diagnostics tagged with a
// superseded generated-file version are dropped even when the build is settled.
func TestLSPStaleVersionDiagnosticsDropped(t *testing.T) {
	p, cap, source, gen := newProxyFixture(t, stormSrc)
	genURI, srcURI := pathToURI(gen), pathToURI(source)

	p.changed[source] = 4
	p.done[source] = 4

	p.goplsDiagnostics(storm(genURI, 1 /* != vf.version (2) */, 2, 4, 6))

	ds, published := cap.publishedDiags(t, srcURI)
	if published && len(ds) > 0 {
		t.Fatalf("a stale-version gopls storm was published: %d diagnostics", len(ds))
	}
}
