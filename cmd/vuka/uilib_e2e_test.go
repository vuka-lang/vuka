package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const uiLibPage = `<script lang="go">
package widgets

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/shop/components/ui/button"
	"example.com/shop/components/ui/dialog"
	selectbox "example.com/shop/components/ui/select"
)

// Widgets is a live page made of a library's components.
type Widgets struct {
	ui.Live
	n int
}

func (p *Widgets) Bump() { p.n++ }
</script>

<main>
  <output id="n">{n}</output>
  <button.Button id="bump" variant={button.VariantOutline} aria-label="Bump it" onClick={Bump}>Bump</button.Button>
  <dialog.Dialog>
    <button.Button id="open" attributes={dialog.Trigger(ctx)}>Open</button.Button>
    <dialog.Content>
      <p id="inner">Count {n}</p>
      <button.Button id="inner-bump" onClick={Bump}>Bump inside</button.Button>
    </dialog.Content>
  </dialog.Dialog>
  <selectbox.Select name="fruit" value="a">
    <selectbox.Trigger />
    <selectbox.Content>
      <selectbox.Item value="a">Apples</selectbox.Item>
      <selectbox.Item value="b">Pears</selectbox.Item>
    </selectbox.Content>
  </selectbox.Select>
</main>
`

const uiLibPlainPage = `<script lang="go">
package plain

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/shop/components/ui/button"
)

// Plain shows a component without a script.
type Plain struct {
	ui.Live
}
</script>

<main>
  <button.Button id="plain">Plain</button.Button>
</main>
`

