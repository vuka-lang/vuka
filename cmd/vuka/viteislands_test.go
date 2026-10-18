package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// spikeManifest is a real Vite island-build manifest (the proven spike): the
// Vue adapter entry (adapter.ts → vue.*.js), an island entry (entry.ts →
// card.*.js), a CSS entry (app.css → styles.*.css), and the shared runtime
// chunk the JS entries import.
const spikeManifest = `{
  "_chunk.BSsxhQjU.js": { "file": "chunk.BSsxhQjU.js", "name": "runtime-core.esm-bundler" },
  "adapter.ts": { "file": "vue.BHG6ctf6.js", "name": "vue", "src": "adapter.ts", "isEntry": true, "imports": ["_chunk.BSsxhQjU.js"] },
  "app.css": { "file": "styles.CKJN6eaY.css", "name": "styles", "src": "app.css", "isEntry": true },
  "entry.ts": { "file": "card.DfZaRFxT.js", "name": "card", "src": "entry.ts", "isEntry": true, "imports": ["_chunk.BSsxhQjU.js"] }
}`

// TestViteIslandManifest checks the manifest-reading logic without a Vite
// build: each source key resolves to its emitted chunk file, which is what
// buildViteIslands registers under /_vuka/i/.
func TestViteIslandManifest(t *testing.T) {
	man, err := parseViteManifest([]byte(spikeManifest))
	if err != nil {
		t.Fatal(err)
	}
	fileOf := func(relKey string) string {
		e, ok := man[relKey]
		if !ok {
			return ""
		}
		return filepath.Base(e.File)
	}
	if got := fileOf("adapter.ts"); got != "vue.BHG6ctf6.js" {
		t.Errorf("adapter chunk = %q, want vue.BHG6ctf6.js", got)
	}
	if got := fileOf("entry.ts"); got != "card.DfZaRFxT.js" {
		t.Errorf("island chunk = %q, want card.DfZaRFxT.js", got)
	}
	if got := fileOf("app.css"); got != "styles.CKJN6eaY.css" {
		t.Errorf("css chunk = %q, want styles.CKJN6eaY.css", got)
	}
	// The island entry pulls in the shared runtime chunk, which is served
	// alongside it (registering every emitted file makes the relative import
	// resolve under /_vuka/i/).
	if imps := man["entry.ts"].Imports; len(imps) != 1 || imps[0] != "_chunk.BSsxhQjU.js" {
		t.Errorf("island imports = %v, want [_chunk.BSsxhQjU.js]", imps)
	}
}

