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
list, a filter, a tab. The components stay mounted and keep their state;
the route runs again for the new URL, so a [Live page](/web/live)'s props
are bound from it again, and the reply is the usual patch — only what
changed.

When the component has to react to the new URL — load the next rows, say —
give it an `Update` overload taking a struct that embeds `web.URLParams`.
Its fields bind from the new URL the way a Live page's props do (the path
segment, else the query parameter, of each field's name), and `Update` runs
before the patch renders:

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

`Mount` sees the first URL; `Update` sees each patch. An error `Update`
returns reaches the page as a handler's does. Params that don't bind (a
`page=x`) make the route answer 400, and the browser loads that URL as
an ordinary page.

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
