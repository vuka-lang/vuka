# Streams: `ui.Stream[T]`

Status: phase 1 is implemented (ui, web and vuka, branch `feat/streams`);
phases 2 and 3 are not. Where phase 1 differs from the draft, the text says
so, and "Phase 1 as built" at the end lists each change.

## Principle

A stream is a list the server sends and then forgets. A live page queues
operations on it — insert these rows, delete that key, start over — and
the next reply carries them; once the reply is handed to the socket the
server drops them. The browser keeps the rows in the DOM and applies each
batch of operations once, by key. Between replies a stream costs the
server a few words, whatever the browser holds.

Today a list on a live page is a field holding every row
(`rows []Order`), rendered with a keyed loop. The v2 tree diff
(ui `live/tree.go`, `keyedPlan`) sends only the rows that changed, but
the session holds every row three times over: the field, the instance's
last render tree (`instance.tree`) and the shadow of what the client holds
(`instance.shadow`, `live/tree.go` `shadow`), plus each row's handler
closures (`instance.keyed`). Measured on ui's `demo.KeyedTable` (origin/main,
v2 join, verification off), a session holding a 1000-row table with two
handlers per row is **940 KB** of heap, against 3.4 KB for the same page with
no rows: about 0.94 KB per row per connection, before the app's own data.
A chat, a log or an activity feed that grows while the page is open grows
that without bound.

## 1. When to use one

| | Use |
|---|---|
| A keyed slice field | Lists up to a few hundred rows that the server sorts, filters or counts, whose rows hold live component state, or whose every render must reflect the whole list. The default. |
| Paging (`?page=` with a patch, [navigation](/web/navigation)) | Lists people address: a page number in the URL, the back button, "page 3 of 40", jumping. The server holds one page. |
| A stream | Lists that grow while the page is open or that the browser accumulates: a chat, a log tail, a notification feed, infinite scroll, a table of thousands of rows loaded in chunks. The server holds nothing between replies. |

What a stream gives up follows from the server not holding the rows: it
can't count them, sort them, filter them in place, read them in a handler,
or render anything that depends on them (a total, an "empty" message,
which row is selected). Each of those has a pattern below; when a page
needs several of them, it wants a slice or paging.

## 2. The API

### The field

A stream is an unexported field of a live shape:

```go
type Chat struct {
    ui.Live
    Room     string
    messages ui.Stream[models.Message]
    chat     *services.Chat
}
```

`ui.Stream[T]` is tracked state, like an Assign (ui `assign.go`): it
implements the `assigner` interface, dirty while it has operations queued,
so a page whose state is all tracked still renders when only its stream
changed, and is skipped otherwise. An exported stream (a prop) is an
error: a parent can't own rows the server forgets.

| Method | What it queues |
|---|---|
| `Insert(rows ...T)` | each row at the end — or, when the browser already holds its key, in place (below) |
| `Prepend(rows ...T)` | the rows at the start, in the order given |
| `InsertAt(i int, rows ...T)` | the rows before the browser's row `i` (from 0; past the end, at the end) |
| `Delete(keys ...any)` | remove the rows with these keys; a key the browser doesn't hold is ignored |
| `Reset(rows ...T)` | replace every row with these, in order |
| `Limit(n int)` | the browser keeps at most `n` rows (0: no limit, the default) |

There is no `Rows`-like accessor for handlers and no `Len`: see §8.

### The key

A row's key is the `key` attribute of the row's element, as for any keyed
loop. The stream itself takes no key function: `Delete(m.ID)` takes the
key's value, and the browser matches rows by the key's text, so the server
never needs the row to find it. The compiler knows the key expression's
type from the markup and checks that `Delete`'s arguments have it (vet,
§8); a stream has one key type.

The key is written on the row as `data-vk-row="<text>"`, the key as
`fmt.Sprint` writes it, HTML-escaped. Not `data-vk-key`, which keyed
elements carry today (ui `html.go`, `HTMLRenderer` writing `key`) and which
the runtime also reads as a keyboard filter (`data-vk-key="Enter Escape"`,
web `live.js` `handle`); a row needs an attribute nothing else means.
Plain text, not the handler-id encoding (docs/reference/live-protocol.md
"Handler Ids"), because the viewport events of §5 hand the key back to the
server, and the handler-id form hashes long strings.

### Markup

The loop ranges over `Rows()` and is the only content of its element, the
**container**:

