package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A page-backed island's component using vuka:live with no types: the
// shape a plain-Go app's component has when vuka-live.d.ts isn't generated.
const untypedLiveVue = `<script setup lang="ts">
import { useLive } from 'vuka:live'
const props = defineProps(['q', 'rows'])
const live = useLive()
</script>
<template><input v-model="live.q.value"><p v-for="r in props.rows" :key="r.id" @click="live.pick(r.id)">{{ r.name }}</p></template>
`

// TestIslandUntyped: without vuka-live.d.ts the island builds, vuka:live
// coming from the build itself; only typed use (defineProps<T>() of its
// types) needs the generated file.
func TestIslandUntyped(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		needs(t, "node not on PATH")
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		needs(t, "npm not on PATH")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"app","private":true,"version":"0.0.0"}`+"\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "islands"), 0o755)
	os.WriteFile(filepath.Join(root, "islands", "Levels.vue"), []byte(untypedLiveVue), 0o644)
	install := exec.Command(npm, "install", "--prefer-offline", "--no-audit", "--no-fund", "-D", "esbuild", "esbuild-plugin-vue3", "vue")
	install.Dir = root
	if out, err := install.CombinedOutput(); err != nil {
		needs(t, "npm install failed (offline?): %v\n%s", err, out)
	}
	islands := []*islandComp{{name: "Levels", src: filepath.Join(root, "islands", "Levels.vue"), rel: "islands/Levels.vue", framework: "vue"}}
	bundles, entries, err := buildVueIslands(root, islands, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "vuka-live.d.ts")); err == nil {
		t.Fatal("the build wrote vuka-live.d.ts")
	}
	e := entries["Levels"]
	if e == nil || e.Module == "" || e.Adapter == "" {
		t.Fatalf("manifest island: %+v", e)
	}
	var js strings.Builder
	for _, b := range bundles {
		js.Write(b.js)
	}
	if !strings.Contains(js.String(), "vuka:live") {
		t.Fatal("the chunks don't read vuka:live from the adapter")
	}
}
