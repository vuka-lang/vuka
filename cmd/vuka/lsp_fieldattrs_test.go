package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lspFieldAttrsPkg = `package orm

import "github.com/vuka-lang/vuka"

// Column names the column.
type Column string

// Char is a VARCHAR of Max characters.
type Char struct{ Max int }

// PK makes the field the primary key.
type PK struct{}

// Default is a column default, from DefaultTo.
type Default struct{ SQL string }

// DefaultTo is the column default v.
func DefaultTo(v any) Default { return Default{} }

// Meta is what a model declares about itself.
type Meta struct{ Table string }

// Base is a model's base.
type Base struct{ id int }

// Unique is a unique index; UniqueIndex makes one.
type Unique struct{ fields []string }

// UniqueIndex is a unique index of fields.
func UniqueIndex(fields ...string) Unique { return Unique{fields} }

// Index indexes the column.
type Index struct{}

func (Index) vukaTargets() [vuka.OnField | vuka.OnParam]struct{} {
	return [vuka.OnField | vuka.OnParam]struct{}{}
}

// Path binds a parameter from the URL.
type Path string

func (Path) vukaTargets() [vuka.OnParam]struct{} { return [vuka.OnParam]struct{}{} }

// FK is a foreign key.
type FK[T any] struct{ ID int64 }

// Option configures Open.
type Option interface{ apply() }

// Open opens a database.
func Open(dsn string, opts ...Option) error { return nil }
`

const lspFieldAttrsSource = `package main

import "lsptest/orm"

decorator timed(c) { c.Next() }

type Post struct {
	orm.Base @orm.Meta{Table: "posts"}
	ID    int64  @orm.PK
	Title string @orm.Char{Max: 200}
	Slug  string ` + "`json:\"slug\"`" + ` @orm.Column("slug")
	Views int    @orm.DefaultTo(0)
}

@timed
func show() string { return "x" }

func main() { _ = show(); _ = Post{} }
`

// completionItems is the completion at the end of needle in src.
func completionItems(c *lspClient, uri, src, needle string) map[string]map[string]any {
	off := strings.Index(src, needle) + len(needle)
	v := c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(src), off), "context": map[string]any{"triggerKind": 2, "triggerCharacter": "."}})
	items := v
	if list, ok := v.(map[string]any); ok {
		items = list["items"]
	}
	out := map[string]map[string]any{}
	arr, _ := items.([]any)
	for _, it := range arr {
		item := it.(map[string]any)
		out[item["label"].(string)] = item
	}
	return out
}

func itemText(item map[string]any) string {
	if te, ok := item["textEdit"].(map[string]any); ok {
		return te["newText"].(string)
	}
	return item["label"].(string)
}

func itemDoc(item map[string]any) string {
	switch d := item["documentation"].(type) {
	case string:
		return d
	case map[string]any:
		s, _ := d["value"].(string)
		return s
	}
	return ""
}

