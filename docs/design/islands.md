# UI Islands — locked design

Astro-style client islands for vuka: server-rendered `.vuka`/JSX pages stay
zero-JS; a component file becomes an interactive island, hydrated on the client
with a `client:*` strategy. The authoring surface is locked below; machinery
reuses the existing `web.Script` loader seam (`data-vk-s` scoping, lazy module
import, lifecycle hooks, SSE dev reload) and esbuild in `vuka build`.

## Declaration — the decorator is the contract

An island is a Go struct type annotated with `@ui.Island(path[, defaultStrategy])`.
**Go owns the props** (no extracting types out of the SFC): exported fields are
the props (json tags = wire names, `validate:` runs server-side), and a
`Children ui.Node` field is the component's slot.

```vuka
@ui.Island("islands/Counter.vue")
type Counter struct {
    Count    int     `json:"count"`
    Children ui.Node
}
```

- Framework inferred from the extension: `.ts`/`.js` (vanilla), `.tsx`/`.jsx`
  (React), `.vue`, `.svelte`.
- Registered name = the Go type name (`Counter`); the path is its source. Two
  islands may share a component file.
- A field typed `*ui.Assign[T]` is a **live prop** (Phase 3): server re-render
  calls the adapter's `update()`, no remount. A plain field is one-shot
  (Astro-style).
- Props are public — boot refuses secret-named fields (as `[runtime.browser]`).

## Placement — bare tag, or a region wrapper for fallback/opts

Simple case — a bare, typed tag (the decorated type is in scope):

```vuka
<Counter client:visible count={5}>
  <small>clicks</small>          {/* the component's slot (Children) */}
</Counter>
```

Rich case — wrap to add a fallback or region options. `<ui.Island>` is the
region container; `<ui.Template …>` holds named region slots distinct from the
component's own slot:

```vuka
<ui.Island>
  <ui.Template fallback><Skeleton/></ui.Template>   {/* shown until hydration */}
  <Counter client:visible count={5}>
    <small>clicks</small>                            {/* the component's slot */}
  </Counter>
</ui.Island>
```

- `<ui.Island>` is **optional**: bare `<Counter …/>` is sugar for a wrapper with
  no templates. Wrap only to add a fallback / `ssr` / region class.
- `<ui.Template>` is meaningful only inside `<ui.Island>`; boolean attrs name the
  slot: `fallback` (initial/until-hydration + SSR target), later `error`,
  `loading`. It renders no element of its own — a slot label the transpiler
  consumes, like Vue `<template #slot>`.
- **Fallback ≠ slot**: the fallback is the pre-hydration view; the component's
  children are its real slot, handed to the framework on mount. Keeping them
  separate is the win over Astro (which reuses children as both).

### `client:*` strategies (transpiler-routed attributes)

| directive | Go | fires |
|---|---|---|
| `client:load` | `ui.Load()` | on load |
| `client:idle` | `ui.Idle()` | `requestIdleCallback` |
| `client:visible` / `client:visible={"200px"}` | `ui.Visible()` | `IntersectionObserver` (default) |
| `client:media={"(max-width:600px)"}` | `ui.Media(q)` | `matchMedia` |
| `client:only` | `ui.ClientOnly()` | never SSR'd |
| `server:defer` | `ui.SSR()` | opt-in sidecar render (Phase 4) |

Default with no directive = `Visible()`. The decorator's optional 2nd arg sets a
default strategy; the tag's `client:*` overrides it.

## Lowering

```vuka
<ui.Island>
  <ui.Template fallback><Skeleton/></ui.Template>
  <Counter client:visible count={5}><small>clicks</small></Counter>
</ui.Island>
// →
ui.Island(Counter{Count: 5},
    ui.Visible(),
    ui.Slot(<small>clicks</small>),
    ui.Fallback(<Skeleton/>),
)
```

Renders the self-describing host (modelled on `<astro-island>`):

```html
<vk-island data-vk-s="i7" data-vk-island="Counter"
  data-vk-module="/_vuka/i/Counter.js" data-vk-adapter="/_vuka/i/vanilla.js"
  data-vk-client="visible" data-vk-props='{"count":5}'>
  <div class="skeleton">…</div>                           <!-- fallback -->
  <template data-vk-slot><small>clicks</small></template> <!-- component slot -->
</vk-island>
```

Props are JSON; `Date`/`Map`/`Set`/`BigInt`/typed arrays use Astro's
`[TYPE, value]` tagging only when a field needs it. `data-vk-s` makes it an
instance root the existing loader recognises (`data-vk-id` inside a live view).

## Client runtime — a branch on the existing loader

