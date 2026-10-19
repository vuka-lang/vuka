# Dependency injection

Each app has a container. A handler, a controller or a service names what it
needs as a parameter or a field; the container builds it once and hands the
same value to everyone.

Web checks the whole graph when the app is built: every route's
dependencies, before anything is served.<span class="vuka-only"> Vuka writes
the constructors (from a struct's fields, see
[Dependency injection](/features/dependency-injection)).</span><span class="go-only">
Go writes the constructors, `func(deps…) T`.</span>

<!-- go:context
type Pet struct{ Name string }

func (s *PetStore) All() ([]Pet, error) { return nil, nil }

func (db *DB) Ping(ctx context.Context) error { return nil }
func (db *DB) Close() error                   { return nil }

type Config struct{ Path string }

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
-->

::: vuka
```vuka
type DB struct{ … }

func OpenDB() *DB { … }

@web.Service
type PetStore struct {
	db *DB
}

@web.Get("/pets")
func ListPets(store *PetStore) Result[[]Pet] { return store.All() }

func main() {
	app := web.New(web.Provide(OpenDB))   // or app.Provide(OpenDB)
	app.Run("")
}
```
:::

::: go
```go
type DB struct{ url string }

func OpenDB() *DB { return &DB{url: web.Env("DATABASE_URL", "sqlite:app.db")} }

type PetStore struct {
	db *DB
}

func NewPetStore(db *DB) *PetStore { return &PetStore{db: db} }

func ListPets(store *PetStore) ([]Pet, error) { return store.All() }

func init() {
	web.Routes.Service(NewPetStore)
	web.Routes.Get("/pets", ListPets)
}

func main() {
	app := web.New(web.Provide(OpenDB)) // or app.Provide(OpenDB)
	log.Fatal(app.Run(""))
}
```
:::

## Providers

| | |
|---|---|
| `web.Provide(ctors…)`, `app.Provide(ctors…)` | constructors — `func(deps…) T` or `func(deps…) (T, error)` — and ready values (anything that isn't a function) |
| <span class="vuka-only">`@web.Service`, </span>`web.Routes.Service(NewS)`, `app.Service(NewS)` | a struct built <span class="vuka-only">from its fields (or </span>by its constructor<span class="vuka-only">)</span> |
| <span class="vuka-only">`@web.Controller(prefix)`, </span>`web.Routes.Controller(prefix, NewC)`, `app.Controller(prefix, NewC)` | the same, with [routes](/web/routes#controllers) |

<span class="vuka-only">A service's or controller's fields are injected by type, unexported ones
included; tag a field `inject:"-"` to leave it out. A struct with its own
static `New` (Go's `NewX`) is built with that one.</span><span class="go-only">A
constructor's parameters are injected by type.</span>

- Every value is a **lazy singleton per app**: built the first time something
  needs it, then shared. Two apps (in tests, say) have their own.
- Dependencies match by **exact type**. To inject an interface, provide a
  constructor returning the interface; there are no named values or groups.
- A constructor's error fails `app.Start` (and `Run`).

## Lifecycle

A provided value with a `Start` or `Stop` method takes part in the app's
life:

::: vuka
```vuka
func (db *DB) Start(ctx context.Context) error { return db.Ping(ctx) }
func (db *DB) Stop(ctx context.Context) error  { return db.Close() }
```
:::

::: go
```go
func (db *DB) Start(ctx context.Context) error { return db.Ping(ctx) }
func (db *DB) Stop(ctx context.Context) error  { return db.Close() }
```
:::

`app.Start` (and `Run`) builds what the routes need, then runs the start
sequence; `app.Stop` (and `Run`'s shutdown) runs the stop sequence, its exact
reverse. The sequence holds, in order:

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

| | |
|---|---|
| `web.OnStart(fn)`, `web.OnStop(fn)`, `app.OnStart`, `app.OnStop` | hooks, `func(context.Context) error` |
| `web.Eager(ctors…)`, `app.Eager` | constructors built and started even when nothing needs them |

::: vuka
```vuka
app := web.New(
	web.Provide(cfg),         // cfg.Start, if any, runs first
	web.OnStart(migrate),
	web.Eager(NewMailer),     // started though no route takes it
)
```
:::

::: go
<!-- go:fragment -->
```go
app := web.New(
	web.Provide(cfg),      // cfg.Start, if any, runs first
	web.OnStart(migrate),
	web.Eager(NewMailer),  // started though no route takes it
)
```
:::

## Build errors

`app.Build()` resolves everything and returns all problems at once, each
naming the route, its source position and the parameter:

```text
web: GET /admin (main.Admin at admin.vuka:9): parameter db *sql.DB: no provider for *sql.DB, needed by main.Admin at admin.vuka:9
web: dependency cycle: *main.A -> *main.B -> *main.A
```

Missing providers, cycles and two providers of one type are Build errors.
`Handler`, `Start` and `Run` call `Build` first, so a broken app never
serves.

## Options from libraries

A library hands the app one option that does all it needs: `web.Options`
groups options (nil ones are skipped), and `web.OptionFunc` runs code against
the app inside `New`, before `Build`, where every `App` method is open to it.
`web.Check(fn)` (or `app.Check`) adds a check whose error is one of `Build`'s,
reported with the routes' and the container's.

```go
func CacheOption(cfg Config) web.Option {
	return web.Options(
		web.Provide(cfg, NewCache),
		web.OptionFunc(func(a *web.App) {
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

`web.Resolve[T](app)` returns the app's `T`, built with its dependencies —
for `main` and [tests](/web/testing).
