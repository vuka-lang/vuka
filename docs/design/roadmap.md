# Roadmap: from fast to finished

vuka grew quickly: JSX, live views, islands, component-backed pages,
deferred fields, navigation, layouts. The features work, and real apps run
on them. What's missing is the feeling that every part was designed to fit
the others, that releases are predictable, and that a surprise is a bug and
not a convention you hadn't learned yet.

This roadmap is about that. It adds almost no new features. Each phase ends
with something a user can notice: fewer surprises, clearer errors, code they
can read, docs that answer questions.

## Principles

- **Explicit over discovered.** Behaviour comes from what you wrote (a
  typed attribute, an interface), not from a method's name or a string in a
  struct tag.
- **One way to do a thing.** When two forms exist, one is the default and
  the other is documented as the escape hatch.
- **Errors teach.** Every diagnostic says where, why and what to do, and has
  a stable code.
- **Red means stop.** No test is "known failing".
- **Generated code is source code.** If people can't read it, they can't
  trust it.
- **Breaking changes are rare, announced and automated.** `vuka fix` does
  the edit.

## Phase 1 — Trust (release discipline, tests, errors)

Goal: every release is boring. Upgrading is a decision, not a gamble.

### 1.1 One version for the whole toolchain

Today vuka, ui, web and cmd/vuka are tagged separately and must be lined up
by hand (ui v0.30.0 + web v0.36.3 + vuka v0.32.0 + cmd v0.45.3).

- Release all four together under one version: `vuka v0.50.0` means the
  library, ui, web and the CLI at matching tags.
- `vuka version` prints the set and warns when an app's go.mod mixes sets.
- `vuka upgrade [version]` moves an app's go.mod (never `go mod tidy`), runs
  `vuka fix` for the steps in between, then vet and tests.
- New apps from `vuka new` pin one set.

Done when: an app has one vuka version to think about.

### 1.2 A release train

- Fixes land on main continuously; patch releases are cut on a schedule
  (weekly), not per fix. Security fixes are the exception and ship at once.
- Minor releases carry features and are announced with upgrade notes.
- Each release has a section in `docs/releases.md` written for app authors:
  what changed, what to do, what `vuka fix` does for you.

Done when: there is at most one patch release per week outside security.

### 1.3 Compatibility promise

- Write down what may change in a minor release before 1.0 and after.
- Deprecations: a symbol keeps working for at least one minor release, with
  a `vuka vet` warning naming its replacement.
- Every breaking change ships a `vuka fix` rewrite and a release note
  example.

Done when: the policy page exists and the last three breaking changes had a
fix.

### 1.4 No red tests

- Fix `internal/format` `islands_emit` or delete it; CI fails on any
  failure, with no allow-list.
- Always run the real-Vite and Chrome end-to-end suites in CI (provision a
  pinned `node_modules` cache instead of skipping when `VUKA_VITE_NM` is
  unset). This week's bugs — lazy chunks fetched from `/`, islands mounted
  twice, body morphs removing teleports — were only visible there.
- Flaky tests are quarantined with an owner and a date, never retried
  silently.
- Fuzz the transpiler, the formatter (idempotence: `fmt(fmt(x)) == fmt(x)`)
  and the live diff (patch(old, diff(old, new)) == new).

Done when: CI is green on main for 30 days and every e2e suite runs on every
pull request.

### 1.5 Diagnostics with codes

Every compiler, `vuka vet` and boot-time diagnostic gets:

- a stable code (`E0412`), a primary span and a code frame;
- a `help:` line with the fix, and a `did you mean` where it applies;
- `vuka explain E0412` with a longer explanation and an example, mirrored on
  the docs site.

```
error[E0412]: deferred field CV has no loader
  --> modules/applicants/detail.vuka:118:2
    |
118 |     CV *CV @ui.Defer("profile")
    |     ^^ the group "profile" needs a loader
    = help: add @ui.Loads("profile") on a method
            func (p *ApplicantDetail) loadProfile(ctx context.Context) error
```

Boot-time errors (bad attribute, missing loader, name clash) move to compile
time wherever the information exists then.

Done when: no diagnostic is a bare Go error string, and the catalogue is on
the docs site.

## Phase 2 — Language (explicit, typed, readable)

