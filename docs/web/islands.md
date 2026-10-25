# Islands

[[toc]]

## Introduction

A page shape renders HTML on the server. An island is a browser component
mounted into part of that page: its JavaScript loads according to its hydration
strategy while the rest of the document stays server-rendered.

You reach for an island when part of a page needs a component from a browser
framework — a chart, a date picker, a rich table. Use it for the interactive
part, not as a second way to author the surrounding page.

## Importing a Component {#import-a-component}

Import `.vue`, `.tsx`/`.jsx`, `.svelte` or `.ts`/`.js` component files in the
`<script lang="go">` of a `.vuka.html` shape. The compiler reads their props,
generates the Go types and checks the tag without requiring handwritten props
in the common case. Keep the one rendering struct in that same script.

Save as `pages/planner.vuka.html` (the Vue component is shown below):

```html
<script lang="go">
package pages

import (
    "github.com/vuka-lang/ui/v2"
    Week "../frontend/islands/Week.vue"
)

type Planner struct {
    days []string
    booked []string
    picked ui.State[string]
}

func (p *Planner) Book(day string) { p.picked = day }
</script>

<section>
  <Week days={days} booked={booked} picked={picked} onBook={Book} client:load />
</section>
```

```vue
<script setup lang="ts">
defineProps<{ days: string[]; booked: string[]; onBook?: (day: string) => void }>()
const picked = defineModel<string>('picked')
</script>
```

### Import Rules

An island import may use a package alias, just like a Go import. The path is
relative to the shape file when it starts with `./` or `../`. Use `@/` to
import from the main module root (the directory holding `go.mod`), so a page
can move between folders without changing its component imports. For example, `pages/admin/users/edit.vuka.html`:

```html
<script lang="go">
package users

import Form "@/web/islands/Form.vue"

type Edit struct{}
</script>

<main><Form title="Edit user" client:load /></main>
```

The rule applies to `.vuka.html` and `.vuka` imports. `@/` requires a known
main module root; paths escaping it, such as `@/../Form.vue`, are rejected.
It does not read `tsconfig.json` path mappings.

Its package clause, imports, struct, hooks and
methods precede the first markup line; do not place an HTML filename comment
above the package clause. The imported tag needs no wrapper except when a
fallback is required.

### Other Frameworks

The following complete shape imports React, Svelte and vanilla components:

```html
<script lang="go">
package pages

import (
    Clock "../frontend/clock.tsx"
    Picker "../frontend/picker.svelte"
    Sparkline "../frontend/sparkline.ts"
)

type Overview struct {
    Zone string
    points []float64
}
</script>

<main>
  <Clock zone={Zone} client:load />
  <Picker client:idle />
  <Sparkline points={points} client:visible />
</main>
```

### Where Props Are Read From

The components must declare the props used on their tags. A wrong type
(`days="mon"` when Days is `[]string`), an unknown attribute, or a string
literal outside a declared union is a compile error.

Each framework declares them its own way:

| framework | props read from |
|---|---|
| Vue | `defineProps<{…}>()` / `defineProps<Props>()`, `withDefaults(…, {…})` and destructuring defaults (optional), `defineModel<T>('name')` (two-way), `defineEmits<{ select: [day: string] }>()` (function props `onSelect`) |
| React | the default export's typed first parameter: `function X(props: Props)`, `({a, b = 1}: Props)`, `const X: React.FC<Props>`, `forwardRef<El, Props>`, `memo` |
| Svelte | `let { a, b = 1, value = $bindable() }: Props = $props()`; Svelte 4's `export let a: T` |
| vanilla | `export default island<Props>((el, props, ctx) => …)` (from `vuka:island`), or `export function mount(el, props: Props, ctx)` |

`Props` may be an interface or type of the same file, or one imported from a
relative path (followed one level).

### TypeScript to Go

Prop types map to Go like this:

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

### Optional and Unsupported Types

An optional prop (`?`, a default) or a nullable one (`| null`, `| undefined`)
is a pointer with `omitempty`: left out, the component's own default
applies; a value written on the tag is passed as it is (the compiler takes
its address). Slices, maps, `any` and functions are nil already.

Anything else — `Map`, a tuple, a mixed union — is a compile error naming the
prop and asking for the explicit form below. Generated types are named after
the island: `days: { label: string }[]` on `Week` is `[]WeekDays`, an
interface `Point` is `WeekPoint`.

## Declaring Props in Go {#declare-the-props-in-go}

