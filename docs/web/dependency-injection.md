# Dependency Injection

[[toc]]

## Introduction

Each app has a container. A service is a struct with a constructor whose
parameters are its own dependencies, registered in `main` with
`app.Service(NewX)`. A handler, a hook or another service takes it as a
parameter, by type, and the container builds it once and hands the same
value to everyone.

Web checks the whole graph when the app is built: every route's, hook's and
handler's parameters, before anything is served.

<!-- go:context
type Pet struct{ Name string }

func (s *PetStore) All() ([]Pet, error) { return nil, nil }

func (db *DB) Ping(ctx context.Context) error { return nil }
func (db *DB) Close() error                   { return nil }

func (c Config) Validate() error { return nil }

type Cache struct{}

func NewCache(cfg Config) *Cache                                { return &Cache{} }
func (c *Cache) Stats() map[string]int                         { return nil }
func cacheHeaders(next http.Handler) http.Handler               { return next }
func flush(ctx context.Context) error                           { return nil }
func migrate(ctx context.Context) error                         { return nil }

type Mailer struct{}

func NewMailer() *Mailer { return &Mailer{} }

var cfg Config

type Todo struct{ Text string }

type NewTodo struct {
	Text string `form:"text" validate:"required"`
}

type Todos struct{ rows []Todo }

func (t *Todos) All() []Todo     { return t.rows }
func (t *Todos) Add(text string) { t.rows = append(t.rows, Todo{text}) }
-->

## Writing Services

Services use ordinary constructors in `.go` or `.vuka` logic files. A
constructor's parameters identify its dependencies, and a construction
failure stops startup. Here `NewGreeter` depends on the `*Config` that
`NewConfig` builds:

```go
package services

type Config struct { Prefix string }
type Greeter struct { config *Config }

func NewConfig() *Config { return &Config{Prefix: "Hello"} }
func NewGreeter(c *Config) *Greeter { return &Greeter{config: c} }
func (g *Greeter) Greet(name string) string { return g.config.Prefix + ", " + name }
```

The app registers both constructors explicitly with `app.Service(NewX)`,
before Build or Run. Handler and decorated hook parameters are then filled by
type: the context, a form struct, the HTTP request, the signed-in identity
and registered services.

In a shape, an unexported field of a provided type is injected; other
unexported fields are tracked server state.

### Dependency Fields {#dependency-fields}

A page under `pages/` declares its dependencies as unexported fields of
provided types, and needs no constructor for them:

```html
<!-- pages/users/index.vuka.html -->
<script lang="go">
type Index struct {
	users *users.Service
	rows  []users.User
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows = p.users.List(ctx)
	return nil
}

func (p *Index) Post(ctx context.Context, f users.Form) (web.Redirect, error) { … }
</script>
```

`pages/route.go` builds the page with `&Index{}` (or its `NewIndex`, when
it has one), and web then injects each unexported field whose type the
app provides — `users` here — before the page's parameters bind and its
mount hook runs. The same holds for the receiver of the page's `Post`,
`Put`, `Patch` and `Delete` handlers, for a live page, for a page-backed
island, and for a folder's layouts and guards. Only fields of provided
types are touched: `rows` is tracked state, and a `ui.State`, a field the
compiler tracks, an exported prop or an unexported field of a type nothing
provides is left as the constructor made it, with no error. A dependency
field the URL would bind too — named like a path parameter (`users` on
`/users/{users}`) or tagged `url:` — is [E0419](/errors/E0419) at
start-up.

## Registering Services

In `main.go`, register each constructor on the app:

```go
// main.go (service types and constructors live in logic files)
package main

import (
    "log"
    "example.com/app/pages"
    "github.com/vuka-lang/web/v2"
)

type DB struct { url string }
func OpenDB() *DB { return &DB{url: web.Env("DATABASE_URL", "sqlite:app.db")} }

type PetStore struct { db *DB }
func NewPetStore(db *DB) *PetStore { return &PetStore{db: db} }

func main() {
    app := web.New(pages.Routes())
    app.Service(OpenDB)
    app.Service(NewPetStore)
    log.Fatal(app.Run(""))
}
```

The rendering page belongs in a `.vuka.html` shape under `pages/`. It
receives `PetStore` as a provided field or a typed hook or handler
parameter, and it does not register its own route or service.

## Providers

Three methods put values in the container:

| | |
|---|---|
| `app.Service(NewS)` | a service built by `func(deps…) T` or `func(deps…) (T, error)` |
| `app.Provide(value)` | an already constructed configuration or resource value; library options can use `web.Provide(value)` |
| `app.Controller(prefix, NewC)` | an API controller built by its constructor; page rendering still belongs in the page tree |

A constructor's parameters are injected by type, and three rules hold for
every value:

- Every value is a **lazy singleton per app**: built the first time something
  needs it, then shared. Two apps (in tests, say) have their own.
- Dependencies match by **exact type**. To inject an interface, provide a
  constructor returning the interface; there are no named values or groups.
- A constructor's error fails `app.Start` (and `Run`).

## Parameters by Type {#parameters-by-type}

Hooks and handlers declare what they need by parameter type, and each
parameter is filled from the one source its type names. The rule is the same
for a route's handler, a controller's method, a live page's hooks
(`@ui.Mount`, `@ui.OnChange`, `@ui.Loads`) and event handlers, a layout's or
guard's `Load`, and a middleware's constructor:

