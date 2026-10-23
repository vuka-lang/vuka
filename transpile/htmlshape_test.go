package transpile_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

const shapeIndex = `package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Index struct {
	ui.Live
	Title  string
	ID     int64 @ui.URL("id")
	store  *Store
	rows   []Todo
	show   ui.State[string]
	Filter ui.State[bool]
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows = p.store.All()?
	return nil
}

func (p *Index) Add(ctx context.Context, f NewTodo) error { return nil }
<main>{Title}</main>
`

// TestParseShape: a template file is read as its header and its markup, its
// one struct's fields classified.
func TestParseShape(t *testing.T) {
	s, err := transpile.ParseShape([]byte(shapeIndex))
	if err != nil || s == nil {
		t.Fatalf("ParseShape: %v, %v", s, err)
	}
	if s.Header != 8 || s.Markup != 25 || s.Type != "Index" || s.TypeLine != 9 || s.Recv != "p" || !s.Ptr {
		t.Errorf("shape: header %d, markup %d, type %s at %d, recv %q, ptr %v", s.Header, s.Markup, s.Type, s.TypeLine, s.Recv, s.Ptr)
	}
	var got []string
	for _, f := range s.Fields {
		got = append(got, fmt.Sprintf("%s %s: %s @%d", f.Name, f.Type, f.Role, f.Line))
	}
	want := []string{
		"Live ui.Live: embedded @10",
		"Title string: prop @11",
		"ID int64: prop @12",
		"store *Store: server state @13",
		"rows []Todo: server state @14",
		"show ui.State[string]: browser state @15",
		"Filter ui.State[bool]: browser state @16",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("fields:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !s.Live || !s.EmbedsLive {
		t.Errorf("live %v (%s), embeds %v", s.Live, s.LiveWhy, s.EmbedsLive)
	}
}

// TestParseGenericShape: a generic shape's type arguments, its fields after
// one whose attribute chains a call (.Default(1)) included, and its hook.
func TestParseGenericShape(t *testing.T) {
	src := `package components

import "context"

type Table[K comparable, V any] struct {
	Rows  map[K]V
	page  int    @ui.URL("page").Default(1) @ui.Bind{}
	Sort  string @ui.URL("sort")
	shown int
}

@ui.Mount
func (t *Table[K, V]) mount(ctx context.Context) error { return nil }
<p>{page}</p>
`
	s, err := transpile.ParseShape([]byte(src))
	if err != nil || s == nil {
		t.Fatalf("ParseShape: %v, %v", s, err)
	}
	var got []string
	for _, f := range s.Fields {
		got = append(got, fmt.Sprintf("%s %s: %s @%d", f.Name, f.Type, f.Role, f.Line))
	}
	want := []string{
		"Rows map[K]V: prop @6",
		"page int: server state @7",
		"Sort string: prop @8",
		"shown int: server state @9",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("fields:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !s.Generic || s.TypeArgs != "[K, V]" || !s.Live || s.Recv != "t" {
		t.Errorf("generic %v %q, live %v (%s), recv %q", s.Generic, s.TypeArgs, s.Live, s.LiveWhy, s.Recv)
	}
}

// TestShapeLive: what makes a struct live — embedding ui.Live, a hook, an
// event its markup sends — and what leaves it static.
func TestShapeLive(t *testing.T) {
	for _, c := range []struct{ name, src, why string }{
		{"embeds", "type P struct{ ui.Live }\n<p/>", "it embeds ui.Live"},
		{"mount", "type P struct{ n int }\n\n@ui.Mount\nfunc (p *P) mount() {}\n<p/>", "it declares the hook @ui.Mount"},
		{"loads", "type P struct{ n int }\n\n@ui.Loads(\"lists\")\nfunc (p *P) load() {}\n<p/>", "it declares the hook @ui.Loads"},
		{"onchange", "type P struct{ n int }\n\n@ui.OnChange\nfunc (p *P) changed() {}\n<p/>", "it declares the hook @ui.OnChange"},
		{"bare event", "type P struct{ n int }\n\nfunc (p *P) Inc() { p.n++ }\n<button onClick={Inc}/>", "the markup sends the event Inc"},
		{"selector event", "type P struct{ n int }\n\nfunc (p *P) Inc() {}\n<button onClick={p.Inc}/>", "the markup sends the event Inc"},
		{"pushed event", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<form onSubmit={ui.JS.Busy().Push(p.Save)}/>", "the markup sends the event Save"},
		{"read method", "type P struct{ n int }\n\nfunc (p P) Label() string { return \"\" }\n<p>{Label()}</p>", ""},
		{"js commands", "type P struct{ n int }\n<button onClick={ui.JS.Toggle(ui.Ref(\"m\"))}/>", ""},
		{"unexported handler", "type P struct{ n int }\n\nfunc (p *P) inc() {}\n<button onClick={inc}/>", ""},
		{"other decorator", "type P struct{ n int }\n\n@audited\nfunc (p *P) Inc() {}\n<p/>", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := transpile.ParseShape([]byte("package p\n\n" + c.src + "\n"))
			if err != nil || s == nil {
				t.Fatalf("ParseShape: %v, %v", s, err)
			}
			if s.Live != (c.why != "") || s.LiveWhy != c.why {
				t.Errorf("live %v (%q), want %q", s.Live, s.LiveWhy, c.why)
			}
		})
	}
}

// TestShapeRules: a template file renders one struct, and has markup; a
// file with no struct above its markup isn't read as the shape (compiled,
// it is E0152).
func TestShapeRules(t *testing.T) {
	for _, c := range []struct{ name, src, code string }{
		{"no struct", "var x = 1\n<p/>", "E0146"},
		{"type alias only", "type ID int\n<p/>", "E0146"},
		{"two structs", "type A struct{}\n\ntype B struct{}\n<p/>", "E0147"},
		{"grouped structs", "type (\n\tA struct{}\n\tB struct{}\n)\n<p/>", "E0147"},
		{"no markup", "type A struct{}\n", "E0148"},
		{"markup in a body only", "type A struct{}\n\nfunc (a A) F() {\n<p/>\n}\n", "E0148"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := transpile.ParseShape([]byte("package p\n\n" + c.src))
			if err == nil || string(err.Code) != c.code {
				t.Errorf("ParseShape: %v, %v; want %s", s, err, c.code)
			}
		})
	}
	for _, src := range []string{
		"package p\n\n<p>{Title}</p>\n",
		"package p\n\nn := len(Week)\n<p>{n}</p>\n",
		"package p\n\nfunc Card(title string)\n<h1>{title}</h1>\n",
		"package p\n\n// Card is a card.\nfunc (c Card) View()\n<div/>\n",
		"package p\n\nif ok {\n  <p/>\n}\n",
	} {
		if s, err := transpile.ParseShape([]byte(src)); s != nil || err != nil {
			t.Errorf("a file with no struct above its markup read as the shape: %v, %v\n%s", s, err, src)
		}
	}
}

// TestShapeTemplateHeaders: a header's methods aren't templates; a func line
// after its markup is (a second template, E0153).
func TestShapeTemplateHeaders(t *testing.T) {
	src := "package p\n\ntype A struct{}\n\nfunc (a A) F() int { return 1 }\n<p/>\n\nfunc row(n int)\n<i>{n}</i>\n"
	hs := transpile.TemplateHeaders([]byte(src))
	if len(hs) != 1 || hs[0].Name != "row" || hs[0].Line != 8 {
		t.Errorf("headers: %+v", hs)
	}
	if got := transpile.BlankTemplate([]byte(src), strings.Index(src, "return 1")); got != nil {
		t.Errorf("blanking in the header: %q", got)
	}
}

// TestGoName: the Go file of a source Go wouldn't compile under its own
// name: a folder route's parameter file, [id].vuka.html, and a private
// _layout.vuka.html, which go would ignore.
func TestGoName(t *testing.T) {
	for in, want := range map[string]string{
		"index.vuka.html":    "index_html.go",
		"[id].vuka.html":     "param_id_html.go",
		"[path...].vuka":     "param_path_vuka.go",
		"_layout.vuka.html":  "private_layout_html.go",
		"pets_test.vuka":     "pets_vuka_test.go",
		"[id]_test.vuka":     "param_id_vuka_test.go",
		"team-settings.vuka": "team-settings_vuka.go",
	} {
		if got := transpile.GoName(in); got != want {
			t.Errorf("GoName(%s) = %s, want %s", in, got, want)
		}
	}
}
