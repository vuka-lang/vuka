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

// clientStateApp builds the app of files (pages and components, main.go
// pagesMain), serves it and opens its page at path in Chrome, the console's
// errors and warnings kept in window.__errs; done reports what the page
// logged and its markup when the test failed.
func clientStateApp(t *testing.T, files map[string]string, path string) (p *tab, base string, done func()) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	files["main.go"] = pagesMain
	root := assetsModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
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
	base = fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + path); return err == nil && res.Body.Close() == nil })
	ch := launchChrome(t)
	p = ch.open("about:blank", 900, 700)
	p.do("Page.enable", map[string]any{})
	p.do("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.__errs = [];
for (const k of ["error", "warn"]) { const f = console[k]; console[k] = (...a) => { window.__errs.push(a.map(String).join(" ")); f.apply(console, a) } }
addEventListener("error", (e) => window.__errs.push("error " + e.message));`})
	p.goTo(base + path)
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	return p, base, func() {
		if t.Failed() {
			t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
		}
	}
}

const derivedPage = `<script lang="go">
package pages

import (
	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

// Index derives a total from two states, and a flag from the total.
type Index struct {
	ui.Live
	count ui.State[int]
	price ui.State[int]
	total ui.Derived[int]  @ui.From(count * price)
	over  ui.Derived[bool] @ui.From(total > 100)
	saved int
}

// Save is a server event: the page renders again.
func (p *Index) Save() { p.saved++ }
</script>

<main>
  <button id="more" onClick={count++}>more</button>
  <button id="dear" onClick={price = 60}>dear</button>
  <button id="save" onClick={Save}>save</button>
  <p id="total" class:over={over}>{total}</p>
  <p id="plus" title={total + 1}>{count} at {price}</p>
  <ui.Show when={over}><b id="warn">over</b></ui.Show>
  <p id="saved">{saved}</p>
  <components.Line />
</main>
`

const derivedLine = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Line is a static component deriving from its own states.
type Line struct {
	n     ui.State[int]
	twice ui.Derived[int] @ui.From(n * 2)
}
</script>

<p><button id="line" onClick={n++}>+</button><span id="twice">{twice}</span></p>

<script lang="ts">
import { component, state } from 'vuka:component'

export default component<Line>((el) => {
  const twice = state('twice')
  twice.watch((v) => { el.dataset.seen = String(v) })
})
</script>
`

// TestDerivedE2E: a Derived renders with the states' initial values,
// follows them in the browser in a chain, survives the page rendering
// again, and a script reads and watches one.
func TestDerivedE2E(t *testing.T) {
	p, _, done := clientStateApp(t, map[string]string{"pages/index.vuka.html": derivedPage, "components/line.vuka.html": derivedLine}, "/")
	defer done()
	if got := p.text("#total"); got != "0" {
		t.Errorf("the total as rendered: %q", got)
	}
	p.click("#dear")
	p.click("#more")
	p.wait(`document.querySelector("#total").textContent === "60" && document.querySelector("#plus").title === "61"`)
	if p.eval(`document.querySelector("#total").classList.contains("over")`).(bool) {
		t.Error("60 is over 100")
	}
	p.click("#more")
	p.wait(`document.querySelector("#total").textContent === "120" && document.querySelector("#total").classList.contains("over") && !!document.querySelector("#warn").offsetParent`)
	p.click("#save")
	p.wait(`document.querySelector("#saved").textContent === "1"`)
	if got := p.text("#total"); got != "120" {
		t.Errorf("the total after the page rendered again: %q", got)
	}
	p.click("#line")
	p.click("#line")
	p.wait(`document.querySelector("#twice").textContent === "4" && document.querySelector("#twice").closest("p").dataset.seen === "4"`)
	if errs, _ := p.eval(`window.__errs`).([]any); len(errs) > 0 {
		t.Errorf("console: %v", errs)
	}
}
