package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const namesIndex = `<script lang="go">
package pages

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

// Index is the board.
type Index struct {
	ui.Live
	Q    string @ui.URL("q")
	page int    @ui.URL("page").Default(1)
	open ui.State[bool]
}

func (p *Index) Update(web.URLParams) {}

// Reorder puts the cards in the order of ids.
// It saves them too.
func (p *Index) Reorder(ids []int) {
	p.PushEvent("saved", Point{X: 1})
	p.PushEvent("count", len(ids))
	p.PushEvent("named", "x")
}
</script>

<main>
  <input ref="search" />
  <canvas ref="chart"></canvas>
  <components.Card ref="card" title={Q} onPicked={Reorder} />
</main>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Index>((el, props, ctx) => {})
</script>
`

const namesUser = `<script lang="go">
package user

type Page struct{ ID string }
</script>

<p>{ID}</p>
`

const namesCard = `<script lang="go">
package components

// Card is a card its parent listens to.
type Card struct {
	Title    string
	OnPicked func(id int)
	name     string
}
</script>

<section onPicked={OnPicked} onClose={OnPicked}>
  <h3 ref="title">{Title}</h3>
  <div ref={name}></div>
</section>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Card>((el, props, ctx) => {})
</script>
`

// TestScriptNames: the generated types list what a script names as
// strings, from the fixture's markup and Go: refs (with their elements'
// types), PushEvent's events and payloads, the events a component's root
// listens for (ctx.emit), a page's @ui.URL keys (patch({q})), the folder
// routes (navigate), and the Go doc of each live method.
func TestScriptNames(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"main.go":                    "package main\n\nfunc main() {}\n",
		"pages/index.vuka.html":      namesIndex,
		"pages/point.go":             "package pages\n\n// Point is a place on the board.\ntype Point struct{ X, Y int }\n",
		"pages/users/[id].vuka.html": namesUser,
		"components/card.vuka.html":  namesCard,
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	dts := string(componentDTS(root, "example.com/app", pa.comps))
	for _, want := range []string{
		"search: TagElement<\"input\">;", "chart: TagElement<\"canvas\">;", "card: HTMLElement;",
		"saved: Point;", "count: unknown;", "named: string;",
		"q: true;", "page: true;",
		"title: TagElement<\"h3\">;", "[name: string]: HTMLElement;", "picked: unknown;", "close: unknown;",
		"export type Route = \"/\" | `/users/${string}`;",
		"/** Reorder puts the cards in the order of ids.\n\t\t * It saves them too. */",
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("no %q in\n%s", want, dts)
		}
	}
	dir := filepath.Join(root, ".vuka")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, componentDTSName), []byte(dts), 0o644)
	if out := tscCheck(t, dir, `import { component } from 'vuka:component'
export const a = component<Index>((el, props, ctx) => {
	const s: HTMLInputElement | null = ctx.ref("search")
	ctx.ref("chart")?.getContext("2d")
	ctx.on("saved", (p) => { p.X.toFixed() })
	ctx.on("named", (p) => { p.toUpperCase() })
	ctx.patch({ q: "shoes", page: null })
	ctx.patch("?q=x")
	ctx.navigate("/users/7")
	ctx.navigate("https://example.com/")
	ctx.live.reorder([1])
})
export const b = component<Card>((el, props, ctx) => {
	ctx.emit("picked", 3)
	ctx.ref("anything")
})
`); out != "" {
		t.Errorf("a correct script doesn't type-check:\n%s", out)
	}
	out := tscCheck(t, dir, `import { component } from 'vuka:component'
export const a = component<Index>((el, props, ctx) => {
	ctx.ref("serch")
	ctx.on("sved", () => {})
	ctx.patch({ qq: "x" })
	ctx.emit("picked")
})
export const b = component<Card>((el, props, ctx) => {
	ctx.emit("pick")
})
`)
	for _, want := range []string{`"serch"`, `"sved"`, "qq", "emit", `"pick"`} {
		if !strings.Contains(out, want) {
			t.Errorf("tsc doesn't flag %s:\n%s", want, out)
		}
	}
}

// TestVetScripts: vuka vet warns (W0308) on a script naming a ref, state
// or event its shape doesn't have, and on ctx.emit in a page's script, at
// the name's line and column, with the name it likely meant.
func TestVetScripts(t *testing.T) {
	page := strings.Replace(namesIndex, "export default component<Index>((el, props, ctx) => {})", `export default component<Index>((el, props, c) => {
  c.ref("search")
  c.ref("serch")
  c.state("open")
  c.state("shut")
  c.on("saved", () => {})
  c.on("sved", () => {})
  c.emit("picked")
})`, 1)
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"main.go":                   "package main\n\nfunc main() {}\n",
		"pages/index.vuka.html":     page,
		"pages/point.go":            "package pages\n\ntype Point struct{ X, Y int }\n",
		"components/card.vuka.html": strings.Replace(namesCard, "(el, props, ctx) => {}", "(el, props, ctx) => { ctx.emit('picked'); ctx.emit('pick') }", 1),
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range vetScripts(root, "example.com/app", pa) {
		got = append(got, d.Span.String()+": "+d.Message+" ("+d.Help+")")
	}
	line := func(file, src, needle string) string {
		return file + ":" + itoaTest(uint32(strings.Count(src[:strings.Index(src, needle)], "\n")+1))
	}
	cardSrc, _ := os.ReadFile(filepath.Join(root, "components", "card.vuka.html"))
	want := []string{
		line("components/card.vuka.html", string(cardSrc), "ctx.emit('pick')") + ":" + itoaTest(uint32(strings.Index(string(cardSrc), "ctx.emit('pick')")-strings.LastIndex(string(cardSrc)[:strings.Index(string(cardSrc), "ctx.emit('pick')")], "\n")+len("ctx.emit('"))) + `: ctx.emit("pick"): the root of Card's markup has no on… attribute for "pick" (did you mean "picked"?)`,
		line("pages/index.vuka.html", page, `c.ref("serch")`) + `:10: ctx.ref("serch"): no element of Index's markup has ref="serch" (did you mean "search"?)`,
		line("pages/index.vuka.html", page, `c.state("shut")`) + `:12: ctx.state("shut"): Index has no ui.State field "shut" (declare the field: shut ui.State[T])`,
		line("pages/index.vuka.html", page, `c.on("sved"`) + `:9: ctx.on("sved"): no method of Index calls PushEvent("sved", …) (did you mean "saved"?)`,
		line("pages/index.vuka.html", page, `c.emit("picked")`) + `:11: Index is a page: ctx.emit("picked") has no parent to reach (call the page's own method, ctx.live.picked(…), or push its event from Go)`,
	}
	slices.Sort(got)
	slices.Sort(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
