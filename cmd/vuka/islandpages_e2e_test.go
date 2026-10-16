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
	"strconv"
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

@web.Get("/find")
@ui.Island("./components/Find.vue")
type Find struct {
	ui.Live
	Q       string   ` + "`" + `url:"q" bind:"q,debounce=150"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *Find) Mount()   { f.Rows = f.db.Find(f.Q) }
func (f *Find) Changed() { f.Changes++; f.Rows = f.db.Find(f.Q) }
func (f *Find) Drop()    { f.Rows = nil }

@web.Get("/findstatic")
@ui.Island("./components/Find.vue")
type FindStatic struct {
	Q       string   ` + "`" + `url:"q" bind:"q,debounce=150"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *FindStatic) Mount() { f.Rows = f.db.Find(f.Q) }

func (f *FindStatic) Changed(ctx context.Context) error {
	f.Changes++
	f.Rows = f.db.Find(f.Q)
	return nil
}

func (f *FindStatic) Drop() { f.Rows = nil }

@web.Get("/rfind")
@ui.Island("./components/RFind.tsx")
type RFind struct {
	ui.Live
	Q       string   ` + "`" + `url:"q" bind:"q,debounce=150"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *RFind) Mount()   { f.Rows = f.db.Find(f.Q) }
func (f *RFind) Changed() { f.Changes++; f.Rows = f.db.Find(f.Q) }
func (f *RFind) Drop()    { f.Rows = nil }

@web.Get("/sfind")
@ui.Island("./components/SFind.svelte")
type SFind struct {
	Q       string   ` + "`" + `url:"q" bind:"q,debounce=150"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *SFind) Mount()   { f.Rows = f.db.Find(f.Q) }
func (f *SFind) Changed() { f.Changes++; f.Rows = f.db.Find(f.Q) }
func (f *SFind) Drop()    { f.Rows = nil }

@web.Get("/only")
@ui.Island("./components/Only.vue")
type Only struct {
	ui.Live
	Q       string   ` + "`" + `bind:"q,debounce=150" validate:"max=5"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *Only) Mount()   { f.Rows = f.db.Find(f.Q) }
func (f *Only) Changed() { f.Changes++; f.Rows = f.db.Find(f.Q) }

@web.Get("/onlystatic")
@ui.Island("./components/Only.vue")
type OnlyStatic struct {
	Q       string   ` + "`" + `bind:"q,debounce=150" validate:"max=5"` + "`" + `
	Rows    []string
	Changes int
	db      *DB
}

func (f *OnlyStatic) Mount()   { f.Rows = f.db.Find(f.Q) }
func (f *OnlyStatic) Changed() { f.Changes++; f.Rows = f.db.Find(f.Q) }

@web.Get("/nav")
@ui.Island("./components/Nav.vue")
type Nav struct {
	ui.Live
	Q       string ` + "`" + `json:"q" url:"q"` + "`" + `
	Updates int    ` + "`" + `json:"updates"` + "`" + `
}

type NavParams struct {
	web.URLParams
	Q string ` + "`" + `url:"q"` + "`" + `
}

func (n *Nav) Mount()             {}
func (n *Nav) Update(p NavParams) { n.Q = p.Q; n.Updates++ }

@web.Get("/nav2")
@ui.Island("./components/Nav2.vue")
type Nav2 struct {
	ui.Live
	Title string ` + "`" + `json:"title"` + "`" + `
}

func (n *Nav2) Mount() { n.Title = "second" }

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

const rboardTSX = `import { useLive, VukaLink } from 'vuka:live'
import type { RBoard as T } from 'vuka:live'
export default function RBoard(props: { q: string; rows: string[]; count: number }) {
  const live = useLive<T>()
  const w = window as any
  if (!w.__counted) { w.__counted = true; w.__mounts = (w.__mounts || 0) + 1 }
  return <div id="b"><span className="q">{props.q}</span><span className="rows">{props.rows.join(',')}</span><span className="count">{props.count}</span><span className="err">{live.error ? live.error.fields?.q : ''}</span><button className="search" onClick={() => live.search('Gr')}>s</button><button className="bad" onClick={() => live.search('G').catch(() => {})}>b</button><button className="shout" onClick={() => live.shout()}>x</button><VukaLink href="/react" className="lnk">here</VukaLink><VukaLink href="/nav" patch>nav</VukaLink></div>
}
`

const sboardSvelte = `<script lang="ts">
  import { live, VukaLink } from 'vuka:live'
  let { q, rows, count }: { q: string; rows: string[]; count: number } = $props()
  const l = live()
  const w: any = window
  w.__mounts = (w.__mounts || 0) + 1
</script>
<div id="b"><span class="q">{q}</span><span class="rows">{rows.join(',')}</span><span class="count">{count}</span><span class="err">{$l.error ? $l.error.fields?.q : ''}</span><button class="search" onclick={() => l.search('Gr')}>s</button><button class="bad" onclick={() => l.search('G').catch(() => {})}>b</button><button class="shout" onclick={() => l.shout()}>x</button><VukaLink href="/svelte" class="lnk">here</VukaLink><VukaLink href="/nav" patch>nav</VukaLink></div>
`

const findVue = `<script setup lang="ts">
import { useLive } from 'vuka:live'
import type { Find } from 'vuka:live'
const live = useLive<Find>()
const w = window as any
w.__mounts = (w.__mounts || 0) + 1
</script>
<template><div id="f"><input class="q" v-model="live.q.value" /><span class="rows">{{ (live.rows.value || []).join(',') }}</span><span class="changes">{{ live.changes.get() }}</span><span class="pending">{{ live.drop.pending }}</span><button class="drop" @click="live.drop()">d</button></div></template>
`

const rfindTSX = `import { useLive } from 'vuka:live'
import type { RFind as T } from 'vuka:live'
export default function RFind() {
  const live = useLive<T>()
  const w = window as any
  if (!w.__counted) { w.__counted = true; w.__mounts = (w.__mounts || 0) + 1 }
  return <div id="f"><input className="q" value={live.q.value} onChange={(e) => live.q.set(e.target.value)} /><span className="rows">{(live.rows.value || []).join(',')}</span><span className="changes">{live.changes.get()}</span><span className="pending">{String(live.drop.pending)}</span><button className="drop" onClick={() => live.drop()}>d</button></div>
}
`

const sfindSvelte = `<script lang="ts">
  import { live } from 'vuka:live'
  import type { SFind } from 'vuka:live'
  const l = live<SFind>()
  const w: any = window
  w.__mounts = (w.__mounts || 0) + 1
</script>
<div id="f"><input class="q" bind:value={l.q.value} /><span class="rows">{(l.rows.value || []).join(',')}</span><span class="changes">{l.changes.get()}</span><span class="pending">{$l.pending}</span><button class="drop" onclick={() => l.drop()}>d</button></div>
`

const onlyVue = `<script setup lang="ts">
import { useLive } from 'vuka:live'
import type { Only } from 'vuka:live'
const live = useLive<Only>()
</script>
<template><div id="o"><input class="q" v-model="live.q.value" /><span class="err">{{ live.q.error }}</span><span class="rows">{{ (live.rows.value || []).join(',') }}</span><span class="changes">{{ live.changes.get() }}</span></div></template>
`

const navVue = `<script setup lang="ts">
import { ref } from 'vue'
import { useLive, useVuka, VukaLink } from 'vuka:live'
import type { Nav } from 'vuka:live'
defineProps<Nav>()
const live = useLive<Nav>()
const vuka = useVuka()
const clicks = ref(0)
const w = window as any
w.__mounts = (w.__mounts || 0) + 1
const go = () => live.navigate('/nav2').then((r) => { w.__went = r.url })
const patch = () => live.patch('/nav?q=b').then((r) => { w.__patched = r.patch })
const replace = () => vuka.patch('/nav?q=r', { replace: true })
</script>
<template><div id="n"><span class="q">{{ q }}</span><span class="updates">{{ updates }}</span><span class="clicks">{{ clicks }}</span>
<button class="inc" @click="clicks++">+</button><button class="go" @click="go">go</button><button class="patch" @click="patch">p</button><button class="replace" @click="replace">r</button>
<VukaLink href="/nav?q=c" patch class="plink" exact>c</VukaLink><VukaLink href="/nav" class="here">here</VukaLink><VukaLink href="/nav2" class="lnk">two</VukaLink></div></template>
`

const nav2Vue = `<script setup lang="ts">
import { VukaLink } from 'vuka:live'
defineProps<{ title: string }>()
const w = window as any
w.__mounts2 = (w.__mounts2 || 0) + 1
</script>
<template><div id="n2"><span class="title">{{ title }}</span><VukaLink href="/nav" class="back">back</VukaLink></div></template>
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
		"tsconfig.json": `{"compilerOptions": {"strict": true, "jsx": "react-jsx", "paths": {"vuka:live": ["./vuka-live"]}}}`, "components/RBoard.tsx": rboardTSX, "components/SBoard.svelte": sboardSvelte,
		"components/Find.vue": findVue, "components/RFind.tsx": rfindTSX, "components/SFind.svelte": sfindSvelte, "components/Only.vue": onlyVue,
		"components/Nav.vue": navVue, "components/Nav2.vue": nav2Vue})
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
		"search(q: string): Promise<void>;", "shout(): Promise<void>;", "export interface Static {", "export declare function useLive<T>(): Live<T>;",
		"\t\tq: LiveBound<string>;\n\t\trows: LiveField<string[]>;\n\t\tchanges: LiveField<number>;", "\t\tdrop(): Promise<void>;"} {
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
			if path == "/react" || path == "/svelte" {
				// VukaLink: an <a> marked for the runtime, active on its own page.
				got := p.eval(`[...document.querySelectorAll("#b a")].map((a) => [a.getAttribute("href"), a.getAttribute("data-vk-link"), a.getAttribute("class"), a.getAttribute("aria-current"), a.textContent])`)
				want := []any{[]any{path, "", "lnk active", "page", "here"}, []any{"/nav", "patch", nil, nil, "nav"}}
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("VukaLinks = %v, want %v", got, want)
				}
			}
			if t.Failed() {
				t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
			}
		})
	}

	// A page's component moves the page: live.patch keeps it mounted, with
	// its own state, and Update runs; live.navigate and a VukaLink go to
	// another page over the connection (the document stays: a marker on
	// window survives), its island mounting and the old one leaving.
	t.Run("navigation", func(t *testing.T) {
		p := ch.open(base+"/nav?q=a", 900, 700)
		connected := `document.documentElement.getAttribute("data-vk-state") === "connected"`
		p.wait(connected)
		p.wait(`(document.querySelector("#n .q") || {}).textContent === "a"`)
		p.eval(`window.__marker = 1`)
		if got := p.eval(`[...document.querySelectorAll("#n a")].map((a) => a.className + "|" + (a.getAttribute("aria-current") || ""))`); fmt.Sprint(got) != "[plink| here active|page lnk|]" {
			t.Errorf("active classes = %v", got)
		}
		p.click("#n .inc")
		p.click("#n .inc")
		p.click("#n .patch")
		p.wait(`document.querySelector("#n .q").textContent === "b" && window.__patched === true`)
		if c, u := p.text("#n .clicks"), p.text("#n .updates"); c != "2" || u != "1" {
			t.Errorf("after live.patch: clicks %s, updates %s", c, u)
		}
		p.click("#n .plink")
		p.wait(`document.querySelector("#n .q").textContent === "c" && location.search === "?q=c"`)
		p.wait(`document.querySelector("#n .plink").className === "plink active"`)
		before := p.eval(`history.length`)
		p.click("#n .replace")
		p.wait(`document.querySelector("#n .q").textContent === "r" && location.search === "?q=r"`)
		if after := p.eval(`history.length`); after != before {
			t.Errorf("replace added an entry: %v -> %v", before, after)
		}
		if c, m := p.text("#n .clicks"), p.eval(`window.__mounts`); c != "2" || m != float64(1) {
			t.Errorf("patched: clicks %s, mounted %v times", c, m)
		}

		// A ctrl- or meta-click is the browser's (a new tab): not taken.
		for _, k := range []string{"ctrlKey", "metaKey"} {
			prevented := p.eval(`(() => { let was; const stop = (e) => { was = e.defaultPrevented; e.preventDefault() }; addEventListener("click", stop); document.querySelector("#n .lnk").dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, ` + k + `: true})); removeEventListener("click", stop); return was })()`)
			if prevented != false {
				t.Errorf("%s-click taken by the runtime", k)
			}
		}
		if p.eval(`document.documentElement.hasAttribute("data-vk-navigating")`) != false || p.eval(`location.pathname`) != "/nav" {
			t.Errorf("a modifier click moved the page")
		}

		p.click("#n .go")
		p.wait(`(document.querySelector("#n2 .title") || {}).textContent === "second"`)
		p.wait(`window.__went === location.href`)
		if m, gone, path := p.eval(`window.__marker`), p.eval(`!document.querySelector("#n")`), p.eval(`location.pathname`); m != float64(1) || gone != true || path != "/nav2" {
			t.Errorf("live.navigate: marker %v, old island gone %v, at %v", m, gone, path)
		}
		p.wait(connected)
		p.click("#n2 .back")
		p.wait(`(document.querySelector("#n .q") || {}).textContent === "" && location.pathname === "/nav"`)
		p.click("#n .lnk")
		p.wait(`(document.querySelector("#n2 .title") || {}).textContent === "second"`)
		if m, a, b := p.eval(`window.__marker`), p.eval(`window.__mounts`), p.eval(`window.__mounts2`); m != float64(1) || a != float64(2) || b != float64(2) {
			t.Errorf("VukaLinks: marker %v, mounts %v and %v", m, a, b)
		}
		p.eval(`history.back()`)
		p.wait(`(document.querySelector("#n .q") || {}).textContent === "" && location.pathname === "/nav"`)
		if m := p.eval(`window.__marker`); m != float64(1) {
			t.Errorf("back: reloaded")
		}
		if t.Failed() {
			t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
		}
	})
	// Pages whose component sets a bind: field: typing into the input
	// (v-model="live.q.value", value/onChange, bind:value) is one debounced
	// set; the server runs Changed and the list follows.
	typeInto := func(p *tab, v string) {
		p.eval(`(() => { const el = document.querySelector("#f .q"); el.focus(); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, ` + strconv.Quote(v) + `); el.dispatchEvent(new Event("input", {bubbles: true})); })()`)
	}
	for _, path := range []string{"/find", "/findstatic", "/rfind", "/sfind"} {
		t.Run(path, func(t *testing.T) {
			p := ch.open(base+path+"?q=A", 900, 700)
			if path == "/find" || path == "/rfind" {
				p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
			}
			p.wait(`(document.querySelector("#f .rows") || {}).textContent === "Ada,Alan"`)
			if v := p.eval(`document.querySelector("#f .q").value`); v != "A" {
				t.Errorf("input = %v", v)
			}
			typeInto(p, "G")
			typeInto(p, "Gr")
			if v := p.eval(`document.querySelector("#f .q").value`); v != "Gr" {
				t.Errorf("input while typing = %v", v)
			}
			p.wait(`document.querySelector("#f .rows").textContent === "Grace,Guido"`)
			p.wait(`location.search === "?q=Gr"`)
			if c := p.text("#f .changes"); c != "1" {
				t.Errorf("Changed ran %s times for one debounced set", c)
			}
			p.click("#f .drop")
			p.wait(`document.querySelector("#f .rows").textContent === ""`)
			p.wait(`document.querySelector("#f .pending").textContent === "false"`)
			if v := p.eval(`document.querySelector("#f .q").value`); v != "Gr" {
				t.Errorf("input after drop = %v", v)
			}
			if n := p.eval(`window.__mounts`); n != float64(1) {
				t.Errorf("mounted %v times", n)
			}
			if t.Failed() {
				t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
			}
		})
	}

	// A page with fields and no methods but Mount and Changed: its
	// component still has live.
	for _, path := range []string{"/only", "/onlystatic"} {
		t.Run(path, func(t *testing.T) {
			p := ch.open(base+path, 900, 700)
			if path == "/only" {
				p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
			}
			p.wait(`(document.querySelector("#o .rows") || {}).textContent === "Ada,Alan,Grace,Guido"`)
			p.eval(`(() => { const el = document.querySelector("#o .q"); el.focus(); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, "Gr"); el.dispatchEvent(new Event("input", {bubbles: true})); })()`)
			p.wait(`document.querySelector("#o .rows").textContent === "Grace,Guido"`)
			p.wait(`document.querySelector("#o .changes").textContent === "1"`)
			p.eval(`(() => { const el = document.querySelector("#o .q"); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, "Gracey"); el.dispatchEvent(new Event("input", {bubbles: true})); })()`)
			p.wait(`document.querySelector("#o .err").textContent === "at most 5 characters"`)
			if v, c := p.eval(`document.querySelector("#o .q").value`), p.text("#o .changes"); v != "Gracey" || c != "1" {
				t.Errorf("an invalid set: input %v, Changed ran %s times", v, c)
			}
			p.eval(`(() => { const el = document.querySelector("#o .q"); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, "Ad"); el.dispatchEvent(new Event("input", {bubbles: true})); })()`)
			p.wait(`document.querySelector("#o .rows").textContent === "Ada,Alan" && document.querySelector("#o .err").textContent === ""`)
			if t.Failed() {
				t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
			}
		})
	}
}
