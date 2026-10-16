# Navigation

A list you page through, tabs whose choice belongs in the URL, a list and
the detail page of each row: moving between them should feel instant. A
live page can move to another URL over the connection it already has —
no document reload, no new WebSocket, no layout sent again — while the
address bar, the back button and links opened in a new tab keep working
as they do for any site.

```vuka
<ui.Link href="/books/7">Dune</ui.Link>                    // navigate: another page
<ui.Link href={shelfURL(s.page + 1)} patch>Next</ui.Link>  // patch: this page, other parameters
```

## Links

`ui.Link` renders an ordinary `<a href>`, marked for the runtime
(`data-vk-link`, or `data-vk-link="patch"`). So it stays a plain link
wherever the runtime doesn't take it: without JavaScript, on a page that
isn't live, with a `target` (`_blank`), clicked with a modifier key or the
middle button (a new tab or window), or to another origin.

| Attribute | |
|---|---|
| `href` | where to go |
| `patch` | the same page with other parameters (below) |
| `id`, `class`, `title`, `target`, `rel` | as on `<a>` |
| `attrs` | any other attributes, as `[]ui.Attr` |

A link to the page's own URL with only a `#hash` is the browser's: it
scrolls, nothing is sent.

## Patch or navigate

**Patch** moves to another URL of the page you are on: the next page of a
list, a filter, a tab. The route runs again for the new URL, and a [Live
page](/web/live) that takes the URL stays mounted and keeps its state: its
props are bound from the URL again, and the reply is the usual patch — only
what changed.

A Live page takes the URL with an `Update` overload taking a struct that
embeds `web.URLParams`. Its fields bind from the new URL the way a Live
page's props do (the path segment, else the query parameter, of each
field's name), and `Update` runs before the patch renders:

```vuka
@web.Live("/books")
type Shelf struct {
	ui.Live
	Page int // ?page=, the first URL
	rows []Book
}

type ShelfParams struct {
	web.URLParams
	Page int // ?page=
}

func (s *Shelf) Mount()               { s.show(s.Page) }
func (s *Shelf) Update(p ShelfParams) { s.show(p.Page) }

func (s *Shelf) Render() ui.Node {
	return <section>
		<ol>{for _, b := range s.rows { <li key={b.ID}><ui.Link href={bookURL(b.ID)}>{b.Title}</ui.Link></li> }}</ol>
		<ui.Link href={shelfURL(s.page + 1)} patch>Next</ui.Link>
	</section>
}
```

`Mount` sees the first URL; `Update` sees each patch. Params that don't
bind (a `page=x`) make the route answer 400, and the browser loads that URL
as an ordinary page.

### The server decides what stays mounted

`patch` on a link (and `Patch`, `PushPatch`) only asks. The page stays
mounted only when its type has an `Update` taking `web.URLParams`; any other
page **mounts again**, a fresh instance whose `Mount` sees the new URL, even
on the same route — as a navigate does. A browser can send any URL as a
patch, so this is what keeps a check in `Mount` from being skipped.

It follows that **`Update` must repeat every check `Mount` makes** of what
the URL names. A page showing one order checks in `Mount` that the visitor
may see it; its `Update` checks the new id the same way:

```vuka
@web.Live("/orders/{id}")
type OrderPage struct {
	ui.Live
	ID     int
	Orders *Orders
	me     *User
	order  Order
}

type OrderParams struct {
	web.URLParams
	ID int
}

func (p *OrderPage) Mount(ctx context.Context) error { return p.load(ctx, p.ID) }
func (p *OrderPage) Update(ctx context.Context, q OrderParams) error { return p.load(ctx, q.ID) }

func (p *OrderPage) load(ctx context.Context, id int) error {
	o, err := p.Orders.Get(ctx, id)
	if err != nil {
		return err
	}
	if o.Owner != auth.User[*User](ctx).ID {
		return web.Forbidden
	}
	p.order = o
	return nil
}
```

An error `Update` returns fails the patch: nothing of the page stays
mounted to take events, and the browser loads the URL, so the route answers
as for any request (here, `Mount` refusing with 403). The same goes for a
`Mount` that refuses on a navigate.

