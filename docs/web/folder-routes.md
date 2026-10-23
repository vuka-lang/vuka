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

func NewStore() *Store                              { return &Store{} }
func (s *Store) Employer(id int) (Employer, bool)   { return Employer{ID: id}, true }
func (s *Store) Reloading() bool                    { return false }

type Login struct{}

// What vuka writes in pages/urls, as the samples use it.
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

// What vuka writes in pages/route.go.
var pages = struct{ Routes func() web.Option }{func() web.Option { return web.OptionFunc(func(*web.App) {}) }}
-->

An app with many pages spends a lot of lines saying where each one lives:
a registration per page, its path spelt again in every link, its layout
named on each route, its parameters parsed and checked in each handler.
Folder routes put that in the directory tree instead. A `pages/` directory is discovered at the module root and directly beside
each `package main`. Each tree describes one app: each folder is a path segment, each page shape in it
a route, a name in brackets — `[employer]` — a typed parameter, a
`_layout.vuka.html` wraps every page below it, a `guard.go` checks every
request below it, and the `vuka` command writes one file, `pages/route.go`,
registering it all, and a typed link to every page. A JSON API lives in the
same tree: a file of functions named `GET`, `POST`, `PUT`, `PATCH` and
`DELETE` is an endpoint.

The generated route table is ordinary Go; applications pass its exported
`pages.Routes()` option to `web.New`.

| | |
|---|---|
| static pages | `index.vuka.html`, `new.vuka.html`: the folder's path, `/new` |
| path parameters | a name in brackets: `[id].vuka.html` is `/{id}`, a `[employer]/` folder `{employer}` for everything in it; typed by the field of its name |
| catch-all | `[path...]`: the rest of the path, slashes included — also a client app's fallback |
| layouts | `_layout.vuka.html`, around every page below it, composed parent → child; its `Load` fetches what it shows |
| route groups | a folder in parentheses, `(console)`: no part of the URL; its layout, guard and middleware cover the pages inside it alone |
| root layouts | a layout with none above it is a page's document; between two of them, navigation is a full page load |
| private folders | a folder named `_parts`: no routes, a package of components and helpers the pages import |
| several methods per page | `Post`, `Put`, `Patch`, `Delete` methods on the page's struct answer on its path |
| JSON endpoints | a file of `GET`, `POST`, `PUT`, `PATCH` and `DELETE` funcs: `api/employers/[employer].vuka` answers on `/api/employers/{employer}` |
| guards | a `Guard` in `guard.go`, a check over every page, handler and endpoint below it, rendering nothing |
| middleware | a `Middleware` in `middleware.go`, HTTP middleware around every route below it — headers, logging, rate limits |
| precedence | static, then parameter, then catch-all — what `net/http`'s mux does |
| one registration | `pages/route.go`'s `Routes()`, passed to `web.New` |
| checked at build time | conflicts are errors with a file and line; `vuka routes` prints the table |

## The tree

```text
main.go                        web.New(pages.Routes(), …)
pages/
  _layout.vuka.html            Layout: the document of every page
  middleware.go                Middleware: a header and a log line on every request
  about.vuka.html              About — /about
  route.go                     (written by vuka) func Routes() web.Option
  employers/
    index.vuka.html            Index — /employers
    new.vuka.html              New   — /employers/new (+ Post)
    [employer]/                {employer}, for everything below
      _layout.vuka.html        Layout{Employer int; Children} with Load
      index.vuka.html          Index — /employers/{employer}
      edit.vuka.html           Edit  — /employers/{employer}/edit (+ Post, Delete)
      permits/
        [permit].vuka.html     Permit — /employers/{employer}/permits/{permit}
  files/
    [file...]/                 {file...}, the rest of the path
      _layout.vuka.html        Layout with Load: an unknown file is not found
      index.vuka.html          Index — /files/{file...}
  app/
    settings.vuka.html         Settings — /app/settings
    [route...].vuka.html       Shell — /app/{route...}
  api/
    guard.go                   Guard: an API key for everything below
    middleware.go              Middleware: the API's rate limit
    employers/
      index.go                 Index — GET and POST /api/employers (an endpoint type)
      [employer].vuka          GET, PUT, DELETE /api/employers/{employer}
```

`main` passes `pages.Routes()` to `web.New`, once:

```go
func main() {
	a := web.New(pages.Routes())
	a.Service(NewStore)
	if err := a.Run(""); err != nil {
		log.Fatal(err)
	}
}
```

