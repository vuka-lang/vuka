# RFC: One way to write UI (vuka 2.0)

Status: draft, for review. No code changes accompany this document; it is
the artifact to agree on before any implementation.

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
| `Mount` / `Changed` found by name | pre-1.0; deprecated at **v1.0.0**, restored as the simple form at **v1.2.0** | `@ui.Mount` / `@ui.OnChange` — see open decision (b) |
| `@web.Service`, `inject:"-"` field tag, `web.Params` | `web.Params` **v1.0.0**; `@web.Service` and `inject:` pre-1.0, promised from **v1.0.0** | `app.Service(ctor)` in Go; params filled by type |
| `@web.Get` / `@web.Live` / `web.Routes.*` / `app.Get` **for pages** | `web.Routes` **v1.0.0**; `@web.Live` **v0.44.0**; `@web.Get`-on-page **v0.30.0** | folder routes under `pages/` (compiler still emits `web.Routes.*`). `app.Get` stays for **APIs** in Go |
| The docs' Vuka / Go sample toggle | **v1.0.0** ("each page has a Vuka / Go switch") | one documented form per sample |

`web.Routes.*`, `ui.HTML`, `LiveSchema`, `ui.Assign` and the rest remain
**exported** in v2 — the generated code and interop depend on them — but
they leave the guide, and `vukavet` reports a hand-written use with a
did-you-mean to the one way.

### Recommended migration path

1. **A final 1.x minor (call it 1.6)** ships the one-way *additions* that
   are safe under the promise (they add API and accept new file shapes
   without removing anything): the `.vuka.html` Go-header grammar as an
   accepted shape, `[id]` filename routing as an alternative to `params.go`,
   and typed parameter injection for hooks and handlers. It also ships
   `vuka fix one-way`, a rewrite that moves a 1.x app onto the one-way
   forms, and `vukavet` **warnings** on every retired surface (new warnings
   are allowed in a minor and never fail a build).
2. **2.0** removes the retired surfaces, flips the defaults (one struct per
   file becomes mandatory; unexported render fields auto-lower to
   `ui.Assign`), and `vuka upgrade` to 2.0 runs `vuka fix one-way` across
   the import-path bump.

This keeps the promise intact (nothing is removed inside v1), gives a
mechanical upgrade, and lets teams adopt the one-way forms before the break.

### Open decisions for the reviewer

These two are deliberately left for review; the RFC recommends but does not
decide them.

**(a) Deprecation window vs hard-remove-now.** Options: ship the additive
1.6 + `vuka fix one-way` + warnings first (above), or make 2.0 the only
vehicle and skip the 1.6 bridge.

> **Recommendation: the deprecation window.** It is the only option that
> honors the v1 promise (no removals in 1.x) and still lets apps move early;
> the bridge release costs one minor but turns the 2.0 upgrade into a
> `vuka upgrade` with the fixers already exercised. A hard cut with no 1.6
> bridge leaves every app's first contact with one-way being a module-path
> break, which is riskier for no real saving.

**(b) The hook reversal: decorator-only vs keep `Mount`-by-name.** The brief
calls for `@ui.Mount` / `@ui.OnChange` / `@ui.Loads` to be the sole way, and
`Mount`/`Changed` found by name to go. Note the history: `Mount`/`Changed`
by name were deprecated at v1.0.0, then **deliberately restored at v1.2.0**
as the "simple form" precisely because the decorator felt heavy for the
common case.

