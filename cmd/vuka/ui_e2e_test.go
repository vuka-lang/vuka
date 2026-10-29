package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The components vuka ui add writes, in an app vuka new made: vuka ui init
// and add, the app's tests (cn's among them) and vuka vet pass, vuka build
// builds it with its Tailwind stylesheet; in Chrome the page draws with the
// tokens in both themes (data-theme and prefers-color-scheme), a dialog
// opens, closes on Escape, the backdrop and its close button, giving the
// focus back each time, a dialog the server owns closes through its
// handler, and tabs switch over the live connection. With VUKA_SHOTS set,
// it saves screenshots there: the page's, and each component's in both
// themes, which docs/public/ui/components keeps for the components' pages
// (cp $VUKA_SHOTS/*-light.png $VUKA_SHOTS/*-dark.png docs/public/ui/components).
func TestUIComponentsE2E(t *testing.T) {
	e := newE2E(t)
	dir := e.new("kit", "--db=none", "--auth=no", "--mail=no")
	out := e.run(dir, nil, "vuka", "ui", "init")
	if !strings.Contains(out, "wrote styles/app.css: the design tokens") {
		t.Fatalf("vuka ui init:\n%s", out)
	}
	e.run(dir, nil, "vuka", "ui", "add", "button", "badge", "card", "alert", "input", "label", "tabs", "dialog")
	page, err := os.ReadFile("../../examples/conformance/uikit/vuka/pages/index.vuka.html")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(string(page), "package pages\n", "package kit\n", 1)
	src = strings.Replace(src, `"example.com/app/components/ui"`, `"example.com/kit/components/ui"`, 1)
	writeFile(t, filepath.Join(dir, "pages/kit/index.vuka.html"), src)
	// The document draws with the tokens too, so the whole page follows the
	// theme.
	layout := filepath.Join(dir, "pages/_layout.vuka.html")
	doc := read(t, layout)
	themed := strings.Replace(doc, `class="h-full bg-slate-50 antialiased dark:bg-slate-950"`, `class="h-full bg-background text-foreground antialiased"`, 1)
	if themed == doc {
		t.Fatalf("the layout's <html> class changed:\n%s", doc)
	}
	writeFile(t, layout, themed)
	e.run(dir, nil, "vuka", "test", "./...")
	e.run(dir, nil, "vuka", "vet", "./...")

	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	bin := exe(filepath.Join(dir, "kit"))
	e.run(dir, nil, "vuka", "build", "-o", bin, ".")
	base, _ := serve(t, dir, bin)

	shots := os.Getenv("VUKA_SHOTS")
	shot := func(p *tab, name string) {
		if shots != "" {
			p.eval(`new Promise((r) => setTimeout(r, 250))`)
			p.screenshot(filepath.Join(shots, name+".png"))
		}
	}
	// Each component's part of the page, by its name in the registry.
	parts := map[string]string{
		"button": `document.querySelector("#save").parentElement`,
		"badge":  `document.querySelector("main > div:nth-of-type(2)")`,
		"alert":  `document.querySelector("main [role=alert]")`,
		"card":   `document.querySelector("#name").closest("main > *")`,
		"input":  `document.querySelector("#name").closest("form")`,
		"label":  `document.querySelector("#name").closest("form")`,
		"tabs":   `document.querySelector("[role=tablist]").closest("main > *")`,
	}
	shotParts := func(p *tab, theme string) {
		if shots == "" {
			return
		}
		p.eval(`new Promise((r) => setTimeout(r, 250))`)
		for name, el := range parts {
			p.screenshotOf(el, filepath.Join(shots, name+"-"+theme+".png"), 16)
		}
	}
	ch := launchChrome(t)
	p := ch.open(base+"/kit", 1280, 1100)
	p.wait(`document.documentElement.dataset.vkState === "connected"`)
	bg := `getComputedStyle(document.querySelector("main")).backgroundColor`
	primary := `getComputedStyle(document.querySelector("#save")).backgroundColor`

	// The themes: the tokens follow data-theme, and prefers-color-scheme
	// when the page sets none.
	p.dark(false)
	light, lightPrimary := p.eval(bg), p.eval(primary)
	if light != "oklch(1 0 0)" {
		t.Errorf("the light background: %v", light)
	}
	shot(p, "ui-light")
	shotParts(p, "light")
	p.eval(`document.documentElement.dataset.theme = "dark"`)
	dark := p.eval(bg)
	if dark == light || p.eval(primary) == lightPrimary {
		t.Errorf("data-theme=dark: the background is %v, the primary %v, as in the light theme", dark, p.eval(primary))
	}
	shot(p, "ui-dark")
	shotParts(p, "dark")
	p.eval(`delete document.documentElement.dataset.theme`)
	p.dark(true)
	if got := p.eval(bg); got != dark {
		t.Errorf("prefers-color-scheme: dark: the background is %v, want %v", got, dark)
	}
	p.eval(`document.documentElement.dataset.theme = "light"`)
	if got := p.eval(bg); got != light {
		t.Errorf("data-theme=light under a dark preference: the background is %v, want %v", got, light)
	}
	p.eval(`delete document.documentElement.dataset.theme`)
	p.dark(false)

	// The dialog: opened by its button's JS commands, focus inside; closed
	// by Escape, the backdrop and the close button, focus back on the
	// button each time.
	open := func() {
		t.Helper()
		p.eval(`document.querySelector("#open-share").focus()`)
		p.click("#open-share")
		p.wait(`document.querySelector("#share").open && getComputedStyle(document.querySelector("#share")).display !== "none"`)
		if id := p.eval(`document.activeElement.id`); id != "link" {
			t.Errorf("the focus in the open dialog: %q, want the first control, #link", id)
		}
	}
	closed := func(how string) {
		t.Helper()
		p.wait(`!document.querySelector("#share").open && getComputedStyle(document.querySelector("#share")).display === "none"`)
		if id := p.eval(`document.activeElement.id`); id != "open-share" {
			t.Errorf("the focus after closing the dialog by %s: %q, want #open-share", how, id)
		}
	}
	open()
	shot(p, "ui-dialog")
	panel := `document.querySelector("#share > div:last-child")`
	if shots != "" {
		p.screenshotOf(panel, filepath.Join(shots, "dialog-light.png"), 24)
	}
	p.eval(`document.documentElement.dataset.theme = "dark"`)
	shot(p, "ui-dialog-dark")
	if shots != "" {
		p.screenshotOf(panel, filepath.Join(shots, "dialog-dark.png"), 24)
	}
	p.eval(`delete document.documentElement.dataset.theme`)
	p.do("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	p.do("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	closed("Escape")
	open()
	for _, typ := range []string{"mousePressed", "mouseReleased"} { // a corner: the backdrop
		p.do("Input.dispatchMouseEvent", map[string]any{"type": typ, "x": 12, "y": 12, "button": "left", "clickCount": 1})
	}
	closed("a click on the backdrop")
	open()
	p.click(`#share button[aria-label="Close"]`)
	closed("its close button")
	open()
	p.click(`#share button:not([aria-label])`)
	closed("its Done button")

	// A live render while the dialog is open keeps it open.
	open()
	p.click("#save")
	p.wait(`document.querySelector("main").textContent.includes("1 saved")`)
	if open := p.eval(`document.querySelector("#share").open`); open != true {
		t.Errorf("a live render closed the open dialog")
	}
	p.click(`#share button[aria-label="Close"]`)
	closed("its close button")

	// The dialog the page's handlers open and close (PushJS): opened with
	// the focus inside, closed by Escape, which reaches its OnClose, with
	// the focus back; its Delete closes it from the server.
	p.eval(`document.querySelector("#ask-delete").focus()`)
	p.click("#ask-delete")
	p.wait(`document.querySelector("#delete").open && document.querySelector("#delete").contains(document.activeElement)`)
	p.wait(`document.querySelector("main").textContent.includes("asked 1")`)
	shot(p, "ui-dialog-server")
	p.do("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	p.wait(`!document.querySelector("#delete").open && document.querySelector("main").textContent.includes("asked 0")`)
	if id := p.eval(`document.activeElement.id`); id != "ask-delete" {
		t.Errorf("the focus after Escape closed the server's dialog: %q", id)
	}
	p.click("#ask-delete")
	p.wait(`document.querySelector("#delete").open`)
	p.click("#confirm-delete")
	p.wait(`!document.querySelector("#delete").open && document.querySelector("[role=alert]").textContent.includes("The project is deleted")`)

	// The tabs: a click switches the panel over the live connection.
	if got := p.text("[role=tabpanel]"); got != "Billed every month." {
		t.Errorf("the first panel: %q", got)
	}
	p.click("#plan-tab-yearly")
	p.wait(`document.querySelector("#plan-tab-yearly").getAttribute("aria-selected") === "true"`)
	if got := p.text("[role=tabpanel]"); got != "Billed once a year, two months free." {
		t.Errorf("the panel after switching: %q", got)
	}
	if got := p.eval(`document.querySelector("#plan-tab-monthly").getAttribute("aria-selected")`); got != "false" {
		t.Errorf("the first tab after switching: aria-selected %v", got)
	}
	if got := p.eval(`document.querySelector("[role=tabpanel]").getAttribute("aria-labelledby")`); got != "plan-tab-yearly" {
		t.Errorf("the panel is labelled by %v", got)
	}
	shot(p, "ui-tabs")
}
