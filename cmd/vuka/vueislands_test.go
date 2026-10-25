package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// moduleNameRe mirrors web's accepted /_vuka/i/ chunk name (assets.go
// moduleName): lowercase dotted segments ending in .js.
var moduleNameRe = regexp.MustCompile(`^[a-z0-9]+(?:\.[a-z0-9]+)*\.js$`)

const counterVue = `<script setup lang="ts">
import { ref } from 'vue'
const props = defineProps<{ count: number }>()
const n = ref(props.count)
</script>
<template><button @click="n++">{{ n }}<slot/></button></template>
`

// TestVueIslands builds a .vue island end to end: in a temp frontend root it
// installs vue + esbuild + esbuild-plugin-vue3, declares a Counter.vue island,
// runs the asset compile, and checks that a component (module) chunk and a
// shared adapter chunk are emitted and registered, that the manifest entry
// names the Vue framework and a non-empty adapter, and that the adapter chunk
// exports mount. Skips cleanly when Node / npm are unavailable (as the Phase 1
// island tests skip without the web checkout).
func TestVueIslands(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		needs(t, "node not on PATH")
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		needs(t, "npm not on PATH")
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
	write("package.json", `{"name":"app","private":true,"version":"0.0.0"}`+"\n")
	write("islands/Counter.vue", counterVue)

	install := exec.Command(npm, "install", "-D", "esbuild", "esbuild-plugin-vue3", "vue")
	install.Dir = root
	if out, err := install.CombinedOutput(); err != nil {
		needs(t, "npm install failed (offline?): %v\n%s", err, out)
	}

	pa := &pageAssets{
		root:         root,
		frontendRoot: root,
		global:       &assetComp{},
		islands: []*islandComp{{
			name:      "Counter",
			src:       filepath.Join(root, "islands", "Counter.vue"),
			rel:       "islands/Counter.vue",
			framework: "vue",
			props:     []islandProp{{name: "count", ts: "number"}},
		}},
	}

	out, err := compileAssets(pa, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// The manifest entry names Vue and points at a module and adapter chunk.
	var mf manifestFile
	if err := json.Unmarshal(out.manifest, &mf); err != nil {
		t.Fatalf("manifest %s: %v", out.manifest, err)
	}
	e := mf.Islands["Counter"]
	if e == nil || e.Name != "Counter" || e.Framework != "vue" || e.Module == "" || e.Adapter == "" {
		t.Fatalf("manifest island: %+v (%s)", e, out.manifest)
	}
	if e.Module == e.Adapter {
		t.Fatalf("module and adapter are the same chunk: %s", e.Module)
	}

	// Every emitted chunk is registered as a bundle; the module and adapter
	// chunks (and a shared Vue chunk) are among them.
	byName := map[string][]byte{}
	for _, ib := range out.islands {
		byName[ib.name] = ib.js
		if !moduleNameRe.MatchString(ib.name) {
			t.Errorf("chunk name %q not servable under /_vuka/i/", ib.name)
		}
	}
	if len(out.islands) < 3 {
		t.Errorf("want module + adapter + shared chunk, got %d: %v", len(out.islands), keysOf(byName))
	}
	mod, ok := byName[e.Module]
	if !ok {
		t.Fatalf("module chunk %q not registered; have %v", e.Module, keysOf(byName))
	}
	if !strings.Contains(string(mod), "default") {
		t.Errorf("module chunk has no default export:\n%s", mod)
	}
	adapter, ok := byName[e.Adapter]
	if !ok {
		t.Fatalf("adapter chunk %q not registered; have %v", e.Adapter, keysOf(byName))
	}
	if !strings.Contains(string(adapter), "mount") {
		t.Errorf("adapter chunk has no mount export:\n%s", adapter)
	}

	// assetsGo embeds every chunk via web.RegisterIsland.
	g := string(assetsGo(out, false))
	for name := range byName {
		if !strings.Contains(g, "RegisterIsland(\""+name+"\"") {
			t.Errorf("assetsGo: no RegisterIsland for %q", name)
		}
	}

	// islands.d.ts still comes from the Go struct.
	if dts := string(out.dts); !strings.Contains(dts, "CounterProps") || !strings.Contains(dts, "count: number;") {
		t.Errorf("islands.d.ts:\n%s", dts)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
