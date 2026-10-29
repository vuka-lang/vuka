# Templates

[[toc]]

## Introduction

A `.vuka.html` shape file holds its Go in a `<script lang="go">` element,
followed by the markup that Go renders. You write your components and pages
this way, so the data a view renders and the markup that renders it live in
one file.

The script has the package, imports, exactly one rendering struct, decorated
hooks and methods. Everything after `</script>` is markup, except the
component's `<style>` and `<script lang="ts">` blocks at the end. Logic
without a view belongs in `.go` or `.vuka` files.

Errors point to the shape file's own line, not the generated Go.

## Writing a Component {#a-component}

Save this as `components/sales.vuka.html`:

```html
<script lang="go">
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
</script>

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
shape, or its generated node value from Go logic.

The same markup participates
in static rendering and live render-tree diffing.

### Scope and the Receiver {#scope-and-receiver}

Fields and methods are available by bare name: `{Title}`, `{Total()}`. A field
or method wins over a package name in a value expression; locals and loop
variables shadow fields. A tag such as `<Icon/>` resolves the package's
component type. The receiver named by the script's methods is available too,
so `{s.Title}` makes the choice explicit.

For a field whose name is also a type (`Glyph Glyph`), `{Glyph.Name}` and
`f(Glyph)` read the field, while `Glyph(x)`, `Glyph{Name: "x"}`, `[]Glyph`
and `new(Glyph)` name the type. [W0101](/errors/W0101) flags an ambiguous
bare name; use the receiver to disambiguate a value.

The generated receiver
is a pointer for a live shape, browser-state fields or pointer-receiver
methods, otherwise a value.

## The Struct in the File {#the-struct-in-the-file}

Exported fields are props, unexported fields are tracked server state or
provided dependencies, and ui.State[T] fields are browser state.

A shape is live if it embeds ui.Live, declares a decorated hook or wires a
server method to an event. A command-only event remains browser-side and does
not make the shape live. The compiler supplies the appropriate rendering
interface.

This counter is live, because it declares a mount hook and wires `Increment`
to a click:

```html
<script lang="go">
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
</script>

<button onClick={Increment}>{n}</button>
```

No struct is [E0146](/errors/E0146), multiple structs [E0147](/errors/E0147),
and a script without markup [E0148](/errors/E0148).

A form DTO, record type
or other non-render data structure goes in an adjacent logic file. Each
reusable rendering partial becomes its own shape file.

The struct's methods may also live in a `.vuka` file beside the shape: its
tracked fields read and write there as in the script. A method in a `.go`
file can't use them ([E0155](/errors/E0155)); see [where its methods
live](/ui/stateful#where-its-methods-live).

## Laying Out the File {#the-file}

The `<script lang="go">` comes first: only blank lines and `<!-- comments -->`
may precede it, and a file has one ([E0157](/errors/E0157)). Its content is
unindented Go. The package clause matches the directory (external test
shapes may use the `orders_test` package). Single and grouped imports precede
the one struct, hooks and ordinary methods.

The compiler reads the script as Go, so a `</script>` inside a string or a
comment doesn't end it; a script with no `</script>` is
[E0156](/errors/E0156). Any other `<script>` — `<script lang="ts">`,
`<script is:inline>`, a plain one — is markup.

The struct name need not match the filename. A routed page's filename supplies
its URL; it is not inferred from the struct name. Package-level declarations
in logic files are available in markup; keep the imports for a complete
example in its own script. A missing or mismatched package is
[E0142](/errors/E0142).

`vuka fmt` and editor format-on-save format the whole file: the Go as
gofmt does, the markup indented two spaces a level, and the CSS of its
`<style>`. See [formatting shape files](/tools/cli#fmt-shapes). Document an exported
component on its struct. One file renders one shape; there is no second
template body after a later function header.

### Converting a Header-Form File {#header-form}

Before 2.3, a shape wrote its Go bare above the markup, which started at the
first line beginning with `<`:

```html
<!-- card.vuka.html, before 2.3 -->
package components

type Card struct{ Title string }
<h2>{Title}</h2>
```

That form still compiles in 2.x and is removed in 3.0. `vukavet` and the
editor report it as [W0107](/errors/W0107), with a quick fix. Rewrite every
file under a directory, and format it, with:

```sh
vuka fmt -rewrite -w .
```

A package may mix both forms while you migrate. See
[converting shapes](/tools/cli#shape-script) for a rewrite that leaves the
formatting as it is.

## Writing Holes {#holes}

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

### Calls Returning an Error {#calls-returning-an-error}

A call returning `(T, error)` is its `T` wherever markup takes one value: a
hole, an attribute, a component's prop, a `for`'s range, an `if`'s or a
`switch`'s condition. The generated Go wraps it in `ui.Must`:

```html
<ul class={Store.ListClass()}>
  for _, u := range Store.Users() {
    <li>{u.Name}</li>
  }
