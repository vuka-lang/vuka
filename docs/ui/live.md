# Live sessions

`github.com/vuka-lang/ui/live` keeps a page's [stateful
components](/ui/stateful) alive for one connected browser: a session holds
their instances, calls their event handlers and `Update` methods, and
answers each with what changed. It knows no transport — a WebSocket server
drives it with the messages of the [live protocol](/reference/live-protocol);
the [web framework](/web/live) is one such server.

::: vuka
```vuka
s := live.NewSession(ctx, func(ctx context.Context) ui.Node { return <App user={u} /> })
html, err := s.Render()                         // the page container's HTML
p, err := s.Event(ui.Event{Target: "c2:0", Type: "click"})
p, err := s.Info("room:go", NewMessage{"hi"})    // "" reaches every instance
topics := s.Topics()                            // what to route to Info
p, err := s.Navigate(route, patch)              // another URL's page, on the same connection
s.Close()                                       // unmounts everything
```
:::

<!-- go:context
type User struct{ Name string }

type App struct{ User User }

func (a App) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<p>{a.User.Name}</p>`, a.User.Name).Render(ctx, w)
}

type NewMessage struct{ Text string }

type page struct {
	Render func(ctx context.Context) ui.Node
	Params map[ui.Stateful]any
}

type routes struct{}

func (routes) Match(url string) (page, bool) { return page{}, false }

var (
	u      User
	route  live.Route
	patch  bool
	pages  routes
	s      = live.NewSession(context.Background(), func(ctx context.Context) ui.Node { return App{} })
)
-->

::: go
<!-- go:fragment -->
```go
s := live.NewSession(ctx, func(ctx context.Context) ui.Node { return App{User: u} })
html, err := s.Render()                       // the page container's HTML
p, err := s.Event(ui.Event{Target: "c2:0", Type: "click"})
p, err = s.Info("room:go", NewMessage{"hi"}) // "" reaches every instance
topics := s.Topics()                          // what to route to Info
p, err = s.Navigate(route, patch)             // another URL's page, on the same connection
s.Close()                                     // unmounts everything
```
:::

A `live.Patch` holds the changed instances and `Error`, a handler's message.
Calls are safe from several goroutines and run one at a time; `ctx` is the
context of every render, `Mount`, handler and `Update`.

## Errors

What a handler, `Mount` or `Update` returns, or panics with, and a failed
render become the text of a reply's `error`. By default that text is the
error's own, which may say more than a visitor should read — a query, a
path, a panic's value. A transport serving pages to the public sets
`s.SetErrorText(func(err error) string)`: it decides each error's text and
can log the error. `live.PublicMessage(err)` finds a `live.PublicError` in
the chain — an error whose `PublicMessage()` is meant for the visitor, such
as a form field that doesn't bind, or the web framework's `*web.Error` —
which such a transport shows while hiding the rest. The [web
framework](/web/live#errors) does.

## What a reply holds

After an event the session renders the page again and compares. In
**protocol v1** a reply is the HTML of each instance whose own HTML changed
(children excluded), outermost first, which the browser morphs in place: a
click in one counter of a hundred sends one counter.

In **protocol v2** (`s.RenderTrees()`, or a client joining with `"v":2`)
each instance renders to a **render tree**: the compiler's
[frames](/features/jsx#what-it-becomes) become the strings the source fixes
(statics, sent once per connection) around what the expressions rendered
(dynamics), a block's items as a list, another instance as a reference. The
session keeps a shadow of what the client holds and answers with the
difference: changed dynamics by index, list items kept, moved by `key`,
inserted or removed, a branch's new frame, long markup by token patch. A
one-cell change in a 1000-row table is 89 bytes instead of 122 KB. The
format is in the [reference](/reference/live-protocol#protocol-v2-render-trees).

[`.templ` files](/ui/templ#on-live-pages) the vuka command compiles record
the same trees, so a templ layout or view diffs like JSX. A frame holding
nothing but one other frame (a page returning its layout) is that frame, so
pages sharing a layout key alike, and navigating between them is a change
inside it.

Either version's reply also carries the [JS commands](/ui/js#from-the-server)
components pushed with `PushJS` while it was made (`Patch.JS`, and
`s.JS()` for those a `Mount` pushed during the join's render), run by the
browser once the reply is applied.

## Navigation

A connected page can move to another URL without a new session: the
transport resolves the URL to a `live.Route` and the session makes its page
its own ([navigation](/web/navigation) is what users see of it).

<!-- go:fragment -->
```go
s.SetRouter(func(url string) live.Route {
	page, ok := pages.Match(url)
	if !ok {
		return live.Route{}                        // not a page: the browser loads url
	}
	return live.Route{Ctx: ctx, Render: page.Render, Updates: page.Params}
})
```

| `live.Route` | |
|---|---|
| `Render` | the new page's render function; nil when the URL isn't a page |
| `Ctx` | the context of what runs from then on (the session's when nil) |
| `Redirect` | with `Render` nil, the URL the browser loads (the URL itself when empty) |
| `Updates` | for a patch: an instance kept for a tag whose props are a key takes the value in its `Update` method before it renders |

`s.Navigate(route, patch)` returns what changed, as an event does. As a
**patch**, the instances the new page renders where the old one did stay
mounted, their props set from its tags, and `Updates` reach their `Update`
methods. Otherwise every instance unmounts and the new page's mount. The
page itself (`c0`) is the same instance either way, so the client gets only
what changed of its markup — in v2, against statics it already holds.
`Handle` answers the protocol's `navigate` message through the router;
without one, every navigation is a `redirect`, a full page load.

**The router decides what stays mounted**, not the client asking for a
patch. A patch whose route has no `Updates` is a navigation: everything
mounts afresh. An instance whose tag `Updates` names is kept only when an
`Update` method of its takes the value; otherwise it mounts afresh, so its
`Mount` sees the new URL. A component that checks in `Mount` whether the
visitor may see what its props name has to check again in that `Update`.

An error — a `Mount`'s, an `Update`'s, a render's — fails the navigation:
`Navigate` returns it, every instance unmounts, and the page takes no events
until it navigates or joins again; a browser loads the URL itself. A
`redirect` the router asks for is sent only for a path or an `http` or
`https` URL (`ui.LoadableURL`); any other is an `error`.

## Change tracking

Components whose state is all tracked are skipped when nothing they
read changed — see [Change tracking](/ui/stateful#change-tracking). Skipping
is what makes large pages cheap: a thousand row components, one clicked,
renders one.

## Verification

In tests (`testing.Testing()`) and with `VUKA_LIVE_VERIFY=1`, a session
renders every component it would skip and fails the event with an error
naming it when the output differs (a plain field changed, or `Render` read
something else); it also checks that frames sharing a fingerprint render the
same statics. `VUKA_LIVE_VERIFY=0` turns it off; `live.Verify` is the
default for new sessions.

## Numbers

Go benchmarks in ui's `live/bench_test.go`, on an M1 Pro (10 cores),
verification off; bytes are the JSON of the reply:

| | v1 | v2 |
|---|---|---|
| 1000-row table, one component, one cell per event: bytes | 121,753 | 89 |
| — server time per event | 1.37 ms | 2.0 ms |
| 1000 row components, one row per event, rows with tracked state: bytes / time | 182 / 93 µs | 63 / 105 µs |
| — the same rows with plain state | 182 / 1.66 ms | 63 / 2.9 ms |
| a page of 100 counters, one clicked: bytes / time | 154 / 65 µs | 62 / 102 µs |

A v2 render builds a tree instead of a string, so a component that renders
again costs about 1.5× the CPU of v1; skipping tracked components is what makes large
pages cheap, and v2 makes the replies small. 5,000 joined sessions of a
page of about 25 instances hold 29 KB per session.

## Known limits

- A stateful component re-renders with its whole page on every event,
  unless its state is all tracked; the reply is still only what
  changed.
- In v2, handler ids count a component's handlers in render order: inserting
  an item with handlers changes the ids after it, which travel as changed
  dynamics. Token patches keep the common start and end of long markup and
  replace the middle.
- Next: spot-level skipping inside a component's render, handler ids that
  don't shift when items are inserted, streams, presence and flash messages.
