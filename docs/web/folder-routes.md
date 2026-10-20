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

func (s *Store) Employer(id int) (Employer, bool)   { return Employer{ID: id}, true }
func (s *Store) Update(id int, f EmployerForm) bool { return true }
func (s *Store) Delete(id int) bool                 { return true }
func (s *Store) Pending() int                       { return 0 }

func IndexURL(employer int) string { return "" }
func EditURL(employer int) string  { return "" }
-->

An app with many pages spends a lot of lines saying where each one lives:
a `web.Routes` call per page, its path spelt again in every link, its
layout named on each route, its parameters parsed and checked in each
handler. Folder routes put that in the directory tree instead. The
`pages/` folder of the module is the site: each folder is a path segment,
each page type in it a route, a `params.go` makes a folder a typed
parameter, a `layout.go` wraps every page below it, and the `vuka` command
writes the `web.Routes` calls and a typed URL function for each page.

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
| precedence | static, then dynamic, then catch-all — what `net/http`'s mux does |
| typed handlers | a method handler takes a form or JSON struct, bound and validated |
| templates | a page's `.vuka.html` sits beside its `.go`; a `.vuka.html` alone is a page too |
| checked at build time | conflicts are errors with a file and line; `vuka routes` prints the table |

## The tree

```text
main.go                      imports _ "example.com/hr/pages"
pages/
  layout.go                  Layout: the document of every page
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
```

`main` imports `pages` once, blank. `vuka gen` keeps `pages/pages_gen.go`
registering every page of every folder, so a new folder is served on the
next build with nothing else to edit, and each folder gets a
`routes_gen.go` holding its pages' typed URL functions.

The [`folder-routes`](https://github.com/vuka-lang/vuka/tree/main/examples/folder-routes)
example is this tree, with an in-memory store.

## Pages and segments

A page is an exported struct type with a constructor, `New<T>`, taking what
it needs from the [container](/web/dependency-injection). What `*T` is
picks the route:

- a [live component](/web/live) (it embeds `ui.Live` and has
  `Render() ui.Node`): a live page;
- a `ui.Node` (a `Render(ctx, w)` method, or a `.vuka.html` view): a
  stateless page, built and rendered per request.

`Index` serves the folder's own path; any other page adds its name in
kebab-case: `New` is `/new`, `Edit` is `/edit`, `ResetPassword` is
`/reset-password`. Types named `Layout` and `Params` are never pages, and
an exported type without a `New<T>` is a component the pages use. A `New`
page's constructor is `NewNew`: the rule has no exceptions, so nothing is
guessed.

Each request builds the page with its constructor and binds its exported
fields as a live page's are: a provided type injected, text from the path
segment of its name, else the query parameter. A stateless page may have a
`Mount()` or `Mount(ctx)` method, returning nothing or an error, run once
the fields are bound — where it loads what the parameters name:

```go
// pages/employers/employer/index.go
package employer

type Index struct {
	Params
	store    *Store
	employer Employer
}

func NewIndex(s *Store) *Index { return &Index{store: s} }

func (p *Index) Mount() { p.employer, _ = p.store.Employer(p.Employer) }

func (p *Index) Title() string { return p.employer.Name }

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<h2>{p.employer.City}</h2>
<a id="edit" href={EditURL(p.Employer)}>Edit</a>`, p.employer.City, EditURL(p.Employer)).Render(ctx, w)
}
```

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
`{Employer}` — with its `New<T>` and `<T>URL`:

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
called, and then it renders around the next one. An error from `Load`
answers the request as a handler's would: `web.NotFound` is a 404 for every
page below, a `web.Redirect` redirects.

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
	return ui.HTML(`<article>
  <h1>{l.employer.Name}</h1>
  <nav><a href={IndexURL(l.Employer)}>Overview</a> <a href={EditURL(l.Employer)}>Edit</a></nav>
  {l.Children}
</article>`, l.employer.Name, IndexURL(l.Employer), EditURL(l.Employer), l.Children).Render(ctx, w)
}
```

So `/employers/99`, `/employers/99/edit` and every permit of employer 99
are not found, checked in one place. A live page's layouts are part of its
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
	if !p.store.Update(p.Employer, form) {
		return "", web.NotFound
	}
	return web.Redirect(IndexURL(p.Employer)), nil
}

func (p *Edit) Delete(ctx context.Context) error {
	if !p.store.Delete(p.Employer) {
		return web.NotFound
	}
	return nil
}

func (p *Edit) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<form method="post" action={EditURL(p.Employer)}>
  <input name="name" value={p.employer.Name}/> <input name="city" value={p.employer.City}/>
  <button>Save</button>
</form>`, EditURL(p.Employer), p.employer.Name, p.employer.City).Render(ctx, w)
}
```

