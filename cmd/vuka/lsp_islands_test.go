package main

import (
	"encoding/json"
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
