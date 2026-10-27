# Parts of a Page

[[toc]]

## Introduction

A dashboard's figures take a slow query, an inbox waits on another
service, a list of recent items reads a table that is busy at noon. A page
that reads them all before it answers is as slow as its slowest part, and
shows nothing until then.

A page that isn't live can show itself at once and fill in its slow parts
afterwards, with no client code and no live connection. You mark the slow
method `@ui.Defer`; the markup that calls it is its **part**. The page is
sent with a stand-in for each part, and each part follows in the same
response as soon as its method returns.

## Deferring a Method

This page shows an account's figures, which take a slow query. The method
returning them is marked `@ui.Defer`, and the markup calls it as it would
any method:

```html
<script lang="go">
package pages

import (
	"context"

	"example.com/app/applications"
)

type Index struct {
	apps *applications.Service
}

@ui.Defer
func (p *Index) Stats(ctx context.Context) (applications.Stats, error) {
	return p.apps.Stats(ctx)
}
</script>

<section>
  <h2>My applications</h2>
  <p>{Stats(ctx).Open} open · {Stats(ctx).Shortlisted} shortlisted</p>
</section>
```

The page answers before `Stats` returns: the heading at once, the `<p>`
empty until the figures arrive. Then the `<p>` with its figures replaces it.

A deferred method takes a `context.Context` and returns `(T, error)`. Its
context is the request's: it is canceled when the visitor goes away.

### What a Part Is

The part of a call is the nearest element around it: the element whose own
markup — its attributes, its text, the headers of the `for`, `if` and
`switch` blocks directly in it — calls the method. In the page above, that
is the `<p>`. A `for` over a deferred method's result makes the element
holding the loop the part:

```html
<ul class="recent">
  for _, a := range Recent(ctx) {
    <li>{a.Title}</li>
  }
</ul>
```

Several calls of one method in one element are one part. Calls in separate
elements are separate parts, sharing one call of the method per request:
the method runs once, however many parts show it. An element inside a part
of the same method is part of it, not one of its own. An element calling
two deferred methods arrives when both have returned.

Keep a deferred call inside the smallest element that shows it: a `for`
header or an `if` condition directly inside `<main>` makes all of `<main>`
the part.

### Groups

Parts arrive as their methods return, in whatever order that is. Methods
that belong together take a group's name, `@ui.Defer("lists")`; a group's
parts arrive together, once all of its methods have returned:

```html
<script lang="go">
package pages

import "context"

type Index struct{}

@ui.Defer("lists")
func (p *Index) Recent(ctx context.Context) ([]string, error) { return []string{"Clerk"}, nil }

@ui.Defer("lists")
func (p *Index) Tags(ctx context.Context) ([]string, error) { return []string{"remote"}, nil }
</script>

<ul>
  for _, r := range Recent(ctx) {
    <li>{r}</li>
  }
</ul>
<p>{len(Tags(ctx))} tags</p>
```

A page's deferred methods run concurrently, eight at a time. They run on
the same page value: read its fields, don't write them.

## Fallbacks

Until its method returns, a part shows its fallback. Without one, the part's
element is there empty, marked `aria-busy="true"` — with its literal
attributes, so it keeps its place and its classes: style it with
`[aria-busy]`.

`<ui.Fallback for={Stats}>` gives the parts of `Stats` a fallback of their
own, written anywhere in the markup. It renders nothing where it is written:

```html
<p class="figures">{Stats(ctx).Open} open</p>
<ui.Fallback for={Stats}><span class="skeleton"></span></ui.Fallback>
```

A fallback stands in for every part of its method. It may not call a
deferred method itself.

## When a Method Fails

A part whose method returns an error shows its error fallback,
`<ui.Fallback for={Stats} error>`, else a short message with
`role="alert"`: the error's public message (a `*web.Error`'s), its text in
development, otherwise "This part couldn't load.". The rest of the page is
unaffected: its status is already sent. The error is logged with the
request's method and path, and the development toolbar shows it.

```html
<p class="alerts">{Alerts(ctx)} alerts</p>
<ui.Fallback for={Alerts} error><p class="alerts">Alerts are unavailable.</p></ui.Fallback>
```

A part's markup failing to render (a `(T, error)` call in it failing)
shows the same.

## How a Part Arrives

Everything that isn't deferred runs first: guards, every layout's `Load`,
the page's other methods. So the status and the headers are decided before
anything is sent, and a redirect, a 404 or a sign-in still answers as it
does for any page. Then the page is sent up to its `</body>` and flushed,
compressed or not. Its deferred methods run, and each part is appended as
it arrives: a `<template data-vk-part>` holding its markup, and a one-line
script moving it into its place. `</body></html>` ends the response.

