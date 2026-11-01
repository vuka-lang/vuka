package transpile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const scriptCounter = `<script lang="go">
package main

import "fmt"

// Counter counts.
type Counter struct {
	n    int
	note string // "</script>" in a comment doesn't end the Go
}

func (c *Counter) Add() { c.n++; c.note = fmt.Sprint("</script>") }
</script>

<main>
  <output>{n}</output>
  <button onClick={Add}>+</button>
</main>
`

// TestSplitTemplate: the script form's parts, its boundary found by Go's
// tokens; the header form's Go and markup.
func TestSplitTemplate(t *testing.T) {
	p, err := transpile.SplitTemplate([]byte(scriptCounter))
	if err != nil {
		t.Fatal(err)
	}
	src := scriptCounter
	if !p.IsScript() || src[p.Script.Open:p.Script.OpenEnd] != `<script lang="go">` || src[p.Script.Close:p.Script.CloseEnd] != "</script>" {
		t.Fatalf("script %+v", p.Script)
	}
	if !strings.HasPrefix(src[p.Markup:], "<main>") || p.GoStart != p.Script.OpenEnd || p.GoEnd != p.Script.Close {
		t.Errorf("parts %+v", p)
	}
	view := transpile.HeaderView([]byte(src))
	if len(view) != len(src) || strings.Count(string(view), "\n") != strings.Count(src, "\n") || strings.Contains(string(view), "<script") {
		t.Errorf("header view:\n%s", view)
	}
	h, herr := transpile.ParseShape([]byte(src))
	if herr != nil || h == nil || h.Type != "Counter" || h.TypeLine != 7 || h.Markup != 14 || !h.Live {
		t.Fatalf("shape %+v, %v", h, herr)
	}
	if hd := transpile.ParseTemplateHead([]byte(src)); hd.Package != "main" || hd.Line != 1 || len(hd.Imports) != 1 {
		t.Errorf("head %+v", hd)
	}

	hdr, _ := transpile.ToScriptForm([]byte(scriptCounter))
	if string(hdr) != scriptCounter {
		t.Errorf("a script-form file is rewritten")
	}
	header := strings.Replace(strings.Replace(scriptCounter, "<script lang=\"go\">\n", "", 1), "</script>\n\n", "", 1)
	p, _ = transpile.SplitTemplate([]byte(header))
	if p.IsScript() || p.GoStart != 0 || !strings.HasPrefix(header[p.Markup:], "<main>") || p.GoEnd != p.Markup {
		t.Errorf("header parts %+v", p)
	}
	out, ok := transpile.ToScriptForm([]byte(header))
	if !ok || string(out) != scriptCounter {
		t.Errorf("rewritten:\n%s", out)
	}
}

// TestScriptFormErrors: an unterminated script, one not first, a second one.
func TestScriptFormErrors(t *testing.T) {
	for _, c := range []struct {
		src, code string
		line, col int
	}{
		{"<script lang=\"go\">\npackage p\n\ntype A struct{}\n<p/>\n", "E0156", 1, 1},
		{"\n\n  <script lang=\"go\">\npackage p\n// </script>\ntype A struct{}\n", "E0156", 3, 3},
		{"package p\n\ntype A struct{}\n<p/>\n<script lang=\"go\">\n</script>\n", "E0157", 5, 1},
		{"<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n<p/>\n<script lang='go'></script>\n", "E0157", 7, 1},
	} {
		_, err := transpile.SplitTemplate([]byte(c.src))
		if err == nil || string(err.Code) != c.code || err.Pos.Line != c.line || err.Pos.Column != c.col {
			t.Errorf("%q: %v", c.src, err)
		}
	}
	// Other scripts are markup.
	src := "<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n<p/>\n<script lang=\"ts\">let a = 1</script>\n<script>x()</script>\n"
	if _, err := transpile.SplitTemplate([]byte(src)); err != nil {
		t.Error(err)
	}
	// Leading comments are not markup.
	src = "<!-- a file comment -->\n<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n<p/>\n"
	if p, err := transpile.SplitTemplate([]byte(src)); err != nil || !p.IsScript() {
		t.Errorf("%+v %v", p, err)
	}
}

var lineDirective = regexp.MustCompile(`(?m)^//line .*\n|/\*line [^*]*\*/`)

