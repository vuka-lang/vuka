package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The components vuka ui add writes, in an app vuka new made: vuka ui init
// and add, the app's tests (cn's among them) and vuka vet pass, vuka build
// builds it with its Tailwind stylesheet; in Chrome the page draws with the
// tokens in both themes (data-theme and prefers-color-scheme), a dialog
// opens, closes on Escape, the backdrop and its close button, giving the
// focus back each time, a dialog the server owns closes through its
// handler, and tabs switch over the live connection; SkeletonOf's copy of a
// component has the component's boxes, drawn as bones as its annotations
// say, a Skeleton wrapping a button keeps its box as Loading toggles, and
// nothing moves under prefers-reduced-motion. With VUKA_SHOTS set,
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
	e.run(dir, nil, "vuka", "ui", "add", "button", "badge", "card", "alert", "input", "label", "tabs", "dialog", "skeleton")
	page, err := os.ReadFile("../../examples/conformance/uikit/vuka/pages/index.vuka.html")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(string(page), "package pages\n", "package kit\n", 1)
	src = strings.Replace(src, `"example.com/app/components/ui"`, `"example.com/kit/components/ui"`, 1)
	writeFile(t, filepath.Join(dir, "pages/kit/index.vuka.html"), src)
	writeFile(t, filepath.Join(dir, "pages/kit/parts/index.vuka.html"), partsPage)
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
		"button":   `document.querySelector("#save").parentElement`,
		"badge":    `document.querySelector("main > div:nth-of-type(2)")`,
		"alert":    `document.querySelector("main [role=alert]")`,
		"card":     `document.querySelector("#name").closest("main > *")`,
		"input":    `document.querySelector("#name").closest("form")`,
		"label":    `document.querySelector("#name").closest("form")`,
		"tabs":     `document.querySelector("[role=tablist]").closest("main > *")`,
		"skeleton": `document.querySelector("#skeletons")`,
	}
	shotParts := func(p *tab, theme string) {
		if shots == "" {
			return
		}
		p.eval(`new Promise((r) => setTimeout(r, 250))`)
		p.eval(`document.getAnimations().filter((a) => a.animationName?.startsWith("skeleton")).forEach((a) => { a.pause(); a.currentTime = 0 })`)
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

	// SkeletonOf: its copy of the card has the card's boxes, every one;
	// its text is bars, its badge (replace) a bone, its kept label as it
	// is, its ignored dot hidden, its united tag a bone inside its border,
	// its button and icon blocks.
	boxes := `JSON.stringify([document.querySelector("#post > *"), document.querySelector("#post-skeleton [data-skeleton-of] > *")].map((root) => {
		const o = root.getBoundingClientRect()
		return [root, ...root.querySelectorAll("*")].map((e) => {
			const r = e.getBoundingClientRect()
			return [e.tagName, r.x - o.x, r.y - o.y, r.width, r.height].map((v) => typeof v === "number" ? Math.round(v * 2) / 2 : v).join(" ")
		})
	}))`
	var pair [2][]string
	if err := json.Unmarshal([]byte(p.eval(boxes).(string)), &pair); err != nil {
		t.Fatal(err)
	}
	if len(pair[0]) < 10 || !slices.Equal(pair[0], pair[1]) {
		t.Errorf("SkeletonOf's boxes aren't the card's:\n card     %q\n skeleton %q", pair[0], pair[1])
	}
	drawn := p.eval(`(() => {
		const copy = document.querySelector("#post-skeleton [data-skeleton-of]"), s = (sel) => getComputedStyle(copy.querySelector(sel))
		const bone = getComputedStyle(document.querySelector("#skeletons .bg-skeleton")).backgroundColor
		return {
			text: s("p").textDecorationLine + " " + s("p").color,
			replace: s("[data-skeleton=replace]").backgroundColor === bone,
			keep: s("[data-skeleton=keep]").color === getComputedStyle(document.querySelector("#post [data-skeleton=keep]")).color,
			ignore: s("[data-skeleton=ignore]").visibility,
			unite: s("[data-skeleton=unite]").backgroundClip + " " + (s("[data-skeleton=unite]").backgroundColor === bone),
			button: s("button").backgroundColor === bone && s("button svg").visibility === "hidden",
			status: [copy.parentElement.getAttribute("role"), copy.parentElement.getAttribute("aria-busy"), copy.inert, copy.getAttribute("aria-hidden"), copy.parentElement.textContent.includes("Loading the post")].join(" "),
		}
	})()`).(map[string]any)
	for k, want := range map[string]any{"text": "underline rgba(0, 0, 0, 0)", "replace": true, "keep": true, "ignore": "hidden", "unite": "content-box true", "button": true, "status": "status true true true true"} {
		if drawn[k] != want {
			t.Errorf("SkeletonOf's copy, %s: %v, want %v", k, drawn[k], want)
		}
	}

	// A Skeleton wrapping a button: loading, the button is laid out but
	// hidden under a bone of its box; loaded, it shows in the same box, and
	// nothing around it moves.
	around := `JSON.stringify(["#publish-wrap", "#publish", "#after-publish", "#toggle-loading"].map((s) => { const r = document.querySelector(s).getBoundingClientRect(); return [r.x, r.y, r.width, r.height] }))`
	loading := p.eval(around)
	if got := p.eval(`[document.querySelector("#publish-wrap").getAttribute("aria-busy"), getComputedStyle(document.querySelector("#publish")).visibility, JSON.stringify(document.querySelector("#publish-wrap > [aria-hidden]").getBoundingClientRect()) === JSON.stringify(document.querySelector("#publish").getBoundingClientRect())].join(" ")`); got != "true hidden true" {
		t.Errorf("the loading skeleton (aria-busy, the button's visibility, the bone has its box): %v", got)
	}
	p.click("#toggle-loading")
	p.wait(`!document.querySelector("#publish-wrap").hasAttribute("aria-busy") && getComputedStyle(document.querySelector("#publish")).visibility === "visible"`)
	if got := p.eval(around); got != loading {
		t.Errorf("Loading moved the layout: %v loading, %v loaded", loading, got)
	}
	p.click("#toggle-loading")
	p.wait(`document.querySelector("#publish-wrap").getAttribute("aria-busy") === "true"`)

	// Animation: pulse and wave, and none at all under prefers-reduced-motion.
	motion := `[...document.querySelectorAll("#skeletons .skeleton-pulse, #skeletons .skeleton-wave, #post-skeleton [data-skeleton-of] > *")].map((e) => getComputedStyle(e).animationName).join(" ")`
	if got := p.eval(motion).(string); !strings.Contains(got, "skeleton-pulse") || !strings.Contains(got, "skeleton-wave") || strings.Contains(got, "none") {
		t.Errorf("the skeletons' animations: %q", got)
	}
	p.do("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "reduce"}}})
	if got := strings.Fields(p.eval(motion).(string)); len(got) == 0 || slices.ContainsFunc(got, func(a string) bool { return a != "none" }) {
		t.Errorf("the skeletons' animations under prefers-reduced-motion: %q", got)
	}
	p.dark(false)

	// The fallbacks of a page that isn't live while its slow parts load: a
	// deferred method's (ui.Skeleton) and a boundary's (design.Skeleton).
	p.do("Page.navigate", map[string]any{"url": base + "/kit/parts"})
	p.wait(`document.querySelector("#parts .vk-skeleton") && document.querySelector("#parts .bg-skeleton")`)
	if got := p.eval(`(() => { const s = getComputedStyle(document.querySelector("#parts .vk-skeleton")); return [s.animationName, s.backgroundColor !== "rgba(0, 0, 0, 0)", s.height !== "0px"].join(" ") })()`); got != "pulse true true" {
		t.Errorf("ui.Skeleton, a part's fallback (animation, background, height): %v", got)
	}
	if shots != "" {
		p.eval(`document.getAnimations().forEach((a) => { a.pause(); a.currentTime = 0 })`)
		p.screenshotOf(`document.querySelector("#parts")`, filepath.Join(shots, "skeleton-parts-light.png"), 0)
		p.eval(`document.documentElement.dataset.theme = "dark"`)
		p.screenshotOf(`document.querySelector("#parts")`, filepath.Join(shots, "skeleton-parts-dark.png"), 0)
	}
}

// partsPage is a page whose parts take a minute: their fallbacks show.
const partsPage = `<script lang="go">
package parts

import (
	"context"
	"time"

	design "example.com/kit/components/ui"
)

// Index is GET /kit/parts.
type Index struct{}

func slow(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Minute):
		return nil
	}
}

@ui.Defer
func (p Index) Posts(ctx context.Context) ([]string, error) { return []string{"Shipping notes"}, slow(ctx) }

func (p Index) Readers(ctx context.Context) (int, error) { return 12, slow(ctx) }
</script>

<main id="parts" class="mx-auto grid max-w-md gap-6 bg-background p-8 text-foreground">
  <section class="grid gap-3">
    <h2 class="font-semibold">Recent posts</h2>
    <ul>
      for _, t := range Posts() {
        <li>{t}</li>
      }
    </ul>
    <ui.Fallback for={Posts}><ui.Skeleton lines={3} class="text-muted-foreground"/></ui.Fallback>
  </section>
  <section class="grid gap-3">
    <h2 class="font-semibold">Readers</h2>
    <ui.Suspense>
      <slot:fallback><design.Skeleton lines={2}/></slot:fallback>
      readers := Readers()
      <p>{readers} readers this week</p>
    </ui.Suspense>
  </section>
</main>
`
