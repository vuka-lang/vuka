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
	"strings"
	"testing"
	"time"
)

const pageScriptPage = `<script lang="go">
package pages

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

// Index is a live page with a script: the component API on a page. Its
// Update keeps it mounted on a patch, so the script's update runs.
type Index struct {
	ui.Live
	Q    string @ui.URL("q")
	n    int
	open ui.State[bool]
}

func (p *Index) Update(web.URLParams) {}

// Bump counts, and tells the script.
func (p *Index) Bump() {
	p.n++
	p.PushEvent("bumped", map[string]int{"n": p.n})
}
</script>

<main>
  <p id="n">{n}</p>
  <p id="q">{Q}</p>
  <input ref="box" id="box" />
  <button id="tog" onClick={open = !open}>toggle</button>
  <p id="flag" hidden={!open}>open</p>
</main>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Index>((el, props, ctx) => {
  const w = window as any
  w.__props = props
  w.__ctx = ctx
  w.__updates = []
  w.__mounts = (w.__mounts || 0) + 1
  w.__bump = () => ctx.live.bump()
  ctx.on('bumped', (p) => { w.__bumped = p.n; ctx.ref('box')!.focus() })
  ctx.state('open').watch((v) => { w.__open = v })
  return {
    update(next) { w.__updates.push(next.Q) },
    destroy() { w.__destroyed = true },
  }
})
</script>
`

const pageScriptOld = `<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

// Old is a live page whose script is page<T> with two arguments.
type Old struct {
	ui.Live
	Q string @ui.Bind
}

func (p *Old) Shout() { p.Q += "!" }
</script>

<main><p id="q">{Q}</p></main>

<script lang="ts">
import { page } from 'vuka:component'
export default page<Old>((el, live) => {
  // two arguments, as before page<T> took ctx
  const w = window as any
  w.__live = live
  w.__q = live.q.value
  w.__shout = () => live.shout()
})
</script>
`

// TestPageScriptE2E: a page's script is a component script: component<T>
// gets its props, ctx.state, ctx.ref, ctx.on (PushEvent), ctx.live and
// ctx.patch, update(next) on a URL patch; page<T> with two arguments still
// works.
func TestPageScriptE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain,
		"pages/index.vuka.html":  pageScriptPage,
		"pages/old.vuka.html":    pageScriptOld,
		"pages/alerts.vuka.html": scriptRefPage,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	if dts, _ := os.ReadFile(filepath.Join(root, ".vuka", componentDTSName)); strings.Count(string(dts), "readonly __page?: true;") != 3 {
		t.Errorf("Index, Old and Alerts aren't pages in the types:\n%s", dts)
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
	for i := 0; ; i++ {
		if r, err := http.Get(base + "/"); err == nil {
			r.Body.Close()
			break
		}
		if i > 100 {
			t.Fatalf("app didn't start:\n%s", logs.Bytes())
		}
		time.Sleep(50 * time.Millisecond)
	}
	c := launchChrome(t)
	p := c.open(base+"/?q=a", 800, 600)
	p.wait(`window.__ctx && window.vuka && document.querySelector("[data-vk-id]")`)
	time.Sleep(300 * time.Millisecond)
	if q := p.eval(`window.__props.Q`); q != "a" {
		t.Errorf("props.Q = %v", q)
	}
	p.eval(`window.__bump()`)
	p.wait(`window.__bumped === 1`)
	p.wait(`document.activeElement && document.activeElement.id === "box"`)
	p.click("#tog")
	p.wait(`window.__open === true && window.__ctx.state("open").get() === true`)
	p.eval(`window.__ctx.patch("?q=b")`)
	p.wait(`document.querySelector("#q").textContent === "b"`)
	p.wait(`window.__updates.join() === "b"`)
	p.eval(`window.__ctx.patch({ q: "c" })`)
	p.wait(`location.search === "?q=c" && window.__updates.join() === "b,c"`)
	if m := p.eval(`window.__mounts`); m != float64(1) {
		t.Errorf("mounted %v times: the patch kept the page", m)
	}
	old := c.open(base+"/old", 800, 600)
	old.wait(`window.__live && window.__q === ""`)
	old.eval(`window.__shout()`)
	old.wait(`document.querySelector("#q").textContent === "!"`)

	alerts := c.open(base+"/alerts", 800, 600)
	alerts.wait(`window.__alertRefs && window.vuka`)
	alerts.click("#open")
	alerts.wait(`document.querySelector('#dialog').open`)
	alerts.click("#check0")
	alerts.wait(`document.querySelector('#count').textContent === '1 selected'`)
	alerts.fill("#search", "1")
	alerts.click("#add")
	alerts.wait(`window.__alertRefs.checks().length === 3`)
	alerts.wait(`document.querySelectorAll('label:not([hidden])').length === 1`)
	if kept := alerts.eval(`window.__alertRefs.dialog === document.querySelector('#dialog')`); kept != true {
		t.Error("patch replaced the kept dialog ref")
	}
	alerts.click("#clear")
	alerts.wait(`document.querySelectorAll('label:not([hidden])').length === 3`)
	alerts.click("#leave")
	alerts.wait(`location.pathname === '/' && window.__alertsDestroyed === 1`)
	if clean := alerts.eval(`(() => { const before = window.__alertChanges; window.__alertRefs.form.dispatchEvent(new Event('change')); return before === window.__alertChanges && !window.__alertRefs.dialog.open })()`); clean != true {
		t.Error("navigation left a listener or open dialog")
	}
	if t.Failed() {
		t.Logf("logs:\n%s", logs.Bytes())
	}
}