// TestLSPFieldAttrs covers field attributes: completion after @pkg. lists
// what the package has for a field (types and functions returning one, with
// their docs and how each is written), on a plain field, after another
// attribute and on an embedded field; a declaration's attribute still
// completes decorators; the half-typed attribute leaves the file's other
// diagnostics as they are; and hover on a field attribute shows its doc.
func TestLSPFieldAttrs(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": lspFieldAttrsSource, "orm/orm.go": lspFieldAttrsPkg})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": lspFieldAttrsSource}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	hover := func(src, needle string, delta int) string {
		pos := positionOf([]byte(src), strings.Index(src, needle)+delta)
		b, _ := json.Marshal(c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}))
		return string(b)
	}
	if got := hover(lspFieldAttrsSource, `@orm.Column("slug")`, 6); !strings.Contains(got, "type Column string") || !strings.Contains(got, "names the column") {
		t.Fatalf("hover on a field attribute: %s", got)
	}
	if got := hover(lspFieldAttrsSource, `@orm.DefaultTo(0)`, 6); !strings.Contains(got, "DefaultTo(v any)") {
		t.Fatalf("hover on a field attribute's function: %s", got)
	}

	version := 1
	edit := func(from, to string) string {
		src := strings.Replace(lspFieldAttrsSource, from, to, 1)
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
		return src
	}
	want := map[string]string{"Column": `Column("$1")`, "Char": "Char{$1}", "PK": "PK", "DefaultTo": "DefaultTo($1)",
		"Meta": "Meta{$1}", "Index": "Index",
		"Unique": "Unique", "UniqueIndex": "UniqueIndex($1)"}
	check := func(what string, items map[string]map[string]any) {
		t.Helper()
		for label, text := range want {
			item, ok := items[label]
			if !ok {
				t.Fatalf("%s: no %s among %v", what, label, keys(items))
			}
			if got := itemText(item); got != text {
				t.Errorf("%s: picking %s inserts %q, want %q", what, label, got, text)
			}
		}
		for _, label := range []string{"Path", "FK", "Option", "Open", "timed", "orm.timed", "func", "type"} {
			if _, ok := items[label]; ok {
				t.Errorf("%s: %s offered", what, label)
			}
		}
		if doc := itemDoc(items["Column"]); !strings.Contains(doc, "names the column") {
			t.Errorf("%s: Column's documentation is %q", what, doc)
		}
		if doc := itemDoc(items["DefaultTo"]); !strings.Contains(doc, "column default v") {
			t.Errorf("%s: DefaultTo's documentation is %q", what, doc)
		}
	}

	// A plain field, after its tag.
	src := edit(`@orm.Column("slug")`, `@orm.`)
	check("on a field", completionItems(c, uri, src, "`json:\"slug\"` @orm."))
	// After another attribute.
	src = edit(`ID    int64  @orm.PK`, `ID    int64  @orm.PK @orm.`)
	check("after an attribute", completionItems(c, uri, src, "@orm.PK @orm."))
	// An embedded field.
	src = edit(`orm.Base @orm.Meta{Table: "posts"}`, `orm.Base @orm.Meta{Table: "posts"} @orm.`)
	check("on an embedded field", completionItems(c, uri, src, `@orm.Meta{Table: "posts"} @orm.`))
	// Mid-name, before another attribute.
	src = edit(`ID    int64  @orm.PK`, `ID    int64  @orm.Co @orm.PK`)
	if text := itemText(completionItems(c, uri, src, "@orm.Co")["Column"]); text != `Column("$1")` {
		t.Errorf("at @orm.Co, picking Column inserts %q", text)
	}
	// A bare @ offers the packages, not the decorators.
	src = edit(`Title string @orm.Char{Max: 200}`, `Title string @`)
	items := completionItems(c, uri, src, "Title string @")
	if _, ok := items["orm"]; !ok {
		t.Errorf("at a field's bare @: no orm among %v", keys(items))
	}
	if _, ok := items["timed"]; ok {
		t.Errorf("at a field's bare @: the decorator timed is offered")
	}

	// A declaration's attribute still completes decorators.
	src = edit("@timed\nfunc show", "@ti\nfunc show")
	if text := completionText(c, uri, src, "@ti", "timed"); text != "timed" {
		t.Fatalf("at a declaration's @ti, picking timed inserts %q", text)
	}

	// While a field's attribute is half-typed, the rest of the file's
	// diagnostics stand, and the attribute brings none of its own.
	for _, typing := range []string{`Title string @orm.`, `Title string @`, `Title string @orm. @orm.Char{Max: 200}`} {
		src = strings.Replace(lspFieldAttrsSource, `Title string @orm.Char{Max: 200}`, typing, 1)
		src = strings.Replace(src, `return "x"`, `return 1`, 1)
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
		ds := c.waitDiags(uri, func(ds []any) bool {
			b, _ := json.Marshal(ds)
			return strings.Contains(string(b), "cannot use 1")
		})
		for _, d := range ds {
			msg := d.(map[string]any)["message"].(string)
			if !strings.Contains(msg, "cannot use 1") {
				t.Errorf("typing %q: diagnostic %q", typing, msg)
			}
		}
	}
}

func keys(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
