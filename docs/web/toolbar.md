# Development toolbar

Why is this page slow? Which statements did it run, and did one of them run
once per row? What did that click do on the server? Did my last save
compile? In development every page you open answers these on the spot,
without a profiler, log grep or terminal: a handle on the page's right edge
shows the request's time and statement count, and clicking it opens a
drawer of panels for that request.

![The SQL panel naming an N+1](/toolbar-sql.png)

## Panels

**Request.** The method, path and status; the route's pattern
(`GET /pets/{id}`); the handler that answered, with its file and line; the
arguments it was called with and where each came from — path, query, body
or a dependency; and the request's and response's headers, with cookies and
credentials hidden.

**SQL.** Every statement the request ran, in order, with its arguments,
rows and time as a bar. A statement that ran more than once is amber. One
that ran three times or more with different arguments is an N+1 — one
statement per row of an earlier result — and the panel says so, with the
statement: read those rows in one query instead
([`SelectRelated` or `PrefetchRelated`](/orm/queries#relations) with the
ORM). A statement that failed, or that the ORM refused before sending it, is
red with its error.

**Timeline.** Where the time went, as bars against the request: the app's
and the route's middleware, each call decorator (`@timed`, `@cached`), the
handler, the render, and the statements, nested as they ran. A decorated
function called while the page renders shows inside the render — without a
`context.Context` parameter too.

**Logs.** What `slog`'s default logger wrote for the request: records
written with its context (`slog.InfoContext(ctx, …)`), and those written
from the request's goroutine. They still go to the console as before.

**Live.** On a [live page](/web/live), each message its connection carried,
the latest first: the join, each event named by its handler
(`Counter.Inc (click)`), each navigation and patch, each broadcast it took.
Each shows its time, statements and reply size; open one for its SQL,
timeline and logs. A message that failed is red, and so are failures only
the browser saw: a push that got no reply in time, a connection that
dropped under it.

![A live page's messages](/toolbar-live.png)

## Every call the page makes

The list at the top of the drawer holds the page and each `fetch` and XHR
call it made since it loaded (`POST /api/pets → 201`); pick one to see its
panels. Every response of a development app carries the id of its record in
an `X-Vuka-Toolbar` header, which is how the page finds them — and how you
can find a request's record from the network tab.

## While vuka run -watch rebuilds

With [`vuka run -watch`](/tools/cli#run-watch) the old build keeps answering
while the next one compiles, so the page in front of you can be older than
your code. The handle says so: `building… 1.2s` while the next build
compiles, and `build failed` when it didn't — the drawer then shows the
compiler's output above the panels, `.vuka` file and line included. The
build that keeps answering is the last good one, until a save compiles.

## Your own panels

Request code records values for a panel with `web.Note`, and
`web.AddPanel` draws them:

```vuka
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	v, ok := c.m[key]
	web.Note(ctx, "Cache", Lookup{key, ok})
	return v, ok
}

func init() {
	web.AddPanel("Cache", func(notes []any) ui.Node {
		return <ul>{for _, n := range notes {
			<li>{n.(Lookup).Key}: {n.(Lookup).Hit}</li>
		}}</ul>
	})
}
```

A panel noted without `AddPanel` lists its values as they are. `web.Note`
does nothing outside development, so it can stay in production code. A
panel whose render panics shows the panic in its place.

**Another database layer.** The [ORM](/orm/)'s statements reach the SQL
panel through `ormweb` by themselves. Another library reports its own with
`web.NoteStatement(ctx, web.Statement{SQL, Args, DB, Start, Duration, Rows,
Err})`, and `app.Toolbar()` says whether the app shows the toolbar at all.

## When it is on

| | toolbar |
|---|---|
| `vuka run`, `vuka run -watch` | on |
| `web.Dev()`, `VUKA_ENV=development` | on |
| `web.NoToolbar()` | off |
| a test binary | off, so tests see the pages the app serves |
| any other app — `vuka build` for production | off |

When it is off nothing is recorded, no route or header is added, and
nothing hooks into the program; a request costs what it did without it.
When it is on, recording adds a few microseconds to a request.

## What it changes on the page

Only one script tag in the page's `<head>`, beside the stylesheet's link;
the page's markup is otherwise the one the app renders, and live updates
never carry the toolbar. The toolbar draws itself in a shadow DOM, so your
Tailwind classes and its styles never meet, and it follows the system's
light or dark scheme. Pages that aren't whole documents — fragments, JSON,
answers to a form's POST, pages fetched by a script — get no tag.

The app keeps its latest 100 requests (and 200 messages per live page) in
memory, and the toolbar fetches them from `/_vuka/toolbar/`. A record that
has dropped out says so.
