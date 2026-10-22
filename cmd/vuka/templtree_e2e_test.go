package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// treeTempl is a broad set of templ: text, attributes, loops, branches,
// components with children, JSX inside templ and templ inside JSX.
const treeTempl = `package views

import (
	"fmt"
	"strings"

	"github.com/vuka-lang/ui/v2"
)

type Item struct {
	ID   int
	Name string
	Done bool
	Tags []string
}

templ Text(name string, n int) {
	<p>Hello, { name }! You have { fmt.Sprint(n) } items.</p>
	<p>{ "" }{ name }</p>
	<span title={ name }>{ strings.ToUpper(name) } &amp; { "<b>" }</span>
}

templ Attrs(href string, on bool, attrs templ.Attributes, extra string) {
	<a href={ templ.SafeURL(href) } class={ "btn", templ.KV("on", on), templ.KV("off", !on) } data-x={ extra } disabled?={ on } { attrs... }>link</a>
	<input type="text" value={ extra } checked?={ !on }/>
	<div style="color: red" class="static">s</div>
	<a href={ templ.URL(href) }>u</a>
}

templ List(items []Item) {
	<ul>
		for i, it := range items {
			if i == 3 {
				{{ continue }}
			}
			<li id={ fmt.Sprint(it.ID) } class={ templ.KV("done", it.Done) }>
				{ it.Name }
				for _, t := range it.Tags {
					<b>{ t }</b>
				}
			</li>
			if i == 5 {
				{{ break }}
			}
		}
	</ul>
	for i := 0; i < len(items) && i < 2; i++ {
		<i>{ fmt.Sprint(i) }</i>
	}
}

templ Branch(n int, s string) {
	if n > 10 {
		<b>big</b>
	} else if n > 5 {
		<i>mid { fmt.Sprint(n) }</i>
	} else {
		small
	}
	switch s {
		case "a":
			<p>A</p>
		case "b":
			<p>B { s }</p>
		default:
			<p>other</p>
	}
	if n == 0 {
	}
}

templ Card(title string) {
	<section><h2>{ title }</h2>{ children... }</section>
}

templ Wrap() {
	{ children... }
}

templ Page(title string, items []Item) {
	@Card(title) {
		<p>inside</p>
		@List(items)
		@Card("nested") {
			deep { title }
		}
	}
	@Card("empty")
	@templ.Raw("<hr>")
	@Wrap() {
		<em>wrapped</em>
	}
}

templ WithNode(n ui.Node, label string) {
	<div>
		@n
		<span>{ label }</span>
		@ui.Text(label)
		@ui.El("em", nil, ui.Text(label))
	</div>
}

templ Script(v string) {
	<script>var x = { v };</script>
	<style>p { color: red; }</style>
	<p onclick={ templ.JSFuncCall("alert", v) }>click</p>
}

templ GoCode(xs []string) {
	{{ n := len(xs) }}
	<p>{ fmt.Sprint(n) }</p>
	{{ joined := strings.Join(xs, ",") }}
	<p>{ joined }</p>
}

templ Comps(items []Item) {
	for _, it := range items {
		@Card(it.Name) {
			{ fmt.Sprint(it.ID) }
		}
	}
}

templ Empty() {
}
`