```html
<script lang="go">
package chat

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Chat struct {
    ui.Live
    Room     string
    messages ui.Stream[models.Message]
    chat     *services.Chat
}

@ui.Mount
func (c *Chat) mount(ctx context.Context) error {
    c.messages.Limit(200)
    recent, err := c.chat.Recent(ctx, c.Room, 50)
    c.messages.Reset(recent...)
    c.Subscribe("chat:" + c.Room)
    return err
}

func (c *Chat) Update(m models.Said) { c.messages.Insert(m.Message) }

func (c *Chat) Update(d models.Unsaid) { c.messages.Delete(d.ID) }

func (c *Chat) Remove(ctx context.Context, id int64) error {
    if err := c.chat.Delete(ctx, c.Room, id); err != nil { return err }
    return web.Broadcast(ctx, "chat:"+c.Room, models.Unsaid{ID: id})
}
</script>

<section>
  <ul class="log">
    for _, m := range messages.Rows() {
      <li key={m.ID}>
        <b>{m.Who}</b> {m.Text}
        <button onClick={ui.JS.Push(Remove, m.ID)}>Delete</button>
      </li>
    }
  </ul>
  <form onSubmit={Send}><input name="text" /><button>Send</button></form>
</section>
```

`Rows()` is the rows queued since the last reply, in the order their
operations apply (a `Prepend`'s rows last first, each going to the start): the
first render after `Reset(recent...)` renders 50 rows, the render after an
`Insert` one, a render after neither none. That is the only place a row is
rendered on the server.

The compiler lowers the container to one call (the Go form below) instead
of an element with a block inside. The container must be an element (not
a fragment or a component tag) and the loop its only content (whitespace
aside), because the browser owns the element's children: anything the
server rendered beside the rows would be lost or duplicated.

### The Go form

A plain-Go component writes this; the markup lowers to `Block`, what `List`
is built on (see the end of this section):

```go
func (c *Chat) Render() ui.Node {
    return ui.El("section", nil,
        c.messages.List("ul", []ui.Attr{{Name: "class", Value: "log"}}, func(m models.Message) ui.Node {
            return ui.El("li", []ui.Attr{{Name: "key", Value: m.ID}},
                ui.El("b", nil, ui.Text(m.Who)), ui.Text(" "+m.Text),
                ui.El("button", []ui.Attr{{Name: "onClick", Value: ui.On(ui.JS.Push(c.Remove, m.ID))}}, ui.Text("Delete")))
        }),
        // the form …
    )
}
```

`List(tag, attrs, row)` renders the container with its stream attributes
and the queued rows; `row`'s result must be one element with a `key`. In
compiled markup each row is a frame (`ui.F`) like any block item, so its
statics are sent once per connection.

Compiled markup keeps the container in its template, its attributes and
handlers lowered as any element's, and lowers the loop to a block whose
result the stream's `Block` takes; the container gets the block's
attributes as an attribute set and the block as its content:

```go
__messages := c.messages.Block(__lis) // __lis: the loop's rows
… ui.HTML(`<ul class="log" {__messages.Attrs()...}>{__messages}</ul>`, __messages.Attrs(), __messages)
``` The stream's name on the page is its
field's name, found by the session from the component's fields as it finds
Assigns (`stateOf` in ui `assign.go`), so plain Go needs no name either.

### Insert means upsert

The server doesn't know which keys the browser holds — it forgot them —
so an insert carries the row and where it would go, and the browser
decides:

- the container holds a row with the key: the row is morphed in place
  (the runtime's usual morph, so focus, a field being typed in and
  reconciled command changes survive) and **stays where it is**; no limit
  trimming;
- else it is inserted where the operation says: the end (`Insert`), the
  start (`Prepend`), before row `i` (`InsertAt`).

So "update a row" is `Insert(row)`, and the same call is right whether or
not the browser still holds it (a trimmed row comes back at the end). To
move a row, `Delete` it and insert it again in the same batch: operations
apply in the order queued.

Within one batch the server compacts by key, so a batch never names a key
twice: a second insert of a key replaces the first's row and keeps the
first's position; `Delete` after an insert of the same key drops the
insert and keeps the delete; `Reset` drops everything queued before it.

### Limit

`Limit(n)` is the most rows the browser keeps. A batch that adds rows
trims from **the end away from where they went in**: `Insert` (the end)
trims from the start, `Prepend` (the start) trims from the end, `InsertAt`
from the end. A row replaced in place adds nothing and trims nothing. A
`Reset` with more than `n` rows keeps the first `n`.

That one rule gives the two common shapes:

```go
// A chat or a log: newest at the bottom, the oldest drop off the top.
c.messages.Limit(200)
c.messages.Insert(m)

// A newest-first feed of the last 50: newest at the top, the oldest drop off the bottom.
f.events.Limit(50)
f.events.Prepend(e)
```

The limit is sent with each batch (it is the stream's setting, not an
operation), so changing it takes effect at the next batch.

### Messages, broadcasts and navigation

Stream operations are ordinary state changes, so they belong wherever
state changes: the mount hook, event handlers, `Update` overloads for
messages, `Update` taking `web.URLParams` for a patch. A broadcast
(`web.Broadcast`, `app.Broadcast`, through a relay) reaches each
subscribed instance's `Update`, which queues the row on its own stream;
each connection's reply carries it and its session drops it. A record
broadcast to a thousand pages is held a thousand times only until each
page's reply is written.

A filter in the URL resets the stream in its `Update`:

```go
func (l *Log) Update(ctx context.Context, p models.LogParams) error {
    lines, err := l.logs.Tail(ctx, p.Level, 500)
    l.lines.Reset(lines...)
    return err
}
```

A navigation to another page unmounts the instance; its stream goes with
it (§6).

### Deferred loading

A stream may be deferred, and its loader fills it:

```go
type Feed struct {
    ui.Live
    events ui.Stream[models.Event] @ui.Defer("events")
    store  *services.Events
}

@ui.Loads("events")
func (f *Feed) loadEvents(ctx context.Context) error {
    evs, err := f.store.Latest(ctx, 50)
    f.events.Reset(evs...)
    return err
}
```

A loader runs on a copy of the component and `DeferredLoad.Apply` copies
the group's fields back (ui `island_defer.go`). A stream can't simply be
copied back: an event or a message may have queued operations on the
original while the loader ran. So the copy starts with no operations, and
on apply the stream **merges**: the loader's operations first, then those
the original queued meanwhile — the snapshot is older than the messages
that arrived during the load. The container usually renders only once
`Loading("events")` is false, and a stream whose container wasn't rendered
keeps its operations (§4), so nothing queued during the load is lost.

### Events in rows

A row's handler must say which row it is for without the server holding
the row. Today a handler inside a key is named by the key path and its
number in the item's render (`c2:row-42:0`, docs/reference/live-protocol.md
"Handler Ids"), and resolved through the instance's last render
(`instance.keyed`, cleared by every render in `renderInst`): once the row
is no longer rendered, the id names nothing. A closure
(`onClick={func() { c.Remove(m.ID) }}`) is worse: it is the row, held.

