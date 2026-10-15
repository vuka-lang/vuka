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
	"strings"
	"testing"
)

const pagesApp = `package main

import (
	"context"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

type DB struct{ names []string }

func (d *DB) Find(q string) []string {
	var out []string
	for _, n := range d.names {
		if q == "" || n[:1] == q[:1] {
			out = append(out, n)
		}
	}
	return out
}

@web.Get("/live")
@ui.Island("./components/Board.vue")
type Board struct {
	ui.Live
	Q     string   ` + "`json:\"q\" url:\"q\"`" + `
	Rows  []string ` + "`json:\"rows\"`" + `
	Count int      ` + "`json:\"count\"`" + `
	db    *DB
}

func (b *Board) Mount(ctx context.Context) error {
	b.Subscribe("board")
	b.Rows = b.db.Find(b.Q)
	return nil
}

func (b *Board) Search(ctx context.Context, q string) error {
	if len(q) < 2 {
		return web.Invalid{"q": "two letters at least"}
	}
	b.Q, b.Rows = q, b.db.Find(q)
	return nil
}

func (b *Board) Shout(ctx context.Context) error { return web.Broadcast(ctx, "board", "hi") }

func (b *Board) Update(msg string) { b.Count++ }

@web.Get("/static")
@ui.Island("./components/Board.vue")
type Static struct {
	Q     string   ` + "`json:\"q\" url:\"q\"`" + `
	Rows  []string ` + "`json:\"rows\"`" + `
	Count int      ` + "`json:\"count\"`" + `
	db    *DB
}

func (s *Static) Mount(ctx context.Context) { s.Rows = s.db.Find(s.Q) }

func (s *Static) Search(ctx context.Context, q string) error {
	if len(q) < 2 {
		return web.Invalid{"q": "two letters at least"}
	}
	s.Q, s.Rows = q, s.db.Find(q)
	return nil
}

func (s *Static) Shout() { s.Count++ }

@web.Get("/react")
@ui.Island("./components/RBoard.tsx")
type RBoard struct {
	ui.Live
	Q     string   ` + "`json:\"q\" url:\"q\"`" + `
	Rows  []string ` + "`json:\"rows\"`" + `
	Count int      ` + "`json:\"count\"`" + `
	db    *DB
}

func (b *RBoard) Mount() { b.Rows = b.db.Find(b.Q) }

func (b *RBoard) Search(q string) error {
	if len(q) < 2 {
		return web.Invalid{"q": "two letters at least"}
	}
	b.Q, b.Rows = q, b.db.Find(q)
	return nil
}

func (b *RBoard) Shout() { b.Count++ }

@web.Get("/svelte")
@ui.Island("./components/SBoard.svelte")
type SBoard struct {
	Q     string   ` + "`json:\"q\" url:\"q\"`" + `
	Rows  []string ` + "`json:\"rows\"`" + `
	Count int      ` + "`json:\"count\"`" + `
	db    *DB
}

func (b *SBoard) Mount() { b.Rows = b.db.Find(b.Q) }

func (b *SBoard) Search(q string) error {
	if len(q) < 2 {
		return web.Invalid{"q": "two letters at least"}
	}
	b.Q, b.Rows = q, b.db.Find(q)
	return nil
}

func (b *SBoard) Shout() { b.Count++ }

func main() {
	web.New(web.Provide(func() *DB { return &DB{names: []string{"Ada", "Alan", "Grace", "Guido"}} })).Run("")
}
`

const boardVue = `<script setup lang="ts">
import { useLive } from 'vuka:live'
import type { Board } from 'vuka:live'
const props = defineProps<Board>()
const live = useLive<Board>()
const w = window as any
w.__mounts = (w.__mounts || 0) + 1
</script>
<template><div id="b"><span class="q">{{ q }}</span><span class="rows">{{ rows.join(',') }}</span><span class="count">{{ count }}</span><span class="err">{{ live.error ? live.error.fields?.q : '' }}</span><span class="pending">{{ live.pending }}</span><button class="search" @click="live.search('Gr')">s</button><button class="bad" @click="live.search('G').catch(() => {})">b</button><button class="shout" @click="live.shout()">x</button></div></template>
`