When frontend types are out of reach, declare the island's prop struct in a
`.go` or `.vuka` **logic file** and register it with `ui.RegisterIsland[T]()`.
This struct describes the external component; it is not an application render
struct and does not replace the page's shape file. An explicit prop type wins
over an inferred import of the same name. This logic file declares and
registers a chart's props:

```go
// components/chart.go
package components

import "github.com/vuka-lang/ui/v2"

type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

type Chart struct {
    Series []Point `json:"series"`
    Title string `json:"title"`
}

func init() { ui.RegisterIsland[Chart]() }
```

### Finding the Component

A bare registration finds the component first beside the declaring file,
`<snake>.<ext>` (`sales_chart.tsx` for `SalesChart`), then
`islands/<Type>.<ext>` under the frontend root. For a type under
`modules/sales/`, the inferred component is under the frontend's
`islands/sales/`. Supported extensions determine the adapter. No matching
file, or two candidates such as Chart.vue and Chart.tsx, is an error in the
build and editor. Scaffolding puts files where inference looks.

Pass a file path when the component lives elsewhere:
`ui.RegisterIsland[Chart]("charts/Revenue.vue")` resolves from the frontend
root; a `./` or `../` path resolves from the declaring file. Registration can
also set the default strategy: `ui.RegisterIsland[Chart](ui.Strategy("load"))`
or `ui.RegisterIsland[Chart]("charts/Revenue.vue", ui.Strategy("idle"))`.
A `client:*` directive on a tag overrides the default.

Use the registered type as a tag from a page shape:

```html
<script lang="go">
package pages

import "example.com/app/components"

type Revenue struct { sales []components.Point }
</script>

<main><components.Chart series={sales} title="Revenue" client:load /></main>
```

## Placing an Island in a Page {#place-it-in-a-page}

A bare component tag mounts the island. Attributes bind its props, and content
between its tags is its slot or children. A fallback is distinct from those
children: it is the pre-hydration view. This page gives a chart a
fallback that holds its height:

```html
<script lang="go">
package pages

import (
    "github.com/vuka-lang/ui/v2"
    Chart "../frontend/chart.tsx"
)

type Report struct { sales []float64 }
</script>

<main>
  <ui.Island>
    <ui.Template fallback><p style="min-height: 12rem">Loading chart…</p></ui.Template>
    <Chart points={sales} client:visible />
  </ui.Island>
</main>
```

### Hydration Strategies {#hydration-strategies}

A `client:*` directive picks when an island hydrates:

| Directive | When it hydrates |
|---|---|
| `client:load` | Immediately on load |
| `client:idle` | On requestIdleCallback |
| `client:visible` | When the host intersects the viewport; the default |
| `client:media={"(max-width: 600px)"}` | When the media query matches |
| `client:only` | No server render; mount fresh in the browser |

> [!WARNING]
> A zero-height host with no visible fallback may never intersect and therefore
> never hydrate under `client:visible`. Give the fallback height or use
> `client:load`.

Framework slots do not implicitly become fallback content.

## Reactivity

Props can flow from the server to an island, from the island back to the
server, or both ways through browser state.

### Live Props (Server to Island) {#live-props-server-island}

On a live page, changed props reach the mounted component through its adapter's
update operation without remounting or losing local state. Unchanged props do
not trigger an update. `once` keeps the values supplied on the first render:

```html
<script lang="go">
package pages

import Chart "../frontend/chart.tsx"

type Comparison struct { sales []float64 }

func (p *Comparison) Clear() { p.sales = nil }
</script>

<section>
  <Chart points={sales} client:load />
  <Chart points={sales} once client:load />
  <button onClick={Clear}>Clear current series</button>
</section>
```

The first chart follows sales; the second keeps its initial series. Tracked
server fields are serialized as their values. An island owns its hydrated DOM;
the server patches the host's props rather than replacing that DOM.

### Function Props (Island to Server) {#function-props-island-server}

A function prop takes a live page method. Calling it in the browser sends its
first argument as the server handler's payload, checked against the declared
function type. Other handler parameters, such as context and services, are
filled by type. Planner.Book in the first example receives a day string.

Vue defineEmits produces `on<Emit>` function props. React and Svelte receive
the callback as a prop. Browser-only callbacks may instead run JS commands;
when writing browser state from a shape's own event attribute use assignment,
for example `onClick={open = false}`.

> [!WARNING]
> Every protected server callback must validate its payload and authorize the
> action.

