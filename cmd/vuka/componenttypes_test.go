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
