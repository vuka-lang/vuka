# Live components

A [stateful component](/features/components#stateful-components) — a struct
embedding `vuka.Live` — keeps its state on the server and answers its
elements' events over a WebSocket; the page changes in place, with no
JavaScript to write. Vuka gives the component model and the
[session](/features/components#the-session); web gives the transport, the
browser runtime and the routes.

```vuka
@web.Live("/counter")
type Counter struct {
	vuka.Live
	Start int // ?start=3
	n     int
}

func (c *Counter) Mount() { c.n = c.Start }
func (c *Counter) Inc()   { c.n++ }

func (c *Counter) Render() vuka.Node {
	return <div><output>{c.n}</output><button onClick={c.Inc}>+</button></div>
}
```

## How a page becomes live

Two ways, one mechanism:

- **`@web.Live(path, opts…)`** on a stateful component serves it as a GET
  page. Its exported fields of provided types are injected; the others that
  hold text bind from the path segment or query parameter of their name
  (`form` tag, else the field name, ignoring case). The page is the component
  inside a layout — `func(page vuka.Node) vuka.Node`:

  | | |
  |---|---|
  | `web.Layout(f)` | this page's layout (an option of `@web.Live`) |
  | `web.LiveLayout(f)` | the app's, for every live page without its own |
  | neither | a bare HTML document |
  | `web.Use(mw…)` | middleware for this page (an option of `@web.Live`) |

  From Go: `app.Live("/counter", &Counter{Start: 1})`, the value's fields
  being the defaults.

- **Any GET route** whose page — a `vuka.Node` result or a templ view —
  renders a stateful component or an event handler is live by itself:

  ```vuka
  @web.Get("/")
  func Home() vuka.Node { return <Shell title="Home"><Counter Start={10} /></Shell> }
  ```

  A Vuka component rendered from a templ view is live too (templ passes the
  context on). `html/template` views and JSON are never live.

The first, HTTP render is complete HTML, and before `</body>` goes
`<script src="/_vuka/live.js" data-vk-token="…" defer>`. The script opens
`/_vuka/live?t=<token>` and sends `join`; the server runs the same route
again — a GET to the token's URI, with the WebSocket request's headers
(cookies, auth), through the app's and the route's middleware, with its
dependencies — and the page it renders becomes the connection's session.
That second run mounts the instances that keep state (LiveView's two-phase
mount). A join the route answers with a redirect sends `redirect`, and the
browser follows it; any other non-page answer (a 404, JSON) is an `error`,
and the connection closes.

The route's handler runs once per connection: its result is the page, and
the page is rendered again after every event. State belongs in the
components.

## Events

`onClick={c.Inc}` renders `data-vk-on-click="c1:0"`. The runtime listens on
the document for each event type a page names, and sends the nearest
handler's id, the element's value (a checkbox's `"true"`/`"false"`), the key
of a keyboard event, and a submitted form's fields. What a handler receives
follows its type — nothing, the value, `vuka.Event`, `url.Values`, a struct
bound from the form; see [Events](/features/components#events).

```vuka
type NewItem struct {
	Text string `form:"text"`
}

func (t *Todo) Add(f NewItem) error {
	text := strings.TrimSpace(f.Text)
	if text == "" {
		return errors.New("write something first")
	}
	t.next++
	t.items = append(t.items, Item{ID: t.next, Text: text})
	return nil
}

func (t *Todo) Filter(q string) { t.filter = q }

func (t *Todo) Render() vuka.Node {
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
| `<html data-vk-state>` | `connecting`, `connected`, `disconnected` or `closed`: style a banner on it |
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
the [`Update`](/features/components#messages) overload of its type, and its
page gets the patch:

```vuka
type Said struct{ Who, Text string }
type Joined struct{ Who string }

@web.Live("/chat/{room}")
type Chat struct {
	vuka.Live
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

A bad or expired token closes the connection with code 4001, and the runtime
reloads the page (at most once in 10 seconds) for a fresh one.

## The wire

Vuka's [wire protocol](/features/components#the-wire-protocol) as is, plus:

| | |
|---|---|
| `GET /_vuka/live?t=<token>` | the WebSocket; the token is in the URL, so a bad one is refused before any message |
| `GET /_vuka/live.js?v=<hash>` | the runtime, immutable for its hash (ETag otherwise) |
| `join` | runs the route; a second `join` on the same connection starts over |
| `redirect` | sent for a join the route redirects; the connection then closes normally (1000) |
| close 4001 | invalid or expired token: reload |
| close 4004 | the route isn't a live page: the error message precedes it; no retry |
| close 1001 | the server is stopping: reconnect |
| request header `X-Vuka-Live: join` | set on the route's run for a join |

A broadcast arriving before `join` is dropped (the join renders fresh).

## Limits

- Patches are whole components' HTML, morphed in the browser; state starts
  over on reconnect.
- The live endpoints are at `/_vuka/…` from the root: an app mounted under a
  path prefix needs them routed there.
- Event handlers get the join's context: `web.Request` is the join's
  request, and writing to `web.ResponseWriter` goes nowhere.

The [`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live)
app has a counter page, a counter inside an ordinary page, a todo list and a
chat whose tabs see each other's messages.
