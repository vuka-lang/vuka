# Client state

Some state belongs to the browser: whether a menu is open, which tab is
picked, what a list is filtered by. A `ui.State` field holds it; a write
changes it at once, with no round trip; and the parts of the page that read
it follow — attributes, text, classes — written as plain Go expressions.

```html
<!-- menu.vuka.html -->
package app

type Menu struct {
	Label string
	open  ui.State[bool]
}
<div>
  <button aria-expanded={open} onClick={open = !open}>{Label}</button>
  <ul hidden={!open}>
    <li>Settings</li>
  </ul>
</div>
```

The server renders the page with each state's initial value, so the HTML is
right before any script runs; the browser then keeps the values. No
JavaScript is written, and none is evaluated: reads and writes travel as
JSON in `data-` attributes the [runtime](/web/live) runs, as
[JS commands](/ui/js) do.

## Declaring

A state is declared by its type, and starts at its type's zero value. Where
it is declared decides whose it is:

- **A field of the struct** is the rendering component instance's. A
  component rendered ten times has ten states, and two components naming a
  field `open` never share one.
- **A package-level variable** is the page's: one value every component
  reads.

```html
<!-- tabs.vuka.html -->
package app

var theme ui.State[string] // the page's

type Tabs struct {
	tab ui.State[string] // each Tabs' own
}
<nav data-tab={tab} data-theme={theme}></nav>
```

A state that starts elsewhere than its zero value is written in a live
component's `@ui.Mount`, or by the first click that needs it.

## Writing

A state is written by assignment in an element's `on…` attribute. The
statements are a chain of [JS commands](/ui/js) the browser runs at once:

| | |
|---|---|
| `tab = "env"` | set it to a value of the server's: a constant, a prop, a loop variable |
| `open = !open` | flip a `ui.State[bool]` |
| `n++`, `n--` | add or subtract one, on a number |
| `f.Kind = "dog"` | set one field of a struct state |
| `Save()`, `Remove(t.ID)` | call a handler on the server, after the writes before it |

```html
<button onClick={tab = "env"; open = false; Opened()}>Environment</button>
```

They run in the order written; the page updates once for the writes — a
binding that reads two of the states changes once, with no flicker between
— and then the handler is called, as a `Push` is. Writes are not
predictions: a failing call doesn't take them back.

Anything else is [E0150](/errors/E0150): another operator (`n += 2`), a
value that reads a state (`tab = tab + "!"`), a field that isn't a state.
The types are checked: `open = !open` on a `ui.State[string]`, or `tab = 3`,
is a compile error.

A state written anywhere else — while the markup renders, in a plain
function, in a static component's method — has no page to go to, and is
[E0149](/errors/E0149). A live component's method writes them as an `on…`
attribute does, and the command is pushed to the page with the reply:

```html
<!-- board.vuka.html -->
package app

type Board struct {
	ui.Live
	tab ui.State[string]
}

func (b *Board) ShowLogs() { b.tab = "logs" }
<button onClick={ShowLogs}>{tab}</button>
```

## Reading

An attribute, a child or a `ui.Class` value that reads a state is kept up
to date by the browser. Anything a Go expression of the state's type can
be, among these, works:

