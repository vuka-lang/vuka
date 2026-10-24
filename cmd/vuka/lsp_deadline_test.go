package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// answerDeadline is how long an editor request may wait for the proxy's
// answer: a hang fails the test rather than the editor.
const answerDeadline = 30 * time.Second

// callWithin is c.call that fails the test when the proxy hasn't answered
// within d, and hands back an error answer rather than failing on it.
func (c *lspClient) callWithin(d time.Duration, method string, params any) rpcMsg {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id, _ := json.Marshal(c.next)
	ch := make(chan rpcMsg, 1)
	c.resps[string(id)] = ch
	c.mu.Unlock()
	start := time.Now()
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	select {
	case m := <-ch:
		c.t.Logf("%s answered in %v", method, time.Since(start).Round(time.Millisecond))
		return m
	case <-time.After(d):
		c.t.Fatalf("%s: no answer in %v", method, d)
		return rpcMsg{}
	}
}

const deadlineCard = `<script lang="go">
package pages

import "strconv"

// Card is a page declared in the script form.
type Card struct {
	Title string
	Count int
}
</script>

<article>
  <h2>{Title}</h2>
  <p>{strconv.Itoa(Count)}</p>
</article>
`

const deadlineList = `package kit

// List is a generic shape.
type List[T any] struct {
	Items []T
	Empty string
}
<ul>
  for _, item := range Items {
    <li>{item}</li>
  }
  if len(Items) == 0 {
    <li>{Empty}</li>
  }
</ul>
`

const deadlineBrand = `package site

import "context"

// Vars are Context's variables.
type Vars struct{ Brand string }

// Context is a context processor.
func Context(ctx context.Context) (Vars, error) { return Vars{Brand: "pets"}, nil }
`

const deadlineFooter = `package pages

// Footer reads a context processor's variable by name.
type Footer struct{}
<footer>{brand}</footer>
`

const deadlineMain = `package main

import (
	"github.com/vuka-lang/web/v2"

	"example.com/app/pages"
	"example.com/app/site"
)

func main() {
	web.New(pages.Routes(), web.ContextProcessors(site.Context)).Get("/health", health)
}

func health() string { return "ok" }
`

// TestLSPAnswersShapes: on a folder-route app, with and without context
// processors, each kind of shape — a page in the script form, a generic
// shape, one reading a processor's variable — gets its semantic tokens,
// hover, completion and definition answered in time.
func TestLSPAnswersShapes(t *testing.T) {
	for _, processors := range []bool{false, true} {
		name := "without processors"
		if processors {
			name = "with processors"
		}
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				"pages/card.vuka.html": deadlineCard,
				"kit/list.vuka.html":   deadlineList,
			}
			if processors {
				files["main.go"] = deadlineMain
				files["site/site.go"] = deadlineBrand
				files["pages/footer.vuka.html"] = deadlineFooter
			}
			app := routesModule(t, files)
			c := startLSPAt(t, app, lspOptions{renameCommands: true})
			for name, src := range files {
				if !strings.HasSuffix(name, ".vuka.html") {
					continue
				}
				uri := pathToURI(filepath.Join(app, name))
				c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
					"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
				doc := map[string]any{"uri": uri}
				at := func(needle string) map[string]any {
					i := strings.Index(src, needle)
					if i < 0 {
						t.Fatalf("%s has no %q", name, needle)
					}
					return map[string]any{"textDocument": doc, "position": positionOf([]byte(src), i+1)}
				}
				hole := map[string]string{"pages/card.vuka.html": "Title}", "kit/list.vuka.html": "Items {", "pages/footer.vuka.html": "brand}"}[name]
				m := c.callWithin(answerDeadline, "textDocument/semanticTokens/full", map[string]any{"textDocument": doc})
				if len(m.Error) > 0 {
					t.Errorf("%s: semantic tokens: %s", name, m.Error)
				} else if toks := decodeTokens(t, mustMap(t, m.Result)); len(toks) == 0 {
					t.Errorf("%s: no semantic tokens", name)
				}
				for _, method := range []string{"textDocument/hover", "textDocument/completion", "textDocument/definition"} {
					m := c.callWithin(answerDeadline, method, at(hole))
					if len(m.Error) > 0 {
						t.Errorf("%s: %s: %s", name, method, m.Error)
					}
					if method == "textDocument/hover" && name == "pages/card.vuka.html" && !strings.Contains(string(m.Result), "Title") {
						t.Errorf("%s: hover on Title: %s", name, m.Result)
					}
				}
			}
		})
	}
}

func mustMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		t.Fatalf("not an object: %s", raw)
	}
	return v
}

// The test binary is the gopls of TestLSPGoplsUnanswered when this is set:
// it answers initialize, shutdown and the proxy's gopls.list_imports, and
// nothing else.
const muteGoplsEnv = "VUKA_TEST_MUTE_GOPLS"

func init() {
	if log := os.Getenv(muteGoplsEnv); log != "" {
		muteGopls(os.Stdin, os.Stdout, log)
		os.Exit(0)
	}
}

// muteGopls reads LSP messages from in, answers initialize, shutdown and
// gopls.list_imports on out, and appends the method of every other message
// to the file log, with the document of a didOpen, didChange or didClose
// and the command of an executeCommand.
func muteGopls(in io.Reader, out io.Writer, log string) {
	conn := newRPCConn(in, out)
	f, _ := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer f.Close()
	for {
		body, err := conn.read()
		if err != nil {
			return
		}
		var m rpcMsg
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch m.Method {
		case "initialize":
			_ = conn.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"capabilities": map[string]any{
				"hoverProvider": true, "definitionProvider": true, "completionProvider": map[string]any{},
				"semanticTokensProvider": map[string]any{"legend": map[string]any{"tokenTypes": []any{"variable"}, "tokenModifiers": []any{}}, "full": true},
			}}})
		case "shutdown":
			_ = conn.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
		case "workspace/executeCommand":
			var cmd struct{ Command string }
			_ = json.Unmarshal(m.Params, &cmd)
			f.WriteString(m.Method + " " + cmd.Command + "\n")
			if cmd.Command == "gopls.list_imports" {
				_ = conn.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{}})
			}
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
			var doc struct{ TextDocument struct{ URI string } }
			_ = json.Unmarshal(m.Params, &doc)
			f.WriteString(m.Method + " " + doc.TextDocument.URI + "\n")
		case "exit":
			return
		default:
			line := m.Method
			if m.Method == "$/cancelRequest" {
				line += " " + string(m.Params)
			} else if len(m.ID) > 0 {
				line += " id=" + string(m.ID)
			}
			f.WriteString(line + "\n")
		}
	}
}

