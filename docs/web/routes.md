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

You don't import page packages: the `vuka` command links every package of the
module that declares routes, crons or live pages into main, so a new package
of pages serves as soon as it is saved — no `_ "app/pages/reports"` in
main.vuka. `vuka gen` writes the generated imports as `vuka_link_gen.go` for
plain go tools, and a `//vuka:nolink` line above a package clause opts a
package (or, on main, the whole app) out; see
[the CLI](/tools/cli#build-run-test-vet-install).

## Paths

| Pattern | Matches |
|---|---|
| `/pets` | exactly `/pets` — `/pets/` is [redirected](#trailing-slashes) to it |
| `/pets/{id}` | one segment, bound to a parameter named `id` |
| `/files/{path...}` | the rest of the path, bound to `path` |

A segment's value binds to the parameter of the same name; see
[Parameters](/web/parameters). Declaring the same method and path twice is a
Build error naming both handlers.

### Trailing slashes

A request whose path differs from a route's only by a trailing slash is
redirected to the route: with `/orders` declared, `GET /orders/?tab=2` answers
`308` to `/orders?tab=2`, and with `/docs/` declared, `/docs` goes to
`/docs/`. `308` keeps the method and body, so a form posted to the other
form arrives as a POST. It never takes a path the app answers otherwise:

- with both `/pets` and `/pets/` declared, each serves its own;
- a `{path...}` route or `app.Static` tree that answers the path keeps it,
  and `/_vuka/` is never redirected;
- a [frontend](/web/frontend)'s fallback doesn't count: `/settings/` goes to
  your `/settings` route rather than the single-page app's `index.html`, as
  `/settings` itself would; paths no route has stay the frontend's.

Controller prefixes and route patterns with `{name}` segments work the same
(`/pets/7/` → `/pets/7`). The `Location` is relative (`../orders`), so an app
mounted under a prefix with `http.StripPrefix` redirects within it.
`web.ExactPaths()` turns the redirect off.

## Forms that fail

A page answering a POST (or PUT, PATCH, DELETE) depends on who asked. A
form the browser posts itself — no script involved — shows the response as
the new document, so it gets a **whole page**, as a GET would: the
[stylesheet](/web/styling), the runtime for its JS commands and, in
development, the [toolbar](/web/toolbar). A script's request — `fetch`, htmx
— gets the **fragment** the handler rendered, to put where it likes.

So a form that fails re-renders itself, with its errors and what was typed,
instead of redirecting with `?error=`:

```vuka
type Signup struct {
	Email string `form:"email"`
	Name  string `form:"name"`
}

@web.Get("/signup")
func SignupForm() ui.Node { return <SignupPage form={Signup{}} problem="" /> }

@web.Post("/signup")
func SignUp(ctx context.Context, in Signup, users *Users) ui.Node {
	if err := users.Create(ctx, in); err != nil {
		return <SignupPage form={in} problem={err.Error()} />   // the whole page again
	}
	return web.Redirect("/welcome")                             // 303
}
```

The browser says it is loading a document with `Sec-Fetch-Dest: document`
(`iframe` for a form in a frame); a browser too old to send it counts when
it asks for `text/html` without htmx's `HX-Request`. A page rendered for a
post isn't live — reconnecting would rebuild it with a GET, losing what the
post rendered — so its JS commands run but its stateful components don't
connect; a form on a [live page](/web/live) belongs in a live event instead.
For signing in, see [Signing in and out](/web/auth#signing-in-and-out).

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

Everything works from Go, without decorators, with the calls the decorators
are: `@web.Get("/pets/{id}")` on `ShowPet` is
`web.Routes.Get("/pets/{id}", ShowPet, web.Params("id", "store"))`, and an
app's methods of the same names register on that app alone:

```go
func main() {
	app := web.New()
	app.Service(NewStore)
	app.Get("/pets", ListPets)
	app.Get("/pets/{id}", ShowPet, web.Params("id", "store"), auth.Required())
	app.Live("/board", NewBoard)
	app.Live("/applicants", NewApplicants, ui.Island(ui.Layout(layouts.Page("Applicants"))))
	app.Run("")
}
```

`web.Routes` is the registry every app takes, filled at init: what a
package's `.vuka` decorators lower to, and what a Go package registering its
own routes calls in its `init`. `vuka eject` turns a `.vuka` file's
decorators into these calls (see the [library example](https://github.com/vuka-lang/vuka/tree/main/examples/library-users)).

Reflection can't see parameter names, so scalar parameters are named with
`web.Params`, in order (`""` for a parameter that isn't a scalar), the
receiver excluded; structs, request values and dependencies bind as usual.
Options are typed values: web's own, another package's (an auth gate,
`ui.Island`), or any value carrying [attributes](/features/attributes),
which become the route's as attributes written on the handler are.

| | |
|---|---|
| `app.Get`, `Post`, `Put`, `Patch`, `Delete(path, fn, opts…)`, `app.Handle(method, path, fn, opts…)` | a route; `fn` a handler, or an `@ui.Island` struct's constructor (a page backed by its island) |
| `app.Live(path, NewPage, opts…)` | a [live page](/web/live#how-a-page-becomes-live): the constructor builds each page's component, its parameters of provided types injected and the others zero |
| `app.Controller(prefix, NewController, opts…)` | a [controller](#controllers); its routes are its method expressions: `app.Get("/{id}", (*PetAdmin).Show, web.Params("id"))` |
| `app.Service(NewService)` | a [service](/web/dependency-injection); `app.Provide` takes any constructor or value |
| `web.Params(names…)` | the parameters' names, for scalars |
| `web.Template(file [, name])` | the view, as `@web.Template` |
| `web.Use(mw…)`, `web.Wrap(mw…)` | middleware for this route |
| `web.Layout(f)` | a live page's layout; `ui.Island(ui.Layout(f))` for a page backed by its island |
| `app.Static(prefix, fsys)` | serves an `fs.FS` (an `embed.FS` sub-tree, `os.DirFS(…)`): a directory as its `index.html`, else 404 — never a listing — and no dotfile or dot directory (`.env`, `.git/`) but `.well-known/` |

The same methods on `web.Routes` register for every app (`web.Only`
selects by package). From Go, `vuka.File("…")` reads from disk relative to
the working directory; in Vuka a decorator's
[`vuka.File`](/features/decorators#files) literal is embedded in the binary.

Register everything before the app is first built (`Build`, `Handler`,
`Start` or `Run`); a route added after that panics.
