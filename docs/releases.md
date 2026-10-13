# Releases

Install a release with your own toolchain:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Each release states the **runtime** its generated code needs — the
`github.com/vuka-lang/vuka` package your module requires. `vuka fix runtime`
upgrades it.

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
