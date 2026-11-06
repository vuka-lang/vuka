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
	e.run(dir, nil, "vuka", "ui", "add", "button", "badge", "card", "alert", "input", "label", "tabs", "dialog", "dropdown-menu", "skeleton")
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

	key := func(k string) {
		t.Helper()
		code := map[string]int{"Escape": 27, "ArrowDown": 40, "ArrowUp": 38}[k]
		for _, typ := range []string{"keyDown", "keyUp"} {
			p.do("Input.dispatchKeyEvent", map[string]any{"type": typ, "key": k, "code": k, "windowsVirtualKeyCode": code})
		}
	}
	corner := func() { // a click on the page's top-left corner
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			p.do("Input.dispatchMouseEvent", map[string]any{"type": typ, "x": 12, "y": 12, "button": "left", "clickCount": 1})
		}
	}
	focused := func(want, after string) {
		t.Helper()
		if id := p.eval(`document.activeElement.id`); id != want {
			t.Errorf("the focus after %s: %q, want #%s", after, id, want)
		}
	}
	shown := func(sel string) string {
		return `!document.querySelector("` + sel + `").hidden && getComputedStyle(document.querySelector("` + sel + `")).display !== "none"`
	}
	hidden := func(sel string) string {
		return `document.querySelector("` + sel + `").hidden && getComputedStyle(document.querySelector("` + sel + `")).display === "none"`
	}

	// The dialog its page's browser state shows: from a button writing the
	// state and from a live handler writing it, the focus inside; Escape,
	// the backdrop, the close button and a button writing false hide it,
	// write false back and give the focus to what had it.
	open := func(by string) {
		t.Helper()
		p.eval(`document.querySelector("#` + by + `").focus()`)
		p.click("#" + by)
		p.wait(shown("#share") + ` && document.querySelector("#share-state").textContent === "true"`)
		focused("link", "opening the dialog from #"+by)
	}
	closed := func(by, how string) {
		t.Helper()
		p.wait(hidden("#share") + ` && document.querySelector("#share-state").textContent === "false"`)
		focused(by, "closing the dialog by "+how)
	}
	open("open-share")
	if got := p.eval(`JSON.parse(document.querySelector("#share").dataset.vkTransition).ms`); got != float64(150) {
		t.Errorf("the dialog's transition: %v ms", got)
	}
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
	key("Escape")
	closed("open-share", "Escape")
	open("share-from-server")
	corner()
	closed("share-from-server", "a click on the backdrop")
	open("open-share")
	p.click(`#share button[aria-label="Close"]`)
	closed("open-share", "its close button")
	open("open-share")
	p.click(`#share button:not([aria-label])`)
	closed("open-share", "its Done button")

	// A live render while the dialog is open keeps it open.
	open("open-share")
	p.click("#save")
	p.wait(`document.querySelector("main").textContent.includes("1 saved")`)
	if got := p.eval(shown("#share")); got != true {
		t.Errorf("a live render hid the open dialog")
	}
	p.click(`#share button[aria-label="Close"]`)
	closed("open-share", "its close button")

	// The dialog the page's handlers show and hide with the commands
	// (PushJS): shown with the focus inside, hidden by Escape, which
	// reaches its OnClose, with the focus back; its Delete hides it from
	// the server.
	p.eval(`document.querySelector("#ask-delete").focus()`)
	p.click("#ask-delete")
	p.wait(shown("#delete") + ` && document.querySelector("#delete").contains(document.activeElement)`)
	p.wait(`document.querySelector("main").textContent.includes("asked 1")`)
	shot(p, "ui-dialog-server")
	key("Escape")
	p.wait(hidden("#delete") + ` && document.querySelector("main").textContent.includes("asked 0")`)
	focused("ask-delete", "Escape hid the commands' dialog")

	// The dialog a server bool shows: Escape reaches its OnClose, which
	// clears the bool.
	p.click("#rename")
	p.wait(shown("#rename-dialog"))
	p.eval(`document.querySelector("#new-name").focus()`)
	key("Escape")
	p.wait(hidden("#rename-dialog") + ` && document.querySelector("main").textContent.includes("renamed 1")`)

	p.click("#ask-delete")
	p.wait(shown("#delete"))
	p.click("#confirm-delete")
	p.wait(hidden("#delete") + ` && document.querySelector("[role=alert]").textContent.includes("The project is deleted")`)

	// The tabs: a click on a tab and a button writing the page's state
	// switch the panel in the browser.
	panelShown := `[...document.querySelectorAll("[role=tabpanel]")].filter((p) => !p.hidden).map((p) => p.textContent).join("|")`
	if got := p.eval(panelShown); got != "Billed every month." {
		t.Errorf("the first panel: %q", got)
	}
	p.click("#plan-tab-yearly")
	p.wait(`document.querySelector("#plan-tab-yearly").getAttribute("aria-selected") === "true"`)
	if got := p.eval(panelShown); got != "Billed once a year, two months free." {
		t.Errorf("the panel after switching: %q", got)
	}
	if got := p.eval(`document.querySelector("#plan-tab-monthly").getAttribute("aria-selected")`); got != "false" {
		t.Errorf("the first tab after switching: aria-selected %v", got)
	}
	if got := p.eval(`document.querySelector("#plan-panel-yearly").getAttribute("aria-labelledby")`); got != "plan-tab-yearly" {
		t.Errorf("the panel is labelled by %v", got)
	}
	p.click("#plan-tab-monthly")
	p.wait(`document.querySelector("#plan-tab-monthly").getAttribute("aria-selected") === "true"`)
	shot(p, "ui-tabs")
	p.click("#pick-yearly")
	p.wait(`document.querySelector("#plan-tab-yearly").getAttribute("aria-selected") === "true"`)
	if got := p.eval(panelShown); got != "Billed once a year, two months free." {
		t.Errorf("the panel after the page wrote the state: %q", got)
	}
	p.click("#plan-tab-monthly")

	// The dropdown menu: its button and a button writing the page's state
	// show it, the focus on its first item; the arrow keys move between
	// the enabled items; Escape, a click outside and a click on an item
	// hide it, the focus back where it was.
	trigger := `#menu [aria-haspopup="menu"]`
	p.eval(`document.querySelector('` + trigger + `').id = "menu-button"`)
	menuShown := func() {
		t.Helper()
		p.wait(shown("#menu [role=menu]") + ` && document.activeElement.textContent === "Save"`)
	}
	menuHidden := func(by, how string) {
		t.Helper()
		p.wait(hidden("#menu [role=menu]") + ` && document.querySelector('` + trigger + `').getAttribute("aria-expanded") === "false"`)
		focused(by, "hiding the menu by "+how)
	}
	p.eval(`document.querySelector('` + trigger + `').focus()`)
	p.click(trigger)
	menuShown()
	if got := p.eval(`document.querySelector('` + trigger + `').getAttribute("aria-expanded")`); got != "true" {
		t.Errorf("the menu's button: aria-expanded %v", got)
	}
	if shots != "" {
		// The button and the menu under it, which overflows the root.
		menu := `({ getBoundingClientRect: () => {
			const a = document.querySelector("#menu").getBoundingClientRect(), b = document.querySelector("#menu [role=menu]").getBoundingClientRect()
			const x = Math.min(a.x, b.x), y = Math.min(a.y, b.y)
			return { x, y, width: Math.max(a.right, b.right) - x, height: Math.max(a.bottom, b.bottom) - y }
		} })`
		p.eval(`new Promise((r) => setTimeout(r, 250))`)
		p.screenshotOf(menu, filepath.Join(shots, "dropdown-menu-light.png"), 16)
		p.eval(`document.documentElement.dataset.theme = "dark"`)
		p.screenshotOf(menu, filepath.Join(shots, "dropdown-menu-dark.png"), 16)
		p.eval(`delete document.documentElement.dataset.theme`)
	}
	key("ArrowDown")
	key("ArrowDown")
	if got := p.eval(`document.activeElement.textContent`); got != "Delete…" {
		t.Errorf("two ArrowDowns from Save: %q, want Delete… (Archive is disabled)", got)
	}
	key("ArrowDown")
	if got := p.eval(`document.activeElement.textContent`); got != "Save" {
		t.Errorf("ArrowDown from the last item: %q, want Save", got)
	}
	key("Escape")
	menuHidden("menu-button", "Escape")
	p.eval(`document.querySelector("#open-menu").focus()`)
	p.click("#open-menu")
	menuShown()
	corner()
	p.wait(hidden("#menu [role=menu]"))
	p.eval(`document.querySelector('` + trigger + `').focus()`)
	p.click(trigger)
	menuShown()
	p.click(`#menu [role=menuitem]:nth-child(2)`)
	menuHidden("menu-button", "a click on an item")
	p.wait(`document.querySelector("#plan-tab-yearly").getAttribute("aria-selected") === "true"`)

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
	// Under prefers-reduced-motion the dialog shows and hides at once.
	p.click("#open-share")
	if got := p.eval(shown("#share") + ` && !document.querySelector("#share").style.opacity`); got != true {
		t.Errorf("under prefers-reduced-motion the dialog isn't shown at once")
	}
	p.click(`#share button[aria-label="Close"]`)
	if got := p.eval(hidden("#share")); got != true {
		t.Errorf("under prefers-reduced-motion the dialog isn't hidden at once")
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
