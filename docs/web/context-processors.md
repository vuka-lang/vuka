# Context Processors

[[toc]]

## Introduction

Most pages show something that isn't theirs: the signed-in user's name in
the header, a menu item only some people may use, the message "Saved" after
a form, the current path in a breadcrumb. Passing those down as props
through every layout and component is tedious, and calling
`auth.Current(ctx)` in each one computes the same thing again and again.

A **context processor** is a function that computes such values once per
request. Each exported field of the struct it returns becomes a template
variable that any template of the app reads by name:

```html
<header>
  if user != nil {
    <span>{user.Name}</span>
  }
  <small>{request.Path}</small>
</header>
```

`user` and `request` aren't props, fields or locals here. They are
variables of the processors `main` lists, typed like any Go value: `user`
is an `*auth.Identity`, a typo is a compile error with a suggestion, and
the editor completes `user.` and says where `user` comes from.

## Listing Processors {#listing}

Every processor an app uses is listed once, in `main`, with
`web.ContextProcessors`. Nothing registers itself on import, and the
framework has no list of its own: a processor not in the list doesn't run,
and its variables don't exist in templates.

```go
package main

import (
	"log"

	"example.com/app/pages"
	"example.com/app/users"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

func main() {
	app := web.New(
		pages.Routes(),
		web.ContextProcessors(
			web.RequestContext,
			auth.Context,
			web.MessagesContext,
			web.CSRFContext,
			ui.PageContext,
			users.Context,
		),
	)
	log.Fatal(app.Run(""))
}
```

`vuka new` writes this list with the built-in processors, so they are
visible from the start; remove `auth.Context` and `user` and `perms` are
gone from templates.

The built-in processors are ordinary processors, written as yours are:

| Processor | Variables |
|---|---|
| `web.RequestContext` | `request`: the path, query, parameters and route |
| `auth.Context` | `user` (`*auth.Identity`, nil for an anonymous visitor), `perms` (`auth.Perms`, with `Has`), `actor` (the real user while impersonating, else nil) |
| `web.MessagesContext` | `messages` (`[]web.Message`: `Level`, `Text`): what `web.Flash` added, each shown once |
| `web.CSRFContext` | `csrfToken`: a token for a form posted from another origin, checked with `web.ValidCSRFToken` |
| `ui.PageContext` | `page` (`ui.PageHeader`): `Title`, `Crumbs` of the page being rendered |

