package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// mpRow counts its renders and calls a func prop only from its button.
const mpRow = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Row struct {
	ui.Live
	ID       int
	OnCancel func()
	Other    int
}
</script>
<button onClick={OnCancel}>{ID}{countRow()}</button>
`

// mpShow calls its func prop to render.
const mpShow = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Show struct {
	ui.Live
	Label func() string
}
</script>
<b>{Label()}</b>
`

// mpBox is generic, as its parent's tag is.
const mpBox = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Box[T any] struct {
	ui.Live
	Value T
	OnPick func(T)
}
</script>
<i>{Value}{countBox()}</i>
`

const mpTypes = `package main

import "github.com/vuka-lang/ui/v2"

type Order struct {
	ID   int
	Name string
}

func (o Order) Cancel() { hits = append(hits, "value "+o.Name) }
func (o *Order) Drop()  { hits = append(hits, "ptr "+o.Name) }

type Base struct{ name string }

func (b *Base) Hit() { hits = append(hits, "base "+b.name) }

type Doer interface{ Do() }

type doer struct{ name string }

func (d *doer) Do() { hits = append(hits, "doer "+d.name) }

var (
	hits       []string
	rowRenders int
	boxRenders int
)

func countRow() string { rowRenders++; return "" }
func countBox() string { boxRenders++; return "" }

// A child written in plain Go: it reads the prop while it renders.
type Plain struct {
	ui.Live
	Label func() string
}

func (p *Plain) Render() ui.Node { return ui.Text("[" + p.Label() + "]") }
`

// mpPage passes its children every shape of func prop.
const mpPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Page struct {
	ui.Live
	*Base
	Worker Doer
	Orders []Order
	Which  string
	n      int
}

func (p *Page) Bump()          { p.n++ }
func (p *Page) Cancel()        { hits = append(hits, "page") }
func (p *Page) Text() string   { return "n=" + itoa(p.n) }
func (p *Page) Pick(n int)     {}
</script>
<div>
  <button onClick={Bump}>{n}</button>
  if Which == "method" {
    <Row onCancel={Cancel}/>
    <Row onCancel={p.Cancel}/>
    <Row onCancel={Cancel} key={"after"}/>
    <Row onCancel={Hit}/>
    <Row onCancel={Worker.Do}/>
    <Box value={1} onPick={Pick}/>
  }
  if Which == "value" {
    for _, o := range Orders {
      <Row key={o.ID} onCancel={o.Cancel}/>
    }
  }
  if Which == "ptrloop" {
    for _, o := range Orders {
      <Row key={o.ID} onCancel={o.Drop}/>
    }
  }
  if Which == "ptrindex" {
    for i := range Orders {
      <Row key={Orders[i].ID} onCancel={Orders[i].Drop}/>
    }
  }
  if Which == "closure" {
    <Row onCancel={func() { p.Cancel() }}/>
  }
  if Which == "render" {
    <Show label={Text}/>
    <Plain label={Text}/>
  }
</div>
`

const mpMain = `package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
)

func itoa(n int) string { return strconv.Itoa(n) }

func page(which string, p *Page) *live.Session {
	p.Which = which
	if p.Base == nil {
		p.Base = &Base{"a"}
		p.Worker = &doer{"a"}
		p.Orders = []Order{{1, "one"}, {2, "two"}}
	}
	return live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("p", nil, p) })
}

func event(s *live.Session, target string) {
	if _, err := s.Event(ui.Event{Target: target, Type: "click"}); err != nil {
		panic(err)
	}
}

func main() {
	for _, verify := range []bool{false, true} {
		live.Verify = verify
		fmt.Println("verify", verify)
		for _, which := range []string{"method", "value", "ptrloop", "ptrindex", "closure"} {
			s := page(which, &Page{})
			if _, err := s.RenderTrees(); err != nil {
				panic(err)
			}
			rowRenders, boxRenders = 0, 0
			event(s, "c1:0")
			fmt.Println(which, "rows", rowRenders, "boxes", boxRenders)
		}
		s := page("render", &Page{})
		if _, err := s.RenderTrees(); err != nil {
			panic(err)
		}
		for range 2 {
			event(s, "c1:0")
			h := s.HTML()
			fmt.Println("render", strings.Count(h, "n=2") + strings.Count(h, "n=1"), strings.Contains(h, "[n="))
		}
	}

	// The receiver really changes: the child's button calls the new one.
	live.Verify = false
	hits = nil
	p := &Page{}
	s := page("method", p)
	if _, err := s.RenderTrees(); err != nil {
		panic(err)
	}
	rowRenders = 0
	p.Base = &Base{"b"}
	p.Worker = &doer{"b"}
	event(s, "c1:0")
	fmt.Println("after the swap, rows", rowRenders)
	for _, id := range []string{"c5:0", "c6:0", "c2:0"} {
		event(s, id)
	}
	fmt.Println(hits)
}
`

