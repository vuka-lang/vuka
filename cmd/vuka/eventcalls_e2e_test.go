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

const eventsPicker = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Picker is a select passing the tag's other attributes on to it.
type Picker struct {
	Name  string
	Attrs []ui.Attr
}
</script>

<select id="picker" name={Name} {Attrs...}>
  <option value="">-</option>
  <option value="open">open</option>
  <option value="shipped">shipped</option>
</select>
`

const eventsIndex = `<script lang="go">
package pages

import (
	"context"
	"strconv"

	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

// Index is a live page whose events call its methods with the elements'
// reads.
type Index struct {
	ui.Live
	status string
	items  []int
	done   map[int]bool
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.items, p.done = []int{1, 2, 3}, map[int]bool{}
	return nil
}

func (p *Index) Choose(s string)      { p.status = s }
func (p *Index) Mark(id int, on bool) { p.done[id] = on }

func (p *Index) Remove(id int) {
	for i, it := range p.items {
		if it == id {
			p.items = append(p.items[:i:i], p.items[i+1:]...)
		}
	}
}

func (p *Index) state(id int) string { return strconv.FormatBool(p.done[id]) }
</script>

<main>
  <p id="status">status {status}</p>
  <components.Picker name="status" onChange={Choose(this.value)} />
  <select id="direct" onChange={Choose(this.value)}>
    <option value="">-</option>
    <option value="paid">paid</option>
  </select>
  <ul>
    for _, id := range items {
      <li key={id} id="row-{strconv.Itoa(id)}">
        <input type="checkbox" class="mark" onChange={Mark(id, this.checked)} />
        <span class="done">{state(id)}</span>
        <button class="rm" onClick={Remove(id)}>remove</button>
      </li>
    }
  </ul>
</main>
`

const eventsAbout = `<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

// About isn't live: its event's call is a push over HTTP.
type About struct{}

func (p *About) Greet(name string) (ui.Node, error) { return ui.Text("hi " + name), nil }
</script>

<main>
  <button id="greet" data-name="ann" onClick={Greet(this.dataName)}>greet</button>
</main>
`

// TestEventCallsE2E: an event's call of a page's method is a push made when
// the event fires, with the element's reads: from an element, from a
// component's tag passing it on to the element it spreads its attributes on,
// in a loop, and over HTTP on a page that isn't live (vuka#353).
func TestEventCallsE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain,
		"components/picker.vuka.html": eventsPicker,
		"pages/index.vuka.html":       eventsIndex,
		"pages/about.vuka.html":       eventsAbout,
	})
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
	p.do("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.__errs = [];
for (const k of ["error", "warn"]) { const f = console[k]; console[k] = (...a) => { window.__errs.push(a.map(String).join(" ")); f.apply(console, a) } }
addEventListener("error", (e) => window.__errs.push("error " + e.message));`})
	p.goTo(base + "/")
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	pick := func(sel, value string) {
		p.eval(fmt.Sprintf(`(() => { const s = document.querySelector(%q); s.value = %q; s.dispatchEvent(new Event("change", { bubbles: true })) })()`, sel, value))
	}

	pick("#picker", "shipped")
	p.wait(`document.querySelector("#status").textContent === "status shipped"`)
	pick("#direct", "paid")
	p.wait(`document.querySelector("#status").textContent === "status paid"`)
	p.click("#row-2 .mark")
	p.wait(`document.querySelector("#row-2 .done").textContent === "true"`)
	p.click("#row-2 .mark")
	p.wait(`document.querySelector("#row-2 .done").textContent === "false"`)
	p.click("#row-1 .rm")
	p.wait(`!document.querySelector("#row-1") && !!document.querySelector("#row-3")`)

	p.goTo(base + "/about")
	p.wait(`!!document.querySelector("#greet")`)
	p.click("#greet")
	p.wait(`document.querySelector("#greet").textContent === "hi ann"`)

	if errs, _ := p.eval(`window.__errs`).([]any); len(errs) > 0 {
		t.Errorf("console: %v", errs)
	}
	if t.Failed() {
		t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
	}
}
