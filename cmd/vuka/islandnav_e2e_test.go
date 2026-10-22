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

const navLayout = `package pages

import "github.com/vuka-lang/ui/v2"

type Layout struct{ Children ui.Node }
<html><head><meta charset="utf-8" /><title>admin</title></head><body><nav><ui.Link href="/pa" ID="to-a">A</ui.Link><ui.Link href="/pb" ID="to-b">B</ui.Link></nav><main>{Children}</main></body></html>
`

const navPageA = `package pages

import (
	"context"
	"time"

	"github.com/vuka-lang/ui/v2"

	A "../islands/PageA.vue"
)

type PageA struct {
	ui.Live
	Title string   ` + "`json:\"title\"`" + `
	Stats []string ` + "`json:\"stats\" defer:\"\"`" + `
}

@ui.Mount
func (a *PageA) mount(ctx context.Context) error { a.Title = "page a"; return nil }

@ui.Loads("stats")
func (a *PageA) loadStats(ctx context.Context) error {
	select {
	case <-time.After(600 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	a.Stats = []string{"x", "y"}
	return nil
}
<A title={Title} stats={Stats} client:load />
`

const navPageB = `package pages

import (
	"context"
	"time"

	"github.com/vuka-lang/ui/v2"

	B "../islands/PageB.vue"
)

type PageB struct {
	ui.Live
	Title string   ` + "`json:\"title\"`" + `
	Rows  []string ` + "`json:\"rows\" defer:\"\"`" + `
}

@ui.Mount
func (b *PageB) mount(ctx context.Context) error { b.Title = "page b"; return nil }

@ui.Loads("rows")
func (b *PageB) loadRows(ctx context.Context) error {
	select {
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	b.Rows = []string{"r"}
	return nil
}
<B title={Title} rows={Rows} client:load />
`

func navVueSrc(name string) string {
	return `<script setup lang="ts">
import { defineAsyncComponent, onMounted, onBeforeUnmount } from 'vue'
import { useLive, VukaLink } from 'vuka:live'
const live = useLive<any>()
const Lazy = defineAsyncComponent(() => import('./Lazy.vue'))
const w = window as any
onMounted(() => (w.__log = w.__log || []).push('mount ` + name + `'))
onBeforeUnmount(() => w.__log.push('unmount ` + name + `'))
</script>
<template><section id="p` + name + `"><h1>{{ live.title.value }}</h1><Lazy /><Teleport to="body"><template v-for="x in ['` + name + `1', '` + name + `2']" :key="x"><i class="tp">{{ x }}</i></template></Teleport><VukaLink href="/pa" class="la">a</VukaLink><VukaLink href="/pb" class="lb">b</VukaLink></section></template>
`
}

// TestIslandNavigateE2E: two live pages backed by Vue components sharing a
// layout, each with deferred fields, a lazily loaded child and content
// teleported to the body (a fragment, as a popover's portal); moving
// between them over the connection (the layout's links, the component's
// VukaLink, quickly, before the deferred loads land, and back) leaves one
// app on the host each time, no Vue warning or error, and asks for nothing
// outside /_vuka/i/ but the pages.
func TestIslandNavigateE2E(t *testing.T) {
	nm := fixtureModules("VUKA_VITE_NM", "vite")
	if nm == "" {
		needs(t, "set VUKA_VITE_NM to a node_modules with vite, @vitejs/plugin-vue and vue (or npm ci --prefix testdata/node/vite)")
	}
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain,
		"pages/_layout.vuka.html": navLayout,
		"pages/pa.vuka.html":      navPageA,
		"pages/pb.vuka.html":      navPageB,
		"package.json":            `{"name":"app","private":true,"type":"module"}`,
		"vite.config.mts":         "import { defineConfig } from 'vite'\nimport vue from '@vitejs/plugin-vue'\nimport vuka from './vuka/vite.js'\nexport default defineConfig({ base: '/elsewhere/', plugins: [vue(), vuka()] })\n",
		"app.css":                 "",
		"islands/PageA.vue":       navVueSrc("A"),
		"islands/PageB.vue":       navVueSrc("B"),
		"islands/Lazy.vue":        "<template><b class=\"lazy\">lazy</b></template>\n<style>.lazy { color: rgb(1, 2, 3) }</style>\n",
		"tsconfig.json":           `{"compilerOptions": {"strict": true, "paths": {"vuka:live": ["./vuka-live"]}}}`,
	})
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
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/pa"); return err == nil && res.Body.Close() == nil })

	ch := launchChrome(t)
	p := ch.open("about:blank", 900, 700)
	p.do("Page.enable", map[string]any{})
	p.do("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.__errs = [];
for (const k of ["error", "warn"]) { const f = console[k]; console[k] = (...a) => { window.__errs.push(a.map(String).join(" ")); f.apply(console, a) } }
addEventListener("error", (e) => window.__errs.push("error " + e.message));
addEventListener("unhandledrejection", (e) => window.__errs.push("rejection " + e.reason));`})
	p.goTo(base + "/pa")
	connected := `document.documentElement.getAttribute("data-vk-state") === "connected"`
	p.wait(connected)
	p.wait(`(document.querySelector("#pA h1") || {}).textContent === "page a" && !!document.querySelector("#pA .lazy")`)
	p.eval(`window.__marker = 1`)

	one := func(name string) {
		t.Helper()
		p.wait(`(document.querySelector("#p` + name + ` h1") || {}).textContent === "page ` + map[string]string{"A": "a", "B": "b"}[name] + `" && !!document.querySelector("#p` + name + ` .lazy")`)
		if n := p.eval(`document.querySelectorAll("main section").length`); n != float64(1) {
			t.Errorf("%d sections on the page", int(n.(float64)))
		}
	}
	// The layout's link, before A's deferred load lands.
	p.click("#to-b")
	one("B")
	p.click("#pB .la")
	one("A")
	// Quickly, one after another.
	p.eval(`document.querySelector("#to-b").click(); document.querySelector("#to-a").click(); document.querySelector("#to-b").click()`)
	p.wait(`location.pathname === "/pb"`)
	one("B")
	p.eval(`history.back()`)
	p.wait(`location.pathname === "/pa"`)
	one("A")
	p.click("#to-b")
	one("B")
	p.wait(`!document.documentElement.hasAttribute("data-vk-navigating")`)
	// Let the deferred loads of every page visited land.
	p.eval(`new Promise((r) => setTimeout(r, 900))`)
	one("B")

	if m := p.eval(`window.__marker`); m != float64(1) {
		t.Errorf("the page reloaded")
	}
	if n := p.eval(`[...document.querySelectorAll("body > i.tp")].map((e) => e.textContent).join(",")`); n != "B1,B2" {
		t.Errorf("teleported content in the body: %v", n)
	}
	if errs, _ := p.eval(`window.__errs`).([]any); len(errs) > 0 {
		t.Errorf("console: %v", errs)
	}
	mounts, _ := p.eval(`window.__log`).([]any)
	t.Logf("log: %v", mounts)
	open := map[string]int{}
	for _, e := range mounts {
		switch s := e.(string); s {
		case "mount A", "mount B":
			open[s[6:]]++
		default:
			open[s[8:]]--
		}
	}
	if open["A"] != 0 || open["B"] != 1 {
		t.Errorf("apps left mounted: %v (log %v)", open, mounts)
	}
	if t.Failed() {
		t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
	}
}