Each request builds its own receiver with the page's constructor and binds
the path's parameters into it (a bad one is a 404, as for the page), so a
handler reads `p.Employer` and nothing is shared between requests. After
`ctx` a handler takes what [any handler](/web/parameters) takes — a form or
JSON struct, bound and validated (a failing `validate:` tag is 422),
`*http.Request`, … — and returns an `error`, a `ui.Node` and an error, or
a [`web.Redirect`](/web/parameters#redirects) and an error.

The layouts' `Load` belongs to rendering a page, so a handler checks what it
changes itself, as `Post` and `Delete` do above. A handler's `ui.Node`
result is wrapped in the layouts when the response is a whole page (a
`GET`, or the browser asking for a document); a fragment for a script goes
out bare.

## Typed URLs

For every page, `vuka gen` writes `<T>URL` in the page's package, its
parameters those of the path, in order and typed as the `Params` fields:

| page | function |
|---|---|
| `employers.Index` | `employers.IndexURL() string` |
| `employers.New` | `employers.NewURL() string` |
| `employer.Edit` | `employer.EditURL(employer int) string` |
| `permit.Index` | `permit.IndexURL(employer int, permit int) string` |
| `file.Index` | `file.IndexURL(file web.Rest) string` |
| `pages.About` (a template) | `pages.AboutURL() string` |

Each segment is escaped (`url.PathEscape`; a `web.Rest` segment by segment,
its slashes kept; a type with `MarshalText` marshalled). Renaming a folder
or changing a parameter's type makes every stale link a compile error.

The registrations live in `pages_gen.go`, so a layout links anywhere: the
document's navigation calls `employers.IndexURL()`. One link follows Go's
import graph: a parameter folder below another imports it (it embeds its
`Params`), so a page in `employer/` can't call `permit.IndexURL` — that
would be an import cycle. Such a link, from a parameter folder to the ones
below it, is written as a path.

## Precedence and conflicts

At one position a fixed segment beats a parameter, and a parameter beats a
catch-all, as `net/http`'s mux matches: `/employers/new` is the `New` page,
never employer `new`. Everything else that could make two routes meet, or
a page, layout or URL function that can't be generated, is an error when
the routes are generated, at the file and line that causes it:

| | |
|---|---|
| [E0701](/errors/E0701) | two pages on one path |
| [E0702](/errors/E0702) | two routes matching the same paths, such as two parameter folders side by side |
| [E0703](/errors/E0703) | a folder or page below a catch-all folder |
| [E0704](/errors/E0704) | a malformed `Params`: its own fields, their name, the parent's `Params` |
| [E0705](/errors/E0705) | a parameter type a URL can't hold |
| [E0706](/errors/E0706) | a `<T>URL` name, or a template-only page's type, the package already declares |
| [E0707](/errors/E0707) | a folder route and a hand-written route on one path |
| [E0708](/errors/E0708) | a malformed `Layout` or `Load` |
| [E0709](/errors/E0709) | a method handler that can't be bound, or a `Get` method |

Hand-written routes stay: `web.Routes` calls and `app.Get(…)` work beside
folder routes, on paths of their own.

## vuka routes

`vuka routes` prints the table — method, path, page (marked when it is
live or a template), source and layouts:

```text
$ vuka routes
METHOD  PATH                                    PAGE                    FILE                                                 LAYOUTS
GET     /about                                  pages.About (template)  pages/about.vuka.html:1                              pages.Layout
GET     /app/settings                           app.Settings            pages/app/settings.go:16                             pages.Layout
GET     /app/{route...}                         route.Index             pages/app/route/index.go:11                          pages.Layout
GET     /employers                              employers.Index (live)  pages/employers/index.go:14                          pages.Layout
GET     /employers/new                          employers.New           pages/employers/new.go:15                            pages.Layout
POST    /employers/new                          employers.New.Post      pages/employers/new.go:23                            pages.Layout
GET     /employers/{employer}                   employer.Index          pages/employers/employer/index.go:16                 pages.Layout, employer.Layout
GET     /employers/{employer}/edit              employer.Edit           pages/employers/employer/edit.go:16                  pages.Layout, employer.Layout
POST    /employers/{employer}/edit              employer.Edit.Post      pages/employers/employer/edit.go:28                  pages.Layout, employer.Layout
DELETE  /employers/{employer}/edit              employer.Edit.Delete    pages/employers/employer/edit.go:35                  pages.Layout, employer.Layout
GET     /employers/{employer}/permits/{permit}  permit.Index (live)     pages/employers/employer/permits/permit/index.go:15  pages.Layout, employer.Layout
GET     /files/{file...}                        file.Index              pages/files/file/index.go:12                         pages.Layout, file.Layout
```

## What it lowers to

`vuka build`, `run`, `test`, `vet` and the editor see the generated files
without writing them; `vuka gen -inplace` writes them beside the pages
(`pages/pages_gen.go` and a `routes_gen.go` per folder), so a plain
`go build` serves the same routes; `-check` fails when they're stale, and
[`vuka eject`](/tools/cli#eject) keeps them. They are the registration a
person would write. For `pages/layout.go` and `pages/about.vuka.html`:

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

// AboutURL is the path of the page About.
func AboutURL() string {
	return "/about"
}
```

`web.PageRoute()` gives the route the behaviour of a page — fields bound
from the path, a bad path parameter a 404, `Mount`, `Title` — and
`web.LayoutOf[T]()` adds a layout, the first given outermost. A live page
is `web.Routes.Live`, and a method handler is registered with the page's
constructor as its receiver, `web.Receiver(NewT)`. The example's
`pages_gen.go` holds, among others:

```text
web.Routes.Get("/employers/{employer}", employer.NewIndex, web.PageRoute(), web.LayoutOf[Layout](), web.LayoutOf[employer.Layout]())
web.Routes.Post("/employers/{employer}/edit", (*employer.Edit).Post, web.Receiver(employer.NewEdit), web.LayoutOf[Layout](), web.LayoutOf[employer.Layout]())
web.Routes.Live("/employers/{employer}/permits/{permit}", permit.NewIndex, web.PageRoute(), web.LayoutOf[Layout](), web.LayoutOf[employer.Layout]())
web.Routes.Get("/files/{file...}", file.NewIndex, web.PageRoute(), web.LayoutOf[Layout](), web.LayoutOf[file.Layout]())
```

Delete the generated files and write those calls yourself, and the app is
the same.
