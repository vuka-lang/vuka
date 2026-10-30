package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestComponentDTS: a scripted component's struct types its script — its
// data fields as props, its methods on live, its fields' handles and its
// states — and a script misusing them doesn't type-check.
func TestComponentDTS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "chart.go"), []byte(`package views

import "github.com/vuka-lang/ui/v2"

type Point struct {
	X, Y int
}

type Chart struct {
	ui.Live
	Title    string
	Points   []Point `+"`json:\"points\"`"+`
	Q        string  `+"`bind:\"\"`"+`
	Zoom     func(int)
	Children ui.Node
	open     ui.State[bool]
}

func (c *Chart) Reorder(ids []int) error { return nil }
`), 0o644)
	c := &assetComp{dir: dir, name: "Chart", scripts: []assetSrc{{inline: "x"}}}
	dts := string(componentDTS(dir, "", []*assetComp{c}))
	for _, want := range []string{"Title: string;", "points: Point[];", "reorder(ids: number[]): Promise<void>;", "q: LiveBound<string>;", "open: boolean;", "export interface Point {", `type Chart = import("vuka:component").Chart;`} {
		if !strings.Contains(dts, want) {
			t.Errorf("no %q in\n%s", want, dts)
		}
	}
	for _, bad := range []string{"Zoom", "Children"} {
		if strings.Contains(dts, bad) {
			t.Errorf("%s is sent:\n%s", bad, dts)
		}
	}
	tsc := os.Getenv("VUKA_TSC")
	if nm := fixtureModules("VUKA_TS_NM", "islands"); tsc == "" && nm != "" {
		tsc = filepath.Join(nm, ".bin", "tsc")
	}
	if tsc == "" {
		if tsc, _ = exec.LookPath("tsc"); tsc == "" {
			t.Skip("no tsc ($VUKA_TSC, or npm ci --prefix testdata/node/islands)")
		}
	}
	os.WriteFile(filepath.Join(dir, componentDTSName), []byte(dts), 0o644)
	check := func(script string) string {
		os.WriteFile(filepath.Join(dir, "s.ts"), []byte(`/// <reference path="./`+componentDTSName+`" />
`+script), 0o644)
		out, _ := exec.Command(tsc, "--noEmit", "--strict", "--lib", "es2020,dom", "--target", "es2020", "--moduleResolution", "bundler", "--module", "esnext", filepath.Join(dir, "s.ts")).CombinedOutput()
		return string(out)
	}
	if out := check(`import { component, page } from 'vuka:component'
export default component<Chart>((el, props, ctx) => {
	el.title = props.Title + props.points.map((p) => p.X).join()
	ctx.live.reorder([1, 2])
	ctx.state("open").set(!ctx.state("open").get())
	ctx.navigate("/x")
	return { update(next) { el.title = next.Title } }
})
export const p = page<Chart>((el, live) => { live.q.set(live.q.value + "!") })
`); out != "" {
		t.Errorf("a correct script doesn't type-check:\n%s", out)
	}
	if out := check(`import { component } from 'vuka:component'
export default component<Chart>((el, props, ctx) => {
	props.Zoom
	ctx.state("shut")
	ctx.live.reorder("x")
})
`); !strings.Contains(out, "Zoom") || !strings.Contains(out, `"shut"`) || !strings.Contains(out, "reorder") && !strings.Contains(out, "number[]") {
		t.Errorf("a wrong script type-checks:\n%s", out)
	}
}

// TestScriptLivePushes: a page script's live.reorder(…) and live.q.set(…)
// are pushes AllowPush lists (Reorder, _set); a name that is no method
// isn't.
func TestScriptLivePushes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "orders.go"), []byte("package views\n\ntype Orders struct{ Q string }\n\nfunc (o *Orders) Reorder(ids []int) {}\n"), 0o644)
	c := &assetComp{dir: dir, name: "Orders", live: true, scripts: []assetSrc{{inline: "export default page((el, live) => { live.reorder([1]); live.q.set('x'); live.q.get(); live.navigate('/') })", label: "orders.vuka.html:3"}}}
	got, err := scriptPushes(c)
	if err != nil || strings.Join(got, ",") != "Reorder,_set" {
		t.Fatalf("pushes %v %v", got, err)
	}
}

