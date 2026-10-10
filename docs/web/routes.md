# Routes

A route is a function or a method with a route decorator on it:

```vuka
@web.Get("/pets")
func ListPets(store *PetStore) Result[[]Pet] { return store.All() }

@web.Post("/api/pets")
func CreatePet(in NewPet, store *PetStore) Result[Pet] { return store.Add(in) }

@web.Handle("OPTIONS", "/api/pets")
func PetOptions(w http.ResponseWriter) { w.Header().Set("Allow", "GET, POST") }
```

| Decorator | |
|---|---|
| `@web.Get(path)`, `@web.Post(path)`, `@web.Put(path)`, `@web.Patch(path)`, `@web.Delete(path)` | a route for that method |
| `@web.Handle(method, path)` | a route for any method |
| `@web.Template(file [, name])` | render the result with a [view](/web/views); before or after the route decorator |
| `@web.Use(mw…)` | [middleware](/web/middleware) for this route |
| `@web.Service` | a struct becomes a [dependency](/web/dependency-injection) |
| `@web.Controller(prefix)` | a struct becomes a dependency whose route methods are served under `prefix` |
| `@web.Live(path, opts…)` | a stateful component becomes a [live page](/web/live) |

Route decorators are Vuka
[declarers](/features/decorators#declaration-decorators): each runs once at
init and records the function with its parameter names, which is how `id`
binds to `{id}`. Other decorators on the same function still wrap it — a call
decorator such as `@audited` runs on every request:

```vuka
@web.Get("/pets/{id}")
@audited
func ShowPet(id int, store *PetStore) Result[Pet] { return store.Find(id) }
```

## Paths

| Pattern | Matches |
|---|---|
| `/pets` | exactly `/pets` — not `/pets/` |
| `/pets/{id}` | one segment, bound to a parameter named `id` |
| `/files/{path...}` | the rest of the path, bound to `path` |

A segment's value binds to the parameter of the same name; see
[Parameters](/web/parameters). Declaring the same method and path twice is a
Build error naming both handlers.

## Controllers

`@web.Controller(prefix)` on a struct makes it a dependency built from its
fields, and its methods with route decorators are served under the prefix.
Under a controller, `"/"` is the prefix itself:

```vuka
@web.Controller("/admin/pets")
type PetAdmin struct {
	store *PetStore
}

@web.Get("/")                                      // GET /admin/pets
func (a *PetAdmin) Index() Result[[]Pet] { return a.store.All() }

@web.Delete("/{id}")                               // DELETE /admin/pets/{id}
func (a *PetAdmin) Remove(ctx context.Context, id int) error { return a.store.Remove(id) }
```

The controller is a singleton like any dependency: its fields are injected
once, and each request calls the method on it. Pointer and value receivers
both work.

## Services

`@web.Service` on a struct makes it a dependency — a lazy singleton built from
its fields, each injected by type (Vuka's
[generated constructor](/features/dependency-injection)):

```vuka
@web.Service
type PetStore struct {
	db  *DB
	mu  sync.Mutex `inject:"-"`   // not a dependency
}
```

A method of a service (or of any provided type) can be a route too: the
receiver comes from the container, with no prefix.

## Plain Go

Everything works from Go, without decorators. Reflection can't see parameter
names, so scalar parameters are named with `web.Params`, in order (`""` for a
parameter that isn't a scalar); structs, request values and dependencies bind
as usual:

```go
app := web.New(web.Provide(NewStore))
app.Get("/pets/{id}", func(s *Store, id int) (Pet, error) { return s.Find(id) }, web.Params("s", "id"))
app.Post("/pets", func(in NewPet, s *Store) (Pet, error) { return s.Add(in) })
app.Get("/page", page, web.View(vuka.File("views/page.html")), web.Wrap(requireToken))
```

| | |
|---|---|
| `app.Get`, `Post`, `Put`, `Patch`, `Delete(path, fn, opts…)`, `app.Handle(method, path, fn, opts…)` | a route |
| `web.Params(names…)` | the parameters' names, for scalars |
| `web.View(file [, name])` | the view, as `@web.Template` |
| `web.Wrap(mw…)` | middleware for this route, as `@web.Use` |
| `app.Live(path, &Component{…})` | a [live page](/web/live#how-a-page-becomes-live) |
| `app.Static(prefix, fsys)` | serves an `fs.FS` (an `embed.FS` sub-tree, `os.DirFS(…)`) |

From Go, `vuka.File("…")` reads from disk relative to the working directory;
in Vuka a decorator's [`vuka.File`](/features/decorators#files) literal is
embedded in the binary.

Register everything before the app is first built (`Build`, `Handler`,
`Start` or `Run`); a route added after that panics.
