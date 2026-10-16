# Islands

A Vuka page renders to HTML on the server and ships no JavaScript. An **island**
is the exception: a client component — a framework component (Vue) or a vanilla
module — mounted into a server-rendered page and brought to life in the browser.
The page stays static, a few interactive spots hydrate on their own, and
everything else costs zero JS.

## Import a component

The simplest island is a component file imported into a `.vuka` file. The
compiler reads the component's props from the file — no Node, no Go to
write — and generates their Go type, so the tag is type-checked like any
component's:

```vuka
import (
    "github.com/vuka-lang/ui"

    Week "./frontend/islands/Week.vue"
)

func (p *Planner) Render() ui.Node {
    return <Week days={p.days} booked={p.booked} picked={p.picked} onBook={p.Book} client:load />
}
```

```vue
<script setup lang="ts">
defineProps<{ days: string[]; booked: string[]; onBook?: (day: string) => void }>()
const picked = defineModel<string>('picked')
</script>
```

`days="mon"` is a compile error (`days` is a `[]string`), as is an unknown
attribute or a literal a string union doesn't allow. The tag needs no
`<ui.Island>` wrapper; add one only for a fallback (below).

Any framework the file's extension names works — `.vue`, `.tsx`/`.jsx`
(React), `.svelte`, `.ts`/`.js` (vanilla) — with the props declared the way
that framework declares them:

| framework | props read from |
|---|---|
| Vue | `defineProps<{…}>()` / `defineProps<Props>()`, `withDefaults(…, {…})` and destructuring defaults (optional), `defineModel<T>('name')` (two-way), `defineEmits<{ select: [day: string] }>()` (function props `onSelect`) |
| React | the default export's typed first parameter: `function X(props: Props)`, `({a, b = 1}: Props)`, `const X: React.FC<Props>`, `forwardRef<El, Props>`, `memo` |
| Svelte | `let { a, b = 1, value = $bindable() }: Props = $props()`; Svelte 4's `export let a: T` |
| vanilla | `export function mount(el, props: Props, ctx)` |

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
struct annotated `@ui.Island`. Its exported fields are the props; it wins
over an import of the same name:

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

Written bare, `@ui.Island` finds the component by the type's name:
`islands/<Type>.<ext>` under the frontend root (`web.FrontendRoot("web")`:
here `web/islands/Chart.vue`), in the module's own folder for a package
under `modules/<m>/` — `@ui.Island` on `Orders` in `modules/sales/` is
`web/islands/sales/Orders.vue`. The extension is whichever of `.vue`,
`.tsx`, `.jsx`, `.svelte`, `.ts`, `.js` exists, and gives the framework.
No such file, or two (`Chart.vue` and `Chart.tsx`), is an error naming
them, in the build and in the editor. `vuka add island`, `vuka add page
--island` and `vuka new` put the component where the bare form looks.

To keep the inference and set the default hydration strategy (used when a
tag has no `client:*` directive), give an empty path: `@ui.Island("",
"load")`.

Name the file when it is elsewhere: `@ui.Island("charts/Revenue.vue")` is
read from the frontend root, whichever package declares the struct; a path
starting with `./` or `../`, or a file beside the declaring one, is read from
there instead.

## Place it in a page

A bare tag mounts it; a `client:*` directive says *when* it hydrates. To show
something until then, wrap it in `<ui.Island>` with a `<ui.Template fallback>`:

```vuka
<ui.Island>
  <ui.Template fallback><p>Loading…</p></ui.Template>
  <Chart series={p.sales} title="Revenue" client:visible/>
</ui.Island>
```

The component's attributes bind to the struct fields (type-checked by Go), its
children become the component's slot, and the fallback is distinct from the
slot — it's the pre-hydration view.

### Hydration strategies

| directive | when it mounts |
|---|---|
| `client:load` | immediately on load |
| `client:idle` | on `requestIdleCallback` |
| `client:visible` | when it scrolls into view (`IntersectionObserver`) — the default |
| `client:media={"(max-width: 600px)"}` | when the media query matches |
| `client:only` | never server-rendered; mount fresh on the client |

An island with no visible fallback and `client:visible` never hydrates (a
zero-height host never intersects) — give it a fallback with height, or use
`client:load`.

## Reactivity

### Live props (server → island)

On a live page every prop is live: when a re-render changes the island's
props, the mounted component gets them through its adapter's `update` — no
remount, no lost local state. Props that didn't change cause no update.
`once` on the tag keeps the props it was first given:

```vuka
<Chart series={p.sales} client:load />        // follows p.sales
<Chart series={p.sales} once client:load />   // the first series only
```

A `*ui.Assign[T]` field of an `@ui.Island` struct is sent as its value.

### Function props (island → server)

A prop whose type is a function takes a live view method. Calling the
function in the browser pushes the method, its first argument as the
method's parameter (checked against the function's type at compile time):

```vuka
func (p *Planner) Book(day string) { … }

<Week onBook={p.Book} />            // onBook?: (day: string) => void
```

It takes JS commands too, run in the browser when the function is called:
`<Dialog onClose={open.Set(false)} />`. In Vue the function is the listener
of the matching emit (`defineEmits<{ close: [] }>()` gives `onClose`); in
React and Svelte it is the prop itself.

### State props (two-way)

A [`ui.State`](/ui/state) passed as a prop is shared with the component:
the component gets its value, follows it when the page (or another island)
changes it, and its writes set the state — and every binding on the page
that reads it:

```vuka
picked := ui.State("mon")
<b>{picked}</b>
<Week picked={picked} />
```

