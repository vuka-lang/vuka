package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const urlShape = `<script lang="go">
package pages

type Table struct {
	ui.Live
	Sort string @ui.URL("sort")
	page int    @ui.URL("page").Default(1)
	id   int64  @ui.URL
	ok   bool   @ui.URLish
}
</script>
<p>{page}</p>
`

func TestURLAttrs(t *testing.T) {
	as := urlAttrs([]byte(urlShape))
	if len(as) != 3 {
		t.Fatalf("%+v", as)
	}
	if a := as[0]; a.field != "Sort" || a.key != "sort" || a.bare || a.def != "" {
		t.Errorf("%+v", a)
	}
	if a := as[1]; a.field != "page" || a.key != "page" || a.def != "1" {
		t.Errorf("%+v", a)
	}
	if a := as[2]; a.field != "id" || !a.bare {
		t.Errorf("%+v", a)
	}
	if h := urlHover(as[1]); !strings.Contains(h, "`?page=`") || !strings.Contains(h, "it is `1`") || !strings.Contains(h, "?name.page=") {
		t.Error(h)
	}
	if h := urlHover(as[2]); !strings.Contains(h, "path parameter `{id}`") {
		t.Error(h)
	}
}

func TestURLKeyCompletion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "table.vuka.html"), []byte(urlShape), 0o644)
	os.WriteFile(filepath.Join(dir, "other.go"), []byte(`x string @ui.URL("ignored")`), 0o644)
	src := "type Tabs struct {\n\ttab string @ui.URL(\"so\n"
	off := len(src) - 1
	from, ok := urlKeyAt([]byte(src), off)
	if !ok || src[from:off] != "so" {
		t.Fatalf("at %d %v", from, ok)
	}
	if _, ok := urlKeyAt([]byte(`a @ui.URL("q") b`), len(`a @ui.URL("q") b`)); ok {
		t.Error("after the closing quote")
	}
	keys := packageURLKeys(dir, filepath.Join(dir, "tabs.vuka.html"), []byte(src))
	if len(keys) != 2 || keys["page"][0] != "page" || keys["sort"][0] != "Sort" {
		t.Fatalf("%v", keys)
	}
}
