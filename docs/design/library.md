# vuka as a library: plain Go, `go run .`

## Goal

A normal Go module — `.go` files only, built and run with `go build` and
`go run .`, no transpiler and no CLI — gets the orm, web (routes, live
pages, islands, auth, deferred fields, validation) and ui (components,
client state) with the same behaviour as a `.vuka` app.

The CLI stays useful (watcher, scaffolding, upgrades, diagnostics, the
language server) but is never required to build or run.

## Principle: plain Go is the language; `.vuka` is sugar that generates it

The library API in this document is the primary way to write a vuka app.
`.vuka` adds syntax (JSX, decorators, attributes) and nothing else: the CLI's
only job is to turn a `.vuka` file into the plain Go a person would write by
hand against this API.

The contract for generated code:

- **Public API only.** Generated files call exported orm, web and ui API —
  `app.Get`, `app.Live`, `ui.HTML`, `orm.Schema`, `ui.LiveSchema`, … —
  never internal hooks (`vuka.TypeWith`, fingerprinted `ui.FT`/`ui.F`
  forms), private helpers or `__`-prefixed names. A test builds every
  generated golden against the public API alone and fails on any other
  symbol.
- **Hand-written shape.** gofmt'd, real names, doc comments carried over,
  templates as backtick HTML with typed holes, one `//line` marker per
  statement with relative paths. It should pass review as hand-written code.
- **Ejectable.** Running `vuka gen -inplace` (or `vuka eject file.vuka`)
  writes the `.go` next to the source; deleting the `.vuka` leaves a working
  app that builds with `go build` / `go run .` and behaves identically.
  The conformance suite (§8) checks this for every example.
- **Optional speed-ups stay separate.** What only makes things faster (orm
  scanners, live render tracking, island TypeScript types) is generated into
  its own `*_gen.go` files registered at init, the same files a plain-Go app
  gets from `go generate`. The main generated file never depends on them.
- **No hidden runtime.** Anything the generated code needs that a person
  couldn't call is a gap in the public API, fixed by adding public API,
  not by a private hook.

## One runtime, two ways in

Everything a `.vuka` file says already lowers to Go calls on orm, web and ui.
That lowered layer becomes the documented, stable public API, and `.vuka` is
syntax over it:

```
.vuka (JSX, decorators, attributes) ──┐
                                      ├─► public Go API (orm, web, ui) ─► runtime
plain .go (calls, templates, schemas) ┘
```

Consequences:

- A plain-Go app is written the way vuka generates; both run identical code.
- Readable generated code (roadmap 2.3) and backtick JSX (2.4) are this API
  seen from the transpiler's side: generated files are a worked example of
  hand-written ones.
- Typed attributes (2.1) and explicit hooks (2.2) are designed once, as Go
  API, and the `.vuka` attribute syntax maps to it one-to-one.

## 1. Registration: calls instead of decorators

A package registers what it serves at init, on the package's registry —
the code the compiler writes for decorators, and what a person writes:

```go
func init() {
	web.Routes.Service(NewOrderService)
	web.Routes.Get("/orders", ListOrders, auth.Required())
	web.Routes.Get("/orders/{id}", ShowOrder, web.Params("id", "orders"))
	web.Routes.Live("/dashboard", NewDashboard, web.Layout(layouts.AdminPage("Dashboard")))
	web.Routes.Live("/applicants/{id}", NewApplicantDetail,
		ui.Island(ui.Layout(layouts.AdminPage("Applicant"))))
}

func main() {
	app := web.New(web.Frontend(dist, "web/dist"), orm.Use(databases))
	log.Fatal(app.Run(""))
}
```

| `.vuka` | Go |
|---|---|
| `@web.Get("/orders") func ListOrders(…)` | `web.Routes.Get("/orders", ListOrders)` |
| `@web.Get("/orders/{id}") func ShowOrder(id int, …)` | `web.Routes.Get("/orders/{id}", ShowOrder, web.Params("id", …))` |
| `@web.Live("/x") type X struct{ ui.Live … }` | `web.Routes.Live("/x", NewX)` |
| `@web.Live("/x")` + `@ui.Island(opts…)` | `web.Routes.Live("/x", NewX, ui.Island(opts…))` |
| `@web.Get("/x")` + `@ui.Island(opts…)` (stateless) | `web.Routes.Get("/x", NewX, ui.Island(opts…))` |
| `@web.Use(mw)`, `@web.Template(f)`, `web.Layout(l)` | the options `web.Use(mw)`, `web.Template(f)`, `web.Layout(l)` |
| `@web.Controller("/admin") type Admin` | `web.Routes.Controller("/admin", NewAdmin)` |
| `@web.Service type S` | `web.Routes.Service(NewS)` |
| `@auth.Required` | `auth.Required()` option |
| `func X.New(…)` (static constructor) | `func NewX(…)` |

**Registries.** `web.Routes` is a `*web.Registry` with the methods above.
Decorators are scattered over packages that `main` doesn't list, so they
register at init; `web.New` gives each app a copy. For routes one app
alone serves (tests, two apps in a process), `App` has the same methods:
`app.Get("/orders", ListOrders)` adds to that app only.

