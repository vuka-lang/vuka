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

// The 2.2 compiler fixes, as compilerFixes.
var compilerFixes22 = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	// A string naming a package imports nothing; a selector does.
	fmt.Println("import", (&Reminder{}).hint(), strings.Contains(render(Stamp{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}), "2026-01-02"))

	// Several tracked fields, and fields of one, assigned at once.
	s := &Search{}
	s.Find("pets")
	s.Unlimit()
	a := render(s.Render())
	s.Clear()
	s.Retire()
	fmt.Println("multi", strings.Contains(a, "all pets 1 0 -1"), strings.Contains(render(s.Render()), "gone  1 0 -1"))

	// delete, clear and copy change tracked state.
	c := &Cart{}
	c.Add("pen")
	c.Add("ink")
	c.Clean()
	c.Remove("pen")
	d1 := c.Clean()
	c.Empty()
	fmt.Println("builtin", d1, c.Clean(), strings.Contains(render(c.Render()), "0 items"))

	// A component's prop takes onClick's statements as browser commands.
	fmt.Println("click", strings.Count(render(&Panel{}), "data-vk-js"))
}
`,
	"clock.vuka": `package main

import "time"

func now() time.Time { return time.Now() }
`,
	"reminder.vuka.html": `package main

// Reminder names no package: "any time." is text.
type Reminder struct{}

// hint reads like time.Now() in a comment.
func (r *Reminder) hint() string { return "any time." }
<p>{hint()}</p>
`,
	"stamp.vuka.html": `package main

// Stamp reads a sibling's import through a selector.
type Stamp struct {
	At time.Time
}
<p>{At.Format(time.DateOnly)}</p>
`,
	"query.vuka": `package main

// Query is a search's tracked state.
type Query struct {
	Term string
	Page int
}
`,
	"search.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Search assigns several tracked fields at once.
type Search struct {
	ui.Live
	q     Query
	note  string
	tags  []string
	limit int64
}

func (s *Search) Find(term string) { s.q.Term, s.q.Page = term, 1 }

func (s *Search) Clear() { s.note, s.tags = "", nil }

func (s *Search) Unlimit() { s.note, s.limit = "all", -1 }

func (s *Search) Retire() { s.note, s.q.Term = "gone", "" }
<p>{note} {q.Term} {q.Page} {len(tags)} {limit}</p>
`,
	"cart.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Cart changes its tracked map in place.
type Cart struct {
	ui.Live
	items map[string]int
}

func (c *Cart) Add(name string) {
	if c.items == nil {
		c.items = map[string]int{}
	}
	c.items[name]++
}

func (c *Cart) Remove(name string) { delete(c.items, name) }

func (c *Cart) Empty() { clear(c.items) }

// Clean reports whether items changed, and clears the mark.
func (c *Cart) Clean() bool {
	d := c.items.AssignDirty()
	was := *d
	*d = false
	return was
}
<p>{len(items)} items</p>
`,
	"chip.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Chip is a button whose click its user decides.
type Chip struct {
	Label   string
	OnClick ui.JSChain
}
<button onClick={OnClick}>{Label}</button>
`,
	"panel.vuka.html": `package main

import "github.com/vuka-lang/ui/v2"

// Panel opens its settings from a chip.
type Panel struct {
	manage ui.State[bool]
}
<section>
  <Chip label="Manage" onClick={manage = !manage}/>
  <ui.Show when={manage}><p>managing</p></ui.Show>
</section>
`,
}

// TestCompilerFixes22 builds and runs compilerFixes22.
func TestCompilerFixes22(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/fixes")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range compilerFixes22 {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	want := "import any time. true\nmulti true true\nbuiltin true true true\nclick 1\n"
	if got := out.String(); !strings.HasSuffix(got, want) {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}
