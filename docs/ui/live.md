# Live sessions

`github.com/vuka-lang/ui/live` keeps a page's [stateful
components](/ui/stateful) alive for one connected browser: a session holds
their instances, calls their event handlers and `Update` methods, and
answers each with what changed. It knows no transport — a WebSocket server
drives it with the messages of the [live protocol](/reference/live-protocol);
the [web framework](/web/live) is one such server.

```go
s := live.NewSession(ctx, func(ctx context.Context) ui.Node { return <App user={u} /> })
html, err := s.Render()                         // the page container's HTML
p, err := s.Event(ui.Event{Target: "c2:0", Type: "click"})
p, err := s.Info("room:go", NewMessage{"hi"})    // "" reaches every instance
topics := s.Topics()                            // what to route to Info
s.Close()                                       // unmounts everything
```

A `live.Patch` holds the changed instances and `Error`, a handler's message.
Calls are safe from several goroutines and run one at a time; `ctx` is the
context of every render, `Mount`, handler and `Update`.

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

## Change tracking

Components whose state is all `ui.Assign`s are skipped when nothing they
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
| 1000 row components, one row per event, rows with Assigns: bytes / time | 182 / 93 µs | 63 / 105 µs |
| — the same rows with plain state | 182 / 1.66 ms | 63 / 2.9 ms |
| a page of 100 counters, one clicked: bytes / time | 154 / 65 µs | 62 / 102 µs |

A v2 render builds a tree instead of a string, so a component that renders
again costs about 1.5× the CPU of v1; Assign skipping is what makes large
pages cheap, and v2 makes the replies small. 5,000 joined sessions of a
page of about 25 instances hold 29 KB per session.

## Known limits

- A stateful component re-renders with its whole page on every event,
  unless its state is all `ui.Assign`s; the reply is still only what
  changed.
- In v2, handler ids count a component's handlers in render order: inserting
  an item with handlers changes the ids after it, which travel as changed
  dynamics. Token patches keep the common start and end of long markup and
  replace the middle.
- Next: spot-level skipping inside a component's render, handler ids that
  don't shift when items are inserted, streams, presence and flash messages.