const scriptRefPage = `<script lang="go">
package pages

import (
 "fmt"
 "github.com/vuka-lang/ui/v2"
)

type Alerts struct { ui.Live; N int }
func (p *Alerts) Add() { p.N++ }
</script>
<main>
 <button ref="open" id="open">Open alerts</button>
 <dialog ref="dialog" id="dialog">
  <input ref="search" id="search" type="search"/>
  <form ref="form">
   for i := range N+2 {
    <label><input ref="category" id={fmt.Sprint("check",i)} type="checkbox" value={fmt.Sprint(i)}/><span>Category {i}</span></label>
   }
  </form>
  <span ref="count" id="count">0 selected</span>
  <button ref="submit" id="submit" disabled>Subscribe</button>
  <button ref="clear" id="clear">Clear search</button>
  <button ref="close" id="close">Close</button>
  <button id="add" onClick={Add}>Add category</button>
  <ui.Link id="leave" href="/">Leave</ui.Link>
 </dialog>
</main>
<script lang="ts">
import { component, ref, listen, onDestroy } from 'vuka:component'
export default component<Alerts>(() => {
 const dialog = ref('dialog')
 const search = ref('search')
 const form = ref('form')
 const count = ref('count')
 const submit = ref('submit')
 const checks = ref.all('category')
 const w = window as any
 w.__alertRefs = { dialog, form, checks }
 const update = () => {
  const n = checks().filter(c => c.checked).length
  count.textContent = n + ' selected'
  submit.disabled = n === 0
  w.__alertChanges = (w.__alertChanges || 0) + 1
 }
 const filter = () => {
  for (const c of checks()) c.closest('label')!.hidden = !c.value.includes(search.value)
 }
 listen(ref('open'), 'click', () => dialog.showModal())
 listen(ref('close'), 'click', () => dialog.close())
 listen(ref('clear'), 'click', () => { search.value = ''; filter() })
 listen(search, 'input', filter)
 listen(form, 'change', update)
 checks.watch(() => { filter(); update() })
 onDestroy(() => { dialog.close(); w.__alertsDestroyed = (w.__alertsDestroyed || 0) + 1 })
})
</script>
`
