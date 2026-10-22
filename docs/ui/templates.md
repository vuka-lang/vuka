# Templates

A `.vuka.html` shape file contains a Go header followed by markup. The header
has the package, imports, exactly one rendering struct, decorated hooks and
methods. The first line whose first non-space character is `<` starts markup.
Logic without a view belongs in `.go` or `.vuka` files. Errors point to the
shape file's own line, not the generated Go.

## A component

Save as `components/sales.vuka.html`:

```html
package components

import "github.com/vuka-lang/ui/v2"

type Sales struct {
    Title string
    Week []int
    Children ui.Node
}

func (s Sales) Total() int {
    n := 0
    for _, amount := range s.Week { n += amount }
    return n
}
<section class="sales">
  <h2>{Title}</h2>
  for _, amount := range Week {
    <span class="day" class:big={amount > 10}>{amount}</span>
  }
  <p>Total: {Total()}</p>
  {Children}
</section>
```

The struct is the component and its fields are props. Use its tag from another
shape, or its generated node value from Go logic. The same markup participates
in static rendering and live render-tree diffing.

### Scope and receiver

Fields and methods are available by bare name: `{Title}`, `{Total()}`. A field
or method wins over a package name in a value expression; locals and loop
variables shadow fields. A tag such as `<Icon/>` resolves the package's
component type. The receiver named by the header's methods is available too,
so `{s.Title}` makes the choice explicit.

For a field whose name is also a type (`Glyph Glyph`), `{Glyph.Name}` and
`f(Glyph)` read the field, while `Glyph(x)`, `Glyph{Name: "x"}`, `[]Glyph`
and `new(Glyph)` name the type. [W0101](/errors/W0101) flags an ambiguous
bare name; use the receiver to disambiguate a value. The generated receiver
is a pointer for a live shape, browser-state fields or pointer-receiver
methods, otherwise a value.

## The struct in the file

Exported fields are props, unexported fields are tracked server state or
provided dependencies, and ui.State[T] fields are browser state. A shape is
live if it embeds ui.Live, declares a decorated hook or wires a server method
to an event. A command-only event remains browser-side and does not make the
shape live. The compiler supplies the appropriate rendering interface.

```html
package components

import (
    "context"
    "github.com/vuka-lang/ui/v2"
)

type Counter struct {
    Start int
    n int
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Increment() { c.n++ }
<button onClick={Increment}>{n}</button>
```

No struct is [E0146](/errors/E0146), multiple structs [E0147](/errors/E0147),
and a header without markup [E0148](/errors/E0148). A form DTO, record type
or other non-render data structure goes in an adjacent logic file. Each
reusable rendering partial becomes its own shape file.

## The file

The package clause matches the directory (external test shapes may use the
`orders_test` package). Single and grouped imports precede the one struct,
hooks and ordinary methods. Do not put an HTML filename comment before the
Go header: it would be the first line starting with `<`.

The struct name need not match the filename. A routed page's filename supplies
its URL; it is not inferred from the struct name. Package-level declarations
in logic files are available in markup; keep the imports for a complete
example in its own header. A missing or mismatched package is
[E0142](/errors/E0142).

`vuka fmt` and editor format-on-save format the header and keep the markup
readable. Document an exported component on its struct. One file renders one
shape; there is no second template body after a later function header.

## Holes

Holes accept Go expressions; their required type depends on their place:

| | |
|---|---|
| `<td>{o.Ref}</td>` | a child |
| `title={o.Ref}`, `title="{o.Ref}"` | an attribute's value |
| `href="/orders/{o.ID}"` | part of a value |
| `class:late={o.Late}` | a toggled class: a bool or a `ui.State[bool]` |
| `<input {attrs...}>` | an attribute set |
| `onClick={c.Inc}`, `onClick={c.Add(2)}` | an event: a method value, or a call with no result, which runs on the event |
| `bind:value={Q}` | two-way: the field's value, and what is typed sets it — a field marked `@ui.Bind` on a live page (`@ui.Bind` on the field, its debounce and `validate:` rules), else an error |
| `style:--accent={Color}` | a CSS variable (any property) beside the element's `style`, sanitized as a `ui.Style` |

<span v-pre>`{{`</span> is a literal brace and `{/* … */}` a comment.

## Statements

A Go statement at the start of a line, with markup for its body:

The following is a fragment inside a shape's markup root:

```html
<section>
  if len(orders) == 0 {
    <p>No orders yet.</p>
  } else {
    <ul>
      for _, order := range orders {
        <li key={order.ID}>{order.Ref}</li>
      }
    </ul>
  }
  n := len(orders)
  switch n {
  case 1:
    <p>one order</p>
  default:
    <p>{n} orders</p>
  }
</section>
```

`if`, `else if`, `else`, `for` (over a range or a condition), `switch`
with `case` and `default`, `x := expr` and `x = expr`. Text that starts with a
keyword is written as a string, `{"if you can"}`.

They run in the markup's order, as Go's: a hole shows the value at its
place, not the value after the statements below it:

```html
<section>
  x := 1
  <p>{x}</p>   <!-- 1 -->
  x = 2
  <p>{x}</p>   <!-- 2 -->
</section>
```

## Components and slots

A capitalized tag names a component in the current package; a dotted tag,
`<components.Pill>` or `<ui.Card>`, names one from an import. Lowercase HTML,
SVG and MathML names remain elements, including custom-element names with a
hyphen. Component names and attributes are case-sensitive; an unknown tag is
[E0143](/errors/E0143), with the nearest matching name when possible.