const rboardTSX = `import { useLive } from 'vuka:live'
import type { RBoard as T } from 'vuka:live'
export default function RBoard(props: { q: string; rows: string[]; count: number }) {
  const live = useLive<T>()
  const w = window as any
  if (!w.__counted) { w.__counted = true; w.__mounts = (w.__mounts || 0) + 1 }
  return <div id="b"><span className="q">{props.q}</span><span className="rows">{props.rows.join(',')}</span><span className="count">{props.count}</span><span className="err">{live.error ? live.error.fields?.q : ''}</span><button className="search" onClick={() => live.search('Gr')}>s</button><button className="bad" onClick={() => live.search('G').catch(() => {})}>b</button><button className="shout" onClick={() => live.shout()}>x</button></div>
}
`

const sboardSvelte = `<script lang="ts">
  import { live } from 'vuka:live'
  let { q, rows, count }: { q: string; rows: string[]; count: number } = $props()
  const l = live()
  const w: any = window
  w.__mounts = (w.__mounts || 0) + 1
</script>
<div id="b"><span class="q">{q}</span><span class="rows">{rows.join(',')}</span><span class="count">{count}</span><span class="err">{$l.error ? $l.error.fields?.q : ''}</span><button class="search" onclick={() => l.search('Gr')}>s</button><button class="bad" onclick={() => l.search('G').catch(() => {})}>b</button><button class="shout" onclick={() => l.shout()}>x</button></div>
`

// TestIslandPagesE2E: pages backed by a Vue component, live and stateless:
// the props are the exported fields, ?q= binds Q, the component's
// useLive() calls the methods — live events on the live page, signed calls
// on the stateless one — and the new props update it without a remount,
// the address following Q; a broadcast's Update reaches the live page's
// component; a field error shows as live.error. vuka-live.d.ts types them.
func TestIslandPagesE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs an app")
	}
	if chromeBinary() == "" {
		t.Skip("no Chrome (set VUKA_CHROME)")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	nm := islandModules(t)
	root := assetsModule(t, map[string]string{"main.vuka": pagesApp, "components/Board.vue": boardVue,
		"tsconfig.json": `{"compilerOptions": {"strict": true, "jsx": "react-jsx", "paths": {"vuka:live": ["./vuka-live"]}}}`, "components/RBoard.tsx": rboardTSX, "components/SBoard.svelte": sboardSvelte})
	if err := os.Symlink(nm, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)

	// The types the component imports.
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	d := newDevAssets(root, io.Discard)
	d.set(pa)
	d.close()
	dts, _ := os.ReadFile(filepath.Join(root, "vuka-live.d.ts"))
	for _, want := range []string{"export interface Board {", "\tq: string;", "\trows: string[];",
		"search(q: string): Promise<void>;", "shout(): Promise<void>;", "export interface Static {", "export declare function useLive<T>(): Live<T>;"} {
		if !strings.Contains(string(dts), want) {
			t.Errorf("vuka-live.d.ts lacks %q:\n%s", want, dts)
		}
	}
	if strings.Contains(string(dts), "db") || strings.Contains(string(dts), "update(") || strings.Contains(string(dts), "mount(") {
		t.Errorf("vuka-live.d.ts has the server's own: %s", dts)
	}

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
	waitFor(t, func() bool { res, err := http.Get(base + "/static"); return err == nil && res.Body.Close() == nil })

	ch := launchChrome(t)
	for _, path := range []string{"/live", "/static", "/react", "/svelte"} {
		t.Run(path, func(t *testing.T) {
			p := ch.open(base+path+"?q=A", 900, 700)
			if path == "/live" || path == "/react" {
				p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
			}
			p.wait(`(document.querySelector("#b .rows") || {}).textContent === "Ada,Alan"`)
			if q := p.text("#b .q"); q != "A" {
				t.Errorf("q = %q", q)
			}
			p.click("#b .search")
			p.wait(`document.querySelector("#b .rows").textContent === "Grace,Guido"`)
			p.wait(`location.search === "?q=Gr"`)
			p.click("#b .bad")
			p.wait(`document.querySelector("#b .err").textContent === "two letters at least"`)
			p.click("#b .shout")
			p.wait(`document.querySelector("#b .count").textContent === "1"`)
			if n := p.eval(`window.__mounts`); n != float64(1) {
				t.Errorf("mounted %v times", n)
			}
			if t.Failed() {
				t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
			}
		})
	}
}