The compiler knows no library: a decorator naming a method of its package's
registry — an exported package-level variable whose type has the marker
method `vukaRegistry()` — is a call of that method with the declaration (a
function, a method expression, a type's `NewX`), the decorator's arguments
around it as the method's parameters place them. The declaration's other
decorators whose values the method takes as options (a `web.RouteOption`:
`@web.Use`, `@ui.Island`) go with the call, and when the package has
`Params(...string)` the parameters reflection can't tell apart are named.
Options are typed values; `web.RouteOption` is any value carrying
attributes, so an auth gate (`vuka.Bundle`), `ui.Island` and web's own
options are all one, with no positional strings.

Defaults are inferred as the decorators infer them: `ui.Island()` bare finds
its component from the type's name, `ui.Island("islands/x.vue")` names it
(`ui.Component` was taken: it is a live component's tag), a constructor's
parameters of provided types are injected (the others start zero), and a
route's attributes include those registered on the handler.

A type decorator whose package has a generic twin — `Register<Name>[T]`,
taking what the decorator takes — lowers to the twin, the call a person
writes: `@ui.Island` on `Chart` is `ui.RegisterIsland[Chart]()`, and
`@ui.Island("islands/Chart.vue", ui.Strategy("load"))` is
`ui.RegisterIsland[Chart]("islands/Chart.vue", ui.Strategy("load"))`; the
path is written only when the source names one. The form older compilers
wrote, `ui.Island(…)(vuka.TypeFor[Chart](NewChart))`, keeps working.

A composed decorator (`vuka.Bundle`, such as `@auth.Required`) beside a
registry decorator taking it is an option of the registration too:
`@web.Live("/applicants")` with `@auth.Requires("view_applicant")` on a
type is `web.Routes.Live("/applicants", NewApplicants,
auth.Requires("view_applicant"))`, and the route applies the gate to its
requests and its live page's events — no method of the type is wrapped. A
bundle on a declaration with no registry decorator (a method's own gate)
still wraps it, so its gate checks direct calls too. Decorators of
libraries without a registry lower generically, to calls a person writes as
well: `Model(vuka.TypeFor[User](NewUser))` for a type decorator,
`Command("greet")(vuka.DeclOf(greet))` for a declarer, `b.Declare(d)` and
`b.Decorate(t)` for a bundle. The forms older compilers wrote
(`vuka.TypeWith`, `vuka.FuncDecl`, `vuka.DeclareWith`, `web.Get(path)` as a
bundle, `X_New`) keep working, deprecated.

`examples/conformance/library-users` is an app written this way, with its `.vuka`
twin; `TestLibraryExample` (cmd/vuka) builds it with plain `go build`, ejects
the twin and checks both are written alike (the same `web.Routes` calls, the
same templates) and answer the same.

### Folder routes (1.1; endpoints and guards 1.2; middleware 1.3)

The registration of an app with many pages is mostly bookkeeping: a
`web.Routes` call per page, the path spelt again in every link, the layout
named per route, parameters converted and checked per handler. Folder
routes derive it from the module's `pages/` tree and generate the calls a
person writes, so nothing at run time depends on the tree
([guide](../web/folder-routes.md)).

- **Tree.** `<module root>/pages/**` are ordinary packages; the folder path
  is the URL. `pages/pages_gen.go` (package `pages`, generated) registers
  every route of every folder, so `main` imports `pages` once and a layout
  never has to be imported by the pages it wraps. `pages/urls` holds the
  typed links; a folder with template-only pages gets a `routes_gen.go`
  declaring their types.
- **Pages.** An exported struct `T` with `func New<T>(deps…) *T`: `*T` a
  stateful component (`ui.Live` + `Render() ui.Node`) is `web.Routes.Live`,
  a `ui.Node` is `web.Routes.Get` with the constructor (a stateless page
  built per request). `Index` serves the folder's path, any other page adds
  its kebab-case name (`New` → `new`, `ResetPassword` → `reset-password`).
  `Layout`, `Guard` and `Params` are never pages. Every page route carries
  `web.PageRoute()`, the opt-in to the behaviour below, so 1.0 routes are
  unchanged.
- **Generated constructors (1.2).** An exported page-shaped struct (or
  endpoint) with no `New<T>` gets `func New<T>() *T { return &T{} }` in its
  package's `routes_gen.go`, visible to the template compiler as a
  template-only page's type is; a hand-written `New<T>` turns it off.
  Unexported types, `Layout`, `Guard`, `Params` and types registered another
  way (route decorators, `web.Routes` calls) are left alone. In 1.1 such a
  type was a component the pages used; from 1.2 it is a route, so shared
  components live in another package or unexported, and an accidental route
  shows in `vuka routes` or as E0701.
- **Template-only pages.** A `.vuka.html` with top-level markup whose
  file-based type (`about.vuka.html` → `About`, `index.vuka.html` →
  `Index`) the package doesn't declare is a stateless page: gen declares
  `type About struct{ Params }` (or `struct{}`) and `NewAbout` in
  `routes_gen.go`, visible to the template compiler, and routes and links
  it like any page (`urls.About`). No `Title`; declaring the type by hand turns the generated one off.
  A file of `func` templates only is not a page. `vuka routes` marks them.
- **Single-page-app fallback.** Not a mechanism of its own: a catch-all
  folder (`app/route/`, `Params{Route web.Rest}`) whose `Index` renders a
  client app's shell answers every `/app/...` URL, and static and dynamic
  routes beside it win by precedence (`/app/settings`).
- **Binding.** A page's exported fields, promoted ones from embedded structs
  at any depth included, bind as a live page's do: a provided type
  injected, text from the path segment of its name, else the query
  parameter. A path parameter that doesn't parse or validate is 404
  (`web.NotFound`): the URL names nothing. A query parameter keeps 400. A
  stateless page's `Mount()` / `Mount(ctx)` (returning nothing or an error)
  runs after binding and after its guards' and layouts' `Load` (1.2; in 1.1
  the layouts loaded after `Mount`), so a guard protects `Mount`.
- **Parameters.** A package declaring `type Params struct` makes its folder
  the segment `{folder}`; `Params` has one own field named like the folder
  (case-insensitive) and embeds the nearest ancestor parameter folder's
  `Params`, nothing else. The field's type is the constraint: integer kinds
  (digits), `string` (one segment), a `TextMarshaler`/`TextUnmarshaler`,
  `web.Rest` (the rest of the path, `{name...}`, a leaf). `validate:`
  applies.
- **Layouts.** A package declaring `type Layout struct` wraps every page in
  and below its folder: `web.LayoutOf[T]()` per ancestor, outermost first.
  A layout is a stateless struct component with `Children ui.Node`,
  optionally embedding `Params`, optionally `Load(ctx, deps…) error` (deps
  from the container). Per request: zero `T`, bind (path only, 404 on
  failure), `Load`, `Children` = the next layout or the page, render. A
  `Load` error answers the request (`NotFound` 404, `Forbidden` 403, a
  `Redirect` redirects), for the pages and their method handlers alike. A live page's layouts are part of its page render, the GET and
  the join's run of the route (`Load` runs for each); patches re-render the
  live component only. Folder layouts replace `web.Layout`, the app's
  `web.LiveLayout` and an island page's layout.
- **Titles.** `Title() string` sets `ui.WithPageTitle` before the layouts
  render; layouts read `ui.PageTitle(ctx)`. A stateless page's after its
  `Mount`; a live page's after binding and before `Mount` (the session
  mounts while it renders inside the layouts), so a live page titles itself
  from its parameters or its constructor.
- **Method handlers.** `Post`, `Put`, `Patch`, `Delete` methods of a page
  type answer those methods on its path; a method named `Get` is an error.
  Registered as `web.Routes.Post(path, (*T).Post, web.Receiver(NewT),
  layouts…)`: the receiver is built per request by the page's constructor
  with its path parameters bound (404 on failure); its other parameters and
  results are any handler's. Layouts wrap a `ui.Node` result when the
  response is a whole page (GET, or a browser document request); fragments
  go bare. The layouts' `Load` runs before every handler as before the
  page: its `NotFound`, `Forbidden` or `Redirect` stops `Post`, `Put`,
  `Patch` and `Delete` too, so a folder's checks live in one place.
- **Endpoints (1.2).** An exported struct `T` whose `*T` is not a page (no
  `ui.Node`, no `ui.Stateful`, no `Render`/view) with at least one method
  `Get`, `Post`, `Put`, `Patch` or `Delete` is an endpoint; its path follows
  the page rules. Each method is `web.Routes.<Method>(path, (*T).<Method>,
  web.Receiver(NewT), chain…)`, exactly a page's method handler, with `Get`
  allowed (E0709 only on pages). Results as any handler's: a value → 200
  JSON, an error → its status (JSON), a nil error alone → 204, a `ui.Node`
  → a page, a `Redirect` → a redirect. A page and an endpoint share a path
  when their methods don't collide; a collision is E0701. `pages/urls`
  links them; `vuka routes` marks them `(endpoint)`.
- **Guards (1.2).** A package declaring `type Guard struct` guards every
  page, handler and endpoint in and below its folder: `web.GuardOf[T]()`
  in the chain at its folder, before that folder's `web.LayoutOf`. A guard
  renders nothing, optionally embeds `Params` (bound from the path), and
  must have `Load(ctx, deps…) error` (E0708 at generation; web's E0321 /
  E0407 at build). Guards' and layouts' `Load`s run in option order,
  root-first, before a stateless page's `Mount`, a live page's render (GET
  and join) and a handler's body binding; the first error answers:
  `NotFound` 404, `Forbidden` 403, another `web.Error` its status,
  `Redirect` 302 on GET / 303 otherwise; an HTML error page for pages, JSON
  for JSON handlers, a live error with the status for a refused join. A
  route with only guards keeps its own document.
- **Middleware (1.3).** A package declaring `Middleware` — `func(next
  http.Handler) http.Handler`, or a constructor `func(deps…) web.Middleware`
  (deps from DI, resolved at Build, E0407 when unprovided) — wraps every
  page, method handler and endpoint in and below its folder; never
  hand-written routes, static files or the app's 404. gen emits it root-first
  as `web.UseOf(pkg.Middleware)` before the folder's GuardOf/LayoutOf; per
  request: middlewares (root → leaf, outermost) → guards'/layouts' Load →
  bind → Mount → handler → back out. It wraps the whole route handler, so a
  bad path parameter's 404 passes through it; not calling next
  short-circuits. Live pages: the GET and the join only, never socket
  events (a per-session check is a guard). One per package; a wrong
  signature is E0710; `vuka routes` gains a MIDDLEWARE column.
- **Type attributes on stateless pages (1.3).** `vuka.RegisterType[T](attrs…)`
  now reaches the route spec of stateless folder pages (PageRoute
  constructor routes) and method handlers/endpoints (Receiver routes), as it
  already did live pages and islands — flattened, type attrs first. So
  `vuka.RegisterType[Login](auth.Public())` opens a stateless login page and
  its handlers, and a RouteAttr on the type reaches WrapRoutes.
- **Typed URLs.** One generated package, `pages/urls`
  (`urls_gen.go`), importing no page package, so any page links to any
  other, parent to child included (per-package functions would make a
  parameter folder's link to its children an import cycle, since a child
  embeds its parent's `Params`). The tree as a chain:
  `urls.Employers.Employer(7).Permits.Permit(3).Path()`. `urls.Root` is the
  `pages/` folder and each of its children is also a package-level var. A
  folder value has a field per fixed child folder (CamelCase of its name)
  and per non-`Index` page (its type name), a method per parameter child
  (CamelCase, taking the `Params` field's type), and `Path()` only when it
  has an `Index`, so only real pages are linkable; a page field has
  `Path()`. Values carry the path built so far in unexported per-node types.
  Escaping: integers via `strconv`, strings and `MarshalText` output via
  `url.PathEscape`, `web.Rest` per segment. Query parameters are outside
  the chain.
- **Precedence.** Static, then `{name}`, then `{name...}`, as `net/http`'s
  `ServeMux`. Everything else is a generation error with file and line:
  E0701 two pages on one path, E0702 two routes matching the same paths
  (parameter folders side by side), E0703 something below a catch-all
  folder, E0704 a malformed `Params`, E0705 a parameter type a URL can't
  hold or `urls` can't name (declared in a page package), E0706 a taken
  name (a child folder and a page alike in `urls`, a template-only page's
  type), E0707 a hand-written route on a folder route's path, E0708 a
  malformed `Layout`, E0709 a method handler that can't be bound. Web's
  build checks keep their codes (E0321 a layout or
  `Load` signature, E0407 an unprovided `Load` dependency, E0410 a
  `Receiver` that doesn't build the receiver, E0333 a stateless `Mount`).
- **Generation.** `vuka build/run/test/vet/lsp` overlay `pages/pages_gen.go`,
  `pages/urls/urls_gen.go` and the `routes_gen.go` files; `vuka gen -inplace` writes them, `-check`
  reports staleness, `vuka eject` keeps them. Plain Go, the public web API
  only (`contract.txt`), gofmt'd, deterministic. `vuka routes` prints the
  table (method, path, page, file:line, guards, layouts).
- **Additive.** Routes without `PageRoute`, `LayoutOf` or `Receiver` behave
  as in 1.0 (a live page's bad path parameter is still 400), and
  hand-written routes coexist on other paths. `examples/folder-routes` is
  the tree in plain Go; `TestFolderRoutesE2E` (cmd/vuka) generates, builds
  and requests it.

## 2. Markup

Markup is written two ways, compiled to the same calls
([templates.md](templates.md) is the design).

### `.vuka.html` templates (default with the vuka command)

```html
<!-- orders.vuka.html: the markup of Orders, its fields by bare name -->
<table>
  for _, o := range Rows {
    {row(o, Cancel)}
  }
</table>

func row(o Order, cancel func(int))
<tr key={o.ID} class="row" class:late={o.Late}>
  <td>{o.Ref}</td>
  <td class="tabular-nums">{money(o.Total)}</td>
  <td><button onClick={cancel(o.ID)}>Cancel</button></td>
</tr>
```

- A file holds templates: the markup before its first `func` line is the
  markup of the type the file is named after — the `View` of a struct
  component, which is then itself a `ui.Node`, or the `Render` of a stateful
  type — and `func` lines start the others (partials and function
  components). Go statements start lines (`if`, `for`, `switch`, `x :=
  …`); holes are any Go; component tags are struct components (`Card{…}`),
  stateful types or function components, their
  attributes the fields (`<slot:name>` fills a Node field). A
  `<script lang="ts">` and `<style scoped>` after the markup are the
  component's, bundled and scoped by the vuka command.
- `vuka run|build|test|vet` compile them through the overlay; `vuka gen`
  writes `orders_html.go` for a plain `go build`; `vuka eject -rm` turns a
  `.vuka` file's markup functions into a `.vuka.html`.

### `ui.HTML` in plain Go

What a template compiles to, and what plain Go writes with no tools:

```go
func row(o Order, cancel func(int)) ui.Node {
	click := func() { cancel(o.ID) }
	return ui.HTML(`
<tr key={o.ID} class="row" class:late={o.Late}>
  <td>{o.Ref}</td>
  <td class="tabular-nums">{money(o.Total)}</td>
  <td><button onClick={click}>Cancel</button></td>
</tr>`, o.ID, o.Late, o.Ref, money(o.Total), click)
}
```

- Each hole is labelled with its value's Go; the values follow in the order
  the labels first appear, a label written twice being one value. vukavet
  checks the values against the labels (§7).
- Parsed once per source (a constant's), static parts precomputed, and under
  a live session the same render tree (statics, dynamics, keyed lists) JSX
  lowers to; as fast as the numbered-hole templates it replaced.
- Holes are escaped by place: child (`ui.Node`, `[]ui.Node`, text, a
  `ui.State`, a stateful component, an island), text (in title, textarea),
  attribute value (what a JSX attribute takes; an `on…` attribute a handler —
  a func, `ui.On(…)`, JS commands), part of a value (text), toggled class
  (`class:x={bool or State}`), attribute set (`{attrs...}`: `ui.Attr`,
  `[]ui.Attr`, `templ.Attributes`, `ui.BindAttr(…)`). A value of the wrong
  kind is a render error naming the hole's line and label; malformed markup
  fails the render with its line.
- Loops and conditionals are ordinary Go producing nodes:
  `ui.Each(rows, OrderRow)`, `ui.If(cond, node)`, or a `[]ui.Node` given to
  a hole.
- A hole is a place: a stateful component (`&Counter{…}`,
  `ui.Key(id, &Counter{…})`) or an island (`Chart{…}`,
  `ui.Mount(Chart{…}, ui.Idle())`) in it is identified by it, and a list's
  components by the hole the list is in. Client state is a `ui.State` field
  or a package-level `var x ui.State[T]` (the page's).
- An expression of states is a derived state the browser evaluates again
  when a state it reads changes — what `!open && tab == "env"` lowers to,
  in package uigen, the one exception to "no hidden runtime": public so the
  generated code builds, but the compiler's output, not an API to write
  (2.0, docs/rfc/one-way.md):

  ```go
  ready := uigen.And(uigen.Not(open), uigen.Eq(tab, "env")) // !open && tab == "env"
  q := uigen.At[string](f, "Q")                             // f.Q, a state of its own
  hint := uigen.Plus(uigen.Derived("Hello, "), name)        // "Hello, " + name
  cls := ui.Classes{"menu": true, "is-open": open}          // ui.Class{…} reading a state
  ```

  Not, And, Or, Eq, Ne, Lt, Le, Gt, Ge, Plus, Minus, Neg and Len are the
  operators the browser runs; an operand is a value or a state. The
  server renders the value with the states' initial values.

### Builders (alternative)

`h.Div(h.Class("…"), h.Text(x), children…)` for code that prefers calls
over strings. Same nodes, no template parse.

### templ

Existing templ support stays; templ components are `ui.Node`s.

## 3. Configuration: attributes as values in one schema method

A `.vuka` attribute is a value. In plain Go the same value is passed to a
schema method, field by field, so the hand-written form and the generated
form are the same text: `@X{…}` on a field is `X{…}` after `&t.Field`.

### orm models

```go
type Applications struct {
	orm.Model[Applications]
	ID              int
	CreatedAt       Option[time.Time]
	Applicant       orm.FK[Applicant]
	IsShortlisted   bool
	InterviewNumber Option[string]
	TrackNumber     string
}

func (a *Applications) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "applicantlist", Unmanaged: true}, mirror.Legacy{})
	s.Field(&a.ID, orm.PK{}, mirror.Column{Name: "applicant_list_id"})
	s.Field(&a.CreatedAt, orm.Datetime{Column: "created_at", Null: true}, mirror.Omit{})
	s.Field(&a.Applicant, orm.Rel{Column: "applicant_id", Related: "applications", Null: true})
	s.Field(&a.IsShortlisted, orm.Column("is_shortlisted"))
	s.Field(&a.InterviewNumber, orm.Char{Column: "interview_number", Max: 50, Null: true})
	s.Field(&a.TrackNumber, orm.Char{Column: "track_number", Max: 50})
}
```

The `.vuka` it is generated from:

```vuka
type Applications struct {
	orm.Base                          @orm.Meta{Table: "applicantlist", Unmanaged: true} @mirror.Legacy{}
	ID              int               @orm.PK @mirror.Column{Name: "applicant_list_id"}
	CreatedAt       Option[time.Time] @orm.Datetime{Column: "created_at", Null: true} @mirror.Omit
	…
}
```

- **Fields by address.** orm calls `Schema` once on a zero value and maps
  each `&a.X` to its field by offset: renames are compiler-checked, no
  column names in strings, no closure per field. A pointer that isn't a
  field of the receiver is an error at registration, with a code.
- **Only what differs.** A field with no attributes isn't listed; column
  name, nullability and type come from the field's name and Go type, as
  they do now.
- **Attributes are open.** A field attribute implements `orm.FieldAttr`, a
  model attribute `orm.ModelAttr`. `mirror.Column`, `mirror.Omit` and
  `mirror.Legacy` are ordinary values from the app's own package; the
  transpiler has no special case for any attribute.
- **One way.** Struct tags carry their Go meaning (json, db) only; orm stops
  reading `orm:` tags after the deprecation window (roadmap 2.1).
- Schema replaces `Meta()` and `SetFieldOptions`; both keep working for one
  minor release with a vet warning and a `vuka fix` rewrite.

### Queries

Querying is already Go, and stays as it is. What changes is the plain-Go
spelling of the two model statics `.vuka` offers:

| `.vuka` | Go |
|---|---|
| `Post.Objects` | `orm.Objects[Post]()` |
| `Post.Slug` (typed column) | `PostFields.Slug` |
| `func Post.Live() orm.QuerySet[Post]` (a named query) | `func PostLive() orm.QuerySet[Post]` |
| `p := Post.Objects.Get(ctx, Post.Slug.Eq(slug))?` | `p, err := orm.Objects[Post]().Get(ctx, PostFields.Slug.Eq(slug))` + `if err != nil { return err }` |

`orm.Objects[Post]()` is the model's own manager, the one `orm.For[Post]()`
declares (or one made from its Meta): `For` takes options and makes a
manager each time, so it is called once, package-level, and the static —
read wherever the code names it — is `Objects`.

```go
func matching(search string) orm.QuerySet[Applicant] {
	f := ApplicantFields
	qs := orm.Objects[Applicant]().QuerySet
	for _, w := range strings.Fields(search) {
		qs = qs.Filter(orm.Or(
			f.FirstName.IStartsWith(w),
			f.FamilyName.IStartsWith(w),
		))
	}
	return qs.OrderBy(f.CreatedAt.Desc())
}

rows, err := matching(q).Limit(50).All(ctx)
```

- **Typed columns are generated**, because Go can't add fields to a type:
  `ApplicantFields` (one `orm.Field[V]` / `orm.TextField[V]` per column,
  relations walkable as `f.Applicant.Nin`) lives in `applicant_gen.go`,
  written by `vuka gen` or `//go:generate go tool vuka gen`. It is a
  convenience, not a requirement: without it every query works with
  `orm.Q{"first_name__istartswith": w}` and `orm.F("…")`, as now, and an
  unknown lookup is still a runtime error with a did-you-mean.
- `<Model>Fields` is the one naming rule; it needs no pluralisation and
  can't collide with the model type.
- Row methods (`Save`, `SaveFields(ctx, f.Title)`, `Load(ctx, f.Tags)`,
  `Add/Remove/Set`), `Values`, `Annotate`, `GroupBy`/`Having`, subqueries,
  `Raw` and transactions are unchanged; `@orm.Transaction` is
  `orm.Transaction(ctx, func(ctx context.Context) error { … })`.

### Live and island pages

The same shape for page state and hooks:

```go
type Dashboard struct {
	ui.Live
	Q     string
	Rows  []Task
	Total int
}

func (d *Dashboard) LiveSchema(l *ui.LiveSchema) {
	l.Field(&d.Q, ui.Bind{Debounce: 300 * time.Millisecond}, ui.URL("q"), validate.Max(100))
	l.Field(&d.Rows, ui.Defer("lists"))
	l.Field(&d.Total, ui.Defer("lists"))
	l.Loads("lists", d.loadLists)
	l.OnChange(d.changed)
	l.Mount(d.mount)
}
```

These are the explicit hooks of roadmap 2.2: hooks may be unexported, only
exported methods without a hook are callable from the browser, and signature
mistakes are compile errors (the method values are typed). `.vuka`
attributes (`@ui.Bind{…}`, `@ui.Defer("lists")`, `@ui.Loads("lists")`,
`@ui.OnChange`, `@ui.Mount`) lower to exactly these calls.

### Hooks declared by the constructor (1.1)

A page whose only schema is its hooks declares them in its constructor, on
the embedded `ui.Live`, by method expression:

```go
type Login struct {
	ui.Live
	login ui.Assign[Credentials]
}

func NewLogin() *Login {
	l := &Login{}
	l.OnMount((*Login).mount)
	l.Loads("stats", (*Login).loadStats)
	return l
}

func (l *Login) mount(ctx context.Context) error     { … }
func (l *Login) loadStats(ctx context.Context) error { … }
```

- `(l *Live) OnMount(fn any)`, `Loads(group string, fn any)` and
  `OnChange(fn any)`. A hook is a method expression, `func(*T, …)`, never a
  method value: the runtime runs hooks on the page it holds (a loader's
  copy, a resumed page), so a hook takes the page as its first argument.
  Signatures as `LiveSchema`'s: `OnMount` and `Loads` take `func(*T)` or
  `func(*T, context.Context)`, returning nothing or an error; `OnChange`
  also `func(*T, context.Context, ui.Changed)`.
- The hooks are the type's, not the instance's: the first instance of a type
  to declare them sets its table, checked then (`T` must be the type
  embedding this `Live`: E0336, naming the call's line); an instance
  declaring other hooks for the same type is E0336 too. Later constructor
  calls compare and store a pointer.
- A hook is an unexported method (E0336 otherwise), out of the browser's
  reach; `ui.Live`'s methods, these included, are never events.
- The app's start-up checks run before any constructor: a deferred group
  without a loader (E0324) on a page routed with a constructor is checked
  again as the constructor builds the page, and a failed check isn't
  cached.
- One declaration per hook: a hook set by both the constructor and
  `LiveSchema` is E0336. Fields keep their attributes in `LiveSchema`; a
  page may use both, the constructor for hooks and `LiveSchema` for fields.
- A page built without its constructor has the hooks its type already set,
  or none. A routed page is built by its constructor; a stateful component
  placed by a template is a literal (`&Counter{…}`) and runs none, so a
  component whose hooks live in a constructor is placed as
  `NewCounter(…)`, or declares them in `LiveSchema`. vukavet reports a
  literal of a type whose hooks are declared only by a constructor (W0304),
  and a hook declared outside a function returning the type (W0305).
- vukavet checks each method expression against the hook's signatures
  statically, so a mistake shows in the editor before boot.
- `.vuka` is unchanged: `@ui.Mount` and the other hook attributes lower to
  `LiveSchema`, which needs no constructor. Nothing to eject: the form is
  plain Go.
- Additive: `LiveSchema` stays the explicit form. From 1.2 `Mount` and
  `Changed` by name are ordinary hooks again (signatures checked: E0329,
  E0327; the browser can't call them; no W0303); a hook named in
  `LiveSchema` or the constructor takes the place of a method of its name.
  `Load<Group>` by name keeps W0303, since the group in its name can drift
  from the fields' tag.

## 4. Generated code is optional — a speed-up, not a requirement

| Generated today | Without it (`go run .`) | With `go generate` |
|---|---|---|
| orm row scanners and writers | reflection: the ORM's own cost 1.1–1.7× (1000 rows: 1.34× at 6 columns, 1.67× at 23), 1.05–1.07× end to end on SQLite | generated, as now |
| orm field sets (`<Model>Fields`) | `orm.Q{"…"}` / `orm.F("…")`, checked at run time | typed |
| orm `Register[T]()` | a model's `orm.For[T]()` (or first `Objects`) registers it | at init |
| live render tracking (`ui.F` frames from JSX) | components built from nodes diff whole: the same page; 1.7–2.2× the bytes on the demo pages' random events | fine-grained, as now; `ui.HTML` builds frames when it parses, no generation needed |
| `vuka-live.d.ts` (island TypeScript types) | islands work, untyped (`useLive()` comes from the island build) | typed (`defineProps<T>()` needs it); `go tool vuka build-islands` writes it |

Measured on an Apple M1 Pro: orm `go test -bench Fallback` (a stub driver
for the ORM's own cost, SQLite end to end), ui `go test -bench Fallback
./live` and `TestWholeComponentFallback`.

- No check costs anything when the generated code is there: an orm model
  looks for its scanner once, on its first query, and keeps the answer; a
  live component's node type decides, a frame or plain markup.
- One line opts in: `//go:generate go tool vuka gen`.
- The fallbacks are tested with generation off: the orm suite runs again
  with its generated scanners ignored (`ORM_REFLECT=1`); the live sessions'
  random-event tests run every demo page with frames and as whole
  components and compare each page; web's runtime test applies whole-
  component replies with `live.js`. The conformance suite (§8) runs
  every example both ways.

## 5. Frontend assets

- **Production:** `//go:embed all:web/dist` of a prebuilt bundle, as today.
  The islands go into the same build: `go tool vuka build-islands` finds the
  registered island types by reading the Go source (never running the app)
  and writes their bundle and stylesheets to `web/dist/.vuka/`, which the
  app serves; one line opts in, after the frontend's own build:

  ```go
  //go:generate go tool vuka build-islands
  ```

  It writes `islands.d.ts` and `vuka-live.d.ts` too, typed from the Go
  structs. A build of islands alone (no `index.html`) is served as one.
- **Development with `go run .`:** `web.Dev()` — active only when
  `VUKA_DEV=1` or the environment is development — starts Tailwind and Vite as
  child processes when they're installed and serves through them, and for an
  app registering islands `vuka build-islands -watch`, which keeps them built
  in `.vuka/assets` from the component files; otherwise it serves the last
  build and logs one line saying how to get live CSS and islands.
- Without Node, everything except framework islands and Tailwind works
  (vanilla islands build in Go).
- **Which one to ship.** Pick one per app. `vuka gen -inplace ./...` writes
  `vuka_assets_gen.go` in `main` (the islands' built JavaScript and the
  Tailwind CSS as Go constants), so a plain `go build` holds everything;
  `vuka gen -inplace -check` then wants that file present and current.
  `go tool vuka build-islands` writes the same islands to `web/dist/.vuka/`
  for an app that already embeds `web/dist` after its own frontend build.
  Either way the output is generated: keep a `//go:generate` line in
  `main.go` and run it before `go build` (in CI too) rather than committing
  a megabyte of JavaScript. `vuka gen` writes Vuka's generated Go only; a
  `.go` package with models keeps its `//go:generate go tool orm generate`
  line for the ORM's field sets and scanners.

An island is declared the same way in both: `ui.RegisterIsland[Chart]()`
finds `islands/[<module>/]Chart.<ext>` under the frontend root, as a bare
`@ui.Island` does (one implementation, the vuka command's); a missing
component is E0310 at build time, its help naming the call to write. The
hand-written line and the generated one are the same.

## 6. What the CLI is for

Convenience, never a requirement: the watcher (`go tool vuka run`),
scaffolding (`vuka new`, `vuka add`), `vuka upgrade`, diagnostics with codes,
the language server.

## 7. Checks without the CLI

The vet-time checks (missing loader, handle clash, bad bind/validate,
template hole kinds, route conflicts) are packaged as `go/analysis`
analyzers, `github.com/vuka-lang/vuka/cmd/vukavet` (its own module, so the
vuka module stays free of dependencies), runnable as
`go vet -vettool=$(go tool -n vukavet)`, with the same diagnostic codes.
`vukavet` is `go vet`'s own analyzers plus four checks
([vukavet](../tools/vukavet.md)):

- `routes`: registrations with web — a method and path declared twice,
  across the packages it imports too (E0401), a handler that isn't a
  function (E0403), an unnamed scalar parameter (E0405), results the app
  can't write (E0406), `web.Params` that doesn't fit (E0419, new), a
  constructor of the wrong shape (E0418), an island page that isn't a GET
  (E0331);
- `live`: live pages' constructors and structs (E0329, E0330) and
  `ui.CheckIsland`'s checks of island pages (E0322–E0328, E0333);
- `templates`: `ui.HTML`'s values against its labels — their number
  (E0127), each written as its label (E0132), and its type against its
  hole's place (E0128);
- `schema`: `s.Field` not given the receiver's field (E0501), a nil
  attribute (E0502), a field behind a pointer (E0503).

`live` reads struct tags today; once `ui.LiveSchema` lands (step 3) it reads
the `l.Field`/`l.Loads` calls the same way. gopls can't load third-party analyzers, so editors get them from `go vet`;
a `.vuka` app runs them as `vuka vet -vettool=$(go tool -n vukavet)`, the
positions its `.vuka` lines.

## 8. Conformance

Every app of `examples/conformance/<app>` exists twice — `vuka/`, the
`.vuka` app, and `go/`, the same app written by hand in Go (`.go` and
`.vuka.html`, struct-first, registered in `init`) — and `TestConformance`
(cmd/vuka, the CI job `conformance`) builds it five ways:

| run | |
|---|---|
| `vuka` | `vuka/`, built by `vuka build` |
| `go+gen` | `go/` after `orm generate`, `vuka gen -inplace` (and `vuka build-islands` for a `web/dist`), built by `go build`; `go vet` with `vukavet` passes it |
| `go` | the same without the speed-ups: no `orm_scanners*_gen.go` (the orm reflects), no `vuka_calls_gen.go` (`vuka.Wrap` reflects), every live component diffed whole (`VUKA_LIVE_WHOLE=1`); the two apps must have had the same speed-ups |
| `ejected+gen` | `vuka/` after `vuka eject -rm` (no `.vuka` file left; `vuka gen -inplace -check` must find nothing stale), built by `go build`; `go vet` with `vukavet` passes it |
| `ejected` | the same without the speed-ups |

Each run plays the app's `script` — HTTP requests, and live pages whose
messages go over the page's socket with protocol 1 and 2 — and answers with
a transcript: each response's status, headers and body; each live reply and
the page `livetest`'s reference client holds once it applied it; the SQL
each step ran (an `orm.Observe` route the suite adds); every `/_vuka/` file
a page or the loader's island manifest names; and the `vuka-live.d.ts` and
`vuka-island.d.ts` the vuka command wrote. Every transcript must be the
first's; the render trees protocol 2 sends follow the code's shape (a Go
`if` or `ui.If`), so they are compared where the code is the same (`vuka`
and `ejected+gen`) and among the whole-component runs. A page's join
token, a cookie's value and a hole's source position (an island's
`data-vk-s`, named by order of appearance) are left out. A difference is a
bug: the test shows the first step answered differently, line by line.

The apps: `library-users` (REST over the orm, a live page, a page backed by
its island), `crud` (Schema, a relation walked by typed fields, a named
query, transactions, update arithmetic, typed `SaveFields`, a call
decorator), `live` (a page backed by its script island with a deferred
group, a bound and validated query in the URL, an `Update` per message and
per URL, navigation; a JSX page with a bound input its form clears once sent, a
validated form and patches), `components` (struct components with slots, a script and a
scoped style, a layout taking `ctx` used as a tag, partials, keyed
stateful components), `islands` (`.ts` script islands without Node, in a
live page and a page that isn't) and `islands-vue` (a `.vue` island; it
needs Node, `npm ci --prefix testdata/node/islands`).
`VUKA_CONFORMANCE_OUT=dir` writes each run's transcript there.

## Plan

Folded into roadmap Phase 2:

| Step | Item | Roadmap |
|---|---|---|
| 0 | Generated-code contract: public-API-only check on goldens, `vuka eject` — **done** | 2.3 |
| 1 | Public registration API (`web.Routes`, `app.Get/Live/Controller/Service`), transpiler emits it — **done** | 2.5 |
| 2 | Markup in Go — now `.vuka.html` templates and `ui.HTML` ([templates.md](templates.md)); transpiler emits them — **done** | 2.3, 2.4 |
| 3 | `orm.Schema` / `ui.LiveSchema`; `.vuka` attributes and hooks lower to them — **done** | 2.1, 2.2 |
| 4 | orm reflection fallback, live render fallback, call decorators through `vuka.Wrap` with typed adapters as a speed-up — **done** | 2.6 |
| 5 | `web.Dev()`: Tailwind, Vite and the islands for `go run .` in development — **done** | 2.6 |
| 6 | `vukavet` analyzer — **done** | 2.6 |
| 2a | Island path inference without the CLI: bare `ui.RegisterIsland[T]()`, the transpiler emits it — **done** | 2.5, 2.6 |
| 2b | Islands built for plain Go: `go tool vuka build-islands` into the frontend's build, `web.Dev()` keeps them built, `vuka-live.d.ts` from Go types — **done** | 2.6 |
| 4a | Overloads named as Go names them (`areaCircle`, `UpdateNewMessage`), field references by address (`vuka.RefTo`), orm arithmetic and typed `SaveFields` — **done** | 2.3, 2.6 |
| 7 | Conformance suite — **done** | 2.6 |
| 8 | Hooks declared by the constructor (§3) — **done**, 1.1 | 2.2 |
| 9 | Folder routes: `pages/` folders as routes, layouts with `Load`, typed URLs, `vuka routes` (§1) — 1.1 | 2.5 |

Step 0 is done: `TestContract` (transpile) checks every golden for gofmt,
one import block, no `/*line*/` markers and no `__` names, and checks its
calls into vuka and ui against `transpile/testdata/contract.txt`. Generated
files carry `//line` directives only where the lines stop following the
source, real names and their doc comments, and `vuka eject -rm` turns an app
into plain Go.

Step 1 is done: registry decorators lower to `web.Routes` calls, type
decorators and declarers to `vuka.TypeFor` and `vuka.DeclOf`, constructors
are `NewX`. Public now, as the calls a person writes: `vuka.Decorated`,
`Instance(s)` (a decorated function built on first use, which recursion
needs), `RegisterFunc`, `RegisterType` (the attribute registry), `FileOf`
(an embedded file), `ui.IslandPage` (a live island page's one-line
`Render`) and `ui.RegisterIsland` (an imported component's props type).

Step 2 is done, and then reworked by [templates.md](templates.md): each
piece of JSX is a `ui.HTML` call at its place, its holes labelled with their
Go; blocks are statements before the return appending to a `[]ui.Node` a
hole names; `.vuka.html` templates compile the same way; stateful components are `&C{…}` or `ui.Key`,
islands their props or `ui.Mount`; states and expressions of them are
package uigen's (§2). The
fingerprinted markup, `ui.Component`, `ui.MountIsland`, the state binding
(`ui.BindExpr`, once `ui.Bind`), `ui.NewState`, `ui.PageState`,
`ui.NewDerived` and `ui.StateField` are gone from generated code; `ui.Try`, `ui.WithChildren`, `ui.Callback`, `ui.Emit`,
`ui.StateProp` and `ui.Ptr` are public as they are.

Step 3 is done: a type embedding `orm.Base` (or `orm.Model`) gets a
`Schema` method from its attributes — `X @a @b` is `s.Field(&p.X, a, b)`,
the embedded base's are `s.Model(…)`, a bare `@orm.PK` is `orm.PK{}` — and
a type embedding `ui.Live` a `LiveSchema` method from its fields'
attributes and its methods' hooks (`@ui.Mount` on `mount` is
`l.Mount(d.mount)`). An embedded `orm.Base` is written `orm.Model[Post]`, what plain Go
embeds (`func (Base[Self]) vukaPlain(Model[Self])` names it), unless the
code names the field `Base`. The two types say which method they want with a
marker method, `vukaSchema(interface{ Schema(*orm.Schema) })`, so the
transpiler names no package. `Post.Slug` is `PostFields.Slug`,
`Post.Objects` is `orm.Objects[Post]()`, a static `func Post.Live()` is
`PostLive()` and any static `TypeMember` (`userCount` unexported). An
around decorator — `func(ctx, func(ctx) error, opts…) error`, like
`orm.Transaction` — wraps the function's body, its options the
declaration's decorators of the option type (`@orm.TxOn("crm")`). The
vuka command overlays what `orm generate` would write (`post_gen.go`
beside `post.vuka`) until it is written. Old tags (`bind:`, `url:`,
`defer:`, `validate:`) and hooks found by name (`Mount`, `Changed`,
`Load<Group>`) still work for one minor release: `vuka vet` warns (W0302,
W0303) and `vuka fix attributes` / `vuka fix hooks` (or `--attributes`,
`--hooks`) rewrite them. New codes: E0129 (attributes and a hand-written
schema method), E0130 (a hook written as a composite literal), E0131 (a
function an around decorator can't wrap), E0335 and E0336 (`LiveSchema`
fields and hooks, at boot and in `vukavet`). Public now: `RegisterFields`
(field attributes of a struct with no schema method) and `StaticOf`,
`Static`, `Statics` (a static of a generic type: one value per
instantiation, which Go has no variable for).

Step 4 is done: a call decorator lowers to `vuka.Wrap(fn, vuka.FuncInfo{…},
decorators…)`, the call a person writes, which packs each call by
reflection; the vuka command writes a typed adapter per signature the
package's decorators wrap into `vuka_calls_gen.go`
(`vuka.RegisterAdapter`), which `Wrap` uses in its place — a speed-up the
main file doesn't depend on, ejected and committed like `post_gen.go`. A
call through two decorators: 500ns by reflection, 110ns with the adapter,
95ns through the adapter older commands inlined (`vuka.NewCall`, still
working, deprecated).

Step 4a closes what was left: an overload is named after its parameters'
types as a person names such functions — `area(c Circle)` is `areaCircle`,
`Add(a, b int)` `AddIntInt`, a live component's `Update(m NewMessage)`
`UpdateNewMessage`, which `ui.IsUpdate` takes for an Update (and
`Update__T`, the old name, for one minor release); a field reference on a
struct with no typed fields is the field by address,
`vuka.StringRefTo(func(p *Post) *string { return &p.Author.Related().Name })`
(`RefOf` with index paths deprecated); the orm's number fields take
arithmetic (`orm.Set{"views": PostFields.Views.Add(1)}`) and `SaveFields`
typed fields. The contract file has no pending entries: everything
generated code calls is public API.

Steps 2a and 2b are done: `ui.RegisterIsland[T](args…)` takes what
`ui.Island` takes and, bare, leaves the component to the vuka command's
inference; the transpiler writes it for `@ui.Island` (a type decorator's
`Register<Name>[T]` twin). `go tool vuka build-islands` reads the
registrations from Go source — `ui.RegisterIsland[T]`, a route's
`ui.Island` option (the page its constructor returns), the older generated
form — and builds them with the same code `vuka build` and `vuka run` use,
into `web/dist/.vuka/`; `-watch` is what `web.Dev()` runs.
`examples/islands-go` is such an app, a Vue island in a live page and a page
backed by its component; `TestIslandsGoE2E` (cmd/vuka) builds it with
`go tool vuka build-islands` and `go build`, mounts both in Chrome, and again
under `web.Dev()` with `VUKA_DEV=1`.
