package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTemplateAssets: a template's script and scoped stylesheet are its
// component's — the script bundled with vuka:component, the stylesheet
// scoped to the elements the template writes — and its root carries the
// props the script reads.
func TestTemplateAssets(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no tailwindcss on PATH")
	}
	root := assetsModule(t, map[string]string{
		"main.go": `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes()).Run("") }
`,
		"pages/index.vuka.html": `package pages

import "example.com/app/components"

type Index struct{}
<html><head><title>chart</title></head><body><components.Chart title="Sales" points={[]int{3, 5}} /></body></html>
`,
		"components/chart.vuka.html": `package components

// Chart is a component with a script.
type Chart struct {
	Title  string
	Points []int
}
<figure class="chart">
  <figcaption>{Title}</figcaption>
</figure>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Chart>((el, props) => {
  el.dataset.points = String(props.Points.length)
  return { destroy() { el.dataset.points = "" } }
})
</script>

<style scoped>
figcaption { color: rgb(1, 2, 3) }
</style>
`,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(twEnv, "")
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
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=", assetsEnv+"=")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	get := func(path string) string {
		var res *http.Response
		waitFor(t, func() bool {
			var err error
			res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
			return err == nil
		})
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	page := get("/")
	id := scopeID("example.com/app/components", "Chart")
	if !strings.Contains(page, `<figure data-vk-p="{&#34;Points&#34;:[3,5],&#34;Title&#34;:&#34;Sales&#34;}" class="chart" data-vk-s="`+id+`">`) ||
		!strings.Contains(page, `<figcaption data-vk-s="`+id+`">Sales</figcaption>`) || strings.Contains(page, "<style") {
		t.Errorf("the template's elements: %s", page)
	}
	css := get(regexp.MustCompile(`/_vuka/app\.css\?v=[^"]+`).FindString(page))
	if !regexp.MustCompile(`figcaption\[data-vk-s="?` + id + `"?\]\{color:#010203\}`).MatchString(css) {
		t.Errorf("app.css lacks the scoped rule:\n%s", css)
	}
	js := get(regexp.MustCompile(`/_vuka/c/loader\.js\?v=[^"]+`).FindString(page))
	mod := regexp.MustCompile(`/_vuka/c/` + id + `\.[0-9a-f]+\.js`).FindString(js)
	if body := get(mod); mod == "" || !strings.Contains(body, "data-vk-p") || !strings.Contains(body, "dataset.points") {
		t.Errorf("module %q:\n%s", mod, body)
	}
}

// TestTemplateAssetsSlots: a live page whose markup is a component's tag
// filling its slots — repeated, single and left to their fallback — with a
// script and a scoped stylesheet builds; the slots' content is marked with
// the page's scope and the slots themselves render nothing; the props the
// script reads are on a <vk-c> around the markup, holding every element of
// the page's scope, so the script mounts once there and runs.
func TestTemplateAssetsSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no tailwindcss on PATH")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		needs(t, "no node")
	}
	root := assetsModule(t, map[string]string{
		"main.go": `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes()).Run("") }
`,
		"components/column.go": `package components

import "github.com/vuka-lang/ui/v2"

// Column is one of a Table's columns.
type Column[T any] struct {
	Key   string
	Label string
	Cell  func(T) ui.Node
}
`,
		"components/table.vuka.html": `package components

import "github.com/vuka-lang/ui/v2"

// Table is a generic table.
type Table[T any] struct {
	Rows    []T
	Columns []Column[T]
	Caption ui.Node
	Empty   ui.Node
}
<table>
  <caption>{Caption}</caption>
  for _, r := range Rows {
    <tr>
      for _, c := range Columns {
        <td data-key={c.Key}>{c.Cell(r)}</td>
      }
    </tr>
  }
  if len(Rows) == 0 {
    <tr><td><slot:empty>no rows</slot:empty></td></tr>
  }
</table>
`,
		"pages/row.go": `package pages

// Row is a row.
type Row struct{ Name string }
`,
		"pages/index.vuka.html": `<script lang="go">
package pages

import (
	"context"

	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

// Index is GET /.
type Index struct {
	ui.Live
	Rows []Row
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.Rows = []Row{{"a"}, {"b"}}
	return nil
}
</script>

<components.Table[Row] rows={Rows}>
  <slot:column key="name" label="Name" let={r}><b>{r.Name}</b></slot:column>
  <slot:caption><em>Rows</em></slot:caption>
</components.Table>

<script lang="ts">
import { page } from 'vuka:component'
export default page<Index>((el, live) => { el.dataset.rows = String(live.rows.get().length) })
</script>

<style scoped>
b { color: rgb(1, 2, 3) }
</style>
`,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(twEnv, "")
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
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=", assetsEnv+"=")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	get := func(path string) string {
		var res *http.Response
		waitFor(t, func() bool {
			var err error
			res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
			return err == nil
		})
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	page := get("/")
	id := scopeID("example.com/app/pages", "Index")
	mark := ` data-vk-s="` + id + `"`
	open := regexp.MustCompile(`<vk-c style="display:contents;" data-vk-p="([^"]*)"` + mark + `[^>]*>`).FindStringSubmatchIndex(page)
	if open == nil || strings.Contains(page, "slot:") || !strings.Contains(page, "<b"+mark+">a</b>") || !strings.Contains(page, "<em"+mark+">Rows</em>") {
		t.Fatalf("the page's elements: %s", page)
	}
	if first := strings.Index(page, mark); first < open[0] || first > open[1] || strings.LastIndex(page, mark) > strings.LastIndex(page, "</vk-c>") {
		t.Errorf("an element of the page's scope outside its <vk-c>: %s", page)
	}
	css := get(regexp.MustCompile(`/_vuka/app\.css\?v=[^"]+`).FindString(page))
	if !regexp.MustCompile(`b\[data-vk-s="?` + id + `"?\]\{color:#010203\}`).MatchString(css) {
		t.Errorf("app.css lacks the scoped rule:\n%s", css)
	}
	js := get(regexp.MustCompile(`/_vuka/c/loader\.js\?v=[^"]+`).FindString(page))
	mod := get(regexp.MustCompile(`/_vuka/c/` + id + `\.[0-9a-f]+\.js`).FindString(js))
	props := strings.NewReplacer("&#34;", `"`, "&amp;", "&").Replace(page[open[2]:open[3]])
	prog := `const m = await import("data:text/javascript;base64," + process.argv[1]);
const el = { dataset: {}, getAttribute: (a) => a === "data-vk-p" ? process.argv[2] : null };
for (const h of m.default) h.mounted(el, { push() {} });
console.log(el.dataset.rows);
`
	out, err := exec.Command(node, "--input-type=module", "-e", prog, base64.StdEncoding.EncodeToString([]byte(mod)), props).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "2" {
		t.Errorf("the page's script: %v\n%s\nmodule:\n%s", err, out, mod)
	}
}

// TestFindTemplateAssets: a template file's scripts and stylesheets are its
// templates' — a scoped one its component's, another the app's — and the
// component files it imports are islands.
func TestFindTemplateAssets(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"main.go":   "package main\n\ntype Chart struct{ Title string }\n\nfunc main() {}\n",
		"clock.vue": "<script setup lang=\"ts\">\ndefineProps<{ zone?: string }>()\n</script>\n<template><time /></template>\n",
		"chart.vuka.html": "package main\n\nimport Clock \"./clock.vue\"\n\n<figure>{Title}<Clock /></figure>\n\n<script lang=\"ts\">\nexport default {}\n</script>\n\n<style scoped>\nfigure { margin: 0 }\n</style>\n\n" +
			"func Row(n int)\n<p>{n}</p>\n\n<style>\nbody { margin: 0 }\n</style>\n",
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil || pa == nil {
		t.Fatal(pa, err)
	}
	var chart *assetComp
	for _, c := range pa.comps {
		if c.name == "Chart" {
			chart = c
		}
	}
	if chart == nil || len(chart.scripts) != 1 || chart.scripts[0].label != "chart.vuka.html:8" || len(chart.styles) != 1 || chart.styles[0].inline != "figure { margin: 0 }\n" {
		t.Errorf("Chart: %+v", chart)
	}
	if len(pa.global.styles) != 1 || pa.global.styles[0].inline != "body { margin: 0 }\n" {
		t.Errorf("global: %+v", pa.global.styles)
	}
	if len(pa.islands) != 1 || pa.islands[0].name != "Clock" {
		t.Errorf("islands: %+v", pa.islands)
	}
}

// TestJSXAssets: a component's <script lang="ts"> and <style scoped> in
// .vuka JSX are its own, as a template's are.
func TestJSXAssets(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"main.vuka": "package main\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Chart struct{ Title string }\n\nfunc (c Chart) Render() ui.Node {\n\treturn <figure>{c.Title}\n\t\t<script lang=\"ts\">\nexport default { mounted(el: HTMLElement) { el.dataset.x = \"1\" } }\n</script>\n\t\t<style scoped>figure { margin: 0 }</style>\n\t</figure>\n}\n\nfunc main() {}\n",
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil || pa == nil {
		t.Fatal(pa, err)
	}
	if len(pa.comps) != 1 || pa.comps[0].name != "Chart" || len(pa.comps[0].scripts) != 1 || pa.comps[0].scripts[0].label != "main.vuka:10" || len(pa.comps[0].styles) != 1 {
		t.Errorf("comps: %+v", pa.comps)
	}
}
