# Islands

A Vuka page renders to HTML on the server and ships no JavaScript. An **island**
is the exception: a client component — a framework component (Vue) or a vanilla
module — mounted into a server-rendered page and brought to life in the browser.
The page stays static, a few interactive spots hydrate on their own, and
everything else costs zero JS.

<!-- go:context
type Planner struct {
	ui.Live
	days, booked []string
	picked       string
	sales        []Point
	rows         []Row
}

var p = &Planner{}

type Row struct {
	ID int64 `json:"id"`
}

type DataTable struct {
	Rows []Row `json:"rows"`
}

func (p *Planner) Open(r Row)   {}
func (p *Planner) Remove(r Row) {}

type Order struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
}

type OrderStats struct {
	Count int `json:"count"`
}

type DB struct{}

func (*DB) Stats(ctx context.Context) (OrderStats, error) { return OrderStats{}, nil }
func (*DB) Orders(ctx context.Context, q string, page int) ([]Order, int, error) {
	return nil, 0, nil
}
func (*DB) Cancel(ctx context.Context, id int64) error { return nil }

var layouts struct {
	Page  func(title string) func(ui.Node) ui.Node
	Admin func(title string) func(context.Context, ui.Node) ui.Node
}

type Document struct {
	Title    string
	Children ui.Node
}

func (Document) Render(ctx context.Context, w io.Writer) error { return nil }

type Shell struct {
	Me       *auth.Identity
	Path     string
	Title    string
	Children ui.Node
}

func (Shell) Render(ctx context.Context, w io.Writer) error { return nil }

type Levels struct{ ui.Live }

func NewLevels() *Levels            { return &Levels{} }
func (l *Levels) Render() ui.Node { return ui.IslandPage(l) }
-->

## Import a component

::: vuka
The simplest island is a component file imported into a `.vuka` file. The
compiler reads the component's props from the file — no Node, no Go to
write — and generates their Go type, so the tag is type-checked like any
component's:
:::

