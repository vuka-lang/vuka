package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileShape(t *testing.T) {
	const base = "package p\n\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\n/*line a.vuka:5:1*/return ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Name)\n}\n"
	for _, c := range []struct {
		name, src string
		same      bool
	}{
		{"as it was", base, true},
		{"fixed HTML", "package p\n\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\n/*line a.vuka:5:1*/return ui.FT(0x2, \"E(tEd(h))\", func(__d []any) ui.Node { return ui.F(0x2, \"E(tEd(h))\", ui.El(\"div\", nil, ui.Text(\"hi\"), ui.El(\"p\", []ui.Attr{{Name: \"class\", Value: \"x\"}}, ui.Child(__d[0])))) }, x.Name)\n}\n", true},
		{"a comment and a line directive", "package p\n\n// T.\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\n/*line a.vuka:9:1*/return ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Name)\n}\n", true},
		{"an expression", "package p\n\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\nreturn ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Title)\n}\n", false},
		{"a field", "package p\n\ntype T struct{ Name, Title string }\n\nfunc (x T) Render() ui.Node {\nreturn ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Name)\n}\n", false},
		{"a struct tag", "package p\n\ntype T struct{ Name string `orm:\"x\"` }\n\nfunc (x T) Render() ui.Node {\nreturn ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Name)\n}\n", false},
		{"a string in a body", "package p\n\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\nreturn ui.FT(0x1, \"E(h)\", func(__d []any) ui.Node { return ui.F(0x1, \"E(h)\", ui.El(\"p\", nil, ui.Child(__d[0]))) }, x.Name+\"!\")\n}\n", false},
		{"not the compiler's FT", "package p\n\ntype T struct{ Name string }\n\nfunc (x T) Render() ui.Node {\nreturn FT(0x1, \"a\", func(y []any) ui.Node { return nil })\n}\n", false},
	} {
		if got := fileShape([]byte(c.src)) == fileShape([]byte(base)); got != c.same {
			t.Errorf("%s: same shape = %v, want %v", c.name, got, c.same)
		}
	}
	html := func(markup, args string) [32]byte {
		return fileShape([]byte("package p\n\nfunc f(x T) ui.Node { return ui.HTML(`" + markup + "`, " + args + ") }\n"))
	}
	if html("<p>{x.A}</p>", "x.A") != html("<section><h1>Hi</h1><p class=\"b\">{x.A}!</p></section>", "x.A") {
		t.Error("fixed HTML in ui.HTML changed the shape")
	}
	if html("<p>{x.A}</p>", "x.A") == html("<p>{x.B}</p>", "x.B") {
		t.Error("an expression in ui.HTML left the shape")
	}
	// Two versions of FT that isn't the compiler's stay apart too.
	a := "package p\n\nfunc f() { FT(1, \"a\", func(y []any) {}) }\n"
	b := "package p\n\nfunc f() { FT(2, \"a\", func(y []any) {}) }\n"
	if fileShape([]byte(a)) == fileShape([]byte(b)) {
		t.Error("an FT of the module's own lost its arguments")
	}
}

func TestSourceShapesModule(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	write := func(path, src string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(filepath.Join(root, "go.mod"), "module m\n\ngo 1.25\n")
	lib := write(filepath.Join(root, "lib", "lib.go"), "package lib\n\nfunc F() int { return 1 }\n")
	write(filepath.Join(root, "node_modules", "x", "x.go"), "package x\n")
	test := write(filepath.Join(root, "lib", "lib_test.go"), "package lib\n")

	s := newSourceShapes()
	first := s.module(root, "", false)
	if first == "" {
		t.Fatal("no shape")
	}
	if got := s.module(root, "", false); got != first {
		t.Error("nothing changed, the shape did")
	}
	write(filepath.Join(root, "node_modules", "x", "x.go"), "package x\n\nvar Y int\n")
	write(test, "package lib\n\nvar Y int\n")
	if got := s.module(root, "", false); got != first {
		t.Error("a file the build doesn't read changed the shape")
	}
	write(lib, "package lib\n\nfunc F() int { return 2 }\n")
	second := s.module(root, "", false)
	if second == first {
		t.Error("a function's body left the shape")
	}

	// A generated file exists in the overlay only.
	gen := write(filepath.Join(t.TempDir(), "gen.go"), "package lib\n\nvar A int\n")
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	data, _ := json.Marshal(struct{ Replace map[string]string }{map[string]string{filepath.Join(root, "lib", "gen_vuka.go"): gen}})
	write(overlay, string(data))
	third := s.module(root, overlay, false)
	if third == second {
		t.Error("the overlay's file left the shape")
	}
	write(gen, "package lib\n\nvar B int\n")
	if s.module(root, overlay, false) == third {
		t.Error("the overlay's content left the shape")
	}

	write(filepath.Join(root, "go.mod"), "module m\n\ngo 1.25\n\nreplace x => ../x\n")
	if got := s.module(root, "", false); got != "" {
		t.Error("a dependency in a directory can change unseen:", got)
	}
	if got := (*sourceShapes)(nil).module(root, "", false); got != "" {
		t.Error("outside a watch:", got)
	}
}