// TestUILibraryE2E: vuka ui add library/name in an app vuka new made, from
// a stand-in library (testdata/uilib/widgets) that [ui.libraries] names:
// one command writes a dialog, a select and a button into components/ui
// with the packages they import, their scripts and the library's theme,
// gets templ as a direct requirement, and records the sources; vuka ui
// diff and list see them. vuka build bundles the scripts with no
// hand-written mount or script tag: in Chrome the page that shows the
// dialog loads them, and the dialog and select work on the live page
// through server updates, the select's while it's open and while it
// plays its closing animation; a page with only the button loads none. The
// theme's dark follows the visitor's preference.
func TestUILibraryE2E(t *testing.T) {
	e := newE2E(t)
	dir := e.new("shop", "--db=none", "--auth=no", "--mail=no")
	lib, err := filepath.Abs(filepath.Join("testdata", "uilib", "widgets"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vuka.toml"), fmt.Sprintf(`[ui.libraries.widgets]
dir = %q
scripts = ["shared"]
theme = "assets/css/theme.css"
css = ["assets/css/extras.css"]
style = "assets/css/style.css"
`, filepath.ToSlash(lib)))
	out := e.run(dir, nil, "vuka", "ui", "add", "widgets/dialog", "widgets/select", "widgets/button")
	for _, want := range []string{
		"vuka.toml has no [ui]: vuka ui init", "wrote styles/app.css: the design tokens",
		"wrote components/ui/dialog/dialog.templ", "wrote components/ui/dialog/dialog.js", "wrote components/ui/select/select.templ",
		"wrote components/ui/button/button.templ", "wrote components/ui/utils/utils.go", "wrote components/ui/shared/portal.js",
		"wrote styles/extras.css", "wrote styles/widgets.css: widgets's theme and styles", "wrote styles/app.css: imports widgets.css",
		"wrote pages/_layout.vuka.html: the document's colours", "wrote vuka.toml: [ui.sources]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("vuka ui add doesn't say %q:\n%s", want, out)
		}
	}
	for _, f := range []string{"components/ui/dialog/dialog.min.js", "components/ui/dialog/dialog_templ.go", "components/ui/unused"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("vuka ui add wrote %s", f)
		}
	}
	if src := read(t, filepath.Join(dir, "components/ui/dialog/dialog.templ")); !strings.Contains(src, `"example.com/shop/components/ui/button"`) || !strings.Contains(src, `"example.com/shop/components/ui/utils"`) {
		t.Errorf("dialog.templ imports the library's packages from elsewhere:\n%s", src)
	}
	if gomod := read(t, filepath.Join(dir, "go.mod")); !strings.Contains(gomod, "github.com/a-h/templ v") || strings.Contains(gomod, "github.com/a-h/templ v0.3.1020 // indirect") {
		t.Errorf("go.mod doesn't require templ directly:\n%s", gomod)
	}
	toml := read(t, filepath.Join(dir, "vuka.toml"))
	for _, n := range []string{"button", "dialog", "select", "shared", "utils"} {
		if !strings.Contains(toml, n+` = "widgets"`) {
			t.Errorf("vuka.toml doesn't record %s's source:\n%s", n, toml)
		}
	}
	theme := read(t, filepath.Join(dir, "styles/widgets.css"))
	for _, want := range []string{`@import "./extras.css";`, "--color-surface: var(--surface);", "--radius-2xl:", `:root:not([data-theme="light"])`, `:root[data-theme="dark"]`, ".wd-button {", ".wd-button-outline {", ".wd-dialog {"} {
		if !strings.Contains(theme, want) {
			t.Errorf("styles/widgets.css hasn't %q:\n%s", want, theme)
		}
	}
	for _, not := range []string{".dark", "wd-unused", "--font-sans", "--color-background", "body"} {
		if strings.Contains(theme, not) {
			t.Errorf("styles/widgets.css has %q:\n%s", not, theme)
		}
	}
	if css := read(t, filepath.Join(dir, "styles/extras.css")); strings.Contains(css, "custom-variant dark") {
		t.Errorf("styles/extras.css makes dark a class:\n%s", css)
	}
	if out := e.run(dir, nil, "vuka", "ui", "diff"); !strings.Contains(out, "widgets/button, widgets/dialog, widgets/select, widgets/shared, widgets/utils: the same as their libraries'") {
		t.Errorf("vuka ui diff:\n%s", out)
	}
	if out := e.run(dir, nil, "vuka", "ui", "list", "--offline"); !strings.Contains(out, "widgets/dialog") || !strings.Contains(out, "From libraries") {
		t.Errorf("vuka ui list:\n%s", out)
	}
	if out := e.run(dir, nil, "vuka", "ui", "list", "widgets"); !strings.Contains(out, "widgets/unused\n") || !strings.Contains(out, "widgets/dialog  (the app has it: widgets)") {
		t.Errorf("vuka ui list widgets:\n%s", out)
	}
	sel := filepath.Join(dir, "components/ui/select/select.js")
	orig := read(t, sel)
	writeFile(t, sel, orig+"// the app's\n")
	if out := e.run(dir, nil, "vuka", "ui", "diff", "widgets/select"); !strings.Contains(out, "--- components/ui/select/select.js") || !strings.Contains(out, "-// the app's") || !strings.Contains(out, "vuka ui add -f widgets/select") {
		t.Errorf("vuka ui diff widgets/select:\n%s", out)
	}
	if out := e.cmdOut(dir, "vuka", "ui", "add", "widgets/select"); !strings.Contains(out, "components/ui/select/select.js differs") {
		t.Errorf("vuka ui add over a changed file:\n%s", out)
	}
	e.run(dir, nil, "vuka", "ui", "add", "-f", "widgets/select")
	if read(t, sel) != orig {
		t.Errorf("vuka ui add -f didn't take the library's select.js")
	}
	writeFile(t, filepath.Join(dir, "pages/widgets/index.vuka.html"), uiLibPage)
	writeFile(t, filepath.Join(dir, "pages/plain/index.vuka.html"), uiLibPlainPage)
	e.run(dir, nil, "vuka", "vet", "./...")

	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	bin := exe(filepath.Join(dir, "shop"))
	e.run(dir, nil, "vuka", "build", "-o", bin, ".")
	base, _ := serve(t, dir, bin)
	c := launchChrome(t)

	modules := `performance.getEntriesByType("resource").map((r) => r.name).filter((n) => n.includes("/_vuka/c/` + scopeID("example.com/shop/components/ui", "library:widgets") + `.")).length`
	p := c.open(base+"/plain", 800, 600)
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	if n := p.eval(modules); n != float64(0) {
		t.Errorf("a page without the library's scripted components loaded %v modules", n)
	}

	p = c.open(base+"/widgets", 800, 600)
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	p.wait(`!!window.widgets`)
	if ok := p.eval(`getComputedStyle(document.querySelector("#bump")).borderTopWidth === "1px" && getComputedStyle(document.documentElement).backgroundColor === "oklch(1 0 0)"`); ok != true {
		t.Errorf("the library's style and the tokens don't draw the page: %v", p.eval(`document.querySelector("#bump").outerHTML`))
	}
	ids := `[...document.querySelectorAll("[id^='id-']")].map((e) => e.id).sort().join(" ")`
	before := p.eval(ids)
	p.click("#bump")
	p.wait(`document.querySelector("#n").textContent === "1"`)
	p.click("#open")
	p.wait(`document.querySelector("[role=dialog]").closest("[data-dialog-portal]").parentNode === document.body`)
	for i := 2; i <= 3; i++ {
		p.click("#inner-bump")
		p.wait(fmt.Sprintf(`document.querySelector("#n").textContent === "%d"`, i))
		p.wait(fmt.Sprintf(`document.querySelector("#inner").textContent === "Count %d"`, i))
		if ok := p.eval(`(() => {
			const ds = document.querySelectorAll("[role=dialog]"), portal = ds[0].closest("[data-dialog-portal]");
			return ds.length === 1 && portal.parentNode === document.body && !portal.hidden && ds[0].checkVisibility();
		})()`); ok != true {
			t.Fatalf("after update %d the dialog isn't one open dialog: %v", i, p.eval(`document.body.innerHTML`))
		}
	}
	p.click("[data-dialog-close]")
	p.wait(`document.querySelector("[data-dialog-portal]").hidden`)
	// The select's script puts a backdrop beside the list's positioner in
	// the portal and focus guards where the list was declared; an update
	// leaves them where they are, while the list is open and while it
	// plays its closing animation, and the select opens again.
	selectOpen := `(() => {
		const ls = document.querySelectorAll("[role=listbox]"), pos = ls[0].parentElement, portal = pos.parentElement;
		return ls.length === 1 && portal.parentNode === document.body && !portal.hidden && ls[0].checkVisibility() &&
			document.querySelectorAll("[data-select-backdrop]").length === 1 && pos.previousElementSibling.matches("[data-select-backdrop]") &&
			document.querySelectorAll("[data-focus-guard]").length === 2;
	})()`
	p.click("[data-select-trigger]")
	p.wait(selectOpen)
	p.click("#bump")
	p.wait(`document.querySelector("#n").textContent === "4"`)
	if ok := p.eval(selectOpen); ok != true {
		t.Fatalf("an update closed the open select: %v", p.eval(`document.body.innerHTML`))
	}
	p.click(`[role=option][data-value="b"]`)
	p.wait(`document.querySelector("[data-select-value]").textContent === "Pears"`)
	p.click("#bump")
	p.wait(`document.querySelector("#n").textContent === "5"`)
	if ok := p.eval(`!!document.querySelector("[data-ending-style]")`); ok != true {
		t.Fatalf("no closing animation runs after the update (it came too late, or replaced the closing list): %v", p.eval(`document.body.innerHTML`))
	}
	p.wait(`document.querySelector("[data-select-portal]").hidden && !document.querySelector("[data-ending-style], [data-select-backdrop], [data-focus-guard]")`)
	if ok := p.eval(`document.querySelector("[data-select-value]").textContent === "Pears" && document.querySelector("input[name=fruit]").value === "b" &&
		document.querySelectorAll("[role=listbox]").length === 1`); ok != true {
		t.Fatalf("the select's choice didn't outlive the update: %v", p.eval(`document.body.innerHTML`))
	}
	p.click("[data-select-trigger]")
	p.wait(selectOpen)
	if ok := p.eval(`document.querySelector('[role=option][data-value="b"]').getAttribute("aria-selected") === "true"`); ok != true {
		t.Errorf("the reopened select lost the choice: %v", p.eval(`document.querySelector("[role=listbox]").outerHTML`))
	}
	p.click(`[role=option][data-value="a"]`)
	p.wait(`document.querySelector("[data-select-value]").textContent === "Apples" && document.querySelector("input[name=fruit]").value === "a"`)
	if after := p.eval(ids); after != before {
		t.Errorf("library ids changed across patches:\n%v\n%v", before, after)
	}
	if n := p.eval(modules); n != float64(1) {
		t.Errorf("the page loaded %v of the library's modules, not one", n)
	}

	// Dark from the visitor's preference, not a .dark class.
	p = c.open(base+"/widgets", 800, 600)
	p.dark(true)
	if v := p.eval(`getComputedStyle(document.documentElement).getPropertyValue("--surface").trim()`); !strings.Contains(fmt.Sprint(v), ".02 250") {
		t.Errorf("--surface in the dark: %v", v)
	}
}

// cmdOut is the output of a command expected to fail.
func (e *e2e) cmdOut(dir, name string, args ...string) string {
	e.t.Helper()
	out, err := e.cmd(dir, nil, name, args...).CombinedOutput()
	if err == nil {
		e.t.Errorf("%s %s succeeded:\n%s", name, strings.Join(args, " "), out)
	}
	return string(out)
}

const shadcnTemplPage = `<script lang="go">
package products

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/shop/components/ui/button"
	"example.com/shop/components/ui/dialog"
	sel "example.com/shop/components/ui/select"
)

// Index is /products: shadcn-templ's components on a live page.
type Index struct {
	ui.Live
	n int
}

func (p *Index) Bump() { p.n++ }
</script>

<main class="p-8">
  <p>Stock: <output id="stock">{n}</output></p>
  <button.Button id="bump" variant={button.VariantOutline} aria-label="Bump stock" onClick={Bump}>Bump</button.Button>
  <dialog.Dialog>
    <button.Button id="open" attributes={dialog.Trigger(ctx)}>Open</button.Button>
    <dialog.Content>
      <dialog.Title>Edit product</dialog.Title>
      <dialog.Description>Stock is <span id="dlg-stock">{n}</span></dialog.Description>
      <button.Button id="dlg-bump" onClick={Bump}>Bump inside</button.Button>
    </dialog.Content>
  </dialog.Dialog>
  <sel.Select name="category" defaultValue="lamps">
    <sel.Trigger class="w-48"><sel.Value placeholder="Pick one" /></sel.Trigger>
    <sel.Content>
      <sel.Item value="lamps">Lamps</sel.Item>
      <sel.Item value="chairs">Chairs</sel.Item>
    </sel.Content>
  </sel.Select>
</main>
`

// TestUIShadcnTemplE2E is TestUILibraryE2E with the library vuka knows,
// shadcn-templ, fetched at the version vuka pins: its dialog, select and
// button on a live page in Chrome, through server updates. It reads the
// network, so it runs only with VUKA_UI_LIBRARIES=1.
func TestUIShadcnTemplE2E(t *testing.T) {
	if os.Getenv("VUKA_UI_LIBRARIES") == "" {
		t.Skip("fetches shadcn-templ: set VUKA_UI_LIBRARIES=1")
	}
	e := newE2E(t)
	dir := e.new("shop", "--db=none", "--auth=no", "--mail=no")
	out := e.run(dir, nil, "vuka", "ui", "add", "shadcn-templ/dialog", "shadcn-templ/select", "shadcn-templ/button")
	if !strings.Contains(out, "wrote styles/shadcn-templ.css") || !strings.Contains(out, "wrote components/ui/baseui/portal.js") {
		t.Fatalf("vuka ui add:\n%s", out)
	}
	writeFile(t, filepath.Join(dir, "pages/products/index.vuka.html"), shadcnTemplPage)
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	bin := exe(filepath.Join(dir, "shop"))
	e.run(dir, nil, "vuka", "build", "-o", bin, ".")
	base, _ := serve(t, dir, bin)
	c := launchChrome(t)
	p := c.open(base+"/products", 900, 700)
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	p.wait(`!!(window.templ && window.templ.dialog)`)
	ids := `[...document.querySelectorAll("[id^='id-']")].map((e) => e.id).sort().join(" ")`
	before := p.eval(ids)
	p.click("#bump")
	p.wait(`document.querySelector("#stock").textContent === "1"`)
	p.click("#open")
	p.wait(`(() => { const d = document.querySelector("[role=dialog]"); return d && d.checkVisibility(); })()`)
	for i := 2; i <= 3; i++ {
		p.click("#dlg-bump")
		p.wait(fmt.Sprintf(`document.querySelector("#dlg-stock").textContent === "%d"`, i))
		if ok := p.eval(`document.querySelectorAll("[role=dialog]").length === 1 && document.querySelector("[role=dialog]").checkVisibility()`); ok != true {
			t.Fatalf("after update %d the dialog isn't one open dialog: %v", i, p.eval(`document.body.innerHTML`))
		}
	}
	p.do("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	p.wait(`!document.querySelector("[role=dialog]").checkVisibility()`)
	// Updates while the list is open and while it plays its closing
	// animation leave the backdrop and focus guards its script added.
	p.click("[data-slot=select-trigger]")
	p.wait(`document.querySelector("[role=listbox]").checkVisibility()`)
	p.click("#bump")
	p.wait(`document.querySelector("#stock").textContent === "4"`)
	if ok := p.eval(`document.querySelectorAll("[role=listbox]").length === 1 && document.querySelector("[role=listbox]").checkVisibility()`); ok != true {
		t.Fatalf("an update closed the open select: %v", p.eval(`document.body.innerHTML`))
	}
	p.eval(`(() => {
		document.addEventListener("vk:patched", () => { window.patchedClosing = !!document.querySelector("[data-ending-style]"); }, { once: true });
		document.querySelector('[role=option][data-templ-value="chairs"]').click();
		document.querySelector("#bump").click();
	})()`)
	p.wait(`document.querySelector("#stock").textContent === "5"`)
	if ok := p.eval(`window.patchedClosing`); ok != true {
		t.Fatalf("the update didn't come during the closing animation")
	}
	p.wait(`!document.querySelector("[role=listbox]").checkVisibility() && !document.querySelector("[data-ending-style]")`)
	if ok := p.eval(`document.querySelector("[data-slot=select-value]").textContent.trim() === "chairs" &&
		document.querySelector("input[name=category]").value === "chairs" && document.querySelectorAll("[role=dialog]").length === 1`); ok != true {
		t.Fatalf("the select's choice didn't outlive the update: %v", p.eval(`document.body.innerHTML`))
	}
	// Open again after the update: one list, the choice kept.
	p.click("[data-slot=select-trigger]")
	p.wait(`(() => { const l = document.querySelectorAll("[role=listbox]"); return l.length === 1 && l[0].checkVisibility(); })()`)
	if ok := p.eval(`document.querySelector('[role=option][data-templ-value="chairs"]').getAttribute("aria-selected") === "true"`); ok != true {
		t.Errorf("the reopened select lost the choice: %v", p.eval(`document.querySelector("[role=listbox]").outerHTML`))
	}
	p.click(`[role=option][data-templ-value="lamps"]`)
	p.wait(`document.querySelector("input[name=category]").value === "lamps"`)
	if after := p.eval(ids); after != before {
		t.Errorf("library ids changed across patches:\n%v\n%v", before, after)
	}
	if bg := p.eval(`getComputedStyle(document.querySelector("#bump")).borderTopWidth`); bg != "1px" {
		t.Errorf("the outline button's border: %v (the library's style isn't in the stylesheet)", bg)
	}
}
