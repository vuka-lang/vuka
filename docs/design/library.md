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
  `app.Get`, `app.Live`, `ui.Template`, `orm.Schema`, `ui.LiveSchema`, … —
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

Two forms stay a decorator's own rather than an option: a composed
decorator (`vuka.Bundle`, such as `@auth.Required`) on a function wraps the
function as well — its gate checks direct calls too — and the route reads
its attributes from the function; and decorators of libraries without a
registry lower generically, to calls a person writes as well:
`Model(vuka.TypeFor[User](NewUser))` for a type decorator,
`Command("greet")(vuka.DeclOf(greet))` for a declarer, `b.Declare(d)` and
`b.Decorate(t)` for a bundle. The forms older compilers wrote
(`vuka.TypeWith`, `vuka.FuncDecl`, `vuka.DeclareWith`, `web.Get(path)` as a
bundle, `X_New`) keep working, deprecated.

`examples/library-users` is an app written this way, with its `.vuka`
twin; `TestLibraryExample` (cmd/vuka) builds it with plain `go build`, ejects
the twin and checks both are written alike (the same `web.Routes` calls, the
same templates) and answer the same.

## 2. Markup

### Templates with typed holes (default)

```go
var orderRow = ui.Template(`
<tr key="{{0}}" class="{{1}}">
  <td>{{2}}</td>
  <td class="tabular-nums">{{3}}</td>
  <td><button onClick="{{4}}">Cancel</button></td>
</tr>`)

func (p *Orders) row(o Order) ui.Node {
	return orderRow.Render(o.ID, ui.Class{"row": true, "late": o.Late}, o.Ref, money(o.Total),
		func() { p.Cancel(o.ID) })
}
```

- The same `ui.Template` readable generated code uses (2.3), so performance is
  identical to transpiled JSX: parsed once at init, static parts precomputed,
  and under a live session the same render tree (statics, dynamics, keyed
  lists) JSX lowers to.
- Holes are escaped by position kind: child (`ui.Node`, `[]ui.Node`, text,
  a `ui.State`, a stateful component, an island), text (in title, textarea,
  script), attribute value (what a JSX attribute takes; an `on…` attribute
  takes a handler — a func, `ui.On(…)`, JS commands), part of a value
  (text), attribute set (`ui.Attr`, `[]ui.Attr`, `templ.Attributes`,
  `ui.BindAttr(…)`). A value of the wrong kind for its hole is a render-time
  error naming the template's line and the hole, and a vet error where the
  analyzer can see it (§7); a malformed template panics at init.
- Loops and conditionals are ordinary Go producing nodes:
  `ui.Each(rows, OrderRow)`, `ui.If(cond, node)`, or a `[]ui.Node`.
- A hole is a place: a stateful component (`&Counter{…}`,
  `ui.Key(id, &Counter{…})`) or an island (`Chart{…}`,
  `ui.Mount(Chart{…}, ui.Idle())`) in it is identified by it, and a list's
  components by the hole the list is in. Client state is declared where it is
  called: `open := ui.UseState(false)`, `var theme = ui.UsePageState("light")`.
