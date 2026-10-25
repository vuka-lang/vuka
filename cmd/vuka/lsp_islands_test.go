package main

import (
	"encoding/json"
	"github.com/vuka-lang/vuka/v2/diag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const islandLSPSource = `package main

import Picker "./islands/Picker.vue"

type Page struct{}
<section>
	<Picker title="Week" client:load />
</section>
`

const islandLSPVue = `<script setup lang="ts">
defineProps<{ title: string; weeks?: number; onPick?: (day: string) => void }>()
</script>
`

// The LSP sees an island imported in a .vuka.html header with its props as
// the compiler reads them from the component: no diagnostics, hover on an attribute, attribute
// completion, and a wrong value reported where it is written.
func TestLSPIslandImport(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "page.vuka.html": islandLSPSource, "islands/Picker.vue": islandLSPVue})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": islandLSPSource}})
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
	ds := c.waitDiags(uri, func(ds []any) bool {
		b, _ := json.Marshal(ds)
		return strings.Contains(string(b), "float64")
	})
	if r, _ := toRange(ds[0].(map[string]any)["range"]); r.Start.Line != positionOf([]byte(src), strings.Index(src, "weeks=")).Line {
		t.Fatalf("a wrong value's diagnostic: %s", jsonOf(ds))
	}
}

const islandDecoSource = `package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
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

@ui.Mount
func (b *Board) Mount(ctx context.Context) error { return nil }

@ui.OnChange
func (b *Board) Changed(ctx context.Context, c ui.Changed) error { return nil }

func (b *Board) LoadStats(ctx context.Context) error { return nil }

func main() {}
`

// TestLSPIslandDecorators: hover on @ui.Island names the component file,
// inferred or written, and its framework; definition opens it; a missing
// one is a diagnostic on the decorator, without a build; hover on an island
// page's Load<Group> that no attribute marks says it isn't a hook, naming
// the attribute; on a method an attribute marks there is no such note.
func TestLSPIslandDecorators(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": islandDecoSource,
		"islands/Counter.ts": "export function mount() { return { update() {}, unmount() {} } }\n",
		"islands/Chart.vue":  "<template><p/></template>\n", "islands/Board.vue": "<template><p/></template>\n"})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": islandDecoSource}})
	// The island's diagnostic, and Board's loader found by name (E0324),
	// alone once gopls has loaded the package (while it loads, its own may
	// come along).
	want := `"code":"E0310","codeDescription":{"href":"` + diag.DocsURL + `E0310"},"message":"island Gone: no component at islands/Gone.{vue,tsx,jsx,svelte,ts,js}\nhelp: add it there, or name the file with @ui.Island(\"…\")"`
	c.waitDiags(uri, func(ds []any) bool {
		diags := jsonOf(ds)
		return strings.Contains(diags, want) && strings.Count(diags, `"code":"E0324"`) == 1 && strings.Count(diags, `"message"`) == 2
	})
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
	if h := jsonOf(c.call("textDocument/hover", at("Mount(ctx", 2))); strings.Contains(h, "Not a hook") || !strings.Contains(h, "func (b *Board) Mount") {
		t.Errorf("hover on Mount: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at("Changed(ctx", 2))); strings.Contains(h, "Not a hook") {
		t.Errorf("hover on Changed: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at("LoadStats(ctx", 2))); !strings.Contains(h, `Not a hook: a hook is declared with @ui.Loads(\"stats\")`) {
		t.Errorf("hover on LoadStats: %s", h)
	}

	// The file added: the diagnostic goes with the next edit.
	os.WriteFile(filepath.Join(dir, "islands/Gone.vue"), []byte("<template><p/></template>\n"), 0o644)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": islandDecoSource + "\n"}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 1 && strings.Contains(jsonOf(ds), `"code":"E0324"`) })
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestLSPRootIslandImport(t *testing.T) {
	src := strings.ReplaceAll(islandLSPSource, "./islands/Picker.vue", "@/web/islands/Picker.vue")
	c, dir, _ := startLSPWith(t, map[string]string{
		"main.go":                          "package main\nfunc main() {}\n",
		"pages/admin/users/page.vuka.html": src,
		"web/islands/Picker.vue":           islandLSPVue,
	})
	uri := pathToURI(filepath.Join(dir, "pages/admin/users/page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": src}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), strings.Index(src, needle)+delta)}
	}
	if d := jsonOf(c.call("textDocument/definition", at("<Picker", 2))); !strings.Contains(d, pathToURI(filepath.Join(dir, "web/islands/Picker.vue"))) {
		t.Fatalf("definition: %s", d)
	}
	if h := jsonOf(c.call("textDocument/hover", at("<Picker", 2))); !strings.Contains(h, "title") || !strings.Contains(h, "weeks") {
		t.Fatalf("hover: %s", h)
	}
}
