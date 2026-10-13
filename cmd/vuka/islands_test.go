package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const islandPage = "package main\n\n" +
	"import (\n\t\"github.com/vuka-lang/ui\"\n\t\"github.com/vuka-lang/web\"\n)\n\n" +
	"@ui.Island(\"islands/Counter.ts\")\n" +
	"type Counter struct {\n" +
	"\tCount    int      `json:\"count\"`\n" +
	"\tLabel    string   `json:\"label\"`\n" +
	"\tTags     []string `json:\"tags\"`\n" +
	"\tNote     *string  `json:\"note\"`\n" +
	"\tHidden   int      `json:\"-\"`\n" +
	"\tinternal int\n" +
	"\tChildren ui.Node\n" +
	"}\n\n" +
	"func main() { web.New().Run(\"\") }\n"

const counterTS = "import { twice } from \"./lib\";\n" +
	"export function mount(el: HTMLElement, props: { count: number }) {\n" +
	"\tel.textContent = String(twice(props.count));\n" +
	"\treturn { update(p: { count: number }) { el.textContent = String(p.count) }, unmount() {} };\n" +
	"}\n"

// TestIslands: @ui.Island is discovered with its type name, component path and
// vanilla framework; the asset compile bundles the component under i/, records
// it in the manifest with web's islandAsset shape, and generates islands.d.ts
// from the Go struct (pointer → optional, []T → T[], json:"-" and the slot left
// out).
func TestIslands(t *testing.T) {
	root := localModule(t, webDir(t), map[string]string{
		"main.vuka":          islandPage,
		"islands/Counter.ts": counterTS,
		"islands/lib.ts":     "export const twice = (n: number): number => n * 2;\n",
	})

	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.islands) != 1 {
		t.Fatalf("islands: %+v", pa.islands)
	}
	ic := pa.islands[0]
	if ic.name != "Counter" || ic.framework != "vanilla" || ic.rel != "islands/Counter.ts" {
		t.Fatalf("island: %+v", ic)
	}

	out, err := compileAssets(pa, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// (a) a chunk is bundled under i/.
	if len(out.islands) != 1 {
		t.Fatalf("bundles: %+v", out.islands)
	}
	ib := out.islands[0]
	if !regexp.MustCompile(`^counter\.[0-9a-f]{8}\.js$`).MatchString(ib.name) {
		t.Errorf("chunk name %q", ib.name)
	}
	if js := string(ib.js); !strings.Contains(js, "mount") || strings.Contains(js, "import") || !strings.Contains(js, "*2") {
		t.Errorf("chunk not bundled/minified:\n%s", js)
	}

	// (b) the manifest's islands entry: right name, module, framework.
	var mf manifestFile
	if err := json.Unmarshal(out.manifest, &mf); err != nil {
		t.Fatalf("manifest %s: %v", out.manifest, err)
	}
	e := mf.Islands["Counter"]
	if e == nil || e.Name != "Counter" || e.Module != ib.name || e.Framework != "vanilla" || e.Adapter != "" {
		t.Fatalf("manifest island: %+v (%s)", e, out.manifest)
	}

	// (c) islands.d.ts: the props interface from the Go struct.
	dts := string(out.dts)
	for _, want := range []string{
		"export interface CounterProps {",
		"count: number;",
		"label: string;",
		"tags: string[];",
		"note?: string;",
		"export interface IslandProps {",
		"Counter: CounterProps;",
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("islands.d.ts lacks %q:\n%s", want, dts)
		}
	}
	for _, no := range []string{"Children", "Hidden", "internal"} {
		if strings.Contains(dts, no) {
			t.Errorf("islands.d.ts should not mention %q:\n%s", no, dts)
		}
	}

	// assetsGo embeds the chunk via web.RegisterIsland.
	if g := string(assetsGo(out)); !strings.Contains(g, "RegisterIsland(\""+ib.name+"\"") {
		t.Errorf("assetsGo: no RegisterIsland:\n%s", g)
	}
}

// TestIslandsErrors: a framework still awaiting a later phase is reported; a
// missing component file is reported.
func TestIslandsErrors(t *testing.T) {
	web := webDir(t)
	for _, c := range []struct{ name, decl, file, want string }{
		{"framework", `@ui.Island("islands/Chart.svelte")`, "islands/Chart.svelte", "island Chart: svelte islands need a later phase (vanilla and Vue are supported)"},
		{"missing", `@ui.Island("islands/Gone.ts")`, "", "island Gone: component islands/Gone.ts not found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			typeName := "Chart"
			if c.name == "missing" {
				typeName = "Gone"
			}
			src := "package main\n\nimport (\n\t\"github.com/vuka-lang/ui\"\n\t\"github.com/vuka-lang/web\"\n)\n\n" +
				c.decl + "\ntype " + typeName + " struct{ N int `json:\"n\"` }\n\nvar _ = ui.Node(nil)\n\nfunc main() { web.New().Run(\"\") }\n"
			files := map[string]string{"main.vuka": src}
			if c.file != "" {
				files[c.file] = "<div/>\n"
			}
			root := localModule(t, web, files)
			pa, err := findAssets(root, "example.com/app")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := compileAssets(pa, false, io.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %s", err, c.want)
			}
		})
	}
}