// TestHasViteConfig: a frontend root with a vite.config.* takes the Vite
// island path; one without keeps the esbuild path.
func TestHasViteConfig(t *testing.T) {
	dir := t.TempDir()
	if hasViteConfig(dir) {
		t.Fatal("empty dir reported as a Vite project")
	}
	if err := os.WriteFile(filepath.Join(dir, "vite.config.mts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasViteConfig(dir) {
		t.Fatal("vite.config.mts not recognised")
	}
}

// TestViteIslandsE2E builds a Nuxt UI island through Vite, end to end. It is
// skipped unless VUKA_VITE_NM points at a node_modules directory that already
// has vite, @vitejs/plugin-vue, @nuxt/ui, @tailwindcss/vite, tailwindcss, vue
// and vue-router installed (a full Nuxt UI install is too large for CI). It
// sets up a frontend root reusing those deps, places the vuka Vite plugin,
// declares a .vue island using <UButton>, runs the asset compile, and checks
// that the island + adapter chunks and a CSS chunk are produced and that the
// manifest names the Vue framework.
func TestViteIslandsE2E(t *testing.T) {
	nm := fixtureModules("VUKA_VITE_NM", "vite")
	if nm == "" {
		needs(t, "set VUKA_VITE_NM to a node_modules with vite + @nuxt/ui installed (or npm ci --prefix testdata/node/vite)")
	}
	plugin := vitePlugin()
	if plugin == "" {
		needs(t, "set VUKA_PLUGIN to the vuka vite plugin (vuka-web/vite/vuka.js)")
	}

	root := t.TempDir()
	write := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(nm, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	pluginSrc, err := os.ReadFile(plugin)
	if err != nil {
		t.Fatal(err)
	}
	write("vuka/vite.js", string(pluginSrc))
	write("vite.config.mts", `import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import tailwindcss from '@tailwindcss/vite'
import vuka from './vuka/vite.js'
export default defineConfig({ plugins: [vue(), ui(), tailwindcss(), vuka()] })
`)
	write("app.css", "@import \"tailwindcss\";\n@import \"@nuxt/ui\";\n")
	write("islands/Card.vue", `<script setup lang="ts">
import { defineAsyncComponent, ref } from 'vue'
defineProps<{ label: string }>()
const Lazy = defineAsyncComponent(() => import('./Lazy.vue'))
const word = ref('')
import('./word').then((m) => { word.value = m.word })
</script>
<template><UButton color="primary" variant="soft">{{ label }}</UButton><Lazy /><i class="word">{{ word }}</i></template>
`)
	write("islands/Lazy.vue", `<template><b class="lazy">lazy</b></template>
<style>.lazy { color: rgb(1, 2, 3) }</style>
`)
	write("islands/word.ts", "export const word = 'dynamic'\n")

	pa := &pageAssets{
		root:         root,
		frontendRoot: root,
		global:       &assetComp{},
		islands: []*islandComp{{
			name:      "Card",
			src:       filepath.Join(root, "islands", "Card.vue"),
			rel:       "islands/Card.vue",
			framework: "vue",
			props:     []islandProp{{name: "label", ts: "string"}},
		}},
	}

	out, err := compileAssets(pa, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	var mf manifestFile
	if err := json.Unmarshal(out.manifest, &mf); err != nil {
		t.Fatalf("manifest %s: %v", out.manifest, err)
	}
	e := mf.Islands["Card"]
	if e == nil || e.Framework != "vue" || e.Module == "" || e.Adapter == "" || e.Module == e.Adapter {
		t.Fatalf("manifest island: %+v (%s)", e, out.manifest)
	}
	if len(mf.IslandCSS) == 0 {
		t.Fatalf("no island CSS in manifest: %s", out.manifest)
	}

	byName := map[string][]byte{}
	for _, ib := range out.islands {
		byName[ib.name] = ib.js
		if !islandFileRe.MatchString(ib.name) {
			t.Errorf("chunk/css name %q not servable under /_vuka/i/", ib.name)
		}
	}
	if _, ok := byName[e.Module]; !ok {
		t.Fatalf("island chunk %q not registered", e.Module)
	}
	if _, ok := byName[e.Adapter]; !ok {
		t.Fatalf("adapter chunk %q not registered", e.Adapter)
	}
	css := mf.IslandCSS[0]
	for _, c := range mf.IslandCSS {
		if strings.HasPrefix(c, "styles.") {
			css = c
		}
	}
	cssBytes, ok := byName[css]
	if !ok {
		t.Fatalf("css chunk %q not registered", css)
	}
	// Nuxt UI's design tokens and Tailwind land in the CSS.
	if s := string(cssBytes); !strings.Contains(s, "--ui-") && !strings.Contains(s, "tailwind") && len(s) < 1000 {
		t.Errorf("island CSS looks empty (%d bytes)", len(s))
	}
	if !strings.Contains(string(byName[e.Adapter]), "mount") {
		t.Errorf("adapter chunk has no mount export")
	}

	// In a browser, from a page under a sub-path: the lazy component, its
	// CSS chunk and the dynamic import load from /_vuka/i/, nothing else is
	// asked for.
	if chromeBinary() == "" {
		return
	}
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if name, ok := strings.CutPrefix(r.URL.Path, "/_vuka/i/"); ok {
			if b, ok := byName[name]; ok {
				ct := "text/javascript"
				if strings.HasSuffix(name, ".css") {
					ct = "text/css"
				}
				w.Header().Set("Content-Type", ct)
				w.Write(b)
				return
			}
		}
		if r.URL.Path == "/sub/page" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><div id="host"></div><script type="module">
const [m, a] = await Promise.all([import("/_vuka/i/%s"), import("/_vuka/i/%s")])
a.mount(m.default, document.getElementById("host"), { label: "hi" }, {})
</script>`, e.Module, e.Adapter)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ch := launchChrome(t)
	p := ch.open(srv.URL+"/sub/page", 800, 600)
	p.wait(`(document.querySelector("#host .lazy") || {}).textContent === "lazy" && document.querySelector("#host .word").textContent === "dynamic"`)
	p.wait(`getComputedStyle(document.querySelector("#host .lazy")).color === "rgb(1, 2, 3)"`)
	mu.Lock()
	defer mu.Unlock()
	for _, a := range asked {
		if a != "/sub/page" && a != "/favicon.ico" && !strings.HasPrefix(a, "/_vuka/i/") {
			t.Errorf("asked for %s, outside /_vuka/i/ (all: %v)", a, asked)
		}
	}
}

// islandFileRe mirrors web's /_vuka/i/ name check (assets.go islandFile):
// mixed-case dotted segments ending in .js or .css.
var islandFileRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*\.(?:js|css)$`)

// TestDevAssetsViteRebuild: under vuka run the Vite-built islands are built
// again when a shared component an island imports, the vite.config or the
// CSS entry changes — not only the island's own file. Skipped unless
// VUKA_VITE_NM (a node_modules with vite, @vitejs/plugin-vue and vue) and
// VUKA_PLUGIN are set.
func TestDevAssetsViteRebuild(t *testing.T) {
	nm, plugin := fixtureModules("VUKA_VITE_NM", "vite"), vitePlugin()
	if nm == "" || plugin == "" {
		needs(t, "set VUKA_VITE_NM (or npm ci --prefix testdata/node/vite) and VUKA_PLUGIN")
	}
	root := t.TempDir()
	fr := filepath.Join(root, "web")
	pluginSrc, err := os.ReadFile(plugin)
	if err != nil {
		t.Fatal(err)
	}
	config := func(label string) string {
		return "import { defineConfig } from 'vite'\nimport vue from '@vitejs/plugin-vue'\nimport vuka from './vuka/vite.js'\n" +
			"export default defineConfig({ define: { __LABEL__: JSON.stringify(" + strconv.Quote(label) + ") }, plugins: [vue(), vuka()] })\n"
	}
	for p, c := range map[string]string{
		"web/package.json":       `{"type": "module"}`,
		"web/vuka/vite.js":       string(pluginSrc),
		"web/vite.config.mts":    config("one"),
		"web/app.css":            ".card { color: red }\n",
		"web/components/Btn.vue": "<template><button>first</button></template>\n",
		"web/islands/Card.vue":   "<script setup lang=\"ts\">\nimport Btn from '../components/Btn.vue'\nconst label = __LABEL__\n</script>\n<template><div class=\"card\">{{ label }}<Btn/></div></template>\n",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	if err := os.Symlink(nm, filepath.Join(fr, "node_modules")); err != nil {
		t.Fatal(err)
	}
	pa := &pageAssets{root: root, frontendRoot: fr, global: &assetComp{}, islands: []*islandComp{{
		name: "Card", src: filepath.Join(fr, "islands", "Card.vue"), rel: "web/islands/Card.vue", framework: "vue",
	}}}
	var log syncBuffer
	d := newDevAssets(root, &log)
	defer d.close()
	d.set(pa)
	manifest := func() string {
		b, _ := os.ReadFile(filepath.Join(d.dir(), assetsManifest))
		return string(b)
	}
	first := manifest()
	if !strings.Contains(first, `"Card"`) {
		t.Fatalf("no island built: %s\n%s", first, log.String())
	}
	at := time.Now()
	for _, c := range []struct{ what, file, data string }{
		{"a shared component", "web/components/Btn.vue", "<template><button>second</button></template>\n"},
		{"the vite.config", "web/vite.config.mts", config("two")},
		{"the CSS entry", "web/app.css", ".card { color: blue }\n"},
	} {
		before := manifest()
		f := filepath.Join(root, filepath.FromSlash(c.file))
		writeFile(t, f, c.data)
		at = at.Add(time.Second)
		os.Chtimes(f, at, at)
		for end := time.Now().Add(60 * time.Second); manifest() == before; time.Sleep(100 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s: the islands weren't built again\n%s", c.what, log.String())
			}
		}
	}
}