Goal: what you write says what it does; what vuka generates reads like Go
you'd have written — and a plain Go module can use orm, web and ui with
`go run .` and no transpiler.

Plain Go on the library API is the language; `.vuka` is sugar. The CLI's
job is to generate the plain Go a person would write against that API —
public calls only, hand-written shape, ejectable (delete the `.vuka`, keep
the `.go`, `go run .` still works). Items 2.1–2.5 are designed as that API
first and as `.vuka` syntax second. Design: [vuka as a library](./library.md).

### 2.1 Typed attributes instead of struct-tag DSLs

vuka's own semantics move out of tag strings into attributes the compiler
and editor check:

| Today | After |
|---|---|
| ``bind:"q,debounce=500"`` | `@ui.Bind{Debounce: 500 * time.Millisecond}` |
| ``url:"q"`` | `@ui.URL("q")` |
| ``defer:"lists"`` | `@ui.Defer("lists")` |
| ``validate:"required,max=50"`` | `@validate.Required @validate.Max(50)` |

- Struct tags keep their Go meaning (json, db); vuka stops reading them.
- `vuka fix --attributes` rewrites existing code; the old tags keep working
  for one minor release with a vet warning.
- The editor completes attribute names and arguments and shows their docs on
  hover.

### 2.2 Explicit hooks instead of magic method names

`Mount`, `Changed`, `Update(T)` overloads and `Load<Group>` are found by
name today. That's how a field `CV` required a loader named `LoadCv`, and
why fields and methods can collide on the browser's `live` object.

```vuka
@ui.Mount                func (p *Orders) mount(ctx context.Context) error
@ui.OnChange             func (p *Orders) changed(ctx context.Context, c ui.Changed) error
@ui.Loads("lists")       func (p *Orders) loadLists(ctx context.Context) error
@ui.On[OrderPlaced]      func (p *Orders) placed(ctx context.Context, m OrderPlaced) error
```

- Hooks may be unexported, so they can never be called from the browser.
- Only exported methods without a hook attribute are callable as
  `live.method()`.
- Misuse (wrong signature, unknown group) is a compile error with a code.
- The old names keep working for one minor release with a vet warning;
  `vuka fix --hooks` rewrites them.

### 2.3 Readable generated code

Generated Go should read like hand-written Go:

- **JSX becomes HTML templates in backticks** with numbered, typed holes:
  ```go
  var adminShell = ui.Template(`
  <div class="flex h-screen">
    <aside class="… {{0}}">{{1}}</aside>
    <main id="main">{{2}}</main>
  </div>`)

  return adminShell.Render(wide, Banner(), children)
  ```
  Holes are escaped by kind (text, attribute value, attribute set, node).
  Static parts are parsed once; benchmarks must show no regression against
  today's fingerprint form.
- **Position markers per statement, relative paths:** `//line
  admin.vuka:23` instead of a `/*line /Users/…*/` before every expression.
  No absolute local paths in files or binaries.
- **No blank-line padding**, gofmt'd output, real names (`ordersType`, not
  `__Orders_type`), doc comments carried over, normal import blocks.
- Golden tests compare generated files, so readability is checked like any
  other output.

### 2.4 Backtick JSX as an equivalent form

- `ui.JSX(\`…\`)` in `.vuka` compiles exactly like bare JSX and gets the same
  editor features inside the string (types, hover, go-to-definition,
  injected highlighting).
- It keeps a file valid Go syntax for tools that don't know vuka.
- Bare JSX stays the default; `vuka fix --jsx=backticks|bare` converts.
- Backticks inside markup use `{"`"}`; the formatter does it for you.

### 2.5 One shape for options and names

A one-time API pass with a written style guide and a vet rule:

- Options are typed values, never positional strings
  (`@ui.Island(ui.Strategy("visible"))`, not `@ui.Island("", "visible")`).
- One constructor convention (`NewOrders`), one error type per kind shared
  across packages (`web.Invalid` and `ui.Invalid` become one).
- The same verbs for decorators and Go calls (`@web.Get` / `web.Get`).
- Exported names reviewed for consistency before 1.0; renames go through
  deprecation and `vuka fix`.

### 2.6 vuka as a library

Plain Go, no transpiler, `go run .` ([design](./library.md)):

