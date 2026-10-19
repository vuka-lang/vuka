package main

import (
	"encoding/json"
	"github.com/vuka-lang/vuka/diag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const islandLSPSource = `package main

import (
	"github.com/vuka-lang/ui"

	Picker "./islands/Picker.vue"
)

func Page() ui.Node {
	return <Picker title="Week" client:load />
}

func main() {}
`

const islandLSPVue = `<script setup lang="ts">
defineProps<{ title: string; weeks?: number; onPick?: (day: string) => void }>()
</script>
`

// The LSP sees an imported island's props as the compiler reads them from
// the component: no diagnostics, hover on an attribute, attribute
// completion, and a wrong value reported where it is written.
func TestLSPIslandImport(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": islandLSPSource, "islands/Picker.vue": islandLSPVue})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": islandLSPSource}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(src, needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), strings.Index(src, needle)+delta)}
	}
	ask := func(method string, params map[string]any) string {
		b, _ := json.Marshal(c.call(method, params))
		return string(b)
	}
	if h := ask("textDocument/hover", at(islandLSPSource, `title="Week"`, 1)); !strings.Contains(h, "field Title string") {
		t.Fatalf("hover on title=: %s", h)
	}
	version := 1
	edit := func(src string) {
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
	}
	src := strings.Replace(islandLSPSource, `client:load />`, `client:load  />`, 1)
	edit(src)
	var r struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	json.Unmarshal([]byte(ask("textDocument/completion", at(src, `client:load  />`, 12))), &r)
	var got []string
	for _, it := range r.Items {
		got = append(got, it.Label)
	}
	sort.Strings(got)
	if s := strings.Join(got, " "); !strings.Contains(s, "weeks") || !strings.Contains(s, "onPick") {
		t.Fatalf("completion: %v", got)
	}
	src = strings.Replace(islandLSPSource, `title="Week"`, `title="Week" weeks="2"`, 1)
	edit(src)
	c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "float64")
	})
}

const islandDecoSource = `package main

import (
	"context"

	"github.com/vuka-lang/ui"
)

@ui.Island
type Counter struct {
	Count int ` + "`json:\"count\"`" + `
}

@ui.Island("islands/Chart.vue")
type Chart struct {
	Series []int ` + "`json:\"series\"`" + `
}

@ui.Island("", "load")
type Gone struct {
	N int ` + "`json:\"n\"`" + `
}

@ui.Island
type Board struct {
	ui.Live
	Q     string ` + "`json:\"q\" bind:\"q\"`" + `
	Stats []int  ` + "`json:\"stats\" defer:\"\"`" + `
}

func (b *Board) Mount(ctx context.Context) error   { return nil }
func (b *Board) Changed(ctx context.Context) error { return nil }
func (b *Board) LoadStats(ctx context.Context) error { return nil }

func main() {}
`

// TestLSPIslandDecorators: hover on @ui.Island names the component file,
// inferred or written, and its framework; definition opens it; a missing
// one is a diagnostic on the decorator, without a build; hover on an island
// page's Mount and Changed says they are the server's hooks.
func TestLSPIslandDecorators(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": islandDecoSource,
		"islands/Counter.ts": "export function mount() { return { update() {}, unmount() {} } }\n",
		"islands/Chart.vue":  "<template><p/></template>\n", "islands/Board.vue": "<template><p/></template>\n"})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": islandDecoSource}})
	var diags string
	c.waitDiags(uri, func(ds []any) bool {
		diags = jsonOf(ds)
		return strings.Contains(diags, "island Gone")
	})
	if want := `"code":"E0310","codeDescription":{"href":"` + diag.DocsURL + `E0310"},"message":"island Gone: no component at islands/Gone.{vue,tsx,jsx,svelte,ts,js}\nhelp: add it there, or name the file with @ui.Island(\"…\")"`; !strings.Contains(diags, want) || strings.Count(diags, `"message"`) != 1 {
		t.Fatalf("diagnostics: %s", diags)
	}
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(islandDecoSource), strings.Index(islandDecoSource, needle)+delta)}
	}
	for _, tc := range []struct {
		needle string
		want   []string
	}{
		{"@ui.Island\ntype Counter", []string{"island `Counter`: `islands/Counter.ts` (vanilla)", "inferred"}},
		{`@ui.Island("islands/Chart.vue")`, []string{"island `Chart`: `islands/Chart.vue` (vue)", "written"}},
		{`@ui.Island("", "load")`, []string{"island `Gone`", "**Warning:** no component at islands/Gone."}},
	} {
		if h := jsonOf(c.call("textDocument/hover", at(tc.needle, 4))); !containsAll(h, tc.want) {
			t.Errorf("hover on %s: %s", tc.needle, h)
		}
	}
	if d := jsonOf(c.call("textDocument/definition", at("@ui.Island\ntype Counter", 5))); !strings.Contains(d, pathToURI(filepath.Join(dir, "islands/Counter.ts"))) {
		t.Errorf("definition: %s", d)
	}
	if d := c.call("textDocument/definition", at(`@ui.Island("", "load")`, 5)); d != nil {
		t.Errorf("definition of a missing file: %s", jsonOf(d))
	}
	if h := jsonOf(c.call("textDocument/hover", at("Mount(ctx", 2))); !strings.Contains(h, "Lifecycle hook: the server runs Mount") || !strings.Contains(h, "func (b *Board) Mount") {
		t.Errorf("hover on Mount: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at("Changed(ctx", 2))); !strings.Contains(h, "after the component sets a bind: field") {
		t.Errorf("hover on Changed: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at("LoadStats(ctx", 2))); !strings.Contains(h, "Loader of deferred fields (live.stats)") {
		t.Errorf("hover on LoadStats: %s", h)
	}

	// The file added: the diagnostic goes with the next edit.
	os.WriteFile(filepath.Join(dir, "islands/Gone.vue"), []byte("<template><p/></template>\n"), 0o644)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": islandDecoSource + "\n"}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