For each `[data-vk-island]` root: read `data-vk-client`, arm the strategy
(shared `IntersectionObserver`/idle/media); on fire,
`Promise.all([import(module), import(adapter)])` — both lazy, per island, per
framework; deserialize props, read `<template data-vk-slot>`, call the adapter;
keep the handle; reuse the loader's `updated`/`destroyed` wiring for live patch
and navigation.

**Adapter contract** (one per framework):

```ts
export function mount(Component, el, props, ctx): { update(props): void; unmount(): void }
```

`ctx` is the existing hook ctx — `{ el, push, on, ref }` — so an island reaches
live events with no new API. Vanilla = the file itself exports `mount`. Vue =
`createApp(...).mount(el)`, React = `createRoot`, Svelte = `new Component`.
`_setup.ts` is the per-app hook (Pinia/plugins), the `appEntrypoint` analogue.

## Build & codegen (deco scanner + esbuild)

`@ui.Island` is picked up by the deco scanner like `@orm.Meta`: it generates the
registration + the tag's boundary-render wiring (overlaid in dev/build/test/lsp,
never written to source; `vuka generate islands` ejects it), adds the referenced
file to the esbuild island set → a lazy chunk + its adapter served under
`/_vuka/i/`, recorded in the manifest, and checks every path resolves (dev warn,
build error). `.vue`/`.svelte` pull their esbuild plugin only when present
(opt-in deps). Codegen also emits `islands.d.ts` **from the Go structs** (Go is
the source of truth) so the component is typed against the contract; a drift
check flags a mismatched `defineProps`.

## SSR — client-only default, opt-in sidecar

- **Default (client-only):** ship the `fallback` as initial HTML; the client
  mounts over it. Zero Node at runtime (honours the single Go binary).
- **`ssr` / `server:defer` (opt-in):** `vuka build` emits `dist/ssr/islands.js`
  (Node, framework server renderers); Go POSTs props → HTML placed in the host,
  marked `data-vk-ssr` so the client **hydrates** instead of mounting. Dev and
  live re-renders skip it. Maps to Astro server islands.

## Phase 2 — Vue islands (build approach, locked)

Go's esbuild library can't compile `.vue`, so framework islands add a **Node
build step** (used only when `.vue` islands exist; vanilla stays pure-Go). vuka
uses **Node esbuild + `esbuild-plugin-vue3`** (proven: an SFC compiles to an ESM
chunk whose default export is the component; a Vue adapter bundles `createApp`
mount). It reuses Phase 1 end to end — `/_vuka/i/` serving, `RegisterIsland`,
the manifest, and the loader (which already imports an adapter + the module).

- One esbuild build per app with `splitting: true`, entry points = the Vue
  adapter + every `.vue` island; esbuild emits a shared Vue chunk the entries
  import by relative URL (all served under `/_vuka/i/`). The manifest's
  `adapter` field points at the adapter chunk; the loader calls
  `adapter.mount(module.default, el, props, ctx)`.
- The adapter: `createApp({render:()=>h(Comp, reactiveProps)}).mount(el)`;
  `update` = `Object.assign(state, next)`; `unmount` = `app.unmount()`. An
  optional `_setup.ts` default export runs per app (Pinia/plugins) — the
  appEntrypoint analogue.
- Build deps (`vue`, `esbuild`, `esbuild-plugin-vue3`) come from the frontend
  root's `node_modules` (`web.FrontendRoot`); a server-rendered app without a
  frontend gains a minimal `package.json` there.
- **SFC location (Vue):** `esbuild-plugin-vue3` resolves an SFC's bare imports
  (`vue`, a datepicker lib, …) from the SFC file's own directory upward — not
  from esbuild's working dir — so a `.vue` island must live **under the frontend
  root** (e.g. `web/islands/X.vue`), where `node_modules` is reachable. The
  `@ui.Island("…/X.vue")` path (relative to the declaring Go file) points there.
  Vanilla `.ts` islands import nothing bare, so they may sit anywhere.
- A JS-`import`ed `.css` inside an SFC (e.g. a flatpickr theme) can't be
  consumed by the island loader — import the component's JS and provide its CSS
  through the app's stylesheet instead.

## Phasing

1. **Vanilla islands** — decorator + codegen, `<ui.Island>`/`<ui.Template>` +
   `client:*` lowering, `<vk-island>` host, loader branch + strategies, esbuild
   discovery + manifest + `/_vuka/i/`, vanilla adapter. No framework deps, no
   Node.
2. **Vue + React adapters** + `_setup.ts`; then Svelte.
3. **`live(...)` reactivity** — `*ui.Assign[T]` fields + the `update` path.
4. **`server:defer` SSR sidecar** + hydrate path.
