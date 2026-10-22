# Routes

<!-- go:context
type DB struct{ *orm.DB }

type Pet struct {
	ID   int
	Name string
}

type NewPet struct{ Name string }

func (s *PetStore) All() ([]Pet, error)        { return nil, nil }
func (s *PetStore) Add(in NewPet) (Pet, error) { return Pet{Name: in.Name}, nil }
func (s *PetStore) Find(id int) (Pet, error)   { return Pet{ID: id}, nil }
func (s *PetStore) Remove(id int) error        { return nil }

func NewStore(db *DB) *PetStore { return NewPetStore(db) }

func OpenDB() *DB { return &DB{} }

func audit(name string, id int) func() { return func() {} }

type Users struct{}

func (u *Users) Create(ctx context.Context, in Signup) error { return nil }

func SignupPage(form Signup, problem string) ui.Node {
	return ui.HTML(`<form method="post"><p>{problem}</p><input name="email" value={form.Email}/></form>`, problem, form.Email)
}

type Board struct{ ui.Live }

func (b *Board) Render() ui.Node { return ui.HTML(`<p>board</p>`) }
func NewBoard() *Board            { return &Board{} }

type Applicants struct{ ui.Live }

func (a *Applicants) Render() ui.Node { return ui.IslandPage(a) }
func NewApplicants() *Applicants        { return &Applicants{} }

var layouts = struct{ Page func(title string) func(ui.Node) ui.Node }{
	Page: func(title string) func(ui.Node) ui.Node { return func(n ui.Node) ui.Node { return n } },
}
-->

A route is a function or a method <span class="vuka-only">with a route
decorator on it</span><span class="go-only">registered on `web.Routes` (or an
app)</span>:

::: vuka
```vuka
@web.Get("/pets")
func ListPets(store *PetStore) Result[[]Pet] { return store.All() }

@web.Post("/api/pets")
func CreatePet(in NewPet, store *PetStore) Result[Pet] { return store.Add(in) }

@web.Handle("OPTIONS", "/api/pets")
func PetOptions(w http.ResponseWriter) { w.Header().Set("Allow", "GET, POST") }
```
:::

::: go
```go
func ListPets(store *PetStore) ([]Pet, error) { return store.All() }

func CreatePet(in NewPet, store *PetStore) (Pet, error) { return store.Add(in) }

func PetOptions(w http.ResponseWriter) { w.Header().Set("Allow", "GET, POST") }

func init() {
	web.Routes.Get("/pets", ListPets)
	web.Routes.Post("/api/pets", CreatePet)
	web.Routes.Handle("OPTIONS", "/api/pets", PetOptions)
}
```
:::

::: vuka
| Decorator | |
|---|---|
| `@web.Get(path)`, `@web.Post(path)`, `@web.Put(path)`, `@web.Patch(path)`, `@web.Delete(path)` | a route for that method |
| `@web.Handle(method, path)` | a route for any method |
| `@web.Template(file [, name])` | render the result with a [view](/web/views); before or after the route decorator |
| `@web.Use(mw…)` | [middleware](/web/middleware) for this route |
| `@web.Controller(prefix)` | a struct becomes a dependency whose route methods are served under `prefix` |
| `@web.Live(path, opts…)` | a stateful component becomes a [live page](/web/live) |
:::

::: go
| Call | |
|---|---|
| `web.Routes.Get(path, fn, opts…)`, `Post`, `Put`, `Patch`, `Delete` | a route for that method |
| `web.Routes.Handle(method, path, fn, opts…)` | a route for any method |
| `web.Template(file [, name])` | an option: render the result with a [view](/web/views) |
| `web.Use(mw…)` | an option: [middleware](/web/middleware) for this route |
| `web.Routes.Controller(prefix, NewX)` | a struct becomes a dependency whose method routes are served under `prefix` |
| `web.Routes.Live(path, NewX, opts…)` | a stateful component becomes a [live page](/web/live) |
:::

