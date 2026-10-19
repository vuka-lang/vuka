# Client state

Some state belongs to the browser: whether a menu is open, which tab is
picked, what a list is filtered by. `ui.State` declares it; commands change
it at once, with no round trip; and the parts of the page that read it
follow — attributes, text, classes — written as plain Go expressions<span class="go-only">,
or in Go code as a state's methods (`open.Not()`)</span>.

<!-- go:context
type Pet struct{ Name, Kind string }

type Page struct {
	ui.Live
	pets []Pet
}

func (p *Page) Opened() {}

var (
	c    = &Page{}
	open = ui.UsePageState(false)
	tab  = ui.UsePageState("env")
	f    = ui.UsePageState(struct{ Q string }{})
)

// Button is a component of the app's own.
type Button struct {
	OnClick  any
	Attrs    []ui.Attr
	Children ui.Node
}

func (b Button) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<button onClick={b.OnClick} {b.Attrs...}>{b.Children}</button>`, b.OnClick, b.Attrs, b.Children).Render(ctx, w)
}

func OpenDialog(open ui.State[bool]) ui.Commands { return open.Set(true) }
-->

::: vuka
```vuka
func Menu() ui.Node {
	open := ui.State(false)
	return <div>
		<button aria-expanded={open} onClick={open.Toggle()}>Menu</button>
		<ul hidden={!open}>…</ul>
	</div>
}
```
:::

::: go
```go
func Menu() ui.Node {
	open := ui.UseState(false)
	toggle, hidden := open.Toggle(), open.Not()
	return ui.HTML(`
<div>
	<button aria-expanded={open} onClick={toggle}>Menu</button>
	<ul hidden={hidden}><li>Settings</li></ul>
</div>`, open, toggle, hidden)
}
```
:::

The server renders the page with each state's initial value, so the HTML is
right before any script runs; the browser then keeps the values. No
JavaScript is written, and none is evaluated: reads and writes travel as
JSON in `data-` attributes the [runtime](/web/live) runs, as
[JS commands](/ui/js) do.

## Declaring

<span class="vuka-only">`ui.State(v)`</span><span class="go-only">`ui.UseState(v)`</span> declares a state of `v`'s type, `ui.State[T]`. Where it is
declared decides whose it is:

- **In a function** — a component, a `Render` method, a `Mount` — it is the
  rendering component instance's. A component rendered ten times has ten
  states, and two components naming a variable `open` never share one: the
  compiler gives each declaration a place of its own.
- **At package level** it is the page's: one value every component reads<span class="go-only">
  (`ui.UsePageState(v)` in Go)</span>.

::: vuka
```vuka
var theme = ui.State("light") // the page's

func Tabs() ui.Node {
	tab := ui.State("env") // each Tabs' own
	…
}
```
:::

::: go
```go
var theme = ui.UsePageState("light") // the page's

func Tabs() ui.Node {
	tab := ui.UseState("env") // each Tabs' own
	return ui.HTML(`<nav data-tab={tab} data-theme={theme}></nav>`, tab, theme)
}
```
:::

## Writing

Writes are commands, chained with the rest of [`ui.JS`](/ui/js):

| | |
|---|---|
| `s.Set(v)` | set it to `v` |
| `s.Toggle()` | flip a `ui.State[bool]` |
| `s.Inc()`, `s.Dec()` | add or subtract one, on a number |
| `s.Reset()` | back to its initial value |
| `chain.Set(s, v)` | set another state in the same chain |

They are type-checked: <span class="vuka-only">`Toggle` on a `ui.State[string]`, or `Set(tab, 3)`,
is a compile error</span><span class="go-only">`tab.Set(3)` on a `ui.State[string]` doesn't compile</span>.

::: vuka
```vuka
<button onClick={tab.Set("env").Set(open, false).Push(c.Opened)}>Environment</button>
```
:::

::: go
```go
func envButton(tab ui.State[string], open ui.State[bool]) ui.Node {
	pick := tab.Set("env").Set(open, false).Push(c.Opened)
	return ui.HTML(`<button onClick={pick}>Environment</button>`, pick)
}
```
:::

A chain's writes happen in the order written, before its other steps —
before its `Push` — and the page updates once for them all: a binding that
reads two of the states changes once, with no flicker between. Writes are
not predictions: a failing `Push` doesn't take them back.

## Reading

::: vuka
A JSX attribute, child or `ui.Class` value that reads a state is kept up to
date by the browser. Anything a Go expression of the state's type can be,
among these, works:

- literals, constants, and states (a struct state's fields too);
- `!`, unary `-`, `&&`, `||`, `==`, `!=`, `<`, `<=`, `>`, `>=`;
- `+` (numbers, and strings joined), `-`, `len(…)`;
- `ui.Class{"name": expr, …}` for classes switched on and off.

Values of the server's — a prop, a loop variable, `x.Get()` — may sit beside
the states: they are sent with the expression. Anything else mixed with a
state is a compile error saying what can't run in the browser:
`strings.ToUpper can't run in the browser; compute it on the server`.
:::

