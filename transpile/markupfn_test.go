package transpile_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const markupFnShape = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Order struct {
	ID     int
	Number string
}

type Review struct {
	ui.Live
	Title  string
	Orders []Order
	open   bool
}

func (p *Review) Approve(id int) error { return nil }
func (p *Review) label(o Order) string { return p.Title + o.Number }
</script>

`

const markupFnList = `package main

import (
	"context"
	"io"

	ui "vukatest.local/opttarget"
)

type List struct {
	Items []Order
	Row   func(Order) ui.Node
}

func (l List) View() ui.Node                                    { return nil }
func (l List) Render(ctx context.Context, w io.Writer) error { return nil }

type Labels struct {
	Format func(Order) string
}

func (l Labels) View() ui.Node                                    { return nil }
func (l Labels) Render(ctx context.Context, w io.Writer) error { return nil }
`

// goTypeErrors type-checks the Go a review.vuka.html compiled to, with
// markupFnList, and returns its errors: their positions are the //line
// directives', the template's.
func goTypeErrors(t *testing.T, src string) []types.Error {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for name, text := range map[string]string{"review_html.go": src, "list.go": markupFnList} {
		f, err := parser.ParseFile(fset, name, text, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	var errs []types.Error
	conf := types.Config{Importer: load.NewImporter("testdata/golden", ""), Error: func(err error) { errs = append(errs, err.(types.Error)) }}
	conf.Check("main", fset, files, nil)
	return errs
}

func compileMarkupFn(t *testing.T, markup string) (string, error) {
	t.Helper()
	res, err := compileShapes(map[string]string{"review.vuka.html": markupFnShape + markup, "list.go": markupFnList})
	if err != nil {
		return "", err
	}
	return string(res.Files[0].Src), nil
}

// compileMarkupFnGo is compileMarkupFn for markup whose Go type-checks.
func compileMarkupFnGo(t *testing.T, markup string) string {
	t.Helper()
	src, err := compileMarkupFn(t, markup)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range goTypeErrors(t, src) {
		t.Errorf("%s: %s", e.Fset.Position(e.Pos), e.Msg)
	}
	return src
}

// TestMarkupFuncs: a func literal bound by a template statement or set on
// a component's Node func prop takes markup for its body, closes over the
// shape's props, states and methods, and its events push the shape's
// methods as the main markup's do.
func TestMarkupFuncs(t *testing.T) {
	src := compileMarkupFnGo(t, `row := func(o Order) {
  <li>
    {label(o)} {Title}
    if open { <b>open</b> }
    <button onClick={Approve(o.ID)}>approve</button>
  </li>
}
typed := func(o Order) ui.Node { <i>{o.Number}</i> }

<section>
  for _, o := range Orders {
    {row(o)}
    {typed(o)}
  }
  <List items={Orders} row={row}/>
  <List items={Orders} row={func(o Order) {
    inner := func(n string) { <em>{n}</em> }
    {inner(o.Number)}
    <List items={Orders} row={func(o Order) { <u>{o.ID}</u> }}/>
  }}/>
</section>
`)
	for _, want := range []string{
		"row := func(o Order) ui.Node {",
		"typed := func(o Order) ui.Node {",
		"inner := func(n string) ui.Node {",
		"p.label(o)",
		"p.Title",
		"if p.open {",
		"ui.JS.Push(p.Approve, o.ID)",
		"Row: row}",
		"Row: func(o Order) ui.Node {",
		"<u>{o.ID}</u>",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %q in:\n%s", want, src)
		}
	}
}

// TestMarkupFuncScopes: a binding in a markup func's body is the body's,
// as a Go func's: the main markup binds the same name apart.
func TestMarkupFuncScopes(t *testing.T) {
	src := compileMarkupFnGo(t, `row := func(o Order) {
  n := len(o.Number)
  l := label(o)
  <li>{n} {l}</li>
}
<section>
  n := len(Orders)
  <p>{n}</p>
  for _, o := range Orders { {row(o)} }
</section>
`)
	if !strings.Contains(src, "n := len(o.Number)") || !strings.Contains(src, "n := len(p.Orders)") {
		t.Errorf("bindings:\n%s", src)
	}
}

// TestMarkupFuncRecursion: a var declared in the markup holds a markup func
// calling itself, as in Go.
func TestMarkupFuncRecursion(t *testing.T) {
	src := compileMarkupFnGo(t, `<ul>
  var tree func(o Order, depth int) ui.Node
  tree = func(o Order, depth int) {
    <li>
      {o.Number}
      if depth > 0 { <ul>{tree(o, depth-1)}</ul> }
    </li>
  }
  for _, o := range Orders { {tree(o, 2)} }