Inside a stream row, a server handler is a `Push` naming a method of the
stream's owner, with the row's values as arguments:

```html
<button onClick={ui.JS.Push(Remove, m.ID)}>Delete</button>
<input type="checkbox" checked={m.Done} onChange={ui.JS.Push(SetDone, m.ID, ui.This.Checked())} />
```

The arguments travel in the attribute (the Push step's `args`); the step's
handler id is the method by name (`c1:Remove`), the form the protocol
already has for a component's scripts, which `live.AllowPush` gates (ui
`live/session.go`). The session allows the methods a type's stream rows
push as a row's render names them, so they are allowed for that type
without a call, in compiled markup and plain Go alike. A row
rendered an hour ago still works: nothing it names was dropped. The
arguments are the browser's, as every push argument is: a handler checks
that the visitor may touch the record the key names.

A plain method value (`onClick={Remove}`), a closure, or a func prop set
to either is an error in a stream row (§8). A func prop taking `ui.Commands`
(a row component's `onRemove={ui.JS.Push(Remove, u.ID)}`) is fine.

### Components in rows

A row may use stateless shapes freely: they render into the row's markup.
A **live component** tag in a row (`<UserRow key={u.ID} user={u} />`
where `UserRow` embeds `ui.Live`) is an error.

The alternatives were to keep the instance, or to rebuild it from the key
when an event names it. Keeping it brings back what a stream exists to
avoid: an instance, its tree and its shadow per row (the 0.94 KB per row
above, plus its state), held for every row ever shown, since the server
doesn't see the browser trim — `Limit` would cap the DOM and not the
server. Rebuilding from the key would run the component's mount hook on
every click and still lose any state it had between events, which makes
it the same as a method of the owner that reloads the record and
re-inserts the row, with more machinery. Per-row state that must outlive
a render belongs in the record (the store) or in the browser
(`ui.State`); per-row server work belongs to the owner's methods.

### Forms in rows

A row may hold a form; its submit is a Push like any row handler, the
record's key a hidden field of its payload:

```go
type Rename struct {
    ID   int64  `form:"id"`
    Name string `form:"name" validate:"required,max=80"`
}

func (t *Tasks) Rename(ctx context.Context, f Rename) error {
    task, err := t.tasks.Rename(ctx, f.ID, f.Name)
    if err != nil { return err }
    t.rows.Insert(task)
    return nil
}
```

Validation errors can't re-render the row: the server has no row to
render, and page-level form state (`ui.FormState`) is per form, not per
row. They don't need to. Struct forms already have a mode in which the
server answers with a **report** — `{errors, summary, words, choices}` —
and the runtime places each error at its field without a render
(`applyReport` in web `live.js`, used by forms on pages that aren't live,
docs/ui/forms.md "Forms on Pages That Aren't Live"). A `<ui.Form>` inside
a stream row uses it over the socket: a submit failing its `validate:`
tags, or a handler returning a `forms` error, answers with the report as
the Push's `result` (`{"report": …}`), addressed to that row's form, and
nothing else changes. A success re-inserts the row, which morphs in place;
its fields take the server's values where those changed, as everywhere.
Report results are phase 3; until then a row form validates in its handler
and returns an error the page shows.

### Counting and empty states

The server can't count a stream: it doesn't know which keys were new, and
the browser trims. Counts come from where the records are, kept in
tracked fields beside the stream and changed by the same code that
changes the records:

```go
type Inbox struct {
    ui.Live
    mail   ui.Stream[models.Mail]
    total  int // in the store
    unread int
    store  *services.Mail
}

@ui.Mount
func (i *Inbox) mount(ctx context.Context) error {
    i.mail.Limit(100)
    page, n, unread, err := i.store.Latest(ctx, 100)
    i.mail.Reset(page...)
    i.total, i.unread = n, unread
    i.Subscribe("mail")
    return err
}

func (i *Inbox) Update(m models.Arrived) {
    i.mail.Prepend(m.Mail)
    i.total++
    i.unread++
}
```

```html
<p>Showing the latest {min(total, 100)} of {total}, {unread} unread</p>
```

For what only the browser knows, the runtime keeps two attributes on the
container: `data-vk-rows="<n>"` and, with no rows, `data-vk-empty`.
`ul[data-vk-empty] + .none { display: block }` shows an empty message
without the server; a `ui.State` binding can read neither, and needs none.

## 3. The wire

Phase 1 adds no message types and no tree syntax. A stream is markup
with a contract, so it works the same in the HTTP render, a v1 patch, a
v2 tree and the Go reference client:

```html
<ul class="log" data-vk-stream="messages" data-vk-batch="q7.12"
    data-vk-ops="[[&#34;delete&#34;,&#34;9030&#34;],[&#34;insert&#34;,&#34;9031&#34;,&#34;end&#34;]]"
    data-vk-limit="200">
  <li data-vk-row="9031"><b>ana</b> hello <button data-vk-js-click="…">Delete</button></li>
</ul>
```

| Attribute | |
|---|---|
| `data-vk-stream` | the stream's name in its instance (the field's name); refs and the runtime find it in the nearest `data-vk-id` scope, as `data-vk-ref` is |
| `data-vk-batch` | `<generation>.<n>`: a generation is random per stream per mount; `n` counts the batches of that generation from 1 |
| `data-vk-ops` | the batch's operations in order: `["reset"]`, `["delete", key]`, `["insert", key, "end" \| "start" \| i]`; empty when the render has none |
| `data-vk-limit` | the limit, absent for none |
| children | the batch's inserted rows, each with `data-vk-row`, in the order of their operations |

