package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticSpot(t *testing.T) {
	for src, want := range map[string]string{
		"x := Post.":            "Post",
		"x := a.Post.":          "a Post",
		"x := a.Post.Obj":       "a Post Obj",
		"x := u.b.Post.":        "",
		"x := f().Post.":        "",
		"x := rows[0].":         "",
		"x := Post .":           "", // not a selector the scanner reads
		"fmt.Println(a.Post.Li": "a Post Li",
	} {
		s, ok := completionStaticSpot([]byte(src), len(src))
		got := ""
		if ok {
			got = strings.TrimSpace(s.qual + " " + s.typeName + " " + s.member)
		}
		if got != want {
			t.Errorf("completionStaticSpot(%q) = %q, want %q", src, got, want)
		}
	}
}

const staticsA = `package a

type Store[T any] struct{ rows []*T }

func (s *Store[T]) Add(x *T)  { s.rows = append(s.rows, x) }
func (s *Store[T]) All() []*T { return s.rows }

type Model[Self any] struct {
	// Objects is every row of the model, stored.
	static Objects = Store[Self]{}
}

type Post struct {
	Model
	Title string

	// Limit is how many posts a page shows.
	static Limit = 10
}
`

const staticsB = `package b

import (
	"fmt"

	"lsptest/a"
)

func Show() {
	fmt.Println(a.Post.Objects.All(), a.Post.Limit)
}
`

const staticsMain = `package main

import (
	"lsptest/b"
)

func main() {
	b.Show()
}
`

func completionLabels(t *testing.T, v any) map[string]map[string]any {
	t.Helper()
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	out := map[string]map[string]any{}
	list, _ := items.([]any)
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			if label, ok := m["label"].(string); ok {
				if _, taken := out[label]; !taken {
					out[label] = m
				}
			}
		}
	}
	return out
}

func TestLSPStatics(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{
		"a/a.vuka": staticsA, "b/b.vuka": staticsB, "main.vuka": staticsMain,
	})
	bPath := filepath.Join(dir, "b", "b.vuka")
	bURI := pathToURI(bPath)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": bURI, "languageId": "vuka", "version": 1, "text": staticsB}})
	c.waitDiags(bURI, func(ds []any) bool { return len(ds) == 0 })

	wantStatics := func(t *testing.T, v any, qualified bool) {
		t.Helper()
		items := completionLabels(t, v)
		b, _ := json.Marshal(v)
		objects := items["Objects"]
		if objects == nil {
			t.Fatalf("no Objects completion: %.700s", b)
		}
		detail, _ := objects["detail"].(string)
		want := "static Objects Store[Post]"
		if qualified {
			want = "static Objects Store[a.Post]"
		}
		if detail != want {
			t.Errorf("Objects detail = %q, want %q", detail, want)
		}
		if doc, _ := json.Marshal(objects["documentation"]); !strings.Contains(string(doc), "every row of the model") {
			t.Errorf("Objects documentation = %s, want its doc comment", doc)
		}
		limit := items["Limit"]
		if limit == nil {
			t.Fatalf("no Limit completion: %.700s", b)
		}
		if doc, _ := json.Marshal(limit["documentation"]); !strings.Contains(string(doc), "how many posts") {
			t.Errorf("Limit documentation = %s, want its doc comment", doc)
		}
		if items["Title"] == nil {
			t.Errorf("no Title field completion: %.700s", b)
		}
	}

	t.Run("completion on a qualified type's dot", func(t *testing.T) {
		src := strings.Replace(staticsB, "fmt.Println(a.Post.Objects.All(), a.Post.Limit)",
			"x := a.Post.\n\tfmt.Println(x)", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": bURI, "version": 2},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, "a.Post.\n") + len("a.Post.")
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": bURI},
			"position": positionOf([]byte(src), off)})
		wantStatics(t, v, true)
	})

	t.Run("hover on a static through embedding", func(t *testing.T) {
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": bURI, "version": 3},
			"contentChanges": []any{map[string]any{"text": staticsB}}})
		c.waitDiags(bURI, func(ds []any) bool { return len(ds) == 0 })
		off := strings.Index(staticsB, "Objects.All") + 2
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": bURI},
			"position": positionOf([]byte(staticsB), off)})
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "static a.Post.Objects Store[a.Post]") || !strings.Contains(string(b), "every row of the model") {
			t.Fatalf("hover: %s", b)
		}
	})

	t.Run("hover on a declared static", func(t *testing.T) {
		off := strings.Index(staticsB, "a.Post.Limit") + len("a.Post.Li")
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": bURI},
			"position": positionOf([]byte(staticsB), off)})
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "static a.Post.Limit") || !strings.Contains(string(b), "how many posts") {
			t.Fatalf("hover: %s", b)
		}
	})

	t.Run("completion on the type's own package's dot", func(t *testing.T) {
		aPath := filepath.Join(dir, "a", "a.vuka")
		aURI := pathToURI(aPath)
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": aURI, "languageId": "vuka", "version": 1, "text": staticsA}})
		c.waitDiags(aURI, func(ds []any) bool { return len(ds) == 0 })
		src := staticsA + "\nfunc demo() {\n\tx := Post.\n}\n"
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": aURI, "version": 2},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, "x := Post.\n") + len("x := Post.")
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": aURI},
			"position": positionOf([]byte(src), off)})
		wantStatics(t, v, false)
	})

	t.Run("a variable's dot stays gopls's", func(t *testing.T) {
		src := strings.Replace(staticsB, "fmt.Println(a.Post.Objects.All(), a.Post.Limit)",
			"p := &a.Post{}\n\t_ = p\n\tfmt.Println(a.Post.Objects.All(), a.Post.Limit)", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": bURI, "version": 4},
			"contentChanges": []any{map[string]any{"text": src}}})
		c.waitDiags(bURI, func(ds []any) bool { return true })
		src2 := strings.Replace(src, "_ = p", "_ = p.", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": bURI, "version": 5},
			"contentChanges": []any{map[string]any{"text": src2}}})
		off := strings.Index(src2, "_ = p.") + len("_ = p.")
		v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": bURI},
			"position": positionOf([]byte(src2), off)})
		items := completionLabels(t, v)
		if items["Title"] == nil {
			b, _ := json.Marshal(v)
			t.Fatalf("no Title completion on a value: %.700s", b)
		}
	})
}