A struct component's attributes set exported fields. CamelCase mapping turns
`noPad` into NoPad and `onSelect` into OnSelect. A bare bool attribute is true;
quoted text is a string and `{expression}` is typed. HTML elements retain
HTML names (`class`, `for`, `data-*`, `aria-*`). Unknown component attributes
are errors rather than arbitrary DOM attributes. Function props let a parent
supply a callback. A typed node helper in a logic file can still be called from
a hole; reusable application markup belongs in its own shape file.

Content fills Children ui.Node. A `<slot:actions>` directly inside the
component tag fills Actions ui.Node. A slot in any other position is an error.
In `components/admin.vuka.html`:

```html
package components

import "github.com/vuka-lang/ui/v2"

type Admin struct {
    Title string
    Actions ui.Node
    Children ui.Node
}
<div class="admin">
  <header><h1>{Title}</h1>{Actions}</header>
  <main>{Children}</main>
</div>
```

The caller, `pages/orders.vuka.html`:

```html
package pages

import "example.com/app/components"

type Orders struct { Count int }
<components.Admin title="Orders">
  <slot:actions><button>New</button></slot:actions>
  <p>{Count} orders</p>
</components.Admin>
```

Go visibility applies: an unexported component is local to its package; an
exported one can be used through an import. Keep stable `key` values on
repeated live component tags to preserve logical instances across reorderings.

An imported frontend component is an island tag, with props read from its file:

```html
package pages

import Clock "../frontend/clock.tsx"

type WorldClock struct { Zone string }
<main><Clock zone={Zone} client:visible /></main>
```

See [Islands](/web/islands) for supported frameworks, prop inference,
optional values, fallback and hydration strategies.

## Scripts and styles

A `<script lang="ts">` after a template's markup is its component's
script, and a `<style scoped>` its CSS — one of each per shape:

```html
package components

import "example.com/app/models"

type Chart struct {
    Title string
    Points []models.Point
}
<figure class="chart">
  <figcaption>{Title}</figcaption>
</figure>

<script lang="ts">
import { component } from 'vuka:component'

export default component<Chart>((el, props, ctx) => ({
  update(next) { el.dataset.n = String(next.Points.length) },
  destroy() {},
}))
</script>

<style scoped>
figcaption { font-weight: bold }
</style>
```

The script runs once per instance: `el` is its root, `props` the struct's
data fields as JSON (`ui.Node`, func and unexported fields are not sent),
`update` runs when a live re-render changes them, `destroy` when the
instance leaves the page. `ctx.live` calls the component's exported
methods (`ctx.live.reorder(ids)` is `Reorder`), `ctx.state("open")` is a
`ui.State` field's handle — `get()`, `set(v)`, `watch(fn)` — and
`ctx.navigate(href)` / `ctx.patch(href)` move the page. A live page's
script can use `page<Orders>((el, live) => …)`: `live.q` is the
field `Q`'s handle (`get()`, `set(v)` for a field marked `@ui.Bind`), `live.reorder`
a method.

The types come from the Go struct: `component<Chart>` knows `props.Title`,
`ctx.live`'s methods and `ctx.state`'s names. The `vuka` command writes
them to `.vuka/vuka-component.d.ts`, and `vuka lsp` gives each script to a
TypeScript language server with them — completion, hover, definition and
errors in the `.vuka.html` — when `typescript-language-server` is on PATH
or in `node_modules/.bin` (`$VUKA_TSLS` names one). The `vuka` command bundles the script and scopes the style to
the template's own elements (`:global(…)` escapes). A style has no holes.

`<script is:inline>` is written as it is, not bundled; its one hole is
`{json(x)}`, the value as script-safe JSON:

```html
<script is:inline>window.points = {json(Points)};</script>
```

## Building

`vuka run`, `vuka build` and `vuka test` compile `.vuka.html` through an
overlay without writing among your sources. `vuka gen` writes each shape's
generated Go (`x_html.go`) for ordinary Go builds. A package may mix logic
`.go` and `.vuka` files with shape files in the same scope. Errors and editor
navigation map back to source. The generator also emits page routes, typed
links, scoped CSS and component-script types when the project uses them.

The emitted render methods and node-construction calls are compiler output.
Keep the shape file as the rendering source instead of editing generated Go.

## Render context

A template can read `ctx`, the `context.Context` of its current render,
without storing it on the page or passing it through every component:

```html
package dashboard

import "github.com/vuka-lang/web/v2/auth"

type Analytics struct{}
<section>
  me := auth.Current(ctx)
  if me != nil && me.Can("view_analytics") {
    <p>Analytics</p>
  }
</section>
```

This works throughout a shape's markup. Existing explicit context parameters
in Go methods keep their meaning. Context-sensitive markup runs at render
time with the HTTP request or live session's current context.
The context is not a component prop and is not sent to the browser.

Keep rendering free of writes and expensive database work: fetch data in
the mount hook, loaders or handlers. Context-sensitive live output is rendered
again even when its props and tracked fields have not changed. This avoids
reusing output after a context-dependent permission or locale changes;
it does not refresh an application's authentication store by itself.

Templates are trusted Go application code, not a sandbox for user-supplied
markup. Never print tokens or other secret context values, put context in
browser props, or share personalized output through a global cache.
Permission checks in a template control presentation only: guards and
handlers must authorize every protected action on the server. Live
applications must also refresh or invalidate identities using their auth
layer when permissions change. Preserve request cancellation and deadlines;
do not save the render context in globals or long-lived component fields.
