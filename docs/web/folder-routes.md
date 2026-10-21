# Folder routes

<!-- go:context
type Employer struct {
	ID         int
	Name, City string
}

type EmployerForm struct {
	Name string `form:"name" validate:"required"`
	City string `form:"city" validate:"required"`
}

type Store struct{}

func (s *Store) Employers() []Employer               { return nil }
func (s *Store) Employer(id int) (Employer, bool)   { return Employer{ID: id}, true }
func (s *Store) Add(f EmployerForm) Employer        { return Employer{} }
func (s *Store) Update(id int, f EmployerForm)      {}
func (s *Store) Delete(id int)                      {}
func (s *Store) Pending() int                       { return 0 }
func (s *Store) Reloading() bool                    { return false }

type Login struct{}

// What vuka gen writes in pages/urls, as the samples use it.
type pathURL struct{ path string }

func (u pathURL) Path() string { return u.path }

type employersURL struct {
	path string
	New  pathURL
}

func (u employersURL) Path() string                      { return u.path }
func (u employersURL) Employer(employer int) employerURL { return employerURL{} }

type employerURL struct {
	path    string
	Edit    pathURL
	Permits permitsURL
}

func (u employerURL) Path() string { return u.path }

type permitsURL struct{ path string }

func (u permitsURL) Permit(permit int) pathURL { return pathURL{} }

type filesURL struct{ path string }

func (u filesURL) File(file string) pathURL { return pathURL{} }

type apiEmployersURL struct{ path string }

func (u apiEmployersURL) Path() string                  { return u.path }
func (u apiEmployersURL) Employer(employer int) pathURL { return pathURL{} }

type apiURL struct{ Employers apiEmployersURL }

var urls = struct {
	Root      pathURL
	About     pathURL
	Employers employersURL
	Files     filesURL
	Api       apiURL
}{}
-->

An app with many pages spends a lot of lines saying where each one lives:
a `web.Routes` call per page, its path spelt again in every link, its
layout named on each route, its parameters parsed and checked in each
handler. Folder routes put that in the directory tree instead. The
`pages/` folder of the module is the site: each folder is a path segment,
each page type in it a route, a `params.go` makes a folder a typed
parameter, a `layout.go` wraps every page below it, a `guard.go` checks
every request below it, and the `vuka` command writes the `web.Routes`
calls and a typed link to every page. A JSON API lives in the same tree:
a type with `Get` and `Post` methods and no markup is an endpoint.