// TestScriptFormGoldens: every golden template package compiles the same
// in the script form, but for its line directives.
func TestScriptFormGoldens(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/golden/templates/*")
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			entries, _ := os.ReadDir(dir)
			var files, scripts []transpile.File
			rewritten := false
			for _, e := range entries {
				n := e.Name()
				if strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html") {
					src, _ := os.ReadFile(filepath.Join(dir, n))
					files = append(files, transpile.File{Name: n, Src: src})
					if out, ok := transpile.ToScriptForm(src); ok && strings.HasSuffix(n, ".vuka.html") {
						again, ok2 := transpile.ToScriptForm(out)
						if ok2 || string(again) != string(out) {
							t.Errorf("%s: rewriting twice changes it", n)
						}
						src, rewritten = out, true
					}
					scripts = append(scripts, transpile.File{Name: n, Src: src})
				}
			}
			if !rewritten {
				return
			}
			opts := transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: dir}
			want, werr := transpile.Package(files, opts)
			got, gerr := transpile.Package(scripts, opts)
			if (werr == nil) != (gerr == nil) {
				t.Fatalf("errors: header form %v; script form %v", werr, gerr)
			}
			if werr != nil {
				return
			}
			for i, o := range got.Files {
				g := lineDirective.ReplaceAllString(string(o.Src), "")
				w := lineDirective.ReplaceAllString(string(want.Files[i].Src), "")
				if g != w {
					t.Errorf("%s differs:\n%s\nwant:\n%s", o.GoName, g, w)
				}
			}
		})
	}
}

// TestScriptFormPositions: errors in a script-form file are at its own
// lines and columns: in the Go of the script, and in the markup's holes.
func TestScriptFormPositions(t *testing.T) {
	for _, c := range []struct {
		src       string
		line, col int
	}{
		{"<script lang=\"go\">\npackage main\n\ntype Card struct {\n\tTitle string\n}\n\nfunc (c Card) Upper() string { return c.Title ++ }\n</script>\n\n<p>{Title}</p>\n", 8, 47},
		{"<script lang=\"go\">\npackage main\n\ntype Card struct {\n\tTitle string\n}\n</script>\n\n<div>\n  <p>{Title +}</p>\n</div>\n", 10, 14},
	} {
		errs := transpile.Syntax(transpile.File{Name: "card.vuka.html", Src: []byte(c.src)}, transpile.Options{Importer: load.NewImporter("testdata/golden", "")},
			transpile.File{Name: "main.go", Src: []byte("package main\n")})
		if len(errs) == 0 || errs[0].Pos.Line != c.line || errs[0].Pos.Column != c.col {
			t.Errorf("%q: %v, want %d:%d", c.src, errs, c.line, c.col)
		}
	}
}

// TestScriptVukaSkeleton: everything .vuka allows, in a script, leaves the
// struct and its fields as they are, and declares no struct of its own.
func TestScriptVukaSkeleton(t *testing.T) {
	var enums strings.Builder
	for i := range 20 {
		fmt.Fprintf(&enums, "enum E%d { A, B }\n\n", i)
	}
	for _, c := range []struct{ name, goSrc, typ string }{
		{"enums past the parser's limit", enums.String() + "type Page struct{ N int }\n", "Page"},
		{"enum with payloads", "enum Shape {\n\tCircle{R float64}\n\tRect{W, H float64}\n}\n\ntype Page struct{ N int }\n", "Page"},
		{"decorator holding a struct", "decorator d(c) {\n\ttype inner struct{}\n\tc.Next()\n}\n\ntype Page struct{ N int }\n", "Page"},
		{"decorator with parameters and an alias", "decorator d(n int)(c) {\n\tc.Next()\n}\n\ndecorator D = @d(1) @other\n\ntype Page struct{ N int }\n", "Page"},
		{"a receive above the struct", "func wait(c chan int) {\n\t<-c\n}\n\ntype Page struct{ N int }\n", "Page"},
		{"a raw string above the struct", "func s() string {\n\treturn `\n<div>\n`\n}\n\ntype Page struct{ N int }\n", "Page"},
		{"a static method above the struct", "func Page.New() *Page {\n\treturn &Page{}\n}\n\nfunc Store[T].Make() {}\n\ntype Page struct{ N int }\n", "Page"},
		{"try, match and Result in bodies", "func f() Result[int] {\n\tn := g()?\n\tmatch n {\n\tcase 1:\n\t}\n\treturn Ok(n)\n}\n\ntype Page struct{ N int }\n", "Page"},
		{"a group after a func", "func f() {}\n\ntype (\n\tPage struct{ N int }\n)\n", "Page"},
		{"static fields", "type Page struct {\n\tstatic Table = \"pages\"\n\tN int\n\tstatic const Max = 1\n\tstatic Limits = []int{\n\t\t1,\n\t}\n\tstatic count int\n}\n", "Page"},
		{"a static on the struct's line", "type Page struct{ N int; static X = 1 }\n", "Page"},
		{"attributes on params and fields", "func f(@Path(\"id\") id int) {}\n\ntype Page struct {\n\tN int @PK @Char{Max: 2}\n}\n", "Page"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "<script lang=\"go\">\npackage p\n\n" + c.goSrc + "</script>\n\n<p>{N}</p>\n"
			s, err := transpile.ParseShapeFile("page.vuka.html", []byte(src))
			if err != nil || s == nil || s.Type != c.typ || len(s.Helpers) != 0 {
				t.Fatalf("%+v, %+v", s, err)
			}
			if len(s.Fields) != 1 || s.Fields[0].Name != "N" {
				t.Errorf("fields: %+v", s.Fields)
			}
			if wantLine := strings.Count(src[:strings.Index(src, "<p>")], "\n"); s.Markup != wantLine {
				t.Errorf("markup at line %d, want %d", s.Markup, wantLine)
			}
		})
	}
}

// TestScriptMarkup: markup in a script is E0151 at its tag, whatever comes
// after it: never the struct below it declared nowhere.
func TestScriptMarkup(t *testing.T) {
	src := "<script lang=\"go\">\npackage p\n\nvar tag =\n\t<b>x</b>\n\ntype Page struct{}\n</script>\n\n<p/>\n"
	s, err := transpile.ParseShapeFile("page.vuka.html", []byte(src))
	if s != nil || err == nil || err.Code != "E0151" || err.Pos.Line != 5 || err.Pos.Column != 2 {
		t.Fatalf("%+v, %+v", s, err)
	}
	_, gerr := transpile.Package([]transpile.File{{Name: "page.vuka.html", Src: []byte(src)}}, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if gerr == nil || !strings.Contains(gerr.Error(), "page.vuka.html:5:2") || strings.Count(gerr.Error(), "\n") != 0 {
		t.Errorf("compiled: %v", gerr)
	}
}