> **Recommendation: keep `@ui.Mount` as the canonical form, but treat a
> method literally named `Mount` / `Changed` as recognized sugar the
> compiler lowers to the same hook** — identical semantics, uncallable from
> the browser, params filled by type. This gives the document exactly one
> *concept* (a hook is a hook) while not reversing the ergonomics win of
> v1.2.0 or breaking muscle memory. If the reviewer wants the stricter
> reading (decorator-only, by-name is an error with a did-you-mean), that is
> a one-line change to the lowering and is also defensible — it is simply
> stricter than v1.2.0 chose to be. `@ui.OnChange` and `@ui.Loads` have no
> by-name form either way, since `Changed`-by-name is the only one that ever
> existed and `Loads` never did.

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
| 1 | File header: `type Index struct { ui.Live; store *Todos; rows []Todo }` + markup | `type Index struct { ui.Live; store *Todos; rows ui.Assign[[]Todo] }`; `func (p *Index) Render() ui.Node { return ui.HTML(``…``, …) }`; constructor `NewIndex(store *Todos)` |
| 2 | unexported non-dep field `rows []Todo` | `rows ui.Assign[[]Todo]`; markup `{rows}` → `p.rows.Get()`; `p.rows = v` in a method → `p.rows.Set(v)` |
| 3 | `ui.State[string]` field `filter`, read `{filter}` in markup | field kept `ui.State[string]`; hole takes the state as-is (browser keeps it current) |
| 4a | write in `on…`: `onClick={editing = !editing}` (bool) | `onClick={p.editing.Toggle()}` |
| 4b | write in `on…`: `onClick={tab = "env"}` | `onClick={p.tab.Set("env")}` |
| 4c | write in `on…`: `onClick={open = false}` plus `tab = "env"` in one handler | `onClick={p.tab.Set("env").Set(p.open, false)}` (chained, order preserved) |
| 4d | write + server call: `onClick={open = false; Save()}` | `onClick={p.open.Set(false).Push(p.Save)}` |
| 4e | server-side write in a method: `p.editing = false` | `p.PushJS(p.editing.Set(false))` |
| 5 | `@ui.Mount func (p *Index) mount(ctx) error` | `func (p *Index) LiveSchema(l *ui.LiveSchema) { l.Mount(p.mount) }` (compiler may still emit `LiveSchema`) |
| 5b | `@ui.OnChange` / `@ui.Loads("lists")` / field `@ui.Bind` `@ui.URL("q")` `@ui.Defer("lists")` | the same `l.OnChange(…)`, `l.Loads("lists", …)`, `l.Field(&p.Q, ui.Bind{…}, ui.URL("q"), ui.Defer("lists"))` |
| 6 | file `pages/pets/[id].vuka.html` | `web.Routes.Get("/pets/{id}", NewIndex, web.PageRoute(), web.LayoutOf[…]())` with `ID` bound from `{id}`; or `web.Routes.Live(…)` when the struct is live |
| 6b | `pages/_layout.vuka.html` | `web.LayoutOf[Layout]()` on every route in and below the folder, outermost-first |
| 6c | `pages/api/todos.go` exporting `func List(ctx, q Query) ([]Todo, error)` | `web.Routes.Get("/api/todos", List, web.Params(…))` |
| 7 | component tag `<components.Item todo={t} />` | `components.Item{Todo: t}` (a `ui.Node`); in a keyed loop, computed into a variable |
| 7b | `<components.Item …>text</…>` and `<slot:actions>…</slot:actions>` | `Children:` and `Actions:` fields of the literal |
| 8 | handler/hook parameter `f NewTodo`, `id int`, `r *http.Request`, `u auth.User`, `s *Store` | filled by type at the call site; a struct binds the form/JSON, a scalar the event value, the rest from request or container (see §5) |

The three chain examples (4a–4d) are exactly the shapes the client-state
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

> **Recommendation: `[id]` wins as the one way, lowering to the existing
> folder-route machinery.** The bracketed name reads the URL at a glance and
> removes the `params.go` boilerplate and the embedding chain; the parameter
> type and rules move onto the struct that actually uses them, which is
> where a reader looks. The 1.6 bridge accepts `[id]` *and* `params.go` so
> apps can migrate folder by folder; `vuka fix one-way` rewrites a
> `params.go` folder into a bracket folder/file and folds the `Params` field
> into the page struct; 2.0 removes `params.go` as an authoring form (the
> generated code still produces the same `web.PageRoute()` registration, so
> the lowering target is unchanged). Keep a bracket **folder** for the cases
> `params.go` handled that a single file can't: a segment shared by a
> layout, several pages and an `api/` subtree, and `web.Rest`.

This is the **one place the one-way syntax genuinely conflicts with shipped
1.x behavior** rather than merely retiring a redundant spelling, so it is
called out for the reviewer explicitly.

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

