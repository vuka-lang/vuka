package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	first := s.module(root, "", false, nil)
	if first == "" {
		t.Fatal("no shape")
	}
	if got := s.module(root, "", false, nil); got != first {
		t.Error("nothing changed, the shape did")
	}
	write(filepath.Join(root, "node_modules", "x", "x.go"), "package x\n\nvar Y int\n")
	write(test, "package lib\n\nvar Y int\n")
	if got := s.module(root, "", false, nil); got != first {
		t.Error("a file the build doesn't read changed the shape")
	}
	write(lib, "package lib\n\nfunc F() int { return 2 }\n")
	second := s.module(root, "", false, nil)
	if second == first {
		t.Error("a function's body left the shape")
	}

	// A generated file exists in the overlay only.
	gen := write(filepath.Join(t.TempDir(), "gen.go"), "package lib\n\nvar A int\n")
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	data, _ := json.Marshal(struct{ Replace map[string]string }{map[string]string{filepath.Join(root, "lib", "gen_vuka.go"): gen}})
	write(overlay, string(data))
	third := s.module(root, overlay, false, nil)
	if third == second {
		t.Error("the overlay's file left the shape")
	}
	write(gen, "package lib\n\nvar B int\n")
	if s.module(root, overlay, false, nil) == third {
		t.Error("the overlay's content left the shape")
	}

	write(filepath.Join(root, "go.mod"), "module m\n\ngo 1.25\n\nreplace x => ../x\n")
	if got := s.module(root, "", false, nil); got != "" {
		t.Error("a dependency in a directory can change unseen:", got)
	}
	if got := (*sourceShapes)(nil).module(root, "", false, nil); got != "" {
		t.Error("outside a watch:", got)
	}
}

func TestDeclarationShape(t *testing.T) {
	const base = "package p\n\ntype T struct{ Name string }\n\nfunc F(x int) int { return x + 1 }\n\nfunc (T) Schema() int { return 1 }\n\nfunc M(db any) any { return orm.For[T](db) }\n\nvar V = func() int { return 1 }\n"
	for _, c := range []struct {
		name, old, new string
		same           bool
	}{
		{"a function's body", "return x + 1", "return x + 2", true},
		{"a function's signature", "F(x int) int", "F(x int) string", false},
		{"a struct field", "Name string", "Name, Title string", false},
		{"a Schema method's body", "return 1 }\n\nfunc M", "return 2 }\n\nfunc M", false},
		{"a body's orm.For", "orm.For[T](db)", "orm.For[U](db)", false},
		{"a package variable's function", "func() int { return 1 }", "func() int { return 2 }", false},
		{"a new function", "func F(", "func G() {}\n\nfunc F(", false},
		{"a new orm.For in a body", "return x + 1", "orm.For[U](nil); return x + 1", false},
	} {
		src := strings.Replace(base, c.old, c.new, 1)
		if src == base {
			t.Fatalf("%s: nothing replaced", c.name)
		}
		if got := shapesOf([]byte(src)).decls == shapesOf([]byte(base)).decls; got != c.same {
			t.Errorf("%s: same declarations = %v, want %v", c.name, got, c.same)
		}
		if shapesOf([]byte(src)).full == shapesOf([]byte(base)).full {
			t.Errorf("%s: the full shape didn't change", c.name)
		}
	}
	if a, b := shapesOf([]byte("package p\nfunc (")), shapesOf([]byte("package p\nfunc ((")); a.decls == b.decls || a.full != a.decls {
		t.Error("a file that doesn't parse should hash as it is")
	}
}

func TestSourceShapesBodies(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module m\n\ngo 1.25\n")
	write("lib/lib.go", "package lib\n\nfunc F() int { return 1 }\n")
	write("pages/index.go", "package pages\n\nfunc G() int { return 1 }\n")
	s := newSourceShapes()
	t2 := &routeTree{dirs: []*routeDir{{dir: filepath.Join(root, "pages"), goDir: filepath.Join(root, "pages")}}}
	first := s.module(root, "", false, t2.bodiesRead)
	write("lib/lib.go", "package lib\n\nfunc F() int { return 2 }\n")
	if s.module(root, "", false, t2.bodiesRead) != first {
		t.Error("a body outside the pages changed the route check's shape")
	}
	write("lib/lib.go", "package lib\n\nfunc F() string { return \"\" }\n")
	second := s.module(root, "", false, t2.bodiesRead)
	if second == first {
		t.Error("a signature outside the pages left the route check's shape")
	}
	write("pages/index.go", "package pages\n\nfunc G() int { return 2 }\n")
	if s.module(root, "", false, t2.bodiesRead) == second {
		t.Error("a body in the pages left the route check's shape")
	}

	orm := s.module(root, "", false, ormBodiesRead)
	write("lib/x_gen.go", "package lib\n\nfunc H() int { return 1 }\n")
	gen := s.module(root, "", false, ormBodiesRead)
	if gen == orm {
		t.Error("a new generated file left the orm's shape")
	}
	write("lib/x_gen.go", "package lib\n\nfunc H() int { return 2 }\n")
	if s.module(root, "", false, ormBodiesRead) == gen {
		t.Error("a generated file's body left the orm's shape")
	}
}
