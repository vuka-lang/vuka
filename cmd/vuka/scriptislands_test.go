package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scriptIslandPage = "package main\n\n" +
	"import (\n\t\"github.com/vuka-lang/ui/v2\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\n" +
	"@ui.Island\n" +
	"type SalesChart struct {\n\tPoints []int `json:\"points\"`\n}\n\n" +
	"func main() { web.New().Run(\"\") }\n"

// TestScriptIsland: a bare @ui.Island's component is found next to the file
// declaring it (sales_chart.ts for SalesChart) before islands/; its
// island(fn) comes from vuka:island and its bare imports from node_modules,
// bundled with no Node.
func TestScriptIsland(t *testing.T) {
	root := localModule(t, webDir(t), map[string]string{
		"main.vuka": scriptIslandPage,
		"sales_chart.ts": "import { island } from 'vuka:island'\nimport { sum } from 'tiny-sum'\n" +
			"export default island<{ points: number[] }>((el, props) => {\n\tel.textContent = String(sum(props.points))\n" +
			"\treturn { update(next) { el.textContent = String(sum(next.points)) } }\n})\n",
		"islands/SalesChart.vue":             "<template><p/></template>\n",
		"node_modules/tiny-sum/package.json": `{"name":"tiny-sum","main":"index.js"}`,
		"node_modules/tiny-sum/index.js":     "export const sum = (xs) => xs.reduce((a, b) => a + b, 0) + 0.5;\n",
	})
	t.Setenv("PATH", "")
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.islands) != 1 || pa.islands[0].rel != "sales_chart.ts" || pa.islands[0].framework != "vanilla" {
		t.Fatalf("islands: %+v", pa.islands[0])
	}
	out, err := compileAssets(pa, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	js := string(out.islands[0].js)
	if !strings.Contains(js, "mount") || !strings.Contains(js, "+.5") || strings.Contains(js, "vuka:island") {
		t.Errorf("bundle:\n%s", js)
	}
	if _, err := os.Stat(filepath.Join(root, "vuka-island.d.ts")); err != nil {
		t.Errorf("no vuka-island.d.ts: %v", err)
	}
}

// TestReactIslandNoNode: a .tsx island builds with the in-process esbuild,
// React's adapter beside it, with no node on PATH; a .vue one needs Node and
// says so.
func TestReactIslandNoNode(t *testing.T) {
	nm := islandModules(t)
	page := strings.Replace(scriptIslandPage, "@ui.Island\n", "@ui.Island(\"./chart.tsx\")\n", 1)
	root := localModule(t, webDir(t), map[string]string{
		"main.vuka": page,
		"chart.tsx": "export default function Chart({ points }: { points: number[] }) {\n\treturn <b>{points.length}</b>\n}\n",
	})
	os.Symlink(nm, filepath.Join(root, "node_modules"))
	t.Setenv("PATH", "")
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	out, err := compileAssets(pa, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var mf manifestFile
	json.Unmarshal(out.manifest, &mf)
	e := mf.Islands["SalesChart"]
	if e == nil || e.Framework != "react" || !strings.HasPrefix(e.Adapter, "reactadapter.") || !strings.HasPrefix(e.Module, "saleschart.") {
		t.Fatalf("manifest: %s", out.manifest)
	}
	names := map[string]bool{}
	for _, b := range out.islands {
		names[b.name] = true
		if strings.Contains(string(b.js), "chunk-") {
			t.Errorf("%s imports an unrenamed chunk", b.name)
		}
	}
	if !names[e.Adapter] || !names[e.Module] {
		t.Errorf("bundles %v lack %s or %s", names, e.Adapter, e.Module)
	}

	os.WriteFile(filepath.Join(root, "main.vuka"), []byte(strings.Replace(page, "./chart.tsx", "./chart.vue", 1)), 0o644)
	os.WriteFile(filepath.Join(root, "chart.vue"), []byte("<template><p/></template>\n"), 0o644)
	pa, err = findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileAssets(pa, false, io.Discard); err == nil || !strings.Contains(err.Error(), "chart.vue needs Node") || !strings.Contains(err.Error(), ".tsx") {
		t.Errorf("a .vue island without Node: %v", err)
	}
}

// TestDoctor: vuka doctor names what each island builds with, and what
// one is missing.
func TestDoctor(t *testing.T) {
	page := scriptIslandPage + "\n@ui.Island(\"./map.vue\")\ntype Map struct{}\n"
	root := localModule(t, webDir(t), map[string]string{
		"main.vuka":      page,
		"sales_chart.ts": "export function mount() {}\n",
		"map.vue":        "<template><p/></template>\n",
	})
	t.Setenv("PATH", "")
	var out strings.Builder
	err := doctor([]string{root}, &out)
	if err == nil || !strings.Contains(out.String(), "none: .vue and .svelte") {
		t.Errorf("doctor: %v\n%s", err, out.String())
	}
	for _, want := range []string{"island SalesChart  sales_chart.ts  vanilla  in-process esbuild", "island Map", "needs Node: write it as a .tsx or .ts island"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}
}
