package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// tagsModule writes a module with a components package: a shape struct, a
// .vuka struct with field attributes and a function component.
func tagsModule(t *testing.T) (*proxy, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "components", "card.vuka.html"), `package components

import "github.com/vuka-lang/ui/v2"

// Card frames content.
type Card struct {
	Title    string
	Count    *int
	OwnerID  int64
	Children ui.Node
	Actions  ui.Node
	open     ui.State[bool]
}
<div class="card"><h2>{Title}</h2>{Children}</div>
`)
	writeFile(t, filepath.Join(root, "components", "row.vuka"), "package components\n\ntype Row struct {\n\t// Label is shown.\n\tLabel string @ui.Bind{}\n\tOn bool\n}\n\nfunc (r *Row) View() {}\n")
	writeFile(t, filepath.Join(root, "components", "badge.go"), "package components\n\nimport \"context\"\n\nfunc Badge(ctx context.Context, label string, on bool, extra ...string) ui.Node { return nil }\n")
	p := &proxy{bufs: map[string][]byte{}, importDirs: map[string]string{}, modRoot: root, modPath: "example.com/app"}
	return p, root
}

const tagsPage = `package pages

import (
	"example.com/app/components"
	"github.com/vuka-lang/ui/v2"
)

type Index struct{ ui.Live }
<main>
  <components.Card title="x" />
  <components.Badge />
  <components.Row />
</main>
`

func TestPropCompletion(t *testing.T) {
	p, root := tagsModule(t)
	page := filepath.Join(root, "pages", "index.vuka.html")
	at := func(needle string) int { return strings.Index(tagsPage, needle) + len(needle) }
	labels := func(items []any) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, it := range items {
			m := it.(map[string]any)
			out[m["label"].(string)] = m
		}
		return out
	}

	items, ok := p.propCompletion(page, []byte(tagsPage), at(`<components.Card title="x" `))
	if !ok {
		t.Fatal("no completion in <components.Card>")
	}
	got := labels(items)
	if got["title"] != nil || got["children"] != nil || got["actions"] != nil || got["open"] != nil {
		t.Errorf("Card offers a used attribute, a slot, the children or state: %v", got)
	}
	if got["count"] == nil || got["ownerID"] == nil || got["count"]["detail"] != "*int" {
		t.Errorf("Card's props: %v", got)
	}
	if d := got["ownerID"]["labelDetails"].(map[string]any)["description"]; d != "optional" {
		t.Errorf("a struct's prop is optional, got %v", d)
	}

	items, _ = p.propCompletion(page, []byte(tagsPage), at(`<components.Badge `))
	got = labels(items)
	if len(got) != 3 || got["ctx"] != nil {
		t.Fatalf("Badge's parameters: %v", got)
	}
	for name, want := range map[string]string{"label": "required", "on": "required", "extra": "optional"} {
		if d := got[name]["labelDetails"].(map[string]any)["description"]; d != want {
			t.Errorf("%s: %v, want %s", name, d, want)
		}
	}
	if got["on"]["textEdit"].(map[string]any)["newText"] != "on" || !strings.HasPrefix(got["label"]["sortText"].(string), "0") {
		t.Errorf("a bool attribute is bare and required ones sort first: %v", got)
	}

	items, _ = p.propCompletion(page, []byte(tagsPage), at(`<components.Row `))
	got = labels(items)
	if got["label"] == nil || got["on"] == nil || got["label"]["documentation"] == nil {
		t.Errorf("Row's props from the .vuka struct: %v", got)
	}

	if _, ok := p.propCompletion(page, []byte(tagsPage), at(`<main`)+1); ok {
		t.Error("an HTML element's attributes are not a component's")
	}
}

func TestTagHover(t *testing.T) {
	p, root := tagsModule(t)
	page := filepath.Join(root, "pages", "index.vuka.html")
	s := scanShape([]byte(tagsPage))
	h := p.tagHover(page, []byte(tagsPage), s, strings.Index(tagsPage, "Card"))
	if h == nil {
		t.Fatal("no hover on <components.Card>")
	}
	md := h.(map[string]any)["contents"].(map[string]any)["value"].(string)
	for _, w := range []string{"**component** `<components.Card>`", "card.vuka.html](file://", "#L6)", "| `title` | `string` |", "`<slot:actions>`", "zero value"} {
		if !strings.Contains(md, w) {
			t.Errorf("hover %q lacks %q", md, w)
		}
	}
	h = p.tagHover(page, []byte(tagsPage), s, strings.Index(tagsPage, "Badge"))
	md = h.(map[string]any)["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(md, "**function**") || !strings.Contains(md, "| `label` | `string` | required |") {
		t.Errorf("Badge hover %q", md)
	}
}

func TestRootIslandTag(t *testing.T) {
	p, root := tagsModule(t)
	file := filepath.Join(root, "web", "islands", "Form.vue")
	writeFile(t, file, `<script setup lang="ts">defineProps<{ title: string; count?: number }>()</script>`)
	src := []byte("package users\nimport Form \"@/web/islands/Form.vue\"\ntype Edit struct{}\n<main><Form title=\"Edit\" /></main>\n")
	info := p.resolveComponent(filepath.Join(root, "pages", "admin", "users", "edit.vuka.html"), src, "Form")
	if info == nil {
		t.Fatal("no island tag")
	}
	if info.file != file {
		t.Fatalf("definition: %s, want %s", info.file, file)
	}
	if h := info.markdown("Form"); !strings.Contains(h, "title") || !strings.Contains(h, "number") {
		t.Fatalf("hover: %s", h)
	}
}
