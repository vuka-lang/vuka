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
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Chart is a component with a script.
type Chart struct {
	Title  string
	Points []int
}

func home() ui.Node { return Page() }

func main() {
	app := web.New()
	app.Get("/", home)
	app.Run("")
}
`,
		"chart.vuka.html": `<figure class="chart">
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

func Page()
<html><head><title>chart</title></head><body><Chart title="Sales" points={[]int{3, 5}} /></body></html>
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
	id := scopeID("example.com/app", "Chart")
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

// TestFindTemplateAssets: a template file's scripts and stylesheets are its
// templates' — a scoped one its component's, another the app's — and the
// component files it imports are islands.
func TestFindTemplateAssets(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"main.go":   "package main\n\ntype Chart struct{ Title string }\n\nfunc main() {}\n",
		"clock.vue": "<script setup lang=\"ts\">\ndefineProps<{ zone?: string }>()\n</script>\n<template><time /></template>\n",
		"chart.vuka.html": "import Clock \"./clock.vue\"\n\n<figure>{Title}<Clock /></figure>\n\n<script lang=\"ts\">\nexport default {}\n</script>\n\n<style scoped>\nfigure { margin: 0 }\n</style>\n\n" +
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
	if chart == nil || len(chart.scripts) != 1 || chart.scripts[0].label != "chart.vuka.html:6" || len(chart.styles) != 1 || chart.styles[0].inline != "figure { margin: 0 }\n" {
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
		"main.vuka": "package main\n\nimport \"github.com/vuka-lang/ui\"\n\ntype Chart struct{ Title string }\n\nfunc (c Chart) Render() ui.Node {\n\treturn <figure>{c.Title}\n\t\t<script lang=\"ts\">\nexport default { mounted(el: HTMLElement) { el.dataset.x = \"1\" } }\n</script>\n\t\t<style scoped>figure { margin: 0 }</style>\n\t</figure>\n}\n\nfunc main() {}\n",
	})
	pa, err := findAssets(root, "example.com/app")
	if err != nil || pa == nil {
		t.Fatal(pa, err)
	}
	if len(pa.comps) != 1 || pa.comps[0].name != "Chart" || len(pa.comps[0].scripts) != 1 || pa.comps[0].scripts[0].label != "main.vuka:10" || len(pa.comps[0].styles) != 1 {
		t.Errorf("comps: %+v", pa.comps)
	}
}