::: go
In Go, an island's props are a struct you declare beside the component,
registered with `ui.RegisterIsland` (what the compiler writes for a `.vuka`
file's import of a component), and `ui.Mount` places it in a page — in a
template's hole, or as a `Render`'s result:
:::

::: vuka
```vuka
import (
    "github.com/vuka-lang/ui"

    Week "./frontend/islands/Week.vue"
)

func (p *Planner) Render() ui.Node {
    return <Week days={p.days} booked={p.booked} picked={p.picked} onBook={p.Book} client:load />
}
```
:::

::: go
```go
// Week's props, as the component declares them.
type Week struct {
	Days   []string `json:"days"`
	Booked []string `json:"booked"`
	Picked *string  `json:"picked,omitempty"`
}

func init() {
	ui.RegisterIsland[Week]("islands/Week.vue")
}

func (p *Planner) Render() ui.Node {
	return ui.Mount(Week{Days: p.days, Booked: p.booked, Picked: &p.picked},
		ui.Callback("onBook", p.Book), ui.Load())
}
```
:::

```vue
<script setup lang="ts">
defineProps<{ days: string[]; booked: string[]; onBook?: (day: string) => void }>()
const picked = defineModel<string>('picked')
</script>
```

A [`.vuka.html` template](/ui/templates) imports one the same way, on
its first lines:

```html
<!-- page.vuka.html -->
package app

import Clock "./clock.tsx"

<main>
  <Clock zone={Zone} client:visible />
</main>
```

`days="mon"` is a compile error (`days` is a `[]string`), as is an unknown
attribute or a literal a string union doesn't allow. The tag needs no
<span class="vuka-only">`<ui.Island>` wrapper; add one only for a fallback (below).</span><span class="go-only">wrapper; `ui.Mount`'s options give it a fallback (below).</span>

Any framework the file's extension names works — `.vue`, `.tsx`/`.jsx`
(React), `.svelte`, `.ts`/`.js` (vanilla) — with the props declared the way
that framework declares them:

| framework | props read from |
|---|---|
| Vue | `defineProps<{…}>()` / `defineProps<Props>()`, `withDefaults(…, {…})` and destructuring defaults (optional), `defineModel<T>('name')` (two-way), `defineEmits<{ select: [day: string] }>()` (function props `onSelect`) |
| React | the default export's typed first parameter: `function X(props: Props)`, `({a, b = 1}: Props)`, `const X: React.FC<Props>`, `forwardRef<El, Props>`, `memo` |
| Svelte | `let { a, b = 1, value = $bindable() }: Props = $props()`; Svelte 4's `export let a: T` |
| vanilla | `export default island<Props>((el, props, ctx) => …)` (from `vuka:island`), or `export function mount(el, props: Props, ctx)` |

`Props` may be an interface or type of the same file, or one imported from a
relative path (followed one level).

### TypeScript to Go

| TypeScript | Go |
|---|---|
| `string` | `string` |
| `number` | `float64` — any Go number may be passed |
| `boolean` | `bool` |
| `'a' \| 'b'` | a named string type, its values as constants; another literal is a compile error |
| `T[]`, `Array<T>` | `[]T` |
| `{ … }`, an interface | a struct; json tags are the prop names |
| `Record<string, T>`, `{ [k: string]: T }` | `map[string]T` |
| `Date` | `time.Time` (sent as a date) |
| `any`, `unknown` | `any` |
| `(a: A) => void`, a Vue emit | a function prop: a live view method or JS commands |
| `children`, a default `<slot>` | the tag's children |

An optional prop (`?`, a default) or a nullable one (`| null`, `| undefined`)
is a pointer with `omitempty`: left out, the component's own default
applies; a value written on the tag is passed as it is (the compiler takes
its address). Slices, maps, `any` and functions are nil already.

Anything else — `Map`, a tuple, a mixed union — is a compile error naming the
prop and asking for the explicit form below. Generated types are named after
the island: `days: { label: string }[]` on `Week` is `[]WeekDays`, an
interface `Point` is `WeekPoint`.

## Declare the props in Go

When the component's types are out of reach, declare the props as a Go
struct <span class="vuka-only">annotated `@ui.Island`</span><span class="go-only">registered with `ui.RegisterIsland[T]()`</span>. Its exported fields are the props; it wins
over an import of the same name:

::: vuka
```vuka
type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

@ui.Island
type Chart struct {
    Series []Point `json:"series"`
    Title  string  `json:"title"`
}
```
:::

::: go
```go
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Chart struct {
	Series []Point `json:"series"`
	Title  string  `json:"title"`
}

func init() {
	ui.RegisterIsland[Chart]()
}
```
:::

Written bare, <span class="vuka-only">`@ui.Island`</span><span class="go-only">`ui.RegisterIsland[Chart]()`</span> finds the component by the type's name: first
next to the file declaring the type, `<snake>.<ext>` (`sales_chart.tsx` for
`SalesChart`), then `islands/<Type>.<ext>` under the frontend root (`web.FrontendRoot("web")`:
here `web/islands/Chart.vue`), in the module's own folder for a package
under `modules/<m>/` — <span class="vuka-only">`@ui.Island` on</span><span class="go-only">registering</span> `Orders` in `modules/sales/` is
`web/islands/sales/Orders.vue`. The extension is whichever of `.vue`,
`.tsx`, `.jsx`, `.svelte`, `.ts`, `.js` exists, and gives the framework.
No such file, or two (`Chart.vue` and `Chart.tsx`), is an error naming
them, in the build and in the editor. `vuka add island`, `vuka add page
--island` and `vuka new` put the component where the bare form looks.

::: vuka
To set the default hydration strategy (used when a tag has no `client:*`
directive), add `ui.Strategy`: `@ui.Island(ui.Strategy("load"))` keeps the
inference, `@ui.Island("charts/Revenue.vue", ui.Strategy("idle"))` names
the file. A second string is a strategy too: `@ui.Island("", "load")`.

Name the file when it is elsewhere: `@ui.Island("charts/Revenue.vue")` is
read from the frontend root, whichever package declares the struct; a path
starting with `./` or `../`, or a file beside the declaring one, is read from
there instead.
:::

::: go
To set the default hydration strategy (used when `ui.Mount` is given none),
add `ui.Strategy`: `ui.RegisterIsland[Chart](ui.Strategy("load"))` keeps the
inference, `ui.RegisterIsland[Revenue]("charts/Revenue.vue", ui.Strategy("idle"))`
names the file. A second string is a strategy too:
`ui.RegisterIsland[Chart]("", "load")`.

Name the file when it is elsewhere: `ui.RegisterIsland[Revenue]("charts/Revenue.vue")`
is read from the frontend root, whichever package declares the struct; a path
starting with `./` or `../`, or a file beside the declaring one, is read from
there instead.
:::

## Place it in a page

<span class="vuka-only">A bare tag mounts it; a `client:*` directive says *when* it hydrates. To show
something until then, wrap it in `<ui.Island>` with a `<ui.Template fallback>`:</span><span class="go-only">`ui.Mount(props, opts…)` mounts it, in a template's hole or as a `Render`'s result; a strategy option says *when* it hydrates, and `ui.Fallback` what shows until then:</span>

::: vuka
```vuka
<ui.Island>
  <ui.Template fallback><p>Loading…</p></ui.Template>
  <Chart series={p.sales} title="Revenue" client:visible/>
</ui.Island>
```
:::

::: go
<!-- go:fragment -->
```go
chart := ui.Mount(Chart{Series: p.sales, Title: "Revenue"}, ui.Visible(), ui.Fallback(ui.HTML(`<p>Loading…</p>`)))
```
:::

The component's <span class="vuka-only">attributes bind to</span><span class="go-only">props are</span> the struct fields (type-checked by Go), its
children (<span class="go-only">`ui.Slot(n)`</span><span class="vuka-only">the tag's</span>) become the component's slot, and the fallback is distinct from the
slot — it's the pre-hydration view.

### Hydration strategies

| directive | `ui.Mount` option | when it mounts |
|---|---|---|
| `client:load` | `ui.Load()` | immediately on load |
| `client:idle` | `ui.Idle()` | on `requestIdleCallback` |
| `client:visible` | `ui.Visible()` | when it scrolls into view (`IntersectionObserver`) — the default |
| `client:media={"(max-width: 600px)"}` | `ui.Media("(max-width: 600px)")` | when the media query matches |
| `client:only` | `ui.ClientOnly()` | never server-rendered; mount fresh on the client |

An island with no visible fallback and `client:visible` (`ui.Visible()`) never hydrates (a
zero-height host never intersects) — give it a fallback with height, or use
`client:load` (`ui.Load()`).

## Reactivity

### Live props (server → island)

On a live page every prop is live: when a re-render changes the island's
props, the mounted component gets them through its adapter's `update` — no
remount, no lost local state. Props that didn't change cause no update.
<span class="vuka-only">`once` on the tag</span><span class="go-only">`ui.Once()`</span> keeps the props it was first given:

::: vuka
```vuka
<Chart series={p.sales} client:load />        // follows p.sales
<Chart series={p.sales} once client:load />   // the first series only
```
:::

::: go
<!-- go:fragment -->
```go
live := ui.Mount(Chart{Series: p.sales}, ui.Load())            // follows p.sales
once := ui.Mount(Chart{Series: p.sales}, ui.Once(), ui.Load()) // the first series only
```
:::

A tracked field — a `.vuka.html` struct's unexported one, which the compiler
writes as a `uigen.Assign` — is sent as its value.

### Function props (island → server)

A prop whose type is a function takes a live view method. Calling the
function in the browser pushes the method, its first argument as the
method's parameter (checked against the function's type at compile time):

::: vuka
```vuka
func (p *Planner) Book(day string) { … }

<Week onBook={p.Book} />            // onBook?: (day: string) => void
```
:::

::: go
```go
func (p *Planner) Book(day string) { p.picked = day }
```

<!-- go:fragment -->
```go
week := ui.Mount(Week{}, ui.Callback("onBook", p.Book)) // onBook?: (day: string) => void
```
:::

It takes JS commands too, run in the browser when the function is called:
<span class="vuka-only">`<Dialog onClose={open.Set(false)} />`</span><span class="go-only">`ui.Mount(Dialog{}, ui.Callback("onClose", open.Set(false)))`</span>. In Vue the function is the listener
of the matching emit (`defineEmits<{ close: [] }>()` gives `onClose`); in
React and Svelte it is the prop itself.

### State props (two-way)

A [`ui.State`](/ui/state) passed as a prop is shared with the component:
the component gets its value, follows it when the page (or another island)
changes it, and its writes set the state — and every binding on the page
that reads it:

::: vuka
```vuka
picked := ui.State("mon")
<b>{picked}</b>
<Week picked={picked} />
```
:::

::: go
```html
<!-- week.vuka.html -->
package app

import Week "./islands/Week.vue"

type Planner struct {
	picked ui.State[string]
}
<div><b>{picked}</b><Week picked={picked} /></div>
```
:::

The component writes back by its framework's convention: a Vue
`defineModel` (`update:picked`), a React `setPicked` / `onPickedChange` prop
(the adapter passes them), a Svelte `$bindable` prop. An expression of
states is passed one way. The state's type must fit the
prop's (`ui.State[int]` for a `boolean` prop is a compile error).

### Emits

Without a declared prop, <span class="vuka-only">`on<Emit>` attributes</span><span class="go-only">`ui.Emit(name, handler)` options</span> bind a Vue component's emits
to live-view handlers; the emit's argument arrives as JSON, bound into the
handler's struct parameter:

::: vuka
```vuka
<ui.Island>
  <DataTable rows={p.rows} onRowClick={p.Open} onDelete={p.Remove} client:load/>
</ui.Island>
```
:::

::: go
<!-- go:fragment -->
```go
table := ui.Mount(DataTable{Rows: p.rows}, ui.Emit("rowClick", p.Open), ui.Emit("delete", p.Remove), ui.Load())
```
:::

## Pages backed by a component

A page can be a component: <span class="vuka-only">put `@web.Live` on an `@ui.Island` struct
embedding `ui.Live`</span><span class="go-only">route a struct embedding `ui.Live` with `web.Routes.Live` and the `ui.Island(…)` option, its `Render` returning `ui.IslandPage`</span>, and the page's template is the component file (here
`web/islands/Orders.vue`). The struct says what the page holds; the
component reads it, sets what it may and calls the rest.