| Parameter | Filled from |
|---|---|
| `context.Context` | the request's, or the live session's |
| a struct, or pointer to one, that isn't provided | the form or JSON body (a route), the event's form fields (a live handler); its `validate:` tags checked, 422 |
| `url.Values` | the form's fields, as sent |
| a string, number, bool or `encoding.TextUnmarshaler` | a route: the path segment of its name, else the query parameter; a live handler: the event's value (a keyboard event's key, else the element's value) |
| `*http.Request`, `http.ResponseWriter` | the request (a live page's: the one it joined with) |
| `auth.User`, or the app's request identity (`web.ParamFrom`) | the signed-in visitor |
| a service type registered with `app.Service` | the container |

The compiler can read scalar parameter names in a shape header, so path
segments bind by matching name and query values use the corresponding name.
For a plain Go API registration whose parameter names are unavailable at
runtime, scalars map to trailing path segments in order; put query values
in a form/query struct. Type matching takes priority over positional binding.
One payload struct is bound and validated; a provided struct is a service,
not a second request body.

Declare form data in a logic file and the page's single rendering struct in
its shape. In `pages/index.vuka.html`:

```html
<script lang="go">
package pages

import (
    "example.com/app/store"

    "context"
    "net/http"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Index struct {
    rows []models.Todo
}

@ui.Mount
func (p *Index) mount(ctx context.Context, store *services.Todos, r *http.Request) error {
    p.rows = store.All()
    return nil
}

func (p *Index) Add(ctx context.Context, f models.NewTodo, store *services.Todos) error {
    store.Add(f.Text)
    p.rows = store.All()
    return nil
}
</script>

<section>
  <form onSubmit={Add}><input name="text" /><button>Add</button></form>
  for _, row := range rows {
    <p>{row.Text}</p>
  }
</section>
```

A live page's exported methods are callable event methods. A closure can
capture ordinary arguments (for example an ID and a bool); those values are
not extra independent event payloads. Provided parameters are still checked
even on methods whose direct payload signature is not an event signature.
An unfillable parameter is reported with its method, source file, line and type.

## Starting and Stopping Services {#lifecycle}

A provided value with a `Start` or `Stop` method takes part in the app's
life:

```go
func (db *DB) Start(ctx context.Context) error { return db.Ping(ctx) }
func (db *DB) Stop(ctx context.Context) error  { return db.Close() }
```

`app.Start` (and `Run`) builds what the routes need, then runs the start
sequence; `app.Stop` (and `Run`'s shutdown) runs the stop sequence, its exact
reverse. The sequence holds, in order (register services before building the app):

1. ready values given to `Provide`, and hooks added with `OnStart`/`OnStop`,
   in the order they were added — a ready value was given explicitly, so it
   starts even when nothing needs it;
2. values built from constructors, in dependency order — only those
   something needs (a route, a live page, another value), and those provided
   with `Eager`.

A value implementing `Start(context.Context) error` (`web.Starter`) is
started, one implementing `Stop(context.Context) error` (`web.Stopper`)
stopped. The first failing start ends `Start` with its error; every stop
runs, and their errors are joined.

### Hooks and Eager Services

These options add hooks and start constructors nothing needs:

| | |
|---|---|
| `web.OnStart(fn)`, `web.OnStop(fn)`, `app.OnStart`, `app.OnStop` | hooks, `func(context.Context) error` |
| `web.Eager(ctors…)`, `app.Eager` | constructors built and started even when nothing needs them |

For example, this app starts its configuration, a migration hook and a
mailer no route takes:

<!-- go:fragment -->
```go
app := web.New(
	web.Provide(cfg),      // cfg.Start, if any, runs first
	web.OnStart(migrate),
	web.Eager(NewMailer),  // started though no route takes it
)
```

## Build Errors

`app.Build()` resolves everything and returns all problems at once, each at
the file and line of the handler or hook, naming the parameter and its type:

```text
error[E0407]: GET / (live page pages.Index): Index.Add's 2nd parameter, *sql.DB, is not provided
 --> index.vuka.html:41
  = help: register it in main with app.Service(NewX)
error[E0405]: GET /items (live page pages.Item): Item.mount's 2nd parameter, chan int, can't come from a hook's call, which gives the context
 --> item.vuka.html:12
error[E0409]: dependency cycle: *main.A -> *main.B -> *main.A
```

A parameter nothing fills ([E0407](/errors/E0407) for a pointer or interface
nothing provides, [E0405](/errors/E0405) for a type a request or an event
can't carry), missing providers, cycles and two providers of one type are
Build errors. `Handler`, `Start` and `Run` call `Build` first, so a broken
app never serves.

## Options From Libraries

A library hands the app one option that does all it needs: `web.Options`
groups options (nil ones are skipped), and `web.OptionFunc` runs code against
the app inside `New`, before `Build`, where every `App` method is open to it.
`web.Check(fn)` (or `app.Check`) adds a check whose error is one of `Build`'s,
reported with the routes' and the container's. This option provides a
configuration, registers a service, a middleware, a route and a stop hook,
and checks the configuration:

```go
func CacheOption(cfg Config) web.Option {
	return web.Options(
		web.Provide(cfg),
		web.OptionFunc(func(a *web.App) {
			a.Service(NewCache)
			a.Use(cacheHeaders)
			a.Get("/cache/stats", (*Cache).Stats)
			a.OnStop(func(ctx context.Context) error { return flush(ctx) })
		}),
		web.Check(func(*web.App) error { return cfg.Validate() }),
	)
}

func newApp() *web.App { return web.New(CacheOption(cfg), web.Dev()) }
```

The ORM's [`ormweb.Databases`](/orm/databases#web-integration) is such an
option.

## Resolving a Service

`web.Resolve[T](app)` returns the app's `T`, built with its dependencies —
for `main` and [tests](/web/testing).