::: go
A hole of `ui.HTML` given a state — or an expression of states, built with
its methods — is kept up to date by the browser:

| `.vuka` | Go |
|---|---|
| `!open` | `open.Not()` |
| `a && b`, `a \|\| b` | `a.And(b)`, `a.Or(b)` |
| `==`, `!=`, `<`, `<=`, `>`, `>=` | `Eq`, `Ne`, `Lt`, `Le`, `Gt`, `Ge` |
| `+`, `-`, unary `-` | `Plus`, `Minus`, `Neg` |
| `len(s)` | `s.Len()` |
| `"Hello, " + name` | `ui.Derived("Hello, ").Plus(name)` |
| `f.Q` (a struct state's field) | `ui.At[string](f, "Q")` |
| `ui.Class{"x": cond}` | `ui.Classes{"x": state}` |

An operand is a value or a state; a value of the server's (a prop, a loop
variable, `x.Get()`) is sent with the expression.
:::

::: vuka
```vuka
<button aria-selected={tab == "env"} disabled={q == "" || tab != "logs"}>Env</button>
<span>{count}</span> of <span>{len(items)}</span>
<li class={ui.Class{"bg-brand": tab == "env", "opacity-50": !ok}}>…</li>
```
:::

::: go
```go
func envTab(tab, q ui.State[string], ok ui.State[bool], count ui.State[int], items []string) ui.Node {
	selected := tab.Eq("env")
	disabled := q.Eq("").Or(tab.Ne("logs"))
	cls := ui.Classes{"bg-brand": selected, "opacity-50": ok.Not()}
	return ui.HTML(`
<button aria-selected={selected} disabled={disabled}>Env</button>
<span>{count}</span> of <span>{len(items)}</span>
<li class={cls}>Env</li>`, selected, disabled, count, len(items), cls)
}
```
:::

`aria-*` attributes render a bool as `"true"` or `"false"`; any other
attribute is there when true and absent when false. `value` and `checked`
set the field too.

Each expression reads a fixed set of states, which the <span class="vuka-only">compiler records</span><span class="go-only">methods record</span>; the
browser evaluates it again when one of them changes, and only then.

### Writes and values built from states

Not every expression mentioning a state reads it. A command (`open.Toggle()`,
`OpenDialog(s)`), a node (`p.menu(upload)`), an attribute list — any call or
composite literal whose value isn't a scalar — hands the states to Go code
as they are, in a component's prop as anywhere else<span class="go-only">
(in Go, a state goes wherever its type does)</span>:

::: vuka
```vuka
<components.Button onClick={open.Toggle()}>Menu</components.Button>
<components.Button onClick={components.OpenDialog(upload)}>Upload</components.Button>
{p.menu(upload)}

func (p *Page) menu(open ui.State[bool]) ui.Node {
	return <ul hidden={!open}>…</ul>
}
```
:::

::: go
```go
func (p *Page) toolbar(open, upload ui.State[bool]) ui.Node {
	toggle, show := open.Toggle(), OpenDialog(upload)
	menu := p.menu(upload)
	return ui.HTML(`