- Generated code becomes an optional speed-up: an orm reflection fallback
  for row scanners and field sets, and a whole-component live diff fallback
  when render tracking isn't generated; `//go:generate go tool vuka gen` opts
  back in.
- `web.Dev()` runs Tailwind and Vite for `go run .` in development when
  they're installed, and keeps the islands built.
- Islands from Go source alone: `ui.RegisterIsland[T]()` finds its
  component as `@ui.Island` does, and `//go:generate go tool vuka
  build-islands` builds the bundle into the frontend's build for `go build`.
- The vet-time checks ship as a `go/analysis` analyzer (`vukavet`) with the
  same diagnostic codes.
- A conformance suite runs every example as `.vuka` and as hand-written Go,
  with and without generated code, and compares HTML, live patches, SQL and
  HTTP responses.

2.1–2.5 carry the API half: the registration calls (2.5), `ui.Template` with
typed holes (2.3, 2.4), and `orm.Schema` / `ui.LiveSchema` as the lowered
form of typed attributes and explicit hooks (2.1, 2.2).

## Phase 3 — Knowledge (reference and guides)

Goal: when something surprises you, the docs say whether it's a bug.

### 3.1 Language reference

Separate from the guides, precise and complete:

- Grammar: JSX, decorators, attributes, `?`, `match`, overloading.
- Every built-in decorator and attribute: arguments, where it's allowed,
  what it generates.
- Lifecycles as diagrams: a page load, a live join, a navigation, a patch, a
  deferred load, an island mount and update.
- The browser runtime API: `live.*`, `useVuka()`, `VukaLink`, `window.vuka`.
- The diagnostic catalogue (from 1.5).

### 3.2 Guides that build one app

One guide, start to finish: a small orders app with a list, a detail page
backed by a component, deferred fields, validation, navigation, a layout, a
background job, deploy. Every feature page links back into it.

### 3.3 Upgrade notes and cookbook

- Per-minor upgrade pages with before/after code.
- A cookbook for the recurring questions: islands with Nuxt UI, tooltips and
  toasts, legacy databases, file storage, auth gates.

## Phase 4 — Defaults and tools (fewer afternoons lost)

Goal: the toolchain owns the pitfalls instead of the app.

### 4.1 Safe defaults in the frontend toolchain

Each of these cost an afternoon this week; the toolchain should handle them:

- Pin a supported TypeScript for Vue islands and refuse an unsupported one
  with a clear message (done in part).
- Provide the UI library's app/tooltip providers for every island, so
  `UTooltip` and toasts work without wrapping.
- Island builds always use the right `base` (done in v0.45.2).
- The live morph leaves nodes page scripts add to `<body>` alone (done in
  web v0.36.3).
- Asset types beyond `.js` and `.css` (fonts, images) emitted by island
  builds are served.

### 4.2 `vuka doctor`

Checks the project and explains each problem with its fix: Go version,
module versions out of step, Node, package manager, TypeScript, Vite and
plugin versions, Tailwind sources, generated files on disk, the frontend
root, and common config mistakes.

### 4.3 Editor at Go's level

- Rename and find-references across `.vuka`, JSX and generated code.
- Code actions: add the missing loader, create the component, add the
  import, convert JSX form.
- Breakpoints on `.vuka` lines in delve, through the `//line` mapping from
  2.3.

### 4.4 Performance budgets

Benchmarks in CI for render, live diff, island hydrate and transpile, with
budgets that fail the build on regression.

## Phase 5 — 1.0

1.0 means: the compatibility promise applies, the reference is complete, CI
has been green for a quarter, and two real apps upgraded through at least
two minor releases using only `vuka upgrade` and `vuka fix`.

## Order and size

| Phase | Items | Size | Depends on |
|---|---|---|---|
| 1 Trust | 1.1–1.5 | 2–3 weeks | — |
| 2 Language | 2.1–2.6 | 6–8 weeks | 1.3 (deprecations), 1.5 (codes) |
| 3 Knowledge | 3.1–3.3 | 2 weeks, ongoing | 2 (so the reference describes the final shapes) |
| 4 Defaults and tools | 4.1–4.4 | 3–4 weeks, partly parallel | 2.3 for debugging |
| 5 1.0 | — | when the criteria hold | 1–4 |

Phase 1 comes first because everything after it changes the language, and
those changes are only safe with deprecations, codes and green CI in place.
