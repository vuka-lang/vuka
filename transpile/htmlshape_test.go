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
		{"pushed event", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<form onSubmit={ui.JS.Busy().Push(p.Save)}/>", ""},
		{"pushed closure", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<form onSubmit={ui.JS.Push(func() { p.Save() })}/>", "the markup sends the event Save"},
		{"pushed hooks", "type P struct{ n int }\n\nfunc (p *P) Save() {}\nfunc (p *P) Done() {}\n<button onClick={ui.JS.Push(Save).Before(ui.JS.Confirm(\"?\")).Success(Done).OnError(p.Done).After(Done)}/>", ""},
		{"a hook's closure", "type P struct{ n int }\n\nfunc (p *P) Save() {}\nfunc (p *P) Done() {}\n<button onClick={ui.JS.Push(Save).After(func() { p.Done() })}/>", "the markup sends the event Done"},
		{"called component", "type P struct{ n int }\n\nfunc (p *P) Save() {}\nfunc (p *P) Seen() {}\n<button onClick={ui.JS.Call(Editor{ID: 1, OnSave: Save}).Closed(Seen)}/>", ""},
		{"a called component's closure", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<button onClick={ui.JS.Call(Editor{OnSave: func() { p.Save() }})}/>", "the markup sends the event Save"},
		{"options", "type P struct{ n int }\n\nfunc (p *P) Cities(c string) []ui.Option { return nil }\n<select onChange={ui.JS.Options(ui.Sel(\"#c\"), Cities, ui.This.Value())}/>", ""},
		{"written and called", "type P struct{ open ui.State[bool] }\n\nfunc (p *P) Save() {}\n<button onClick={open = false; Save()}/>", "the markup sends the event Save"},
		{"read method", "type P struct{ n int }\n\nfunc (p P) Label() string { return \"\" }\n<p>{Label()}</p>", ""},
		{"js commands", "type P struct{ n int }\n<button onClick={ui.JS.Toggle(ui.Ref(\"m\"))}/>", ""},
		{"unexported handler", "type P struct{ n int }\n\nfunc (p *P) inc() {}\n<button onClick={inc}/>", ""},
		{"struct form, exported handler", "type P struct{ Form F }\n\nfunc (p *P) Save() {}\n<ui.Form for={Form} onSubmit={Save}/>", ""},
		{"struct form, selector", "type P struct{ Form F }\n\nfunc (p *P) Save() {}\n<main>\n<ui.Form submitLabel=\"a>b\" onSubmit={p.Save} for={p.Form}/>\n</main>", ""},
		{"struct form, its onChange", "type P struct{ Form F }\n\nfunc (p *P) Check() {}\n<ui.Form for={Form} onChange={Check}/>", "the markup sends the event Check"},
		{"struct form, a closure", "type P struct{ Form F }\n\nfunc (p *P) Save() {}\n<ui.Form for={Form} onSubmit={func() { p.Save() }}/>", "the markup sends the event Save"},
		{"form without for", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<ui.Form onSubmit={Save}/>", "the markup sends the event Save"},
		{"plain form", "type P struct{ n int }\n\nfunc (p *P) Save() {}\n<form onSubmit={Save}/>", "the markup sends the event Save"},
		{"struct form and a click", "type P struct{ Form F }\n\nfunc (p *P) Save() {}\nfunc (p *P) Inc() {}\n<div><ui.Form for={Form} onSubmit={Save}/><button onClick={Inc}/></div>", "the markup sends the event Inc"},
		{"another package's Form", "type P struct{ Form F }\n\nfunc (p *P) Save() {}\n<forms.Form for={Form} onSubmit={Save}/>", "the markup sends the event Save"},
		{"a named ui", "import u \"github.com/vuka-lang/ui/v2\"\n\ntype P struct{ Form F }\n\nfunc (p *P) Save() {}\n<u.Form for={Form} onSubmit={Save}/>", ""},
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

// TestShapeNamedAfterFile: a file declaring several structs renders the one
// named after it, whatever the file's name, and the others are its helpers;
// a file declaring one renders it, whatever it's called.
func TestShapeNamedAfterFile(t *testing.T) {
	src := func(names ...string) []byte {
		var b strings.Builder
		b.WriteString("<script lang=\"go\">\npackage p\n\n")
		for _, n := range names {
			fmt.Fprintf(&b, "type %s struct{}\n\n", n)
		}
		b.WriteString("</script>\n\n<p/>\n")
		return []byte(b.String())
	}
	for _, c := range []struct {
		file    string
		structs []string
		want    string
	}{
		{"orders.vuka.html", []string{"Orders", "Row"}, "Orders"},
		{"orders.vuka.html", []string{"Row", "Orders"}, "Orders"},
		{"pages/orders.vuka.html", []string{"Row", "Note", "Orders"}, "Orders"},
		{"order-row.vuka.html", []string{"Cell", "OrderRow"}, "OrderRow"},
		{"order_row.vuka.html", []string{"Cell", "OrderRow"}, "OrderRow"},
		{"order_row.vuka.html", []string{"Cell", "orderRow"}, "orderRow"},
		{"index.vuka.html", []string{"Row", "Index"}, "Index"},
		{"[id].vuka.html", []string{"Row", "Id"}, "Id"},
		{"[id].vuka.html", []string{"Row", "ID"}, "ID"},
		{"[path...].vuka.html", []string{"Row", "Path"}, "Path"},
		{"_layout.vuka.html", []string{"Link", "Layout"}, "Layout"},
		{"index.vuka.html", []string{"Home"}, "Home"},
		{"[id].vuka.html", []string{"Show"}, "Show"},
	} {
		s, err := transpile.ParseShapeFile(c.file, src(c.structs...))
		if err != nil || s == nil || s.Type != c.want {
			t.Errorf("%s with %v: %+v, %v; want %s", c.file, c.structs, s, err, c.want)
			continue
		}
		if len(s.Helpers) != len(c.structs)-1 {
			t.Errorf("%s with %v: helpers %v", c.file, c.structs, s.Helpers)
		}
	}
	for _, c := range []struct {
		file    string
		structs []string
		msg     string
	}{
		{"index.vuka.html", []string{"Home", "Row"}, "the template file declares Home and Row, and none is named after it: index.vuka.html renders the struct Index"},
		{"order-row.vuka.html", []string{"A", "B", "C"}, "the template file declares A, B and C, and none is named after it: order-row.vuka.html renders the struct OrderRow"},
		{"[id].vuka.html", []string{"Show", "Row"}, "renders the struct Id"},
		{"[id].vuka.html", []string{"Id", "ID"}, "both are named after [id].vuka.html"},
		{"404.vuka.html", []string{"A", "B"}, "no struct can be named after 404.vuka.html"},
		{"", []string{"A", "B"}, "the template file declares A and B: a .vuka.html file renders one struct"},
	} {
		s, err := transpile.ParseShapeFile(c.file, src(c.structs...))
		if s != nil || err == nil || err.Code != "E0147" || !strings.Contains(err.Msg, c.msg) || err.Help == "" {
			t.Errorf("%s with %v: %+v, %+v; want E0147: %s", c.file, c.structs, s, err, c.msg)
		}
		if err != nil && err.Pos.Line < 4 {
			t.Errorf("%s with %v: at line %d", c.file, c.structs, err.Pos.Line)
		}
	}
}

// TestShapeHelpers: what a helper declares isn't the rendered struct's: its
// methods don't make the page live, its fields aren't the page's, its
// receiver isn't the page's, and a form stays the exception it was.
func TestShapeHelpers(t *testing.T) {
	const pkg = "package p\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n"
	t.Run("not live", func(t *testing.T) {
		s, err := transpile.ParseShapeFile("orders.vuka.html", []byte(pkg+`type Row struct {
	ui.Live
	Name string
}

@ui.Mount
func (r *Row) mount() error { return nil }

func (r *Row) Add() {}

type Orders struct {
	Rows []Row
}

func (o Orders) Total() int { return 0 }
<button onClick={Rows[0].Add}>{Total()}</button>
`))
		if err != nil || s == nil {
			t.Fatalf("%+v, %v", s, err)
		}
		if s.Type != "Orders" || s.Live || s.EmbedsLive || s.Recv != "o" || s.Ptr || len(s.Fields) != 1 || s.Fields[0].Name != "Rows" {
			t.Errorf("%+v", s)
		}
		if len(s.Helpers) != 1 || s.Helpers[0] != "Row" {
			t.Errorf("helpers %v", s.Helpers)
		}
	})
	t.Run("the rendered struct's hook", func(t *testing.T) {
		s, err := transpile.ParseShapeFile("orders.vuka.html", []byte(pkg+`type Row struct{}

type Orders struct{}

@ui.Mount
func (o *Orders) mount() error { return nil }
<p/>
`))
		if err != nil || s == nil || !s.Live || s.LiveWhy != "it declares the hook @ui.Mount" {
			t.Errorf("%+v, %v", s, err)
		}
	})
	t.Run("a form beside one struct", func(t *testing.T) {
		s, err := transpile.ParseShapeFile("anything.vuka.html", []byte(pkg+`type Signup struct {
	ui.FormState
	Email string
}

type Join struct {
	Form Signup
}
<p/>
`))
		if err != nil || s == nil || s.Type != "Join" || len(s.Helpers) != 1 {
			t.Errorf("%+v, %v", s, err)
		}
	})
	t.Run("a form type beside one struct", func(t *testing.T) {
		s, err := transpile.ParseShapeFile("anything.vuka.html", []byte(pkg+`type ProductForm struct {
	ui.FormFor[Product]
}

type Products struct {
	Form ProductForm
}
<p/>
`))
		if err != nil || s == nil || s.Type != "Products" || len(s.Helpers) != 1 {
			t.Errorf("%+v, %v", s, err)
		}
	})
	t.Run("a form, a helper and the struct", func(t *testing.T) {
		s, err := transpile.ParseShapeFile("join.vuka.html", []byte(pkg+`type Signup struct {
	ui.FormState
	Email string
}

type Row struct{}

type Join struct {
	Form Signup
}
<p/>
`))
		if err != nil || s == nil || s.Type != "Join" || len(s.Helpers) != 2 {
			t.Errorf("%+v, %v", s, err)
		}
	})
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