**Navigate** moves to another page. Its route runs as a page load does —
middleware, gates, dependencies, its layout — its components mount on the
same connection, and the old page's unmount. With [protocol
v2](/reference/live-protocol#protocol-v2-render-trees) the new page
arrives as a change to the trees the connection already holds: the
layout's markup isn't sent again, only the parts that differ.

## From the server and from commands

A handler moves the page once its reply is applied:

```vuka
func (p *BookPage) Done() { p.PushNavigate(shelfURL(p.shelfPage())) }
func (p *BookPage) Random() { p.PushPatch(bookURL(rand.IntN(len(books)) + 1)) }
```

In a [JS command](/ui/js) chain, `Patch(href)` and `Navigate(href)` do it
from the browser, after the steps before them:

```vuka
<button onClick={ui.JS.AddClass(ui.This, "going").Navigate("/books")}>All books</button>
```

## From an island

A component in an [island](/web/islands) moves the page the same way. On
a [page backed by a component](/web/islands#pages-backed-by-a-component),
`useLive()` has `navigate` and `patch`; in any island, `useVuka()` has
them too:

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

- **`navigate(href, opts?)` and `patch(href, opts?)`** do what a
  `data-vk-link` click does: over the connection on a live page, joined —
  the route and its gates run, a patch keeps the page mounted for its
  `Update` (else it mounts again), the address, scroll and focus follow
  and `vk:navigate` fires. Without a connection — a stateless page, one
  still joining or reconnecting — or to another origin, it's a page load.
  `{ replace: true }` takes the current history entry's place instead of
  adding one.
- **The promise** resolves with `{url, patch}` once the move is applied. It
  rejects for a URL that isn't `http` or `https` (`javascript:`, `data:`…,
  refused as everywhere else), and when another move takes its place
  (`AbortError`). A page load leaves the page, so its promise never
  settles.
- **`<VukaLink>`** renders an `<a href>` marked `data-vk-link` (`patch`:
  `="patch"`), so new tabs, modifier clicks and pages without JavaScript
  treat it as a link. `replace` takes the current entry's place.
  `activeClass` (default `active`) is added while the address is the
  link's path or below it (`/orders` for `/orders/7`; `/` only itself);
  `exact` asks for the same path, and the same query when the link has
  one. `aria-current="page"` marks the link to the current path. Other
  attributes and the children pass through (React: `className`). It is in
  `vuka:live` for Vue, React and Svelte, typed in `vuka-live.d.ts` for the
  frameworks the islands use.
- **Islands follow the page**: a navigate unmounts the old page's islands
  and mounts the new page's — even where the new page's island takes the
  old one's place in the markup; a patch keeps them, with new props.
- In a script outside an island, `window.vuka.navigate(href, {patch,
  replace})` is the same call.

## In the browser

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
  Scripts in the new page's body don't run; put a page's scripts in its head.
- **While in flight** `<html data-vk-navigating>` is set — a CSS hook for a
  progress bar or a dimmed page — and the page sends no events. When a
  navigation completes, `vk:navigate` fires on `window` with
  `detail: {url, patch}`.

```css
html[data-vk-navigating] main { opacity: .6; transition: opacity .2s .1s }
```

```js
addEventListener("vk:navigate", (e) => analytics.page(e.detail.url));
```

## When it's a page load

Whatever can't move over the connection is loaded as usual, so a link
always gets you there:

- a URL whose route doesn't answer with a page — a redirect (the browser
  follows its `Location`), an error page, JSON, a file;
- another origin;
- a page that isn't connected (still joining, or reconnecting);
- a navigation that fails, or gets no reply within 10 seconds, or whose
  connection drops on the way.

Only paths and `http` and `https` URLs are followed. The runtime refuses to
load a `javascript:`, `data:`, `vbscript:` or any other URL, wherever it
comes from — a link, a command, a redirect — and the server never sends a
redirect to one: an app route redirecting to `javascript:` (an open
redirect fed a crafted URL) answers the navigation with an error, and the
browser then loads the link's own URL, whose redirect it refuses.
`ui.JS.Navigate` and `Patch` turn such a URL into
`about:invalid#TemplFailedSanitizationURL` when the chain is built.

A new page drops the [optimistic pushes](/ui/js#optimistic-updates) still
waiting for the old one, without running their hooks. A reconnect later
joins the page the connection moved to: each navigation's reply brings that
page's join token.

## Testing

The browser runtime is tested in Chrome by the
[`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live)
app's `TestBrowserNavigation`. Without a browser, ui's [reference
client](/ui/testing#navigation) follows links and the patch and navigate
commands, through a session's router.

The `/books` page of that app is a paged shelf with a page per book: the
pager patches the shelf, a title navigates to the book, the book's next link
patches it, and "Done reading" sends the reader back from the server.
