# Client state

Some state belongs to the browser: whether a menu is open, which tab is
picked, what a list is filtered by. `ui.State` declares it; commands change
it at once, with no round trip; and the parts of the page that read it
follow — attributes, text, classes — written as plain Go expressions.

```vuka
func Menu() ui.Node {
	open := ui.State(false)
	return <div>
		<button aria-expanded={open} onClick={open.Toggle()}>Menu</button>
		<ul hidden={!open}>…</ul>
	</div>
}
```

The server renders the page with each state's initial value, so the HTML is
right before any script runs; the browser then keeps the values. No
JavaScript is written, and none is evaluated: reads and writes travel as
JSON in `data-` attributes the [runtime](/web/live) runs, as
[JS commands](/ui/js) do.

## Declaring

`ui.State(v)` declares a state of `v`'s type, `ui.State[T]`. Where it is
declared decides whose it is:

- **In a function** — a component, a `Render` method, a `Mount` — it is the
  rendering component instance's. A component rendered ten times has ten
  states, and two components naming a variable `open` never share one: the
  compiler gives each declaration a place of its own.
- **At package level** it is the page's: one value every component reads.

```vuka
var theme = ui.State("light") // the page's

func Tabs() ui.Node {
	tab := ui.State("env") // each Tabs' own
	…
}
```

## Writing

Writes are commands, chained with the rest of [`ui.JS`](/ui/js):

| | |
|---|---|
| `s.Set(v)` | set it to `v` |
| `s.Toggle()` | flip a `ui.State[bool]` |
| `s.Inc()`, `s.Dec()` | add or subtract one, on a number |
| `s.Reset()` | back to its initial value |
| `chain.Set(s, v)` | set another state in the same chain |

They are type-checked: `Toggle` on a `ui.State[string]`, or `Set(tab, 3)`,
is a compile error.

```vuka
<button onClick={tab.Set("env").Set(open, false).Push(c.Opened)}>Environment</button>
```

A chain's writes happen in the order written, before its other steps —
before its `Push` — and the page updates once for them all: a binding that
reads two of the states changes once, with no flicker between. Writes are
not predictions: a failing `Push` doesn't take them back.

## Reading

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

```vuka
<button aria-selected={tab == "env"} disabled={q == "" || tab != "logs"}>Env</button>
<span>{count}</span> of <span>{len(items)}</span>
<li class={ui.Class{"bg-brand": tab == "env", "opacity-50": !ok}}>…</li>
```

`aria-*` attributes render a bool as `"true"` or `"false"`; any other
attribute is there when true and absent when false. `value` and `checked`
set the field too.

Each expression reads a fixed set of states, which the compiler records; the
browser evaluates it again when one of them changes, and only then.

### Derived

`ui.Derived(expr)` names an expression for reuse, read wherever a state is:

```vuka
ready := ui.Derived(!open && tab == "env")
<button disabled={!ready}>Go</button>
```

## Struct state

A struct state's fields are states of their own:

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

What reads `f.Kind` updates when `f.Kind` changes, not when `f.Sort` does;
`f.Set` replaces every field in one batch.

## Passing to a child

A state passed as a prop is the same state, not a copy: the child reads and
writes the parent's. `x.Get()` passes the value it renders with instead.

```vuka
<Count kind={f.Kind} />

func Count(kind ui.State[string]) ui.Node {
	return <p hidden={kind == ""}>only {kind}s</p>
}
```

An expression of states passed to a `ui.State` prop is passed as a derived
state; one passed to a prop of another type is an error (pass `.Get()`).

## Showing and hiding

`<ui.Show when={expr}>` renders its children always and shows them only
while `expr` holds, as Vue's `v-show` does. Transitions are the children's
own CSS: while hidden they have `data-closed`, while appearing
`data-enter`, while disappearing `data-leave`, and they are hidden once
their transitions or animations end. With Tailwind's data variants:

```vuka
<ui.Show when={open}>
	<div class="transition duration-150 data-closed:opacity-0 data-closed:scale-95 data-leave:duration-100">
		…
	</div>
</ui.Show>
```

The children are inside a `<vk-show>` element (`display: contents`). Users
asking for reduced motion get no wait. A child with the `popover` attribute
is shown with `showPopover()` instead, in the browser's top layer, with the
same phases:

```vuka
tip := ui.State(false)
<button onClick={tip.Toggle()}>?</button>
<ui.Show when={tip}>
	<div popover="manual" class="transition data-closed:opacity-0">A tip</div>
</ui.Show>
```

## With a live session

On a [live](/ui/live) page the server renders the page again on every
event, each time with the states' initial values; the browser keeps its
own, so a menu the visitor opened stays open. The server knows only the
initial value (`Get`); it sets a state by pushing the command:

```vuka
func (b *Board) Mount()     { b.tab = ui.State("a") }
func (b *Board) ShowLogs()  { b.PushJS(b.tab.Set("logs")) }
```

## Without the compiler

Go code declares a state with `ui.NewState(site, v)` (`ui.PageState` for the
page's), `site` naming the declaration uniquely, and writes reads as
`ui.Bind(value, expr, deps…)` — what the compiler writes; see
`go doc github.com/vuka-lang/ui Bind` for the expression's JSON.