::: vuka
```vuka
@web.Live("/orders")
@ui.Island(ui.Layout(layouts.Page("Orders")))
@auth.Required
type Orders struct {
    ui.Live                                // a live page
    Q      string  @ui.Bind @ui.URL("q")         // the component sets it; kept in the address's ?q=
    Status string  @ui.Bind{Debounce: ui.NoDebounce} // set at once, no wait
    Rows   []Order                               // read only
    db     *DB                                   // the server's own: injected, never sent
}

@ui.Mount
func (o *Orders) mount(ctx context.Context) error { return o.load(ctx) }

@ui.OnChange
func (o *Orders) changed(ctx context.Context, c ui.Changed) error { return o.load(ctx) } // after the component sets Q or Status

func (o *Orders) Cancel(ctx context.Context, id int64) error { … } // callable from the component
```

The attributes and hooks are the page's `LiveSchema` method, which a plain
Go page writes itself:

```go
func (o *Orders) LiveSchema(l *ui.LiveSchema) {
    l.Field(&o.Q, ui.Bind{}, ui.URL("q"))
    l.Field(&o.Status, ui.Bind{Debounce: ui.NoDebounce})
    l.Mount(o.mount)
    l.OnChange(o.changed)
}
```
:::

::: go
```go
type Orders struct {
	ui.Live         // a live page
	Q       string  // the component sets it; kept in the address's ?q=
	Status  string  // set at once, no wait
	Rows    []Order // read only
	page    int     // the server's own, as db is
	db      *DB     // the server's own: injected, never sent
}

func NewOrders(db *DB) *Orders { return &Orders{db: db} }

func (o *Orders) LiveSchema(l *ui.LiveSchema) {
	l.Field(&o.Q, ui.Bind{}, ui.URL("q"))
	l.Field(&o.Status, ui.Bind{Debounce: ui.NoDebounce})
	l.Mount(o.mount)
	l.OnChange(o.changed)
}

func (o *Orders) mount(ctx context.Context) error                 { return o.load(ctx) }
func (o *Orders) changed(ctx context.Context, c ui.Changed) error { return o.load(ctx) } // after the component sets Q or Status
func (o *Orders) Cancel(ctx context.Context, id int64) error      { return o.db.Cancel(ctx, id) } // callable from the component

func (o *Orders) load(ctx context.Context) (err error) {
	o.Rows, _, err = o.db.Orders(ctx, o.Q, o.page)
	return err
}

// Render is the component file: web/islands/Orders.vue.
func (o *Orders) Render() ui.Node { return ui.IslandPage(o) }

func init() {
	web.Routes.Live("/orders", NewOrders, ui.Island(ui.Layout(layouts.Page("Orders"))), auth.Required())
}
```
:::