It is plain Go — the files are ordinary packages, and what `vuka gen`
writes is the registration you would write by hand
([below](#what-it-lowers-to)).

| | |
|---|---|
| static pages | `index.go`, `new.go`, `edit.go`: the folder's path, `/new`, `/edit` |
| dynamic segments | a folder named after the resource with a `params.go`: `employer/` is `{employer}`, typed by its field |
| catch-all | a `web.Rest` parameter: `files/file/` is `/files/{file...}`, slashes included — also a client app's fallback |
| layouts | `layout.go`, inherited by every page below, composed parent → child; its `Load` fetches what it shows |
| several methods per page | `Post`, `Put`, `Patch`, `Delete` methods on the page type answer on its path |
| JSON endpoints | a type with `Get`, `Post`, `Put`, `Patch` or `Delete` methods and no markup: `api/employers/` answers JSON |
| guards | `guard.go`, a check over every page, handler and endpoint below it, rendering nothing |
| middleware | `middleware.go`, HTTP middleware around every route below it — headers, logging, rate limits |
| precedence | static, then dynamic, then catch-all — what `net/http`'s mux does |
| typed handlers | a method handler takes a form or JSON struct, bound and validated |
| templates | a page's `.vuka.html` sits beside its `.go`; a `.vuka.html` alone is a page too |
| checked at build time | conflicts are errors with a file and line; `vuka routes` prints the table |

## The tree

```text
main.go                      imports _ "example.com/hr/pages"
pages/
  layout.go                  Layout: the document of every page
  middleware.go              Middleware: a header and a log line on every request
  about.vuka.html            a page with no Go type — /about
  pages_gen.go               (written by vuka gen) registers every route below
  employers/
    index.go                 Index — /employers
    new.go                   New   — /employers/new (+ Post)
    employer/
      params.go              Params{Employer int} — this folder is {employer}
      layout.go              Layout{Params; Children} with Load
      index.go               Index — /employers/{employer}
      edit.go                Edit  — /employers/{employer}/edit (+ Post, Delete)
      permits/
        permit/
          params.go          Params{employer.Params; Permit int} — {permit}
          index.go           Index — /employers/{employer}/permits/{permit}
  files/
    file/
      params.go              Params{File web.Rest} — {file...}
      layout.go              Layout with Load: an unknown file is not found
      index.go               Index — /files/{file...}
  app/
    settings.go              Settings — /app/settings
    route/
      params.go              Params{Route web.Rest} — {route...}
      index.go               Index: the client app's shell — /app/{route...}
  api/
    guard.go                 Guard: an API key for everything below
    middleware.go            Middleware: the API's rate limit
    employers/
      index.go               Index — GET and POST /api/employers (an endpoint)
      employer/
        params.go            Params{Employer int} — {employer}
        index.go             Index — GET, PUT and DELETE /api/employers/{employer}
```

`main` imports `pages` once, blank. `vuka gen` keeps `pages/pages_gen.go`
registering every page of every folder, so a new folder is served on the
next build with nothing else to edit, and `pages/urls` holds a typed link
to every page ([below](#typed-urls)).

The [`folder-routes`](https://github.com/vuka-lang/vuka/tree/main/examples/folder-routes)
example is this tree, with an in-memory store.

## Pages and segments

A page is an exported struct type whose pointer is a page:

- a [live component](/web/live) (it embeds `ui.Live` and has
  `Render() ui.Node`): a live page;
- a `ui.Node` (a `Render(ctx, w)` method, or a `.vuka.html` view): a
  stateless page, built and rendered per request.

`Index` serves the folder's own path; any other page adds its name in
kebab-case: `New` is `/new`, `Edit` is `/edit`, `ResetPassword` is
`/reset-password`. Types named `Layout`, `Guard` and `Params` are never
pages.

A page with nothing to inject needs no constructor: `vuka gen` writes
`func NewSettings() *Settings { return &Settings{} }` for it.

```go
// pages/app/settings.go
package app

type Settings struct{}

func (p *Settings) Title() string { return "App settings" }

func (p *Settings) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<h1>Settings</h1>`).Render(ctx, w)
}
```

A page that needs something from the [container](/web/dependency-injection)
declares its constructor, `New<T>`, taking it; a constructor written by
hand turns the generated one off. A `New` page's constructor is `NewNew`:
the rule has no exceptions, so nothing is guessed.

Because an exported page-shaped type in `pages/` is a route by itself, a
component the pages share belongs in another package (`components/`) or
unexported. One that becomes a route by accident shows in `vuka routes`,
and lands on a path another page holds as [E0701](/errors/E0701). Types
registered another way — a route decorator, a `web.Routes` call — are
left as they are.

Each request builds the page with its constructor and binds its exported
fields as a live page's are: a provided type injected, text from the path
segment of its name, else the query parameter. A stateless page may have a
`Mount()` or `Mount(ctx)` method, returning nothing or an error, run once
the fields are bound and the [guards](#guards) and [layouts](#layouts)
above it have loaded — where it loads what the parameters name:

```go
// pages/employers/employer/edit.go
package employer

type Edit struct {
	Params
	store    *Store
	employer Employer
}

func NewEdit(s *Store) *Edit { return &Edit{store: s} }

func (p *Edit) Mount() { p.employer, _ = p.store.Employer(p.Employer) }

func (p *Edit) Title() string { return "Edit " + p.employer.Name }
```

A live page's `Mount(ctx) error` runs as its session renders, the same
method by name; see [Hooks](/ui/stateful#hooks).

### Titles

A page with `Title() string` names the document: before the layouts render
the context carries it, and a layout reads it with `ui.PageTitle(ctx)`. A
stateless page's `Title` runs after its `Mount`. A live page's runs once
its fields are bound and before its `Mount`, so a live page titles itself
from its parameters (`"Permit 2"`) or what its constructor set.

### Pages that are only a template

A `.vuka.html` file whose type the package doesn't declare is a page by
itself: `about.vuka.html` is a stateless `About` at `/about`,
`terms-of-service.vuka.html` a `TermsOfService` at `/terms-of-service`, an
`index.vuka.html` the folder's `Index`. `vuka gen` declares the type for it
— embedding the folder's `Params` when it has one, so the markup reads
`{Employer}` — and its `New<T>`, and links to it like any page
(`urls.About.Path()`):

```html
<!-- pages/about.vuka.html -->
package pages

<section id="about">
  <h1>About</h1>
  <p>Employers, their permits and the files that go with them.</p>
</section>
```

A file holding only `func` templates, partials for other pages, is not a
page. A page that needs a title, data or a method handler declares its
type, and is then an ordinary page with its template beside it.

## Parameter folders

A folder whose package declares `type Params struct` is a parameter, named
after the folder: `employer/` is `{employer}`. `Params` has one field of its
own, named like the folder (`Employer`, case aside), and its type is what
the segment accepts:

| field type | the segment |
|---|---|
| `int`, `int64`, `uint`, … | digits |
| `string` | any one segment |
| a type with `MarshalText` and `UnmarshalText` | what it parses |
| `web.Rest` | the rest of the path, slashes included: `{name...}` |

`validate:` tags apply as they do to any binding. A path segment that
doesn't parse or validate is **404**, not 400: `/employers/x` names no
employer, the same as a page that isn't there. (A query parameter that
doesn't parse is still 400.)

```go
// pages/employers/employer/params.go
package employer

type Params struct {
	Employer int
}
```

A parameter folder below another embeds the nearest one's `Params`, so its
pages get every parameter of their path:

```text
// pages/employers/employer/permits/permit/params.go
package permit

import "example.com/hr/pages/employers/employer"

type Params struct {
	employer.Params
	Permit int
}
```

Pages and layouts get the parameters by embedding their package's `Params`;
promoted fields bind by name like direct ones, at any depth of embedding,
so a permit page reads `p.Employer` and `p.Permit`.

### Catch-all folders

A `web.Rest` field takes the rest of the path. Such a folder is the end of
its branch: no subfolders, and no page but its `Index`. In
`pages/files/file/params.go`:

<!-- go:fragment -->
```go
type Params struct {
	File web.Rest
}
```

`/files/handbook/leave.txt` binds `File` to `handbook/leave.txt`.

## A client app under one path

A client-side app — one that routes in the browser — needs the same
document for every URL it owns. A catch-all folder gives it that: its
`Index` renders the app's shell (an [island](/web/islands), or the
frontend's entry), so `/app/`, `/app/employers/7` and anything below
`/app/` answer with it, and the app reads `location.pathname`. Pages beside
it still win: a fixed segment beats the catch-all, so `/app/settings` stays
a server page.

```text
pages/app/
  settings.go            Settings — /app/settings, a server page
  route/
    params.go            Params{Route web.Rest} — /app/{route...}
    index.vuka.html      Index: the client app's shell
```

The shell needs no Go: here it is a [template-only page](#pages-that-are-only-a-template),
inside the layouts above it like any other:

```html
<!-- pages/app/route/index.vuka.html: Index, /app/{route...} -->
package route

<div id="app"></div>
<script type="module" src="/assets/app.js"></script>
```

Hand-written JSON routes under the same prefix (`/app/api/…`) win over the
catch-all the same way.

## Layouts

A folder whose package declares `type Layout struct` wraps every page in it
and below. Layouts compose from the root down: the outermost (nearest
`pages/`) is the document, the next one renders inside its `Children`, and
the page inside the innermost.

```go
// pages/layout.go
package pages

type Layout struct {
	Children ui.Node
	pending  int
}

func (l *Layout) Load(ctx context.Context, s *Store) error {
	l.pending = s.Pending()
	return nil
}

func (l Layout) Render(ctx context.Context, w io.Writer) error {
	title := ui.PageTitle(ctx) + " - HR"
	return ui.HTML(`<html lang="en">
<head><title>{title}</title></head>
<body>
  <nav>Permits waiting: {l.pending}</nav>
  <main>{l.Children}</main>
</body>
</html>`, title, l.pending, l.Children).Render(ctx, w)
}
```

A layout is a stateless component with a `Children ui.Node` field. It may
embed its package's `Params`, and it may have a `Load` method, its
parameters after `ctx` coming from the container. On each request, from
the outermost in, a layout is made fresh, its path parameters bound, `Load`
called, and then it renders around the next one. Every `Load` runs before
the page's `Mount`, so `Mount` sees only what the layouts let through. An
error from `Load` answers the request as a handler's would: `web.NotFound`
is a 404, `web.Forbidden` a 403, a `web.Redirect` redirects. `Load` guards
everything beneath its folder — every page and every page's `Post`, `Put`,
`Patch` and `Delete` — so it is the natural place for a check that holds
for a whole folder and needs what the layout shows anyway: that the record
exists. A check with nothing to show is a [guard](#guards).

```go
// pages/employers/employer/layout.go
package employer

type Layout struct {
	Params
	Children ui.Node
	employer Employer
}

func (l *Layout) Load(ctx context.Context, s *Store) error {
	e, ok := s.Employer(l.Employer)
	if !ok {
		return web.NotFound
	}
	l.employer = e
	return nil
}

func (l Layout) Render(ctx context.Context, w io.Writer) error {
	at := urls.Employers.Employer(l.Employer)
	overview, edit := at.Path(), at.Edit.Path()
	return ui.HTML(`<article>
  <h1>{l.employer.Name}</h1>
  <nav><a href={overview}>Overview</a> <a href={edit}>Edit</a></nav>
  {l.Children}
</article>`, l.employer.Name, overview, edit, l.Children).Render(ctx, w)
}
```

So `/employers/99`, `/employers/99/edit`, a `POST` to it and every permit
of employer 99 are not found, checked in one place. A live page's layouts are part of its
page: they render around it on the GET and again when its socket joins
(`Load` runs for each), and the socket's patches re-render the page alone.
A route with folder layouts takes no `web.Layout` and isn't wrapped in the
app's `web.LiveLayout`.

## Several methods on one page

A page type answers other methods on its own path with methods named after
them: `Post`, `Put`, `Patch` and `Delete`. (`GET` is the page itself.)

```go
// pages/employers/employer/edit.go
package employer

type Edit struct {
	Params
	store    *Store
	employer Employer
}

func NewEdit(s *Store) *Edit { return &Edit{store: s} }

func (p *Edit) Mount() { p.employer, _ = p.store.Employer(p.Employer) }

func (p *Edit) Title() string { return "Edit " + p.employer.Name }

func (p *Edit) Post(ctx context.Context, form EmployerForm) (web.Redirect, error) {
	p.store.Update(p.Employer, form)
	return web.Redirect(urls.Employers.Employer(p.Employer).Path()), nil
}

func (p *Edit) Delete(ctx context.Context) error {
	p.store.Delete(p.Employer)
	return nil
}

func (p *Edit) Render(ctx context.Context, w io.Writer) error {
	action := urls.Employers.Employer(p.Employer).Edit.Path()
	return ui.HTML(`<form method="post" action={action}>
  <input name="name" value={p.employer.Name}/> <input name="city" value={p.employer.City}/>
  <button>Save</button>
</form>`, action, p.employer.Name, p.employer.City).Render(ctx, w)
}
```

Each request builds its own receiver with the page's constructor and binds
the path's parameters into it (a bad one is a 404, as for the page), so a
handler reads `p.Employer` and nothing is shared between requests. After
`ctx` a handler takes what [any handler](/web/parameters) takes — a form or
JSON struct, bound and validated (a failing `validate:` tag is 422),
`*http.Request`, … — and returns an `error`, a `ui.Node` and an error, or
a [`web.Redirect`](/web/parameters#redirects) and an error.

The layouts above the page run their `Load` before a handler as before the
page, so `Post` and `Delete` above never see an unknown employer: the
employer's `Layout` has answered 404 already. A handler's `ui.Node` result
is wrapped in the layouts when the response is a whole page (a `GET`, or
the browser asking for a document); a fragment for a script goes out bare.

## Endpoints

A JSON API is folders of endpoints. An endpoint is an exported struct type
whose pointer isn't a page — no `Render`, no view, no `ui.Live` — with
methods named `Get`, `Post`, `Put`, `Patch` or `Delete`. Each method
answers its HTTP method on the type's path, by the page rules: `Index` is
the folder's path, any other name adds its kebab-case segment. Put them
under `pages/api/` and the API sits beside the pages, with typed links,
`Params` and guards like theirs.

```text
// pages/api/employers/index.go
package employers

type Index struct {
	store *Store
}

func NewIndex(s *Store) *Index { return &Index{store: s} }

// ListQuery is GET's query string: /api/employers?city=Arusha&limit=10.
type ListQuery struct {
	City  string `form:"city"`
	Limit int    `form:"limit" validate:"omitempty,max=100"`
}

func (e *Index) Get(ctx context.Context, q ListQuery) ([]Employer, error) {
	out := []Employer{}
	for _, em := range e.store.Employers() {
		if q.City == "" || strings.EqualFold(em.City, q.City) {
			out = append(out, em)
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (e *Index) Post(ctx context.Context, in EmployerForm) (Employer, error) {
	return e.store.Add(in), nil
}
```

A `GET`'s struct parameter is its query string; a `POST`, `PUT` or
`PATCH`'s is the body, JSON or a form, bound and validated (a failing
`validate:` tag is 422). An endpoint in a parameter folder embeds its
`Params`, bound from the path per request — a segment that doesn't parse
is 404 before the method runs:

```go
// pages/api/employers/employer/index.go
package employer

type Index struct {
	Params
	store *Store
}

func NewIndex(s *Store) *Index { return &Index{store: s} }

func (e *Index) Get(ctx context.Context) (Employer, error) {
	em, ok := e.store.Employer(e.Employer)
	if !ok {
		return Employer{}, web.NotFound
	}
	return em, nil
}

func (e *Index) Put(ctx context.Context, in EmployerForm) (Employer, error) {
	if _, ok := e.store.Employer(e.Employer); !ok {
		return Employer{}, web.NotFound
	}
	e.store.Update(e.Employer, in)
	return e.Get(ctx)
}

func (e *Index) Delete(ctx context.Context) error {
	if _, ok := e.store.Employer(e.Employer); !ok {
		return web.NotFound
	}
	e.store.Delete(e.Employer)
	return nil
}
```

A method takes what [any handler](/web/parameters) takes after `ctx`, and
what it returns is the response:

| the method returns | the response |
|---|---|
| a value and a nil error | 200, the value as JSON |
| a nil error alone | 204, no body |
| an error | its status, as JSON: `web.NotFound` 404, `web.Forbidden` 403, a failed `validate:` 422, any other `web.Error` its own, else 500 |
| a `ui.Node` | the markup, as a page's handler answers |
| a `web.Redirect` | 302 on `GET`, 303 on the others |

A page and an endpoint may share a path when their methods differ; two
types answering one method on one path are [E0701](/errors/E0701). An
endpoint with nothing to inject needs no constructor, as a page doesn't.
`pages/urls` links to endpoints as to pages,
`urls.Api.Employers.Employer(7).Path()`, and `vuka routes` marks them
`(endpoint)`.

## Guards

A folder whose package declares `type Guard struct` checks every request to
a page, handler or endpoint in it and below, and renders nothing. `Guard`
may embed its package's `Params`, and has a `Load(ctx, deps…) error`, its
parameters after `ctx` coming from the container — an API key, a
permission, a tenant that must exist:

```go
// pages/api/guard.go
package api

// Guard: the read key may read, the write key may also change.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	r := web.Request(ctx)
	switch r.Header.Get("X-API-Key") {
	case web.Env("API_WRITE_KEY", "write-key"):
		return nil
	case web.Env("API_READ_KEY", "read-key"):
		if r.Method == http.MethodGet {
			return nil
		}
		return web.Forbidden
	}
	return web.Unauthorized
}
```

Per request a guard is made fresh, its path parameters bound and its
`Load` called; an error answers the request as a layout's does
(`web.NotFound` 404, `web.Forbidden` 403, any other `web.Error` its status,
a `web.Redirect` 302 on `GET` and 303 otherwise) — an HTML error page for a
page, JSON for an endpoint. It runs before anything of the request's own:
before a stateless page's `Mount`, before a live page renders (on the
`GET` and again when its socket joins, a refused join answering a live
error with the status), and before a handler's or endpoint's body is read, so `POST
/api/employers` with the read key is 403 whatever it sends.

Guards and layouts load from the root down, and within one folder the
`Guard` before the `Layout`; the first error stops the request. A layout's
`Load` is for what it shows; a guard is for a check alone, and fits where
nothing renders — an API, or pages that keep their own document.

## Middleware

A folder whose package declares `Middleware` wraps every page, method
handler and endpoint in it and below in plain `net/http` middleware —
where guards and layouts speak the request's language (parameters, the
container, a typed error), middleware speaks HTTP's: response headers, a
log line, a rate limit, a cached or short-circuited response. Two forms:

```go
// pages/middleware.go
package pages

// Middleware stamps and logs every request of the site.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Served-By", "hr")
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
```

The plain form is `func(next http.Handler) http.Handler`. The constructor
form, `func Middleware(deps…) web.Middleware`, takes what it needs from
the [container](/web/dependency-injection) once per route, when the app
builds — the place for configuration and for the state the middleware
keeps, which is that route's own (the limiter below counts each API route
by itself):

```go
// pages/api/middleware.go
package api

// Middleware holds the API while the store reloads.
func Middleware(s *Store) web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Reloading() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "reloading", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Middleware is the outermost ring. Per request, from the root folder down:

```text
middleware (root → folder) → guards' and layouts' Load → bind → Mount → handler
```

and the response passes back out through the middleware. A route's own
middleware — `@web.Use` on a decorated route, or a `web.Use` attribute —
runs inside its folders'; [auth](/web/auth)'s gates run outside them.
Folder middleware wraps the whole route, so it also sees the 404 a path
parameter that doesn't parse answers. Not calling `next` short-circuits the request: the middleware
writes the response itself — a maintenance page, an IP block, a rate
limit's 429, a cache hit — and nothing below it runs, the guards
included, as the api middleware above does while the store reloads.

Middleware wraps folder routes only: hand-written routes, static files
and the app's 404 pass it by (give the whole app middleware with
`web.New(web.Use(mw))`). On a live page it wraps the `GET` and the
socket's join request; the socket's events don't go back through it —
a check that must hold the whole session is a [guard](#guards), which
re-runs on the join. One `Middleware` per package, like `Layout` and
`Guard`; the file name `middleware.go` is convention. A wrong signature
is [E0710](/errors/E0710).

**A middleware or a guard?** A guard decides *whether* — it has the path's
parameters and the container, returns a typed error, and answers in the
route's own shape (an HTML error page, JSON for an endpoint). Middleware
decides *around* — it sees the raw request and response, runs before
everything and after everything, and what it writes is its own. An API
key is a guard; the rate limit in front of it is middleware.

### Attributes on a page type

A folder route has no registration to hang an option on — options a page
needs go on its type, with `vuka.RegisterType`, as a live page's do. From
1.3 they reach stateless pages, their method handlers and endpoints too,
so a login page under `pages/` opts out of [auth](/web/auth)'s sign-in
the usual way:

<!-- go:fragment -->
```go
vuka.RegisterType[Login](auth.Public())
```

An attribute only a `web.WrapRoutes` function gives a meaning to (a
`web.RouteAttr`, what auth's gates are) reaches that function with the
page's route, stateless or live.

## Typed URLs

`vuka gen` writes one more package, `pages/urls`, with a link to every
page, shaped like the tree: a folder is a value, a fixed child folder or a
page its field, a parameter folder a method taking the parameter's type,
and `Path()` the URL.

<!-- go:fragment -->
```go
urls.Root.Path()                                    // "/": the pages/ folder's Index
urls.About.Path()                                   // "/about"
urls.Employers.Path()                               // "/employers"
urls.Employers.New.Path()                           // "/employers/new"
urls.Employers.Employer(7).Path()                   // "/employers/7"
urls.Employers.Employer(7).Edit.Path()              // "/employers/7/edit"
urls.Employers.Employer(7).Permits.Permit(3).Path() // "/employers/7/permits/3"
urls.Files.File("reports/2026/q3.pdf").Path()       // "/files/reports/2026/q3.pdf"
urls.Api.Employers.Employer(7).Path()               // "/api/employers/7": an endpoint
```

- The root folder is `urls.Root`, and each of its children is also a
  variable of the package, so links start at `urls.Employers`, not
  `urls.Root.Employers`.
- A folder's fields are its fixed child folders (CamelCase of the folder's
  name: `job-permits/` is `JobPermits`) and its pages other than `Index`
  (by type name: `New`, `Edit`, `TermsOfService`). Its parameter folders
  are methods named the same way, taking the `Params` field's type:
  `Employer(employer int)`, `File(file web.Rest)`.
- `Path()` exists only where there is a page or an endpoint: on its field,
  and on a folder with an `Index`. `urls.Employers.Employer(7).Permits` has no
  `Path()` — `permits/` holds no `Index` — so a link to a URL that serves
  nothing doesn't compile. Method handlers answer on their page's path:
  the form posts to `urls.Employers.Employer(7).Edit.Path()`.
- Each segment is escaped: an integer written out, a string or a type's
  `MarshalText` output through `url.PathEscape`, a `web.Rest` segment by
  segment, its slashes kept.

`urls` imports no page package, so any page links to any other — a parent
to its children, a layout to the pages it wraps, a page in `employer/` to
the permits below it. Renaming a folder, removing a page or changing a
parameter's type makes every stale link a compile error.

`pages/urls` belongs to the generator: it is never a page folder, and
`vuka gen` creates it, empty, when it is missing. Names in a folder's value
can't meet — a folder `new/` beside a page `New`, a child named `Path` in a
folder with an `Index`, a child named `Root` at the root are each
[E0706](/errors/E0706) — and a parameter folder named `path` would be a
method `Path(…)` beside `Path()`: name such a folder for what it holds, as
`files/file/` does.

Query parameters are not part of the chain: add them to the path, as in
`urls.Employers.Path() + "?page=2"`.

## Precedence and conflicts

At one position a fixed segment beats a parameter, and a parameter beats a
catch-all, as `net/http`'s mux matches: `/employers/new` is the `New` page,
never employer `new`. Everything else that could make two routes meet, or
a page, layout or URL function that can't be generated, is an error when
the routes are generated, at the file and line that causes it:

| | |
|---|---|
| [E0701](/errors/E0701) | two pages or endpoints answering one method on one path |
| [E0702](/errors/E0702) | two routes matching the same paths, such as two parameter folders side by side |
| [E0703](/errors/E0703) | a folder or page below a catch-all folder |
| [E0704](/errors/E0704) | a malformed `Params`: its own fields, their name, the parent's `Params` |
| [E0705](/errors/E0705) | a parameter type a URL can't hold, or one declared in a page package |
| [E0706](/errors/E0706) | a name vuka needs that is taken: a template-only page's type, or a child folder and a page named alike |
| [E0707](/errors/E0707) | a folder route and a hand-written route on one path |
| [E0708](/errors/E0708) | a malformed `Layout`, `Guard` or `Load` |
| [E0709](/errors/E0709) | a method handler that can't be bound, or a `Get` method on a page |
| [E0710](/errors/E0710) | a malformed `Middleware` |

Hand-written routes stay: `web.Routes` calls and `app.Get(…)` work beside
folder routes, on paths of their own.

## vuka routes

`vuka routes` prints the table — method, path, page or endpoint (marked
when it is live, a template or an endpoint), source, middleware, guards
and layouts:

```text
$ vuka routes
METHOD  PATH                                    PAGE                              FILE                                                 MIDDLEWARE                        GUARDS     LAYOUTS
GET     /about                                  pages.About (template)            pages/about.vuka.html:1                              pages.Middleware                             pages.Layout
GET     /api/employers                          employers.Index.Get (endpoint)    pages/api/employers/index.go:24                      pages.Middleware, api.Middleware  api.Guard  pages.Layout
POST    /api/employers                          employers.Index.Post (endpoint)   pages/api/employers/index.go:37                      pages.Middleware, api.Middleware  api.Guard  pages.Layout
GET     /api/employers/{employer}               employer.Index.Get (endpoint)     pages/api/employers/employer/index.go:18             pages.Middleware, api.Middleware  api.Guard  pages.Layout
PUT     /api/employers/{employer}               employer.Index.Put (endpoint)     pages/api/employers/employer/index.go:26             pages.Middleware, api.Middleware  api.Guard  pages.Layout
DELETE  /api/employers/{employer}               employer.Index.Delete (endpoint)  pages/api/employers/employer/index.go:34             pages.Middleware, api.Middleware  api.Guard  pages.Layout
GET     /app/settings                           app.Settings                      pages/app/settings.go:23                             pages.Middleware                             pages.Layout
GET     /app/{route...}                         route.Index                       pages/app/route/index.go:11                          pages.Middleware                             pages.Layout
GET     /employers                              employers.Index (live)            pages/employers/index.go:14                          pages.Middleware                             pages.Layout
GET     /employers/new                          employers.New                     pages/employers/new.go:15                            pages.Middleware                             pages.Layout
POST    /employers/new                          employers.New.Post                pages/employers/new.go:23                            pages.Middleware                             pages.Layout
GET     /employers/{employer}                   employer.Index                    pages/employers/employer/index.go:16                 pages.Middleware                             pages.Layout, employer.Layout
GET     /employers/{employer}/edit              employer.Edit                     pages/employers/employer/edit.go:17                  pages.Middleware                             pages.Layout, employer.Layout
POST    /employers/{employer}/edit              employer.Edit.Post                pages/employers/employer/edit.go:29                  pages.Middleware                             pages.Layout, employer.Layout
DELETE  /employers/{employer}/edit              employer.Edit.Delete              pages/employers/employer/edit.go:34                  pages.Middleware                             pages.Layout, employer.Layout
GET     /employers/{employer}/permits/{permit}  permit.Index (live)               pages/employers/employer/permits/permit/index.go:15  pages.Middleware                             pages.Layout, employer.Layout
GET     /files/{file...}                        file.Index                        pages/files/file/index.go:12                         pages.Middleware                             pages.Layout, file.Layout
```

## What it lowers to

`vuka build`, `run`, `test`, `vet` and the editor see the generated files
without writing them; `vuka gen -inplace` writes them beside the pages
(`pages/pages_gen.go`, `pages/urls/urls_gen.go`, and a `routes_gen.go`
in a folder with template-only pages or pages without a constructor), so a plain
`go build` serves the same routes; `-check` fails when they're stale, and
[`vuka eject`](/tools/cli#eject) keeps them. They are the registration a
person would write. For `pages/layout.go`, `pages/about.vuka.html` and
`pages/app/settings.go`:

```go
// pages/pages_gen.go
package pages

func init() {
	web.Routes.Get("/about", NewAbout, web.PageRoute(), web.LayoutOf[Layout]())
}

// pages/routes_gen.go

// About is the page about.vuka.html renders.
type About struct{}

// NewAbout builds the page About.
func NewAbout() *About { return &About{} }

// pages/app/routes_gen.go

// NewSettings is the constructor of Settings, for its routes.
func NewSettings() *Settings { return &Settings{} }
```

`web.PageRoute()` gives the route the behaviour of a page — fields bound
from the path, a bad path parameter a 404, `Mount`, `Title` — and
`web.LayoutOf[T]()` adds a layout, the first given outermost;
`web.GuardOf[T]()` adds a guard, loading with the layouts in the order the
options are given; `web.UseOf(pkg.Middleware)` wraps the route in each
folder's middleware, the root's outermost. A live page is `web.Routes.Live`, and a method handler
or an endpoint's method is registered with the type's constructor as its
receiver, `web.Receiver(NewT)`. A page or endpoint without a `New<T>` gets
one in its folder's `routes_gen.go`, as a template-only page does. Packages
that share a name are imported by their path (`employers_employer`,
`api_employers_employer`). The example's `pages_gen.go` holds, among
others:

```text
web.Routes.Get("/employers/{employer}", employers_employer.NewIndex, web.PageRoute(), web.UseOf(Middleware), web.LayoutOf[Layout](), web.LayoutOf[employers_employer.Layout]())
web.Routes.Post("/employers/{employer}/edit", (*employers_employer.Edit).Post, web.Receiver(employers_employer.NewEdit), web.UseOf(Middleware), web.LayoutOf[Layout](), web.LayoutOf[employers_employer.Layout]())
web.Routes.Live("/employers/{employer}/permits/{permit}", permit.NewIndex, web.PageRoute(), web.UseOf(Middleware), web.LayoutOf[Layout](), web.LayoutOf[employers_employer.Layout]())
web.Routes.Get("/files/{file...}", file.NewIndex, web.PageRoute(), web.UseOf(Middleware), web.LayoutOf[Layout](), web.LayoutOf[file.Layout]())
web.Routes.Get("/api/employers", (*api_employers.Index).Get, web.Receiver(api_employers.NewIndex), web.UseOf(Middleware), web.UseOf(api.Middleware), web.LayoutOf[Layout](), web.GuardOf[api.Guard]())
web.Routes.Delete("/api/employers/{employer}", (*api_employers_employer.Index).Delete, web.Receiver(api_employers_employer.NewIndex), web.UseOf(Middleware), web.UseOf(api.Middleware), web.LayoutOf[Layout](), web.GuardOf[api.Guard]())
web.Routes.Get("/app/settings", app.NewSettings, web.PageRoute(), web.UseOf(Middleware), web.LayoutOf[Layout]())
```

A layout without a `Load` adds nothing to an endpoint's request: it only
wraps markup.

Delete the generated files and write those calls yourself, and the app is
the same.
