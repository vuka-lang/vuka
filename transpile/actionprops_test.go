package transpile_test

import (
	"strings"
	"testing"
)

const actionButton = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Order struct {
	ID   int
	Name string
}

type Button struct {
	Label string
	On    ui.Action ` + "`vuka:\"required\"`" + `
}
</script>

<button onClick={On}>{Label}</button>
`

const actionTable = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Table struct {
	Orders   []Order
	OnSelect ui.ActionOf[Order]
	OnRename ui.ActionOf[string]
	OnOpen   ui.ActionOf[int]
}
</script>

<table>
  for _, o := range Orders {
    <tr onClick={OnSelect(o)}>
      <td><input value={o.Name} onChange={OnRename(this.value)} /></td>
      <td onClick={OnOpen(o.ID)}>open</td>
      <td onDblclick={OnSelect}>{o.Name}</td>
    </tr>
  }
</table>
`

const actionPage = `<script lang="go">
package main

import (
	"context"

	ui "vukatest.local/opttarget"
)

type Orders struct {
	ui.Live
	orders []Order
}

func (p *Orders) Save(ctx context.Context) error   { return nil }
func (p *Orders) Remove(id int) error              { return nil }
func (p *Orders) Select(ctx context.Context, o Order) error { return nil }
func (p *Orders) Rename(name string)               {}
func (p *Orders) Open(id int) error                { return nil }
func (p *Orders) Count(n int) string               { return "" }
</script>

<main>
  @@
</main>
`

func compileActions(t *testing.T, markup string, files ...string) (string, error) {
	t.Helper()
	srcs := map[string]string{
		"orders.vuka.html": strings.Replace(actionPage, "@@", markup, 1),
		"button.vuka.html": actionButton,
		"table.vuka.html":  actionTable,
	}
	for i := 0; i+1 < len(files); i += 2 {
		srcs[files[i]] = files[i+1]
	}
	res, err := compileShapes(srcs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, o := range res.Files {
		b.Write(o.Src)
	}
	return b.String(), nil
}

// TestActionProps: an attribute setting a ui.Action prop takes what an
// on… attribute takes, whatever its spelling: a call of the page's method
// is a push, modifiers ride along, a method is the handler; an
// ActionOf[T] takes the method itself, and the component's call of it in
// an on… attribute pushes it with the value.
func TestActionProps(t *testing.T) {
	src, err := compileActions(t, `<Button label="remove" on={Remove(7)} />
  <Button label="save" on={Save} />
  <Button label="stop" on.stop={Remove(8)} />
  <Button label="closure" on={func() {}} />
  <Table orders={orders} onSelect={Select} onRename={Rename} onOpen={func(id int) error { return nil }} />`)
	if err != nil {
		t.Fatal(codes(err))
	}
	for _, want := range []string{
		`On: ui.JS.Push(p.Remove, 7)`,
		`On: p.Save`,
		`On: ui.JS.Stop().Then(ui.JS.Push(p.Remove, 8))`,
		`On: func() {}`,
		`OnSelect: p.Select, OnRename: p.Rename`,
		`<tr onClick={ui.JS.Push(t.OnSelect, o)}>`,
		`onChange={ui.JS.Push(t.OnRename, ui.This.Value())}`,
		`<td onClick={ui.JS.Push(t.OnOpen, o.ID)}>`,
		`<td onDblclick={t.OnSelect}>`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
	if strings.Contains(src, "p.Remove(7)") {
		t.Errorf("Remove called while rendering:\n%s", src)
	}
}

// TestActionPropsStateless: on a page that isn't live, a method given an
// Action prop is registered: the component's element pushes it over HTTP.
func TestActionPropsStateless(t *testing.T) {
	page := strings.Replace(actionPage, "\tui.Live\n", "", 1)
	page = strings.Replace(page, "@@", `<Button label="save" on={Save} />
  <Button label="remove" on={Remove(1)} />`, 1)
	res, err := compileShapes(map[string]string{"orders.vuka.html": page, "button.vuka.html": actionButton, "table.vuka.html": actionTable})
	if err != nil {
		t.Fatal(codes(err))
	}
	var src string
	for _, o := range res.Files {
		src += string(o.Src)
	}
	for _, want := range []string{"ui.RegisterPush((*Orders).Save)", "ui.RegisterPush((*Orders).Remove)"} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
}

// TestActionPropsRequired: vuka:"required" on an Action prop is met by
// any form the sugar takes, and a tag leaving it out is E0158.
func TestActionPropsRequired(t *testing.T) {
	if _, err := compileActions(t, `<Button label="x" on={Remove(1)} />`); err != nil {
		t.Fatal(codes(err))
	}
	_, err := compileActions(t, `<Button label="x" />`)
	if err == nil || !strings.Contains(codes(err), "E0158") {
		t.Errorf("no E0158 in %v", err)
	}
}

// TestActionStatements: an Action prop takes an on… attribute's
// statements, as browser commands, whatever the attribute's spelling.
func TestActionStatements(t *testing.T) {
	toggle := `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Toggle struct {
	On ui.Action
}
</script>

<button onClick={On}>toggle</button>
`
	page := `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Panel struct {
	ui.Live
	open ui.State[bool]
}
</script>

<div>
  <Toggle on={open = !open} />
  <p show={open}>hi</p>
</div>
`
	res, err := compileShapes(map[string]string{"panel.vuka.html": page, "toggle.vuka.html": toggle})
	if err != nil {
		t.Fatal(codes(err))
	}
	var src string
	for _, o := range res.Files {
		src += string(o.Src)
	}
	if !strings.Contains(src, "Toggle{On: ") || strings.Contains(src, "Toggle{On: func()") {
		t.Errorf("the statements aren't browser commands:\n%s", src)
	}

	picker := strings.Replace(toggle, "On ui.Action", "OnPick ui.ActionOf[string]", 1)
	picker = strings.Replace(picker, "onClick={On}", "onClick={OnPick}", 1)
	page = strings.Replace(page, "<Toggle on={open = !open} />", "<Toggle onPick={open = !open} />", 1)
	_, err = compileShapes(map[string]string{"panel.vuka.html": page, "toggle.vuka.html": picker})
	want := "E0193 panel.vuka.html:13:11: <Toggle>'s OnPick is ui.ActionOf[string], a handler taking a string: the statements of onPick={…} are browser commands, which it can't hold"
	if err == nil || !strings.Contains(codes(err), want) {
		t.Errorf("no %q in %v", want, err)
	}
}

// TestActionPropErrors: a func prop given a call of a method, or a func
// field called in an event (E0192); a value an ActionOf[T] can't hold
// (E0193); an action called where no event fires, or given a value that
// doesn't fit (E0194).
func TestActionPropErrors(t *testing.T) {
	runner := `<script lang="go">
package main

import "context"

type Runner struct {
	Name  string
	OnRun func(ctx context.Context, transition string) error
}
</script>

<button onClick={OnRun}>{Name}</button>
`
	caller := `<script lang="go">
package main

import (
	"context"

	ui "vukatest.local/opttarget"
)

var _ ui.Action

type Caller struct {
	Name  string
	OnRun func(ctx context.Context, transition string) error
}
</script>

<button onClick={OnRun(ctx, Name)}>{Name}</button>
`
	for _, c := range []struct {
		name, markup string
		files        []string
		want         string
	}{
		{"a func prop given a call", `<Runner name="a" onRun={Remove(1)} />`, []string{"runner.vuka.html", runner},
			"E0192 orders.vuka.html:24:27: <Runner>'s OnRun is a func, func(ctx context.Context, transition string) error: Remove(1) calls Remove while the page renders, not when the event fires; declare the prop as ui.ActionOf[string], which takes the call as a push, or pass the method itself: onRun={Remove}"},
		{"a func field called in an event", `<Caller name="a" onRun={Rename} />`, []string{"caller.vuka.html", caller},
			"E0192 caller.vuka.html:18:18: OnRun is a func, func(ctx context.Context, transition string) error: onClick={OnRun(…)} calls it while the markup renders, not when the event fires; declare OnRun as ui.ActionOf[string]"},
		{"a call given an ActionOf", `<Table orders={orders} onSelect={Remove(1)} />`, nil,
			"E0193 orders.vuka.html:24:36: <Table>'s OnSelect is ui.ActionOf[Order]: Remove(1) calls Remove while the page renders, and OnSelect takes the handler, which the component gives an Order; pass a method taking an Order, not a call of one: onSelect={Remove}"},
		{"a method of the wrong payload", `<Table orders={orders} onSelect={Open} />`, nil,
			"E0193 orders.vuka.html:24:36: <Table>'s OnSelect is ui.ActionOf[Order], and Open takes an int, not the Order the component gives; pass a method taking an Order, onSelect={Choose}, or a closure, onSelect={func(v Order) { … }}"},
		{"a method of two values", `<Table orders={orders} onOpen={Count} onRename={func(a, b string) {}} />`, nil,
			"E0193 orders.vuka.html:24:51: <Table>'s OnRename is ui.ActionOf[string], and func(a, b string) {} takes 2 values, and the component gives one, a string"},
		{"text given an ActionOf", `<Table orders={orders} onRename="Rename" />`, nil,
			"E0193 orders.vuka.html:24:26: <Table>'s OnRename is ui.ActionOf[string], a handler taking a string, and \"Rename\" is text"},
		{"a value given an ActionOf", `<Table orders={orders} onOpen={7} />`, nil,
			"E0193 orders.vuka.html:24:34: <Table>'s OnOpen is ui.ActionOf[int], a handler taking an int, and 7 is int"},
		{"an action called outside an event", `<Table orders={orders} />`, []string{"table.vuka.html", strings.Replace(actionTable, `<td onClick={OnOpen(o.ID)}>open</td>`, `<td>{OnOpen(o.ID)}</td>`, 1)},
			"E0194 table.vuka.html:18:12: OnOpen(…) calls OnOpen where no event fires: OnOpen is ui.ActionOf[int], which runs when an event fires; call it in an on… attribute with the int it takes, onClick={OnOpen(…)}, or forward it: onClick={OnOpen}"},
		{"an action given a value that doesn't fit", `<Table orders={orders} />`, []string{"table.vuka.html", strings.Replace(actionTable, `OnOpen(o.ID)`, `OnOpen(o.Name)`, 1)},
			"E0194 table.vuka.html:18:27: o.Name is string, and OnOpen takes an int: it is ui.ActionOf[int]"},
		{"an action given two values", `<Table orders={orders} />`, []string{"table.vuka.html", strings.Replace(actionTable, `OnOpen(o.ID)`, `OnOpen(o.ID, 2)`, 1)},
			"E0194 table.vuka.html:18:20: OnOpen is ui.ActionOf[int], which takes one value, and onClick={OnOpen(…)} passes 2"},
		{"an Action called", `<Button label="x" on={Save} />`, []string{"button.vuka.html", strings.Replace(actionButton, `onClick={On}`, `onClick={On()}`, 1)},
			"E0194 button.vuka.html:17:18: onClick={On()} calls On, which is ui.Action: it is what the event does, and is given as it is; forward it to an element's on… attribute: onClick={On}"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := compileActions(t, c.markup, c.files...)
			if err == nil {
				t.Fatal("no error")
			}
			if got := codes(err); !strings.Contains(got, c.want) {
				t.Errorf("no %q in\n%s", c.want, got)
			}
		})
	}
}