</ul>
```

When the error isn't nil the render fails as a `Load` or `Mount` error
does: the page answers with the status of a `*web.Error`, else 500; the
error goes to the log, and its text to the browser only in development;
nothing the render wrote is sent. On a live page's re-render the browser
gets the error message a failing event gets. Each call runs once each time
its place is evaluated, as written: two holes calling `Store.Users()` call
it twice.

A statement binding one name to such a call unwraps it the same way:
`users := Store.Users()` is a `[]User`, an error failing the render where
`users` is first shown. A call passing its results on keeps them, as in Go —
`{fmt.Sprint(Store.Users())}` — and so does a statement binding both:
`users, err := Store.Users()`, then `if err != nil { … }`. A call returning
anything else where markup takes one value — `(User, bool)`, three results,
or only an `error` shown as a value — is [E0160](/errors/E0160).

> [!WARNING]
> A call in markup runs on every render, each re-render of a live page
> included: every event, every patch. A query belongs in `Load` or the
> page's `@ui.Mount`, run once into a field the markup reads.

### The Render's Context {#implicit-ctx}

A call in markup whose function takes a `context.Context` first gets the
render's `ctx` when it's written without one: the context markup already
has, as it has `user` and `perms`.

```html
<p>{len(GetCategories())} categories</p>     <!-- GetCategories(ctx) -->
for _, a := range Adverts(page) {             <!-- Adverts(ctx, page) -->
  <li>{a.Title}</li>
}
<p>{Format(user.Joined)}</p>                  <!-- as written: Format takes no context -->
```

It applies to the calls of holes, attributes, component props, blocks'
headers and statements — methods and functions, variadic and generic — and
only to a call that doesn't compile as written: one argument short, its
first parameter a `context.Context`. `GetCategories(ctx)` written out is
left as it is. Go code — a `.vuka` file, a template's header or
`<script lang="go">` — passes its contexts itself, and a method value that
isn't called, `for={GetCategories}`, is no call. Of overloads, the one the
call matches as written wins (`GetCategories()` over `GetCategories(ctx)`);
when none does and more than one matches with the ctx, the call is
ambiguous, [E0121](/errors/E0121). A call both ways fit is
[W0108](/errors/W0108): write `GetCategories(ctx)`, or
`GetCategories(/*vuka:noctx*/)` for the overload without, so that adding or
removing an overload can't change which one the page calls.

Only the context is passed: a call still short of an argument is
[E0162](/errors/E0162). A service belongs to the page, taken by its
constructor and kept in a field the markup reads. The generated Go has the
real call, `p.GetCategories(ctx)`; hover in the editor says the page
supplies the ctx, and an inlay hint shows it.

### Reviewing Authority in Markup {#reviewing-authority}

The render's `ctx` carries the request: the signed-in user, what the
context processors read, the request's deadline, and through them the
data a call may read. A call in markup that takes it shows none of that in
the source — `{len(Salaries())}` reads like a call with no arguments — so a
review that searches the source for `ctx` misses it.

To review what a template's markup can reach, read the Go it compiles to,
which has every call as it runs, its ctx written out:

```sh
vuka explain views/payroll.vuka.html   # the calls given the ctx, then the file's Go
vuka gen                               # build/: the module with Go in place of Vuka
grep -rn '(ctx' build/views/
vuka eject views/                      # the Go beside the sources (x_html.go), to diff or commit
```

`vuka explain` on a `.vuka.html` file lists the calls of its markup given
the ctx before its Go:

```text
// payroll.vuka.html: the calls in its markup given the render's ctx (implicit ctx):
//	payroll.vuka.html:12:13	Salaries(ctx)

