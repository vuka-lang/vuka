# Releases

Install a release with your own toolchain:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Each release states the **runtime** its generated code needs — the
`github.com/vuka-lang/vuka` package your module requires. `vuka fix runtime`
upgrades it.

## cmd/vuka v0.40.1, web v0.31.2 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use web v0.31.2.

- **A page whose component has fields but no methods has `live`.** A page
  backed by a component with `bind:` and read-only fields and only `Mount`
  and `Changed` gave the component `useLive()` as `null`, so reading
  `live.title` failed in the browser. Every page backed by a component now
  gets its handles, live and stateless alike (web v0.31.2; no change to
  your code or to ui).

- **An island build that fails shows in the browser.** Under `vuka run`
  (and `-watch`) a failed build of the islands — a Vite build of a `.vue`
  component that doesn't compile, say — used to reach the terminal only,
  while the page went on with the last good bundle. The
  [development toolbar](/web/toolbar#while-vuka-run-watch-rebuilds) now
  says `island build failed` (`asset build failed` for styles and scripts)
  with the build's error in its drawer, the end of what Vite said
  included, colours stripped; it clears when a later build succeeds.
- **An island the bundle doesn't have says so in its place.** In
  development, `<Levels … />` whose component isn't in the last good bundle
  renders a box — "Island Levels isn't built", and that the island build
  failed when it did — instead of nothing, and mounts once a build has it,
  without a reload. In production nothing changes: no box, and `vuka build`
  stops on an island that doesn't build.
- The last good bundle keeps serving meanwhile: the other islands work.

## cmd/vuka v0.40.0, web v0.31.0, ui v0.26.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.26.0, web v0.31.0.

- **A page's component reads and sets its fields through `live`.** In a
  page backed by a component, every exported field is a handle on
  `useLive()`: `live.rows.value` (or `get()`) reads it and follows the
  server. A field tagged `bind:"q"` is one the component sets —
  `live.q.set(v)`, or `v-model="live.q.value"` — and the value is the
  browser's at once while the set waits for a pause in typing (300ms;
  `bind:"q,debounce=500"`, `set(v, { now: true })`). Sets of several fields
  within one pause go as one update; the server sets the fields, runs the
  type's `Changed` and answers with the props — over the connection on a
  live page, as the signed round trip on a stateless one. A field without
  `bind:` is read only: no `set` in the types, and the server refuses a set
  of it (403). See [Pages backed by a component](/web/islands#pages-backed-by-a-component).
- **Each method has its own `pending` and `error`** (`live.cancel.pending`),
  beside the page's `live.pending` and `live.error`. Calls and sets go one at
  a time, in order; a method sends the sets waiting first.
- A method and a field of one name on `live`, or a `bind:` tag that doesn't
  fit, stop the app at start with what to rename. `Changed` isn't callable.
- Components reading their props with `defineProps<T>()` (React: props,
  Svelte: `$props()`) and calling `live.method(…)` work as before.
- `vuka-live.d.ts` types the handles (`LiveBound<V>`, `LiveField<V>`) and
  each method's state.
- `vuka add page --island` writes a page whose search field is `bind:` with
  a `Changed`, and a component setting it through `live` (`v-model="live.q.value"`,
  React `value`/`onChange`, Svelte `bind:value`). With `--module m` the
  component goes in `web/islands/m/`, as `vuka add island --module m` does.

## cmd/vuka v0.39.3 — 2026-10-16

Runtime unchanged, v0.16.0.

- **A Vue page backed by a component builds when TypeScript 7 is about.**
  Vue's compiler reads `defineProps<T>()` of a type imported from
  `vuka:live` through TypeScript's JavaScript API, which TypeScript 7
  doesn't have; a UI library depending on `typescript` could hoist 7 into
  the frontend, and the island build failed, leaving the page blank.
  `vuka add island` / `page --island` now pin `typescript` `~6.0.3` beside
  Vue when the frontend has none, and the build's error says what to run.
- **Editors find `vuka:live`.** A frontend without a `tsconfig.json` gets
  one (mapping `vuka:live` to `vuka-live.d.ts`) from `vuka add island` and
  from the build that writes `vuka-live.d.ts`.
- **`@ui.Island` paths from the frontend root.** A path that doesn't start
  with `.` and isn't beside the declaring file is found from the frontend
  root `web.FrontendRoot` names: `@ui.Island("islands/Orders.vue")` from any
  package, instead of `"../../web/islands/Orders.vue"`. A path beside the
  file, or starting with `./` or `../`, reads as before.
- **`vuka add page` makes a live page by default** (`@web.Live`, or
  `ui.Live` in a page backed by a component); `--stateless` makes the
  `@web.Get` one. A path with `{parameters}` stays stateless unless `--live`.
- The Vue component `vuka add page --island` writes binds its search box
  with `v-model` and calls the method from a `watch`.
- `vuka add island` picks the framework the frontend already uses; the help
  now says the flags are for adding one in another.

## cmd/vuka v0.39.2 — 2026-10-16

Runtime unchanged, v0.16.0.

- **`vuka add island` writes the `.vuka` side too.** Besides the component
  under `web/islands/`, it writes a component placing it —
  `components/chart.vuka` with `Chart(ChartProps)` — so a page writes
  `<components.Chart Title="Sales" Count={3} />` instead of importing the
  file itself. `--module m` puts both in the module: `modules/m/chart.vuka`
  and `web/islands/m/Chart.vue`.
- **`--module` for services and entities, and the app's own folders.**
  `vuka add service Mailer --module notes` and `vuka add entity Comment
  --module posts` name the module as the other kinds do (`notes/Mailer`
  still works); without a module they go in `services/` and `entities/`.
- **`vuka add entity --unmanaged`** maps an existing table:
  `@orm.Meta{Table: "…", Unmanaged: true}`, no timestamps, test or
  migration, and the app needs no `migrations/`. (In zsh, quote an optional
  field: `'code:string?'`.)

## web v0.30.1 — 2026-10-16

Security fix. Runtime unchanged, v0.16.0; ui unchanged, v0.25.0. The vuka
command is `cmd/vuka/v0.39.1`. New apps use web v0.30.1.

- **A stateless page's signed props are the visitor's.** A page backed by a
  component without `ui.Live` sends its props to the browser signed, and
  each call carries them back. The signature bound the page's path and an
  expiry but not who the page was rendered for, so anyone holding a page's
  props could replay them — as another user, or signed out — until they
  expired (calls still passed the page's gates). The signature now also
  binds the visitor's sign-in (with `web/auth`: the user and the session,
  token or JWT they signed in with; no one, for an anonymous visitor) and
  the page's type. Props replayed by another visitor, anonymously, after a
  sign-out and in again, or at another page answer like a forged signature
  (403). Pages already open when the app is upgraded are refused once and
  work again after a reload. No change to apps or components:
  `go get github.com/vuka-lang/web@v0.30.1`.
- `web.LiveIdentity` keys a sign-in, not only a user: live adoption, too,
  only adopts a page for the sign-in that rendered it.

## v0.30.0 — 2026-10-16

Runtime: unchanged, v0.16.0. The vuka command is
`cmd/vuka/v0.39.0`. New apps use ui v0.25.0, web v0.30.0.

- **Import a component as an island.** `import Week "./web/islands/Week.vue"`
  in a `.vuka` file makes `<Week …/>` a tag: the compiler reads the props
  from the component's TypeScript — Vue's `defineProps`/`defineModel`/
  `defineEmits`, a React component's typed props, Svelte's `$props()`, a
  vanilla `mount(el, props)` — and type-checks every attribute against them,
  with no Go type to write and no Node to run. `<ui.Island>` is needed only
  for a fallback. React (`.tsx`/`.jsx`) and Svelte islands build beside Vue.
  `vuka lsp` completes the props.
- **Island props both ways.** On a live page every prop follows the server
  (`once` keeps the first value). A function prop takes a method of the page
  — the component calls it, the method runs on the server — or JS commands. A
  `ui.State` passed as a prop is shared: the component writes it back
  through `defineModel`, a React `setX` prop or a Svelte `$bindable`, and
  every binding on the page follows.
- **Pages backed by a component.** `@web.Get` on an `@ui.Island` type serves
  the component as the page: exported fields are its props (`url:"q"` fields
  kept in the address), exported methods what it calls with
  `useLive<T>()` from `vuka:live`, typed from the Go struct in
  `vuka-live.d.ts`. Embedding `ui.Live` makes it a live page; without it a
  call is a POST carrying the page's signed props back, so the page needs no
  connection. Errors arrive as `live.error` (`web.Invalid` for fields), a
  `web.Redirect` navigates, and the type's gates (`@auth.Required`) hold for
  calls as for the page. Every island knows the signed-in visitor
  (`useVuka()`). See [Pages backed by a component](/web/islands#pages-backed-by-a-component).
- **Client state.** A write (`open.Set(false)`), a `ui.Node` or an attribute
  list built from states and passed as a prop or child is passed as it is,
  not read as a state. `ui.BindAttr(name, s)` binds a state into a Go
  attribute list. An `auto` popover the browser closes (Escape, a click
  outside) writes its `when` state back, and `<ui.Show>` without a transition hides
  at once, so swapped panels never show together.
- **`vuka add` makes the pieces of an app:** `service`, `component`,
  `island` (Vue, React or Svelte, with its packages installed), `page`
  (`--live`, `--island`) and `entity` (an orm model with its migration).
  `--dry-run` lists what it would write; `--force` overwrites. See
  [add](/tools/cli#add).
- **Long steps say they are going.** A package install, the Vite island
  build and `vuka new`'s module fetch show a spinner on a terminal and a
  heartbeat line elsewhere; a failure prints the tool's last lines.
- **Faster rebuilds.** What a package was transpiled to, and the API it was
  checked against, is kept on disk (the user's cache directory,
  `vuka/transpile`): `vuka build`, `run`, `test`, `vet` and `gen` transpile
  only the packages whose inputs changed, and `run -watch` starts from that.
  The Vite island build is skipped when nothing it reads changed.
  `VUKA_CACHE=off` turns both off. `vuka run` leaves the files
  `web.Frontend` serves out of the binary (they are read from disk in
  development), so the linker doesn't copy them on every rebuild; `-embed` or
  `VUKA_EMBED_STUB=off` builds them in.

**Upgrading.** `web.Get`, `Post`, `Put`, `Patch`, `Delete` and `Handle` now
return a `vuka.Bundle` (made with the new `vuka.Dual`: a route on a
function, a page on a type) rather than a `func(*vuka.Decl)`. `@web.Get(…)`
in a `.vuka` file is unchanged. Go code that used the result as a function
changes:

```go
web.Get("/pets")(d)                   // before
vuka.DeclareWith(web.Get("/pets"), d) // now

func ApiRoute(p string) func(*vuka.Decl) { return web.Get(p) } // before
func ApiRoute(p string) vuka.Bundle      { return web.Get(p) } // now
```

A composed decorator (`decorator ApiRoute(p string) = @web.Get(p) …`)
needs no change. web v0.30.0 requires vuka v0.30.0:
`go get github.com/vuka-lang/web@v0.30.0` brings it along.

## v0.29.0 — 2026-10-15

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.38.1`. New
apps use ui v0.24.0, web v0.29.0.

- **Client state.** `ui.State(v)` declares a value the browser keeps — a
  menu's `open`, the picked tab, a filter. Commands change it with no round
  trip (`open.Toggle()`, `tab.Set("env")`, `n.Inc()`), and the attributes,
  text and classes that read it follow, written as plain Go expressions:
  `<ul hidden={!open}>`. `ui.Derived(expr)` names a value built from
  states, `ui.Class` toggles classes, `<ui.Show when={open}>` shows and hides
  with transitions, and a struct state's fields are read one by one. The
  server renders the initial values, so the HTML is right before any script
  runs; an expression that can't run in the browser is a compile error
  naming what to compute on the server. See [Client state](/ui/state).
- **`vuka lsp`:** type-aware highlighting (semantic tokens) in
  cmd/vuka v0.37.0, and no flicker of stale diagnostics while typing in
  v0.36.0. cmd/vuka v0.38.1 reports its own version correctly.

## v0.27.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.32.0`. New apps
use ui v0.21.0, web v0.24.0.

- **UI islands (Phase 3 — the live bridge).** Islands are now reactive both
  ways. Server → island: a `*ui.Assign[T]` island prop serializes its value, and
  a live re-render pushes the changed value to the mounted component's
  `update()` with no remount (the live-view morph updates an island host's
  attributes but leaves its client-owned DOM). Island → server: `on<Emit>`
  attributes on the tag bind the component's emits to live-view handlers —
  `<DataTable rows={live(d.rows)} onRowClick={d.RowClick}/>` lowers the handler
  to a registered id (like an element `onClick`, no `AllowPush`), and the client
  adapter forwards the Vue emit to it; the emit's argument arrives as JSON bound
  into the handler's struct parameter (`ui.Event.Data`). Verified end to end:
  an island emit runs a server handler that bumps a live prop, which updates the
  island in place.

## v0.28.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.34.0`. New apps
use ui v0.22.0, web v0.25.0.

- **Stable island `data-vk-s`.** A `<ui.Island>` now lowers to
  `ui.MountIsland("<site>", …)` with a stable per-placement id (the file + the
  wrapper's source offset), used as the host's `data-vk-s`. Previously the host
  got a per-render counter, so every island re-render churned `data-vk-s` in the
  live-view diff (spurious patch weight) and broke `ctx.ref` after an update
  (the captured scope no longer matched). The id is now identical across
  renders. Requires ui v0.22.0 (`MountIsland` gained the leading `site`
  parameter).

## cmd/vuka v0.35.0 — 2026-10-13

Runtime unchanged (vuka v0.28.0); a command-only release.

- **`vuka lsp`: auto-imports consolidate into one block.** Accepting a
  completion (or a decorator / static accessor) that needs an import now folds
  it into a single sorted `import ( … )` block — folding any existing lone
  `import "x"` lines in too — instead of appending another lone `import` line
  each time. (`addImports` now returns a set of edits rather than one insertion.)

## cmd/vuka v0.33.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); new apps use ui v0.21.0, web v0.25.0.

- **Vite island build (for Nuxt UI and other Vite-plugin UI libraries).** When
  a framework island's frontend root is a Vite project (has a `vite.config.*`),
  the vuka command builds the `.vue` islands by running the project's **Vite**
  (with its `[vue(), ui(), tailwindcss(), vuka()]` plugins) instead of the
  Node-esbuild path — so Nuxt UI's components auto-resolve and its Tailwind v4
  CSS is produced. vuka reads Vite's manifest, serves the island + adapter +
  shared chunks at `/_vuka/i/`, and injects the island CSS (`<link>` on first
  hydration). A frontend root without a `vite.config` keeps the esbuild path;
  vanilla `.ts` islands always use esbuild. Needs web v0.25.0 (its Vite plugin
  gains the island-build mode + island-CSS serving).
- **`vuka new --islands` now scaffolds a Vite + Nuxt UI frontend root** by
  default: `vite.config.mts` (`[vue(), ui(), tailwindcss(), vuka()]`), the dep
  set (vite, @vitejs/plugin-vue, @nuxt/ui, @tailwindcss/vite, tailwindcss, vue,
  vue-router), `app.css` (`@import "tailwindcss"; @import "@nuxt/ui";`), and a
  sample island using a Nuxt UI `<UButton>`.
- Dev (`vuka run`) rebuilds Vite islands on change (a full island build, not yet
  HMR off the Vite dev server — a follow-up).

## cmd/vuka v0.31.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); a command-only release.

- **`vuka new --islands`.** Scaffolds a sample Vue island wired end to end: a
  `web/` frontend root (`package.json` with esbuild + esbuild-plugin-vue3 + vue,
  a `web/dist/index.html` stub, `web/islands/Counter.vue`), an `@ui.Island` Go
  type, a live page placing `<ui.Island><Counter … client:visible/></ui.Island>`,
  and `web.Frontend(dist, "web/dist", web.FrontendRoot("web"))` in main. Offered
  for `--ui=live --frontend=none` (refused otherwise, with a clear message);
  default off, so every existing `vuka new` combination is byte-for-byte
  unchanged. After scaffolding, run `cd web && npm install` once; `vuka build`
  then bundles the island on every build.

## cmd/vuka v0.30.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); a command-only release.

- **UI islands (Phase 2 — Vue).** `@ui.Island("…/X.vue")` now builds: the vuka
  command compiles the SFC with Node esbuild + `esbuild-plugin-vue3` (one
  code-split build per app — the Vue adapter and every `.vue` island as entry
  points, with the Vue runtime deduped into a shared chunk), serves the chunks
  at `/_vuka/i/`, and records each island with its module + a Vue adapter chunk;
  the client loader mounts it with `createApp`, re-pushing changed props on a
  live re-render and unmounting when it leaves. The build deps (`vue`,
  `esbuild`, `esbuild-plugin-vue3`) come from the frontend root
  (`web.FrontendRoot`); a server-rendered app without a frontend adds a minimal
  `package.json` there. An optional `_setup.ts` (default export `(app)=>{}`)
  runs per island app. Vanilla islands are unchanged; React/Svelte await a later
  phase. Design: docs/design/islands.md.

## v0.26.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.29.0`. New apps
use ui v0.19.0, web v0.22.0.

- **UI islands (Phase 1 — vanilla).** Astro-style client islands: a
  server-rendered page stays zero-JS, and a component becomes an interactive
  island hydrated on a `client:*` strategy. A `<ui.Island>` wrapper around an
  island-component tag lowers to a `ui.MountIsland(...)` call — the body's
  attributes become the island's props, `client:load`/`idle`/`visible`/`media`/
  `only` the hydration strategy, its children the slot, and a nested
  `<ui.Template fallback>` the fallback shown until hydration. An island is a Go
  struct annotated `@ui.Island("islands/Counter.ts")` (Go owns the props); the
  vuka command bundles the component into a lazy chunk served at `/_vuka/i/`,
  records it in the asset manifest, and generates `islands.d.ts` from the struct
  into the frontend root (`web.FrontendRoot`). The client loader arms the
  strategy (IntersectionObserver / idle / media / load), lazy-imports the module
  on trigger, and mounts it; a live re-render re-pushes changed props. Phase 1 is
  vanilla `.ts`/`.js` only (a vanilla module exports `mount` itself); Vue/React/
  Svelte adapters, the `live()` prop bridge, and the opt-in SSR sidecar are later
  phases. Design: docs/design/islands.md.

## v0.25.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.28.0`.

- **Inferred type arguments for generic stateful components.** The type-argument
  inference that v0.24.0 brought to function components now covers **stateful**
  components — struct-type tags whose pointer implements the target's `Stateful`.
  `<Box Value={7} />` for `type Box[T any] struct{ ui.Live; Value T }` infers
  `T = int` from the attribute, where before it required `<Box[int] Value={7} />`.
  Each attribute binds to its exported field, and the field's declared type
  (`Value T`, `Items []T`, `At *T`, `Cols []Column[T]`, …) is unified structurally
  with the value's concrete type to pin the parameters, reusing the same unifier
  as the function-component path. Because Go can't infer a composite literal's
  type, the inferred arguments are written onto the generated literal
  (`&Box[int]{Value: 7}`). Explicit type arguments on the tag still work and take
  precedence; an under-constrained or conflicting tag still asks for them — now
  naming the conflicting parameter. Pure compile-time resolution; no runtime or
  JSX-contract change.

## v0.24.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.27.0`.

- **Inferred type arguments for generic JSX components.** A component whose
  single parameter is a generic props struct — `func Table[T any](p
  TableProps[T]) Node` — can now be used as a tag without writing its type
  arguments: `<Table Rows={rows} Cell={fn} />` infers `T` from the attribute
  values, where before it required `<Table[User] …/>`. Each attribute binds to
  its field, and the field's declared type (`Rows []T`, `Cell func(T) string`,
  `Cols []Column[T]`, `Rows []*T`, …) is unified structurally with the
  attribute value's concrete type to pin the parameters; multi-parameter
  components (`Pair[K, V]`) are inferred from several attributes. Explicit type
  arguments on the tag still work and take precedence. When no attribute
  constrains a parameter, or two attributes imply conflicting types, the tag
  still asks for explicit type arguments — now naming the conflict. Pure
  compile-time resolution; no runtime or JSX-contract change.

## v0.23.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.26.0`.

- **Self-target JSX.** A package that itself declares `const VukaJSX = 1` — a
  JSX runtime such as a component kit — can now author JSX in its own `.vuka`
  files. Its JSX lowers to unqualified `El`/`F`/`Child`/… and same-package
  component tags (`<Badge/>`) resolve in-package, instead of requiring an
  imported target. A file that imports an external JSX target still uses it
  (external wins); nothing changes for apps that import a kit. No runtime or
  JSX-contract change.

## v0.22.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.25.0`.

- **Faster pages.** JSX lowers to a lazy frame (`ui.FT`) when the target
  declares it ([ui](https://github.com/vuka-lang/ui) v0.13.0 and up): the
  element tree moves into a build closure with the dynamics passed as
  arguments, so a server-rendered page writes its static HTML straight through
  and renders only the parts that change. A 50-row table page is about 2×
  faster with ~35% fewer allocations, and the HTML and the live render tree
  are byte-for-byte what the old lowering produced. Generated code built
  against an older ui still lowers the old way; nothing else changes.

## v0.21.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.24.0`.

- **[Enums](/features/enums).** `enum Status { Draft, Sent, Paid }` and
  `enum Role string { Admin = "admin", … }` declare value enums with
  `Status.Values()`, `Status.Parse`, `String`, `Valid`, JSON (`MarshalText` /
  `UnmarshalText`) and `database/sql` (`Scan` / `Value`) methods, so an enum
  field works in JSON and the ORM as it is. `enum Shape { Circle{Radius
  float64}; Empty }` declares variants that carry data. Variants are
  reached as `Status.Draft` and `Shape.Circle{Radius: 2}`, and plain Go uses
  `StatusDraft`, `ParseStatus` and `ShapeCircle`.
- `match` takes variants as patterns, `case Shape.Circle{Radius: r}:`, and a
  match over an enum that misses a variant is an error naming the missing
  ones. Patterns separated by commas may now be type tests:
  `case Shape.Circle{}, Shape.Rect{}:`.
- The editor completes an enum's variants, `Values` and `Parse` after
  `Status.`, with their doc comments on hover; `vuka fmt` lays enums out;
  the VS Code grammar highlights them.
- **Model packages link themselves.** A package whose struct types embed a
  type with a `vukaDeclares()` marker method is auto-linked like a declarer
  package; with orm v0.5.1, whose `Base` and `Model` have it, a model package
  nothing imports is still registered and migrated, with no blank import.
- **Fixed:** `Some` and `None` (and statics) in a field attribute's value are
  qualified and inferred like any other code: `@orm.Int{Min: Some(0)}`.
- **Fixed:** an attribute type's `vukaTargets` result may be an alias or a
  named type of the targets array, and its targets are checked.

## Libraries — 2026-10-12

- **[ui](https://github.com/vuka-lang/ui) v0.12.0 and
  [web](https://github.com/vuka-lang/web) v0.19.0:** server-rendered HTML
  pages are about 40% faster, with half the allocations, and the output
  byte-for-byte the same. An element or frame is built in one allocation,
  text and attribute values are escaped only when they hold `& ' < > "`, and a
  page with nothing live renders straight into a pooled buffer instead of
  through a live session. New API `ui.RenderPage`.
- **[orm](https://github.com/vuka-lang/orm) v0.5.1:** `orm.Base` marks the
  types embedding it as declaring a model, so Vuka links a model's package
  into the program and the ORM sees models no code imports (with the next
  vuka release, which recognises the marker).

## cmd/vuka v0.23.2 — 2026-10-12

Runtime: unchanged, v0.16.0.

- **Editor:** after a struct field, `@pkg.` completes that package's field
  attributes — the types and the functions returning one, with their docs —
  and a half-typed field attribute no longer breaks the file's other
  diagnostics and hover.
- New apps use [web](https://github.com/vuka-lang/web) v0.18.0, whose
  `auth.Config.UserCache` keeps a loaded user for a while, so a signed-in
  request reads only its session or token; `auth.Refresh(ctx, uid)` drops
  one after a change.
## v0.20.1 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.23.1`.

- **Fixed:** auto-linking no longer links a package under an `internal`
  directory into a program outside that directory's tree (Go refuses such
  an import, so a module with examples failed to build).
- With [web](https://github.com/vuka-lang/web) v0.17.0, a live page checks
  the sign-in when it mounts (join, navigate) and its events read no
  session; sign-outs through auth close the page at once, and a periodic
  check (`LiveCheckEvery`, 30s) catches revocations made outside auth.

## v0.20.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.23.0`.

- **Page and route packages link themselves.** Every package of the module
  that declares something — `@web.Live`, `@web.Get`, `@web.Cron`, or a
  composed decorator holding one — is linked into the program by
  `vuka run`, `build`, `test` and `vet`: no blank import to remember, and
  an error in such a package always fails the build. The command says what
  it linked (`● linking 3 declarer packages (…)`); `vuka gen -inplace`
  writes the same file (`vuka_link_gen.go`) for a plain `go build`.
  `//vuka:nolink` above a package clause opts that package out, or, on a
  main package, turns linking off for that program.

## v0.19.1 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.22.2`.

- **`vuka explain` shows what the build compiles.** It now takes the file's
  generated Go from the same full transpile the build runs, so it no longer
  reports errors the build doesn't (a static reached through an embedded
  type from another package said "has no field"). A file that truly fails
  prints the error and exits non-zero.

## cmd/vuka v0.22.1 — 2026-10-12

The vuka command only. Completion after `entities.Letter.` now works in a file
that hasn't imported the type's package yet — picking a member also inserts
the import. Restart the language server after installing.

## cmd/vuka v0.22.0 — 2026-10-11

The vuka command only (vuka v0.19.0, runtime v0.16.0 unchanged). New apps use
ui v0.11.0 and web v0.16.0.

- **Completion knows statics.** `entities.Letter.` offers `Objects`, the
  fields and every static reached through embedding, with docs; hover on a
  static shows its declaration; the file keeps compiling while the member is
  half-typed.
- **Fixed:** picking a decorator from another package in a file with no
  import block inserted the import before the `package` line.

Restart the language server in your editor after installing.

## v0.19.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.22.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.11.0 and [web](https://github.com/vuka-lang/web) v0.16.0.

- **The editor completes statics.** Typing `entities.Letter.` offers
  `Objects`, the model's fields and every static the type reaches through
  embedding, with their docs; hover on a static shows its declaration. A file
  keeps compiling while the member is still being typed.
- **web v0.16.0: `Mount` runs once per page view.** The socket join adopts
  the page the HTTP render mounted when it arrives in time (`web.LiveAdopt`,
  10s) and as the same signed-in user — so a `Mount`'s queries no longer run
  twice. Anything uncertain mounts fresh, exactly as before.

## v0.18.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.21.0`.

- **A faster editor.** `vuka lsp` shows Vuka syntax errors as you type
  (about 1 ms, from the file being edited alone), type errors about 100 ms
  after you pause, and answers hover and completion without waiting for the
  whole module to rebuild: it keeps the transpile cache `vuka run -watch`
  uses and rebuilds the packages you edited first. Restart the language server
  in your editor after upgrading.

## cmd/vuka v0.20.0 — 2026-10-11 — security release

The vuka command only; the language and runtime are unchanged (v0.17.0, runtime
v0.16.0). New apps use [`github.com/vuka-lang/ui`](/ui/) v0.10.0,
[web](https://github.com/vuka-lang/web) v0.15.0 and
[orm](https://github.com/vuka-lang/orm) v0.4.0, which fix the issues a security
review found. Upgrade apps with `go get` of those versions, and build them with
Go 1.27.2 or newer.

What apps may notice:

- **Live pages.** A navigation keeps a page mounted only when it declares an
  `Update` taking `web.URLParams` — and that `Update` must repeat the checks
  `Mount` makes; any other page mounts again. Errors that aren't `*web.Error`
  read "Internal Server Error" outside development (logged in full). Live
  connections have limits: join within 10s, idle 30m, 10,000 in all, 100 per
  client address, 4 per page; checks run every 30s and before broadcasts.
- **JS commands.** `SetAttr`, `RemoveAttr` and `ToggleAttr` refuse event
  handler attributes and `srcdoc`; URL values and `Navigate`/`Patch` take only
  paths and http(s) URLs.
- **Auth.** Gated calls are strict by default (`auth.System(ctx)` for jobs and
  `main`; crons get it automatically; `LaxGates` opts out). Failed logins take
  at least 300ms. Throttling counts per account and folds spellings. Policies
  match `T` and `*T`. Live pages reload when a user's permissions change.
- **Requests.** Security headers by default; request bodies capped at 32 MiB;
  forwarded headers trusted only from `web.TrustedProxies`; static files never
  list directories or serve dotfiles; `.env` files can't set `VUKA_*`.
- **ORM.** A `ReadOnly` database is read-only on the server; pools are capped by
  default (20 open); `/healthz` shows states only outside development;
  `orm migrate` asks before unapplying.

## v0.17.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.19.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.9.0 and [web](https://github.com/vuka-lang/web) v0.14.0.

- **JS commands reach more elements, more safely.** `ui.Ref` and `ui.Sel` take
  several names; `ui.GetAttr(name, values…)` picks elements by attribute, and
  a literal value nothing in the component has is a compile error with a
  did-you-mean. Conditions combine with `And`, `Or`, `Not`, `ui.All`,
  `ui.Any` and `.Some()`; `ui.JS.When(cond, chain).Else(chain)` needs no
  `End`; `ui.JS.Busy()` marks a form or button busy and undoes it on a reply,
  a timeout or the back button. See [JS commands](/ui/js).
- **Fixed:** a decorated method with an unnamed receiver no longer fails to
  compile when a parameter is called `r`.
- web v0.14.0 also brings trailing-slash redirects, failed form posts that
  re-render the whole page, and `auth.Config.Hashers` for passwords from
  another system.

## cmd/vuka v0.18.1 — 2026-10-11

The vuka command only. The editor no longer reports `expected declaration` on a
method chain that goes on to the next line (`ui.JS.` then `If(…)` below it);
`vuka build` always accepted it.

## v0.16.0 — 2026-10-11

Runtime: **v0.16.0** — generated code registers attributes with it; run
`vuka fix runtime` in your apps after upgrading the vuka command. The vuka
command is `cmd/vuka/v0.18.0`. With [`github.com/vuka-lang/ui`](/ui/) v0.8.0 and
[web](https://github.com/vuka-lang/web) v0.13.0.

- **Attributes readable from a value.** `vuka.FuncOf(fn)` (a method value,
  method expression or function), `vuka.MethodAttrs(t, name)` and
  `vuka.TypeAttrs(t)` return the attributes written on a declaration, so a
  library can read `@auth.Requires(…)` where it dispatches. See [Reading
  attributes from a value](/features/attributes).
- **web's auth gates checked before a live handler runs** and at each gated
  call, with the identity carried by `ctx`; `auth.Config.StrictGates` refuses
  a gated call that has none. See [Authentication](/web/auth).
- **`web.IsLiveJoin(ctx)`** tells a live page's route it is running again for
  the socket's join, so side effects such as mail happen once. See [Live
  components](/web/live).

## cmd/vuka v0.17.0 — 2026-10-11

The vuka command only; the language and runtime are unchanged (v0.15.0, runtime v0.10.0).
New apps use [web](https://github.com/vuka-lang/web) v0.12.0 and [orm](https://github.com/vuka-lang/orm) v0.3.0.

- **`vuka new` builds a working app.** It asks for the UI (live pages with
  Tailwind, templ views, or an API), the database (SQLite by default, Postgres,
  MySQL, none), mail, sign-in and an optional React, Vue or Svelte frontend —
  or takes flags and `--yes`. The default app has a login page already
  connected to a users table and sessions in the database, an admin created
  from `.env` with a generated password, migrations run at start, a signed-in
  layout with the Vuka logo, shared components, and tests that pass. See
  [Getting started](/web/getting-started).
- **`vuka add module <name>`** adds a module — model, service, page, migration
  and test — to an app made by `vuka new`.

## cmd/vuka v0.16.0 — 2026-10-11

The vuka command only; the language and runtime are unchanged (v0.15.0, runtime v0.10.0).
With [web](https://github.com/vuka-lang/web) v0.11.0: [authentication](/web/auth),
[mail](/web/mail), [crons](/web/crons) and [embedded frontends](/web/frontend).

- **Frontend apps in `vuka run` and `vuka build`.** A `frontend/package.json`
  starts the project's own Vite beside the app (dependencies installed on
  first run), kept running through `-watch` rebuilds; you open the app's
  address and get hot module reloading. `vuka build` runs `vite build` before
  embedding. A prebuilt `dist` with no `package.json` is served as is.

## v0.15.0 — 2026-10-11

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.15.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.7.0 and [web](https://github.com/vuka-lang/web) v0.10.0.

- **Styles and scripts per page.** `web.Style(...)` and `web.Script(...)` on
  `@web.Live`, `web.Layout` and `web.New` (several files, globs, `web.CSS` and
  `web.JS` inline, `web.Global` unscoped). CSS is scoped to the component —
  a marker the compiler adds to its own markup, so it never travels in live
  diffs — with `:global`, `:deep`, `:scope`, scoped `@keyframes`, Tailwind's
  `@apply` inside, and a warning for a selector that matches nothing. Scripts
  are TypeScript or JavaScript, compiled by the vuka command with no Node:
  `mounted`/`updated`/`destroyed` per instance, `ctx.push("Method")`,
  `ctx.on`, `ctx.ref`, and `ui.JS.Call` reaching them. Only methods a
  component's scripts name with `ctx.push("…")` can be called by name; the
  compiler checks each name. See [Styles and scripts](/web/assets).
- **Faster `vuka run -watch`.** A save answers in about 0.8–1s on a small app,
  down from 1.6–1.8s: file events from the operating system instead of
  polling, transpiling only what changed (checked against a full transpile in
  the tests), no debug info in watch builds (`-debug` keeps it), and a save
  during a build cancels the stale build.

## v0.14.0 — 2026-10-11

Runtime: unchanged, v0.10.0 (`vuka.ObserveCalls` is new in the package; generated
code doesn't need it). The vuka command is `cmd/vuka/v0.14.0`. With
[web](https://github.com/vuka-lang/web) v0.9.0's [development toolbar](/web/toolbar).

- **`vuka.ObserveCalls`** lets a tool see each step of every decorated call —
  each decorator, then the function — on the call's goroutine. The toolbar's
  Timeline uses it to show `@timed` and `@logged` inside a request. With no
  observer it costs one atomic load per step.
- **`vuka run` names a session directory** (`VUKA_DEV_DIR`). Under `-watch`
  it holds the next build's state — building, failed with the compiler's
  output, or ok — so the toolbar shows `building… 1.2s` or `build failed` on
  the page while the last good build keeps serving.

## v0.13.0 — 2026-10-11

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.13.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.5.0 and [web](https://github.com/vuka-lang/web) v0.8.0.

- **Templ components diff on live pages.** In a module on ui v0.5.0 or
  later, the vuka command compiles `.templ` files so a live render records
  their fixed markup apart from the values that change, as it does for JSX.
  An event inside a templ layout sends only what changed, and moving between
  pages that share a templ layout sends what differs inside it, not the whole
  document. Plain HTTP renders write exactly the same HTML as before; templ
  code compiled elsewhere still renders as one block. See [templ](/ui/templ).
- **`vuka run -watch`** rebuilds the app when a `.vuka`, `.templ` or `.go`
  file, `go.mod` or an embedded file changes, and swaps it in once the build
  succeeds. The running app keeps serving while the next one compiles; a
  failed build prints its errors at the `.vuka` line and keeps the last good
  build running; a change that leaves the binary the same (a comment) restarts
  nothing. Tailwind keeps running through the restarts, and with web v0.8.0
  live pages rejoin the new build instead of reloading. See
  [vuka run](/tools/cli#run-watch).

## v0.12.0 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.12.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.3.0's [optimistic updates and
`Call`](/ui/js#optimistic-updates) and [web](https://github.com/vuka-lang/web)
v0.6.0, which runs them in the browser and styles pages with Tailwind.

- **Tailwind for web apps, with no setup.** In a module that uses web,
  `vuka run` keeps `tailwindcss --watch` running and pages pick up each new
  stylesheet without reloading; `vuka build` and `vuka install` build it
  minified into the binary; `vuka gen` writes the file that registers it, so a
  plain `go build` works too. Classes are found in your `.vuka`, `.templ`,
  `.html` and `.go` files and in the component packages you link. Your own
  `app.css`, `styles/app.css` or `web.Tailwind(path)` replaces the built-in
  stylesheet; `web.NoTailwind()` or `VUKA_TAILWIND=off` turns it off. See
  [Styling](/web/styling).

## v0.11.0 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.11.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.2.0's [JS commands](/ui/js) and
[web](https://github.com/vuka-lang/web) v0.5.0's browser engine for them.

- **Refs.** A JSX target declaring `Ref` gets refs: `ref="menu"` on an
  element names it for the function's code, and each `ui.Ref("menu")` with a
  literal name in a function with JSX must name one of its elements — `no
  ref="mnu" in Menu's render; did you mean "menu"?`.
- The editor no longer offers gopls' deep completions (`x.Y().Z`) as a
  props struct's attributes.

## v0.10.1 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.10.1`.

- **A dependency that doesn't compile is reported as such**, once, at the
  import that needs it, with its own error and file — not as "no export data"
  on every package above it. A dependency built for an older Vuka gets a hint
  (`web v0.3.0 was built for an older Vuka; upgrade it (go get …@latest)`);
  code of your own written for one, `run vuka fix ui`. Healthy packages are no
  longer blamed, and errors that only follow from a failed import ("its type is
  unknown") are left out. The editor shows the same.

## v0.10.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.10.0` — the UI layer is gone from it.
New library: [`github.com/vuka-lang/ui`](/ui/) v0.1.0. Run `vuka fix ui`.

- **JSX targets.** JSX is syntax; what it renders with is a library the
  file imports, its [JSX target](/features/jsx#jsx-targets): a package
  declaring `const VukaJSX = 1` and `Node`, `El`, `Text`, `Child`,
  `Fragment`, `Nodes`, `Try` (optionally `On`, `Stateful` + `Component`,
  `WithChildren`, `F`). A file with JSX imports one target; without one it
  is a compile error saying so. Component resolution, attribute binding,
  generic and overloaded components are checked against the target's
  declarations; event payload rules, `Mount`'s shape and `*Live` embedding
  are the target's runtime checks now.
- **`github.com/vuka-lang/ui`**: everything that was `vuka.Node`,
  `vuka.Live`, `vuka.Event`, `vuka.Assign`, `vuka.Tree`, `vuka.String`, … and
  the `live`, `live/livetest`, `term` and `templx` packages, unchanged but for
  the package name. `vuka.TemplComponent`, `RegisterTempl` and
  `TemplComponents` are `templx.Component`, `Register` and `Components`.
- **`vuka fix ui`** rewrites a module: names, imports, the ui import in
  every `.vuka` file with JSX, `go get github.com/vuka-lang/ui`.
- **The runtime requires nothing** (no templ) and needs Go 1.23.
- **The vuka command is its own module**, `github.com/vuka-lang/vuka/cmd/vuka`
  (`go install github.com/vuka-lang/vuka/cmd/vuka@latest` as before). It
  compiles `.templ` files with templ's parser and generator — a plugin of the
  command, on for modules that require templ.
- **Decorators:** optional arguments, parameter attributes, composition,
  type-level advice and attribute targets.
  - A decorator whose parameters are all optional can be written bare:
    `@transaction` calls `transaction()`, and `@transaction(On("db"))` is
    the same decorator with settings.
  - **Parameter attributes:** `func Show(@Path("id") id int, @Body in NewPet)`,
    read by declarers (`vuka.Param.Attrs`) and call decorators
    (`c.ParamAttr(i, &x)`).
  - **Composed decorators:** `decorator ApiRoute(path string) = @web.Get(path)
    @web.Use(auth) @timed`, exported and used like any decorator.
  - **Type-level advice:** a call decorator on a type wraps every method of
    it declared in `.vuka` files; `@vuka.NoAdvice` opts a method out.
  - **Attribute targets:** `@vuka.Targets(vuka.OnField)` on an attribute type
    makes using it elsewhere a compile error.
  [Decorators](/features/decorators), [Attributes](/features/attributes).
- VS Code extension 0.4.2: highlighting for parameter attributes and composed decorators.
- Docs: [UI](/ui/) is its own section; the language's [JSX](/features/jsx)
  page is library-neutral; [Live protocol](/reference/live-protocol) is the
  reference for transport authors.

## v0.9.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.9.0` (`vuka fix runtime` upgrades). New `vuka.Frame`/`vuka.F` (JSX's compile-time shape), `vuka.Tree`,
`vuka.TreeList`, `vuka.TreeRef`, `vuka.BuildTree`, `vuka.Assign`,
`vuka.RootAttrs`; `live.Session.RenderTrees`, `HTML`, `live.TreeUpdate`,
`live.Verify`, `live.Marshal`, package `live/livetest`. Generated code needs
it: JSX now lowers through `vuka.F`.

- **Live render trees (protocol v2).** A client joining with
  `{"type":"join","v":2}` gets each instance's render tree — statics sent
  once per connection, dynamics, block items, instance references — and
  after an event only the changed dynamics: list steps with keyed moves,
  branch switches, long markup by reference or token patch. v1 clients are
  unchanged. A one-cell change in a 1000-row table is 89 bytes instead of
  122 KB. See [Protocol v2](/reference/live-protocol#protocol-v2-render-trees).
- **`vuka.Assign[T]`:** a component whose state is all Assigns is rendered
  again only when one is set or its props change; tests verify skipped
  components against a full render (`VUKA_LIVE_VERIFY`).
- `live/livetest`, a reference client of both protocol versions in Go.
- Fixed: a stateful component tag with an expression key (`<Row key={i} />`)
  crashed the compiler.
- Server messages are written without HTML escaping (`<` rather than
  `\u003c`); the JSON is the same once parsed.
- `vuka gen -inplace` writes line directives relative to the generated
  file, so committed generated code no longer carries the checkout's path.
- [vuka-lang/web](https://github.com/vuka-lang/web) v0.3.0 speaks protocol v2
  in the browser.

## v0.8.0 — 2026-10-10

Runtime: unchanged, v0.7.0.

- **Seamless JSX editing.** Completion for every HTML element (and SVG
  basics) with descriptions, each element's own attributes before global
  ones, event handlers, `aria-*`/`data-*`, enumerated attribute values,
  closing tags, and `{for}`/`{if}`/`{match}` blocks with placeholders; hover
  on elements and attributes with MDN links; linked editing renames a tag's
  closing tag as you type; JSX elements and blocks fold; at most one
  diagnostic while a tag is half-typed.
- **Statics:** a generic type's static takes any initializer —
  `&Store[Self]{}`, `NewStore[Self]()`, `new(Store[Self])`, literals — with
  its type inferred, `Self` per instantiation.
- **VS Code extension 0.4.1:** closing tags after other tags on a line and
  inside attribute braces, `{` pairs inside elements, Enter indents between
  tags, `editor.linkedEditing` on for `.vuka` files.
- Docs: the language pages are library-free; the web framework and the ORM
  have their own sections.

## v0.7.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.7.0` (new: `vuka.Live`, `vuka.Component`,
`vuka.On`, `vuka.Event`, the `live` package; field references `vuka.Ref` with
`OrderedRef`, `StringRef`, `CompareRef`, `NullableRef`, `vuka.Pred`,
`vuka.Order`, `vuka.FieldPath`, `vuka.Related`, `vuka.FieldAttrs`,
`vuka.FieldsOf`; `vuka.Field` has `Attrs`).

- **Stateful components.** A struct embedding `vuka.Live`, with `Mount`,
  `Render` and event handlers: `<Counter Start={5}/>` keeps its state on the
  server, `onClick={c.Inc}` (methods or closures, values, form structs) runs
  there, and only the components that changed are sent back. Components nest,
  `key` keeps list items attached, `Update(msg)` overloads receive broadcasts.
  Rendered without a session they are plain HTML. The `live` package is the
  transport-independent session; [vuka-lang/web](https://github.com/vuka-lang/web)
  serves it over WebSocket. [Stateful components](/ui/stateful).
- **Field attributes.** Typed attributes after a struct field, after its tag:
  `` Title string `json:"title"` @Char{Max: 200} ``, recorded at init.
  [Field attributes](/features/fields#field-attributes).
- **Field references.** `Post.Title` is a typed reference to the field;
  `Post.Author.Name` follows struct, pointer and `vuka.Related` fields. They
  build typed predicates (`Post.Views.Gt(100)`) and orderings, the
  foundation of [vuka-lang/orm](https://github.com/vuka-lang/orm)'s queries.
  [Field references](/features/fields#field-references).
- **JSX:** brace-less `if`/`for`/`match` inside block bodies; JSX in
  package-level variables; props from embedded struct fields; a block header
  may hold composite literals.
- **Statics** may use other statics, field references and `Self` in their
  initializers; initialization cycles are reported with their chain.
- **match:** exhaustiveness checks nested patterns (`Ok(Some(p))`, `Ok(None)`,
  `Err(e)`), and a `match` on a value from `x := f()?` sees its unwrapped type.
- Errors quote field references and statics as the source spells them.
- **Editor:** `vuka.File` paths are links (click to open, hover lists a
  `.templ` file's components); positions stay right while a file with
  several edits doesn't compile; completion offers stateful components, their
  props and event handlers.
- **VS Code extension 0.4.0:** highlighting for field attributes, match
  patterns, JSX blocks, events and component tags; 16 snippets (components,
  routes, live pages, ORM models, transactions); auto-closing tags; the vuka
  version in the status bar.

## v0.6.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.6.0` (new: `vuka.Decl`, `vuka.File`,
`vuka.TemplComponent`).

- **Declaration decorators.** A decorator of type `func(*vuka.Decl)` — or a
  call returning one, such as `@web.Get("/pets/{id}")` — runs once at init with
  the function's name, attributes, parameter names and types, and the function
  itself: the way to register routes, commands or jobs without wrapping calls.
  [Decorators](/features/decorators#declaration-decorators).
- **`vuka.File`.** A string literal where a decorator or attribute takes a
  `vuka.File` is checked when the program compiles (it must exist, in the
  package's directory or below) and embedded in the binary. For a `.templ` file
  `vuka.TemplComponents(f)` gives its components with their parameter names; a
  `.templ` file may sit in a subdirectory that is its own package
  (`views/pets.templ`, `package views`), which Vuka compiles and imports.
  [Files](/features/decorators#files).
- The export-data cache no longer keeps modules replaced by a directory
  (`replace … => ../vuka`), which could serve a stale runtime.
- [**vuka-lang/web**](https://github.com/vuka-lang/web), a web framework built on
  declarers: routes from `@web.Get`, handler parameters bound from the path,
  query and body, dependencies injected, results rendered as JSON or through
  `@web.Template` views (`.html`, `.templ` or JSX).

## v0.5.0 — 2026-10-10

- **Components and JSX.** Markup is an expression in `.vuka` files; components
  are functions, with attributes bound to parameters by name or to a props
  struct, `{for}`, `{if}` and `{match}` blocks, and errors at the tag.
  [Components and JSX](/features/jsx).
- **Rendered by templ.** `vuka.Node` is `templ.Component`: Vuka components and
  templ components call each other, and `.templ` files compile with the rest of
  the package (no `templ generate`), with editor support mapped into them.
- Components can be generic (type arguments inferred, or `<List[User]>`) or
  overloaded; `?` and `match` work inside `{…}`; a Vuka component that would
  drop its children is an error.
- `vuka.Handler`, `vuka.Write`, `vuka.String`; a walkable node tree
  (`vuka.Walk`, `vuka.Renderer`) and a terminal renderer, `vuka/term`, which
  lays out templ components' HTML too.
- **`vuka fmt`**: Go as gofmt prints it, JSX Prettier-style, never changing
  what a page renders. Format on save in the editor.
- Editor: completion after `<`, in a tag's attributes and after `</`; hover,
  go to definition and rename on tags, closing tags and attributes; a tag
  being typed no longer breaks the rest of the file.
- templ's language server can use vuka as its gopls, so `.templ` files see
  code from `.vuka` files.
- VS Code extension 0.3.0: JSX highlighting, templ's icon for `.templ` files,
  format on save, and an offer to serve `.templ` files through Vuka.

**Breaking:** the decorator helper `vuka.Attr[T](c)` is now `vuka.AttrOf[T](c)`
(`vuka.Attr` is JSX's attribute). `vuka fix attr-of` rewrites it. Go 1.25 or
newer is required (templ needs it).

Runtime: v0.5.0.

## v0.4.1 — 2026-10-10

- **`vuka mod`**: `go mod` that sees the imports of `.vuka` files. `vuka mod tidy`
  keeps requirements plain `go mod tidy` would drop.
- Editor: a hover or go-to-definition gopls can't answer (a broken import, code
  mid-edit) is now no answer instead of an error, and VS Code no longer opens
  the output panel on errors.
- An import nothing provides that names a folder of your module gets a hint:
  *it's in this module: import "hello/data"*.

Runtime: v0.3.0.

## v0.4.0 — 2026-10-10

- **One gopls.** Run as `gopls`, vuka is a drop-in gopls, so `.go` files in a
  package with `.vuka` files see their code; every editor session shares one
  gopls daemon.
- **`vuka explain`** shows each line beside the Go it becomes.
- **`vuka fix`**: `runtime`, `static-names`, `orphans`.
- Faster builds: one `go list` per build, export data cached across builds.
- Workspaces reached through symlinks.
- This documentation site.

Runtime: v0.3.0.

## v0.3.1 — 2026-10-09

- Typing `@` lists the project's decorators, narrowing as you type.

## v0.3.0 — 2026-10-09

- **Dependency injection**: a type decorator on a struct gets `t.New`, a
  constructor taking its dependency fields (`inject:"-"` opts out).
- Picking a decorator after `@` inserts its name; a factory gets a call.

Runtime: v0.3.0.

## v0.2.1 — 2026-10-09

- Completion keeps working while an `@` line is being typed; code actions over
  attributes no longer fail; a clear message when the runtime is too old.

## v0.2.0 — 2026-10-09

- **Python-style decorators**: `decorator name(c) { … }`, `c.Next`, `c.Return`,
  `c.Err`, `c.Context`, `c.Attr`; type decorators.
- **Statics** and static methods, statics of generic types and through
  embedding; **Self** inference; **`self` methods**.

Runtime: v0.2.0.

## v0.1.1 — 2026-10-09

- The language server works beside VS Code's Go extension.

## v0.1.0 — 2026-10-09

- Result and Option, `?`, `match`, overloading, attributes, `@export`.
- `vuka build`, `run`, `test`, `gen`, `new`; the `build/` module.
- `vuka lsp` and the VS Code extension.