const treeMain = `package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/vuka-lang/ui/v2"

	"example.com/site/plain"
	"example.com/site/views"
)

func jsx(label string) ui.Node { return Bold{Label: label} }

func templInJSX(title string) ui.Node { return TemplIn{Title: title} }

func plainInJSX(title string) ui.Node { return PlainIn{Title: title} }

type tcase struct {
	name     string
	live     templ.Component // instrumented
	plain    templ.Component // as templ generates it
	recorded bool            // a frame of its tree has statics
}

func render(n templ.Component) string {
	var b strings.Builder
	if err := n.Render(context.Background(), &b); err != nil {
		return "error: " + err.Error()
	}
	return b.String()
}

func main() {
	items := []views.Item{{1, "a<", true, []string{"x", "y"}}, {2, "b", false, nil}, {3, "c", true, []string{"z"}}, {4, "skip", false, nil}, {5, "e", false, nil}, {6, "f", true, nil}, {7, "never", false, nil}}
	pitems := make([]plain.Item, len(items))
	for i, it := range items {
		pitems[i] = plain.Item(it)
	}
	attrs := templ.Attributes{"data-a": "1", "hidden": true}
	var cases []tcase
	for _, s := range []string{"", "vuka", "<script>"} {
		cases = append(cases, tcase{"text " + s, views.Text(s, len(s)), plain.Text(s, len(s)), true})
	}
	for _, on := range []bool{true, false} {
		cases = append(cases, tcase{fmt.Sprint("attrs ", on), views.Attrs("/x?a=1&b=\"", on, attrs, "q\"<"), plain.Attrs("/x?a=1&b=\"", on, attrs, "q\"<"), true},
			tcase{fmt.Sprint("attrs js ", on), views.Attrs("javascript:alert(1)", on, nil, ""), plain.Attrs("javascript:alert(1)", on, nil, ""), true})
	}
	for _, n := range []int{0, 2, len(items)} {
		cases = append(cases, tcase{fmt.Sprint("list ", n), views.List(items[:n]), plain.List(pitems[:n]), true})
	}
	for _, n := range []int{0, 7, 11} {
		for _, s := range []string{"a", "b", "c"} {
			cases = append(cases, tcase{fmt.Sprint("branch ", n, s), views.Branch(n, s), plain.Branch(n, s), true})
		}
	}
	cases = append(cases,
		tcase{"page", views.Page("T&", items), plain.Page("T&", pitems), true},
		tcase{"with node", views.WithNode(jsx("j<"), "l"), plain.WithNode(jsx("j<"), "l"), true},
		tcase{"script", views.Script("</script>\"'"), plain.Script("</script>\"'"), true},
		tcase{"go code", views.GoCode([]string{"a", "b"}), plain.GoCode([]string{"a", "b"}), true},
		tcase{"comps", views.Comps(items), plain.Comps(pitems), true},
		tcase{"empty", views.Empty(), plain.Empty(), false},
		tcase{"templ in jsx", templInJSX("t<"), plainInJSX("t<"), true},
		tcase{"children in jsx", ui.WithChildren(views.Card("c"), jsx("in")), ui.WithChildren(plain.Card("c"), jsx("in")), true},
	)
	ctx := context.Background()
	for _, c := range cases {
		want := render(c.plain)
		if got := render(c.live); got != want {
			fmt.Printf("FAIL %s: plain render\n got %q\nwant %q\n", c.name, got, want)
			continue
		}
		tr, err := ui.BuildTree(ctx, c.live, true)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", c.name, err)
			continue
		}
		var b strings.Builder
		tr.HTML(&b, nil)
		if b.String() != want {
			fmt.Printf("FAIL %s: tree html\n got %q\nwant %q\n", c.name, b.String(), want)
			continue
		}
		if again, _ := ui.BuildTree(ctx, c.live, true); again.Key != tr.Key {
			fmt.Printf("FAIL %s: the same render keys its tree differently\n", c.name)
			continue
		}
		if recorded := hasStatics(tr); recorded != c.recorded {
			fmt.Printf("FAIL %s: recorded %v, statics %q\n", c.name, recorded, tr.Statics)
			continue
		}
		fmt.Printf("ok %s\n", c.name)
	}

	// The same layout keys alike whatever it holds.
	a, _ := ui.BuildTree(ctx, views.Page("one", items[:1]), true)
	b, _ := ui.BuildTree(ctx, views.Page("two", items[:3]), true)
	if a.Key != b.Key {
		fmt.Println("FAIL keys: one layout, two keys")
	} else {
		fmt.Println("ok keys")
	}

	// Outside a live render the instrumented code costs about what templ's does.
	if os.Getenv("TREE_BENCH") != "" {
		page := func(c templ.Component) time.Duration {
			start := time.Now()
			for range 20000 {
				c.Render(ctx, discard{})
			}
			return time.Since(start)
		}
		page(views.Page("w", items))
		page(plain.Page("w", pitems))
		lv, pl := page(views.Page("t", items)), page(plain.Page("t", pitems))
		fmt.Printf("bench instrumented %v plain %v (%.2fx)\n", lv/20000, pl/20000, float64(lv)/float64(pl))
	}
}

// hasStatics reports whether a frame of t has statics: markup it split.
func hasStatics(t *ui.Tree) bool {
	for _, s := range t.Statics {
		if s != "" {
			return true
		}
	}
	for _, d := range t.Dyn {
		switch d := d.(type) {
		case *ui.Tree:
			if hasStatics(d) {
				return true
			}
		case *ui.TreeList:
			for _, it := range d.Items {
				if hasStatics(it) {
					return true
				}
			}
		}
	}
	return false
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
`

// TestTemplTreeEquivalence: instrumented templ writes byte for byte what
// templ's own code does, and its render tree's HTML is that too, over a
// broad set of templ; the uninstrumented copy is templ's output beside it.
func TestTemplTreeEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := templModule(t, map[string]string{
		"views/views.templ": treeTempl,
		"main.go":           treeMain,
		"bold.vuka.html":    "package main\n\ntype Bold struct{ Label string }\n<b class=\"j\">{Label}</b>\n",
		"templin.vuka.html": "package main\n\nimport \"example.com/site/views\"\n\ntype TemplIn struct{ Title string }\n<main><views.Card title={Title}><p>{Title}</p></views.Card></main>\n",
		"plainin.vuka.html": "package main\n\nimport \"example.com/site/plain\"\n\ntype PlainIn struct{ Title string }\n<main><plain.Card title={Title}><p>{Title}</p></plain.Card></main>\n",
	})
	if !load.TemplTrees(root) {
		needs(t, "the ui checkout ($VUKA_UI) has no recorder (templx/record.go)")
	}
	g, _, terr := compileTempl("views.templ", "views.templ", []byte(treeTempl), nil)
	if terr != nil {
		t.Fatal(terr)
	}
	writeFile(t, filepath.Join(root, "plain", "views_templ.go"), strings.Replace(string(g), "package views", "package plain", 1))
	pkgs, err := load.Discover(root, "example.com/site", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	gens, overlay, err := load.Transpile(pkgs, t.TempDir(), load.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gens {
		if strings.HasSuffix(g.Source, ".templ") && !strings.Contains(string(g.Src), "templ_vk.Record(") {
			t.Fatalf("%s isn't instrumented", g.Source)
		}
	}
	out := goRun(t, root, overlay, ".")
	if strings.Contains(out, "FAIL") || strings.Count(out, "ok ") != 28 {
		t.Fatalf("\n%s", out)
	}
	if os.Getenv("TREE_BENCH") != "" {
		t.Log(out[strings.Index(out, "bench"):])
	}
}
