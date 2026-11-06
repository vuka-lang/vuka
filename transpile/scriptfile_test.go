package transpile_test

import (
	"strings"
	"testing"
)

const ordersGo = `package main

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Orders lists the orders.
type Orders struct {
	ui.Live
	Title  string
	orders []int
}

func (p *Orders) Add() { p.orders = append(p.orders, len(p.orders)+1) }

func (p *Orders) label(n int) string { return "#" + strconv.Itoa(n) }
`

const ordersMarkup = `<main>
  <h1>{Title}</h1>
  for _, n := range orders {
    <p>{label(n)} {strconv.Itoa(n)}</p>
  }
  <button onClick={Add}>add</button>
</main>
`

// TestScriptFile: a template of markup alone renders the struct its .go
// script file declares, reading its fields, methods and imports as from an
// inline script (vuka#356).
func TestScriptFile(t *testing.T) {
	res, err := compileShapes(map[string]string{"orders.vuka.html": ordersMarkup, "orders.go": ordersGo})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{"package main", `"strconv"`, "func (p *Orders) Render() ui.Node {", "range p.orders", "p.label(n)", "onClick={p.Add}"} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
}

const ordersVuka = `package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Orders struct {
	ui.Live
	Q      string @ui.Bind
	orders []int
}

@ui.Mount
func (p *Orders) mount(ctx context.Context) error {
	p.orders = []int{1, 2}
	return nil
}

func (p *Orders) Add() { p.orders = append(p.orders, len(p.orders)+1) }
`

func TestScriptFileVuka(t *testing.T) {
	res, err := compileShapes(map[string]string{"orders.vuka.html": "<main>\n  <input bind:value={Q} />\n  <p>{len(orders)}</p>\n  <button onClick={Add}>add</button>\n</main>\n", "orders.vuka": ordersVuka})
	if err != nil {
		t.Fatal(err)
	}
	var all string
	for _, o := range res.Files {
		all += string(o.Src)
	}
	for _, want := range []string{"orders uigen.Assign[[]int]", ".orders.Get())}</p>", ".mount)", "ui.Bind{})", "ui.BindValue("} {
		if !strings.Contains(all, want) {
			t.Errorf("no %s in\n%s", want, all)
		}
	}
}

// TestScriptFileErrors: the struct declared in both places, two structs in
// the script file, and a template of markup alone with no script file.
func TestScriptFileErrors(t *testing.T) {
	inline := "<script lang=\"go\">\npackage main\n\ntype Orders struct{ Title string }\n</script>\n\n<h1>{Title}</h1>\n"
	_, err := compileShapes(map[string]string{"orders.vuka.html": inline, "orders.go": "package main\n\ntype Orders struct{ Title string }\n"})
	if want := "E0174 orders.vuka.html:4:1: Orders is declared twice: in this template's script and in orders.go, the script file beside it"; err == nil || !strings.Contains(codes(err), want) {
		t.Errorf("no %q in %v", want, err)
	}
	_, err = compileShapes(map[string]string{"orders.vuka.html": "<h1>x</h1>\n", "orders.go": "package main\n\ntype A struct{}\n\ntype B struct{}\n"})
	if want := "orders.go, the script of orders.vuka.html:"; err == nil || !strings.Contains(codes(err), "E0147") || !strings.Contains(codes(err), want) {
		t.Errorf("no E0147 %q in %v", want, err)
	}
	_, err = compileShapes(map[string]string{"orders.vuka.html": "<h1>x</h1>\n", "main.go": "package main\n"})
	if want := "declare the struct it renders in orders.go beside it, or in a <script lang=\"go\"> above its markup"; err == nil || !strings.Contains(codes(err), want) {
		t.Errorf("no %q in %v", want, err)
	}
}

