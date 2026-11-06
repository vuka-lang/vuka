package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

func compileShapes(files map[string]string) (*transpile.Result, error) {
	var fs []transpile.File
	for name, src := range files {
		fs = append(fs, transpile.File{Name: name, Src: []byte(src)})
	}
	return transpile.Package(fs, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
}

const eventOrders = `<script lang="go">
package main

import (
	"context"

	ui "vukatest.local/opttarget"
)

type Order struct{ ID int }

type Orders struct {
	ui.Live
	orders []Order
	status string
}

func (p *Orders) Choose(s string)                        { p.status = s }
func (p *Orders) Remove(id int) error                    { return nil }
func (p *Orders) Reset(ctx context.Context, n int) error { return nil }
func (p *Orders) Mark(id int, on bool) error              { return nil }
func (p *Orders) Tag(id string) error                    { return nil }
func (p *Orders) Details(id int) (ui.Node, error)        { return nil, nil }
func (p *Orders) Open() ui.Commands                      { return ui.JS }
func (p *Orders) Save(ctx context.Context, s *Store) error { return nil }
func (p *Orders) toggle(i int)                            {}

type Store struct{}

func helper(s string) ui.Commands { return ui.JS }
</script>

<main>
  <select onChange={Choose(this.value)}><option>a</option></select>
  for _, o := range orders {
    <button onClick={Remove(o.ID)}>remove</button>
    <input type="checkbox" onChange={Mark(o.ID, this.checked)} />
    <button data-tag="x" onClick={Tag(this.dataTag)}>tag</button>
    <button onClick={Details(o.ID)}>details</button>
  }
  <button onClick={Reset(ctx, 0)}>reset</button>
  <button onClick={Open()}>open</button>
  <button onClick={Save(ctx, nil)}>save</button>
  <button onClick={toggle(1)}>toggle</button>
  <button onClick={helper("x")}>helper</button>
</main>
`

// TestEventCalls: a call of the receiver's exported method in an on…
// attribute is a push of it, its this reads the element's (vuka#353); a
// method building a handler, an unexported one, a function and a call
// passing its dependencies keep their meaning.
func TestEventCalls(t *testing.T) {
	res, err := compileShapes(map[string]string{"orders.vuka.html": eventOrders})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{
		`ui.JS.Push(p.Choose, ui.This.Value())`,
		`ui.JS.Push(p.Remove, o.ID)`,
		`ui.JS.Push(p.Mark, o.ID, ui.This.Checked())`,
		`ui.JS.Push(p.Tag, ui.This.Attr("data-tag"))`,
		`ui.JS.Push(p.Details, o.ID)`,
		`ui.JS.Push(p.Reset, 0)`,
		`onClick={p.Open()}`,
		`func(ctx context.Context) error { return p.Save(ctx, nil) }`,
		`func() { p.toggle(1) }`,
		`onClick={helper("x")}`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
	if strings.Contains(src, "this.") {
		t.Errorf("an element read left as Go:\n%s", src)
	}
}

// TestEventCallsStateless: on a page that isn't live the pushes go over
// HTTP, so their methods are registered.
func TestEventCallsStateless(t *testing.T) {
	src := strings.Replace(eventOrders, "\tui.Live\n", "", 1)
	src = strings.Replace(src, "  <button onClick={toggle(1)}>toggle</button>\n", "", 1)
	src = strings.Replace(src, "  <button onClick={Save(ctx, nil)}>save</button>\n", "", 1)
	res, err := compileShapes(map[string]string{"orders.vuka.html": src})
	if err != nil {
		t.Fatal(err)
	}
	out := string(res.Files[0].Src)
	for _, want := range []string{"ui.RegisterPush((*Orders).Choose)", "ui.RegisterPush((*Orders).Remove)", "ui.RegisterPush((*Orders).Reset)"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in\n%s", want, out)
		}
	}
}

const eventCombobox = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Combobox struct {
	Name  string
	Attrs []ui.Attr
}
</script>

<select name={Name} {Attrs...}></select>
`

const eventChip = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Chip struct {
	Label   string
	OnClick ui.Commands
}
</script>

<button onClick={OnClick}>{Label}</button>
`

const eventBadge = `<script lang="go">
package main

type Badge struct{ Label string }
</script>

<span>{Label}</span>
`

// TestEventCallsOnComponents: an on… attribute a component's tag passes on
// to the element it spreads its attributes on, or gives a prop taking
// commands, is a push too.
func TestEventCallsOnComponents(t *testing.T) {
	page := `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Index struct {
	ui.Live
	status string
}

func (p *Index) Choose(s string)   { p.status = s }
func (p *Index) Select(id int) error { return nil }
</script>

<main>
  <Combobox name="status" onChange={Choose(this.value)} />
  <Chip label="a" onClick={Select(1)} />
</main>
`
	res, err := compileShapes(map[string]string{"index.vuka.html": page, "combobox.vuka.html": eventCombobox, "chip.vuka.html": eventChip})
	if err != nil {
		t.Fatal(err)
	}
	var src string
	for _, o := range res.Files {
		if o.Name == "index_html.go" || strings.Contains(string(o.Src), "func (p *Index) Render") {
			src = string(o.Src)
		}
	}
	for _, want := range []string{
		`ui.Attr{Name: "onChange", Value: ui.JS.Push(p.Choose, ui.This.Value())}`,
		`OnClick: ui.JS.Push(p.Select, 1)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
}

// TestEventCallErrors: an element read outside a push, a misspelt read, an
// unknown method, a call that can't fill the method's parameters and a
// component with nowhere to put an event.
func TestEventCallErrors(t *testing.T) {
	for _, c := range []struct{ name, old, new, want string }{
		{"a read outside a push", `onClick={helper("x")}`, `onClick={helper(this.value)}`,
			"E0170 orders.vuka.html:45:27: this.value reads the element onClick is on, in the browser: it is an argument of a call to one of Orders's exported methods, which the event pushes"},
		{"a misspelt read", `Choose(this.value)`, `Choose(this.valeu)`,
			"E0170 orders.vuka.html:34:33: this.valeu reads the attribute valeu; did you mean this.value?"},
		{"an unknown method", `Choose(this.value)`, `Chose(this.value)`,
			"E0172 orders.vuka.html:34:21: Chose isn't a method of Orders: an event calls one of its exported methods; did you mean Choose?"},
		{"too many arguments", `Remove(o.ID)`, `Remove(o.ID, this.value)`,
			"E0168"},
		{"a read of the wrong type", `Details(o.ID)`, `Details(this.checked)`,
			"E0168"},
		{"dependencies on a page that isn't live", "\tui.Live\n", "",
			"E0169 orders.vuka.html:42:11: Orders needs a live connection because onClick={…} calls Save with its dependencies"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Replace(eventOrders, c.old, c.new, 1)
			if src == eventOrders {
				t.Fatalf("no %q", c.old)
			}
			_, err := compileShapes(map[string]string{"orders.vuka.html": src})
			if err == nil {
				t.Fatal("no error")
			}
			if got := codes(err); !strings.Contains(got, c.want) {
				t.Errorf("no %q in\n%s", c.want, got)
			}
		})
	}
	t.Run("a component spreading no attributes", func(t *testing.T) {
		page := `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Index struct{ ui.Live }

func (p *Index) Choose(s string) {}
</script>

<Badge label="x" onChange={Choose(this.value)} />
`
		_, err := compileShapes(map[string]string{"index.vuka.html": page, "badge.vuka.html": eventBadge})
		want := "<Badge> takes no onChange: it has no OnChange field, and no Attrs []ui.Attr field passing it on to the element it spreads its attributes on (<input {Attrs...}/>)"
		if err == nil || !strings.Contains(codes(err), want) {
			t.Errorf("no %q in %v", want, err)
		}
	})
}

// codes is an error list as code and message per line.
func codes(err error) string {
	el, ok := err.(transpile.ErrorList)
	if !ok {
		return err.Error()
	}
	var b strings.Builder
	for _, e := range el {
		b.WriteString(string(e.Code) + " " + e.Error() + "\n")
	}
	return b.String()
}