::: vuka
A stateless page — no connection, each call a signed POST — is `@web.Get`
on a struct without `ui.Live`. `@web.Get` on one with `ui.Live` is a live
page too, as before; `vuka vet` and the editor suggest `@web.Live` for it.
`@web.Live`'s options work as for any live page: `web.Layout`, the type's
gates.
:::

::: go
A stateless page — no connection, each call a signed POST — is
`web.Routes.Get(path, NewT, ui.Island(…))` on a struct without `ui.Live`.
`web.Routes.Live`'s options work as for any live page: `web.Layout`, gates
such as `auth.Required()`.
:::

```vue
<script setup lang="ts">
import { useLive } from 'vuka:live'
import type { Orders } from 'vuka:live'
const live = useLive<Orders>()
</script>

<template>
  <input v-model="live.q.value" placeholder="Search" />
  <ul>
    <li v-for="o in live.rows.value" :key="o.id">
      {{ o.number }}
      <button :disabled="live.cancel.pending" @click="live.cancel(o.id)">Cancel</button>
    </li>
  </ul>
  <p v-if="live.error">{{ live.error.message }}</p>
</template>
```

- **Every exported field is a handle** on `live`, named like the field's
  json name with its first letter lower-cased (`Q` → `live.q`, `Rows` →
  `live.rows`): `get()` and `value` read it, and the component follows when
  the server changes it. Unexported fields are the server's — dependencies
  of provided types are injected into them, as into other components — and
  never reach the browser.
