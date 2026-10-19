package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goIslandsMain = `package main

import (
	"embed"

	"example.com/app/pages"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/web"
)

//go:embed all:web/dist
var dist embed.FS

type Chart struct {
	Title  string ` + "`json:\"title\"`" + `
	Points []int  ` + "`json:\"points\"`" + `
	Note   *string
}

type Clock struct{ Zone string }

type Old struct{}

func init() {
	ui.RegisterIsland[Chart]()
	ui.RegisterIsland[Clock]("web/islands/Clock.ts", ui.Strategy("idle"))
	ui.Island("./web/islands/Old.ts")(vuka.TypeFor[Old](nil))
}

func main() {
	app := web.New(web.Frontend(dist, "web/dist"))
	app.Live("/board", pages.NewBoard, ui.Island(ui.Layout(nil)))
	app.Run("")
}
`

const goIslandsBoard = `package pages

import "github.com/vuka-lang/ui"

type Board struct {
	ui.Live
	Q string ` + "`json:\"q\"`" + `
}

func NewBoard() *Board { return &Board{} }

func (b *Board) Search(q string) error { b.Q = q; return nil }
`

const islandTS = "export function mount(el: HTMLElement) { el.textContent = 'x'; return { update() {}, unmount() {} } }\n"

// TestGoIslands: the islands a plain Go module registers — ui.RegisterIsland
// bare and with a path, a route's ui.Island option (its constructor's type,
// in another package) and the form older vuka commands generated — are
// found from the source as @ui.Island types are, and vuka build-islands
// builds them into the frontend's build with their types.
func TestGoIslands(t *testing.T) {
	root := tempModule(t, map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web v0.50.0\n",
		"main.go":                 goIslandsMain,
		"pages/board.go":          goIslandsBoard,
		"web/islands/Chart.ts":    islandTS,
		"web/islands/Clock.ts":    islandTS,
		"web/islands/Old.ts":      islandTS,
		"web/islands/Board.ts":    islandTS,
		"web/dist/.gitkeep":       "",
		"pages/board_test.go":     "package pages\n",
		"pages/generated_vuka.go": "package pages\n",
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*islandComp{}
	for _, ic := range pa.islands {
		got[ic.name] = ic
	}
	for name, want := range map[string]struct{ rel, strategy, route string }{
		"Chart": {"web/islands/Chart.ts", "", ""},
		"Clock": {"web/islands/Clock.ts", "idle", ""},
		"Old":   {"web/islands/Old.ts", "", ""},
		"Board": {"web/islands/Board.ts", "", "Live"},
	} {
		ic := got[name]
		if ic == nil {
			t.Errorf("%s not found", name)
			continue
		}
		if ic.rel != want.rel || ic.strategy != want.strategy || ic.route != want.route || ic.diagnostic() != nil {
			t.Errorf("%s: rel %q strategy %q route %q, %v", name, ic.rel, ic.strategy, ic.route, ic.diagnostic())
		}
	}
	if b := got["Board"]; b == nil || !b.live || !b.layout || b.dir != filepath.Join(root, "pages") {
		t.Errorf("Board: %+v", b)
	}
	if len(pa.islands) != 4 {
		t.Errorf("%d islands", len(pa.islands))
	}

	t.Chdir(root)
	var out bytes.Buffer
	if err := buildIslands(nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	mf, err := os.ReadFile(filepath.Join(root, "web/dist/.vuka/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Chart", "Clock", "Old", "Board"} {
		if !bytes.Contains(mf, []byte(`"`+name+`":{"name":"`+name+`","module":"`+strings.ToLower(name)+".")) {
			t.Errorf("manifest lacks %s: %s", name, mf)
		}
	}
	chunks, _ := os.ReadDir(filepath.Join(root, "web/dist/.vuka/i"))
	if len(chunks) != 4 {
		t.Errorf("chunks: %v", chunks)
	}
	dts, _ := os.ReadFile(filepath.Join(root, "web/islands.d.ts"))
	if !strings.Contains(string(dts), "export interface ChartProps {\n\ttitle: string;\n\tpoints: number[];\n\tNote?: string;\n}") {
		t.Errorf("islands.d.ts:\n%s", dts)
	}
	live, _ := os.ReadFile(filepath.Join(root, "web/vuka-live.d.ts"))
	if !strings.Contains(string(live), "export interface Board {") || !strings.Contains(string(live), "search(q: string): Promise<void>;") {
		t.Errorf("vuka-live.d.ts:\n%s", live)
	}
}

// TestGoIslandErrors: a missing component names the call that would name
// it, and arguments the vuka command can't read are E0320.
func TestGoIslandErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"func init() { ui.RegisterIsland[Chart]() }",
			"error[E0310]: island Chart: no component at islands/Chart.{vue,tsx,jsx,svelte,ts,js}\n --> main.go:7\n  = help: add it there, or name the file with ui.RegisterIsland[Chart](\"…\")"},
		{"var p = \"x.vue\"\n\nfunc init() { ui.RegisterIsland[Chart](p) }",
			"error[E0320]: ui.RegisterIsland[Chart] takes a string literal path"},
		{"func init() { ui.RegisterIsland[Missing]() }",
			"error[E0320]: ui.RegisterIsland[Missing]: the island's type isn't a struct of the module"},
	} {
		root := tempModule(t, map[string]string{
			"go.mod":  "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web v0.50.0\n",
			"main.go": "package main\n\nimport \"github.com/vuka-lang/ui\"\n\ntype Chart struct{ N int }\n\n" + c.src + "\n",
		})
		var msg string
		pa, err := findAssets(root, "example.com/app")
		switch {
		case err != nil:
			msg = err.Error()
		case pa != nil && len(pa.islands) == 1 && pa.islands[0].diagnostic() != nil:
			msg = pa.islands[0].diagnostic().Error()
		}
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s:\ngot  %s\nwant %s", c.src, msg, c.want)
		}
	}
	root := tempModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web v0.50.0\n",
		"main.go": "package main\n\nimport \"github.com/vuka-lang/ui\"\n\ntype Chart struct{ N int }\n\nfunc init() { ui.RegisterIsland[Chart]() }\n", "islands/Chart.ts": islandTS})
	pa, err := findAssets(root, "example.com/app")
	if err != nil || pa.islands[0].diagnostic() != nil {
		t.Fatal(err, pa.islands[0].diagnostic())
	}
	if help := pa.islands[0].candidates(); len(help) != len(islandExts) {
		t.Errorf("candidates %v", help)
	}
	t.Chdir(root)
	if err := buildIslands(nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "E0609") {
		t.Errorf("no web.Frontend: %v", err)
	}
}