A navigation over HTTP (a `ui.Link` on a page that isn't live) reads the
same stream: the runtime shows the page as soon as it has arrived, and
moves each part in as it follows. Leaving the page stops the parts still
coming.

The response sets `X-Accel-Buffering: no`, so nginx sends it as it is
written. A proxy that buffers responses anyway delivers the page whole:
correct, only not early.

> [!WARNING]
> A part's content arrives in a `<template>`, which a browser shows only
> through script. A crawler or a visitor without JavaScript sees its
> fallback. Don't defer what they must see: a page's main content, its
> title, what a search engine indexes.

A page whose Content-Security-Policy refuses inline scripts gets its parts
moved in when the runtime script loads, at the end of the response.

A page that holds a live component, and a page rendered for a form posted
without script, render their parts in place: the methods run before the
page is sent. `@ui.Defer` is for pages that aren't live; a live page loads
its data in `@ui.Mount` and pushes changes with `Update`.

## Parts Written in Go {#in-go}

A component written in Go registers its deferred methods with
`vuka.RegisterFunc`, and spells each part out with `ui.Part`: the method,
the fallback, and a function rendering the method's result. `ui.Part` is
the one place the method may be named:

<!-- go:context
type Stats struct{ Open, Shortlisted int }

type Service struct{}

func (s *Service) Stats(ctx context.Context) (Stats, error) { return Stats{3, 1}, nil }
-->

```go
package figures

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
)

type Panel struct{ apps *Service }

func init() {
	vuka.RegisterFunc((*Panel).Stats, ui.Defer(""))
}

func (p *Panel) Stats(ctx context.Context) (Stats, error) { return p.apps.Stats(ctx) }

func (p *Panel) View() ui.Node {
	return ui.El("section", nil,
		ui.El("h2", nil, ui.Text("My applications")),
		ui.Part(p.Stats, ui.El("span", []ui.Attr{{Name: "class", Value: "skeleton"}}), func(s Stats) ui.Node {
			return ui.El("p", nil, ui.Textf("%d open · %d shortlisted", s.Open, s.Shortlisted))
		}),
	)
}

func (p *Panel) Render(ctx context.Context, w io.Writer) error { return p.View().Render(ctx, w) }
```

`ui.Defer("")` is the bare `@ui.Defer`; `ui.Defer("lists")` names a group.
A nil fallback is an empty `<span aria-busy="true">`.
`ui.PartFallback{Pending: …, Error: …}` gives a part an error fallback too.

## Testing a Page With Parts

In a test, a page's parts render in place: the HTML a test reads is the
page once every part has arrived. `viewtest.Pending()` renders the page
as it first shows instead, each part's fallback in its place:

<!-- go:context
type Index struct{}

func (p *Index) Render(ctx context.Context, w io.Writer) error { return nil }
-->

```go
package pages_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/viewtest"
)

func TestIndexShowsSkeleton(t *testing.T) {
	app := web.New(viewtest.Pending())
	app.Get("/", func() *Index { return &Index{} })
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `class="skeleton"`) {
		t.Error("no skeleton before the figures")
	}
}
```

## In the Toolbar

Under `vuka run`, the [development toolbar](/web/toolbar)'s timeline shows
each deferred method as a `part` span of its request: its time beside the
page's render, the statements it ran (the SQL panel names them `part
Index.Stats`) and its error.

## Build Errors

A timing attribute goes only on a method of a page or component that its
own markup calls directly. Everything else is an error at the attribute:
`vuka build`, `vuka vet` and the editor report it, and for Go, `vukavet`
(registration fails the app's start too).

| Error | When |
|---|---|
| [E0341](/errors/E0341) | `@ui.Defer` on a function, or on a method of a type no template renders |
| [E0342](/errors/E0342) | the struct's markup never calls the method (in Go: no `ui.Part` names it) |
| [E0343](/errors/E0343) | the method is called from Go: another method, a hook, a handler, a template statement |
| [E0344](/errors/E0344) | its result goes to a child component's prop: defer it in the child |
| [E0345](/errors/E0345) | two timing attributes on one method |
| [E0346](/errors/E0346) | `@ui.Defer` on a live page's method |
| [E0347](/errors/E0347) | the method doesn't take a `context.Context` and return `(T, error)` |
| [E0348](/errors/E0348) | a `<ui.Fallback>` naming no deferred method, or calling one |
| [E0349](/errors/E0349) | in Go, a `ui.Part` naming another type's method |
| [E0350](/errors/E0350) | a deferred call in no element, which would be its part |
