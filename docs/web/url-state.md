# URL State

[[toc]]

## Introduction

A tab, a table's page, a sort order, a search: state the visitor expects
to bookmark, share, reload and step back through belongs in the page's
address. Keep it there and the URL is the one place it lives — the page
renders it on the first load, follows it when the visitor presses back,
and writes it when they click.

A live page does this in two ways. A component's field marked
`@ui.URL("tab")` is read from the URL and written back when an event
assigns it. An event can also change the query itself with `web.Query`.
Either way the change is a [patch](/web/navigation#patch-or-navigate) of
the page that keeps the other parameters, so two components and the page
can each keep their own.

## Keeping a Component's Field in the URL {#fields}

Mark the field with `@ui.URL` and the parameter's name. These tabs keep
their choice in `?tab=`:

```html
<script lang="go">
// tabs.vuka.html
package components

import "github.com/vuka-lang/ui/v2"

type Tabs struct {
	ui.Live
	active string @ui.URL("tab").Default("overview")
}

func (t *Tabs) Show(name string) { t.active = name }
</script>

<nav>
  <button onClick={func() { Show("overview") }} aria-pressed={active == "overview"}>Overview</button>
  <button onClick={func() { Show("history") }} aria-pressed={active == "history"}>History</button>
</nav>
```

The field is filled from the URL:

- when the component first renders, before its mount hook runs, so the
  hook sees it — on the page's HTTP render too, so the first response is
  already right;
- whenever the page's URL changes: a link with `patch`, `PushPatch`,
  back and forward.

When an event assigns the field, the URL follows: once the event's reply
is applied, the page is patched to its URL with the parameter changed and
the others kept. The reply already shows the new value; the patch only
brings the address, and the page's own [`Update`](#keeping-the-page-mounted),
in step.

A write takes the current history entry's place instead of adding one, so
back leaves the page rather than stepping through every tab clicked.
Back and forward still move through the entries other navigations made,
and the field follows them.

### Defaults and the Zero Value {#defaults}

`.Default(v)` is the value the field takes when the URL has no parameter
for it. A field holding its default leaves the parameter out of the URL,
so the plain address is the default state. Without a default, the zero
value plays that part: `""`, `0`, `false`.

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Pager struct {
	ui.Live
	page int    @ui.URL("page").Default(1)
	sort string @ui.URL("sort")
}

func (p *Pager) Next() { p.page++ }
</script>

<p>Page {page}, sorted by {sort} <button onClick={Next}>Next</button></p>
```

A parameter that doesn't parse as the field's type (`?page=x`) is the
default too: the component renders, where a page's own field would answer
400.

### Types {#types}

A URL parameter's text sets the field as a page's parameters bind: a
string as it is, a number or bool as `strconv` reads it, a type with
`UnmarshalText` through it, a slice from every value of the parameter
(`?tag=a&tag=b`), a pointer when the parameter is there. Writing formats
it back the same way. A field of another type — a map, a struct without
`UnmarshalText` — fails the component's check at start-up
([E0335](/errors/E0335)).

### Props the Parent Passes {#props}

An exported field is a prop. Mark one `@ui.URL` and the parent decides:

1. a value the parent passes — any but the zero value — wins, and the
   field is neither read from nor written to the URL;
2. else the URL's parameter;
3. else the default, or the zero value.

```html
<!-- the URL chooses the sort -->
<components.Pager />
<!-- this page always sorts by name -->
<components.Pager sort="name" />
```

## Changing the Query from an Event {#query}

`web.Query` edits the URL from any event of the page. The edits of one
event are one patch, sent once its reply is applied:

```html
<script lang="go">
// pages/requests/index.vuka.html
package requests

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

type Index struct {
	ui.Live
	Status string
}

func (p *Index) Update(web.URLParams) {}

func (p *Index) Filter(ctx context.Context, status string) {
	web.Query.Set(ctx, "status", status)
	web.Query.Del(ctx, "requests.page") // back to the first page
}
</script>

<select onChange={Filter}>
  <option value="">All</option>
  <option value="open">Open</option>
</select>
```

| Call | |
|---|---|
| `web.Query.Set(ctx, key, value)` | sets the parameter: a string as it is, a number or bool as `strconv` writes it, a slice as one value each |
| `web.Query.Del(ctx, key)` | removes it |
| `web.Query.Update(ctx, func(q url.Values) { … })` | edits the parameters in place |
| `web.Query.Get(ctx, key)` | reads one, with the event's edits so far |

Each takes `web.Push` last to add a history entry instead of taking the
current one's place: `web.Query.Set(ctx, "q", term, web.Push)`, for a
search the visitor should be able to step back from.

> [!NOTE]
> Outside a live page's event — a plain HTTP handler, a job — there is no
> page to patch, and an edit does nothing. Redirect there instead.

## Keeping the Page Mounted {#keeping-the-page-mounted}

A patch runs the page's route for the new URL. As for any patch, the page
stays mounted only when it has an `Update` taking `web.URLParams`
([the server decides](/web/navigation#the-server-decides-what-stays-mounted)):
otherwise it, and every component on it, mounts again, filled from the new
URL. A page whose state is all in its components' URL fields needs no
parameters of its own: an `Update` taking `web.URLParams` itself, as the
requests page above declares, keeps it mounted.

> [!WARNING]
> A component's URL field changes without its mount hook running again,
> and a browser can patch to any URL. A component that checks in its
> mount hook that the visitor may see what a field names must check again
> where it uses the field.

## Two Components on One Page {#url-key}

Two instances of one component keep the same parameters, each writing
over the other's. A tag's `urlKey` keeps an instance's parameters under a
name of its own:

```html
<main>
  <components.Pager urlKey="requests" />
  <components.Pager urlKey="users" />
</main>
```

The first keeps `?requests.page=` and `?requests.sort=`, the second
`?users.page=`. `urlKey` takes an expression too, `urlKey={name}`. A generic component's
instances work the same way, `<components.Table[Task] urlKey="tasks" />`
beside `<components.Table[User] urlKey="users" />`: each instantiation has
its own fields and hooks.
[`vukavet`](/tools/vukavet) warns when two components one markup renders
keep the same parameter ([W0307](/errors/W0307)).

## Reading a Path Parameter {#path-parameters}

A bare `@ui.URL` binds the route's path parameter of the field's name,
matched ignoring case. A component rendered by `pages/orders/[id].vuka.html`
reads its `{id}`:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type OrderSummary struct {
	ui.Live
	id int64 @ui.URL
}
</script>

<p>Order {id}</p>
```

A path parameter is read only: assigning the field writes nothing, since
another path is another page — navigate to it. When a folder page renders
the component and its path lacks the parameter, `vuka` reports it as it
builds the routes, naming the page's parameters ([E0340](/errors/E0340)).

## In the Editor {#editor}

Hover on a field's `@ui.URL` shows the parameter it is kept in, its
default, and the name it takes under a `urlKey`. Inside `@ui.URL("…")`,
completion offers the parameters the package's other fields keep, so two
components meant to share one spell it alike.

## Testing URL State {#testing}

ui's [reference client](/ui/testing) follows the patches a write sends and
keeps a history: `SetURL` gives the page's first URL, `URL` and `History`
say where it is, and `Back` and `Forward` move as the browser's buttons
do. A test sets the first URL with `c.SetURL("/orders?tab=overview")`,
clicks the History tab with `c.Click("#history")`, checks that `c.URL()`
is `/orders?tab=history`, then calls `c.Back()` and checks the tab follows
the URL back.