…
func (p Payroll) View() ui.Node {
	return ui.Ctx(func(ctx context.Context) ui.Node {
		… <p>{len(p.Salaries(ctx))} people</p> …
```

A component whose markup writes no `ctx` renders without one; when a call
in its markup needs one, the component is compiled to take the render's
context, its `View` wrapped in `ui.Ctx` as above. Nothing at its callers
changes, so the list `vuka explain` prints is where to see it: a component
with calls listed there renders with the request's context.

A module that wants every use of the context written in the source turns
implicit ctx off in its `vuka.toml`, next to `go.mod`:

```toml
[compile]
implicit_ctx = false   # markup passes no ctx: write it (default true)
```

A call one `context.Context` short is then [E0162](/errors/E0162), "write
ctx explicitly (implicit ctx is off in this module)", as any other short
call, and `{len(Salaries(ctx))}` says in the source what it passes. One
call can say it too, with the setting on: `Items(/*vuka:noctx*/)` takes no
implicit ctx.

What a call does with the ctx is the function's, implicit or not: one may
keep it past the response, in a goroutine or a cache, as it could with an
explicit `ctx`. What it returns renders like any expression — a hole, an
attribute, a component's or an island's props — escaped as always, and
visible to the browser where a hole is; return only what the page may
show. The ctx itself never reaches the page: it isn't a value markup can
render or an island prop can carry.

## Go Statements in Markup {#statements}

A Go statement at the start of a line, with markup for its body, controls what
renders. The following is a fragment inside a shape's markup root:

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

The statements are `if`, `else if`, `else`, `for` (over a range or a
condition), `switch` with `case` and `default`, `x := expr` and `x = expr`. Text that starts with a
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

### Bindings {#bindings}

A statement's names are its element's: from the statement to the end of
the element it is in (the whole markup for one at its top level). A name
used after its element is [E0163](/errors/E0163).

A statement binding one name to a call of the app's code is lazy: the call
runs where the render first reads the name, once, and the result is reused.
A binding no rendered markup reads never runs — one used only in a branch
not taken, or not at all:

```html
<section>
  categories := GetCategories()   <!-- runs at the first {…} reading it -->
  stats := Stats()                <!-- never runs: no use renders -->
  <h3>{len(categories)} categories</h3>
  for _, c := range categories {
    <label>{c.Name}</label>
  }
  if false {
    <p>{stats.Open} open</p>
  }
</section>
```

The `(T, error)` unwrap applies at that first read, where the value would
have appeared. The generated Go is
`categories := sync.OnceValue(func() []Category { return ui.Must(p.GetCategories(ctx)) })`,
each read `categories()`. A binding the markup assigns again (`x = 2`), or
takes the address of, runs at its statement, as do a binding of several
names and one of standard-library calls only (`n := len(xs)`). A deferred
method's binding is its element's part: see
[Parts of a Page](/web/parts#bindings).

## Components and Slots {#components-and-slots}

### Using Component Tags

A capitalized tag names a component in the current package; a dotted tag,
`<components.Pill>` or `<ui.Card>`, names one from an import. Lowercase HTML,
SVG and MathML names remain elements, including custom-element names with a
hyphen. Component names and attributes are case-sensitive; an unknown tag is
[E0143](/errors/E0143), with the nearest matching name when possible.

A generic component takes its type arguments in the tag,
`<components.List[Task]>`, or infers them from its attributes. Close it as
`</components.List>` or `</components.List[Task]>`.

### Setting Props

A struct component's attributes set exported fields. CamelCase mapping turns
`noPad` into NoPad and `onSelect` into OnSelect. A bare bool attribute is true;
quoted text is a string and `{expression}` is typed. Quoted text setting a
`ui.Node` field is that text.

HTML elements retain HTML names (`class`, `for`, `data-*`, `aria-*`). Unknown
component attributes are errors rather than arbitrary DOM attributes. Function props let a parent
supply a callback. A typed node helper in a logic file can still be called from
a hole; reusable application markup belongs in its own shape file.

Go visibility applies: an unexported component is local to its package; an
exported one can be used through an import. Keep stable `key` values on
repeated live component tags to preserve logical instances across reorderings.

### Filling Children and Slots {#slots}

A slot is a field a caller fills with markup. Content between a component's
tags fills its `Children ui.Node` field. A `<slot:actions>` directly inside
the tag fills its `Actions ui.Node` field. A slot anywhere else in the
caller's markup is an error.

In `components/admin.vuka.html`, declare both:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Admin struct {
    Title string
    Actions ui.Node
    Children ui.Node
}
</script>

<div class="admin">
  <header><h1>{Title}</h1>{Actions}</header>
  <main>{Children}</main>
</div>
```

The caller, `pages/orders.vuka.html`, fills the slot and the children:

```html
<script lang="go">
package pages

import "example.com/app/components"

type Orders struct { Count int }
</script>

<components.Admin title="Orders">
  <slot:actions><button>New</button></slot:actions>
  <p>{Count} orders</p>
</components.Admin>
```

A slot the component doesn't have is an error naming the nearest slot it
does have: `<components.Admin> has no slot actoins; did you mean
<slot:actions>?`.

### Passing Values to a Slot {#scoped-slots}

A slot field of a func type returning `ui.Node` is a scoped slot: the
component calls it with values, and the caller's markup for it uses them.
This list renders each task through its caller's `Row`:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type List[T any] struct {
    Items []T
    Row func(T, int) ui.Node
}
</script>

<ul>
  for i, item := range Items {
    <li>{Row(item, i)}</li>
  }
</ul>
```

The caller names the values with `let`, in the order the function takes
them, and its markup reads them:

```html
<script lang="go">
package pages

import "example.com/app/components"

type Tasks struct { Open []components.Task }
</script>

<components.List items={Open}>
  <slot:row let={t, i}>
    <b>{i + 1}.</b> {t.Title}
  </slot:row>
</components.List>
```

The slot compiles to `func(t components.Task, i int) ui.Node`, its types
taken from the field: `List` infers `T` from `items`, or the tag gives it,
`<components.List[components.Task]>`. A `let` may name fewer values than the
function takes (`let={t}`), not more.

> [!NOTE]
> `let` goes only on a slot whose field is a func. On a `ui.Node` slot it is
> an error, as is a `let` naming more values than the function passes.

### Repeating a Slot {#repeated-slots}

A slot field that is a slice of structs takes one element per slot tag, in
the order the tags are written. Name the tag in the singular: each
`<slot:column>` adds a `Column` to `Columns`. The tag's attributes set the
struct's fields, and its content fills the struct's one func field returning
`ui.Node`, else its one `ui.Node` field.

The kit's `ui.Table` and `ui.DataTable` declare their columns this way:
`Columns []ui.Column[T]`, where a `ui.Column[T]` has a `Header ui.Node`, a
`Cell func(T) ui.Node` and the strings `Align`, `Class` and `Sort`. A table
writes each column as a slot, its row named by `let`:

```html
<script lang="go">
package pages

import (
    "example.com/app/models"
    "github.com/vuka-lang/ui/v2"
)

type Books struct { Shelf []models.Book }
</script>

<ui.Table rows={Shelf}>
  <slot:column header="Title" let={b}>
    <b>{b.Title}</b>
  </slot:column>
  <slot:column align="right" let={b}>
    <slot:header><abbr title="Pages">pp.</abbr></slot:header>
    {b.Pages}
  </slot:column>
</ui.Table>
```

A `<slot:header>` inside a `<slot:column>` fills that element's `Header`
with markup instead of text.

### Giving a Slot Fallback Content {#fallback}

Inside a component's own markup, the slot's tag stands for the slot, and its
content is what renders when the caller leaves the slot out:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Dialog struct {
    Title string
    Actions ui.Node
    Children ui.Node
}
</script>

<section>
  <h2>{Title}</h2>
  <slot:children><p>Nothing to show.</p></slot:children>
  <menu>
    <slot:actions><button>Close</button></slot:actions>
  </menu>
</section>
```

`<slot:actions />` with no content is `{Actions}`. A slot left out, or filled
with nothing but white space, shows the fallback.

### Checking Whether a Slot Is Filled {#has}

`ui.Has(Actions)` reports whether the caller filled a slot: false for a slot
left out and for one holding only white space. Use it to leave out the
markup around a slot:

```html
<section>
  {Children}
  if ui.Has(Footer) {
    <footer>{Footer}</footer>
  }
</section>
```

### Requiring a Slot {#required}

Tag a slot field `vuka:"required"` and every tag must fill it. A tag that
doesn't is [E0158](/errors/E0158):

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Card struct {
    Header   ui.Node `vuka:"required"`
    Children ui.Node `vuka:"required"`
    Footer   ui.Node
}
</script>

<article>
  <header>{Header}</header>
  {Children}
  <slot:footer />
</article>
```

The tag works on any prop; a required `Children` needs content between the
tags.

### Forwarding a Slot {#forwarding}

A component passes a slot it was given to another as an attribute, or as
that component's slot holding just the field:

```html
<components.Dialog title="Edit" actions={Actions} />

<components.Dialog title="Edit">
  <slot:actions>{Actions}</slot:actions>
</components.Dialog>
```

Both pass the same node: a slot holding one `ui.Node` adds no wrapper
around it.

### Where Slot Content Runs {#slot-scope}

Slot content belongs to the shape that writes it, though the component
renders it. Its holes read the writer's fields, its `onClick={Remove(t.ID)}`
calls the writer's method, and its browser state is the writer's: a live
page filling a live child's slot gets its own events and state back, and
renders again after its event, while the child keeps its own state.

### Island Tags

An imported frontend component is an island tag, with props read from its file:

```html
<script lang="go">
package pages

import Clock "../frontend/clock.tsx"

type WorldClock struct { Zone string }
</script>

<main><Clock zone={Zone} client:visible /></main>
```

See [Islands](/web/islands) for supported frameworks, prop inference,
optional values, fallback and hydration strategies.

## Scripts and Styles {#scripts-and-styles}

A `<script lang="ts">` after a template's markup is its component's
script, and a `<style scoped>` its CSS — one of each per shape. This chart
shape has both:

```html
<script lang="go">
package components

import "example.com/app/models"

type Chart struct {
    Title string
    Points []models.Point
}
</script>

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

### The Component Script

The script runs once per instance, for a page or a component alike:
`el` is its root — the markup's element, or, when the markup starts with
a component's tag or a slot, a `<vk-c style="display:contents">` around
it — `props` the struct's data fields as JSON (`ui.Node`, func and
unexported fields are not sent), `update` runs when a re-render changes
them, `destroy` when the instance leaves the page. `ctx.live.reorder(ids)`
calls the Go method `Reorder`, `ctx.state("open")` is a `ui.State` field's
handle, `ctx.ref("box")` the element with `ref="box"`, `ctx.on(event, fn)`
hears the Go's `PushEvent`, and `ctx.navigate` / `ctx.patch` move the page.
`page<T>((el, live, ctx) => …)` is the same, with `ctx.live` second.

[Component Scripts](/ui/scripts) is the guide: when a script is the right
tool, the whole `ctx`, worked examples, npm packages and troubleshooting.

### Script Types

The types come from the Go struct: `component<Chart>` knows `props.Title`,
`ctx.live`'s methods (with their Go doc), `ctx.state`'s names, the refs of
the markup, the events the Go pushes and the app's routes. The `vuka`
command writes them to `.vuka/vuka-component.d.ts`, and `vuka lsp` gives
each script to a TypeScript language server with them — completion, hover,
definition (from `ctx.live.reorder` to the Go method) and errors in the
`.vuka.html`. In VS Code it needs nothing installed; other editors name a
server ([Editors](/tools/editors#scripts)). `vuka vet` checks the names a
script passes as strings ([W0308](/errors/W0308)).

### Bundling and Scoping

The `vuka` command bundles the script and scopes the style to
the template's own elements (`:global(…)` escapes). A style has no holes.

### Inline Scripts

`<script is:inline>` is written as it is, not bundled; its one hole is
`{json(x)}`, the value as script-safe JSON:

```html
<script is:inline>window.points = {json(Points)};</script>
```

## Building Shapes {#building}

`vuka run`, `vuka build` and `vuka test` compile `.vuka.html` through an
overlay without writing among your sources. `vuka gen` writes each shape's
generated Go (`x_html.go`) for ordinary Go builds.

A package may mix logic
`.go` and `.vuka` files with shape files in the same scope. Errors and editor
navigation map back to source. The generator also emits page routes, typed
links, scoped CSS and component-script types when the project uses them.

> [!WARNING]
> The emitted render methods and node-construction calls are compiler output.
> Keep the shape file as the rendering source instead of editing generated Go.

## Reading the Render Context {#render-context}

A template can read `ctx`, the `context.Context` of its current render,
without storing it on the page or passing it through every component. This
section shows its markup only to visitors who may view analytics:

```html
<script lang="go">
package dashboard

import "github.com/vuka-lang/web/v2/auth"

type Analytics struct{}
</script>

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

### Keeping Rendering Cheap and Safe

Keep rendering free of writes and expensive database work: fetch data in
the mount hook, loaders or handlers. A [call in markup](#calls-returning-an-error)
runs on every render, each live re-render included. Context-sensitive live output is rendered
again even when its props and tracked fields have not changed. This avoids
reusing output after a context-dependent permission or locale changes;
it does not refresh an application's authentication store by itself.

> [!WARNING]
> Templates are trusted Go application code, not a sandbox for user-supplied
> markup. Never print tokens or other secret context values, put context in
> browser props, or share personalized output through a global cache.

Permission checks in a template control presentation only: guards and
handlers must authorize every protected action on the server. Live
applications must also refresh or invalidate identities using their auth
layer when permissions change. Preserve request cancellation and deadlines;
do not save the render context in globals or long-lived component fields.
