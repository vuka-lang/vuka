package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const modsIndex = `<script lang="go">
package pages

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Index counts what its modified events send.
type Index struct {
	ui.Live
	Q        string @ui.Bind
	searches int
	last     string
	added    []string
	sent     string
	starts   int
	inner    int
	ticked   int
}

func (p *Index) Search(q string)  { p.searches++; p.last = q }
func (p *Index) Add(s string)     { p.added = append(p.added, s) }
func (p *Index) Send(s string)    { p.sent = s }
func (p *Index) Start()           { p.starts++ }
func (p *Index) Inner()           { p.inner++ }
func (p *Index) Tick()            { p.ticked++ }
func (p *Index) count(n int) string { return strconv.Itoa(n) }
</script>

<main>
  <input id="q" onInput.debounce.200ms={Search(this.value)} />
  <p id="searches">{count(searches)} {last}</p>
  <input id="add" onKeydown.enter={Add(this.value)} />
  <p id="added">{len(added)}</p>
  <textarea id="msg" onKeydown.ctrl.enter={Send(this.value)}></textarea>
  <p id="sent">{sent}</p>
  <button id="start" onClick.once={Start()}>start</button>
  <p id="starts">{count(starts)}</p>
  <div id="outer"><button id="inner" onClick.stop={Inner()}>inner</button></div>
  <p id="inners">{count(inner)}</p>
  <input id="tick" type="checkbox" onClick.prevent={Tick()} />
  <p id="ticked">{count(ticked)}</p>
  <input id="bound" bind:value.debounce.100ms={Q} />
  <p id="bq">{Q}</p>
</main>
`

// TestEventModifiersE2E: an event's modifiers in Chrome — a debounce, key
// filters, once, stop, prevent, and a paced bind:value (vuka#354).
func TestEventModifiersE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain, "pages/index.vuka.html": modsIndex})
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), assetsEnv+"=")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/"); return err == nil && res.Body.Close() == nil })

	ch := launchChrome(t)
	p := ch.open("about:blank", 900, 700)
	p.do("Page.enable", map[string]any{})
	p.goTo(base + "/")
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	key := func(sel, k string, ctrl bool) {
		p.eval(fmt.Sprintf(`document.querySelector(%q).dispatchEvent(new KeyboardEvent("keydown", { key: %q, ctrlKey: %v, bubbles: true }))`, sel, k, ctrl))
	}

	for _, v := range []string{"t", "te", "tea"} {
		p.fill("#q", v)
	}
	p.wait(`document.querySelector("#searches").textContent === "1 tea"`)

	p.fill("#add", "milk")
	key("#add", "a", false)
	key("#add", "Enter", false)
	p.wait(`document.querySelector("#added").textContent === "1"`)

	p.eval(`document.querySelector("#msg").value = "hi"`)
	key("#msg", "Enter", false)
	key("#msg", "Enter", true)
	p.wait(`document.querySelector("#sent").textContent === "hi"`)

	p.click("#start")
	p.wait(`document.querySelector("#starts").textContent === "1"`)
	p.click("#start")
	p.eval(`window.__outer = 0; document.querySelector("#outer").addEventListener("click", () => window.__outer++)`)
	p.click("#inner")
	p.wait(`document.querySelector("#inners").textContent === "1"`)
	if n := p.eval(`window.__outer`); n != float64(0) {
		t.Errorf("the click reached the element around: %v", n)
	}
	if n := p.text("#starts"); n != "1" {
		t.Errorf("once ran %s times", n)
	}

	p.click("#tick")
	p.wait(`document.querySelector("#ticked").textContent === "1"`)
	if p.eval(`document.querySelector("#tick").checked`) != false {
		t.Errorf("prevent left the box ticked")
	}

	p.fill("#bound", "x")
	p.fill("#bound", "xy")
	p.wait(`document.querySelector("#bq").textContent === "xy"`)

	if t.Failed() {
		t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
	}
}
