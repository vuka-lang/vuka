# RFC: One way to write UI (vuka 2.0)

Status: accepted, decisions recorded below. No code changes accompany this
document; implementation lands step by step (§8).

## Decisions

Recorded 2026-10-22. They settle the open items of §2 and §9; where the text
below still describes an alternative, this section wins.

1. **Straight to 2.0, no 1.x bridge.** 1.x was released on 2026-10-20 and
   has no dependents outside the maintainer's own apps, which move by
   hand. There is no 1.6 release carrying the one-way forms
   or deprecation warnings: 2.0 hard-removes every surface in the table of
   §2. The set moves to `/v2` module paths:

   | go.mod | 1.x module path | 2.0 module path | tag prefix |
   |---|---|---|---|
   | `vuka/go.mod` | `github.com/vuka-lang/vuka` | `github.com/vuka-lang/vuka/v2` | `v2.x.y` |
   | `vuka/cmd/vuka/go.mod` | `github.com/vuka-lang/vuka/cmd/vuka` | `github.com/vuka-lang/vuka/cmd/vuka/v2` | `cmd/vuka/v2.x.y` |
   | `vuka/cmd/vukavet/go.mod` | `github.com/vuka-lang/vuka/cmd/vukavet` | `github.com/vuka-lang/vuka/cmd/vukavet/v2` | `cmd/vukavet/v2.x.y` |
   | `ui/go.mod` (repo vuka-lang/ui) | `github.com/vuka-lang/ui` | `github.com/vuka-lang/ui/v2` | `v2.x.y` |
   | `web/go.mod` (repo vuka-lang/web) | `github.com/vuka-lang/web` | `github.com/vuka-lang/web/v2` | `v2.x.y` |

   Every import of these paths in the five modules, in `templx`/`live`/
   `term` and the other sub-packages (they are packages, not modules, so
   only their import paths change), in generated code, and in the
   repository's own test modules (`transpile/testdata/golden/*`,
   `cmd/vuka/testdata/routes/web`, `docs/testdata/gomode`, the conformance
   and example apps) moves with them.

   **orm follows.** `github.com/vuka-lang/orm` requires vuka and web and
   exposes vuka types in its API (`vuka.Option`, `vuka.Result`,
   `*vuka.Type` in `orm.Optional`, `orm.Entity`, `orm.Database`, …), so
   moving its requires to `/v2` changes its exported signatures: it becomes
   `github.com/vuka-lang/orm/v2` in the same release. Linking a v1 orm
   beside a v2 set would load two vuka registries, which is not supported.

2. **Go-first UI authoring is retired.** Rendering is `.vuka.html`-only.
   `ui.HTML`, `ui.UseState` and the other retired names survive only as
   compiler output and interop, and leave the guide. Logic stays in `.go`
   and `.vuka` files. The docs' Vuka / Go toggle goes.

3. **Hooks are decorator-only**: `@ui.Mount`, `@ui.OnChange`,
   `@ui.Loads("group")`, and the field attributes `@ui.Bind`, `@ui.URL`,
   `@ui.Defer`. `Mount` / `Changed` found by name, the `LiveSchema` method
   and the constructor hooks (`OnMount`, `Loads`, `OnChange` on `ui.Live`)
   are removed as ways to write; a method named `Mount` is an ordinary
   method (and, being exported, an event the browser can call — `vukavet`
   warns, naming `@ui.Mount`). The compiler may still emit `LiveSchema`.

4. **Routing: `[id]` wins.** `pages/pets/[id].vuka.html` is `/pets/{id}`;
   a `[id]/` bracket folder holds a segment shared by a layout, several
   pages and an `api/` subtree, and nests for nested parameters;
   `[path...]` is the catch-all. `params.go` parameter folders and their
   `Params` structs are removed; each is an error naming the bracket
   form.

5. **No migration command.** There is no `vuka fix one-way`: an app moves
   to 2.0 by hand, guided by the errors. Each retired form is a compile
   error, a `vukavet` diagnostic or a start-up error whose help names the
   one way to write it (E0151–E0153 for markup outside a `.vuka.html`
   file's one struct, E0720, E0721 and W0702 for pages outside `pages/`, W0106 for
   a hand-written `ui.HTML`, E0714–E0716 for 1.x folder forms, E0420 for
   `@web.Service`/`inject:`, E0337–E0338 for by-name and constructor
   hooks).

## 1. Motivation

Today a person building a page in vuka chooses among several equally
supported spellings for the same thing, and the docs teach every one:

- **Where markup lives.** JSX inside a `.vuka` file, a `.vuka.html`
  template beside a `.go` struct, or `ui.HTML(...)` strings written by hand
  in Go. Three authoring surfaces for one node tree.
- **Where a component's type lives.** The struct in `foo.go`, its markup in
  `foo.vuka.html` — two files that must be kept in step by convention.