`request` is a `web.RequestInfo`: `Path`, `Query` (`url.Values`), `Params`
(the path parameters by name), `Name` (the route's handler) and
`Pattern`.

## Writing a Processor {#writing}

A processor is a plain function of a package of the app (not of `main`):
`func(ctx context.Context, deps…) (S, error)`, `S` a struct. The
parameters after `ctx` come from dependency injection, as a handler's do.
Each exported field of `S` is a variable, its name the field's in lower
camel case: `Me` is `me`, `CSRFToken` is `csrfToken`.

```go
// users/context.go
package users

type User struct {
	ID   string
	Name string
}

type Service struct{ db *sql.DB }

func (s *Service) ByID(ctx context.Context, id string) (*User, error) {
	u := &User{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT name FROM users WHERE id = ?", id).Scan(&u.Name)
	return u, err
}

type Vars struct{ Me *User }

func Context(ctx context.Context, s *Service) (Vars, error) {
	id := auth.Current(ctx)
	if id == nil {
		return Vars{}, nil
	}
	u, err := s.ByID(ctx, id.ID)
	return Vars{Me: u}, err
}
```

List it in `main` — `users.Context` — and every template reads `me`:

```html
if me != nil {
  <a href="/account">{me.Name}</a>
}
```

A processor runs only when a template of the render reads one of its
variables, at most once per request or live event; every later read shares
its result. A processor decides what an anonymous visitor gets: here `me`
is nil.

> [!NOTE]
> Two processors giving the same variable name fail the build, naming both
> ([E0421](/errors/E0421)): no list resolves a name by its order.

## Reading Variables in Templates {#templates}

A bare name in a shape's markup, in its `<script lang="go">`, or in a
`.vuka` file reads a variable when it names nothing else there: no field,
local, parameter, package or declaration of the package. The compiler
reads the `web.ContextProcessors` call in `main`, as it reads folder
routes, so the variable has its type:

```html
for _, m := range messages {
  <p class={m.Level}>{m.Text}</p>
}
<form method="post" action="https://pay.example.com/checkout">
  <input type="hidden" name="csrf" value={csrfToken} />
</form>
```

Markup reads a variable through the render's `ctx`. A function of a
`.vuka` file, or a method of a shape's script, reads one when it takes
`ctx context.Context` ([E0423](/errors/E0423) when it doesn't):

```vuka
func (h *Header) greeting(ctx context.Context) string {
	if me == nil {
		return "Welcome"
	}
	return "Hello, " + me.Name
}
```

A name a letter or two from a variable's is a typo:
[E0424](/errors/E0424) suggests the variable and its processor.

### Where Names Are Read {#where}

Every package of the module that a package `main` imports, directly or
through others, reads variables by name: pages, layouts, components,
modules and live components alike.

A package imported by two mains — `cmd/web` and `cmd/admin`, say — reads a
name only when every main that lists processors gives it, with one type;
otherwise [E0422](/errors/E0422) names the main lacking it. A main that
lists none, such as a command running migrations, isn't counted.

Packages of other modules, such as a component library, can't see the
app's `main`, so they read nothing by name. They read a variable
explicitly, and `vuka vet` checks the read against the app's list
([E0427](/errors/E0427)):

<!-- go:fragment -->
```go
me := web.Var[*users.User](ctx, "me")
```

### Names a Template Declares {#names-a-template-declares}

A field, a prop or a local named like a variable wins, as in Go's
scoping: the template reads it. That is how a caller passes a value of its
own to a component that otherwise shows the variable. `vuka vet` says so
([W0401](/errors/W0401)), so a field named `user` by accident doesn't hide
the signed-in user unnoticed.

## Reading a Variable in Go {#reading-a-variable-in-go}

Go code reads a variable with `web.Var`, naming its type:

<!-- go:fragment -->
```go
me := web.Var[*users.User](ctx, "me")
```

A bare name in a template compiles to exactly this call. `web.Var`
returns the zero value when the read fails — no such variable, another
type, the processor's error — and logs why; `web.VarErr` returns the
error:

<!-- go:fragment -->
```go
me, err := web.VarErr[*users.User](ctx, "me")
if err != nil {
	return err
}
fmt.Println("signed in as", me.Name)
```

## Live Pages and Layouts {#live}

On a live page, each read is recorded against the component that made it,
and so is what the processor itself reads: the URL for `request`, the
identity and its permissions for `auth.Context`. When one of those
changes, only the components that read the variable render again.

A layout that stays while the visitor moves between pages can show
`request.Path` or `page.Title` in its markup: that part renders again on
each navigation and the layout stays. A layout that stores a variable in
its `Load`, which runs once, reloads instead ([W0703](/errors/W0703)).

An event that changes what a processor computed — the user renamed
themselves — says so, and the processor runs again on the next read; the
components showing `me` render again if it changed:

<!-- go:fragment -->
```go
web.RefreshVars(ctx, "me")
```

> [!NOTE]
> Template variables are read-only. URL state changes in events, with
> `web.Query.Set(ctx, "tab", key)` or a field bound with `@ui.URL("tab")`;
> `request` then follows.

## Giving Other Values {#overrides}

`web.WithVars` gives a processor's variables other values below a
context: the struct is the processor's result type, and the processor
doesn't run there. A part of a page shows another user's card this way:

<!-- go:fragment -->
```go
other := &users.User{ID: "7", Name: "Grace"}
ctx = web.WithVars(ctx, users.Vars{Me: other})
```

In tests, `viewtest.With(users.Vars{Me: u})` (package
`github.com/vuka-lang/web/v2/viewtest`) is an app option doing the same
for every page, and `viewtest.Context(ctx, users.Vars{Me: u})` for one
context.

### Rendering Outside a Request {#outside-a-request}

An email, a job or a unit test renders with a context that has no request
and no processors. Reading a variable there fails — `web.VarErr` says
there is no template variable here — and `web.Var` returns the zero value.
Give the values the template reads:

<!-- go:context
type OrderShipped struct{ Order int }

func (o OrderShipped) Render(ctx context.Context, w io.Writer) error { return nil }
-->

<!-- go:fragment -->
```go
u := &users.User{ID: "1", Name: "Ada"}
ctx = web.WithVars(context.Background(), users.Vars{Me: u})
ctx = web.WithVars(ctx, web.RequestVars{Request: web.RequestInfo{Path: "/orders"}})
html, err := ui.String(ctx, OrderShipped{Order: 42})
if err != nil {
	return err
}
fmt.Println(html)
```

## Guarding a Processor {#auth}

A processor whose variables only a signed-in visitor may read is listed
with the gates of a route:

<!-- go:fragment -->
```go
opt := web.ContextProcessors(
	web.Auth(users.Context, auth.Required),
)
```

A read by an anonymous visitor then sends them down the sign-in path — a
redirect to the login page, or 401 — instead of running the processor.

What templates show by permission is covered in [permissions in
templates](/web/auth#permissions-in-templates).

## The Browser {#the-browser}

No processor's variables reach islands or the page's scripts unless the
processor is listed with `web.Browser`:

<!-- go:fragment -->
```go
opt := web.ContextProcessors(
	web.Browser(web.RequestContext),
	web.Browser(web.Personal(users.Context)),
)
```

A processor reading the signed-in visitor gives values that are theirs
alone. `web.Browser` refuses it unless it is also marked `web.Personal`
([E0426](/errors/E0426)), and a permission list (`auth.Perms`) never
reaches the browser.

## In the Editor {#editor}

`vuka lsp` completes the variables a file may read, and their fields
after a dot; hover shows a variable's type and the processor it comes
from (`from users.Context (users/context.go)`); go to definition jumps to
the processor and to its entry in `web.ContextProcessors`. Variables have a
semantic token type of their own, `vukaContextVar`. See
[editors](/tools/editors).

## Diagnostics

| Code | |
|---|---|
| [E0421](/errors/E0421) | two processors give one variable |
| [E0422](/errors/E0422) | a variable not every main gives |
| [E0423](/errors/E0423) | a variable read without a `ctx` |
| [E0424](/errors/E0424) | a typo for a variable |
| [E0425](/errors/E0425) | a list entry that is no processor |
| [E0426](/errors/E0426) | personal values sent to the browser |
| [E0427](/errors/E0427) | a `web.Var` read the app doesn't give |
| [W0401](/errors/W0401) | a name shadowing a variable |
| [W0402](/errors/W0402) | `web.Request` in markup where `request` would do |
| [W0403](/errors/W0403) | a control hidden by a permission its event doesn't check |
