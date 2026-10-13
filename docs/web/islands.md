# Islands

A Vuka page renders to HTML on the server and ships no JavaScript. An **island**
is the exception: a client component — a framework component (Vue) or a vanilla
module — mounted into a server-rendered page and brought to life in the browser.
It's the Astro model: the page stays static, a few interactive spots hydrate on
their own, and everything else costs zero JS.

## Declare an island

An island is a Go struct annotated `@ui.Island`, pointing at the component file.
Its exported fields are the props — Go owns the contract, so there's nothing to
restate in the component:

```vuka
type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

@ui.Island("web/islands/Chart.vue")
type Chart struct {
    Series []Point `json:"series"`
    Title  string  `json:"title"`
}
```

The framework is inferred from the file extension — `.vue`, `.tsx`/`.jsx`
(React, later), `.svelte` (later), or `.ts`/`.js` (vanilla). The component file
lives under the frontend root (see [Build](#build)).

## Place it in a page

Wrap the component tag in `<ui.Island>`. A `client:*` directive says *when* it
hydrates; a `<ui.Template fallback>` is shown until then:

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

A prop typed `*ui.Assign[T]` stays live: when a live event changes it, the
server re-render pushes the new value to the mounted component in place — no
remount, no lost local state.

```vuka
@ui.Island("web/islands/Chart.vue")
type Chart struct {
    Series *ui.Assign[[]Point] `json:"series"` // live — updates in place
    Title  string              `json:"title"`   // one-shot
}
```

### Emits (island → server)

Bind a component's emits to live-view handlers with `on<Emit>` attributes, like
an element's `onClick`. The emit's argument arrives as JSON, bound into the
handler's struct parameter:

```vuka
<ui.Island>
  <DataTable rows={live(p.rows)} onRowClick={p.Open} onDelete={p.Remove} client:load/>
</ui.Island>
```

```vuka
func (p *Page) Open(ctx context.Context, row Row) error   { … }
func (p *Page) Remove(ctx context.Context, row Row) error { … }
```

Each binding is a registered handler (an opaque id, like any `onClick`), so
there's no name-based dispatch to whitelist. A Vue component's declared emits
(`row-click`, `delete`, …) are forwarded automatically; the name matches the
`on<Emit>` ignoring case and dashes.

## Build

Island components live under the **frontend root** — the directory
[`web.FrontendRoot`](/web/frontend) names (the one holding `package.json` and
`node_modules`). How they're bundled depends on the framework:

- **Vanilla `.ts`/`.js`** are bundled by the `vuka` command with esbuild — no
  frontend toolchain, the module exports `mount(el, props, ctx)`.
- **Vue** builds through the project's **Vite** when the frontend root has a
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
`update()` — while leaving its client-owned DOM untouched. Emits travel back as
ordinary live events. See [Live components](/web/live) for the live-view model.

## Limits

- A `.vue` island must sit under the frontend root (so its `vue`/library imports
  resolve); a vanilla `.ts` island may live anywhere.
- A `.css` imported inside an SFC can't be island-bundled — import the
  component's JS and provide its CSS through the app's stylesheet.
- React and Svelte adapters, and HMR off the Vite dev server (rather than a
  rebuild per change), are planned.