- **How many templates a file holds.** A `.vuka.html` file may carry a
  headerless main template plus any number of `func` partials and function
  components.
- **How client state is written.** `ui.State(v)` with `Toggle`, `Inc`,
  `Dec`, `Reset`, `Derived`, `At` in `.vuka`; `ui.UseState` /
  `ui.UsePageState` and the same methods in Go.
- **How a page's hooks are declared.** `Mount`/`Changed` found by name, a
  `LiveSchema` method, or hooks set in the constructor — three ways, with a
  `vuka fix` that has moved apps between them more than once.
- **How a route is declared.** A `@web.Get`/`@web.Live` decorator, a
  `web.Routes.*` call, an `app.Get` call, or a folder under `pages/`.
- **How a dynamic segment is named.** A folder plus a `params.go` declaring
  a `Params` struct.

Every one of these is defensible on its own. Together they are the first
thing a newcomer has to navigate and the thing that makes two vuka
codebases look unalike. This RFC proposes that vuka 2.0 pick **one** way for
each and retire the rest as authoring surfaces, keeping them only where they
already serve as the compiler's output or as interop with other packages.

The rule is **one file type per job**:

- `.vuka.html` — anything that renders: pages, layouts, components, emails.
  One struct per file; the Go that belongs to that struct (imports, fields,
  decorated hooks, methods) sits in a header above the markup.
- `.vuka` / `.go` — logic only, never markup: services, models, stores,
  helpers, `main`.
- Routing comes from the folder tree under `pages/`: a file's path is its
  URL, `[id]` in a name is a path parameter, and `api/` holds endpoint
  functions.

Nothing about the language's expression-level features changes: `Result`,
`Option`, `?`, `match`, enums, overloading and decorators as a language
mechanism are untouched. This RFC is about the surface for building UI and
wiring routes, not about Go-the-language-with-sugar.

### What stays, verbatim

To be clear about scope, these keep working exactly as in 1.x and are
**not** part of the proposal to remove:

- Go statements in markup (`if`, `for`, `switch`, `x := …`), holes, and
  the implicit render `ctx` (shipped v1.4.0).
- `class:x={…}`, `{attrs...}`, `bind:value`, `style:prop={…}`.
- `<style scoped>` and `<script lang="ts">` / `<script is:inline>` on a
  template.
- `ui.Show`, keyed `ui.Each` loops, and the JS command chains
  (`ui.JS.…`, `view`-style predictions, `Push`, `Busy`, `When`).
- Importing a `.ts`/`.tsx`/`.vue` island and using it as a tag; `.templ`
  interop through `templx`.
- The live protocol (v1/v2 render trees), presence/streams and everything
  under `ui/live`.

## 2. Compatibility and versioning

This is a **breaking major release**. The
[compatibility promise](/project/compatibility) forbids removing or
renaming any covered name within v1, so none of the removals below can ship
in a 1.x minor. Under the promise, a v2 is a **new module path**
(`github.com/vuka-lang/vuka/v2`, `…/ui/v2`, `…/web/v2`), so every 1.x app
keeps building untouched, and each breaking change ships with a `vuka fix`
rewrite that `vuka upgrade` runs when an app crosses the release, plus a
release-note before/after.

### What this removes, and when it entered the API

Everything named here is a **documented authoring surface being retired**.
Each row stays available to the compiler as a lowering target and to other
packages as interop — it simply stops being a way a person writes vuka, and
`vukavet` warns on hand-written uses.

| Removed as a way to write | Entered | Replaced by |
|---|---|---|
| Hand-written `ui.HTML(markup, args…)` | ui package; public & promised from **v1.0.0** | `.vuka.html` markup (compiler still emits `ui.HTML`) |
| JSX inside a `.vuka` file | the original `.vuka` form (pre-1.0) | `.vuka.html` markup |
| Several `func` templates / components in one `.vuka.html` | **v1.0.0** (template files) | one struct per file; a partial becomes its own component file or a Go func returning `ui.Node` |
| A render struct in a sibling `.go`/`.vuka` while markup is in `.vuka.html` | **v1.0.0** | the struct's Go moves into the `.vuka.html` header |
| `ui.UseState` / `ui.UsePageState` (Go) | client state **v0.29.0**, Go names promised from **v1.0.0** | `ui.State[T]` field or package var, read as a plain variable |
| `ui.State.Toggle/Inc/Dec/Reset/Derived/At` | **v0.29.0**, Go names from **v1.0.0** | plain reads/writes in markup; compiler emits the chain (kept unexported if lowering needs them) |
| Public `ui.Assign[T]` (`Get`/`Set`/`Update`) as a field a person types | change tracking **v1.0.0** | unexported fields auto-lower to `ui.Assign` |
| `LiveSchema(l *ui.LiveSchema)` method | **v1.0.0** | `@ui.Mount` / `@ui.OnChange` / `@ui.Loads` and field attrs (compiler may still emit `LiveSchema`) |
| Constructor hooks `OnMount` / `Loads` / `OnChange` on `ui.Live` | **v1.1.0** | the same decorators |
| `Mount` / `Changed` found by name | pre-1.0; deprecated at **v1.0.0**, restored as the simple form at **v1.2.0** | `@ui.Mount` / `@ui.OnChange` (decided: decorator-only) |
| `@web.Service`, `inject:"-"` field tag, `web.Params` | `web.Params` **v1.0.0**; `@web.Service` and `inject:` pre-1.0, promised from **v1.0.0** | `app.Service(ctor)` in Go; params filled by type |
| `@web.Get` / `@web.Live` / `web.Routes.*` / `app.Get` **for pages** | `web.Routes` **v1.0.0**; `@web.Live` **v0.44.0**; `@web.Get`-on-page **v0.30.0** | folder routes under `pages/` (compiler still emits `web.Routes.*`). `app.Get` stays for **APIs** in Go |
| The docs' Vuka / Go sample toggle | **v1.0.0** ("each page has a Vuka / Go switch") | one documented form per sample |