</ul>
`)
	for _, want := range []string{"var tree func(o Order, depth int) ui.Node", "tree = func(o Order, depth int) ui.Node {", "tree(o, depth-1)"} {
		if !strings.Contains(src, want) {
			t.Errorf("no %q in:\n%s", want, src)
		}
	}
}

// TestMarkupFuncErrors: each error a markup func makes is reported at its
// place in the file, inside the literal's body too.
func TestMarkupFuncErrors(t *testing.T) {
	// The markup starts on line 22; Go's own errors carry their line alone.
	for _, c := range []struct {
		name, markup string
		code         string
		pos          string
		msg          string
	}{
		{"event in body", `row := func(o Order) {
  <li><button onClick={Aprove(o.ID)}>approve</button></li>
}
<ul>{row(Orders[0])}</ul>
`, "E0172", "23:24", "Aprove isn't a method"},
		{"nested body", `row := func(o Order) {
  inner := func() {
    <b>{o.Numbr}</b>
  }
  {inner()}
}
<ul>{row(Orders[0])}</ul>
`, "E0002", "24", "Numbr"},
		{"redeclared", `row := func(o Order) { <li/> }
row := func(o Order) { <p/> }
<ul>{row(Orders[0])}</ul>
`, "E0002", "23", "no new variables"},
		{"in a hole", `<ul>{func(o Order) { <li/> }}</ul>
`, "E0197", "22:22", "where the literal is Go"},
		{"in an event", `<button onClick={func() { <b/> }}>x</button>
`, "E0197", "22:27", "where the literal is Go"},
		{"on an element", `<div title={func() { <b/> }}>x</div>
`, "E0197", "22:22", "where the literal is Go"},
		{"prop not Node", `<Labels format={func(o Order) { <b/> }}/>
`, "E0197", "22:9", "format is func("},
		{"params", `row := func(o Order i int) { <li/> }
`, "E0198", "22:12", "parameters"},
		{"untyped param", `row := func(o) { <li>{o}</li> }
<ul>{row(1)}</ul>
`, "E0198", "22:13", "o has no type"},
		{"event call", `row := func(o Order) { <li/> }
<button onClick={row(Orders[0])}>x</button>
`, "E0199", "23:18", "renders markup"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src, err := compileMarkupFn(t, c.markup)
			if c.code == "E0002" {
				// Go's own errors, which go build reports at the file's lines.
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range goTypeErrors(t, src) {
					if strings.Contains(e.Msg, c.msg) {
						if got := strings.TrimPrefix(e.Fset.Position(e.Pos).String(), "review.vuka.html:"); got != c.pos {
							t.Errorf("%s at %s, want %s", e.Msg, got, c.pos)
						}
						return
					}
				}
				t.Fatalf("no type error %q in:\n%s", c.msg, src)
			}
			var list transpile.ErrorList
			if !errors.As(err, &list) {
				t.Fatalf("want %s, got %v", c.code, err)
			}
			for _, e := range list {
				if string(e.Code) == c.code && strings.Contains(e.Msg, c.msg) {
					if got := strings.TrimPrefix(e.Pos.String(), "review.vuka.html:"); got != c.pos {
						t.Errorf("%s at %s, want %s", e.Msg, got, c.pos)
					}
					return
				}
			}
			t.Errorf("no %s %q among:\n%v", c.code, c.msg, err)
		})
	}
}

// TestMarkupFuncGoLiteral: a func literal writing a result other than Node
// is Go, bound or set on a prop, as it always was.
func TestMarkupFuncGoLiteral(t *testing.T) {
	src := compileMarkupFnGo(t, `label := func(o Order) string { return o.Number }
<section>
  <p>{label(Orders[0])}</p>
  <Labels format={func(o Order) string { return o.Number }}/>
</section>
`)
	for _, want := range []string{"label := func(o Order) string { return o.Number }", "Format: func(o Order) string { return o.Number }"} {
		if !strings.Contains(src, want) {
			t.Errorf("no %q in:\n%s", want, src)
		}
	}
}
