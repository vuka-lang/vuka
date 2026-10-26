package main

import (
	"os"
	"path/filepath"
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
