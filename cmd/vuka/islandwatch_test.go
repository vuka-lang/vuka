package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Under vuka run a framework island is built again when any file its build
// reads changes — the frontend root's config, stylesheet, shared
// components, package.json, lockfile, a directory its stylesheet @sources —
// and not for installed packages, build output, generated declarations or
// Go sources; the island cache's key changes with each such file too.
func TestDevAssetsWatchFrontend(t *testing.T) {
	root := t.TempDir()
	fr := filepath.Join(root, "web")
	for p, c := range map[string]string{
		"web/package.json":                    `{"devDependencies": {"vite": "^8"}}`,
		"web/package-lock.json":               "{}",
		"web/vite.config.mts":                 "export default {}",
		"web/app.css":                         "@import \"tailwindcss\";\n@source \"../pages\";\n",
		"web/_setup.ts":                       "export default () => {}",
		"web/islands/Chart.vue":               "<template><Btn/></template>",
		"web/islands/admin/Users.vue":         "<template><b/></template>",
		"web/components/Btn.vue":              "<template><button/></template>",
		"web/node_modules/vue/index.js":       "x",
		"web/node_modules/.package-lock.json": "{}",
		"web/dist/index.html":                 "built",
		"web/islands.d.ts":                    "",
		"web/vuka-live.d.ts":                  "",
		"web/components.d.ts":                 "",
		"pages/home.vuka":                     `<p class="text-sm">`,
		"pages/extra.html":                    `<p class="text-sm">`,
		"modules/notes/pages.vuka":            "package notes",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	islands := []*islandComp{
		{name: "Chart", src: filepath.Join(fr, "islands", "Chart.vue"), framework: "vue"},
		{name: "Users", src: filepath.Join(fr, "islands", "admin", "Users.vue"), framework: "vue"},
	}
	pa := &pageAssets{root: root, frontendRoot: fr, global: &assetComp{}, islands: islands}
	trees := islandTrees(pa)
	if !slices.Equal(trees, []string{fr, filepath.Join(root, "pages")}) {
		t.Fatalf("trees: %v", trees)
	}
	d := &devAssets{trees: trees, tree: scanTrees(trees), files: map[string]time.Time{}}
	key := islandInputs(fr, islands, true)
	tick := time.Now()
	write := func(p, c string) {
		f := filepath.Join(root, filepath.FromSlash(p))
		writeFile(t, f, c)
		tick = tick.Add(time.Second)
		os.Chtimes(f, tick, tick)
	}
	check := func(what string, want bool, change func()) {
		t.Helper()
		change()
		d.treeAt = time.Time{}
		if got := d.stale(); got != want {
			t.Errorf("%s: rebuild %v, want %v", what, got, want)
		}
		d.tree = scanTrees(d.trees)
		if k := islandInputs(fr, islands, true); want && k == key {
			t.Errorf("%s: the island cache's key is as it was", what)
		} else {
			key = k
		}
	}
	check("a shared component", true, func() { write("web/components/Btn.vue", "<template><button>x</button></template>") })
	check("the vite config", true, func() { write("web/vite.config.mts", "export default {plugins: []}") })
	check("the stylesheet", true, func() { write("web/app.css", "@import \"tailwindcss\";\n@source \"../pages\";\n/* x */\n") })
	check("an island in a folder", true, func() { write("web/islands/admin/Users.vue", "<template><i/></template>") })
	check("a new component", true, func() { write("web/components/New.vue", "<template/>") })
	check("a removed component", true, func() { os.Remove(filepath.Join(fr, "components", "New.vue")) })
	check("the _setup", true, func() { write("web/_setup.ts", "export default (app) => {}") })
	check("package.json", true, func() { write("web/package.json", `{"devDependencies": {"vite": "^8.1"}}`) })
	check("the lockfile", true, func() { write("web/package-lock.json", `{"v": 2}`) })
	check("a file under an @source", true, func() { write("pages/extra.html", `<p class="text-lg">`) })
	check("an installed package", false, func() { write("web/node_modules/vue/index.js", "y") })
	check("the build's output", false, func() { write("web/dist/index.html", "again") })
	check("vuka's declarations", false, func() { write("web/islands.d.ts", "x"); write("web/vuka-live.d.ts", "x") })
	check("a plugin's declarations", false, func() { write("web/components.d.ts", "x") })
	check("a Vite temporary build", false, func() { write("web/.vuka-vite-isl-1/o/a.js", "x") })
	check("Vite's bundled config", false, func() { write("web/vite.config.mts.timestamp-1-ab.mjs", "x") })
	check("a .vuka file (the app's watcher's)", false, func() { write("pages/home.vuka", `<p class="text-xl">`) })

	// A vanilla island watches exactly the files its bundle read.
	if trees := islandTrees(&pageAssets{root: root, global: &assetComp{}, islands: []*islandComp{{name: "C", src: filepath.Join(root, "islands", "C.ts"), framework: "vanilla"}}}); len(trees) != 0 {
		t.Errorf("vanilla trees: %v", trees)
	}
}