// tscCheck type-checks script, a file beside the d.ts in dir, and returns
// tsc's complaints; the test skips without tsc.
func tscCheck(t *testing.T, dir, script string) string {
	t.Helper()
	tsc := os.Getenv("VUKA_TSC")
	if nm := fixtureModules("VUKA_TS_NM", "islands"); tsc == "" && nm != "" {
		tsc = filepath.Join(nm, ".bin", "tsc")
	}
	if tsc == "" {
		if tsc, _ = exec.LookPath("tsc"); tsc == "" {
			t.Skip("no tsc ($VUKA_TSC, or npm ci --prefix testdata/node/islands)")
		}
	}
	os.WriteFile(filepath.Join(dir, "s.ts"), []byte(`/// <reference path="./`+componentDTSName+`" />
`+script), 0o644)
	out, _ := exec.Command(tsc, "--noEmit", "--strict", "--lib", "es2020,dom", "--target", "es2020", "--moduleResolution", "bundler", "--module", "esnext", filepath.Join(dir, "s.ts")).CombinedOutput()
	return string(out)
}

// TestComponentDTSPage: a page's script is a component script —
// component<T> with props, ctx.state, ctx.ref, ctx.on, ctx.live — and
// page<T> its alias, (el, live, ctx); a page has no parent, so ctx.emit is
// a type error there and not on a component.
func TestComponentDTSPage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.go"), []byte(`package views

import "github.com/vuka-lang/ui/v2"

type Index struct {
	ui.Live
	Q    string
	open ui.State[bool]
}

// Bump counts, and tells the script.
func (p *Index) Bump() { p.PushEvent("bumped", map[string]int{"n": 1}) }

type Card struct{ Title string }
`), 0o644)
	page := &assetComp{dir: dir, name: "Index", page: true, scripts: []assetSrc{{inline: "x"}}}
	card := &assetComp{dir: dir, name: "Card", scripts: []assetSrc{{inline: "x"}}}
	dts := string(componentDTS(dir, "", []*assetComp{page, card}))
	if !strings.Contains(dts, "readonly __page?: true;") || strings.Count(dts, "__page?: true") != 1 {
		t.Fatalf("Index isn't the one page:\n%s", dts)
	}
	os.WriteFile(filepath.Join(dir, componentDTSName), []byte(dts), 0o644)
	if out := tscCheck(t, dir, `import { component, page } from 'vuka:component'
export default component<Index>((el, props, ctx) => {
	el.title = props.Q
	ctx.live.bump()
	ctx.state("open").set(!ctx.state("open").get())
	ctx.on("bumped", (p) => { el.title = p.n.toFixed() })
	ctx.patch("?q=x")
	return { update(next) { el.title = next.Q }, destroy() {} }
})
export const p2 = page<Index>((el, live) => { live.bump() })
export const p3 = page<Index>((el, live, ctx) => { ctx.state("open").get(); return { update(next) { next.Q } } })
export const c = component<Card>((el, props, ctx) => { ctx.emit("picked", props.Title) })
`); out != "" {
		t.Errorf("a correct script doesn't type-check:\n%s", out)
	}
	if out := tscCheck(t, dir, `import { component } from 'vuka:component'
export default component<Index>((el, props, ctx) => { ctx.emit("x") })
`); !strings.Contains(out, "emit") {
		t.Errorf("ctx.emit on a page type-checks:\n%s", out)
	}
}

// A page's form and its password fields aren't props: ui.Props leaves them
// out, so its script's type has neither (vuka#229).
func TestComponentDTSNoSecrets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "join.go"), []byte(`package views

import "github.com/vuka-lang/ui/v2"

type Signup struct {
	ui.FormState
	Email    string
	Password string `+"`input:\"new-password\"`"+`
}

type Wrapped struct{ Signup }

type Card struct {
	Holder string
	PIN    string `+"`input:\"password\"`"+`
}

type Join struct {
	ui.Live
	Title  string
	Form   Signup
	Again  *Wrapped
	Model  *ui.ModelForm[Card]
	Card   Card
	Secret string `+"`input:\"current-password\"`"+`
}
`), 0o644)
	c := &assetComp{dir: dir, name: "Join", scripts: []assetSrc{{inline: "x"}}}
	dts := string(componentDTS(dir, "", []*assetComp{c}))
	for _, want := range []string{"Title: string;", "Card: Card;", "Holder: string;"} {
		if !strings.Contains(dts, want) {
			t.Errorf("no %q in\n%s", want, dts)
		}
	}
	for _, bad := range []string{"Form", "Again", "Model", "PIN", "Secret", "Password"} {
		if strings.Contains(dts, bad+":") || strings.Contains(dts, bad+"?:") {
			t.Errorf("%s is typed as a prop:\n%s", bad, dts)
		}
	}
}
