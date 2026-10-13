package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	nm := os.Getenv("VUKA_VITE_NM")
	if nm == "" {
		t.Skip("set VUKA_VITE_NM to a node_modules with vite + @nuxt/ui installed")
	}
	plugin := os.Getenv("VUKA_PLUGIN")
	if plugin == "" {
		t.Skip("set VUKA_PLUGIN to the vuka vite plugin (vuka-web/vite/vuka.js)")
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
defineProps<{ label: string }>()
</script>
<template><UButton color="primary" variant="soft">{{ label }}</UButton></template>
`)

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
}

// islandFileRe mirrors web's /_vuka/i/ name check (assets.go islandFile):
// mixed-case dotted segments ending in .js or .css.
var islandFileRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*\.(?:js|css)$`)
