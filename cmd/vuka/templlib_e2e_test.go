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

const templLibPage = `<script lang="go">
package pages

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/app/components/widget"
)

// Index is a live page made of a templ component library's components.
type Index struct {
	ui.Live
	n int
}

func (p *Index) Bump() { p.n++ }
</script>

<main>
  <widget.Scripts />
  <output id="n">{n}</output>
  <widget.Button id="bump" variant="primary" aria-label="Bump it" data-kind="main" role="button" onClick={Bump}>Bump</widget.Button>
  <widget.Dialog>
    <widget.Button id="open" attributes={widget.Trigger(ctx)}>Open</widget.Button>
    <widget.Content>
      <p id="inner">Count {n}</p>
      <p id="dialog-id" data-for={widget.DialogID(ctx)}>{widget.DialogID(ctx)}</p>
      <widget.Button id="inner-bump" onClick={Bump}>Bump inside</widget.Button>
    </widget.Content>
  </widget.Dialog>
  <widget.Select name="fruit" value="a">
    <widget.SelectTrigger />
    <widget.SelectContent>
      <widget.SelectItem value="a">Apples</widget.SelectItem>
      <widget.SelectItem value="b">Pears</widget.SelectItem>
    </widget.SelectContent>
  </widget.Select>
</main>
`

