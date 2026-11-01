package check

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestRoutes(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Routes, "routes") }
func TestLive(t *testing.T)   { analysistest.Run(t, analysistest.TestData(), Live, "live") }
func TestTemplates(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "templates")
}
func TestSchema(t *testing.T)  { analysistest.Run(t, analysistest.TestData(), Schema, "schema") }
func TestQueries(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Queries, "queries") }
func TestParts(t *testing.T)   { analysistest.Run(t, analysistest.TestData(), Parts, "parts") }
func TestURLState(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), URLState, "urlstate", "urlstate/table")
}

type collect []string

func (c *collect) Errorf(format string, args ...any) { *c = append(*c, fmt.Sprintf(format, args...)) }

func TestBareNames(t *testing.T) {
	var got collect
	analysistest.Run(&got, analysistest.TestData(), Templates, "bare")
	want := []string{
		"card.vuka.html:4:14: unexpected diagnostic: W0101: Title is Card's field or method and a name of the package; the template reads the field",
		"card.vuka.html:4:30: unexpected diagnostic: W0101: Label is Card's field or method and a name of the package; the template reads the field",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("got %q, want %q", got[i], w)
		}
	}
}

func TestListKeys(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Templates, "keys") }

// TestShape: in a file in the 2.0 shape the header's Go is not templates —
// its exported methods and element-named functions aren't reported — and
// the markup below it is its struct's.
func TestShape(t *testing.T) {
	var got collect
	analysistest.Run(&got, analysistest.TestData(), Templates, "shape")
	want := []string{
		"page.vuka.html:14:8: unexpected diagnostic: W0101: Title is Page's field or method and a name of the package; the template reads the field",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("got %q, want %q", got[i], w)
		}
	}
}

// TestHeaderForm: a file in the Go-header form is W0107, its fix the script
// form; one in the script form is not reported.
func TestHeaderForm(t *testing.T) {
	var got collect
	res := analysistest.Run(&got, analysistest.TestData(), Templates, "headerform")
	if len(got) != 1 || !strings.Contains(got[0], "card.vuka.html:1:1: unexpected diagnostic: W0107: Card's Go is a header above its markup") {
		t.Fatalf("got %q", got)
	}
	d := res[0].Diagnostics[0]
	f := res[0].Pass.Fset.File(d.Pos)
	src, _ := os.ReadFile(f.Name())
	edits := d.SuggestedFixes[0].TextEdits
	out := string(src)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = out[:f.Offset(e.Pos)] + string(e.NewText) + out[f.Offset(e.End):]
	}
	want, _ := os.ReadFile(f.Name() + ".fixed")
	if out != string(want) {
		t.Errorf("fixed:\n%s\nwant:\n%s", out, want)
	}
}

// TestHandWrittenGen: uigen in a file a person wrote is W0105; in a
// generated one, nothing.
func TestHandWrittenGen(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "uigenuse")
}

// TestHandWrittenSchema: a LiveSchema method a person wrote is W0306; a
// generated one, nothing.
func TestHandWrittenSchema(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Live, "schemause")
}

func TestPages(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Routes, "pages") }

// TestLayoutsReadingTheRequest: a folder's layout reading the request,
// through a function of its package, is W0703, and so is one reading the
// page's title in its Load; one that doesn't, or reads the title in its
// markup, nothing.
func TestLayoutsReadingTheRequest(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Routes, "layouts/crumbs", "layouts/shell", "layouts/titled", "layouts/header", "layouts/vars")
}

func TestHandWrittenHTML(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "htmluse")
}

// TestContext: main's list of context processors, as the app's boot
// checks it, and the web.Var reads of the packages it links in.
func TestContext(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Context, "ctxapp/cmd/web")
}

func TestForms(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Forms, "forms") }

func TestFormAttrs(t *testing.T) {
	formAttrs = true
	defer func() { formAttrs = false }()
	analysistest.Run(t, analysistest.TestData(), Forms, "formattrs")
}

// TestShapeOf: the struct a file renders and where its markup starts, read as
// the compiler reads them: helpers named or not, and Go starting a line with <
// above the struct.
func TestShapeOf(t *testing.T) {
	const pkg = "package p\n\n"
	for _, c := range []struct{ name, file, src, typ string }{
		{"one struct", "index.vuka.html", pkg + "type Home struct{}\n<p/>\n", "Home"},
		{"named after the file", "order-row.vuka.html", pkg + "type Cell struct{}\n\ntype OrderRow struct{}\n<p/>\n", "OrderRow"},
		{"a bracket", "[id].vuka.html", pkg + "type Row struct{}\n\ntype ID struct{}\n<p/>\n", "ID"},
		{"a form first", "any.vuka.html", pkg + "type Signup struct {\n\tui.FormState\n}\n\ntype Join struct{}\n<p/>\n", "Join"},
		{"a receive above it", "page.vuka.html", pkg + "func wait(c chan int) {\n\t<-c\n}\n\ntype Page struct{}\n<p/>\n", "Page"},
		{"a raw string above it", "page.vuka.html", pkg + "func s() string {\n\treturn `\n<div>\n`\n}\n\ntype Page struct{}\n<p/>\n", "Page"},
		{"no struct", "page.vuka.html", pkg + "<p/>\n", ""},
		{"a func template", "page.vuka.html", pkg + "func Page(title string)\n<p/>\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			typ, at, ok := shapeOf(c.file, c.src)
			if typ != c.typ || ok != (c.typ != "") {
				t.Fatalf("%q, %v; want %q", typ, ok, c.typ)
			}
			if ok && !strings.HasPrefix(c.src[at:], "<p/>") {
				t.Errorf("markup at %d: %q", at, c.src[at:])
			}
		})
	}
}