<Button onClick={toggle}>Menu</Button>
<Button onClick={show}>Upload</Button>
{menu}`, toggle, show, menu)
}

func (p *Page) menu(open ui.State[bool]) ui.Node {
	hidden := open.Not()
	return ui.HTML(`<ul hidden={hidden}><li>New</li></ul>`, hidden)
}
```
:::

Only an expression whose value is computed from states — a bool, a number,
a string — is a read the browser evaluates.

### Derived

<span class="vuka-only">`ui.Derived(expr)` names an expression for reuse, read wherever a state is:</span><span class="go-only">An expression of states is a state itself, a variable reused wherever a state is read:</span>

::: vuka
```vuka
ready := ui.Derived(!open && tab == "env")
<button disabled={!ready}>Go</button>
```
:::

::: go
<!-- go:fragment -->
```go
ready := open.Not().And(tab.Eq("env"))
notReady := ready.Not()
button := ui.HTML(`<button disabled={notReady}>Go</button>`, notReady)
```
:::

## Struct state

A struct state's fields are states of their own:

::: vuka
```vuka
type Filters struct{ Kind, Sort string }

f := ui.State(Filters{Sort: "name"})
<button onClick={f.Kind.Set("dog")}>Dogs</button>
<button onClick={f.Set(Filters{Kind: "cat", Sort: "age"})}>Cats by age</button>
<button onClick={f.Reset()}>All</button>
{for _, p := range pets {
	<li hidden={f.Kind != "" && f.Kind != p.Kind}>{p.Name}</li>
}}
```
:::

::: go
```go
type Filters struct{ Kind, Sort string }

func filters(pets []Pet) ui.Node {
	f := ui.UseState(Filters{Sort: "name"})
	kind := ui.At[string](f, "Kind")
	dogs, cats, all := kind.Set("dog"), f.Set(Filters{Kind: "cat", Sort: "age"}), f.Reset()
	rows := ui.Each(pets, func(p Pet) ui.Node {
		hidden := kind.Ne("").And(kind.Ne(p.Kind))
		return ui.HTML(`<li hidden={hidden}>{p.Name}</li>`, hidden, p.Name)
	})
	return ui.HTML(`
<button onClick={dogs}>Dogs</button>
<button onClick={cats}>Cats by age</button>
<button onClick={all}>All</button>
<ul>{rows}</ul>`, dogs, cats, all, rows)
}
```
:::

What reads <span class="vuka-only">`f.Kind`</span><span class="go-only">`ui.At[string](f, "Kind")`</span> updates when it changes, not when `f.Sort` does;
`f.Set` replaces every field in one batch.

## Passing to a child

A state passed as a prop is the same state, not a copy: the child reads and
writes the parent's. `x.Get()` passes the value it renders with instead.

::: vuka
```vuka
<Count kind={f.Kind} />

func Count(kind ui.State[string]) ui.Node {
	return <p hidden={kind == ""}>only {kind}s</p>
}
```
:::

::: go
```go
func Count(kind ui.State[string]) ui.Node {
	hidden := kind.Eq("")
	return ui.HTML(`<p hidden={hidden}>only {kind}s</p>`, hidden, kind)
}

func countDogs(f ui.State[Filters]) ui.Node { return Count(ui.At[string](f, "Kind")) }
```
:::

<span class="vuka-only">An expression of states passed to a `ui.State` prop is passed as a derived
state, and to a prop of type `any` as a binding; one passed to a prop of
another type is an error (pass `.Get()`).</span>

### In an attribute list

A component taking `[]ui.Attr` gets a bound attribute from `ui.BindAttr`:
rendered with the state's value, kept up to date by the browser, `aria-*`
bools as `"true"`/`"false"` as in JSX. An expression goes through
<span class="vuka-only">`ui.Derived`</span><span class="go-only">its state's methods</span>:

::: vuka
```vuka
<components.Button attrs={[]ui.Attr{
	ui.BindAttr("aria-expanded", open),
	ui.BindAttr("data-busy", ui.Derived(saving && !failed)),
}}>Save</components.Button>
```
:::