The component writes back by its framework's convention: a Vue
`defineModel` (`update:picked`), a React `setPicked` / `onPickedChange` prop
(the adapter passes them), a Svelte `$bindable` prop. A `ui.Derived`, or an
expression of states, is passed one way. The state's type must fit the
prop's (`ui.State[int]` for a `boolean` prop is a compile error).

### Emits

Without a declared prop, `on<Emit>` attributes bind a Vue component's emits
to live-view handlers; the emit's argument arrives as JSON, bound into the
handler's struct parameter:

```vuka
<ui.Island>
  <DataTable rows={p.rows} onRowClick={p.Open} onDelete={p.Remove} client:load/>
</ui.Island>
```

## Pages backed by a component

A page can be a component: put `@web.Get` on an `@ui.Island` struct, and
the page's template is the component file (here `web/islands/Orders.vue`). The struct says what the page
holds; the component reads it, sets what it may and calls the rest.

```vuka
@web.Get("/orders")
@ui.Island
@auth.Required
type Orders struct {
    ui.Live                                         // optional: a live page; leave it out for a stateless one
    Q      string  `url:"q" bind:"q"`               // the component sets it; kept in the address's ?q=
    Status string  `bind:"status,debounce=0"`      // set at once, no wait
    Rows   []Order                                  // read only
    db     *DB                                      // the server's own: injected, never sent
}

func (o *Orders) Mount(ctx context.Context) error   { return o.load(ctx) }
func (o *Orders) Changed(ctx context.Context) error { return o.load(ctx) } // after the component sets Q or Status
func (o *Orders) Cancel(ctx context.Context, id int64) error { … }       // callable from the component
```

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
- **`bind:"name"` fields are the ones the component sets**:
  `live.q.set(v)`, or `live.q.value = v` (so `v-model="live.q.value"`).
  The value is the browser's at once; the set is sent after a pause
  (300ms, `bind:"q,debounce=500"` to change it; `set(v, { now: true })`
  sends it straight away), and the sets of several fields within one pause
  go as one update. The server sets the fields, runs `Changed` if the type
  has one — `Changed()` or `Changed(ctx)`, returning nothing or an error;
  its dependencies are the struct's injected fields, as for the other
  methods — and answers with the props. A field without `bind:` has no
  `set` and its `value` is read only, in the types and on the server, which
  refuses a set of it (403, as `live.error`).
- **Exported methods are what the component calls**: `live.cancel(id)`
  returns a promise; the arguments are JSON, checked against the method's
  Go parameters on the server and typed in `vuka:live`. A method has its own
  `live.cancel.pending` and `live.cancel.error`; `live.pending` and
  `live.error` are the page's (any call). An error is `{status, message,
  fields}`, `fields` from a `web.Invalid{"q": "…"}`. A `web.Redirect`
  navigates. Calls and sets go one at a time, in order; a method waits for
  the sets before it. `Render`, `Mount`, `Update`, `Info` and `Changed`
  aren't callable, and a method and a field can't share a name on `live`
  (the app doesn't start).
- **`url:"name"` fields** are filled from the query when the page loads,
  and when they change the address follows (`history.replaceState`), so a
  reload or a shared link shows the same page.
- **Live** (`ui.Live` embedded): the struct is the live view, its Render the
  island (the compiler writes it). A set or a call is a live event on the
  server's instance; the fields it changes reach the component, with no
  remount. Mount, `Subscribe` and `Update` (broadcasts), `PushJS` and the
  type's gates work as for any live view.
- **Stateless** (no `ui.Live`): no connection. A set or a call posts the
  fields or the method with its arguments, and the props the page was
  rendered with — signed with the app's key (`web.LiveKey`), bound to the
  page, the visitor's sign-in and an expiry — to the page's URL; the server
  builds the struct again from them, injects its dependencies, runs Mount,
  then the set and `Changed` or the method, and answers with the new props,
  signed again. Changed props are refused. Calls are refused cross-origin
  and pass the type's gates (`@auth.Required`…) like the page.
- **The visitor** is known to every island's component: `$vuka` in Vue
  templates (`useVuka()` in script), `useVuka()` in React, and
  `const vuka = useVuka()` (a store, `$vuka.user`) in Svelte —
  `{user: {id}, roles, perms, can(perm)}` from the signed-in identity
  (`web/auth`).

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

`App.Page(path, &T{}, attrs…)` declares one from Go.

## Build

Island components live under the **frontend root** — the directory
[`web.FrontendRoot`](/web/frontend) names (the one holding `package.json` and
`node_modules`), or `web.IslandRoot("web")` for an app with no frontend of
its own. How they're bundled depends on the framework:

- **Vanilla `.ts`/`.js`** are bundled by the `vuka` command with esbuild — no
  frontend toolchain, the module exports `mount(el, props, ctx)`.
- **Vue, React and Svelte** build with Node esbuild (and
  `esbuild-plugin-vue3` / `esbuild-svelte`) from the frontend root's
  `node_modules` — `npm i -D esbuild vue esbuild-plugin-vue3`, `react
  react-dom`, or `svelte esbuild-svelte`.
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

`vuka build` compiles the islands into the binary; `vuka build` + `go build`
ship one binary. In development, `vuka run` rebuilds an island when its source
changes.

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

`<ui.Island>` renders a self-describing host element the client loader
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
back as ordinary live events. See [Live components](/web/live) for the live-view model.

## Limits

- A `.vue` island must sit under the frontend root (so its `vue`/library imports
  resolve); a vanilla `.ts` island may live anywhere.
- A `.css` imported inside an SFC can't be island-bundled — import the
  component's JS and provide its CSS through the app's stylesheet.
- A function prop calls its handler with one payload, its first argument.
- An imported component's props must be typed (a `.jsx` or untyped Svelte
  component needs the `@ui.Island` form).
- HMR off the Vite dev server (rather than a rebuild per change) is planned.