Seven independently reviewable steps, each with its own tests. "Additive"
steps are safe in the final 1.x minor (1.6) under the compatibility promise;
"breaking" steps are 2.0 only (new module path + `vuka fix one-way`).

| # | Step | Nature | Tests |
|---|---|---|---|
| 1 | **`.vuka.html` Go-header grammar**: parse the header (package, imports, one struct, decorated hooks, methods) up to the first `<` line; field classification (prop / server-state → `ui.Assign` / browser-state); live-vs-static inference; one-struct and no-struct errors. | **Additive** (a new accepted file shape; the old split still parses in 1.6). The *mandatory* one-struct rule and auto-`Assign` default are **breaking** → 2.0. | parser golden tests; a conformance app authored in the new shape renders byte-identical to its 1.x twin; error-code tests for no-struct / two-struct / header-without-markup. |
| 2 | **`ui.State` reduced to `Get`/`Set`**: plain-variable reads in markup, assignment writes in `on…` lowering to the chain (4a–4e), server-side `p.x = v` → `PushJS`. Remove `Toggle`/`Inc`/`Dec`/`Reset`/`Derived`/`At`/`UseState`/`UsePageState` from the public surface (keep unexported for lowering). | **Breaking** (removes exported names) → 2.0; the assignment *sugar* can land additively in 1.6. | lowering golden tests for each chain shape; goja coherence test that the emitted chain matches the current method-built chain; vukavet warning tests on hand-written `Toggle`/`UseState`. |
| 3 | **Hooks decorator-only**: `@ui.Mount`/`@ui.OnChange`/`@ui.Loads` and field attrs `@ui.Bind`/`@ui.URL`/`@ui.Defer` as the one way; retire `LiveSchema`, constructor hooks (`OnMount`/`Loads`/`OnChange`), and — per open decision (b) — `Mount`/`Changed` by name (or keep as recognized sugar). | **Breaking** → 2.0. Decorators already exist (additive since v1.0.0), so only the *removal* is new. | lowering to `LiveSchema` unchanged; boot/vukavet error tests (E0335/E0336 equivalents); a fixture per retired form that `vuka fix one-way` rewrites. |
| 4 | **Typed parameter injection** for hooks and handlers: fill by type; unfillable parameter = startup error with file/line/type. Retire `web.Params`, `@web.Service`, `inject:`. | **Additive** for the injection itself (1.6 can fill hooks/handlers by type); **breaking** for removing `web.Params`/`@web.Service`/`inject:` → 2.0. | per-source fill tests (ctx, struct body, url.Values, scalar event value, `*http.Request`, `auth.User`, provided type); unfillable-param error tests; validate-tag enforcement across route/page/live. |
| 5 | **Components as tags, children, slots, layouts**: `<components.Item …/>`, `Children`, `<slot:x>`, `_layout.vuka.html`. | **Additive** (`_layout.vuka.html` as an accepted layout file beside `layout.go` in 1.6); making `_layout.vuka.html` the one way is **breaking** → 2.0. | tag-binding tests already covered by templates; new tests for `_layout.vuka.html` composition order and `Load` equivalence to `layout.go`. |
| 6 | **Folder routing with `[id]`**: bracket filename/foldername parameters, type and `validate:` from the page struct's field, `[path...]` catch-all, `api/` method funcs. | **Additive** in 1.6 (`[id]` accepted *alongside* `params.go`); **breaking** when `params.go` is retired → 2.0. | route-table golden tests (`vuka routes`); 404-on-bad-segment tests; `vuka fix one-way` rewrites a `params.go` folder to a bracket folder/file; E07xx conflict tests. |
| 7 | **Retire the redundant authoring surfaces**: JSX in `.vuka`, hand-written `ui.HTML`, multiple templates per file, `@web.Get`/`@web.Live`/`web.Routes.*`/`app.Get` **for pages** (keep `app.Get` for APIs), and the docs' Vuka/Go toggle. Ship `vuka fix one-way` and `vukavet` warnings. | **Breaking** (removes authoring surfaces) → 2.0; the warnings + `vuka fix one-way` are **additive** → 1.6. | `vuka fix one-way` fixture suite (one per retired form); vukavet warning-code tests; docs build with one form per sample; conformance apps re-authored one-way and diffed against their 1.x responses/SQL/live replies. |

