# Live components

A [stateful component](/ui/stateful) — a struct
embedding `ui.Live` — keeps its state on the server and answers its
elements' events over a WebSocket; the page changes in place, with no
JavaScript to write. [ui](/ui/) gives the component model, the
[session](/ui/live) and the [protocol](/reference/live-protocol); web gives
the transport, the browser runtime and the routes. Props, state, events,
messages and `ui.Assign` are on [Stateful components](/ui/stateful).

```vuka
@web.Live("/counter")
type Counter struct {
	ui.Live
	Start int // ?start=3
	n     int
}

func (c *Counter) Mount() { c.n = c.Start }
func (c *Counter) Inc()   { c.n++ }

func (c *Counter) Render() ui.Node {
	return <div><output>{c.n}</output><button onClick={c.Inc}>+</button></div>
}
```

## How a page becomes live

Two ways, one mechanism:

- **`@web.Live(path, opts…)`** on a stateful component serves it as a GET
  page. Its exported fields of provided types are injected; the others that
  hold text bind from the path segment or query parameter of their name
  (`form` tag, else the field name, ignoring case). The page is the component
  inside a layout — `func(page ui.Node) ui.Node`:

  | | |
  |---|---|
  | `web.Layout(f)` | this page's layout (an option of `@web.Live`) |
  | `web.LiveLayout(f)` | the app's, for every live page without its own |
  | neither | a bare HTML document |
  | `web.Use(mw…)` | middleware for this page (an option of `@web.Live`) |

  From Go: `app.Live("/counter", &Counter{Start: 1})`, the value's fields
  being the defaults.

- **Any GET route** whose page — a `ui.Node` result or a templ view —
  renders a stateful component or an event handler is live by itself:

  ```vuka
  @web.Get("/")
  func Home() ui.Node { return <Shell title="Home"><Counter Start={10} /></Shell> }
  ```

  A Vuka component rendered from a templ view is live too (templ passes the
  context on). `html/template` views and JSON are never live.

The first, HTTP render is complete HTML, and before `</body>` goes
`<script src="/_vuka/live.js" data-vk-token="…" defer>`. The script opens
`/_vuka/live?t=<token>` and sends `join`; the server runs the same route
again — a GET to the token's URI, with the WebSocket request's headers
(cookies, auth), through the app's and the route's middleware, with its
dependencies — and the page it renders becomes the connection's session.
A join the route answers with a redirect sends `redirect`, and the
browser follows it; any other non-page answer (a 404, JSON) is an `error`,
and the connection closes.

**`Mount` runs once per view.** The GET mounted the page's components
already, so the join adopts that session — the mounted instances, their
state and their subscriptions — when it arrives within the adoption window
(`web.LiveAdopt`, 10 seconds by default) and as the same identity: the same
signed-in user, or anonymous both times. `Mount`, and whatever it loads,
doesn't run again. The join still runs the route either way — middleware,
gates and the handler, with the connecting browser's own cookies — so a
sign-in ended between the render and the join is refused all the same;
adoption only skips mounting again. When the window has run out, the token
was already used (each render's token adopts at most once), another
identity joins, the server restarted, or anything else is in doubt, the
join mounts fresh, exactly as a reconnect does: adoption is an
optimization, and no page may depend on it. `web.LiveAdopt(0)` turns it
off.

The route's handler runs once per connection: its result is the page, and
the page is rendered again after every event. State belongs in the
components.

### Side effects in a live page's handler

One visit runs the page's route twice — the GET, then the join — and again
on each reconnect; the join adopting the GET's session skips `Mount`, never
the route. So what the handler does besides building the page, sending a
welcome email or counting a visit, would happen each time.
`web.IsLiveJoin(ctx)` is true only on the join's run, so the side effect
happens once:

```vuka
@web.Get("/welcome")
func Welcome(ctx context.Context, m *mail.Mailer, stats *Stats) (ui.Node, error) {
	if !web.IsLiveJoin(ctx) {
		stats.Visit("/welcome")
		if err := m.Send(ctx, welcomeMail); err != nil {
			return nil, err
		}
	}
	return <WelcomePage />, nil
}
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

```vuka
type NewItem struct {
	Text string `form:"text"`
}

func (t *Todo) Add(f NewItem) error {
	text := strings.TrimSpace(f.Text)
	if text == "" {
		return web.BadRequest("write something first")
	}
	t.next++
	t.items = append(t.items, Item{ID: t.next, Text: text})
	return nil
}

func (t *Todo) Filter(q string) { t.filter = q }

func (t *Todo) Render() ui.Node {
	return <section>
		<form onSubmit={t.Add}>
			<input name="text" autocomplete="off" />
			<button>Add</button>
		</form>
		<input type="search" value={t.filter} onInput={t.Filter} data-vk-debounce="150" />
	</section>
}
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
- for any other error a handler, `Mount` or `Update` returns, or a panic,
  `Internal Server Error`. The error itself goes to the log (`slog`, at
  error level), with the page's URL and the handler, so a database error
  or a file path never reaches the visitor.

In development (`web.Dev()`, `VUKA_ENV=development`) the browser gets every
error's own text, as an HTTP error page shows it.

## Patches

The reply carries the HTML of each component whose output changed (the page
itself is `c0`). The runtime morphs the element in place: elements are
matched by `id`, else `data-vk-id`, else position and tag; a focused field
keeps its value and caret; another field takes the server's value when it
changed. Give list rows an `id` for stable identity across inserts and
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
(`c.Subscribe(topic)` in `Mount`), in every connected page. Each takes it in
the [`Update`](/ui/stateful#messages) overload of its type, and its
page gets the patch:

```vuka
type Said struct{ Who, Text string }
type Joined struct{ Who string }

@web.Live("/chat/{room}")
type Chat struct {
	ui.Live
	Room  string   // the path segment
	Log   *ChatLog // injected
	lines []string
	name  string
}

func (c *Chat) Mount()          { c.Subscribe("chat:" + c.Room) }
func (c *Chat) Update(m Said)   { c.lines = append(c.lines, m.Who+": "+m.Text) }
func (c *Chat) Update(j Joined) { c.lines = append(c.lines, j.Who+" joined") }

func (c *Chat) Send(ctx context.Context, f ChatForm) error {
	return web.Broadcast(ctx, "chat:"+c.Room, Said{c.name, f.Text})
}
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
| `web.LiveAdopt(d)` | how long after a page's GET its join may adopt the session the render mounted, so `Mount` runs once per view; 10s. At most 1024 pages wait at a time, oldest dropped first; `web.LiveAdopt(0)` mounts fresh on every join |

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

**Checks in `Mount` and `Update`.** A page that checks in `Mount` whether
the visitor may see what its URL names (order `{id}`) makes the same check
in its `Update` taking `web.URLParams`, if it has one: a patch keeps the
page mounted and hands the new URL to that `Update`, not to `Mount`. A page
without such an `Update` mounts again on every patch, so its `Mount` sees
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
| `navigate` | runs the URL's route (`X-Vuka-Live: navigate`) and makes its page the session's; the reply carries `url` and `token`, the new page's join token, which a reconnect uses. `patch: true` asks to keep the page mounted, which the server does only for an `Update` taking `web.URLParams` ([Navigation](/web/navigation#the-server-decides-what-stays-mounted)); a `Mount`, `Update` or render failing answers `error`, nothing staying mounted |
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