::: go
```go
func saveButton(open, saving, failed ui.State[bool]) ui.Node {
	return Button{Attrs: []ui.Attr{
		ui.BindAttr("aria-expanded", open),
		ui.BindAttr("data-busy", saving.And(failed.Not())),
	}, Children: ui.Text("Save")}
}
```
:::

## Showing and hiding

`<ui.Show when={expr}>`<span class="go-only"> (`ui.Show(ui.ShowProps{When: s, Children: n})` in Go)</span> renders its children always and shows them only
while `expr` holds, as Vue's `v-show` does. Transitions are the children's
own CSS: while hidden they have `data-closed`, while appearing
`data-enter`, while disappearing `data-leave`, and they are hidden once
their transitions or animations end. With Tailwind's data variants:

::: vuka
```vuka
<ui.Show when={open}>
	<div class="transition duration-150 data-closed:opacity-0 data-closed:scale-95 data-leave:duration-100">
		…
	</div>
</ui.Show>
```
:::

::: go
```html
<ui.Show when={open}>
	<div class="transition duration-150 data-closed:opacity-0 data-closed:scale-95 data-leave:duration-100">
		<p>Menu</p>
	</div>
</ui.Show>
```
:::

The children are inside a `<vk-show>` element (`display: contents`). Children
with no transition or animation are hidden at once, in the same update, so
two `<ui.Show>`s swapped by one state never show together; users asking for
reduced motion get no wait. A child with the `popover` attribute is shown
with `showPopover()` instead, in the browser's top layer, with the same
phases:

::: vuka
```vuka
tip := ui.State(false)
<button onClick={tip.Toggle()}>?</button>
<ui.Show when={tip}>
	<div popover="manual" class="transition data-closed:opacity-0">A tip</div>
</ui.Show>
```
:::

::: go
```go
func help() ui.Node {
	tip := ui.UseState(false)
	toggle := tip.Toggle()
	box := ui.Show(ui.ShowProps{When: tip, Children: ui.HTML(`<div popover="manual" class="transition data-closed:opacity-0">A tip</div>`)})
	return ui.HTML(`<button onClick={toggle}>?</button>{box}`, toggle, box)
}
```
:::

An `auto` popover is one the browser closes too — on Escape, or a click
outside it. The state follows: `when={tip}` is set to false, `when={!hidden}`
to true, so the next click on the trigger opens it again, and a click on the
trigger while it is open closes it. A `when` that is a derived expression
can't be written back; give such a popover `popover="manual"`.

## With a live session

On a [live](/ui/live) page the server renders the page again on every
event, each time with the states' initial values; the browser keeps its
own, so a menu the visitor opened stays open. The server knows only the
initial value (`Get`); it sets a state by pushing the command:

::: vuka
```vuka
func (b *Board) Mount()     { b.tab = ui.State("a") }
func (b *Board) ShowLogs()  { b.PushJS(b.tab.Set("logs")) }
```
:::

::: go
```go
type Board struct {
	ui.Live
	tab ui.State[string]
}

func (b *Board) LiveSchema(l *ui.LiveSchema) { l.Mount(b.mount) }

func (b *Board) mount(ctx context.Context) error { b.tab = ui.UseState("a"); return nil }

func (b *Board) ShowLogs() { b.PushJS(b.tab.Set("logs")) }
```
:::

## Without the compiler

<span class="vuka-only">Go code writes what the compiler does:</span><span class="go-only">In short:</span> `ui.UseState(v)` declares a state
(`ui.UsePageState` the page's) where it is called, a hole of `ui.HTML` takes
a state as it is, and an expression of states is State's methods, each a
derived state the browser evaluates again:

<!-- go:fragment -->
```go
open, tab := ui.UseState(false), ui.UseState("logs")
ready := open.Not().And(tab.Eq("env"))     // !open && tab == "env"
q := ui.At[string](f, "Q")                 // f.Q
toggle := open.Toggle()
nav := ui.HTML(`<nav class:is-open={open}><button onClick={toggle} disabled={ready}>Menu</button></nav>`, open, toggle, ready)
```