Steps 1, 4, 5 and 6 (and the `vuka fix`/vukavet parts of 2, 3, 7) are the
additive 1.6 bridge; the removals in 2, 3, 6 and 7, and the mandatory
defaults in 1, are the 2.0 break.

## 9. Conflicts with what 1.x already shipped

Flagged for the reviewer, worst first.

1. **One-way removes the Go-first authoring path that v1.0.0 made central.**
   v1.0.0's headline was "plain Go is the language, `.vuka` the sugar": an
   app can be `.go` + `.vuka.html` built with `go build`, every doc sample
   has a Go form, and the conformance suite builds each example five ways
   including hand-written Go. Retiring hand-written `ui.HTML`,
   `ui.UseState`, the `State` methods, `LiveSchema` and `web.Routes.*`-for-
   pages as *authoring surfaces* means a person can no longer write a
   rendering component in pure Go — those names survive only as the
   compiler's output and interop. This is a philosophy reversal and the
   biggest thing to agree on: one-way keeps Go as the *lowering target* but
   not as a *way to write UI*. (Logic in `.go` is unaffected.)

2. **`[id]` filename routing conflicts with v1.1.0's `params.go` folders.**
   Covered in §6: the shipped design is a folder + `Params` struct; one-way
   is a bracket name with the type on the page struct. This is a real
   behavioral divergence (not just a retired spelling), which is why the RFC
   recommends `[id]` wins, lowers to the same registration, and is bridged
   in 1.6 with a fixer.

3. **The one-struct-per-file rule conflicts with v1.0.0 template files.**
   Today a `.vuka.html` may hold a headerless main template plus any number
   of `func` partials and function components, and the render struct lives
   in a sibling `.go`. One-way forbids both: one struct, declared in the
   file's own header. Every multi-template file and every struct/markup
   split must be migrated (fixer territory).

4. **Auto-`ui.Assign` for unexported fields changes render semantics.**
   Today `ui.Assign` is opt-in and governs change-tracking/skip behavior;
   plain unexported fields are whole-render state. Auto-lowering every
   unexported non-dependency field to `ui.Assign` makes the "`Render` reads
   only props, Assigns and `vuka:"-"`" rule universal and makes skip
   behavior the default. Combined with v1.4.0 (a component reading the
   render `ctx` is re-rendered regardless of Assigns), the interaction needs
   explicit test coverage so a context-reading one-way page isn't wrongly
   skipped.

5. **"Has handlers ⇒ live" changes the static/live boundary.** Today
   liveness is "embeds `ui.Live` + `Render`". Inferring live from a server
   handler's presence is friendlier but means a component that gains a
   `Push`ing handler silently becomes live (holding a session); the compiler
   must distinguish a JS-command-only `on…` (stays static) from one that
   `Push`es (needs live), and report the transition so it isn't surprising.

6. **Hooks decorator-only reverses a deliberate v1.2.0 decision.** v1.2.0
   restored `Mount`/`Changed` by name as the simple form on purpose. Open
   decision (b) is exactly this reversal; the RFC recommends keeping by-name
   as recognized sugar to avoid re-litigating it.

7. **Retiring `@web.Service`/`inject:`/`web.Params` touches DI ergonomics.**
   These are small and mechanical (a service becomes a `.go` struct with a
   hand-written or generated constructor registered via `app.Service`), but
   `inject:"-"` has no one-way equivalent beyond "a field the constructor
   doesn't take isn't a dependency," which is already how Go-side DI works —
   so this aligns with the DI doc rather than fighting it, and is noted only
   for completeness.

## 10. Out of scope

The expression language is untouched: `Result`/`Option`, `?`, `match`,
enums, overloading and decorators-as-a-language-mechanism keep their syntax
and meaning. Guards (`guard.go`), middleware (`middleware.go`), the live
wire protocol, islands, `templx` interop, auth, the orm and the toolbar are
unchanged except where a render file replaces a markup-bearing one
(`_layout.vuka.html` for `layout.go`). This RFC does not propose any change
to how the binary is built (`go build`, `vuka run`/`build`/`gen`) beyond the
new parser mode and the `[id]` route generation.
