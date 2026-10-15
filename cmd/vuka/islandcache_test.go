package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The island build's key changes with what the build reads, and only then.
func TestIslandInputs(t *testing.T) {
	root := t.TempDir()
	fr := filepath.Join(root, "web")
	for p, c := range map[string]string{
		"web/package.json":                    `{"devDependencies": {"vite": "^8"}}`,
		"web/package-lock.json":               "{}",
		"web/vite.config.mts":                 "export default {}",
		"web/app.css":                         "@import \"tailwindcss\";\n@source \"../pages\";\n",
		"web/islands/Chart.vue":               "<template><b/></template>",
		"web/node_modules/vue/index.js":       "x",
		"web/node_modules/.package-lock.json": "{}",
		"web/dist/index.html":                 "built",
		"pages/home.vuka":                     `<p class="text-sm">`,
		"modules/notes/pages.vuka":            "package notes",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	islands := []*islandComp{{name: "Chart", src: filepath.Join(fr, "islands", "Chart.vue"), framework: "vue"}}
	key := func() string { return islandInputs(fr, islands, false) }
	k := key()
	if key() != k {
		t.Fatal("the same inputs, two keys")
	}
	same := func(what string, change func()) {
		t.Helper()
		change()
		if key() != k {
			t.Errorf("%s changed the key", what)
			k = key()
		}
	}
	differs := func(what string, change func()) {
		t.Helper()
		change()
		if n := key(); n == k {
			t.Errorf("%s left the key as it was", what)
		} else {
			k = n
		}
	}
	same("a Go package's source", func() { writeFile(t, filepath.Join(root, "modules/notes/pages.vuka"), "package notes // edited") })
	same("the build's output", func() { writeFile(t, filepath.Join(fr, "dist/index.html"), "built again") })
	same("an installed package's file", func() { writeFile(t, filepath.Join(fr, "node_modules/vue/index.js"), "y") })
	same("a temporary build directory", func() { writeFile(t, filepath.Join(fr, ".vuka-vite-isl-1/o/x.js"), "z") })
	differs("the island's source", func() { writeFile(t, filepath.Join(fr, "islands/Chart.vue"), "<template><i/></template>") })
	differs("another file of the root", func() { writeFile(t, filepath.Join(fr, "components/Btn.vue"), "<template/>") })
	differs("the stylesheet", func() {
		writeFile(t, filepath.Join(fr, "app.css"), "@import \"tailwindcss\";\n@source \"../pages\";\n/* x */\n")
	})
	differs("a file under an @source", func() { writeFile(t, filepath.Join(root, "pages/home.vuka"), `<p class="text-lg">`) })
	differs("the lockfile", func() { writeFile(t, filepath.Join(fr, "package-lock.json"), `{"v": 2}`) })
	differs("the installed packages", func() { writeFile(t, filepath.Join(fr, "node_modules/.package-lock.json"), `{"v": 2}`) })
	differs("the vite config", func() { writeFile(t, filepath.Join(fr, "vite.config.mts"), "export default {plugins: []}") })
	differs("a removed file", func() { os.Remove(filepath.Join(fr, "components/Btn.vue")) })
	if islandInputs(fr, islands, true) == k {
		t.Error("a development build has a production build's key")
	}
	islands = append(islands, &islandComp{name: "Clock", src: filepath.Join(fr, "islands", "Clock.tsx"), framework: "react"})
	if key() == k {
		t.Error("another island left the key as it was")
	}
	if got := cssSources(fr); len(got) != 1 || !strings.HasSuffix(got[0], "pages") {
		t.Errorf("cssSources: %v", got)
	}
}
