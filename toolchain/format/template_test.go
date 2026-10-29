package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func check(t *testing.T, src, pkg, want string, o Options) {
	t.Helper()
	got, err := TemplateWith([]byte(src), pkg, o)
	if err != nil || string(got) != want {
		t.Fatalf("got (%v)\n%s\nwant\n%s", err, got, want)
	}
	if again, _ := TemplateWith(got, pkg, o); string(again) != want {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

// TestTemplateGolden: a script-form file, every part of it.
func TestTemplateGolden(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "script.vuka.html"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "script.vuka.html.golden"))
	if err != nil {
		t.Fatal(err)
	}
	check(t, string(src), "", string(want), Options{Tailwind: true})
}

// TestTemplateHeaderForm: the Go-header form keeps its form, its Go
// formatted, no blank line above the markup.
func TestTemplateHeaderForm(t *testing.T) {
	src := "package pets\n\n// Tag is a label.\ntype Tag struct{ Name string }\n\nfunc (t Tag) Upper() string { return t.Name }\n<b>{ Name }</b>\n"
	want := "package pets\n\n// Tag is a label.\ntype Tag struct{ Name string }\n\nfunc (t Tag) Upper() string { return t.Name }\n<b>{Name}</b>\n"
	check(t, src, "", want, Options{})
	src = "package pets\ntype Tag struct{\nName string}\n  <div>\n<b>{ Name }</b></div>\n"
	want = "package pets\n\ntype Tag struct {\n\tName string\n}\n<div>\n  <b>{Name}</b></div>\n"
	check(t, src, "", want, Options{})
}

func TestTemplatePackage(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"<p/>\n", "package pages\n\n<p />\n"},
		{"// Copyright 2026.\n\n<p/>\n", "// Copyright 2026.\n\npackage pages\n\n<p />\n"},
		{"package pages\n<p/>\n", "package pages\n\n<p />\n"},
		{"package other\n\n<p/>\n", "package other\n\n<p />\n"},
		{"<script lang=\"go\">\ntype A struct{}\n</script>\n<p/>\n", "<script lang=\"go\">\npackage pages\n\ntype A struct{}\n</script>\n\n<p />\n"},
	} {
		check(t, c.src, "pages", c.want, Options{})
	}
}

// TestTemplateWidth: a tag wraps its attributes past the width, and joins
// them back when it fits.
func TestTemplateWidth(t *testing.T) {
	src := "<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n\n<a href=\"/x\" class=\"y\">z</a>\n"
	want := "<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n\n<a\n  href=\"/x\"\n  class=\"y\"\n>z</a>\n"
	check(t, src, "", want, Options{Width: 20})
	check(t, want, "", src, Options{Width: 40})
}

// TestTemplateErrors: errors are at the file's lines.
func TestTemplateErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"<script lang=\"go\">\npackage p\n\ntype A struct{\n</script>\n<p/>\n", "5:"},
		{"<script lang=\"go\">\npackage p\n", "1:1: this <script"},
		{"<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n\n<div>\n  <p>{a</p>\n</div>\n", "8:1:"},
	} {
		_, err := Template([]byte(c.src), "")
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%q: %v, want %s…", c.src, err, c.want)
		}
	}
}

func TestCSS(t *testing.T) {
	src := "a{color:red;;margin:0 auto}\n\n\n@import url(x.css);\n.b>.c , .d{ --x : 1px  2px ; }\n@layer base{ :root{ --gap:1rem } }\n"
	want := "a {\n  color: red;\n  margin: 0 auto;\n}\n\n@import url(x.css);\n.b>.c,\n.d {\n  --x: 1px  2px;\n}\n@layer base {\n  :root {\n    --gap: 1rem;\n  }\n}\n"
	got, err := CSS(src)
	if err != nil || got != want {
		t.Fatalf("got (%v)\n%s\nwant\n%s", err, got, want)
	}
	if again, _ := CSS(got); again != got {
		t.Errorf("not idempotent:\n%s", again)
	}
	if _, err := CSS("a { color: red;\n"); err == nil {
		t.Error("an unclosed block: no error")
	}
}

func TestTailwind(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"p-4 flex card mx-auto", "card mx-auto flex p-4"},
		{"hover:bg-red-500 bg-white md:p-2 p-1", "bg-white p-1 hover:bg-red-500 md:p-2"},
		{"text-red-500 text-xl font-bold text-center", "text-center text-xl font-bold text-red-500"},
		{"b a", "b a"},
		{"-mt-2 !p-1 absolute", "absolute -mt-2 !p-1"},
	} {
		if got := Tailwind(c.in); got != c.want {
			t.Errorf("Tailwind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseOptions(t *testing.T) {
	o, err := ParseOptions("vuka.toml", []byte("# formatting\n[fmt]\nwidth = 120 # columns\ntailwind = true\n\n[ui]\nstyle = \"default\"\ncomponents = \"components/ui\"\n"))
	if err != nil || o.Width != 120 || !o.Tailwind {
		t.Fatalf("%+v %v", o, err)
	}
	for _, c := range []struct{ src, want string }{
		{"[fmt]\nwidht = 1\n", "vuka.toml:2: unknown key widht in [fmt]; did you mean width?"},
		{"[build]\n", "vuka.toml:1: unknown table [build]: vuka.toml has [fmt] and [ui]"},
		{"width = 90\n", "vuka.toml:1: width is outside a table: write it under [fmt]"},
		{"[fmt]\ntailwind = yes\n", "vuka.toml:2: tailwind is true or false"},
	} {
		if _, err := ParseOptions("vuka.toml", []byte(c.src)); err == nil || err.Error() != c.want {
			t.Errorf("%q: %v", c.src, err)
		}
	}
}
