package format

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplate(t *testing.T) {
	src := "package p\n<p class={ cls }>{a+b} {{x} {/* note */}</p>   \n\n\n<script lang=\"ts\">\nconst x = {a:1}\n\n\n</script>\n// Row is a row.\nfunc row(o   Order)\n<li>{ o.Ref }</li>\n\n\n"
	want := "package p\n\n<p class={cls}>{a + b} {{x} {/* note */}</p>\n\n<script lang=\"ts\">\nconst x = {a:1}\n\n\n</script>\n\n// Row is a row.\nfunc row(o Order)\n<li>{o.Ref}</li>\n"
	check(t, src, "", want)
}

func TestTemplateLongHeader(t *testing.T) {
	src := "package layouts\n\nfunc App(me *auth.Identity, path string, title string, description string, actions ui.Node, children ui.Node)\n<div>{title}</div>\n"
	check(t, src, "", src)
}

func TestTemplateLayout(t *testing.T) {
	src := "import \"x/components\"\nfunc Nav(items []Item)\n\n\n<nav>\n  for _, i := range items {\n\n    <navLink item={i}/>\n  }\n</nav>\n// navLink is one link.\n\nfunc navLink(item Item)\n<a href={item.Href}>{item.Label}</a>\nfunc footer()\n\n<p>©</p>"
	want := "package layouts\n\nimport \"x/components\"\n\nfunc Nav(items []Item)\n<nav>\n  for _, i := range items {\n\n    <navLink item={i}/>\n  }\n</nav>\n\n// navLink is one link.\nfunc navLink(item Item)\n<a href={item.Href}>{item.Label}</a>\n\nfunc footer()\n<p>©</p>\n"
	check(t, src, "layouts", want)
}

func TestTemplatePackage(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"<p/>\n", "package pages\n\n<p/>\n"},
		{"// Orders is a page.\nfunc Orders()\n<p/>\n", "package pages\n\n// Orders is a page.\nfunc Orders()\n<p/>\n"},
		{"// Copyright 2026.\n\n<p/>\n", "// Copyright 2026.\n\npackage pages\n\n<p/>\n"},
		{"package pages\n<p/>\n", "package pages\n\n<p/>\n"},
		{"package other\n\n<p/>\n", "package other\n\n<p/>\n"},
	} {
		check(t, c.src, "pages", c.want)
	}
}

func TestTemplateGolden(t *testing.T) {
	for _, name := range []string{"page", "layout"} {
		src, err := os.ReadFile(filepath.Join("testdata", name+".vuka.html"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("testdata", name+".vuka.html.golden"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, string(src), "pages", string(want))
	}
}

func check(t *testing.T, src, pkg, want string) {
	t.Helper()
	got, err := Template([]byte(src), pkg)
	if err != nil || string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if again, _ := Template(got, pkg); string(again) != want {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

// TestTemplateHeaderStructs: the Go header's one-line structs stay as gofmt
// writes them; only the markup's holes are formatted.
func TestTemplateHeaderStructs(t *testing.T) {
	src := "package pets\n\n// Tag is a label.\ntype Tag struct{ Name string }\n\nfunc (t Tag) Upper() string { return t.Name }\n<b>{ Name }</b>\n"
	want := "package pets\n\n// Tag is a label.\ntype Tag struct{ Name string }\n\nfunc (t Tag) Upper() string { return t.Name }\n<b>{Name}</b>\n"
	check(t, src, "", want)
}