const mpWant = `verify false
method rows 0 boxes 0
value rows 0 boxes 0
ptrloop rows 2 boxes 0
ptrindex rows 0 boxes 0
closure rows 1 boxes 0
render 2 true
render 2 true
verify true
method rows 5 boxes 1
value rows 2 boxes 0
ptrloop rows 2 boxes 0
ptrindex rows 2 boxes 0
closure rows 1 boxes 0
render 2 true
render 2 true
after the swap, rows 2
[base b doer b page]
`

// TestMethodPropsE2E compiles tags passing method values and runs them under
// a session: what the compiler records skips a child only when the method and
// its real receiver are the same, and a child's render calling one still shows
// what it says now.
func TestMethodPropsE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if b, err := os.ReadFile(filepath.Join(ui, "methodprops.go")); err != nil || !strings.Contains(string(b), "func MethodProps") {
		needs(t, "the ui checkout (%s) has no MethodProps", ui)
	}
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n\tgithub.com/vuka-lang/vuka/v2 " + transpile.RuntimeVersion +
			"\n)\n\nreplace github.com/vuka-lang/ui/v2 => " + ui + "\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"go.sum":         string(uiSum) + string(ownSum),
		"main.go":        mpMain,
		"types.go":       mpTypes,
		"row.vuka.html":  mpRow,
		"show.vuka.html": mpShow,
		"box.vuka.html":  mpBox,
		"page.vuka.html": mpPage,
	})
	cmd := exec.Command(buildVuka(t), "run", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "VUKA_TAILWIND=off", "VUKA_FRONTEND=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(out) != mpWant {
		t.Errorf("got:\n%s\nwant:\n%s", out, mpWant)
	}
}

// TestMethodPropsHelpers: a tag records a func prop's method and receiver for
// a method of a helper declared in the page's own script, as for the page's.
func TestMethodPropsHelpers(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if b, err := os.ReadFile(filepath.Join(ui, "methodprops.go")); err != nil || !strings.Contains(string(b), "func MethodProps") {
		needs(t, "the ui checkout (%s) has no MethodProps", ui)
	}
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n\tgithub.com/vuka-lang/vuka/v2 " + transpile.RuntimeVersion +
			"\n)\n\nreplace github.com/vuka-lang/ui/v2 => " + ui + "\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"go.sum":  string(uiSum) + string(ownSum),
		"main.go": "package main\n\nfunc main() {}\n",
		"row.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Row struct {
	ui.Live
	OnCancel func()
}
</script>
<button onClick={OnCancel}>x</button>
`,
		"orders.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Line struct{ n int }

func (l *Line) Close() { l.n++ }

type Orders struct {
	ui.Live
	line Line
}

func (p *Orders) Cancel() {}
</script>
<div>
  <Row onCancel={Cancel}/>
  <Row onCancel={line.Close}/>
</div>
`,
	})
	out := filepath.Join(t.TempDir(), "gen")
	cmd := exec.Command(buildVuka(t), "gen", "-o", out)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "VUKA_TAILWIND=off", "VUKA_FRONTEND=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	src, err := os.ReadFile(filepath.Join(out, "orders_html.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ui.Method("OnCancel", p, "Cancel")`, `ui.Method("OnCancel", &p.line, "Close")`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("no %s in:\n%s", want, src)
		}
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = out
	build.Env = cmd.Env
	if b, err := build.CombinedOutput(); err != nil {
		t.Errorf("the generated Go doesn't build: %v\n%s", err, b)
	}
}