### State Props (Two-Way) {#state-props-two-way}

A `ui.State[T]` prop shares browser state with an island. It reads the current
value and follows writes made elsewhere on the page; its own writes update the
same state and all bindings that observe it. Here the page and a `Week` island
share the `picked` state:

```html
<script lang="go">
package pages

import (
    "github.com/vuka-lang/ui/v2"
    Week "../frontend/islands/Week.vue"
)

type Selection struct {
    days []string
    booked []string
    picked ui.State[string]
}
</script>

<section>
  <button onClick={picked = "mon"}>Monday</button>
  <b>{picked}</b>
  <Week days={days} booked={booked} picked={picked} />
</section>
```

Vue writes back through defineModel's `update:picked`; React through the
adapter's setPicked/onPickedChange prop; Svelte through `$bindable`. An
expression derived from states is passed one way. The state type must fit the
prop: passing `ui.State[int]` to a boolean prop is a compile error.

### Emits

Vue `on<Emit>` attributes can bind declared emits to server methods. A JSON
object payload binds to a handler's form/JSON struct. Keep payload DTOs in logic
files. A DataTable declaring rowClick and delete emits can receive
`onRowClick={Open}` and `onDelete={Remove}` in the page's island tag. A function
prop carries one payload, its first argument, rather than several independent
server arguments.

## Pages Backed by a Component {#pages-backed-by-a-component}

An island-only page is an ordinary `.vuka.html` page whose markup is just the
island tag. Its `<script lang="go">` owns the page's state, decorated hooks and server
methods. Its URL comes from its filename under `pages/`; document and shared
layout come from `_layout.vuka.html`. The generated island-page wrapper is
compiler output, not another authoring API.

The order and filter types and database service below live in logic files
(`example.com/app/models` and `example.com/app/services`). Save the page as
`pages/orders.vuka.html`:

```html
<script lang="go">
package pages

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
    OrdersView "../frontend/islands/Orders.vue"
)

type Orders struct {
    ui.Live
    Q string @ui.Bind @ui.URL("q")
    Status string @ui.Bind{Debounce: ui.NoDebounce}
    Rows []models.Order
    db *services.DB
    page int
}

@ui.Mount
func (o *Orders) mount(ctx context.Context) error { return o.load(ctx) }

@ui.OnChange
func (o *Orders) changed(ctx context.Context, c ui.Changed) error {
    if c.Has("status") { o.page = 1 }
    return o.load(ctx)
}

func (o *Orders) load(ctx context.Context) error {
    rows, _, err := o.db.Orders(ctx, o.Q, o.page)
    o.Rows = rows
    return err
}

func (o *Orders) Cancel(ctx context.Context, id int64) error { return o.db.Cancel(ctx, id) }
</script>

<OrdersView q={Q} status={Status} rows={Rows} onCancel={Cancel} client:load />
```

Register services with `app.Service(services.NewDB)` and start the app with
`web.New(pages.Routes(), …)`. Use a guard for protected subtrees. The parent
shape is live because of its hooks and handlers; an island with browser-only
behavior inside a static page does not by itself create a server session.

### The Live Bridge

The component can use normal framework props and emits. The live-page bridge
also exposes field/method handles through `vuka:live`; this is the behavior of
the compiler-generated page wrapper, not a manually written render method. This
Vue component reads and calls the page through it:

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

### Field and Method Handles

Exported fields are read handles named after their JSON name with the initial
letter lower-cased (`Q` → live.q). Provided dependencies and unexported state
remain server-side. `get()` and `value` read a handle; bound fields additionally
support `set(v)` and value assignment. A bound value changes immediately in the
browser and is sent after its debounce (300ms by default); NoDebounce disables
the wait and `set(v, { now: true })` flushes immediately. Several sets within
one pause travel as one update. The decorated OnChange hook runs after binding.

Read-only field writes and unknown methods are refused. Server methods return
promises; a method exposes pending and error, the page bridge exposes aggregate
pending/error state, and failures reject the promise with safe status/message
information. Unexported helpers and loader methods are not callable from the
browser. Use ordinary framework props outside a live-page wrapper.

### Call Order and Errors

Calls and sets run in order, one at a time; a method waits for preceding
sets. Errors carry `{status, message, fields}`, including field errors from
`web.Invalid`. A `web.Redirect` navigates. Writing a read-only field returns
403. A field and method cannot share a bridge name; that is a startup error.
Lifecycle hooks and the reserved `Render`, `Update` and `Info` methods are
not browser-callable.

