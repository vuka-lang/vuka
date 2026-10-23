# Live components

A [stateful shape](/ui/stateful) keeps its state on the server and answers its
elements' events over a WebSocket; the page changes in place, with no
JavaScript to write. [ui](/ui/) gives the component model, the
[session](/ui/live) and the [protocol](/reference/live-protocol); web gives
the transport, the browser runtime and the routes. Props, state, events,
messages and change tracking are on [Stateful components](/ui/stateful).

Save the counter as `pages/counter.vuka.html`:

```html
package pages

import (
    "context"
    "github.com/vuka-lang/ui/v2"
)

type Counter struct {
    Start int
    n int
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Increment() { c.n++ }
<div><output>{n}</output><button onClick={Increment}>+</button></div>
```

## How a page becomes live

A shape becomes live when it embeds ui.Live, declares a decorated hook or
wires a server method in an event attribute. A static page containing a live
child participates in the live transport as well. Browser-only JS commands
and ui.State assignments do not create a server session by themselves.

Pages live under pages/, generated route.go registers them, and main passes
`pages.Routes()` to `web.New`. Exported props bind path and query values;
unexported provided dependencies are injected and other unexported fields are
tracked server state. A page's methods live in its header or in a `.vuka` file
beside it, which reads and writes that state the same way
([where its methods live](/ui/stateful#where-its-methods-live)). Folder
`_layout.vuka.html` files supply the document and nested layouts, with
middleware and guards from the same tree.

Once mounted, a routed page owns its fields: values changed by handlers, hooks,
updates and bound fields stay changed. A URL patch rebinds only URL fields
before its update handler. A child component's props instead come from its
parent on each render. See [field ownership](/ui/stateful#whose-fields).

The first, HTTP render is complete HTML, and before `</body>` goes
`<script src="/_vuka/live.js" data-vk-token="…" defer>`. The script opens
`/_vuka/live?t=<token>` and sends `join`; the server runs the same route
again — a GET to the token's URI, with the WebSocket request's headers
(cookies, auth), through the app's and the route's middleware, with its
dependencies — and the page it renders becomes the connection's session.
A join the route answers with a redirect sends `redirect`, and the
browser follows it; any other non-page answer (a 404, JSON) is an `error`,
and the connection closes.

**The mount hook runs once per view.** The GET mounted the page's components
already, so the join adopts that session — the mounted instances, their
state and their subscriptions — when it arrives within the adoption window
(`web.LiveAdopt`, 10 seconds by default) and as the same identity: the same
signed-in user, or anonymous both times. The mount hook, and whatever it loads,
doesn't run again. The join still runs the route either way — middleware,
gates and the handler, with the connecting browser's own cookies — so a
sign-in ended between the render and the join is refused all the same;
adoption only skips mounting again. When the window has run out, the token
was already used (each render's token adopts at most once), another
identity joins, the server restarted, or anything else is in doubt, the
join mounts fresh, exactly as a reconnect does: adoption is an
optimization, and no page may depend on it. `web.LiveAdopt(0)` turns it
off.

The route constructs the connection's page, which is rendered again after
every event. State belongs in its components, not transient route logic.

### Side effects in a live page's handler

One visit runs the page's route twice — the GET, then the join — and again
on each reconnect; the join adopting the GET's session skips the mount hook, never
the route. Side effects in request-time constructors or layout loading can therefore
happen more than once. A mount hook may also run again when adoption is not
possible; it is not a durable exactly-once transaction.
`web.IsLiveJoin(ctx)` is true only on the join's run, so the side effect
happens once:

A shape's mount hook can distinguish an initial view from a fresh join.
Keep Stats and NewStats in a services logic package and register the constructor
with app.Service before starting the app. In `pages/welcome.vuka.html`:

```html
package pages

import (
    "context"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Welcome struct{}

@ui.Mount
func (p *Welcome) mount(ctx context.Context, stats *services.Stats) error {
    if !web.IsLiveJoin(ctx) { stats.Visit("/welcome") }
    return nil
}
<main><h1>Welcome</h1></main>
```


| | `IsLiveJoin` |
|---|---|
| the GET | false |
| the join, a reconnect's join | true (whether or not it adopts the GET's session) |
| a [navigation](/web/navigation) over the connection | false: it replaces an HTTP load, a visit of its own |
| the page's events and renders after the join | false |

Web marks its own run in the context, so a client can't fake it: a request
sending `X-Vuka-Live: join` is an ordinary GET. Side effects the browser
triggers belong in event handlers, which run once per event.

Moving between pages — a list and its rows' pages, the next page of a
list — happens over the same connection with `ui.Link`; see
[Navigation](/web/navigation).


## Events

`onClick={c.Inc}` renders `data-vk-on-click="c1:0"`. The runtime listens on
the document for each event type a page names, and sends the nearest
handler's id, the element's value (a checkbox's `"true"`/`"false"`), the key
of a keyboard event, and a submitted form's fields. What a handler receives
follows its type — nothing, the value, `ui.Event`, `url.Values`, a struct
bound from the form; see [Events](/ui/stateful#events).

The payload type belongs in a logic file:

```go
// models/new_item.go
package models

type NewItem struct { Text string `form:"text" validate:"required"` }
```

The page declares just its rendering struct, in `pages/todo.vuka.html`:

```html
package pages

import (
    "strings"
    "example.com/app/models"
    "github.com/vuka-lang/web/v2"
)

type Todo struct {
    items []string
    filter string
}

func (t *Todo) Add(f models.NewItem) error {
    text := strings.TrimSpace(f.Text)
    if text == "" { return web.BadRequest("write something first") }
    t.items = append(t.items, text)
    return nil
}

func (t *Todo) Filter(q string) { t.filter = q }
<section>
  <form onSubmit={Add}>
    <input name="text" autocomplete="off" />
    <button>Add</button>
  </form>
  <input type="search" value={filter} onInput={Filter} data-vk-debounce="150" />
</section>
```


Submits are `preventDefault`ed; so are clicks on links and on buttons of a
form without a submit handler. A form whose submit handler succeeds is reset
to what the server rendered; one whose handler returns an error keeps what
the user typed, and the error reaches the page as `vk:error`. The element of
a click or submit carries `aria-busy="true"` until the reply.

### Errors

What the browser reads of an error follows what an HTTP route answers:

- a `*web.Error`'s message as it is — `web.BadRequest("write something
  first")`, `web.Status(409, "that name is taken")`, `web.Forbidden` — and a
  form field that doesn't bind (`"x" isn't a whole number`);
- an event a [gate](/web/auth) refuses: `unauthorized` or `forbidden`, the
  gates' own `*web.Error`s;
- for any other error a handler, the mount hook or `Update` returns, or a panic,
  `Internal Server Error`. The error itself goes to the log (`slog`, at
  error level), with the page's URL and the handler, so a database error
  or a file path never reaches the visitor.

In development (`web.Dev()`, `VUKA_ENV=development`) the browser gets every
error's own text, as an HTTP error page shows it.

## Patches

The reply carries the HTML of each component whose output changed (the page
itself is `c0`). The runtime morphs the element in place: elements are
matched by `id`, else `data-vk-id`, else position and tag. A field takes
the server's value when it changed; a focused one only while it shows what
it showed when the page last sent an event (a form the server clears once
sent), else it keeps what the user typed since, and its caret. Give list rows an `id` for stable identity across inserts and
removals. A whole-document `c0` patch morphs the `<body>` and sets the title.

## Browser attributes and events

| | |
|---|---|
| `data-vk-debounce="150"` | send the element's events (not submits) 150ms after the last one |
| `data-vk-key="Enter Escape"` | send a keyboard event only for these keys |
| `data-vk-ignore` (with an `id`) | the morph leaves the element as the browser has it |
| `<html data-vk-state>` | `connecting`, `connected`, `disconnected`, `idle` or `closed`: style a banner on it |
| `vk:patched` | a document event after each render or patch; `detail.ids` |
| `vk:error` | a document event for a handler's error or a failed message; `detail.message` (also logged to the console) |

```css
html[data-vk-state=disconnected] body::before { content: "reconnecting…"; position: fixed; top: 0; right: 0 }
[aria-busy=true] { opacity: .6 }
```

## Broadcasts

`web.Broadcast(ctx, topic, msg)` — from an event handler, an `Update`, or any
request — delivers `msg` to every component subscribed to `topic`
(`c.Subscribe(topic)` in the mount hook), in every connected page. Each takes it in
the [`Update`](/ui/stateful#messages) overload of its type, and its
page gets the patch:

Message and form DTOs belong in a logic package:

```go
// models/chat.go
package models

type Said struct { Who, Text string }
type Joined struct { Who string }
type ChatForm struct { Text string `form:"text" validate:"required"` }
```

Save the shape as `pages/chat/[room].vuka.html`:

```html
package chat

import (
    "context"
    "example.com/app/models"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Chat struct {
    ui.Live
    Room string
    lines []string
    name string
}

@ui.Mount
func (c *Chat) mount(ctx context.Context) error {
    c.Subscribe("chat:" + c.Room)
    return nil
}

func (c *Chat) Update(m models.Said) { c.lines = append(c.lines, m.Who+": "+m.Text) }
func (c *Chat) Update(j models.Joined) { c.lines = append(c.lines, j.Who+" joined") }

func (c *Chat) Send(ctx context.Context, f models.ChatForm) error {
    return web.Broadcast(ctx, "chat:"+c.Room, models.Said{Who: c.name, Text: f.Text})
}
<section>
  for _, line := range lines {
    <p>{line}</p>
  }
  <form onSubmit={Send}><input name="text" /><button>Send</button></form>
</section>
```


A topic of `""` reaches every component. `app.Broadcast(ctx, topic, msg)`
does the same from outside a request (a worker, a timer). Delivery is
in-process; `web.WithRelay(r)` sends broadcasts through a `web.Relay` to
reach every replica:

```go
type Relay interface {
	Publish(ctx context.Context, topic string, msg any) error
	Subscribe(deliver func(topic string, msg any))
}
```

Each replica's `deliver` is called with what arrives, its own messages
included; encoding the messages, whose dynamic types pick the `Update`
methods they reach, is the relay's business.

## Streams

A stream of application records can update a connected page through typed
broadcast messages and tracked server fields. Keep the record's stable ID as
the key in its markup loop so inserts, deletes and reordering retain the
corresponding browser/component instances. Replacing the whole slice does
not require replacing every keyed row's DOM.

Load the initial snapshot in a decorated mount hook, then apply incoming
messages in Update methods. Define whether a message inserts, replaces or
removes a record in application logic. Bound the retained collection for long
feeds, handle duplicate/out-of-order deliveries according to the service's
contract, and reload a snapshot after reconnect: connection-local memory is
not a durable stream or replay log. Message DTOs and shared storage remain
logic types rather than extra rendering structs in the shape.

## Presence

Presence is membership associated with a connection or mounted instance, not
an inference from the number of initial HTTP renders. Subscribe only after
authorizing the room/resource; publish application membership updates through
typed messages and render the membership snapshot from tracked state.

The initial GET and join may adopt one mount or create fresh mounts, and a
browser can open several tabs. Use the session/instance lifecycle and optional
Unmount cleanup to release membership; define in the shared service whether
several connections count as one visitor or several. Reconnect and abnormal
disconnect require expiry/refresh behavior in that service. Across replicas,
coordinate presence and messages through the application's shared store/relay.
The transport's serialized event dispatch does not make external service data
thread-safe.

## Uploads

Upload files through an HTTP multipart action, not by assuming a live event's
JSON/form-value payload contains file bytes. Keep file handling in a typed
HTTP handler (a page's Post method or an endpoint under pages/api) and receive
`*http.Request` by type. For example, `pages/upload.vuka.html`:

```html
package pages

import (
    "context"
    "net/http"
    "example.com/app/services"
    "github.com/vuka-lang/web/v2"
)

type Upload struct{}

func (p *Upload) Post(ctx context.Context, r *http.Request, files *services.Files) (web.Redirect, error) {
    file, header, err := r.FormFile("attachment")
    if err != nil { return "", web.BadRequest("choose an attachment") }
    defer file.Close()
    if err := files.Save(ctx, header.Filename, file); err != nil { return "", err }
    return web.Redirect("/uploads"), nil
}
<form method="post" enctype="multipart/form-data">
  <input type="file" name="attachment" />
  <button>Upload</button>
</form>
```

Files is an application service, registered with app.Service; its Save method
must validate the file and choose a safe storage name rather than trust the
submitted filename. Complete copying before request-scoped multipart temporary
files are released. Apply the normal authentication, CSRF and resource checks.
`web.MaxRequestBody(n)` bounds requests (32 MiB by default); an over-limit body
answers 413. Multipart parsing can spill to temporary files, so a memory
threshold is not a total upload-size limit. A streaming HTTP response likewise
belongs in an endpoint with a typed response writer; it is not a live render
patch and must respect cancellation and transport flush behavior.

## Connections and reconnects

One session per connection; its events and broadcasts are handled one at a
time, in order, by the connection's goroutine. The server pings every 25
seconds; a closed connection unmounts its components. `app.Stop` (and
`Run`'s shutdown) closes every connection as "going away".

The runtime reconnects with jittered exponential backoff (up to about 10s)
and joins again — **the route runs again and the state starts over**; what
wasn't saved elsewhere is lost, and events sent while disconnected are
dropped.

### Limits on connections

Each is an option with a default; 0 lifts it.

| Option | |
|---|---|
| `web.LiveJoinTimeout(d)` | how long a socket may stay open without joining its page; 10s. A socket sending nothing, or anything but `join`, is closed (1008) |
| `web.LiveIdleTimeout(d)` | how long a joined page may send nothing before its connection closes (4002) and its session, with its components' state, is dropped; 30m. Broadcasts don't count. The runtime joins again at once if the page is in view, else when it is shown |
| `web.LiveMaxConns(n)` | the app's open live sockets; 10,000. One beyond is refused with 503, and the runtime retries |
| `web.LiveMaxConnsPerIP(n)` | the sockets from one client address (`web.ClientIP`, so through [trusted proxies](/web/deployment#behind-a-proxy)); 100. One beyond is refused with 429 |
| `web.LiveMaxConnsPerPage(n)` | the sockets opened with one page token; 4. One beyond is refused with 429 |
| `web.LiveCheckEvery(d)` | how often the app's `LiveCheck`s run on each joined page; 30s; 0 for never |
| `web.LiveAdopt(d)` | how long after a page's GET its join may adopt the session the render mounted, so the mount hook runs once per view; 10s. At most 1024 pages wait at a time, oldest dropped first; `web.LiveAdopt(0)` mounts fresh on every join |

A page is checked when it mounts: its join and each navigation run its
route through the app's middleware and gates, as a page load does. Its
events and broadcasts trust what it mounted with. A `LiveCheck` failing — a
session ended, a permission revoked — closes the page with 4001, and it
reloads, its route answering for the browser; it catches what changes while
a page sits open without the app hearing of it. A library that ends a
session itself closes its pages at once with `app.CloseLive(match)`, which
closes every joined page whose context `match` picks.

## Security

The token is the page's URI and an expiry, HMAC-SHA256-signed with the app's
key: a connection can only rebuild a page the app rendered, and it does so
with the connecting browser's own cookies, through the route's own
middleware.

| Option | |
|---|---|
| `web.LiveKey(key)` | the signing key; each app draws a random one, so replicas behind one hostname need a shared key |
| `web.LiveTokenTTL(d)` | how long a rendered page may (re)join; 12h by default |
| `web.LiveOrigins(patterns…)` | other origins allowed to open live connections (`"*.example.com"`); by default only the page's own |

Each render's token also carries a nonce: the one-shot key under which the
render's session waits for its join (`web.LiveAdopt`). A join adopts only
the session its own token's render parked, for the same URI, and only as
the identity that rendered it — a library that signs visitors in registers
the comparison with `web.LiveIdentity`, and anything uncertain mounts
fresh.

A bad or expired token closes the connection with code 4001, and the runtime
reloads the page (at most once in 10 seconds) for a fresh one. A library
ending sign-ins ([auth](/web/auth#live-pages)) closes a page the same way:
at once when it ends the sign-in itself (`app.CloseLive`), else at its next
`LiveCheckEvery` check (30 seconds).

**Checks in the mount hook and `Update`.** A page that checks in the mount hook whether
the visitor may see what its URL names (order `{id}`) makes the same check
in its `Update` taking `web.URLParams` , if it has one: a patch keeps the
page mounted and hands the new URL to that `Update`, not to the mount hook. A page
without such an `Update` mounts again on every patch, so its mount hook sees
each URL; the browser's asking for a patch doesn't change that. See
[Navigation](/web/navigation#the-server-decides-what-stays-mounted).

## The wire

Vuka's [wire protocol](/reference/live-protocol) as is, plus:

| | |
|---|---|
| `GET /_vuka/live?t=<token>` | the WebSocket; the token is in the URL, so a bad one is refused before any message |
| `GET /_vuka/live.js?v=<hash>` | the runtime, immutable for its hash (ETag otherwise) |
| `join` | runs the route; a second `join` on the same connection starts over |
| `redirect` | sent for a join the route redirects; the connection then closes normally (1000). For a `navigate` whose route doesn't answer with a page: its `Location`, else the URL. Never to a URL that isn't a path or `http`/`https`: an `error` instead |
| `navigate` | runs the URL's route (`X-Vuka-Live: navigate`) and makes its page the session's; the reply carries `url` and `token`, the new page's join token, which a reconnect uses. `patch: true` asks to keep the page mounted, which the server does only for an `Update` taking `web.URLParams` ([Navigation](/web/navigation#the-server-decides-what-stays-mounted)); a mount hook, `Update` or render failing answers `error`, nothing staying mounted |
| close 4001 | invalid or expired token, or a `LiveCheck` failed: reload |
| close 4002 | the page sat idle (`LiveIdleTimeout`): join again once the page is in view |
| close 1008 | no `join` within `LiveJoinTimeout` |
| close 4004 | the route isn't a live page: the error message precedes it; no retry |
| close 1001 | the server is stopping: reconnect |
| request header `X-Vuka-Live: join` | set on the route's run for a join; `navigate` on a navigation's. A client can send it too: test `web.IsLiveJoin(ctx)`, not the header |

A broadcast arriving before `join` is dropped (the join renders fresh).

## Limits

- Patches are whole components' HTML, morphed in the browser; state starts
  over on reconnect.
- The live endpoints are at `/_vuka/…` from the root: an app mounted under a
  path prefix needs them routed there.
- Event handlers get the join's context: `web.Request` is the join's
  request, and writing to `web.ResponseWriter` goes nowhere.

The [`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live)
app has a counter page, a counter inside an ordinary page, a todo list, a
chat whose tabs see each other's messages, and a paged shelf of books with a
page per book; its menu's links move between them on one connection.