- An expression of states is State's methods, a derived state the browser
  evaluates again when a state it reads changes — what a `.vuka` file's
  `!open && tab == "env"` lowers to:

  ```go
  ready := open.Not().And(tab.Eq("env"))           // !open && tab == "env"
  q := ui.At[string](f, "Q")                       // f.Q, a state of its own
  hint := ui.Derived("Hello, ").Plus(name)         // "Hello, " + name
  cls := ui.Classes{"menu": true, "is-open": open} // ui.Class{…} reading a state
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
	orm.Base[Applications]
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

## 4. Generated code is optional — a speed-up, not a requirement

| Generated today | Without it (`go run .`) | With `go generate` |
|---|---|---|
| orm row scanners and writers | reflection: the ORM's own cost 1.1–1.7× (1000 rows: 1.34× at 6 columns, 1.67× at 23), 1.05–1.07× end to end on SQLite | generated, as now |
| orm field sets (`<Model>Fields`) | `orm.Q{"…"}` / `orm.F("…")`, checked at run time | typed |
| orm `Register[T]()` | a model's `orm.For[T]()` (or first `Objects`) registers it | at init |
| live render tracking (`ui.F` frames from JSX) | components built from nodes diff whole: the same page; 1.7–2.2× the bytes on the demo pages' random events | fine-grained, as now; `ui.Template` builds frames at init, no generation needed |
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
  component replies with `live.js`. The conformance suite (§8) will run
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
- `templates`: `ui.Template` hole count (E0127, new) and hole kind against
  the value's static type (E0128, new), templates of other packages through
  facts;
- `schema`: `s.Field` not given the receiver's field (E0501), a nil
  attribute (E0502), a field behind a pointer (E0503).

`live` reads struct tags today; once `ui.LiveSchema` lands (step 3) it reads
the `l.Field`/`l.Loads` calls the same way. gopls can't load third-party analyzers, so editors get them from `go vet`;
a `.vuka` app runs them as `vuka vet -vettool=$(go tool -n vukavet)`, the
positions its `.vuka` lines.

## 8. Conformance

Every example app exists twice — `.vuka` and hand-written Go — and a test
compares rendered HTML, live patches, SQL and HTTP responses, with and
without the optional `*_gen.go` speed-ups. A third run ejects the `.vuka`
version (generated Go only, sources deleted) and must match too. A
difference is a bug.

## Plan

Folded into roadmap Phase 2:

| Step | Item | Roadmap |
|---|---|---|
| 0 | Generated-code contract: public-API-only check on goldens, `vuka eject` — **done** | 2.3 |
| 1 | Public registration API (`web.Routes`, `app.Get/Live/Controller/Service`), transpiler emits it — **done** | 2.5 |
| 2 | `ui.Template` with typed holes; transpiler emits it — **done** | 2.3, 2.4 |
| 3 | `orm.Schema` / `ui.LiveSchema`; `.vuka` attributes and hooks lower to them — **done** | 2.1, 2.2 |
| 4 | orm reflection fallback, live render fallback (**done**); call decorators through a reflective wrapper (open) | 2.6 |
| 5 | `web.Dev()` | 2.6 |
| 6 | `vukavet` analyzer — **done** | 2.6 |
| 2a | Island path inference without the CLI: bare `ui.RegisterIsland[T]()`, the transpiler emits it — **done** | 2.5, 2.6 |
| 2b | Islands built for plain Go: `go tool vuka build-islands` into the frontend's build, `web.Dev()` keeps them built, `vuka-live.d.ts` from Go types — **done** | 2.6 |
| 7 | Conformance suite | 2.6 |

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

Step 2 is done: each piece of JSX is a package-level `ui.Template` named
after its declaration, rendered with its expressions in document order;
blocks are `ui.Each`, a loop appending to a `[]ui.Node`, or an if or match
picking one node for `ui.If`; stateful components are `&C{…}` or `ui.Key`,
islands their props or `ui.Mount`; states are `ui.UseState` /
`ui.UsePageState` and expressions of them State's methods (§2). The
fingerprinted markup, `ui.Component`, `ui.MountIsland`, the state binding
(`ui.BindExpr`, once `ui.Bind`), `ui.NewState`, `ui.PageState`,
`ui.NewDerived` and `ui.StateField` are gone from generated code; `ui.Try`, `ui.WithChildren`, `ui.Callback`, `ui.Emit`,
`ui.StateProp` and `ui.Ptr` are public as they are.

Step 3 is done: a type embedding `orm.Base` (or `orm.Model`) gets a
`Schema` method from its attributes — `X @a @b` is `s.Field(&p.X, a, b)`,
the embedded base's are `s.Model(…)`, a bare `@orm.PK` is `orm.PK{}` — and
a type embedding `ui.Live` a `LiveSchema` method from its fields'
attributes and its methods' hooks (`@ui.Mount` on `mount` is
`l.Mount(d.mount)`). The two types say which method they want with a
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

What the contract file still allows, by the step that removes it:

- step 3: field references by index path (`vuka.RefOf` and its kinds) on
  a struct with no generated typed fields (`Stats.Author` of a plain
  struct): their public form, a field named by address, is a follow-up;
- step 4: call decorators' per-signature adapter (`vuka.Func`,
  `vuka.NewCall`): its public form is a reflective wrapper, with the typed
  adapter an optional speed-up like the other step 4 fallbacks.

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
