package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// TestScopes marks the elements a scoped declaration's JSX writes — in its
// methods, blocks and a component tag's children — and nothing else: not a
// component's tag, not another declaration's markup, not a package-level
// var's.
func TestScopes(t *testing.T) {
	src := `package main

import "github.com/vuka-lang/ui"

type Card struct {
	ui.Live
	Title    string
	Children ui.Node
}

func (c *Card) Render() ui.Node { return <section><h2>{c.Title}</h2>{c.Children}</section> }

type Counter struct {
	ui.Live
	n int
}

func (c *Counter) Render() ui.Node {
	return <div>
		<Card Title="x"><p>inside</p></Card>
		{for i := range 2 { <span>{i}</span> }}
	</div>
}

func (c *Counter) row() ui.Node { return <li /> }

var banner = <aside />

func page(p ui.Node) ui.Node { return <main>{p}</main> }

func main() {}
`
	dir := "testdata/golden"
	res, err := transpile.Package([]transpile.File{{Name: "scope.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir, Scopes: map[string]string{"Counter": "k1", "page": "p2"}})
	if err != nil {
		t.Fatal(err)
	}
	out := string(res.Files[0].Src)
	mark := func(id string) string { return ` data-vk-s="` + id + `"` }
	for _, el := range []string{"div", "p", "span", "li"} {
		if !strings.Contains(out, "<"+el+mark("k1")) {
			t.Errorf("<%s> of Counter isn't marked:\n%s", el, out)
		}
	}
	if !strings.Contains(out, "<main"+mark("p2")) {
		t.Errorf("page's <main> isn't marked:\n%s", out)
	}
	for _, el := range []string{"section>", "h2>", "aside/>"} {
		if !strings.Contains(out, "<"+el) {
			t.Errorf("<%s is marked:\n%s", el, out)
		}
	}
	if strings.Count(out, "data-vk-s") != 5 {
		t.Errorf("want 5 marks:\n%s", out)
	}
}
