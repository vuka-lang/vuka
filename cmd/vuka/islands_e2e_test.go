package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// islandModules is a node_modules with the island frameworks and their
// esbuild plugins: $VUKA_ISLAND_NM, else installed (npm, offline first)
// into a temp dir; the test skips without it.
func islandModules(t *testing.T) string {
	t.Helper()
	if nm := fixtureModules("VUKA_ISLAND_NM", "islands"); nm != "" {
		return nm
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		needs(t, "no npm")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","private":true}`), 0o644)
	cmd := exec.Command(npm, "install", "--prefer-offline", "--no-audit", "--no-fund", "-D",
		"esbuild", "esbuild-plugin-vue3", "esbuild-svelte", "vue", "react", "react-dom", "svelte")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		needs(t, "npm install failed (offline?): %v\n%s", err, out)
	}
	return filepath.Join(dir, "node_modules")
}

const islandsPage = `package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"

	RCounter "../islands/RCounter.tsx"
	SCounter "../islands/SCounter.svelte"
	VCounter "../islands/VCounter.vue"
)

type Page struct {
	ui.Live
	total float64
	n     ui.State[float64]
}

@ui.Mount
func (p *Page) mount(ctx context.Context) error { p.n = 1.0; return nil }

func (p *Page) Add(by float64) { p.total += by }
<main>
	<output id="n">{n}</output>
	<output id="total">{total}</output>
	<button id="set" onClick={n = 5}>5</button>
	<VCounter label="vue" total={total} count={n} onAdd={p.Add} client:load />
	<RCounter label="react" total={total} count={n} onAdd={p.Add} client:load />
	<SCounter label="svelte" total={total} count={n} onadd={p.Add} client:load />
	<VCounter label="once" total={total} once client:load />
</main>
`

const pagesMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes()).Run("") }
`

const mounted = `const w = window as any; w.__mounts = w.__mounts || {}; w.__mounts[props.label] = (w.__mounts[props.label] || 0) + 1`

var islandFiles = map[string]string{
	"islands/VCounter.vue": `<script setup lang="ts">
const props = defineProps<{ label: string; total: number; onAdd?: (by: number) => void }>()
const count = defineModel<number>('count')
` + mounted + `
</script>
<template><div :id="label"><span class="total">{{ total }}</span><span class="count">{{ count }}</span><button class="inc" @click="count = (count ?? 0) + 1">+</button><button class="add" @click="onAdd?.(2)">add</button></div></template>
`,
	"islands/RCounter.tsx": `import { useEffect } from 'react'
type Props = { label: string; total: number; count?: number; setCount?: (n: number) => void; onAdd?: (by: number) => void }
export default function RCounter(props: Props) {
  const { label, total, count = 0, setCount, onAdd } = props
  useEffect(() => { ` + mounted + ` }, [])
  return <div id={label}><span className="total">{total}</span><span className="count">{count}</span><button className="inc" onClick={() => setCount?.(count + 1)}>+</button><button className="add" onClick={() => onAdd?.(2)}>add</button></div>
}
`,
	"islands/SCounter.svelte": `<script lang="ts">
  import { onMount } from 'svelte'
  let { label, total, count = $bindable(0), onadd }: { label: string; total: number; count?: number; onadd?: (by: number) => void } = $props()
  onMount(() => { const w: any = window; w.__mounts = w.__mounts || {}; w.__mounts[label] = (w.__mounts[label] || 0) + 1 })
</script>
<div id={label}><span class="total">{total}</span><span class="count">{count}</span><button class="inc" onclick={() => count = count + 1}>+</button><button class="add" onclick={() => onadd?.(2)}>add</button></div>
`,
}

// TestIslandsE2E builds an app whose live page imports a Vue, a React and a
// Svelte island, and drives it in Chrome: a function prop pushes a live
// event, a state prop is two-way (the island's write reaches the page's
// binding and the other islands, the page's write reaches the islands),
// changed server props update the islands without a remount, and once
// keeps its first props.
func TestIslandsE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs an app")
	}
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	if _, err := exec.LookPath("node"); err != nil {
		needs(t, "no node")
	}
	nm := islandModules(t)
	files := map[string]string{"main.go": pagesMain, "pages/index.vuka.html": islandsPage}
	for k, v := range islandFiles {
		files[k] = v
	}
	root := assetsModule(t, files)
	if err := os.Symlink(nm, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), assetsEnv+"=")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	waitFor(t, func() bool { res, err := http.Get(url); return err == nil && res.Body.Close() == nil })

	ch := launchChrome(t)
	p := ch.open(url, 900, 700)
	labels := []string{"vue", "react", "svelte"}
	all := func(sel, want string) {
		t.Helper()
		for _, l := range labels {
			p.wait(fmt.Sprintf(`(document.querySelector("#%s %s") || {}).textContent === %q`, l, sel, want))
		}
	}
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	all(".count", "1")
	all(".total", "0")

	// Each island writes the state: the page's binding and the other islands follow.
	for i, l := range labels {
		want := fmt.Sprint(2 + i)
		p.click("#" + l + " .inc")
		p.wait(fmt.Sprintf(`document.querySelector("#n").textContent === %q`, want))
		all(".count", want)
	}
	// The page writes it: the islands follow.
	p.click("#set")
	all(".count", "5")

	// A function prop pushes the live event; the new total reaches every
	// island as a live prop, with no remount; once keeps its first props.
	for i, l := range labels {
		p.click("#" + l + " .add")
		want := fmt.Sprint(2 * (i + 1))
		p.wait(fmt.Sprintf(`document.querySelector("#total").textContent === %q`, want))
		all(".total", want)
	}
	if got := p.text("#once .total"); got != "0" {
		t.Errorf("once island total = %q, want 0", got)
	}
	for _, l := range append(labels, "once") {
		if n := p.eval(fmt.Sprintf(`window.__mounts[%q]`, l)); n != float64(1) {
			t.Errorf("%s mounted %v times", l, n)
		}
	}
	if t.Failed() {
		t.Logf("app log:\n%s", logs.String())
	}
}
