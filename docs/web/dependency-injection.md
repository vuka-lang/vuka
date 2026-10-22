# Dependency injection

Each app has a container. A service is a struct with a constructor whose
parameters are its own dependencies, registered in `main` with
`app.Service(NewX)`; a handler, a hook or another service takes it as a
parameter, by type, and the container builds it once and hands the same
value to everyone.

Web checks the whole graph when the app is built: every route's, hook's and
handler's parameters, before anything is served.

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

type Todo struct{ Text string }

type NewTodo struct {
	Text string `form:"text" validate:"required"`
}

type Todos struct{ rows []Todo }

func (t *Todos) All() []Todo     { return t.rows }
func (t *Todos) Add(text string) { t.rows = append(t.rows, Todo{text}) }
-->

::: vuka
```vuka
type DB struct{ … }

func OpenDB() *DB { … }

type PetStore struct {
	db *DB
}

func NewPetStore(db *DB) *PetStore { return &PetStore{db: db} }

@web.Get("/pets")
func ListPets(store *PetStore) Result[[]Pet] { return store.All() }

func main() {
	app := web.New(web.Provide(OpenDB))   // or app.Provide(OpenDB)
	app.Service(NewPetStore)
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
	web.Routes.Get("/pets", ListPets)
}

func main() {
	app := web.New(web.Provide(OpenDB)) // or app.Provide(OpenDB)
	app.Service(NewPetStore)
	log.Fatal(app.Run(""))
}
```
:::

## Providers

| | |
|---|---|
| `app.Service(NewS)` | a service: the struct its constructor, `func(deps…) *S` or `func(deps…) (*S, error)`, builds |
| `web.Provide(ctors…)`, `app.Provide(ctors…)` | constructors — `func(deps…) T` or `func(deps…) (T, error)` — and ready values (anything that isn't a function) |
| <span class="vuka-only">`@web.Controller(prefix)`, </span>`web.Routes.Controller(prefix, NewC)`, `app.Controller(prefix, NewC)` | a struct built by its constructor, with [routes](/web/routes#controllers) |

A constructor's parameters are injected by type.<span class="vuka-only"> A
`@web.Controller` struct without its own static `New` is built by the one
Vuka writes, which takes its pointer and interface fields.</span>

- Every value is a **lazy singleton per app**: built the first time something
  needs it, then shared. Two apps (in tests, say) have their own.
- Dependencies match by **exact type**. To inject an interface, provide a
  constructor returning the interface; there are no named values or groups.
- A constructor's error fails `app.Start` (and `Run`).

## Parameters, by type

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
| `*auth.Identity` (any `web.ParamFrom` type) | the signed-in visitor |
| a type registered with `app.Service` or `web.Provide` | the container |

<span class="vuka-only">The compiler records a handler's parameter names, so
a scalar binds the path segment or query parameter of its name.</span><span class="go-only">A
handler registered from Go binds its scalars to the path's segments in
order, the last scalar taking the last segment; it takes the query in a
struct, whose fields bind by their `form:` names.</span>

::: vuka
```vuka
@ui.Mount
func (p *Index) mount(ctx context.Context, store *Todos, r *http.Request) error { … }

func (p *Index) Add(ctx context.Context, f NewTodo, store *Todos) error { … }
```
:::

::: go
```go
type Index struct {
	ui.Live
	rows []Todo
}

func (p *Index) LiveSchema(l *ui.LiveSchema) { l.Mount(p.mount) }

func (p *Index) mount(ctx context.Context, store *Todos, r *http.Request) error {
	p.rows = store.All()
	return nil
}

func (p *Index) Add(ctx context.Context, f NewTodo, store *Todos) error {
	store.Add(f.Text)
	p.rows = store.All()
	return nil
}

func (p *Index) Render() ui.Node { return ui.HTML(`<p>todo</p>`) }
```
:::

A live page's exported methods are its event handlers: one taking two
values (`p.Toggle(id, done)`, called from a closure) is no event, and only a
service it takes that nothing provides is reported for it.

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

`app.Build()` resolves everything and returns all problems at once, each at
the file and line of the handler or hook, naming the parameter and its type:

```text
error[E0407]: GET / (live page pages.Index): Index.Add's 2nd parameter, *sql.DB, is not provided
 --> index.vuka.html:41
  = help: register it in main with app.Service(NewX), or web.Provide(NewX)
error[E0405]: GET /items (live page pages.Item): Item.mount's 2nd parameter, chan int, can't come from a hook's call, which gives the context
 --> item.vuka.html:12
error[E0409]: dependency cycle: *main.A -> *main.B -> *main.A
```

A parameter nothing fills ([E0407](/errors/E0407) for a pointer or interface
nothing provides, [E0405](/errors/E0405) for a type a request or an event
can't carry), missing providers, cycles and two providers of one type are
Build errors.
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