// TestTemplLibraryE2E: a templ component library on a live page in Chrome.
// Its random ids are the same in every render, attributes that aren't
// fields and onClick reach its Attributes, a helper reading ctx gets its
// Dialog's, and the popups its script moves to the body are patched there:
// one dialog, still open through server updates, its server text current,
// and a select's chosen value kept.
func TestTemplLibraryE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	files := map[string]string{"main.go": pagesMain, "pages/index.vuka.html": templLibPage}
	for _, name := range []string{"widget.go", "widget.templ", "widget.js"} {
		b, err := os.ReadFile(filepath.Join("testdata", "templlib", name))
		if err != nil {
			t.Fatal(err)
		}
		files["components/widget/"+name] = string(b)
	}
	root := assetsModule(t, files)
	mod := filepath.Join(root, "go.mod")
	b, _ := os.ReadFile(mod)
	os.WriteFile(mod, append(b, "\nrequire github.com/a-h/templ v0.3.1020\n"...), 0o644)
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
	var first string
	for i := 0; ; i++ {
		if r, err := http.Get(base + "/"); err == nil {
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			first = string(body)
			break
		}
		if i > 100 {
			t.Fatalf("app didn't start:\n%s", logs.Bytes())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(first, `aria-label="Bump it"`) || !strings.Contains(first, `data-kind="main"`) || !strings.Contains(first, `role="button"`) ||
		!strings.Contains(first, `data-variant="primary"`) || !strings.Contains(first, `data-vk-on-click=`) {
		t.Fatalf("the tag's attributes aren't on the library's button:\n%s", first)
	}
	c := launchChrome(t)
	p := c.open(base+"/", 800, 600)
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	ids := `[...document.querySelectorAll("[id^='id-']")].map((e) => e.id).sort().join(" ")`
	before := p.eval(ids)
	if s, _ := before.(string); !strings.Contains(s, "-list") || strings.Count(s, "id-") < 4 {
		t.Fatalf("library ids: %v", before)
	}
	if ctl := p.eval(`document.querySelector("#open").getAttribute("data-dialog-trigger")`); ctl != p.eval(`document.querySelector("[role=dialog]").id`) || ctl == "" {
		t.Fatalf("the trigger names %v, not its Dialog", ctl)
	}

	if ok := p.eval(`(() => { const p = document.querySelector("#dialog-id"), id = document.querySelector("[role=dialog]").id;
		return p.textContent === id && p.getAttribute("data-for") === id; })()`); ok != true {
		g, _ := os.ReadFile(filepath.Join(root, "build", "pages", "index_html.go"))
		t.Errorf("markup between the Dialog's tags doesn't read its ctx: %v\n%s", p.eval(`document.querySelector("#dialog-id").outerHTML`), g)
	}

	p.click("#bump")
	p.wait(`document.querySelector("#n").textContent === "1"`)

	p.click("#open")
	p.wait(`document.querySelector("[role=dialog]").closest("[data-dialog-portal]").parentNode === document.body`)
	for i := 2; i <= 4; i++ {
		p.click("#inner-bump")
		p.wait(fmt.Sprintf(`document.querySelector("#n").textContent === "%d"`, i))
		p.wait(fmt.Sprintf(`document.querySelector("#inner").textContent === "Count %d"`, i))
		if ok := p.eval(`(() => {
			const ds = document.querySelectorAll("[role=dialog]"), portal = ds[0].closest("[data-dialog-portal]");
			return ds.length === 1 && portal.parentNode === document.body && !portal.hidden && portal.hasAttribute("data-open") &&
				document.querySelector("#open").getAttribute("aria-expanded") === "true" && ds[0].checkVisibility();
		})()`); ok != true {
			t.Fatalf("after update %d the dialog isn't one open dialog: %v", i, p.eval(`document.body.innerHTML`))
		}
	}
	p.click("[data-dialog-close]")
	p.wait(`document.querySelector("[data-dialog-portal]").hidden`)

	p.click("[data-select-trigger]")
	p.wait(`!document.querySelector("[data-select-portal]").hidden`)
	p.click(`[role=option][data-value="b"]`)
	p.wait(`document.querySelector("[data-select-value]").textContent === "Pears"`)
	for i := 5; i <= 6; i++ {
		p.click("#bump")
		p.wait(fmt.Sprintf(`document.querySelector("#n").textContent === "%d"`, i))
	}
	if ok := p.eval(`document.querySelector("[data-select-value]").textContent === "Pears" &&
		document.querySelector("input[name=fruit]").value === "b" && document.querySelectorAll("[role=listbox]").length === 1 &&
		document.querySelector('[data-value="b"]').getAttribute("aria-selected") === "true" &&
		document.querySelectorAll("[role=dialog]").length === 1`); ok != true {
		t.Fatalf("the select's choice didn't outlive the updates: %v", p.eval(`document.body.innerHTML`))
	}
	if after := p.eval(ids); after != before {
		t.Errorf("library ids changed across patches:\n%v\n%v", before, after)
	}
	if t.Failed() {
		t.Logf("logs:\n%s", logs.Bytes())
	}
}

const templLibWarnPage = `<script lang="go">
package pages

import (
	"github.com/a-h/templ"
	"github.com/vuka-lang/ui/v2"

	"example.com/app/components/widget"
)

type Index struct {
	ui.Live
	n int
}

func (p *Index) Bump() { p.n++ }
</script>

<main>
  <widget.Chip label="tag" attrs={templ.Attributes{"onClick": Bump}} />
  <widget.Button attributes={templ.Attributes{"onClick": Bump}}>bound</widget.Button>
</main>
`

// TestTemplLibraryAttrs: a near miss of a field stays E0106, pointing at
// attributes=; an on… handler in a templ.Attributes nothing binds is W0112.
func TestTemplLibraryAttrs(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{"main.go": "package main\n\nimport _ \"example.com/app/pages\"\n\nfunc main() {}\n", "pages/index.vuka.html": templLibWarnPage}
	for _, name := range []string{"widget.go", "widget.templ", "widget.js"} {
		b, _ := os.ReadFile(filepath.Join("testdata", "templlib", name))
		files["components/widget/"+name] = string(b)
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	root := tempModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("build: %v\n%s", err, stderr.String())
	}
	if out := stderr.String(); strings.Count(out, "warning[W0112]") != 1 || !strings.Contains(out, "onClick in templ.Attributes binds no event") || !strings.Contains(out, "widget.Chip") {
		t.Errorf("vuka build says:\n%s", out)
	}
	page := strings.Replace(templLibWarnPage, `<widget.Button attributes`, `<widget.Button varient="primary" attributes`, 1)
	os.WriteFile(filepath.Join(root, "pages", "index.vuka.html"), []byte(page), 0o644)
	stderr.Reset()
	if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
		t.Fatal("a near miss of a field built")
	}
	if out := stderr.String(); !strings.Contains(out, "error[E0106]: <widget.Button> has no attribute varient; did you mean Variant? (an HTML attribute of that name: attributes={templ.Attributes{\"varient\": …}})") {
		t.Errorf("vuka build says:\n%s", out)
	}
}
