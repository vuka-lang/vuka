# Client State

[[toc]]

## Introduction

A `ui.State[T]` field holds browser state. You reach for it when a value only
matters in the browser, such as an open panel or a selected tab, because
browser changes need no round trip to the server.

You read it as an ordinary variable in markup and write it by assignment in
an `on…` attribute.

## Declaring Browser State

This component declares three state fields and reads and writes them from its
buttons:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Tabs struct {
    open ui.State[bool]
    tab ui.State[string]
    count ui.State[int]
}
</script>

<section>
  <button onClick={open = !open}>Toggle</button>
  <button onClick={tab = "details"; open = true}>Details</button>
  <button onClick={count++}>Count: {count}</button>
  <div hidden={!open} class:selected={tab == "details"}>Details</div>
</section>
```

The zero value is the initial value: false, empty string or zero.

### Shared State

State belongs to each component instance. A package-level
`var theme ui.State[string]` is shared across that browser page. Put such
declarations in a logic file.

## Reading State {#reading}

Use state in children, attributes, conditions and expressions. A browser-state
expression remains reactive in the browser.

It cannot select a Go branch that requires a server execution. Use a reactive
attribute or `ui.Show` for visibility instead.

The browser runs literals, state reads, `!`, unary `-`, `&&`, `||`,
comparisons, `+`, `-`, `*` and `len`; any other operator or a call is
[E0124](/errors/E0124). A value read in several places is a
[Derived](#derived).

## Derived Values {#derived}

A `ui.Derived[T]` field is a value the browser computes from states and
keeps: a cart's total from its count and price, whether it is over a limit.
Its `@ui.From(…)` is the expression, written as markup writes one, the
struct's fields by their bare names:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Cart struct {
	count ui.State[int]
	price ui.State[int]
	total ui.Derived[int]  @ui.From(count * price)
	over  ui.Derived[bool] @ui.From(total > 100)
}
</script>

<section>
  <button onClick={count++}>Add one</button>
  <p class:over={over}>{total}</p>
</section>
```

Markup reads a Derived exactly as it reads a state, in children,
attributes, `class:x` and `ui.Show`. The browser computes it again when a
state it reads changes, once per batch of writes however many places read
it, and what reads it follows only when its value changed. A Derived may
read another one; the compiler orders them, and Derived fields reading each
other in a cycle are [E0176](/errors/E0176), naming the path.

The server renders it with the same expression over the states' initial
values, so the HTML is right before any script runs.

- The expression reads states, other Derived fields and constants. A prop
  or server state isn't the browser's: compare with it in the markup
  (`class:over={total > Limit}`).
- It is never written: an assignment to one is [E0177](/errors/E0177).
  Write the states it reads.
- A component script reads one with `state('total')`, whose handle has
  `get` and `watch` and no `set`.

## Writing State {#writing}

Assignments in one event run in order. You can combine browser changes with a
server call: `onClick={open = false; Save()}`, a [push](/ui/js#push) that runs
on the live instance, or over HTTP on a page that isn't live. Inside a live
component method, `p.open = false` sends the browser update.

> [!WARNING]
> Browser state is not durable storage or trusted authorization data.

## Keeping State {#keep}

Browser state ends with the tab. `@ui.Keep("key")` keeps a state in the
browser's localStorage instead: the value stored wins over the rendered
one when the page loads, and every write is stored. `@ui.Shared` sends a
state's writes to the page's other tabs, which apply them as their own
writes, so their watches run:

```html
<script lang="go">
package layouts

import "github.com/vuka-lang/ui/v2"

type Shell struct {
	theme   ui.State[string] @ui.Keep("theme")
	sidebar ui.State[bool]   @ui.Keep("sidebar") @ui.Shared
}
</script>

<div data-theme={theme}>
  <button onClick={sidebar = !sidebar}>Menu</button>
  <aside hidden={!sidebar}>…</aside>
</div>
```

- The value is stored as JSON, under the key prefixed `vuka:`, one per
  origin: each key names one field ([E0179](/errors/E0179)).
- A private window or storage the browser refuses leaves it an ordinary
  state, and a browser without `BroadcastChannel` keeps its writes to its
  own tab.
- The server renders the state's initial value; the browser shows the kept
  one as the page starts. A layout that must render with the visitor's
  choice keeps it in a cookie or the server's session instead.

> [!WARNING]
> A kept value is the visitor's data, written in their browser: never
> trust it on the server.

## Watching State {#watching}

Something can run when a state changes: a script's code, or a method of a
live component.

### In a Script {#watch-in-a-script}

A [component script](/ui/scripts)'s `state('tab').watch((now, was) => …)`
runs after each write the page shows — the bindings reading the state are
already updated — with the new value and the one before. It runs on a write
in an `on…` attribute, a script's `set`, and a write the server pushes; a
render or a patch of the page is no write. It returns a function that stops
it.

```ts
const stop = state('tab').watch((now, was) => {
  ref('panel').scrollTo({ top: 0 })
})
```

A watch may write states; their watches run after it. Watches that keep
writing each other's states are cut after ten rounds, with a console
warning naming the states.

### On the Server {#notify}

On a live component, `@ui.Notify(OnTab)` on a state's field calls the
method `OnTab` with each value the browser writes, on the event path a
click takes. `Debounce` and `Throttle` pace the calls:

```html
<script lang="go">
package pages

import (
	"time"

	"github.com/vuka-lang/ui/v2"
)

type Index struct {
	ui.Live
	tab  ui.State[string] @ui.Notify(OnTab)
	page ui.State[int]    @ui.Notify(OnPage).Debounce(300 * time.Millisecond)
	seen string
}

func (p *Index) OnTab(tab string) { p.seen = tab }

func (p *Index) OnPage(n int) {}
</script>

<nav>
  <button onClick={tab = "all"}>All</button>
  <button onClick={page++}>Next</button>
</nav>
```

The method takes the state's value, after an optional `context.Context`,
and may return an error. A write the server makes (`p.tab = "home"` in a
method) calls nothing: the server knows it. A misplaced or mistyped
`@ui.Notify` is [E0178](/errors/E0178).

## Passing State to a Child {#passing-to-a-child}

A child can accept a `ui.State[T]` field so both shapes observe the same
browser value. Ordinary exported fields pass props instead. For state and
function props crossing a frontend boundary, see [Islands](/web/islands).

## Handing a Click to a Child {#click-to-a-child}

An element's `onClick={open = !open}` is a chain of browser commands, a
`ui.JSChain`. A component's prop of that type takes the same statements, so
a page can decide what its child's button does and the child forwards it to
its element:

```html
<script lang="go">
// chip.vuka.html
package components

import "github.com/vuka-lang/ui/v2"

type Chip struct {
	Label   string
	OnClick ui.JSChain
}
</script>

<button class="chip" onClick={OnClick}>{Label}</button>
```

```html
<script lang="go">
// panel.vuka.html
package components

import "github.com/vuka-lang/ui/v2"

type Panel struct {
	manage ui.State[bool]
}
</script>

<section>
  <Chip label="Manage" onClick={manage = !manage}/>
  <ui.Show when={manage}><p>Managing</p></ui.Show>
</section>
```

A prop of type `any` takes them too.

> [!NOTE]
> A prop of a func type runs Go on the server and can't hold browser
> commands: statements there are [E0101](/errors/E0101). Declare it
> `ui.JSChain` to take them.
