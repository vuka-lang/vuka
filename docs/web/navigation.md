# Navigation

[[toc]]

## Introduction

A list you page through, tabs whose choice belongs in the URL, a list and
the detail page of each row: moving between them should feel instant.

A live page can move to another URL over the connection it already has. There
is no document reload, no new WebSocket and no layout sent again, while the
address bar, the back button and links opened in a new tab keep working as
they do for any site.

## Linking Between Pages {#links}

You link with `ui.Link`. This markup links to a book's page, and moves the
list to its second page with `patch`:

```html
<nav>
  <ui.Link href="/books/7">Dune</ui.Link>
  <ui.Link href="/books?page=2" patch>Next page</ui.Link>
</nav>
```

`ui.Link` renders an ordinary `<a href>`, marked for the runtime
(`data-vk-link`, or `data-vk-link="patch"`). So it stays a plain link
wherever the runtime doesn't take it: without JavaScript, on a page that
isn't live, with a `target` (`_blank`), clicked with a modifier key or the
middle button (a new tab or window), or to another origin.

The table lists the attributes `ui.Link` takes:

| Attribute | |
|---|---|
| `href` | where to go |
| `patch` | the same page with other parameters (below) |
| `active` | `"exact"` or `"prefix"`: mark the link while it is the current page's ([below](#the-current-link)) |
| `activeClass` | a class added while it is |
| `prefetch` | `"off"`: never fetched ahead; `"viewport"`: fetched once in view ([below](#prefetch)) |
| `id`, `class`, `title`, `target`, `rel` | as on `<a>` |
| `attrs` | any other attributes, as `[]ui.Attr` |

A link to the page's own URL with only a `#hash` is the browser's: it
scrolls, nothing is sent.

## Patch or Navigate

A move over the connection is one of two kinds. A **patch** moves to another
URL of the page you are on: the next page of a list, a filter, a tab. A
**navigate** moves to another page.

On a patch, the route runs again for the new URL, and a
[Live page](/web/live) that takes the URL stays mounted and keeps its state.
Its props are bound from the URL again, and the reply is the usual patch:
only what changed.

### Taking the URL in Update {#taking-the-url-in-update}

A Live page takes the URL with an `Update` overload taking a struct that
embeds `web.URLParams`. Its fields bind from the new URL the way a Live
page's props do (the path segment, else the query parameter, of each
field's name), and `Update` runs before the patch renders.

Declare the URL update message in a logic file, separate from the shape.
This one, `models/shelf_params.go`, carries the shelf's page number:

```go
// models/shelf_params.go
package models

import "github.com/vuka-lang/web/v2"

type ShelfParams struct {
    web.URLParams
    Page int
}
```

Save the page as `pages/books/index.vuka.html`. It lists a page of books,
links each title to its book, and patches itself to the next page; the
Books service and Book record live in services and models logic packages:

```html
<script lang="go">
package books

import (
    "context"
    "fmt"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Shelf struct {
    ui.Live
    Page int
    rows []models.Book
    books *services.Books
}

@ui.Mount
func (s *Shelf) mount(ctx context.Context) error {
    return s.show(ctx, s.Page)
}

func (s *Shelf) Update(ctx context.Context, p models.ShelfParams) error {
    return s.show(ctx, p.Page)
}

func (s *Shelf) show(ctx context.Context, page int) error {
    rows, err := s.books.List(ctx, page)
    s.rows = rows
    return err
}
</script>

<section>
  <ol>
    for _, book := range rows {
      <li key={book.ID}><ui.Link href={fmt.Sprintf("/books/%d", book.ID)}>{book.Title}</ui.Link></li>
    }
  </ol>
  <ui.Link href={fmt.Sprintf("/books?page=%d", Page + 1)} patch>Next</ui.Link>
</section>
```

The mount hook sees the first URL; `Update` sees each patch. Params that don't
bind (a `page=x`) make the route answer 400, and the browser loads that URL
as an ordinary page.

### The Server Decides What Stays Mounted

`patch` on a link (and `Patch`, `PushPatch`) only asks. The page stays
mounted only when its type has an `Update` taking `web.URLParams`. Any other
page **mounts again**, a fresh instance whose mount hook sees the new URL, even
on the same route, as a navigate does.

> [!WARNING]
> A browser can send any URL as a patch, so this rule is what keeps a check
> in the mount hook from being skipped. It follows that **`Update` must
> repeat every check the mount hook makes** of what the URL names.

A page showing one order checks in the mount hook that the visitor may see
it; its `Update` checks the new id the same way. The update-message DTO, in
`models/order_params.go`, is a logic type:

```go
// models/order_params.go
package models

import "github.com/vuka-lang/web/v2"

type OrderParams struct {
    web.URLParams
    ID int
}
```

In `pages/orders/[id].vuka.html`, both entry paths, the mount hook and
`Update`, call the same `load` check:

```html
<script lang="go">
package orders

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
    "github.com/vuka-lang/web/v2/auth"
)

type OrderPage struct {
    ui.Live
    ID int
    orders *services.Orders
    order models.Order
}

@ui.Mount
func (p *OrderPage) mount(ctx context.Context) error { return p.load(ctx, p.ID) }

func (p *OrderPage) Update(ctx context.Context, q models.OrderParams) error {
    return p.load(ctx, q.ID)
}

func (p *OrderPage) load(ctx context.Context, id int) error {
    order, err := p.orders.Get(ctx, id)
    if err != nil { return err }
    me := auth.Current(ctx)
    if me == nil || order.Owner != me.ID { return web.Forbidden }
    p.order = order
    return nil
}
</script>

<h1>Order {order.ID}</h1>
```

An error `Update` returns fails the patch: nothing of the page stays
mounted to take events, and the browser loads the URL. The route then
answers as for any request (here, the mount hook refusing with 403). The
same goes for a mount hook that refuses on a navigate.

### Navigating to Another Page {#navigating-to-another-page}

A navigate's route runs as a page load does: middleware, gates,
dependencies, its layouts. Its components mount on the same connection, and
the old page's unmount. With
[protocol v2](/reference/live-protocol#protocol-v2-render-trees) the new
page arrives as a change to the trees the connection already holds: the
layout's markup isn't sent again, only the parts that differ.

The [folder layouts](/web/folder-routes#layouts-that-stay) the two pages
share stay. Their `Load` doesn't run again, and their elements, scroll,
client state, live components and islands are kept; only what is below the
deepest shared layout is swapped.

Guards run on every navigation all the same. A layout reading the request's
path or query, or the page's title in its `Load`, loads and renders again
each time instead. Its markup renders for every page either way, so a
header of the page's [title and breadcrumbs](/web/folder-routes#page-headers)
follows the page while the layout stays.

## Marking the Current Link {#the-current-link}

A menu in a layout marks the page you are on. Give its links `active`, and
the runtime marks the one matching the address with `aria-current="page"`,
and `activeClass` if you give one. It does so as the page loads, after every
navigation, and on back and forward.

The layout reads nothing of the request, so it
[stays](/web/folder-routes#layouts-that-stay) across navigations, its menu
following the page. This menu marks Home on `/` only, and Orders and
Settings on their paths and below:

```html
<nav>
  <ui.Link href="/" active="exact" activeClass="on">Home</ui.Link>
  <ui.Link href="/orders" active="prefix" activeClass="on">Orders</ui.Link>
  <ui.Link href="/settings" active="prefix" activeClass="on">Settings</ui.Link>
</nav>
```

`exact` matches the same path; `prefix` the path or one below it
(`/orders/7` for `/orders`), so use `exact` for `/`. Style the current link
with the class, or with `a[aria-current=page]`, which also tells screen
readers where they are.

An ordinary `<a>` takes the same marks as attributes:
`data-vk-active="prefix"` and `data-vk-active-class="on"`.

### Opening the Current Link's Group {#the-current-group}

A long menu folds its links into groups, `<details>` the visitor opens and
closes. Mark a group `data-vk-active-within`, and it opens when the
current link is inside it: as the page loads, the server knowing nothing of
the path, and when a navigation moves the current link into it. A group
the visitor opened stays open as they leave it, and one they closed stays
closed until the current link moves into it again:

```html
<nav>
  <details data-vk-active-within>
    <summary>Orders</summary>
    <ui.Link href="/orders/open" active="prefix" activeClass="on">Open</ui.Link>
    <ui.Link href="/orders/shipped" active="prefix" activeClass="on">Shipped</ui.Link>
  </details>
  <details data-vk-active-within>
    <summary>Users</summary>
    <ui.Link href="/users" active="prefix" activeClass="on">Everyone</ui.Link>
  </details>
</nav>
```

The runtime sets the attribute to `"true"` while the element holds the
current link and to `""` when it doesn't, on any element, so a rule styles
the group it is in:

```css
[data-vk-active-within="true"] > summary { font-weight: 600 }
```

> [!NOTE]
> A `<details>` keeps the state the visitor gave it through the page's
> renders, marked or not: a render that leaves its `open` as it was leaves
> the group as the visitor set it, and one that changes `open` is the
> server's.

## Prefetching Pages {#prefetch}

A link the browser will load as a page (a `ui.Link` on a page without a
live connection) is fetched ahead when the pointer rests on it for a
moment, when it gets the focus, or when a finger touches it, so it opens
at once.

`prefetch="viewport"` fetches it as soon as it scrolls into view, for the
one link a page is mostly followed by; `prefetch="off"` never does. An
ordinary `<a>` opts in with `data-vk-prefetch` (or
`data-vk-prefetch="viewport"`).

Only same-origin links to another page are fetched, with `GET`: never a
link with `download` or a `target`, another origin, or when the visitor's
browser asks to save data. The browser keeps the response for the
navigation and drops it after 30 seconds.

> [!WARNING]
> The request carries `Sec-Purpose: prefetch`, but it is the page's
> ordinary render, so a page's `GET` must be safe to run without the visitor
> seeing it.

A live page's session doesn't start on a prefetch: what it mounts waits,
parked, for the page to open, and its connection joins then, without
mounting again. Its mount hook runs once, however the page was reached.

A link that moves a live page over its connection isn't fetched: the
server runs its route when it is followed and sends only what changed below
the layouts the pages share.

## Moving the Page from the Server and from Commands {#from-the-server-and-from-commands}

A handler moves the page once its reply is applied. In a shape header,
handlers can call the methods supplied by ui.Live; here `PushNavigate`
returns to the shelf and `PushPatch` moves to the next book:

```html
<script lang="go">
package books

import "github.com/vuka-lang/ui/v2"

type BookActions struct { ui.Live }

func (p *BookActions) Done() { p.PushNavigate("/books") }
func (p *BookActions) Next() { p.PushPatch("/books/8") }
</script>

<nav>
  <button onClick={Done}>Done reading</button>
  <button onClick={Next}>Next book</button>
</nav>
```

In a [JS command](/ui/js) chain, `Patch(href)` and `Navigate(href)` do it
from the browser, after the steps before them. This button adds a class to
itself, then navigates:

```html
<button onClick={ui.JS.AddClass(ui.This, "going").Navigate("/books")}>All books</button>
```

## Moving the Page from an Island {#from-an-island}

A component in an [island](/web/islands) moves the page the same way. On
a [page backed by a component](/web/islands#pages-backed-by-a-component),
`useLive()` has `navigate` and `patch`; in any island, `useVuka()` has
them too. This Vue component navigates, patches and renders `VukaLink`s:

```vue
<script setup lang="ts">
import { useLive, useVuka, VukaLink } from 'vuka:live'
import type { Orders } from 'vuka:live'
const live = useLive<Orders>()
const vuka = useVuka()

async function open(id: number) {
  await live.navigate(`/orders/${id}`)       // another page
}
const filter = (status: string) => live.patch(`/orders?status=${status}`)  // this page, Update
const sort = (by: string) => vuka.patch(`/orders?sort=${by}`, { replace: true })
</script>

<template>
  <nav>
    <VukaLink href="/orders" exact>All</VukaLink>
    <VukaLink href="/orders?status=open" patch exact>Open</VukaLink>
    <VukaLink href="/reports" class="tab">Reports</VukaLink>
  </nav>
</template>
```

### The navigate and patch Functions {#the-navigate-and-patch-functions}

`navigate(href, opts?)` and `patch(href, opts?)` do what a `data-vk-link`
click does: over the connection on a live page, joined. The route and its
gates run, a patch keeps the page mounted for its `Update` (else it mounts
again), the address, scroll and focus follow and `vk:navigate` fires.

Without a connection (a stateless page, one still joining or reconnecting)
or to another origin, it's a page load. `{ replace: true }` takes the
current history entry's place instead of adding one.

In a script outside an island, `window.vuka.navigate(href, {patch,
replace})` is the same call.

### The Returned Promise {#the-returned-promise}

The promise resolves with `{url, patch}` once the move is applied. It
rejects for a URL that isn't `http` or `https` (`javascript:`, `data:`…,
refused as everywhere else), and when another move takes its place
(`AbortError`). A page load leaves the page, so its promise never settles.

### The VukaLink Component {#the-vukalink-component}

`<VukaLink>` renders an `<a href>` marked `data-vk-link` (`patch`:
`="patch"`), so new tabs, modifier clicks and pages without JavaScript
treat it as a link. `replace` takes the current entry's place.

`activeClass` (default `active`) is added while the address is the link's
path or below it (`/orders` for `/orders/7`; `/` only itself). `exact` asks
for the same path, and the same query when the link has one.
`aria-current="page"` marks the link to the current path.

Other attributes and the children pass through (React: `className`). It is
in `vuka:live` for Vue, React and Svelte, typed in `vuka-live.d.ts` for the
frameworks the islands use.

### Islands Follow the Page {#islands-follow-the-page}

A navigate unmounts the old page's islands and mounts the new page's, even
where the new page's island takes the old one's place in the markup. A
patch keeps them, with new props.

## What the Browser Does {#in-the-browser}

A move over the connection keeps the browser's own behaviour in step:

- **The address** changes with `pushState`. Back and forward patch or
  navigate to match: within one page's history entries they patch, across
  pages they navigate (the page you come back to mounts again).
- **Scroll** goes to the top on a navigate, or to the element the URL's
  `#hash` names; a patch leaves it where it is. Back and forward restore
  where the page was.
- **Focus** moves, on a navigate, to the new page's `[autofocus]` element,
  else its `<main>`, else its first component — where a screen reader
  starts reading the new page.
- **`<head>`**: the title follows the new page's, and its head elements the
  document lacks — a stylesheet, a script, a meta tag — are added once.
- **While in flight** `<html data-vk-navigating>` is set — a CSS hook for a
  progress bar or a dimmed page — and the page sends no events. When a
  navigation completes, `vk:navigate` fires on `window` with
  `detail: {url, patch}`.

> [!NOTE]
> Scripts in the new page's body don't run; put a page's scripts in its head.

This rule dims the page's `<main>` while a navigation is in flight:

```css
html[data-vk-navigating] main { opacity: .6; transition: opacity .2s .1s }
```

This listener reports each completed navigation's URL, here to an
analytics script:

```js
addEventListener("vk:navigate", (e) => analytics.page(e.detail.url));
```

## When a Link Loads a Page {#when-it-s-a-page-load}

Whatever can't move over the connection is loaded as usual, so a link
always gets you there:

- a URL whose route doesn't answer with a page — a redirect (the browser
  follows its `Location`), an error page, JSON, a file;
- another origin;
- a page that isn't connected (still joining, or reconnecting);
- a navigation that fails, or gets no reply within 10 seconds, or whose
  connection drops on the way.

A new page drops the [optimistic pushes](/ui/js#optimistic-updates) still
waiting for the old one, without running their hooks. A reconnect later
joins the page the connection moved to: each navigation's reply brings that
page's join token.

### Unsafe URLs Are Refused {#unsafe-urls-are-refused}

Only paths and `http` and `https` URLs are followed. The runtime refuses to
load a `javascript:`, `data:`, `vbscript:` or any other URL, wherever it
comes from: a link, a command, a redirect.

The server never sends a redirect to one. An app route redirecting to
`javascript:` (an open redirect fed a crafted URL) answers the navigation
with an error, and the browser then loads the link's own URL, whose
redirect it refuses. `ui.JS.Navigate` and `Patch` turn such a URL into
`about:invalid#TemplFailedSanitizationURL` when the chain is built.

## Testing Navigation {#testing}

The browser runtime is tested in Chrome by the
[`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live)
app's `TestBrowserNavigation`. Without a browser, ui's
[reference client](/ui/testing#navigation) follows links and the patch and
navigate commands, through a session's router.

The `/books` page of that app is a paged shelf with a page per book: the
pager patches the shelf, a title navigates to the book, the book's next link
patches it, and "Done reading" sends the reader back from the server.