### URL Fields and Updates

Fields marked `@ui.URL("name")` read the initial query and update the address
with `history.replaceState` when they change. Deferred fields remain pending
until their decorated loader completes. Live calls change the existing server
instance and deliver new props without remounting. Broadcast subscriptions,
`Update` and `PushJS` work as on other live views; an `Update` accepting
`web.URLParams` receives a patched URL and decides what state follows from it.

### The Stateless Transport

The stateless island transport uses signed POSTs instead of a connection.
Its snapshot is bound to the page, visitor and expiry with `web.LiveKey`;
altered snapshots and cross-origin calls are refused. The server reconstructs
the instance, injects dependencies, runs Mount, then applies the set and
OnChange or calls the method, returning freshly signed props. Route guards
apply to these requests too. This transport behavior does not change shape
liveness inference: hooks and server event handlers make a shape live.

### The Visitor and Navigation

Every island can read the visitor through `useVuka()`: Vue also exposes
`$vuka` in templates, React uses the hook, and Svelte exposes a store
(`$vuka.user`). The value contains `{user: {id}, roles, perms, can(perm)}`
from the signed-in identity. `live.navigate(href)` and `live.patch(href)`, or
the same methods on `useVuka()`, perform navigation; `<VukaLink href>` provides
a link. Navigation unmounts the old page's islands. See
[island navigation](/web/navigation#from-an-island).

### React and Svelte

React and Svelte read the same handles. In React:

```tsx
const live = useLive<Orders>()
<input value={live.q.value} onChange={(e) => live.q.set(e.target.value)} />
```

In Svelte:

```svelte
<script lang="ts">
  import { live } from 'vuka:live'
  import type { Orders } from 'vuka:live'
  const l = live<Orders>()
</script>
<input bind:value={l.q.value} />
{#if $l.pending}…{/if}
```

Fields also arrive as ordinary framework props, so Vue can use
`defineProps<Orders>()`, React can read its props, and Svelte can use `$props()`.

`vuka:live` supports React's `useLive<T>()` and Svelte's `live<T>()` store as well.
Generated `vuka-live.d.ts` supplies names and types. In the island root's
`tsconfig.json`:

```json
{ "compilerOptions": { "paths": { "vuka:live": ["./vuka-live"] } } }
```

### Layouts

An island-only page uses its folder's `_layout.vuka.html`, including nested
layouts and their Load methods. A layout receives Children and reads
`ui.PageTitle(ctx)` for the title. A page's Title method is evaluated from bound
URL fields before its mount hook for a live page, or after binding for a static
page. Shared layout elements can survive live navigation, preserving such
browser state as a sidebar's scroll position. An embedded island remains a
child of its surrounding shape; it does not install a separate page layout.

### Validation

`validate:` tags on a method's input structs, and the `validate` attributes of
a page's bound fields, are checked on the server before anything runs,
live and stateless alike:

```go
// models/filter.go
package models

type Filter struct {
    Years int `json:"years" validate:"min=1,max=40"`
    Level Level `json:"level"`
}

type Level struct { Code string `json:"code" validate:"required,len=3"` }
```

The rules: `required` (not the zero value), `omitempty` (skip the rest when
it is), `min=n`, `max=n` and `len=n` (a string's characters, a number's
value, a slice's or map's items), `oneof=a b c` and `email`, comma
separated; the attributes of package `ui/validate` are the same rules
(`@validate.Required`, `@validate.Max(50)`, `@validate.OneOf{"a", "b"}`).
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
  other refusal (a field without `@ui.Bind`) puts the server's value back.

```vue
<input v-model="live.q.value" />
<p v-if="live.q.error">{{ live.q.error }}</p>
```

A `web.Invalid` returned by a method or by the OnChange hook reaches the
component the same way.

### Which Fields Changed {#which-fields-changed}

Several field sets within one pause arrive as one update. The decorated
OnChange hook receives `ui.Changed`; Orders.changed above resets its page when
Status changes. `changed.Names()` returns sorted wire names (JSON name, else Go
name). Has accepts the wire name, handle name or Go name: `Has("Q")` and
`Has("q")` both recognize Q. Validation runs before applying the set, so a
failed update does not invoke the hook.

### Deferred Fields {#deferred-fields}

A field marked `@ui.Defer("group")` is absent from the first render and arrives
when the group's decorated loader finishes. Keep bound and URL-backed fields
out of deferred groups. In `pages/overview.vuka.html`:

```html
<script lang="go">
package pages

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
    OverviewView "../frontend/islands/Overview.vue"
)

type Overview struct {
    ui.Live
    Q string @ui.Bind @ui.URL("q")
    Stats models.OrderStats `json:"stats"` @ui.Defer("stats")
    Rows []models.Order `json:"rows"` @ui.Defer("lists")
    Total int `json:"total"` @ui.Defer("lists")
    db *services.DB
}

@ui.Loads("stats")
func (o *Overview) loadStats(ctx context.Context) error {
    stats, err := o.db.Stats(ctx)
    o.Stats = stats
    return err
}

@ui.Loads("lists")
func (o *Overview) loadLists(ctx context.Context) error {
    rows, total, err := o.db.Orders(ctx, o.Q, 1)
    o.Rows, o.Total = rows, total
    return err
}
</script>

<OverviewView q={Q} stats={Stats} rows={Rows} total={Total} client:load />
```

- **Groups and loaders.** `@ui.Defer("lists")` puts the field in the group
  `lists`. Each group has one loader, the method marked `@ui.Loads("lists")`,
  commonly a `func(ctx context.Context) error`. A missing loader stops the app at start (E0324), as does a
  deferred field the component binds or the URL keeps (E0323). A loader
  isn't on `live`: the component can't call it.
- **When.** On a live page the loaders run once the page has joined, each
  group on its own goroutine, and each group's fields reach the component
  as an update when its loader returns — the fast ones don't wait for the
  slow. Their context ends when the visitor leaves the page or the
  connection closes. On a stateless page the component, once mounted,
  posts one load (signed and gated as its calls are); the server runs
  the mount hook and the loaders, concurrently, and answers with every group at
  once. Calls the component makes meanwhile wait for it.
- **A copy.** A loader runs on a copy of the page taken when it starts, so
  events keep running beside it. Only its group's fields are taken from the
  copy: set those; anything else it changes is dropped. Read what the mount hook
  set and what the URL bound (`o.Q`) freely.
- **In the component** a deferred field's handle has `pending` and `error`:
  `live.stats.value` is `undefined` (and `pending` true) until its group
  arrives; if its loader fails, `pending` turns false and `error` is
  `{status, message}` — a `web.Error`'s message, or `"error"` (the cause
  is logged; under `vuka run`, its text). The page itself is fine: the
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