`web.Routes.*`, `ui.HTML`, `LiveSchema`, `ui.Assign` and the rest remain
**exported** in v2 — the generated code and interop depend on them — but
they leave the guide, and `vukavet` reports a hand-written use with a
did-you-mean to the one way.

### Migration path (decided)

2.0 is the only vehicle (Decision 1). `vuka upgrade` to 2.0 bumps the
import paths to `/v2`; every retired form in the table above is then an
error naming the one way to write it, and the app is moved by hand
(Decision 5); `vuka vet` and `vuka test` then run as for any upgrade. No 1.x release carries the one-way forms or warnings.

### Resolved decisions

The two items this draft left open are settled (see [Decisions](#decisions)):

**(a) Deprecation window vs hard-remove-now — resolved: hard removal in
2.0, no 1.6 bridge.** The draft recommended a bridge to honor the v1
promise for outside users; there are none, so the bridge would cost a
release and buy nothing.

**(b) Hook reversal — resolved: decorator-only.** `@ui.Mount`,
`@ui.OnChange` and `@ui.Loads` are the only hook forms. The draft
recommended keeping `Mount`/`Changed` by name as sugar because v1.2.0 had
restored them on purpose; the decision instead removes them, so a hook is
always marked where it is declared and an exported method is always an
event.

## 3. The `.vuka.html` file grammar

A `.vuka.html` file has two parts, in order: a **Go header**, then
**markup**. The boundary is the **first line whose first non-space
character is `<`** — that line begins the markup, and everything above it is
Go.

```html
<!-- pages/index.vuka.html -->
package pages                         ┐
                                      │
import (                              │
    "context"                         │  Go header:
                                      │  package, imports, ONE struct,
    "example.com/todo/services"       │  decorated hooks, methods.
    "github.com/vuka-lang/ui"         │
)                                     │
                                      │
type Index struct {                   │
    ui.Live                           │
    store *services.Todos             │
    filter ui.State[string]           │
}                                     │
                                      │
@ui.Mount                             │
func (p *Index) mount(ctx context.Context) error { … }  │
                                      │
func (p *Index) Add(ctx context.Context, f NewTodo) error { … }  ┘
<main>                                ┐
  <h1>Todo</h1>                       │  markup: begins at the first
  …                                   │  line starting with `<`
</main>                               ┘
```

### The one-struct rule

A `.vuka.html` file declares **exactly one struct** in its header. That
struct is what the file renders; the markup is its view. The struct may be
named anything — the file name and the struct name need not match (though
`vuka new`/`vuka add` will keep them aligned by default).

- **No struct in the header is an error** (proposed `E07xx`, "a
  `.vuka.html` file renders one struct; this file declares none").
- **More than one struct is an error** ("a `.vuka.html` file renders one
  struct; move `Foo` to its own file or to a `.go` file"). Types that are
  not render structs — a form-input DTO, an enum — belong in a `.vuka`/`.go`
  file beside it, or may be declared as unexported helpers only if they
  carry no markup; the recommended rule is: **data types live in logic
  files**.
- The header is ordinary Go, checked by the Go compiler; its imports are the
  file's, and the package's other `.go` imports are visible to the markup as
  today.
- A file with a header but no markup is not a render file; it is just Go, so
  write it as `.go`/`.vuka` (an error points this out).

This replaces today's split (struct in `foo.go`, markup in
`foo.vuka.html`) and today's multi-template file (a headerless main template
plus `func` partials). A partial becomes **its own component file** or a Go
func returning `ui.Node` in a logic file.

### Field classification

The struct's fields are classified by export and type:

| Field | Role | Lowering |
|---|---|---|
| **Exported** (`Text string`, `ID int64`) | a **prop**. On a page, filled from the matching `[id]` path segment, else the query parameter; on a component, set by the parent tag's attribute. | bound as a routed live page's / component's exported fields are today |
| **Unexported, not `ui.State`** (`store *Todos`, `rows []Todo`) | **server state**: injected if the type is provided, otherwise tracked state the server owns | a non-dependency unexported field auto-lowers to `ui.Assign[T]`, read with `.Get()` / written with `.Set(v)` by the compiler |
| **`ui.State[T]`** (`filter ui.State[string]`) | **browser state**: the browser keeps it; reads follow it with no round trip | stays `ui.State[T]`; see §state below |
| **Embedded `ui.Live`** | marks the struct live | as today |
| **Tagged `` `vuka:"-"` ``** | state the render does not read | excluded from change tracking, as today |

A dependency (a provided type, a pointer/interface the container fills)
unexported field is injected by the generated constructor, exactly as the
DI rules describe today; it is not auto-Assigned. Everything else unexported
becomes an `ui.Assign` so the page gets spot-level change tracking for free
— which is why `Render` may read only props, Assigns and `vuka:"-"` fields
(the existing verification rule, now universal).

### Live vs static

A struct is **live** when it embeds `ui.Live` **or** declares any event
handler or hook (a method the markup wires to an `on…` attribute, or a
`@ui.Mount`/`@ui.OnChange`/`@ui.Loads`). Otherwise it is **static**: built
and rendered per request, its `on…` attributes that are JS command chains
still run in the browser, but it holds no session and its `Push`es do
nothing.

> This is a change from 1.x, where liveness is "embeds `ui.Live` and has
> `Render`". Inferring live from the presence of a server handler removes
> the need to remember to embed `ui.Live`; a component that only runs JS
> command chains (no `Push`) stays static. See §Conflicts.

### Props, children, slots

- Content between a component's tags fills a `Children ui.Node` field.
- `<slot:actions>…</slot:actions>` fills an `Actions ui.Node` field.
- A component is used as a **tag**, capitalised or dotted
  (`<components.Item …/>`), its attributes bound to exported fields by name
  (camelCase: `done` → `Done`).

These are unchanged from today's template rules; the one-way change is only
that the component's struct is declared in the same `.vuka.html` header
rather than a sibling `.go`.

## 4. Lowering table

Each source form on the left is what a person writes in a `.vuka.html`; the
right is the Go the compiler emits (through the overlay for
`vuka run`/`build`/`test`, or on disk with `vuka gen`). None of the emitted
API changes — it is today's public API — so an ejected 2.0 app is plain Go.

| # | Source form (one-way) | Generated Go |
|---|---|---|
| 1 | File header: `type Index struct { ui.Live; store *Todos; rows []Todo }` + markup | `type Index struct { ui.Live; store *Todos; rows uigen.Assign[[]Todo] }`; `func (p *Index) Render() ui.Node { return ui.HTML(``…``, …) }`; constructor `NewIndex(store *Todos)` |
| 2 | unexported non-dep field `rows []Todo` | `rows uigen.Assign[[]Todo]`; markup `{rows}` → `p.rows.Get()`; `p.rows = v` → `p.rows.Set(v)`; `p.n += 2` → `p.n.Set(p.n.Get() + (2))`; `p.n++` → `p.n.Set(p.n.Get() + 1)`; `p.rows[i].Done = true` → `p.rows.Update(func(rows []Todo) []Todo { rows[i].Done = true; return rows })`; `&p.rows` is E0150 |
| 3 | `ui.State[string]` field `filter`, read `{filter}`, `{!open && tab == "env"}` in markup | field kept `ui.State[string]`, given its state before the component mounts (`uigen.States` before a static one's View); a read is the state, an expression `uigen.And(uigen.Not(p.open), uigen.Eq(p.tab, "env"))`; package-level `var theme ui.State[string]` → `= uigen.UsePageState(*new(string))` |
| 4a | write in `on…`: `onClick={editing = !editing}` (bool) | `onClick={uigen.Toggle(ui.JS, p.editing)}`; `n++`/`n--` → `uigen.Inc(ui.JS, p.n)` / `uigen.Dec` |
| 4b | write in `on…`: `onClick={tab = "env"}` | `onClick={ui.JS.Set(p.tab, "env")}` |
| 4c | writes in one `on…`: `onClick={tab = "env"; open = false}` | `onClick={ui.JS.Set(p.tab, "env").Set(p.open, false)}` (chained, order preserved) |
| 4d | write + server call: `onClick={open = false; Save()}` | `onClick={ui.JS.Set(p.open, false).Push(p.Save)}`; a call with arguments `Push(func() { p.Remove(t.ID) })` |
| 4e | write in a live component's method: `p.editing = false` | `p.PushJS(p.editing.Set(false))`; `p.open = !p.open` → `p.PushJS(uigen.Toggle(ui.JS, p.open))`; anywhere else E0149 |
| 5 | `@ui.Mount func (p *Index) mount(ctx) error` | `func (p *Index) LiveSchema(l *ui.LiveSchema) { l.Mount(p.mount) }` (compiler may still emit `LiveSchema`) |
| 5b | `@ui.OnChange` / `@ui.Loads("lists")` / field `@ui.Bind` `@ui.URL("q")` `@ui.Defer("lists")` | the same `l.OnChange(…)`, `l.Loads("lists", …)`, `l.Field(&p.Q, ui.Bind{…}, ui.URL("q"), ui.Defer("lists"))` |
| 6 | file `pages/pets/[id].vuka.html` | `web.Routes.Get("/pets/{id}", NewIndex, web.PageRoute(), web.LayoutOf[…]())` with `ID` bound from `{id}`; or `web.Routes.Live(…)` when the struct is live |
| 6b | `pages/_layout.vuka.html` | `web.LayoutOf[Layout]()` on every route in and below the folder, outermost-first |
| 6c | `pages/api/todos.go` exporting `func List(ctx, q Query) ([]Todo, error)` | `web.Routes.Get("/api/todos", List, web.Params(…))` |
| 7 | component tag `<components.Item todo={t} />` | `components.Item{Todo: t}` (a `ui.Node`); in a keyed loop, computed into a variable |
| 7b | `<components.Item …>text</…>` and `<slot:actions>…</slot:actions>` | `Children:` and `Actions:` fields of the literal |
| 8 | handler/hook parameter `f NewTodo`, `id int`, `r *http.Request`, `u auth.User`, `s *Store` | filled by type at the call site; a struct binds the form/JSON, a scalar the event value, the rest from request or container (see §5) |

`uigen` (`github.com/vuka-lang/ui/uigen`) is the one package generated
code calls that isn't documented API: what `ui.State`'s methods other than
`Get`/`Set` became (step 3), public so generated code builds with `go
build`, reported by `vukavet` when written by hand (W0105). The chain
examples (4a–4d) are exactly the shapes the client-state
doc already lowers; one-way only changes the *source spelling* from method
calls (`open.Toggle()`) to assignments (`open = !open`) inside `on…`.

## 5. Typed parameter injection

A hook or handler declares what it needs by **parameter type**; the runtime
fills each parameter from the one source its type names. This replaces
`web.Params` (positional scalar names), `@web.Service`/`inject:` and the
per-transport payload rules with one rule that reads the same for a route
handler, a page method handler, and a live event handler.

| Parameter type | Filled from |
|---|---|
| `context.Context` | the request's / session's context |
| a **struct** or `*struct` that isn't provided | the form or JSON body (routes/handlers) or the event's form fields (live), bound by `form:`/`json:` tag then name, then `validate:` checked |
| `url.Values` (or `map[string][]string`) | the submitted form fields as sent |
| a **scalar** (`string`, ints, floats, `bool`, `encoding.TextUnmarshaler`) | the path segment of the matching name, else the event value (a key for a keyboard event, else the element's value), parsed |
| `*http.Request`, `http.ResponseWriter` | the request |
| `auth.User` (or the app's identity type) | the signed-in visitor |
| any type registered with `app.Service(ctor)` / `web.Provide(ctor)` | the container |

- Names map **by type first**. Where two parameters share a type that needs
  a name (two path scalars), they map positionally onto the trailing
  parameters, as the segment order gives them — the compiler reads the
  parameter names from source in a `.vuka.html` header (it has them, unlike
  reflection), so no `web.Params` list is needed.
- **An unfillable parameter is a startup error**, reported with the file,
  line and the offending parameter's type: e.g.
  `index.vuka.html:41: Add's parameter db *sql.DB is not provided; register it with app.Service or inject a provided type` and
  `item.vuka.html:12: Toggle's parameter n chan int can't come from a request`.
  This is the same class of Build error that route binding raises today
  (a scalar with no name, a second body struct, a type a request can't
  carry), extended to hooks and live handlers and surfaced by `vukavet`.
- `validate:` tags on a bound struct are checked on every transport, exactly
  as they are for routes and live events today (422 with per-field
  messages).

Services and models are **logic**, so they live in `.go`/`.vuka` files and
register with `app.Service(NewTodos)` (or `web.Provide`) — there is no
`@web.Service` decorator on a struct and no `inject:` tag, because a struct
with markup is a render struct in a `.vuka.html`, and a struct without
markup is plain Go with a hand-written constructor whose parameters are its
dependencies.

## 6. Folder routing and layouts

Routing comes from the `pages/` tree, as it does from v1.1.0 — a folder is a
path segment, `Index` serves the folder's own path, a page type's name adds
its kebab-case segment, `api/` holds endpoints, and `vuka gen` writes the
`web.Routes.*` calls, the link package and the route table. One-way changes
how a **dynamic segment** and a **layout** are spelled.

### The `[id]` filename/foldername parameter

A name wrapped in brackets is a path parameter:

```text
pages/
  _layout.vuka.html            the document — /*
  index.vuka.html              Index       — /
  pets/
    index.vuka.html            the list    — /pets
    new.vuka.html              New         — /pets/new
    [id].vuka.html             one pet     — /pets/{id}
  files/
    [path...].vuka.html        catch-all   — /files/{path...}
  api/
    todos.go                   List/Create — GET,POST /api/todos
```

- `[id].vuka.html` serves `/pets/{id}`. The parameter's **Go type and
  `validate:` rules come from the page struct's matching exported field**
  (`ID int64 \`validate:"min=1"\``), so the type that today lives in a
  `params.go` `Params` field lives on the page that uses it. A segment that
  doesn't parse or validate is **404**, as today.
- `[path...].vuka.html` is the catch-all (`web.Rest`), the end of its
  branch, and the single-page-app shell.
- A **bracket folder** `[id]/` groups several files that share one segment
  (a `_layout.vuka.html`, several pages, `api/` endpoints under it). The
  shared field is declared once on the folder's `_layout.vuka.html` (or, if
  there is none, inferred from the pages in it); nested parameters stack as
  the folders nest.

### `_layout.vuka.html`

A `_layout.vuka.html` in a folder wraps every page in and below it,
composing outermost-first, and is an ordinary one-struct render file with a
`Children ui.Node` field; its `@ui.Loads`/`Load`-equivalent fetches what it
shows and guards the subtree, exactly as `layout.go`'s `Load` does today.
The leading underscore keeps it from being mistaken for a page named
`Layout` and reads as "not a route of its own."

> Guards and middleware: the RFC keeps today's `guard.go` /
> `middleware.go` (logic files, no markup) as-is — they are logic, so they
> stay `.go`. Only the **layout** moves into a `.vuka.html` because it
> renders. An alternative is `_guard.vuka`/`_middleware.vuka` naming for
> symmetry; the RFC recommends leaving them as `guard.go`/`middleware.go`
> since they carry no markup and the one-way rule is about *markup* files.

### Where `[id]` diverges from today's `params.go` folders, and which wins

Today (v1.1.0+), a dynamic segment is a **folder** named for the resource
holding a `params.go` that declares `type Params struct` with one field
named like the folder; nested parameter folders **embed** the parent's
`Params`; pages and layouts get the parameters by embedding their package's
`Params`. One-way proposes a **bracketed name** on the file (or folder) and
no `Params` struct at all.

| Concern | Today (`params.go` folder) | One-way (`[id]`) |
|---|---|---|
| Segment name | the folder's name (`employer/` → `{employer}`) | the bracket (`[id]` → `{id}`) |
| Parameter type | the `Params` field's type | the page struct's matching exported field's type |
| `validate:` | on the `Params` field | on the page struct's field |
| Nesting | child `Params` embeds parent's `Params` | bracket folders stack |
| Shared across sibling pages/endpoints | one `Params` struct embedded by all | one `_layout.vuka.html` field, or a bracket folder |
| Catch-all | `web.Rest` field in `Params` | `[path...]` name |

> **Decided (Decision 4): `[id]` wins, lowering to the existing
> folder-route machinery.** The bracketed name reads the URL at a glance and
> removes the `params.go` boilerplate and the embedding chain; the parameter
> type and rules move onto the struct that uses them. 2.0 removes
> `params.go` parameter folders (E0714, whose help names the bracket
> folder or file and the struct the `Params` field moves to). The generated registration (`web.PageRoute()`, `LayoutOf`,
> `GuardOf`, `UseOf`, typed `pages/urls`) is unchanged. A bracket **folder**
> covers what a single file can't: a segment shared by a layout, several
> pages and an `api/` subtree, nested parameters, and the `[path...]`
> catch-all (`web.Rest`).

This was the one place the one-way syntax conflicted with shipped 1.x
behavior rather than retiring a redundant spelling; it is resolved in its
favor.

## 7. Worked example — a todo app, every file

Logic in `.go`, renders in `.vuka.html`, routes from `pages/`.

```go
// models/todo.go — logic only
package models

type Todo struct {
	ID   int
	Text string
	Done bool
}

// NewTodo is the add form.
type NewTodo struct {
	Text string `form:"text" validate:"required,max=120"`
}
```

```go
// services/todos.go — logic only, registered with app.Service
package services

import (
	"slices"
	"sync"

	"example.com/todo/models"
)

type Todos struct {
	mu   sync.Mutex
	next int
	rows []models.Todo
}

func NewTodos() *Todos { return &Todos{} }

func (s *Todos) All() []models.Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Todo(nil), s.rows...)
}

func (s *Todos) Add(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.rows = append(s.rows, models.Todo{ID: s.next, Text: text})
}

func (s *Todos) Toggle(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Done = !s.rows[i].Done
		}
	}
}

func (s *Todos) Remove(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = slices.DeleteFunc(s.rows, func(t models.Todo) bool { return t.ID == id })
}
```

```go
// main.go — logic only; pages are linked, not imported by hand
package main

import (
	"log"

	_ "example.com/todo/pages" // vuka gen writes this link; shown for plain go build
	"example.com/todo/services"
	"github.com/vuka-lang/web"
)

func main() {
	a := web.New(web.Provide(services.NewTodos))
	if err := a.Run(""); err != nil {
		log.Fatal(err)
	}
}
```

```html
<!-- pages/_layout.vuka.html — the document, wraps every page -->
package pages

import "github.com/vuka-lang/ui"

type Layout struct {
	Children ui.Node
}
<html lang="en">
  <head><title>{ui.PageTitle(ctx)}</title></head>
  <body>
    <main>{Children}</main>
  </body>
</html>
```

```html
<!-- pages/index.vuka.html — the live list at / -->
package pages

import (
	"context"

	"example.com/todo/components"
	"example.com/todo/models"
	"example.com/todo/services"
	"github.com/vuka-lang/ui"
)

type Index struct {
	ui.Live
	store *services.Todos    // injected (provided type)
	rows  []models.Todo      // server state → ui.Assign
	show  ui.State[string]   // browser state: "all" | "active" | "done"
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows = p.store.All()
	return nil
}

func (p *Index) Add(ctx context.Context, f models.NewTodo) error {
	p.store.Add(f.Text)
	p.rows = p.store.All()
	return nil
}

func (p *Index) Toggle(id int) {
	p.store.Toggle(id)
	p.rows = p.store.All()
}

func (p *Index) Remove(id int) {
	p.store.Remove(id)
	p.rows = p.store.All()
}
<section id="todo">
  <h1>Todo</h1>

  <form onSubmit={p.Add}>
    <input name="text" placeholder="What next?" />
    <button>Add</button>
  </form>

  <nav>
    <button onClick={show = "all"} aria-selected={show == "all"}>All</button>
    <button onClick={show = "active"} aria-selected={show == "active"}>Active</button>
    <button onClick={show = "done"} aria-selected={show == "done"}>Done</button>
  </nav>

  <ul>
    for _, t := range p.rows {
      <li hidden={(show == "active" && t.Done) || (show == "done" && !t.Done)}>
        <components.Item
          todo={t}
          onToggle={func() { p.Toggle(t.ID) }}
          onRemove={func() { p.Remove(t.ID) }}
        />
      </li>
    }
  </ul>
</section>
```

```html
<!-- components/item.vuka.html — one row, a component used as a tag -->
package components

import "example.com/todo/models"

type Item struct {
	Todo     models.Todo
	OnToggle func()
	OnRemove func()
}
<div class="item" class:done={Todo.Done}>
  <label>
    <input type="checkbox" checked={Todo.Done} onChange={OnToggle} />
    {Todo.Text}
  </label>
  <button onClick={OnRemove}>×</button>
</div>
```

Notes on the example against the rules:

- `models/todo.go` and `services/todos.go` carry no markup, so they are
  logic files; `Todos` registers with `web.Provide(services.NewTodos)` in
  `main.go` — no `@web.Service`, no `inject:` tag.
- `Index` is **live** because it embeds `ui.Live` and has handlers; `store`
  is injected (provided type), `rows` is server state the compiler lowers to
  `ui.Assign[[]models.Todo]`, and `show` is browser state read in markup as
  a plain variable and written in `on…` attributes (`show = "all"` →
  `p.show.Set("all")`).
- `Item` is **static** (no `ui.Live`, no server handler of its own — its
  `OnToggle`/`OnRemove` are func props the parent passes). It is used as the
  tag `<components.Item …/>`, its exported fields bound by attribute name.
- `_layout.vuka.html` wraps every page; `index.vuka.html` is the folder's
  `Index` at `/`.
- Every hook/handler parameter is filled by type: `ctx`, the `NewTodo` form
  struct (validated), and the `int` the closures capture.

## 8. Deliverables and sequencing

Seven steps, each its own reviewable PR with its own tests, all shipping in
2.0 (Decision 1: no 1.x bridge). Until step 7 the old forms keep compiling,
so every step lands with the existing suites green; step 7 removes them and
moves the module paths.

| # | Step | Tests |
|---|---|---|
| 1 | **This RFC.** | — |
| 2 | **The `.vuka.html` file shape**: the Go header up to the first line starting with `<`; the one-struct rule (none, or more than one, is an error at file:line); field classification (exported = prop, unexported = server state, `ui.State[T]` = browser state); live-vs-static detection; header + markup lowered to the Go today's pipeline produces for a struct with its template. The struct-first split and multi-template files still compile. | golden `.vuka.html` → generated Go for each rule; error goldens for no struct, two structs, a header with no markup. |
| 3 | **Client state**: `ui.State[T]` reduced to `Get`/`Set`; plain-variable reads in markup; assignments in `on…` lowered to the chain (4a–4d); `p.x = v` on a state in a method lowered to `PushJS` (4e); server-state fields lowered to `uigen.Assign` (§3). | lowering goldens per chain shape; a coherence test that the emitted chain equals the method-built one; live verification on auto-Assign pages, including ones reading `ctx`. |
| 4 | **Hooks decorator-only**: `@ui.Mount`, `@ui.OnChange`, `@ui.Loads`, `@ui.Bind`, `@ui.URL`, `@ui.Defer` as the only forms in a `.vuka.html` header. | lowering to `LiveSchema` unchanged; error goldens for a by-name `Mount`/`Changed` and a constructor hook (did-you-mean `@ui.Mount`). |
| 5 | **Folder routing**: `[id]` files, `[id]/` folders, `[path...]`, type and `validate:` from the struct's field, `_layout.vuka.html`, `api/` method funcs. | `vuka routes` goldens; 404 on a bad segment; layout order and `Load`; E07xx conflicts. |
| 6 | **Typed parameter injection** for hooks and handlers (§5); unfillable parameter = startup error with file, line and type. | a test per source (ctx, struct, `url.Values`, scalar, `*http.Request`, `auth.User`, provided type); unfillable-parameter errors; `validate:` on every transport. |
| 7 | **The break**: remove every surface in the table of §2 (JSX in `.vuka`, hand-written `ui.HTML`, several templates per file, the struct/markup split, `UseState`/`UsePageState`/`State` methods, public `LiveSchema` authoring, constructor and by-name hooks, `@web.Service`/`inject:`/`web.Params`, page routes outside `pages/`, `params.go` folders, the docs toggle); `/v2` module paths for vuka, cmd/vuka, cmd/vukavet, ui, web, and orm. | an error per retired form, with its one-way help; the maintainer's apps and the conformance apps migrated by hand and diffed against their 1.x responses, SQL and live replies. |

## 9. Conflicts with what 1.x shipped — resolved

Each conflict this draft raised, with how the decisions settle it.

1. **The Go-first authoring path v1.0.0 made central** — *resolved:
   retired* (Decision 2). Rendering is `.vuka.html`-only; Go stays the
   lowering target and the language of logic. The conformance suite's
   hand-written-Go builds of rendering code go with it in step 7; the
   ejected-Go builds stay, as they test the lowering.
2. **`[id]` vs v1.1.0's `params.go` folders** — *resolved: `[id]` wins*
   (Decision 4); `params.go` folders are removed, each an error naming the
   bracket form.
3. **One struct per file vs v1.0.0 template files** — *resolved: one
   struct per file.* Step 2 accepts the new shape beside the old; step 7
   makes it the only one: a multi-template file is an error (E0153) whose
   help splits it into component files.
4. **Auto-`ui.Assign` changes render semantics** — *accepted*, landing in
   step 3 with live verification on, including pages reading `ctx` (v1.4.0
   re-renders them regardless of Assigns).
5. **"Has handlers ⇒ live" moves the static/live boundary** — *accepted*,
   landing in step 2: a struct is live when it embeds `ui.Live`, declares a
   hook, or has an exported method the markup wires to an `on…` attribute.
   A JS-command-only `on…` keeps it static.
6. **Decorator-only hooks reverse v1.2.0** — *resolved: decorator-only*
   (Decision 3).
7. **Retiring `@web.Service`/`inject:`/`web.Params`** — *accepted* (step 6
   adds injection by type, step 7 removes them); it matches Go-side DI.

## 10. Out of scope

The expression language is untouched: `Result`/`Option`, `?`, `match`,
enums, overloading and decorators-as-a-language-mechanism keep their syntax
and meaning. Guards (`guard.go`), middleware (`middleware.go`), the live
wire protocol, islands, `templx` interop, auth, the orm's API (it moves to
`/v2` import paths only) and the toolbar are unchanged except where a render file replaces a markup-bearing one
(`_layout.vuka.html` for `layout.go`). This RFC does not propose any change
to how the binary is built (`go build`, `vuka run`/`build`/`gen`) beyond the
new parser mode and the `[id]` route generation.
