# Dependency injection

Each app has a container. A handler, a controller or a service names what it
needs as a parameter or a field; the container builds it once and hands the
same value to everyone.

**Go:** a container such as fx wires constructors you write, and a missing one
is found when the program starts — or when the first request needs it.

**Vuka adds** the constructors (from a struct's fields, see
[Dependency injection](/features/dependency-injection)), and web checks the
whole graph when the app is built: every route's dependencies, before
anything is served.

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

## Providers

| | |
|---|---|
| `web.Provide(ctors…)`, `app.Provide(ctors…)` | constructors — `func(deps…) T` or `func(deps…) (T, error)` — and ready values (anything that isn't a function) |
| `@web.Service` | a struct built from its fields |
| `@web.Controller(prefix)` | the same, with [routes](/web/routes#controllers) |

A service's or controller's fields are injected by type, unexported ones
included; tag a field `inject:"-"` to leave it out. A struct with its own
static `New` is built with that one.

- Every value is a **lazy singleton per app**: built the first time something
  needs it, then shared. Two apps (in tests, say) have their own.
- Dependencies match by **exact type**. To inject an interface, provide a
  constructor returning the interface; there are no named values or groups.
- A constructor's error fails `app.Start` (and `Run`).

## Lifecycle

A provided value with a `Start` or `Stop` method takes part in the app's
life:

```vuka
func (db *DB) Start(ctx context.Context) error { return db.Ping(ctx) }
func (db *DB) Stop(ctx context.Context) error  { return db.Close() }
```

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

```vuka
app := web.New(
	web.Provide(cfg),         // cfg.Start, if any, runs first
	web.OnStart(migrate),
	web.Eager(NewMailer),     // started though no route takes it
)
```

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
func Cache(cfg Config) web.Option {
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

app := web.New(Cache(cfg), web.Dev())
```

The ORM's [`ormweb.Databases`](/orm/databases#web-integration) is such an
option.

`web.Resolve[T](app)` returns the app's `T`, built with its dependencies —
for `main` and [tests](/web/testing).
