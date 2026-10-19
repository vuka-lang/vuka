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

```go
func main() {
	app := web.New(
		web.Frontend(dist, "web/dist"),
		orm.Use(databases),
	)
	app.Get("/orders", ListOrders, auth.Required())
	app.Live("/dashboard", NewDashboard, ui.Layout(layouts.AdminPage("Dashboard")))
	app.Island("/applicants/{id}", NewApplicantDetail,
		ui.Component("islands/applicants/ApplicantDetail.vue"),
		ui.Layout(layouts.AdminPage("Applicant")))
	app.Run()
}
```

| `.vuka` | Go |
|---|---|
| `@web.Get("/orders") func ListOrders(…)` | `app.Get("/orders", ListOrders)` |
| `@web.Live("/x") type X struct{ ui.Live … }` | `app.Live("/x", NewX)` |
| `@web.Live` + `@ui.Island` | `app.Island("/x", NewX, ui.Component(path))` |
| `@auth.Required` | `auth.Required()` option |
| `@web.Service type S` | `app.Provide(NewS)` |

Today these lower to `web.Get(path)(decl)` and `vuka.TypeWith`; those stay
as the transpiler's target until both forms emit the calls above. Component
paths may be inferred exactly as bare `@ui.Island` infers them, from the
type's package and name.

## 2. Markup

### Templates with typed holes (default)

```go
var orderRow = ui.Template(`
<tr class="{{0}}">
  <td>{{1}}</td>
  <td class="tabular-nums">{{2}}</td>
</tr>`)

func OrderRow(o Order) ui.Node {
	return orderRow.Render(ui.Class("row", o.Late && "late"), o.Ref, money(o.Total))
}
```

- The same `ui.Template` readable generated code uses (2.3), so performance is
  identical to transpiled JSX: parsed once at init, static parts precomputed.
- Holes are escaped by position kind: text, attribute value, attribute set
  (`ui.Attrs`, `ui.On(…)`, `ui.BindAttr(…)`), child node (`ui.Node`). A value of
  the wrong kind for its hole is a render-time error in development and a
  vet error where the analyzer can see it (§7).
- Loops and conditionals are ordinary Go producing nodes:
  `ui.Each(rows, OrderRow)`, `ui.If(cond, node)`.

### Builders (alternative)

`h.Div(h.Class("…"), h.Text(x), children…)` for code that prefers calls
over strings. Same nodes, no template parse.

### templ

Existing templ support stays; templ components are `ui.Node`s.

## 3. Field configuration: schema methods, not attributes

### orm

```go
type Order struct {
	orm.Model[Order]
	ID   int64
	Ref  string
	Paid bool
}

func (Order) Schema(s *orm.Schema[Order]) {
	s.Table("orders")
	s.Field(func(o *Order) any { return &o.Ref }).Char(64).Unique()
	s.Field(func(o *Order) any { return &o.Paid }).Default(false)
}
```

- Fields are selected by pointer, so renames are compiler-checked.
- Simple cases may use struct tags (`orm:"char=64,unique"`); the schema
  method wins when both exist.
- `@mirror.Column{Name: "x"}` ≡ `s.Field(…).Under("legacy").Column("x")`;
  `@mirror.Omit` ≡ `.Under("legacy").Skip()`.

### Live and island pages

```go
type Dashboard struct {
	ui.Live
	Q     string
	Rows  []Task
	Total int
}

func (Dashboard) LiveSchema(l *ui.LiveSchema[Dashboard]) {
	l.Bind(func(d *Dashboard) any { return &d.Q }).Debounce(300 * time.Millisecond).URL("q").Max(100)
	l.Defer("lists", (*Dashboard).loadLists, func(d *Dashboard) []any { return []any{&d.Rows, &d.Total} })
	l.OnChange((*Dashboard).changed)
	l.Mount((*Dashboard).mount)
}
```

