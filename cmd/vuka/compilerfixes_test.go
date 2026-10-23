package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The 2.1 compiler fixes, each a shape of a small program built and run:
// what each prints is what the fix makes it do.
var compilerFixes = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vuka-lang/ui/v2"
)

func render(n ui.Node) string {
	s, err := ui.String(context.Background(), n)
	if err != nil {
		panic(err)
	}
	return s
}

func main() {
	ctx := context.Background()

	// p.rows = load()? sets tracked state only when the call succeeds.
	o := &Orders{}
	fmt.Println("load", o.Load(ctx, true), o.Load(ctx, false), strings.Contains(render(o), "2 orders"))

	// A swap of two tracked fields.
	p := &Pair{}
	p.Set("rex", "tom")
	p.Swap()
	fmt.Println("swap", strings.Contains(render(p.Render()), "tom rex"))

	// A stateful shape's value in a hole renders, not prints.
	b := render(Board{})
	fmt.Println("hole", strings.Count(b, "class=\"card\""), strings.Contains(b, "{a ") || strings.Contains(b, "{b "))

	// A component attribute reading browser state stays the browser's.
	fmt.Println("show", strings.Count(render(&Tabs{}), "data-vk-show"))

	// Markup statements' variables in slots.
	fmt.Println("slot", strings.Contains(render(List{Pets: []string{"rex", "tom"}}), "<h2>2 pets</h2>"))

	// A field named Result, an attribute naming an initialism's field.
	fmt.Println("names", strings.Contains(render(Check{Result: "ok"}), "ok ok!"), strings.Contains(render(Tags{}), "tag 7"))
}
`,
	"orders.vuka.html": `package main

import (
	"context"
	"errors"
)

// Orders lists orders.
type Orders struct {
	rows []string
}

func load(ok bool) ([]string, error) {
	if !ok {
		return nil, errors.New("down")
	}
	return []string{"a", "b"}, nil
}

func (p *Orders) Load(ctx context.Context, ok bool) error {
	p.rows = load(ok)?
	return nil
}
<p>{len(rows)} orders</p>
`,
	"pair.vuka.html": `package main

// Pair swaps two pets.
type Pair struct {
	a, b string
}

func (p *Pair) Set(a, b string) { p.a, p.b = a, b }

func (p *Pair) Swap() { p.a, p.b = p.b, p.a }
<p onClick={Swap}>{a} {b}</p>
`,
	"card.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Card is stateful: its open state is the browser's.
type Card struct {
	Title    string
	Actions  ui.Node
	Children ui.Node
	open     ui.State[bool]
}
<div class="card" onClick={open = !open}><h2>{Title}</h2>{Actions}{Children}</div>
`,
	"board.vuka.html": `package main

// Board places cards as values.
type Board struct{}
<section>
  {Card{Title: "a"}}
  c := Card{Title: "b"}
  {c}
</section>
`,
	"tabs.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Tabs shows a pane while its tab is chosen.
type Tabs struct {
	tab  ui.State[string]
	open ui.State[bool]
}
<section>
  <button onClick={tab = "b"}>b</button>
  <ui.Show when={tab == "b"}><p>pane b</p></ui.Show>
  <ui.Show when={!open && tab != "a"}><p>neither</p></ui.Show>
</section>
`,
	"list.vuka.html": `package main

// List counts its pets in a card's slot.
type List struct {
	Pets []string
}
<section>
  n := len(Pets)
  <Card>
    label := "pets"
    <slot:actions>
      if n > 1 {
        <b>{n} {label}</b>
      }
    </slot:actions>
    <h2>{n} {label}</h2>
  </Card>
</section>
`,
	"check.vuka.html": `package main

// Check shows a result.
type Check struct {
	Result string
}

func (c Check) Label() string { return c.Result + "!" }
<p>{Result} {Label()}</p>
`,
	"tag.vuka.html": `package main

// Tag is one tag by its ID.
type Tag struct {
	ID int
}
<i>tag {ID}</i>
`,
	"tags.vuka.html": `package main

// Tags names a tag by its initialism field in lowerCamel.
type Tags struct{}
<p><Tag id={7}/></p>
`,
}

// TestCompilerFixes builds and runs compilerFixes.
func TestCompilerFixes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/fixes")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range compilerFixes {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	want := "load <nil> down true\nswap true\nhole 2 false\nshow 2\nslot true\nnames true true\n"
	if got := out.String(); !strings.HasSuffix(got, want) {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}