- literals, constants, and states (a struct state's fields too);
- `!`, unary `-`, `&&`, `||`, `==`, `!=`, `<`, `<=`, `>`, `>=`;
- `+` (numbers, and strings joined), `-`, `len(…)`;
- `ui.Class{"name": expr, …}` for classes switched on and off.

Values of the server's — a prop, a loop variable, `x.Get()` — may sit beside
the states: they are sent with the expression. Anything else mixed with a
state is a compile error saying what can't run in the browser:
`strings.ToUpper can't run in the browser; compute it on the server`.

```html
<button aria-selected={tab == "env"} disabled={q == "" || tab != "logs"}>Env</button>
<span>{count}</span> of <span>{len(items)}</span>
<li class={ui.Class{"bg-brand": tab == "env", "opacity-50": !ok}}>Env</li>
```

`aria-*` attributes render a bool as `"true"` or `"false"`; any other
attribute is there when true and absent when false. `value` and `checked`
set the field too.

Each expression reads a fixed set of states, which the compiler records;
the browser evaluates it again when one of them changes, and only then.

## Struct state

A struct state's fields are states of their own:

```html
<!-- pets.vuka.html -->
package app

type Pets struct {
	Pets []Pet
	f    ui.State[Filters] // Filters{Kind, Sort string}
}
<button onClick={f.Kind = "dog"}>Dogs</button>
<button onClick={f = Filters{Kind: "cat", Sort: "age"}}>Cats by age</button>
<button onClick={f = Filters{}}>All</button>
<ul>
  for _, p := range Pets {
    <li hidden={f.Kind != "" && f.Kind != p.Kind}>{p.Name}</li>
  }
</ul>
```

What reads `f.Kind` updates when it changes, not when `f.Sort` does; a
write of `f` replaces every field in one batch.

## Passing to a child

A state passed as a prop is the same state, not a copy: the child reads and
writes the parent's. `x.Get()` passes the value it renders with instead.

```html
<Count kind={f.Kind} />
```

```html
<!-- count.vuka.html -->
package app

type Count struct {
	Kind ui.State[string]
}
<p hidden={Kind == ""}>only {Kind}s</p>
```

An expression of states passed to a `ui.State` prop is passed as a derived
state, and to a prop of type `any` as a binding; one passed to a prop of
another type is an error (pass `.Get()`).

A component taking `[]ui.Attr` gets a bound attribute from `ui.BindAttr`:
rendered with the state's value, kept up to date by the browser, `aria-*`
bools as `"true"`/`"false"`:

```html
<components.Button attrs={[]ui.Attr{ui.BindAttr("aria-expanded", open)}}>Save</components.Button>
```

## Showing and hiding

`<ui.Show when={expr}>` renders its children always and shows them only
while `expr` holds, as Vue's `v-show` does. Transitions are the children's
own CSS: while hidden they have `data-closed`, while appearing
`data-enter`, while disappearing `data-leave`, and they are hidden once
their transitions or animations end. With Tailwind's data variants:

```html
<ui.Show when={open}>
	<div class="transition duration-150 data-closed:opacity-0 data-closed:scale-95 data-leave:duration-100">
		<p>Menu</p>
	</div>
</ui.Show>
```

The children are inside a `<vk-show>` element (`display: contents`). Children
with no transition or animation are hidden at once, in the same update, so
two `<ui.Show>`s swapped by one state never show together; users asking for
reduced motion get no wait. A child with the `popover` attribute is shown
with `showPopover()` instead, in the browser's top layer, with the same
phases:

```html
<button onClick={tip = !tip}>?</button>
<ui.Show when={tip}>
	<div popover="manual" class="transition data-closed:opacity-0">A tip</div>
</ui.Show>
```

An `auto` popover is one the browser closes too — on Escape, or a click
outside it. The state follows: `when={tip}` is set to false, `when={!hidden}`
to true, so the next click on the trigger opens it again, and a click on the
trigger while it is open closes it. A `when` that is a derived expression
can't be written back; give such a popover `popover="manual"`.

## With a live session

On a [live](/ui/live) page the server renders the page again on every
event, each time with the states' initial values; the browser keeps its
own, so a menu the visitor opened stays open. The server knows only the
initial value (`Get`); a method that writes a state pushes the command
(above), which runs once the reply is applied.

## What it becomes

The compiler writes a `.vuka.html` file's states with package
`github.com/vuka-lang/ui/uigen`: public, so the generated Go builds with
`go build`, but not an API to write by hand — [`vukavet`](/tools/vukavet)
reports a hand-written use ([W0105](/errors/W0105)). `ui.State`'s own API is
`Get` and `Set`.

| In the file | Generated |
|---|---|
| `var theme ui.State[string]` | `var theme ui.State[string] = uigen.UsePageState(*new(string))` |
| a field `open ui.State[bool]` | a state of its own before the component mounts, or (a static one) before its `View` renders: `uigen.States(c)` |
| `hidden={!open}` | `hidden={uigen.Not(open)}` |
| `tab == "env" && !open` | `uigen.And(uigen.Eq(tab, "env"), uigen.Not(open))` |
| `"Hello, " + name` | `uigen.Plus(uigen.Derived("Hello, "), name)` |
| `f.Q` | `uigen.At[string](f, "Q")` |
| `onClick={open = !open}` | `onClick={uigen.Toggle(ui.JS, open)}` |
| `onClick={tab = "env"; open = false}` | `onClick={ui.JS.Set(tab, "env").Set(open, false)}` |
| `onClick={open = false; Save()}` | `onClick={ui.JS.Set(open, false).Push(c.Save)}` |
| `onClick={n++}` | `onClick={uigen.Inc(ui.JS, n)}` |
| `b.tab = "logs"` in a live method | `b.PushJS(b.tab.Set("logs"))` |