### One reply

A v2 page `c1` renders the container as part of a frame whose dynamics
include the stream's attributes — one dynamic, the attribute set — and the
rows block. A message arrives; `Update` inserts row 9031 and deletes 9030;
the reply (whitespace added):

```json
{"type":"patch","trees":[{"id":"c1","tree":{"u":{
  "2":" data-vk-stream=\"messages\" data-vk-batch=\"q7.12\" data-vk-limit=\"200\" data-vk-ops=\"[[&#34;delete&#34;,&#34;9030&#34;],[&#34;insert&#34;,&#34;9031&#34;,&#34;end&#34;]]\"",
  "3":{"c":[{"t":6,"d":[" data-vk-row=\"9031\"","ana","hello"," data-vk-js-click=\"…\""]}]}}}}]}
```

The next reply that renders `c1` for another reason (the "typing…" line)
carries nothing for the stream: once a reply is written the session takes
what the batch added (`data-vk-ops`, and `data-vk-rows`/`data-vk-empty` on
a reset, which come last) off the attributes it keeps, so a render with no
operations is the same; the batch is still `q7.12`, and the client, which
still holds the operations, ignores a batch it applied.

### The session forgets

The block of a stream is a `ui.TreeList` marked as a stream (a field on
`TreeList`, set by `List`). After `Session.patch` encodes a reply
(`treeUpdates`, then `settle`), the session empties every stream block in
the instance's `tree` and `shadow` and clears the stream's operations. The
next render diffs the new batch against an empty list, so the rows go as
a new list (`{"c":[frame, …]}`) and never as copy or change steps. The
session also takes the batch's attributes off the container's (above), and
empties a container rendered as one piece of markup (a Go component's, a
templ component's) the same way. A row's markup is sent inline, never kept
by reference on the connection: it is never sent again, and the
connection's table of kept strings would otherwise hold an entry per row
ever shown (94 KB of a 1000-row stream's session, measured). The
client's tree may still hold the previous batch's items; the loop steps
are defined over the old items ("old items no step takes are gone",
docs/reference/live-protocol.md "Changes"), so the result is the new batch
either way. A v1 connection sends the instance's HTML; the container's
rows are the batch, as above.

A stream is flushed only by a reply that rendered its block. A render that
fails (an error message instead of a patch), a failed navigation, or a
render in which the container isn't shown leaves the operations queued.

### Applied once, never resent

There are no acknowledgements. Replies travel in order on one socket, and
the server counts a batch as delivered once its reply is handed to the
socket. A batch is never resent: Vuka keeps no session across a
disconnect (docs/web/live.md "Connections and Reconnects"), so whatever a
lost reply carried is rebuilt by the next join (§6).

The browser keeps, per container, the batch it applied, and applies a
batch only when it is the next one:

| Batch seen at a morph | |
|---|---|
| the same as applied, or older, same generation | ignored: the container's attributes are synced, its children left alone |
| applied + 1, same generation | applied |
| further ahead, same generation | out of step: the runtime joins again, as it does for a tree it can't apply (`receive` in web `live.js`) |
| another generation | applied, its first operation being `reset` (§6) |

Ignoring a seen batch is what makes a morph safe: a v2 client rebuilds an
instance's whole HTML from its tree for every entry naming it, then morphs
(`tree().apply`, then `apply` in web `live.js`), so a container is morphed
again with a batch it has applied whenever anything else in its instance
changes.

### Ordering in a reply

The runtime applies a reply as now — trees, then morphs outermost first,
the form reset, the error, `vk:patched`, then the reply's pushed `js`. A
container's batch is applied when the morph reaches it, so a parent's
markup is in place first and pushed commands (`ScrollTo` the new row,
`Focus` its field) find the rows. A reply's rows and the result of a Push
in the same reply are applied in that order too: the result's `Into`
target may be inside a row the batch just inserted.

## 4. Server state

Per stream, per connection, at rest: its generation, batch number and
limit, an empty operation slice, and the container's statics, which the
connection's statics table holds once for every page. Measured in phase 1
(ui `BenchmarkStreamMemory`): a session holding a 1000-row stream (two
pushes per row) after one insert is 4.2 KB, the same page with no rows
4.1 KB. Between a change
and its reply it also holds the queued rows (`T` values) and, while the
reply is built, their frames.

| 1000 rows on a page | Held per connection between events |
|---|---|
| a keyed slice, two handlers per row (measured) | 940 KB, about 0.94 KB per row |
| a stream (measured) | 4.2 KB, the page's own: the rows only until their reply is written |

The rows' frames don't stay in the session either: §3 empties the stream
block of `tree` and `shadow` after each reply, and no row handler is held
(§2, Events in rows).

Queued operations are bounded where they could grow: a stream whose
container isn't rendered (inside a closed tab of the page) while messages
arrive. With a limit, the queue compacts to at most `limit` inserts at the
inserting end (older ones would be trimmed anyway); without one, past
10,000 queued rows the session logs a warning naming the stream in
development and tests (phase 1 only warns: it doesn't reset the stream).

## 5. The browser runtime

In web `live.js`, `morph` gains a case beside `data-vk-ignore` and
`data-vk-island`: an element with `data-vk-stream` gets `attrs(el, next)`,
then the batch rule of §3; `children` is never run on its children.
Applying a batch:

- `reset`: the container's rows are made the batch's rows by key — a row
  whose key the batch inserts is morphed in place and moved into order;
  others are removed; new ones inserted. A reset therefore keeps the
  elements a page keeps across a rejoin, with their focus and scroll;
- `delete`: the row with that `data-vk-row` is removed;
- `insert`: the row with that key is morphed in place, else the batch's
  child is imported (`place(document.importNode(…))`, as `children` does)
  and inserted at the position;
- then the limit trims, from the end §2 says;
- then `data-vk-rows` and `data-vk-empty` are set, and handler listeners
  for the new rows are added (`listenIn`, `bindIn` for their bindings).

Rows are matched by `data-vk-row` among the container's element children,
not by `id` (the morph's `keyOf`), so a row's own `id` is the app's.

### Focus and scroll

A row morphed in place keeps focus as any morphed element does. A deleted
or trimmed row holding focus passes it to the next row, else the previous,
else the container (given `tabindex="-1"`). Rows added or trimmed at the
start of a scrolled container would push the visible rows down; the
runtime anchors on the first fully visible row, measuring its offset
before the batch and correcting the scroll position after, and sets
`overflow-anchor: none` on the container so a browser's own anchoring
doesn't correct twice.

### Infinite scroll

Two events, on the container:

| | Fires when |
|---|---|
| `onViewportTop` | the container's first row comes within the margin of its scroller's top |
| `onViewportBottom` | its last row comes within the margin of the bottom |

They are ordinary handler attributes: `data-vk-on-viewporttop` and
`data-vk-on-viewportbottom`, DOM events the runtime dispatches itself
from an `IntersectionObserver` on the edge rows (the root is the nearest
scrolling ancestor, else the viewport; the margin 400px, or
`data-vk-viewport-margin`). The event's value is the edge row's key, so
the handler takes it as it takes any value (`func(id int64)`, parsed). An
edge fires once until its reply comes (the container is `aria-busy`
meanwhile, as an element waiting for a reply is) and fires again only
when the edge row has changed: a reply adding no rows at that end is the
end of the list, and nothing fires again until something does.

### A two-way window

A conversation opened at its newest messages, older ones loaded at the
top, newer ones at the bottom when the visitor jumped back in history,
the browser keeping about 150 rows:

```html
<ul data-vk-viewport-margin="600px" onViewportTop={Older} onViewportBottom={Newer}>
  for _, m := range messages.Rows() {
    <li key={m.ID}>{m.Text}</li>
  }
</ul>
```

```go
@ui.Mount
func (c *Thread) mount(ctx context.Context) error {
    c.messages.Limit(150)
    msgs, err := c.store.Before(ctx, c.ID, math.MaxInt64, 50)
    c.messages.Reset(msgs...)
    return err
}

func (c *Thread) Older(ctx context.Context, first int64) error {
    msgs, err := c.store.Before(ctx, c.ID, first, 50)
    c.messages.Prepend(msgs...) // trims the newest at the bottom past 150
    return err
}

func (c *Thread) Newer(ctx context.Context, last int64) error {
    msgs, err := c.store.After(ctx, c.ID, last, 50)
    c.messages.Insert(msgs...) // trims the oldest at the top past 150
    return err
}
```

The server keeps no edge ids. It could — two tracked fields, `first` and
`last` — but the browser's trimming moves the edges without telling it,
so ids kept on the server go stale after the first trim; the event's value
is always the edge the browser has. What the server may keep is what only
it knows: `atNewest bool`, set when `After` returns nothing, to stop
inserting live messages at the bottom while the window shows older ones.

## 6. First render, join and reconnect

**The HTTP render** (the page's GET, a page without JavaScript, `ui.String`,
a component rendered without a session): the container holds the rows
`Mount` queued — the first batch, generation `g`, `n` 1, with its `reset`
— as plain children. It reads as a list with or without script; without
script, a stream only ever shows what the mount hook put in it.

**The join** adopts the GET's session when it can (docs/web/live.md "The
Mount Hook Runs Once per View"). The GET's render flushed batch `g.1`, so
the join's render has no operations and no rows, and its container says
`g.1`. The runtime reads the applied batch of a container it meets for the
first time from its `data-vk-batch` attribute, so the join's morph finds
`g.1` applied and leaves the rows alone: nothing is sent twice.

**A join that mounts fresh** — adoption off or out of time, another
identity, a server restart, and every reconnect — runs the mount hook
again: the stream gets a new generation `h`, and its first batch `h.1`
starts with `reset`. The container's rows become what the mount hook
loaded, kept elements morphed rather than replaced. Rows the page had
gained from messages since are gone unless the mount hook loads them
again: a stream is never a replay log, as the "Streaming Records" section
of docs/web/live.md already says for slices. Load the snapshot from the
store in the mount hook, and the reset is exactly right.

There is no resume window in which a dropped connection gets its session
back. If one is added, the batch numbers already cover it: replies resent
from a parked session are ignored up to the applied batch and applied
after it.

**Embedded components** behave the same: a stream belongs to its
instance; a fresh mount of the instance (a new key, a navigation) is a new
generation; an unmount removes the container with the rows. A container
that stops being rendered while its instance stays (an `if` around it)
loses its rows in the browser; when it renders again the stream has
nothing to show unless it was `Reset` meanwhile, and the session reports
that under verification (§8).

## 7. Tests and the toolbar

The Go reference client, `ui/live/livetest`, applies stream batches as the
browser does (`applyMarkup`, its morph), so a test drives a page and reads
what a browser would hold:

```go
c, err := livetest.Join(s, 2)
// …
c.Info("chat:lobby", models.Said{Message: models.Message{ID: 9031, Who: "ana", Text: "hello"}})
rows := c.Stream("c1", "messages") // []livetest.Row{Key, HTML, Text}, in DOM order (proposed)
if len(rows) != 51 || rows[50].Key != "9031" { t.Fatal(rows) }
```

`Stream` is the only way to see rows, as in the browser; the component's
`Rows()` in a test shows what is queued, which is what a render would
send. Rendering without a session (`ui.String`) shows the mount hook's
rows. Scrolling, the viewport events and focus are a browser's: web's
Chrome tests (docs/web/testing.md) cover them, with the reference client's `Fire` (a `ui.Event` of type
`viewportbottom` whose `Value` is the edge key) to drive a handler.

Verification (`VUKA_LIVE_VERIFY`, on in tests) keeps working: a skipped
instance's stream block is empty in its last tree, and a render of it
without queued operations is empty too. It also fails a render that shows
a container again after it wasn't rendered, unless the stream was reset
in between.

The toolbar's Live panel (docs/web/toolbar.md) shows a line per stream in
a message's summary — `c1 Chat messages q7.12: +1 −1, limit 200, 312 B` —
and the operations in its Diff view; a batch the browser ignored or found
out of step is shown red under the message that carried it, from what the
runtime reports.

## 8. Errors and checks

The compiler and `vuka vet` (E0353–E0359; the runtime reports E0355,
E0357, E0358 and E0359 too, for Go components):

- a loop over `Rows()` that isn't the only content of an element, or
  whose parent is a fragment or a component tag;
- two loops over one stream, or one inside another loop;
- `Rows()` read anywhere else — in markup outside the loop (`len(messages.Rows())`),
  in a method, in a hook — since it is only what was queued, not what is
  shown;
- a row without a `key` on its first element, or a row that isn't one
  element;
- `Delete`'s argument of another type than the row's key expression;
- an exported `ui.Stream` field, or one marked `@ui.Bind` or `@ui.URL`;
- in a row: a handler that is a closure, a method value or a func prop
  set to one (help: `ui.JS.Push(Remove, m.ID)`), and a live component tag
  (help: render a stateless shape and push to the owner's methods).

At run time, under `vuka run` and in tests:

- a container rendered again after it wasn't, with no `Reset` between;
- the queue warning of §4;
- in the browser, a batch ahead of the next one (an out-of-step join),
  logged to the console and the toolbar.

## 9. Phasing

1. **Phase 1 — the smallest useful stream.** `ui.Stream[T]` with `Insert`,
   `Prepend`, `Delete`, `Reset`, `Limit`; the markup form and `List`; the
   HTML contract and the batch rule; the session emptying stream blocks
   after a reply; the generation reset on a fresh mount; row handlers as
   pushes by method name; deferred streams' merge; `data-vk-rows` and
   `data-vk-empty`; the compile errors; livetest's `Stream`; the toolbar
   line; a chat in web's `examples/live` and a memory benchmark beside
   `BenchmarkLoad`. Enough for a chat, a log tail and a capped feed.
2. **Phase 2 — scrolling.** `InsertAt`, `onViewportTop` and
   `onViewportBottom`, scroll anchoring, focus hand-off, a two-way window
   example and its Chrome test.
3. **Phase 3 — rows that do more.** Struct forms in rows answering with
   reports; a v2 dynamic of its own for a stream block if the attribute
   form proves costly; the sugar of open question 2.

## Open questions

1. **A typed key.** `ui.Stream[K, T]` would let `Delete` take `K` in the
   Go type system instead of by vet. It costs a second type argument on
   every declaration. Is the vet check enough?
2. **Row handlers without `Push`.** Inside a row, `onClick={Remove}` where
   `Remove` takes the key's type could be lowered to
   `ui.JS.Push(Remove, m.ID)`. Shorter, but it is the one place a method
   value means something else.
3. **Limit's end.** "The end away from where rows went in" covers chats,
   feeds and windows. Is there a case for a per-call override
   (`Insert(rows, ui.TrimEnd)`)?
4. **Browser state in rows.** A `ui.State` declared in a row needs a key
   that includes the row's key (`StateKey` and `KeyStates`); rows of two
   batches must never share one. To check against `scope.StateKey` before
   phase 1 relies on it.
5. **Reset on fresh mount.** Morphing kept rows on a reconnect keeps scroll
   and focus but means a row the store deleted while the page was
   disconnected disappears silently. Acceptable, or should a reset after a
   reconnect be marked for the page (`vk:stream-reset`)?
6. **Pages that aren't live.** A stream on a page with no session renders
   as a plain list from its mount hook. Should viewport events there call
   their handler over HTTP, as a Push does on such pages, and insert the
   returned rows? That is most of an infinite-scroll list for static pages.
7. **Names.** `Rows()` (vs ranging the field directly, which the compiler
   could accept), `List`, `data-vk-row`, `onViewportTop`/`onViewportBottom`.

## Phase 1 as built

What phase 1 changed of the draft above, each forced by the
implementation or chosen as its simplest consistent form:

- **The lowering.** Markup lowers to `Stream.Block(rows)` with the
  container left in its template (`{b.Attrs()...}`, `{b}`), not to `List`:
  the container's attributes, handlers and refs lower as any element's.
  `List` is `Block` for Go.
- **One dynamic for the attributes.** The container's stream attributes are
  one attribute set, so one dynamic. What a batch adds comes last and is
  taken off the kept render after the reply, rather than sent going empty
  by the next render. With a reset the server also renders `data-vk-rows`
  and `data-vk-empty`, so a page without script reads them too.
- **Rows inline.** A stream's rows and its attributes are never kept by
  reference on a v2 connection (§3).
- **Row pushes allowed by the session** as a row's render names them,
  rather than registered by the compiler, so plain Go needs nothing either.
- **`Rows()` order** is the order operations apply: a `Prepend`'s rows
  come last first.
- **Delete's type check** (E0356) accepts the key's type and an untyped
  constant of its kind.
- **A container in a branch** (`if`) is allowed; in a loop it is E0353.
- **Verification** fails a render showing a container after a render of
  its owner that didn't, unless `Reset` ran between.
- **The queue warning** only warns (§4).
- **Out of step** in the browser makes the page join again after a patch;
  the browser logs it to the console. The toolbar's stream line (from the
  frames: `c1 Chat messages q7.12: +1 −1, limit 200, 312 B`) is in; the red
  line for a batch the browser ignored or found out of step, which needs
  the runtime to report to the toolbar, is not.
- **Tests**: `livetest.Client.Stream` reads a container's rows, and
  `Client.Reconnect` joins a new session keeping them, as a browser does
  after a dropped connection.