// TestLSPGoplsUnanswered: when gopls never answers, a .vuka request is
// still answered at the deadline — the markup's own tokens, no hover —
// gopls is told to cancel it, and the editor's own $/cancelRequest of a
// .vuka request is answered and passed on to gopls by the proxy's id.
func TestLSPGoplsUnanswered(t *testing.T) {
	log := muteGoplsLog(t)
	defer func(d time.Duration) { goplsDeadline = d }(goplsDeadline)
	goplsDeadline = 2 * time.Second

	app := routesModule(t, map[string]string{"pages/card.vuka.html": deadlineCard})
	c := startMutedLSP(t, app)
	uri := pathToURI(filepath.Join(app, "pages", "card.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": deadlineCard}})
	doc := map[string]any{"uri": uri}
	at := map[string]any{"textDocument": doc, "position": positionOf([]byte(deadlineCard), strings.Index(deadlineCard, "Title}")+1)}

	start := time.Now()
	m := c.callWithin(answerDeadline, "textDocument/semanticTokens/full", map[string]any{"textDocument": doc})
	if len(m.Error) > 0 {
		t.Errorf("semantic tokens: %s", m.Error)
	} else if _, ok := mustMap(t, m.Result)["data"]; !ok {
		t.Errorf("semantic tokens: %s", m.Result)
	}
	if took := time.Since(start); took < goplsDeadline {
		t.Errorf("answered in %v, before the deadline: gopls can't have", took)
	}
	if m := c.callWithin(answerDeadline, "textDocument/hover", at); len(m.Error) > 0 || string(m.Result) != "null" && len(m.Result) > 0 {
		t.Errorf("hover: result %s, error %s; want null", m.Result, m.Error)
	}

	// The editor cancels a hover before the deadline.
	goplsDeadline = time.Minute
	c.mu.Lock()
	c.next++
	id, _ := json.Marshal(c.next)
	ch := make(chan rpcMsg, 1)
	c.resps[string(id)] = ch
	c.mu.Unlock()
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "textDocument/hover", "params": at})
	waitFor(t, func() bool { return strings.Count(logText(log), "textDocument/hover") >= 2 })
	c.notify("$/cancelRequest", map[string]any{"id": json.RawMessage(id)})
	select {
	case m := <-ch:
		if !strings.Contains(string(m.Error), "-32800") {
			t.Errorf("cancelled hover: result %s, error %s; want RequestCancelled", m.Result, m.Error)
		}
	case <-time.After(answerDeadline):
		t.Fatal("cancelled hover: no answer")
	}
	waitFor(t, func() bool { return strings.Count(logText(log), "$/cancelRequest") >= 3 })
	got := logText(log)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "$/cancelRequest") && !strings.Contains(line, `"vuka-`) {
			t.Errorf("gopls was asked to cancel an id it doesn't know: %s", line)
		}
	}
}

func logText(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// muteGoplsLog makes the proxies the test starts run the mute gopls, and
// returns the file it logs to.
func muteGoplsLog(t *testing.T) string {
	log := filepath.Join(t.TempDir(), "gopls.log")
	t.Setenv(muteGoplsEnv, log)
	return log
}

// startMutedLSP runs the proxy on the module at dir with the mute gopls,
// and initializes it.
func startMutedLSP(t *testing.T, app string) *lspClient {
	return startMutedLSPLog(t, app, io.Discard)
}

// startMutedLSPLog is startMutedLSP, the proxy's log written to logw.
func startMutedLSPLog(t *testing.T, app string, logw io.Writer) *lspClient {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toProxyR, toProxyW := io.Pipe()
	fromProxyR, fromProxyW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = runLSP(ctx, toProxyR, fromProxyW, logw, exe, lspOptions{})
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		toProxyW.Close()
		fromProxyR.Close()
		<-done
	})
	c := &lspClient{t: t, conn: newRPCConn(fromProxyR, toProxyW), resps: map[string]chan rpcMsg{}, diags: make(chan map[string]any, 100)}
	go c.loop()
	c.init = c.call("initialize", map[string]any{"rootUri": pathToURI(app), "capabilities": map[string]any{}})
	c.notify("initialized", map[string]any{})
	return c
}

// TestLSPAnswersDuringFirstBuild: a request sent while the first build
// runs — which the proxy finishes before anything else — is answered at
// the deadline, not when the build ends.
func TestLSPAnswersDuringFirstBuild(t *testing.T) {
	muteGoplsLog(t)
	defer func(d time.Duration) { goplsDeadline = d }(goplsDeadline)
	goplsDeadline = 2 * time.Second
	release := make(chan struct{})
	after := load.AfterPackage
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		<-release // a build slowed down by a busy disk
		return after(dir, overlay, gens)
	}
	defer func() { load.AfterPackage = after }()
	defer close(release)

	app := routesModule(t, map[string]string{"pages/card.vuka.html": deadlineCard})
	c := startMutedLSP(t, app)
	uri := pathToURI(filepath.Join(app, "pages", "card.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": deadlineCard}})
	m := c.callWithin(answerDeadline, "textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}})
	if len(m.Error) > 0 {
		t.Errorf("semantic tokens: %s", m.Error)
	}
	m = c.callWithin(answerDeadline, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(deadlineCard), strings.Index(deadlineCard, "Title}")+1)})
	if len(m.Error) > 0 {
		t.Errorf("hover: %s", m.Error)
	}
}