`vuka build`, `run`, `test` and `vet` keep `pages/route.go` registering
every page of every folder, so a new folder is served on the next build
with nothing else to edit, and `pages/urls` holds a typed link to every
page ([below](#typed-urls)). A module with pages whose `main` never calls
`pages.Routes()` serves none of them; `vuka routes` and `vuka vet` say so
([W0701](/errors/W0701)).

The [`folder-routes`](https://github.com/vuka-lang/vuka/tree/main/examples/folder-routes)
example is this tree, with an in-memory store.

## Several apps in one module

A `pages/` directory beside a `package main` belongs to that main: it is
that app's folder routes, its URLs start from that `pages/`
(`cmd/admin/pages/index.vuka.html` is `/` of the `cmd/admin` app), and only
that main serves it — [W0701](/errors/W0701) is reported when it doesn't
pass that tree's `Routes()` to `web.New`. The module-root `pages/` is
served by any of the module's mains. A `pages/` directory beside no main,
other than the module's, is not folder routes.

Discovery checks the module-root `pages/` and a `pages/` directly beside
each package whose clause is `package main`. It is not restricted to a
folder named `cmd/`, and a nested main package does not silently acquire
a sibling app's route tree.

```text
go.mod
pages/                       module-root page tree
  index.vuka.html
  route.go                   generated root Routes
  urls/urls_gen.go           generated root links
cmd/
  admin/
    main.go                  imports example.com/app/cmd/admin/pages
    pages/
      index.vuka.html
      route.go               generated admin Routes
      urls/urls_gen.go        generated admin links
  public/
    main.go                  imports example.com/app/cmd/public/pages
    pages/
      index.vuka.html
      route.go               generated public Routes
      urls/urls_gen.go        generated public links
```

Each app imports its own pages package and passes that package's `Routes()`
to `web.New`; its pages import that tree's own `pages/urls` path. Generation
writes a separate route table and URL package per tree. The trees are not
merged into a global registration. The module-root tree is discovered too;
serving a tree still requires passing its Routes option to the app.

## Pages and segments

A page is a `.vuka.html` shape in the page tree:

- a [live component](/web/live) (it embeds `ui.Live`, or its markup sends
  an event): a live page;
- a static shape (a `.vuka.html` file's struct and markup): a stateless page, built and rendered per
  request.

A page declared in a `.vuka.html` file is named by its file: `index` serves
the folder's own path, any other name adds itself, `new.vuka.html` is
`/new`, `reset-password.vuka.html` `/reset-password`, whatever its struct
is called. A `_layout.vuka.html`'s
struct, and types named `Guard`, are never pages.

```html
// pages/about.vuka.html
package pages

// About is GET /about, a page with nothing to load.
type About struct{}
<section id="about">
  <h1>About</h1>
  <p>Employers, their permits and the files that go with them.</p>
</section>
```

A page with nothing to inject needs no constructor: `route.go` builds it
with `func() *About { return &About{} }`. A shape's provided dependency fields are filled by the generated constructor.
An explicit `New<T>` constructor can still initialize dependencies and defaults;
for a page named `New`, that constructor is `NewNew`.

A rendering shape placed under `pages/` declares a route, so shared
components belong in a [private folder](#private-folders), `pages/_parts/`,
or in another package such as `components/`. A misplaced shape appears in
`vuka routes` and collides with an existing method/path as
[E0701](/errors/E0701). Logic-only types do not become alternative page views.

Each request builds the page with its constructor and binds its exported
fields as a live page's are: props from the path
segment of its name, else the query parameter, once the
[guards](#guards) and [layouts](#layouts) above it have loaded. A stateless
page has no hooks — a page that loads in a hook is a live page, its
`@ui.Mount` running then — so it reads what the parameters name where it
uses it:

```html
// pages/employers/[employer]/edit.vuka.html
package employer

import "example.com/app/store"
import "example.com/app/pages/urls"

type Edit struct {
	Employer int
	db       *store.Store
}

func NewEdit(s *store.Store) *Edit { return &Edit{db: s} }

func (p *Edit) employer() store.Employer {
	e, _ := p.db.Employer(p.Employer)
	return e
}

func (p *Edit) Title() string { return "Edit " + p.employer().Name }
<form method="post" action={urls.Employers.Employer(Employer).Edit.Path()}>
  <input name="name" value={employer().Name}/>
  <button>Save</button>
</form>
```

A live page loads what it shows in its `@ui.Mount` hook, which runs as its
session renders; see [Hooks](/ui/stateful#hooks).

### Titles

A page with `Title() string` names the document: before the layouts render
the context carries it, and a layout reads it with `ui.PageTitle(ctx)`. A
stateless page's `Title` runs once its fields are bound. A live page's runs once
its fields are bound and before its mount hook, so a live page titles
itself from its parameters (`"Permit 2"`) or what its constructor set.

## Path parameters

A name in brackets is a path parameter. A file, `[id].vuka.html`, is a page
at the parameter: `pages/pets/[id].vuka.html` is `/pets/{id}`. A folder,
`[employer]/`, is the segment for everything in it — its layout, its
pages, its endpoints — and folders nest, each binding its own parameter:
`[employer]/permits/[permit].vuka.html` is
`/employers/{employer}/permits/{permit}`. The name inside the brackets is a
Go identifier and binds once in a path ([E0704](/errors/E0704)).

A parameter's type is the type of the field that binds it: the exported
field of its name, case aside, on the structs below it — `Employer int` for
`[employer]`, `ID int64` for `[id]`. Each struct that reads the parameter
declares the field, and they agree ([E0712](/errors/E0712)); the folder's
`_layout.vuka.html` speaks first. A parameter nothing binds has no type
([E0711](/errors/E0711)).

| field type | the segment |
|---|---|
| `int`, `int64`, `uint`, … | digits |
| `string` | any one segment |
| a type with `MarshalText` and `UnmarshalText` | what it parses |
| `web.Rest` or `string`, for `[name...]` | the rest of the path, slashes included |

`validate:` tags on the field apply as they do to any binding. A path
segment that doesn't parse or validate is **404**, not 400: `/employers/x`
names no employer, the same as a page that isn't there. (A query parameter
that doesn't parse is still 400.)

```html
// pages/employers/[employer]/permits/[permit].vuka.html
package permits

import "github.com/vuka-lang/ui/v2"
import "context"
import "github.com/vuka-lang/web/v2"
import "example.com/app/store"

type Permit struct {
	ui.Live
	Employer int
	Permit   int `validate:"min=1"`
	db       *store.Store
	kind     string
}

func NewPermit(s *store.Store) *Permit { return &Permit{db: s} }

@ui.Mount
func (p *Permit) mount(ctx context.Context) error {
	pm, ok := p.db.Permit(p.Employer, p.Permit)
	if !ok {
		return web.NotFound
	}
	p.kind = pm.Kind
	return nil
}
<section id="permit"><h2>{kind}</h2></section>
```

A segment is a file or a folder, not both: `[id].vuka.html` beside `[id]/`
is [E0713](/errors/E0713) — the folder's page is its `index`. Two
parameters side by side, `[employer]/` and `[name].vuka.html`, match the
same URLs ([E0702](/errors/E0702)).

### Catch-all

`[name...]` takes the rest of the path. As a file it is a page; as a folder
it is the end of its branch: no subfolders, and no page but its `index`
([E0703](/errors/E0703)). `/files/handbook/leave.txt` binds
`[file...]`'s `File web.Rest` to `handbook/leave.txt`.

### Go in a bracket folder

No import path can hold a bracket, so the go command builds no `[employer]`
folder. vuka compiles it: the Go of its `.vuka.html` and `.vuka` files goes
where the go command sees it, `param_employer/` — in the build's overlay, or on
disk with `vuka gen -inplace`, so a plain `go build` works after it. The
folder's own `.go` files, logic and `_test.go` alike, go there too: `vuka
build`, `run`, `test` and `vet` and the editor (through `vuka lsp`) take them
as the folder's package, and an error or a definition in one points at the
file in `[employer]/`, not its copy. Every file of the folder is one package
([E0716](/errors/E0716) names a `.go` file whose package clause says
otherwise), and a file named for a parameter, `[id].go`, is `[id].vuka`.

The `param_employer/` directory is the overlay's alone: nothing is made in
your tree for it (`vuka test` and `vuka vet`, whose tools run in a
package's directory, make it for their run and remove it after), and one
left over, empty, from an older vuka is removed. After `vuka gen
-inplace` the copies are on disk in `param_employer/` — generated, so
gitignore the directory — and a plain `go build .` builds; a plain `go
list ./...` still matches a bracketed folder holding `.go` files, whose
name no import path can hold, so name the packages
(`./pages/orders/param_order`) or use the vuka commands.

## A client app under one path

A client-side app — one that routes in the browser — needs the same
document for every URL it owns. A catch-all page gives it that: it renders
the app's shell (an [island](/web/islands), or the frontend's entry), so
`/app/`, `/app/employers/7` and anything below `/app/` answer with it, and
the app reads `location.pathname`. Pages beside it still win: a fixed
segment beats the catch-all, so `/app/settings` stays a server page.

```html
// pages/app/[route...].vuka.html: Shell, /app/{route...}
package app

type Shell struct {
	Route string
}

func (p *Shell) Title() string { return "App" }
<div id="app"></div>
<script type="module" src="/assets/app.js"></script>
```

Hand-written JSON routes under the same prefix (`/app/api/…`) win over the
catch-all the same way.

## Layouts

A folder's `_layout.vuka.html` wraps every page in it and below. Layouts
compose from the root down: the outermost (nearest `pages/`) is the
document, the next one renders inside its `Children`, and the page inside
the innermost. The leading underscore keeps it from being a page of its
own.

```html
// pages/_layout.vuka.html
package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Layout struct {
	Children ui.Node
}

func (l Layout) Title(ctx context.Context) string { return ui.PageTitle(ctx) + " - HR" }
<html lang="en">
<head><title>{Title(ctx)}</title></head>
<body>
  <main>{Children}</main>
</body>
</html>
```

A layout is the struct its file declares, with a `Children ui.Node` field
([E0708](/errors/E0708)). It binds the parameters of its path by field, as
a page does, and it may have a `Load` method, its parameters after `ctx`
coming from the container. On each request, from the outermost in, a
layout is made fresh, its path parameters bound, `Load` called, and then it
renders around the next one. Every `Load` runs before the page renders and
before a live page's mount hook, which sees only what the layouts let
through. An error from `Load`
answers the request as a handler's would: `web.NotFound` is a 404,
`web.Forbidden` a 403, a `web.Redirect` redirects. `Load` guards
everything beneath its folder — every page and every page's `Post`, `Put`,
`Patch` and `Delete` — so it is the natural place for a check that holds
for a whole folder and needs what the layout shows anyway: that the record
exists. A check with nothing to show is a [guard](#guards).

```html
// pages/employers/[employer]/_layout.vuka.html
package employer

import "github.com/vuka-lang/ui/v2"
import "context"
import "github.com/vuka-lang/web/v2"
import "example.com/app/store"
import "example.com/app/pages/urls"

type Layout struct {
	Employer int
	Children ui.Node
	name     string
}

func (l *Layout) Load(ctx context.Context, s *store.Store) error {
	e, ok := s.Employer(l.Employer)
	if !ok {
		return web.NotFound
	}
	l.name = e.Name
	return nil
}
<article>
  <h1>{name}</h1>
  <nav><a href={urls.Employers.Employer(Employer).Path()}>Overview</a> <a href={urls.Employers.Employer(Employer).Edit.Path()}>Edit</a></nav>
  {Children}
</article>
```

So `/employers/99`, `/employers/99/edit`, a `POST` to it and every permit
of employer 99 are not found, checked in one place. A live page's layouts
are part of its page: they render around it on the GET and again when its
socket joins (`Load` runs for each), and the socket's patches re-render the
page alone. A route with folder layouts takes no `web.Layout` and isn't
wrapped in the app's `web.LiveLayout`.

The layout struct and markup belong in the same shape file. A layout type
without its `_layout.vuka.html` renders nothing and is an error
([E0715](/errors/E0715)).

## Route groups

An admin area usually has a shell — its navigation, a header with the
signed-in user — and a guard that sends anyone else to sign in. The sign-in
page lives under the same URL prefix, `/admin/auth/login`, yet must not be
inside that shell or behind that guard: a visitor who isn't signed in has
to be able to reach it. A route group holds the pages that share the shell.
A folder whose name is in parentheses, `(console)`, adds no segment to the
URL, and its `_layout.vuka.html`, `guard.vuka` and middleware apply only to
the pages inside it:

```text
pages/
  _layout.vuka.html                Layout: the document of every page
  admin/
    (console)/
      _layout.vuka.html            Shell: the admin area's navigation
      guard.vuka                   Guard: a signed-in user, else the sign-in page
      index.vuka.html              Index — /admin
      users/index.vuka.html        Index — /admin/users
      users/[id].vuka.html         User  — /admin/users/{id}
    auth/
      login.vuka.html              Login — /admin/auth/login: no shell, no guard
```

`/admin/users` renders inside `Shell` inside `Layout`; `/admin/auth/login`
inside `Layout` alone. A group's layout always nests inside the layouts of
the folders above it — `pages/_layout.vuka.html` still wraps everything —
and never replaces them. Groups compose with ordinary folders and
parameters: a group may sit anywhere in the tree, hold parameter folders,
and hold other groups. Several groups side by side give one segment several
layouts, each over its own pages, and a group can take a part of a folder's
pages into a layout while the pages beside it keep out of it.

The guard sends the visitor elsewhere with `web.Redirect`, as any guard
does, here to a page outside the group:

```go
// pages/admin/(console)/guard.vuka
package console

import (
	"context"

	"github.com/vuka-lang/web/v2"
)

type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	if _, err := web.Request(ctx).Cookie("session"); err == nil {
		return nil
	}
	return web.Redirect("/admin/auth/login")
}
```

A group's name is a Go identifier ([E0723](/errors/E0723)): the go command
sees the folder as `group_console/`, as it sees a bracket folder as a
`param_` directory: its Go in the build's overlay, or written there by
`vuka gen -inplace`. As in a bracket folder, a group's `.go` files
— logic and tests — are its package, compiled where the go command sees
it. The group is invisible to the URL and
to [typed URLs](#typed-urls): the pages above are `urls.Admin.Path()`,
`urls.Admin.Users.ID(7).Path()` and `urls.Admin.Auth.Login.Path()`.
`vuka routes` names the group's layout and guard on each route below it.

Two routes that resolve to the same URL and method through groups — one
inside a group and one beside it, or one in each of two groups — are an
error naming both files ([E0722](/errors/E0722)): a group alone never tells
two pages apart. `vuka add page /admin/(console)/members` makes a page
inside a group, at `/admin/members`.

## Root layouts

A layout with no layout above it is a root layout: the document of the
pages below it. With `pages/_layout.vuka.html` there is one, the site's.
Without it, each top-level folder or group may bring its own — a
marketing site and a shop with different documents, different scripts,
different `<head>`s:

```text
pages/
  (site)/
    _layout.vuka.html            Site: the marketing site's document
    index.vuka.html              Home  — /
    about.vuka.html              About — /about
  (shop)/
    _layout.vuka.html            Shop: the shop's document
    cart.vuka.html               Cart  — /cart
  [lang]/
    _layout.vuka.html            Doc: the docs' document, <html lang={Lang}>
    docs.vuka.html               Docs  — /{lang}/docs
```

A root layout may sit below a parameter folder, as `[lang]` does, and
binds its parameters as any layout does. When the top-level groups bring
the root layouts, the home page belongs to one of them
([E0724](/errors/E0724)).

Each route's root layout is in its registration (the first of its layouts,
`web.RouteInfo.RootLayout`), in `vuka routes -json` as `root`, and on the
page as the runtime script's `data-vk-root`. In-app navigation between two
pages under the same root patches the page in place; between two roots, or
to a page without one, it is a full page load, so the new document's head
and scripts are its own.

## Private folders

A page often has parts — a card, a table row, a form section — that belong
next to it, not in a package across the module. A folder whose name starts
with an underscore is private: it and everything below it route nothing,
and its `.vuka.html` and `.vuka` files are an ordinary package the pages
import. `_layout.vuka.html` is a file, not a folder, and stays the
layout.

```text
pages/
  _parts/
    card.vuka.html               Card: no route
  admin/auth/
    login.vuka.html              Login — /admin/auth/login, framed in a Card
```

The go command sees a private folder below `pages/` as `private_parts/` —
`go build ./...` passes over a directory starting with `_` — so the pages
import it by that name:

```html
<!-- pages/admin/auth/login.vuka.html -->
package auth

import "example.com/app/pages/private_parts"

type Login struct{}
<parts.Card title="Sign in">
  <form method="post"><button>Sign in</button></form>
</parts.Card>
```

As in a group, a `.go` file beside its Vuka source is compiled with it, in
`private_parts/`. A private folder of Go alone is untouched: the go command
builds it where it is, as `pages/_parts`.

## Several methods on one page

A page answers other methods on its own path with methods named after
them: `Post`, `Put`, `Patch` and `Delete`. (`GET` is the page itself.)

```html
// pages/employers/[employer]/edit.vuka.html
package employer

import "context"
import "github.com/vuka-lang/web/v2"
import "example.com/app/store"
import "example.com/app/pages/urls"

type Edit struct {
	Employer int
	db       *store.Store
}

func NewEdit(s *store.Store) *Edit { return &Edit{db: s} }

func (p *Edit) Post(ctx context.Context, form store.EmployerForm) (web.Redirect, error) {
	p.db.UpdateEmployer(p.Employer, form)
	return web.Redirect(urls.Employers.Employer(p.Employer).Path()), nil
}

func (p *Edit) Delete(ctx context.Context) error {
	p.db.DeleteEmployer(p.Employer)
	return nil
}
<form method="post" action={urls.Employers.Employer(Employer).Edit.Path()}>…</form>
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
employer's layout has answered 404 already. A handler's `ui.Node` result
is wrapped in the layouts when the response is a whole page (a `GET`, or
the browser asking for a document); a fragment for a script goes out bare.

## Endpoints

A JSON API is files of functions named for their HTTP method: `GET`,
`POST`, `PUT`, `PATCH` and `DELETE`. The file is the route, as a page's
is: `index.vuka` the folder's path, `[employer].vuka` `/{employer}`,
`search.vuka` `/search`. Put them under `pages/api/` and the API sits
beside the pages, with typed links, parameters and guards like theirs.

```go
// pages/api/employers/[employer].vuka
package employers

// GET is one employer: the scalar named like the parameter binds it.
func GET(ctx context.Context, employer int, s *Store) (Employer, error) {
	em, ok := s.Employer(employer)
	if !ok {
		return Employer{}, web.NotFound
	}
	return em, nil
}

// DELETE removes it.
func DELETE(ctx context.Context, employer int, s *Store) error {
	if _, ok := s.Employer(employer); !ok {
		return web.NotFound
	}
	return nil
}
```

A function takes what [any handler](/web/parameters) takes: `ctx`, a
scalar named like a path parameter (bound from the segment; a segment that
doesn't parse is 404) or a query parameter, one struct from the body —
JSON or a form, bound and validated (a failing `validate:` tag is 422) —
`*http.Request`, and provided dependencies. What it returns is the
response:

| the function returns | the response |
|---|---|
| a value and a nil error | 200, the value as JSON |
| a nil error alone | 204, no body |
| an error | its status, as JSON: `web.NotFound` 404, `web.Forbidden` 403, a failed `validate:` 422, any other `web.Error` its own, else 500 |
| a `ui.Node` | the markup, as a page's handler answers |
| a `web.Redirect` | 302 on `GET`, 303 on the others |

Two files of a package may each declare `GET` — `index.vuka`'s and
`[employer].vuka`'s are different routes — and vuka keeps them apart by
renaming each `.vuka` file's (`GET_index`, `GET_employer` in `route.go`);
two `.go` files can't be renamed ([E0717](/errors/E0717)), so endpoint
files are `.vuka`.

An endpoint may also be a type: an exported struct whose pointer isn't a
page — no markup, no `ui.Live` — with methods named `Get`, `Post`, `Put`,
`Patch` or `Delete`, at its name's path as a page is (`Index` the folder's).
Each request builds it and binds its fields as a page's are. A page and an
endpoint may share a path when their methods differ; two answering one
method on one path are [E0701](/errors/E0701). `pages/urls` links to
endpoints as to pages, `urls.Api.Employers.Employer(7).Path()`, and `vuka
routes` marks them `(endpoint)`.

## Guards

A folder whose package declares `type Guard struct` checks every request to
a page, handler or endpoint in it and below, and renders nothing. `Guard`
binds the parameters of its path by field, and has a `Load(ctx, deps…)
error`, its parameters after `ctx` coming from the container — an API key,
a permission, a tenant that must exist. It is logic, so it lives in a
`.go` file, `guard.go` by convention (`guard.vuka` in a bracket folder):

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
before a stateless page is built, before a live page renders (on the
`GET` and again when its socket joins, a refused join answering a live
error with the status), and before a handler's or endpoint's body is read,
so `POST /api/employers` with the read key is 403 whatever it sends.

Guards and layouts load from the root down, and within one folder the
`Guard` before the layout; the first error stops the request. A layout's
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
keeps, which is that route's own:

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
middleware (root → folder) → guards' and layouts' Load → bind → mount hook → handler
```

and the response passes back out through the middleware. A route's own
middleware — a `web.Use` attribute —
runs inside its folders'; [auth](/web/auth)'s gates run outside them.
Folder middleware wraps the whole route, so it also sees the 404 a path
parameter that doesn't parse answers. Not calling `next` short-circuits
the request: the middleware writes the response itself — a maintenance
page, an IP block, a rate limit's 429, a cache hit — and nothing below it
runs, the guards included.

Middleware wraps folder routes only: hand-written routes, static files
and the app's 404 pass it by (give the whole app middleware with
`web.New(web.Use(mw))`). On a live page it wraps the `GET` and the
socket's join request; the socket's events don't go back through it —
a check that must hold the whole session is a [guard](#guards), which
re-runs on the join. One `Middleware` per package, like `Guard`; the file
name `middleware.go` is convention. A wrong signature is
[E0710](/errors/E0710).

**A middleware or a guard?** A guard decides *whether* — it has the path's
parameters and the container, returns a typed error, and answers in the
route's own shape (an HTML error page, JSON for an endpoint). Middleware
decides *around* — it sees the raw request and response, runs before
everything and after everything, and what it writes is its own. An API
key is a guard; the rate limit in front of it is middleware.

### Attributes on a page type

A folder route has no registration to hang an option on — options a page
needs go on its type, with `vuka.RegisterType`, as a live page's do. They
reach stateless pages, their method handlers and endpoint types too, so a
login page under `pages/` opts out of [auth](/web/auth)'s sign-in the usual
way:

<!-- go:fragment -->
```go
vuka.RegisterType[Login](auth.Public())
```

An attribute only a `web.WrapRoutes` function gives a meaning to (a
`web.RouteAttr`, what auth's gates are) reaches that function with the
page's route, stateless or live.

## Typed URLs

vuka writes one more package, `pages/urls`, with a link to every page,
shaped like the tree: a folder is a value, a fixed child folder or a page
its field, a parameter a method taking the parameter's type, and `Path()`
the URL.

<!-- go:fragment -->
```go
urls.Root.Path()                                    // "/": the pages/ folder's index
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
  name: `job-permits/` is `JobPermits`) and its pages other than its index
  (by file: `New`, `Edit`, `TermsOfService`). Its parameters are methods named for them in Go's style — `[id]` is
  `ID(id int64)`, `[employer]` `Employer(employer int)` — taking the type
  of the field that binds them.
- `Path()` exists only where there is a page or an endpoint: on its field,
  on a folder with an index, and on a parameter with a page.
  `urls.Employers.Employer(7).Permits` has no `Path()` — `permits/` holds
  no index — so a link to a URL that serves nothing doesn't compile.
- Each segment is escaped: an integer written out, a string or a type's
  `MarshalText` output through `url.PathEscape`, a catch-all segment by
  segment, its slashes kept.

`urls` imports no page package, so any page links to any other — a parent
to its children, a layout to the pages it wraps. Renaming a folder,
removing a page or changing a parameter's type makes every stale link a
compile error. A parameter's type is declared outside `pages/` for the same
reason ([E0705](/errors/E0705)).

`pages/urls` belongs to vuka: it is never a page folder. Names in a
folder's value can't meet — a folder `new/` beside a page `new`, a child
named `Path` in a folder with an index, a child named `Root` at the root
are each [E0706](/errors/E0706) — and a parameter named `[path]` would be
a method `Path(…)` beside `Path()`: name it for what it holds.

Query parameters are not part of the chain: add them to the path, as in
`urls.Employers.Path() + "?page=2"`.

## Precedence and conflicts

At one position a fixed segment beats a parameter, and a parameter beats a
catch-all, as `net/http`'s mux matches: `/employers/new` is the `new`
page, never employer `new`. Everything else that could make two routes
meet, or a page, layout or URL function that can't be generated, is an
error when the routes are generated, at the file and line that causes it:

| | |
|---|---|
| [E0701](/errors/E0701) | two pages or endpoints answering one method on one path |
| [E0702](/errors/E0702) | two routes matching the same paths, such as two parameters side by side |
| [E0703](/errors/E0703) | a folder or page below a catch-all folder |
| [E0704](/errors/E0704) | a bracketed name that names no parameter, or one bound twice in a path |
| [E0705](/errors/E0705) | a parameter type a URL can't hold, or one declared in a page package |
| [E0706](/errors/E0706) | a name vuka needs that is taken: a child folder and a page named alike, `Routes` in package pages |
| [E0707](/errors/E0707) | a folder route and a hand-written route on one path |
| [E0708](/errors/E0708) | a malformed layout, `Guard` or `Load` |
| [E0709](/errors/E0709) | a handler or method func that can't be bound, or a `Get` method on a page |
| [E0710](/errors/E0710) | a malformed `Middleware` |
| [E0711](/errors/E0711) | a parameter no field binds, or a parameter page without a struct |
| [E0712](/errors/E0712) | the fields binding a parameter of different types |
| [E0713](/errors/E0713) | a parameter both a file and a folder |
| [E0714](/errors/E0714) | a legacy parameter declaration instead of a bracketed segment |
| [E0715](/errors/E0715) | a layout type without `_layout.vuka.html` |
| [E0716](/errors/E0716) | a `.go` file named for a parameter, or of another package than its folder's |
| [E0717](/errors/E0717) | two `.go` files declaring one method func |
| [E0718](/errors/E0718) | a vuka 1 `pages_gen.go` or `routes_gen.go`, or a hand-written `pages/route.go` |
| [E0719](/errors/E0719) | a page template whose struct is declared nowhere |
| [E0722](/errors/E0722) | two routes on one URL told apart only by a route group |
| [E0723](/errors/E0723) | a name in parentheses that names no route group |
| [E0724](/errors/E0724) | the home page outside the top-level groups' root layouts |

Explicit API handlers registered on an app can coexist on distinct paths.
Page URLs remain declared by the page tree.

## vuka routes

`vuka routes` prints the table — method, path, page or endpoint (marked
when it is live or an endpoint), source, middleware, guards and layouts:

```text
$ vuka routes
METHOD  PATH                                    PAGE                             FILE                                                      MIDDLEWARE                        GUARDS     LAYOUTS
GET     /about                                  pages.About                      pages/about.vuka.html:4                                   pages.Middleware                             pages.Layout
GET     /api/employers                          employers.Index.Get (endpoint)   pages/api/employers/index.go:24                           pages.Middleware, api.Middleware  api.Guard  pages.Layout
POST    /api/employers                          employers.Index.Post (endpoint)  pages/api/employers/index.go:37                           pages.Middleware, api.Middleware  api.Guard  pages.Layout
GET     /api/employers/{employer}               employers.GET (endpoint)         pages/api/employers/[employer].vuka:13                    pages.Middleware, api.Middleware  api.Guard  pages.Layout
PUT     /api/employers/{employer}               employers.PUT (endpoint)         pages/api/employers/[employer].vuka:22                    pages.Middleware, api.Middleware  api.Guard  pages.Layout
DELETE  /api/employers/{employer}               employers.DELETE (endpoint)      pages/api/employers/[employer].vuka:31                    pages.Middleware, api.Middleware  api.Guard  pages.Layout
GET     /app/settings                           app.Settings                     pages/app/settings.vuka.html:23                                  pages.Middleware                             pages.Layout
GET     /app/{route...}                         app.Shell                        pages/app/[route...].vuka.html:5                          pages.Middleware                             pages.Layout
GET     /employers                              employers.Index (live)           pages/employers/index.vuka.html:14                               pages.Middleware                             pages.Layout
GET     /employers/new                          employers.New                    pages/employers/new.vuka.html:15                                 pages.Middleware                             pages.Layout
POST    /employers/new                          employers.New.Post               pages/employers/new.vuka.html:23                                 pages.Middleware                             pages.Layout
GET     /employers/{employer}                   employer.Index                   pages/employers/[employer]/index.vuka.html:13             pages.Middleware                             pages.Layout, employer.Layout
GET     /employers/{employer}/edit              employer.Edit                    pages/employers/[employer]/edit.vuka.html:15              pages.Middleware                             pages.Layout, employer.Layout
POST    /employers/{employer}/edit              employer.Edit.Post               pages/employers/[employer]/edit.vuka.html:32              pages.Middleware                             pages.Layout, employer.Layout
DELETE  /employers/{employer}/edit              employer.Edit.Delete             pages/employers/[employer]/edit.vuka.html:37              pages.Middleware                             pages.Layout, employer.Layout
GET     /employers/{employer}/permits/{permit}  permits.Permit (live)            pages/employers/[employer]/permits/[permit].vuka.html:16  pages.Middleware                             pages.Layout, employer.Layout
GET     /files/{file...}                        file.Index                       pages/files/[file...]/index.vuka.html:9                   pages.Middleware                             pages.Layout, file.Layout
```

## Generated files

`vuka build`, `run`, `test`, `vet` and the editor see generated routes through
an overlay. `vuka run` (and `vuka run -watch`, on every rebuild) also writes
the two files hand-written code imports, `pages/route.go` and
`pages/urls/urls_gen.go`, to disk whenever their text changes, so an editor
and plain Go tools resolve `pages.Routes` and the typed URLs; they are
generated, so gitignore them, and the overlay holds the same text. Nothing
else `vuka run` generates is written. `vuka gen -inplace` writes each tree's `route.go`,
`urls/urls_gen.go` and the Go for bracket folders under `param_name/`, route
groups under `group_name/` and private folders under `private_name/`, so
ordinary Go tooling can build the result. `-check` fails if generated files
are stale. Commit or ignore these generated artifacts consistently; edit the
source tree rather than the generated registrations.

The route table records page constructors, typed parameter binding, layout
and guard loading, method receivers and middleware order. A layout without
Load only wraps markup and adds no work to an endpoint's request.
