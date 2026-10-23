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

const slotsTask = `package components

// Task is a row of a TaskList.
type Task struct {
	ID    int
	Title string
}
`

const slotsTaskList = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// TaskList is a live component rendering its rows through the caller's
// Row, with a state of its own: the row picked.
type TaskList struct {
	ui.Live
	Items  []Task
	Row    func(Task, int) ui.Node ` + "`vuka:\"required\"`" + `
	Footer ui.Node
	picked int
	shut   ui.State[bool]
}

func (l *TaskList) Pick(id int) { l.picked = id }
</script>

<section>
  <button id="shut" onClick={shut = !shut}>shut</button>
  <ul id="list" hidden={shut}>
    for i, t := range Items {
      <li key={t.ID} id="row-{t.ID}" class:picked={t.ID == picked}>
        {Row(t, i)}
        <button class="pick" onClick={Pick(t.ID)}>pick</button>
      </li>
    }
  </ul>
  <slot:footer><p id="nofoot">no footer</p></slot:footer>
</section>
`

const slotsPage = `<script lang="go">
package pages

import (
	"context"

	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

// Index fills a live child's scoped slot with markup holding its own
// events and browser state.
type Index struct {
	ui.Live
	tasks   []components.Task
	removed int
	open    ui.State[bool]
	more    ui.State[bool] // read only inside the slot
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.tasks = []components.Task{{ID: 1, Title: "write"}, {ID: 2, Title: "test"}, {ID: 3, Title: "ship"}}
	return nil
}

func (p *Index) Remove(id int) {
	for i, t := range p.tasks {
		if t.ID == id {
			p.tasks = append(p.tasks[:i:i], p.tasks[i+1:]...)
			p.removed++
			return
		}
	}
}
</script>

<main>
  <p id="removed">removed {removed}</p>
  <p id="flag" hidden={!open}>open</p>
  <components.TaskList items={tasks}>
    <slot:row let={t, i}>
      <span class="title">{i + 1}. {t.Title}</span>
      <button class="rm" onClick={Remove(t.ID)}>remove</button>
      <button class="tog" onClick={open = !open}>toggle</button>
      <em class="seen" hidden={!open}>open</em>
      <button class="more" onClick={more = !more}>more</button>
      <i class="more-on" hidden={!more}>more</i>
    </slot:row>
  </components.TaskList>
</main>
`

// TestSlotsE2E: a live page fills a live child's scoped slot. The slot's
// content runs in the page's scope though the child renders it: its button
// calls the page's handler and the page renders again, its toggle flips the
// page's browser state, read inside the slot and outside it; the child's own
// handlers and state still work beside it.
func TestSlotsE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain,
		"components/task.go":            slotsTask,
		"components/tasklist.vuka.html": slotsTaskList,
		"pages/index.vuka.html":         slotsPage,
	})
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
	p.eval(`window.__marker = 1`)

	if got := p.text("#nofoot"); got != "no footer" {
		t.Errorf("the footer's fallback: %q", got)
	}
	// The page's browser state, toggled from inside the slot: read there
	// and outside.
	p.click("#row-2 .tog")
	p.wait(`!document.querySelector("#flag").hidden && [...document.querySelectorAll(".seen")].every((e) => !e.hidden)`)
	// The child's own handler and state beside the slot's.
	p.click("#row-3 .pick")
	p.wait(`document.querySelector("#row-3").classList.contains("picked")`)
	// The page's handler, from inside the slot: the page renders again.
	p.click("#row-1 .rm")
	p.wait(`document.querySelector("#removed").textContent === "removed 1" && !document.querySelector("#row-1")`)
	if got := p.eval(`[...document.querySelectorAll(".title")].map((e) => e.textContent).join(",")`); got != "1. test,2. ship" {
		t.Errorf("rows after the removal: %v", got)
	}
	if !p.eval(`document.querySelector("#row-3").classList.contains("picked")`).(bool) {
		t.Errorf("the child's state was lost when the page rendered again")
	}
	// The state survives the page's render, and still toggles.
	p.wait(`!document.querySelector("#flag").hidden`)
	p.click("#row-3 .tog")
	p.wait(`document.querySelector("#flag").hidden && [...document.querySelectorAll(".seen")].every((e) => e.hidden)`)
	// A state of the page read only in the slot is one state across the rows.
	p.click("#row-2 .more")
	p.wait(`[...document.querySelectorAll(".more-on")].every((e) => !e.hidden)`)
	p.click("#shut")
	p.wait(`document.querySelector("#list").hidden`)
	if !p.eval(`document.querySelector("#flag").hidden`).(bool) {
		t.Errorf("the child's state toggled the page's")
	}

	if m := p.eval(`window.__marker`); m != float64(1) {
		t.Errorf("the page reloaded")
	}
	if errs, _ := p.eval(`window.__errs`).([]any); len(errs) > 0 {
		t.Errorf("console: %v", errs)
	}
	if t.Failed() {
		t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
	}
}