## Building Islands {#build}

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

### Checking and Shipping

`vuka doctor` lists each island with what builds it — the in-process
esbuild, the project's Vite, or Node — and what is missing for it.

`vuka build` compiles the islands into the binary; `vuka build` + `go build`
ship one binary. In development, `vuka run` rebuilds the islands when a file
their build reads changes: the island's source and what it imports, and with a
Vite frontend anything under its root (`vite.config`, `app.css`, shared
components, `_setup`, `package.json`) and the directories `app.css` names
with `@source` — not `node_modules`, `dist` or `.d.ts` files.

### Scaffolding a Frontend {#scaffold}

Scaffold a new app with islands:

```sh
vuka new myapp --islands
```

The command scaffolds the frontend root as a Vite + Nuxt UI project (the config above, the
dependencies, an `app.css`, and a sample island) so a Nuxt UI component renders
immediately.

### An Optional Per-App Setup {#an-optional-per-app-setup}

A `_setup.ts` (default export `(app) => {}`) in the frontend root runs once per
island app — register a Vue plugin, Pinia, or global components there.

## How It Works {#how-it-works}

An island tag renders a self-describing host element the client loader
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
state (`"x"`: a browser-state expression). Function calls and emits travel
back as ordinary live events. A page's deferred fields are left out of
`data-vk-props` until loaded; `data-vk-defer` lists those still pending
(`p`) and the failed ones' errors (`e`). See [Live Pages](/web/live) for the live-view model.

## Limits

Keep these in mind:

- A `.vue` island must sit under the frontend root (so its `vue`/library imports
  resolve); a vanilla `.ts` island may live anywhere.
- A `.css` imported inside an SFC can't be island-bundled — import the
  component's JS and provide its CSS through the app's stylesheet.
- A function prop calls its handler with one payload, its first argument.
- An imported component's props must be typed (a `.jsx` or untyped Svelte
  component needs an explicit Go prop struct in a logic file).
- HMR off the Vite dev server (rather than a rebuild per change) is planned.