- **<span class="vuka-only">`@ui.Bind`</span><span class="go-only">`ui.Bind{}`</span> fields are the ones the component sets**:
  `live.q.set(v)`, or `live.q.value = v` (so `v-model="live.q.value"`).
  The value is the browser's at once; the set is sent after a pause
  (300ms; <span class="vuka-only">`@ui.Bind{Debounce: 500 * time.Millisecond}`</span><span class="go-only">`ui.Bind{Debounce: 500 * time.Millisecond}`</span> to change it, a
  negative one for none; `set(v, { now: true })` sends it straight away),
  and the sets of several fields within one pause go as one update. The
  server sets the fields, runs the <span class="vuka-only">`@ui.OnChange`</span><span class="go-only">`l.OnChange`</span> hook if the page has one
  — `func(ctx context.Context, c ui.Changed) error`; its dependencies are
  the struct's injected fields, as for the other methods — and answers
  with the props. `c` says which fields the update set
  ([which fields changed](#which-fields-changed)). A field without
  <span class="vuka-only">`@ui.Bind`</span><span class="go-only">`ui.Bind`</span> has no `set` and its `value` is read only, in the types and
  on the server, which refuses a set of it (403, as `live.error`).
- **Exported methods are what the component calls**: `live.cancel(id)`
  returns a promise; the arguments are JSON, checked against the method's
  Go parameters on the server and typed in `vuka:live`. A method has its own
  `live.cancel.pending` and `live.cancel.error`; `live.pending` and
  `live.error` are the page's (any call). An error is `{status, message,
  fields}`, `fields` from a `web.Invalid{"q": "…"}`. A `web.Redirect`
  navigates. Calls and sets go one at a time, in order; a method waits for
  the sets before it. `Render`, `Update`, `Info` and the hooks
  (<span class="vuka-only">`@ui.Mount`, `@ui.OnChange`, `@ui.Loads`</span><span class="go-only">those `LiveSchema` sets: `l.Mount`, `l.OnChange`, `l.Loads`</span>) aren't callable — a hook may
  be unexported — and a method and a field can't share a name on `live`
  (the app doesn't start).
- **<span class="vuka-only">`@ui.Defer("group")`</span><span class="go-only">`ui.Defer("group")`</span> fields** aren't in the first render: the group's
  loader sends them after it, and the component reads them as pending
  until then — [deferred fields](#deferred-fields).
- **<span class="vuka-only">`@ui.URL("name")`</span><span class="go-only">`ui.URL("name")`</span> fields** are filled from the query when the page loads,
  and when they change the address follows (`history.replaceState`), so a
  reload or a shared link shows the same page.
- **Live** (`ui.Live` embedded): the struct is the live view, its Render the
  island (<span class="vuka-only">the compiler writes it</span><span class="go-only">`ui.IslandPage(x)`</span>). A set or a call is a live event on the
  server's instance; the fields it changes reach the component, with no
  remount. Mount, `Subscribe` and `Update` (broadcasts), `PushJS` and the
  type's gates work as for any live view; on a patch, an `Update` <span class="go-only">(`UpdateURLParams`)</span> taking
  [`web.URLParams`](/web/navigation#patch-or-navigate) takes the new URL
  (the fields are the page's own: it sets what follows from it).
- **Stateless** (no `ui.Live`): no connection. A set or a call posts the
  fields or the method with its arguments, and the props the page was
  rendered with — signed with the app's key (`web.LiveKey`), bound to the
  page, the visitor's sign-in and an expiry — to the page's URL; the server
  builds the struct again from them, injects its dependencies, runs its
  Mount hook, then the set and its OnChange hook or the method, and answers with the new props,
  signed again. Changed props are refused. Calls are refused cross-origin
  and pass the <span class="vuka-only">type's gates (`@auth.Required`…)</span><span class="go-only">route's gates (`auth.Required()`…)</span> like the page.
- **The visitor** is known to every island's component: `$vuka` in Vue
  templates (`useVuka()` in script), `useVuka()` in React, and
  `const vuka = useVuka()` (a store, `$vuka.user`) in Svelte —
  `{user: {id}, roles, perms, can(perm)}` from the signed-in identity
  (`web/auth`).
- **Moving the page**: `live.navigate(href)` and `live.patch(href)` — also
  `vuka.navigate` / `vuka.patch` on `useVuka()`, in any island — move the
  page as a link would, over the live connection when there is one;
  `<VukaLink href>` from `vuka:live` is the link itself. A navigation
  unmounts the old page's islands and mounts the new page's.
  [From an island](/web/navigation#from-an-island).

React and Svelte read the same handles:

```tsx
const live = useLive<Orders>()   // re-renders when anything on live changes
<input value={live.q.value} onChange={(e) => live.q.set(e.target.value)} />
```

```svelte
<script lang="ts">
  import { live } from 'vuka:live'
  import type { Orders } from 'vuka:live'
  const l = live<Orders>()
</script>
<input bind:value={l.q.value} />
{#if $l.pending}…{/if}
```

The fields still arrive as the component's props too, so a component may
read them with `defineProps<Orders>()` (React: its props; Svelte:
`$props()`) instead.

`vuka:live` is a module of the island build — React: `useLive<T>()`, Svelte:
`live<T>()` (also a store: `$l.pending`) — and its types are `vuka-live.d.ts`,
which `vuka` writes into the island root from the Go structs. Map the name
to it in `tsconfig.json`, which also lets Vue's compiler read
`defineProps<Orders>()` (with `typescript` installed):

```json
{ "compilerOptions": { "paths": { "vuka:live": ["./vuka-live"] } } }
```

<span class="vuka-only">From Go the decorator is the route's option:</span><span class="go-only">On one app:</span>
`app.Live(path, NewT, ui.Island(opts…))` for a live page and
`app.Get(path, NewT, ui.Island(opts…))` for a stateless one, the component's
path inferred as <span class="vuka-only">bare `@ui.Island` infers it</span><span class="go-only">a bare `ui.RegisterIsland` infers it</span>.

### Layouts

`ui.Layout` in <span class="vuka-only">the decorator</span><span class="go-only">the `ui.Island(…)` option</span> is the layout of the page the type backs:
the page is `layout(component)`. A layout is a `func(page ui.Node)
ui.Node` — what `layouts.Page("Orders")` returns — or a `func(ctx
context.Context, page ui.Node) ui.Node`, which reads the request as a
page's code does: the signed-in user, the path, the page's title.

::: vuka
```vuka
// layouts/admin.vuka
func Admin(title string) func(context.Context, ui.Node) ui.Node {
    return func(ctx context.Context, page ui.Node) ui.Node {
        return <Document title={title}>
            <Shell me={auth.Current(ctx)} path={web.Request(ctx).URL.Path} title={title}>{page}</Shell>
        </Document>
    }
}
```
:::

::: go
```go
// layouts/admin.go
func Admin(title string) func(context.Context, ui.Node) ui.Node {
	return func(ctx context.Context, page ui.Node) ui.Node {
		return Document{Title: title, Children: Shell{
			Me:       auth.Current(ctx),
			Path:     web.Request(ctx).URL.Path,
			Title:    title,
			Children: page,
		}}
	}
}
```
:::

::: vuka
```vuka
@web.Live("/settings/levels")
@ui.Island(ui.Layout(layouts.Admin("Levels")))
type Levels struct { ui.Live; … }
```
:::

::: go
```go
func init() {
	web.Routes.Live("/settings/levels", NewLevels, ui.Island(ui.Layout(layouts.Admin("Levels"))))
}
```
:::

Several nest, the first outermost: `ui.Layout(layouts.Page("Levels"),
layouts.Sidebar)` is `Page(Sidebar(component))`. The page's layout is the
first of:

1. `ui.Layout` on the island,
2. `web.Layout` on the route (<span class="vuka-only">`@web.Live("/x", web.Layout(…))`</span><span class="go-only">`web.Routes.Live("/x", NewX, web.Layout(…))`</span>),
3. the app's `web.LiveLayout`,
4. a bare document.

The layout applies only where the type is routed as a page: placed inside
another page, the island ignores it<span class="vuka-only"> (hover on the decorator says so)</span>.

The layout decides the `<title>`. A type with a `Title() string` method
gives the page's title: the bare document uses it (else the type's name),
a layout reads it with `ui.PageTitle(ctx)`, and a document whose head has
no `<title>` gets it. A live page's `Title` is read before `Mount`, from
what the URL binds; a stateless page's after.

Moving between pages that share a layout — a link, `live.navigate` — goes
over the live connection and changes only what differs: the layout's
elements stay the same elements (a sidebar keeps its scroll position),
its marked link following the path.

### Validation

`validate:` tags on a method's arguments, and the `validate` attributes of
a page's bound fields, are checked on the server before anything runs,
live and stateless alike<span class="go-only"> — a bound field's rules are given to `l.Field`, and a method may check its argument itself, answering with a `web.Invalid` as below</span>:

::: vuka
```vuka
type Filter struct {
    Years int   `json:"years" validate:"min=1,max=40"`
    Level Level `json:"level"`
}

type Level struct {
    Code string `json:"code" validate:"required,len=3"`
}

type Orders struct {
    ui.Live
    Q    string  @ui.Bind @ui.URL("q") @validate.Max(50)
    Rows []Order
}

func (o *Orders) Narrow(ctx context.Context, f Filter) error { … }
```
:::

::: go
```go
type Filter struct {
	Years int   `json:"years"`
	Level Level `json:"level"`
}

type Level struct {
	Code string `json:"code"`
}

// Orders as above, its Q with a rule.
func (o *Orders) LiveSchema(l *ui.LiveSchema) {
	l.Field(&o.Q, ui.Bind{}, ui.URL("q"), validate.Max(50))
}

// Narrow checks its argument itself: a web.Invalid answers 422, its keys
// the fields' paths.
func (o *Orders) Narrow(ctx context.Context, f Filter) error {
	bad := web.Invalid{}
	if f.Years < 1 || f.Years > 40 {
		bad["years"] = "between 1 and 40"
	}
	if len(f.Level.Code) != 3 {
		bad["level.code"] = "exactly 3 characters"
	}
	if len(bad) > 0 {
		return bad
	}
	// …
	return nil
}
```
:::

The rules: `required` (not the zero value), `omitempty` (skip the rest when
it is), `min=n`, `max=n` and `len=n` (a string's characters, a number's
value, a slice's or map's items), `oneof=a b c` and `email`, comma
separated; the attributes of package `ui/validate` are the same rules
(<span class="vuka-only">`@validate.Required`, `@validate.Max(50)`, `@validate.OneOf{"a", "b"}`</span><span class="go-only">`validate.Required{}`, `validate.Max(50)`, `validate.OneOf{"a", "b"}`, given to `l.Field`</span>).
A tag with another rule, or a bad value (`max=five`), stops the app at
start.

- **A method's arguments**: each struct argument (or pointer to one, or a
  slice or map of them) is checked, and the structs it holds at any depth.
  If any field fails, the method doesn't run and the call answers 422 with
  each field by its path inside the argument — its json names, a slice's
  items by index:

  ```json
  {"status": 422, "message": "invalid: level.code: exactly 3 characters; years: at least 1",
   "fields": {"years": "at least 1", "level.code": "exactly 3 characters"}}
  ```

  The promise rejects with an `Error` whose `status` and `fields` are those
  (`err.fields["level.code"]`), and `live.narrow.error.fields` holds them
  too. A slice argument's items are `0.code`, `1.code`; a method taking two
  structs with a field of one name shows the first one's message.
  Arguments that are plain values (`id int64`) have no tags to check.
- **A bound field**: every field of one set is checked before any is
  applied; if one fails, none is set and the OnChange hook doesn't run. The answer
  is the same 422, each field under its wire name (`fields: {"q": "at most
  50 characters"}`, inside a struct field `where.city`). `live.q.error` is
  that field's message (`"city: required"` for one inside it), or `null`.
  What was typed stays in the input, shown with its error, until the field
  is set again (a valid set clears the error) or a method succeeds; any
  other refusal (a field without <span class="vuka-only">`@ui.Bind`</span><span class="go-only">`ui.Bind`</span>) puts the server's value back.

```vue
<input v-model="live.q.value" />
<p v-if="live.q.error">{{ live.q.error }}</p>
```

A `web.Invalid` returned by a method or by the OnChange hook reaches the
component the same way.

### Which fields changed

Several fields set within one pause reach the OnChange hook as one
update; its `ui.Changed` says which:

::: vuka
```vuka
@ui.OnChange
func (o *Orders) changed(ctx context.Context, changed ui.Changed) error {
    if changed.Has("status") {
        o.page = 1                       // a new filter starts at the first page
    }
    return o.load(ctx)
}
```
:::

::: go
```go
// the hook l.OnChange(o.changed) sets
func (o *Orders) changed(ctx context.Context, changed ui.Changed) error {
	if changed.Has("status") {
		o.page = 1 // a new filter starts at the first page
	}
	return o.load(ctx)
}
```
:::

`changed.Names()` are the wire names of the fields the update set — the
prop's json name, else the Go name — sorted. `changed.Has(name)` takes
the wire name, the handle (`live.q`'s `q`) or the Go name (`Q`), so
`changed.Has("Q")` and `changed.Has("q")` both hold for `Q`.

### Deferred fields

A field with <span class="vuka-only">`@ui.Defer("group")`</span><span class="go-only">`ui.Defer("group")`</span> isn't in the page's first render: the page
paints, the component mounts, and the field follows once the server has
it. Nothing in the component asks for it — a slow count or the first
tab's rows don't hold up the page.

::: vuka
```vuka
@web.Live("/overview")
@ui.Island(ui.Layout(layouts.Page("Overview")))
type Overview struct {
    ui.Live
    Q      string     @ui.Bind @ui.URL("q")
    Stats  OrderStats `json:"stats"` @ui.Defer("stats")
    Rows   []Order    `json:"rows"` @ui.Defer("lists")   // Rows and Total load together
    Total  int        `json:"total"` @ui.Defer("lists")
    db     *DB
}

@ui.Loads("stats")
func (o *Overview) loadStats(ctx context.Context) error {
    s, err := o.db.Stats(ctx)
    o.Stats = s
    return err
}

@ui.Loads("lists")
func (o *Overview) loadLists(ctx context.Context) error {
    rows, total, err := o.db.Orders(ctx, o.Q, 1)
    o.Rows, o.Total = rows, total
    return err
}
```
:::

::: go
```go
type Overview struct {
	ui.Live
	Q     string
	Stats OrderStats `json:"stats"`
	Rows  []Order    `json:"rows"`
	Total int        `json:"total"`
	db    *DB
}

func NewOverview(db *DB) *Overview { return &Overview{db: db} }

func (o *Overview) LiveSchema(l *ui.LiveSchema) {
	l.Field(&o.Q, ui.Bind{}, ui.URL("q"))
	l.Field(&o.Stats, ui.Defer("stats"))
	l.Field(&o.Rows, ui.Defer("lists")) // Rows and Total load together
	l.Field(&o.Total, ui.Defer("lists"))
	l.Loads("stats", o.loadStats)
	l.Loads("lists", o.loadLists)
}

func (o *Overview) loadStats(ctx context.Context) error {
	s, err := o.db.Stats(ctx)
	o.Stats = s
	return err
}

func (o *Overview) loadLists(ctx context.Context) error {
	rows, total, err := o.db.Orders(ctx, o.Q, 1)
	o.Rows, o.Total = rows, total
	return err
}

func (o *Overview) Render() ui.Node { return ui.IslandPage(o) }

func init() {
	web.Routes.Live("/overview", NewOverview, ui.Island(ui.Layout(layouts.Page("Overview"))))
}
```
:::

- **Groups and loaders.** <span class="vuka-only">`@ui.Defer("lists")`</span><span class="go-only">`ui.Defer("lists")`</span> puts the field in the group
  `lists`. Each group has one loader, <span class="vuka-only">the method marked `@ui.Loads("lists")`
  (`l.Loads("lists", o.loadLists)` in Go)</span><span class="go-only">the method `l.Loads("lists", o.loadLists)` names</span>, a `func(ctx context.Context)
  error`. A missing loader stops the app at start (E0324), as does a
  deferred field the component binds or the URL keeps (E0323). A loader
  isn't on `live`: the component can't call it.
- **When.** On a live page the loaders run once the page has joined, each
  group on its own goroutine, and each group's fields reach the component
  as an update when its loader returns — the fast ones don't wait for the
  slow. Their context ends when the visitor leaves the page or the
  connection closes. On a stateless page the component, once mounted,
  posts one load (signed and gated as its calls are); the server runs
  `Mount` and the loaders, concurrently, and answers with every group at
  once. Calls the component makes meanwhile wait for it.
- **A copy.** A loader runs on a copy of the page taken when it starts, so
  events keep running beside it. Only its group's fields are taken from the
  copy: set those; anything else it changes is dropped. Read what `Mount`
  set and what the URL bound (`o.Q`) freely.
- **In the component** a deferred field's handle has `pending` and `error`:
  `live.stats.value` is `undefined` (and `pending` true) until its group
  arrives; if its loader fails, `pending` turns false and `error` is
  `{status, message}` — a `web.Error`'s message, or `"error"` (the cause
  is logged; under <span class="vuka-only">`vuka run`</span><span class="go-only">`web.Dev()`</span>, its text). The page itself is fine: the
  other groups arrive, methods and sets work.

```vue
<template>
  <p v-if="live.stats.pending">Loading…</p>
  <p v-else-if="live.stats.error">{{ live.stats.error.message }}</p>
  <Tiles v-else :stats="live.stats.value" />
</template>
```

`vuka-live.d.ts` types a deferred field as optional in the props
(`stats?: OrderStats`) and its handle as `LiveDeferred<OrderStats>`:
`value` and `get()` are `OrderStats | undefined`, with `pending` and
`error`.

## Build

Island components live under the **frontend root** — the directory
[`web.FrontendRoot`](/web/frontend) names (the one holding `package.json` and
`node_modules`), or `web.IslandRoot("web")` for an app with no frontend of
its own. How they're bundled depends on the framework:

- **Script islands need no Node.** A `.ts`/`.js` island is bundled by the
  `vuka` command's own esbuild; it default-exports `island(fn)` from
  `vuka:island` (or exports `mount(el, props, ctx)`):

  ```ts
  import { island } from 'vuka:island'
  import { Chart } from 'chart.js'

  export default island<SalesChartProps>((el, props, ctx) => {
    const c = new Chart(el, { type: 'line', data: { datasets: [{ data: props.points }] } })
    return { update(next) { c.data.datasets[0].data = next.points; c.update() }, destroy() { c.destroy() } }
  })
  ```

  `fn` runs once per instance, `update` with new props, `destroy` when it
  leaves; `pageIsland<T>((el, live) => …)` is a page backed by it. A `.tsx`
  or `.jsx` island default-exports a React component, compiled the same way
  with React's adapter; `react` and `react-dom` (or `preact`, used in their
  place) come from the frontend root's `node_modules`. Packages a script
  imports (`chart.js`) resolve from there too. Their types are in
  `vuka-island.d.ts` (tsconfig `"paths": {"vuka:island": ["./vuka-island"]}`).
- **Vue and Svelte** build with Node esbuild (and
  `esbuild-plugin-vue3` / `esbuild-svelte`) from the frontend root's
  `node_modules` — `npm i -D esbuild vue esbuild-plugin-vue3`, or `svelte
  esbuild-svelte`. Without Node the build says which file needs it, and
  that a `.tsx` or `.ts` island doesn't.
- With a `vite.config.*`, they build through the project's **Vite** when the frontend root has a
  `vite.config.*`, so Vite-plugin UI libraries work. The config is ESM
  (`vite.config.mts`) and lists the UI plugins plus the vuka plugin:

  ```ts
  import { defineConfig } from 'vite'
  import vue from '@vitejs/plugin-vue'
  import ui from '@nuxt/ui/vite'
  import tailwindcss from '@tailwindcss/vite'
  import vuka from './vuka/vite.js'

  export default defineConfig({ plugins: [vue(), ui(), tailwindcss(), vuka()] })
  ```

  With `@nuxt/ui/vite` + `@tailwindcss/vite`, a `.vue` island can use Nuxt UI
  components (`<UButton>`, `<UBadge>`, …) with no imports — they resolve and
  style out of the box. An `app.css` (`@import "tailwindcss"; @import "@nuxt/ui";`)
  is the CSS entry; the command serves the built island CSS automatically.

`vuka doctor` lists each island with what builds it — the in-process
esbuild, the project's Vite, or Node — and what is missing for it.

::: vuka
`vuka build` compiles the islands into the binary; `vuka build` + `go build`
ship one binary. In development, `vuka run` rebuilds the islands when a file
their build reads changes: the island's source and what it imports, and with a
Vite frontend anything under its root (`vite.config`, `app.css`, shared
components, `_setup`, `package.json`) and the directories `app.css` names
with `@source` — not `node_modules`, `dist` or `.d.ts` files.
:::

::: go
`go tool vuka build-islands` builds the islands a module registers
(`ui.RegisterIsland`, a route's `ui.Island` option) from the source alone,
into the frontend's build directory, where `//go:embed` picks them up; it
also writes `islands.d.ts` and `vuka-live.d.ts` beside the components. Run
it after the frontend's own build (`vite build` empties its `dist`), as a
`go:generate` step, and `go build` ships one binary. In development,
`web.Dev()` runs `vuka build-islands -watch` beside the app under `go run`
(`VUKA_DEV=1`), rebuilding the islands when a file their build reads
changes. The command is a tool of the module:
`go get -tool github.com/vuka-lang/vuka/cmd/vuka`. The main package
carries both directives — `//go:generate go tool vuka build-islands`, and
`//go:embed all:web/dist` above `var dist embed.FS` — and serves the build:

<!-- go:context
var dist embed.FS
-->

```go
func main() {
	app := web.New(web.Frontend(dist, "web/dist"), web.Dev())
	app.Live("/orders", NewOrders, ui.Island(ui.Layout(layouts.Page("Orders"))))
	if err := app.Run(""); err != nil {
		log.Fatal(err)
	}
}
```
:::

### Scaffold

```sh
vuka new myapp --islands
```

scaffolds the frontend root as a Vite + Nuxt UI project (the config above, the
dependencies, an `app.css`, and a sample island) so a Nuxt UI component renders
immediately.

### An optional per-app setup

A `_setup.ts` (default export `(app) => {}`) in the frontend root runs once per
island app — register a Vue plugin, Pinia, or global components there.

## How it works

<span class="vuka-only">`<ui.Island>`</span><span class="go-only">An island (`ui.Mount`)</span> renders a self-describing host element the client loader
hydrates:

```html
<vk-island data-vk-s="…" data-vk-island="Chart" data-vk-client="visible"
  data-vk-props='{"series":[…],"title":"Revenue"}'>
  <p>Loading…</p>
</vk-island>
```

The loader arms the strategy, lazy-imports the component chunk (and its
framework adapter) only when the strategy fires, and mounts it. The host's
`data-vk-s` is a stable id for its placement, so a live-view re-render updates
the host's attributes — a changed `data-vk-props` drives the component's
`update()` — while leaving its client-owned DOM untouched. Values JSON can't
carry are tagged in the props: `{"$vk":"date","v":…}` a date,
`{"$vk":"fn","e":"onbook"}` a function prop (the host's `data-vk-on-onbook`
handler or `data-vk-js-onbook` commands), `{"$vk":"state","s":…,"i":…}` a
state (`"x"`: a derived state's expression). Function calls and emits travel
back as ordinary live events. A page's deferred fields are left out of
`data-vk-props` until loaded; `data-vk-defer` lists those still pending
(`p`) and the failed ones' errors (`e`). See [Live components](/web/live) for the live-view model.

## Limits

- A `.vue` island must sit under the frontend root (so its `vue`/library imports
  resolve); a vanilla `.ts` island may live anywhere.
- A `.css` imported inside an SFC can't be island-bundled — import the
  component's JS and provide its CSS through the app's stylesheet.
- A function prop calls its handler with one payload, its first argument.
- An imported component's props must be typed (a `.jsx` or untyped Svelte
  component needs <span class="vuka-only">the `@ui.Island` form</span><span class="go-only">its props declared as a Go struct</span>).
- HMR off the Vite dev server (rather than a rebuild per change) is planned.