::: vuka
Route decorators are Vuka
[declarers](/features/decorators#declaration-decorators): each runs once at
init and records the function with its parameter names, which is how `id`
binds to `{id}`. Other decorators on the same function still wrap it — a call
decorator such as `@audited` runs on every request:
:::
::: go
Reflection can't see parameter names, so a scalar binds the path segment its
position gives — the last scalar the last segment — which is how `id` binds
to `{id}`; the query comes in a struct. What a call decorator does in a
`.vuka` file is code of the handler's own in Go:
:::

::: vuka
```vuka
@web.Get("/pets/{id}")
@audited
func ShowPet(id int, store *PetStore) Result[Pet] { return store.Find(id) }
```
:::

::: go
```go
// audited wraps the handler, as a call decorator does.
func ShowPet(id int, store *PetStore) (Pet, error) {
	defer audit("ShowPet", id)()
	return store.Find(id)
}

func init() {
	web.Routes.Get("/pets/{id}", ShowPet)
}
```
:::

::: vuka
You don't import page packages: the `vuka` command links every package of the
module that declares routes, crons or live pages into main, so a new package
of pages serves as soon as it is saved — no `_ "app/pages/reports"` in
main.vuka. `vuka gen` writes the generated imports as `vuka_link_gen.go` for
plain go tools, and a `//vuka:nolink` line above a package clause opts a
package (or, on main, the whole app) out; see
[the CLI](/tools/cli#build-run-test-vet-install).
:::

::: go
Go links only the packages main imports, so main imports each package
that registers routes, crons or live pages in its `init`, blank if it uses
nothing else: `_ "app/pages/reports"`. A module ejected from `.vuka` files
keeps its generated `vuka_link_gen.go`, which does the same.
:::

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

::: vuka
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
:::

::: go
```go
type Signup struct {
	Email string `form:"email"`
	Name  string `form:"name"`
}

func SignupForm() ui.Node { return SignupPage(Signup{}, "") }

func SignUp(ctx context.Context, in Signup, users *Users) ui.Node {
	if err := users.Create(ctx, in); err != nil {
		return SignupPage(in, err.Error()) // the whole page again
	}
	return web.Redirect("/welcome") // 303
}

func init() {
	web.Routes.Get("/signup", SignupForm)
	web.Routes.Post("/signup", SignUp)
}
```
:::

The browser says it is loading a document with `Sec-Fetch-Dest: document`
(`iframe` for a form in a frame); a browser too old to send it counts when
it asks for `text/html` without htmx's `HX-Request`. A page rendered for a
post isn't live — reconnecting would rebuild it with a GET, losing what the
post rendered — so its JS commands run but its stateful components don't
connect; a form on a [live page](/web/live) belongs in a live event instead.
For signing in, see [Signing in and out](/web/auth#signing-in-and-out).

## Controllers

<span class="vuka-only">`@web.Controller(prefix)` on a struct</span><span class="go-only">`web.Routes.Controller(prefix, NewX)`</span> makes it a dependency built <span class="vuka-only">from its
fields</span><span class="go-only">by its constructor</span>, and its methods <span class="vuka-only">with route decorators</span><span class="go-only">registered after it</span> are served under the prefix.
Under a controller, `"/"` is the prefix itself:

::: vuka
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
:::

::: go
```go
type PetAdmin struct {
	store *PetStore
}

func NewPetAdmin(store *PetStore) *PetAdmin { return &PetAdmin{store: store} }

func (a *PetAdmin) Index() ([]Pet, error) { return a.store.All() }

func (a *PetAdmin) Remove(ctx context.Context, id int) error { return a.store.Remove(id) }

func init() {
	web.Routes.Controller("/admin/pets", NewPetAdmin)
	web.Routes.Get("/", (*PetAdmin).Index)          // GET /admin/pets
	web.Routes.Delete("/{id}", (*PetAdmin).Remove) // DELETE /admin/pets/{id}
}
```
:::

<span class="go-only">Its routes are its method expressions, registered after it.</span>
The controller is a singleton like any dependency: its fields are injected
once, and each request calls the method on it. Pointer and value receivers
both work.

## Services

A service is a struct with a constructor whose parameters are its own
dependencies, registered in `main` with `app.Service(NewX)`: a lazy
singleton, built when something first needs it. Handlers, hooks and other
services take it as a parameter, [by type](/web/dependency-injection#parameters-by-type).

::: vuka
```vuka
type PetStore struct {
	db *DB
	mu sync.Mutex
}

func NewPetStore(db *DB) *PetStore { return &PetStore{db: db} }

func main() {
	app := web.New(web.Provide(OpenDB))
	app.Service(NewPetStore)
	app.Run("")
}
```
:::

::: go
```go
type PetStore struct {
	db *DB
	mu sync.Mutex
}

func NewPetStore(db *DB) *PetStore { return &PetStore{db: db} }

func newApp() *web.App {
	app := web.New(web.Provide(OpenDB))
	app.Service(NewPetStore)
	return app
}
```
:::

A method of a service (or of any provided type) can be a route too: the
receiver comes from the container, with no prefix.

## Plain Go

Everything works from Go, without decorators, with the calls the decorators
are: `@web.Get("/pets/{id}")` on `ShowPet` is
`web.Routes.Get("/pets/{id}", ShowPet)`, after
`vuka.RegisterParams(ShowPet, "id", "store")` records the names reflection
can't see, and an app's methods of the same names register on that app
alone:

```go
func main() {
	app := web.New()
	app.Service(NewStore)
	app.Get("/pets", ListPets)
	app.Get("/pets/{id}", ShowPet, auth.Required())
	app.Live("/board", NewBoard)
	app.Live("/applicants", NewApplicants, ui.Island(ui.Layout(layouts.Page("Applicants"))))
	app.Run("")
}
```

`web.Routes` is the registry every app takes, filled at init: what a
package's `.vuka` decorators lower to, and what a Go package registering its
own routes calls in its `init`. `vuka eject` turns a `.vuka` file's
decorators into these calls (see the [library example](https://github.com/vuka-lang/vuka/tree/main/examples/conformance/library-users)).

Reflection can't see parameter names, so a handler registered from Go binds
its scalar parameters to the path's segments in order, the last scalar
taking the last segment, and takes the query in a struct; structs, request
values and dependencies bind by type as usual.
Options are typed values: web's own, another package's (an auth gate,
`ui.Island`), or any value carrying [attributes](/features/attributes),
which become the route's as attributes written on the handler are.

| | |
|---|---|
| `app.Get`, `Post`, `Put`, `Patch`, `Delete(path, fn, opts…)`, `app.Handle(method, path, fn, opts…)` | a route; `fn` a handler, or an `@ui.Island` struct's constructor (a page backed by its island) |
| `app.Live(path, NewPage, opts…)` | a [live page](/web/live#how-a-page-becomes-live): the constructor builds each page's component, its parameters of provided types injected and the others zero |
| `app.Controller(prefix, NewController, opts…)` | a [controller](#controllers); its routes are its method expressions: `app.Get("/{id}", (*PetAdmin).Show)` |
| `app.Service(NewService)` | a [service](/web/dependency-injection); `app.Provide` takes any constructor or value |
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