// TestFindFrontendRoot: the island build is rooted at web.FrontendRoot when
// given, else the parent of web.Frontend's root argument, else the module root.
func TestFindFrontendRoot(t *testing.T) {
	for _, c := range []struct{ name, call, want string }{
		{"frontendroot", `web.Frontend(dist, "frontend/dist", web.FrontendRoot("frontend"))`, "frontend"},
		{"parent", `web.Frontend(dist, "frontend/dist")`, "frontend"},
		{"nested", `web.Frontend(dist, "web/app/dist")`, "web/app"},
		{"root dist", `web.Frontend(dist, "dist")`, ""},
		{"none", ``, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := "func main() { web.New().Run(\"\") }\n"
			if c.call != "" {
				body = "func main() { web.New(" + c.call + ").Run(\"\") }\n"
			}
			src := "package main\n\nimport (\n\t\"embed\"\n\t\"github.com/vuka-lang/web\"\n)\n\n//go:embed all:dist\nvar dist embed.FS\n\n" + body
			root := tempModule(t, map[string]string{"go.mod": webMod, "main.go": src})
			got := findFrontendRoot(root)
			want := root
			if c.want != "" {
				want = filepath.Join(root, filepath.FromSlash(c.want))
			}
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestIslandStructFields unit-checks the Go→TS field mapping.
func TestIslandStructFields(t *testing.T) {
	src := "package main\n\ntype P struct {\n" +
		"\tA, B int\n" +
		"\tF    float64 `json:\"f\"`\n" +
		"\tOn   bool\n" +
		"\tM    map[string]int\n" +
		"\tOpt  *string\n" +
		"\tlc   int\n" +
		"}\n"
	full, _ := scanFile("p.go", []byte(src))
	got := structFields(full, "P")
	want := []islandProp{
		{"A", "number", false}, {"B", "number", false},
		{"f", "number", false}, {"On", "boolean", false},
		{"M", "Record<string, number>", false}, {"Opt", "string", true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("fields:\n got %+v\nwant %+v", got, want)
	}
}

// TestIslandDevAssets: vuka run writes each island chunk under the assets
// directory's i/ and islands.d.ts into the resolved frontend root (here
// web.Frontend's parent, "frontend").
func TestIslandDevAssets(t *testing.T) {
	src := "package main\n\n" +
		"import (\n\t\"embed\"\n\t\"github.com/vuka-lang/ui\"\n\t\"github.com/vuka-lang/web\"\n)\n\n" +
		"//go:embed all:frontend/dist\nvar dist embed.FS\n\n" +
		"@ui.Island(\"islands/Counter.ts\")\ntype Counter struct {\n\tCount int `json:\"count\"`\n\tChildren ui.Node\n}\n\n" +
		"func main() { web.New(web.Frontend(dist, \"frontend/dist\")).Run(\"\") }\n"
	root := localModule(t, webDir(t), map[string]string{
		"main.vuka":              src,
		"islands/Counter.ts":     counterTS,
		"islands/lib.ts":         "export const twice = (n: number): number => n * 2;\n",
		"frontend/dist/.gitkeep": "",
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "frontend"); pa.frontendRoot != want {
		t.Fatalf("frontendRoot %q, want %q", pa.frontendRoot, want)
	}
	d := newDevAssets(root, io.Discard)
	defer d.close()
	d.set(pa)

	dts := filepath.Join(root, "frontend", "islands.d.ts")
	data, err := os.ReadFile(dts)
	if err != nil || !strings.Contains(string(data), "CounterProps") {
		t.Fatalf("islands.d.ts at %s: %v\n%s", dts, err, data)
	}
	ient, _ := os.ReadDir(filepath.Join(root, ".vuka", "assets", "i"))
	if len(ient) != 1 || !regexp.MustCompile(`^counter\.[0-9a-f]{8}\.js$`).MatchString(ient[0].Name()) {
		t.Fatalf("i/: %v", ient)
	}
}

// webDir is the local web checkout the island tests build against, skipped when
// it has no RegisterIsland.
func webDir(t *testing.T) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "RegisterIsland") {
		t.Skipf("github.com/vuka-lang/web at %s has no RegisterIsland; set VUKA_WEB (and VUKA_UI)", web)
	}
	return web
}