These are the explicit hooks of roadmap 2.2: hooks may be unexported, only
exported methods without a hook are callable from the browser, and signature
mistakes are compile errors (the method expressions are typed). `.vuka`
attributes (`@ui.Bind{…}`, `@ui.Defer("lists")`, `@ui.Loads("lists")`,
`@ui.OnChange`) lower to exactly these calls.

## 4. Generated code is optional — a speed-up, not a requirement

| Generated today | Without it (`go run .`) | With `go generate` |
|---|---|---|
| orm row scanners, field sets | reflection fallback (≈1.5–2× slower scans) | generated, as now |
| live render tracking (changed spots only) | whole-component diffs: correct, larger patches | fine-grained, as now |
| `vuka-live.d.ts` (island TypeScript types) | islands work, untyped | typed |

- Each package checks at init whether generated code registered itself and
  falls back otherwise; behaviour is identical, only cost differs.
- One line opts in: `//go:generate go tool vuka gen`.
- The fallbacks are tested by running the conformance suite (§8) with
  generation off.

## 5. Frontend assets

- **Production:** `//go:embed all:web/dist` of a prebuilt bundle, as today.
- **Development with `go run .`:** `web.Dev()` — active only when
  `VUKA_DEV=1` or the environment is development — starts Tailwind and Vite as
  child processes when they're installed and serves through them; otherwise
  it serves the last build and logs one line saying how to get live CSS and
  islands.
- Without Node, everything except islands and Tailwind works.

## 6. What the CLI is for

Convenience, never a requirement: the watcher (`go tool vuka run`),
scaffolding (`vuka new`, `vuka add`), `vuka upgrade`, diagnostics with codes,
the language server.

## 7. Checks without the CLI

The vet-time checks (missing loader, handle clash, bad bind/validate,
template hole kinds, route conflicts) are packaged as a `go/analysis`
analyzer, runnable as `go vet -vettool=$(go tool -n vukavet)` or through
gopls, with the same diagnostic codes (E03xx, E04xx).

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
| 1 | Public registration API (`app.Get/Live/Island/Provide`), transpiler emits it | 2.5 |
| 2 | `ui.Template` with typed holes; transpiler emits it | 2.3, 2.4 |
| 3 | `orm.Schema` / `ui.LiveSchema`; `.vuka` attributes and hooks lower to them | 2.1, 2.2 |
| 4 | orm reflection fallback; live render fallback | 2.6 |
| 5 | `web.Dev()` | 2.6 |
| 6 | `vukavet` analyzer | 2.6 |
| 7 | Conformance suite | 2.6 |

Step 0 is done: `TestContract` (transpile) checks every golden for gofmt,
one import block, no `/*line*/` markers and no `__` names, and checks its
calls into vuka and ui against `transpile/testdata/contract.txt`. Generated
files carry `//line` directives only where the lines stop following the
source, real names and their doc comments, and `vuka eject -rm` turns an app
into plain Go. What the contract file still allows, by the step that removes
it:

- step 1: decorator wrappers and declaration records (`vuka.Decorated`,
  `Instance(s)`, `NewCall`, `Func`, `FuncDecl`, `MethodDecl`,
  `RegisterFunc`, `RegisterType`, `TypeWith`, `DeclareWith`,
  `DecorateType`, `FileOf`), `ui.IslandPage`, `ui.RegisterIsland`, and
  `X_New` constructors;
- step 2: the fingerprinted markup (`ui.F`, `ui.FT`, `ui.El`, `ui.Attr`,
  `ui.Child`, `ui.Text`, `ui.Nodes`, `ui.Component`, `ui.MountIsland`,
  `ui.Try`, `ui.WithChildren`) and state lowering (`ui.Bind`,
  `ui.NewState`, `ui.PageState`, `ui.NewDerived`, `ui.StateField`,
  `ui.StateProp`, `ui.Callback`, `ui.Emit`, `ui.Ptr`);
- step 3: field references by index path (`vuka.RefOf` and its kinds),
  `vuka.RegisterFields` and statics (`vuka.Static`, `StaticOf`, `Statics`).
