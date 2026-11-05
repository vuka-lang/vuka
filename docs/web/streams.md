# Streams

[[toc]]

## Introduction

A stream is a list a live page sends and then forgets. The page queues
operations on it — insert these rows, delete that key, start over — and the
next reply carries them; once the reply is written the server drops them.
The browser keeps the rows and applies each batch of operations once, by
key. Between replies a stream costs the server a few words, however many
rows the browser holds.

A keyed slice field holds every row on the server, in the field and in the
render the session keeps to diff against. A chat, a log tail or an activity
feed that grows while the page is open grows that without bound. A stream
holds nothing:

| 1,000 rows on a live page, two handlers per row | Server heap per connection between events |
|---|---|
| a keyed slice field | 940 KB |
| a stream | 4.2 KB — the same as the page with no rows |

Measured with ui's `BenchmarkStreamMemory` (v2 join, then one row inserted).

## When to Use One {#when}

| | Use |
|---|---|
| A keyed slice field | Lists up to a few hundred rows that the server sorts, filters or counts, whose rows hold live component state, or whose every render must reflect the whole list. The default. |
| Paging (`?page=` with a patch, [navigation](/web/navigation)) | Lists people address: a page number in the URL, the back button, "page 3 of 40". The server holds one page. |
| A stream | Lists that grow while the page is open or that the browser accumulates: a chat, a log tail, a notification feed, [infinite scroll](#infinite-scroll), a table of thousands of rows loaded in chunks. The server holds nothing between replies. |

What a stream gives up follows from the server not holding the rows: it
can't count them, sort them, filter them in place, read them in a handler,
or render anything that depends on them — a total, an "empty" message,
which row is selected. Each has a pattern [below](#counting); when a page
needs several of them, it wants a slice or paging.

## A Chat {#chat}

A stream is an unexported field of a live shape, `ui.Stream[T]`. Its
container is an element whose only content is a loop over `Rows()`, each
row one element with a `key`:

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

`Rows()` is the rows queued since the last reply: the first render after
`Reset(recent...)` renders 50 rows, the render after an `Insert` one, a
render after neither none. It is the only place a row is rendered on the
server, and nothing else may read it.

The mount hook loads the snapshot, messages insert and delete, and every
connection's reply carries its own batch: a record broadcast to a thousand
pages is held a thousand times only until each page's reply is written.

## The Operations {#operations}

| Method | What it queues |
|---|---|
| `Insert(rows ...T)` | each row at the end — or, when the browser already holds its key, in place |
| `Prepend(rows ...T)` | the rows at the start, in the order given |
| `InsertAt(i int, rows ...T)` | the rows before the browser's row `i` (from 0), in the order given; past the last row, at the end |
| `Delete(keys ...any)` | remove the rows with these keys; a key the browser doesn't hold is ignored |
| `Reset(rows ...T)` | replace every row with these, in order |
| `Limit(n int)` | the browser keeps at most `n` rows (0: no limit, the default); `.TrimStart()` or `.TrimEnd()` on what it returns fixes the end it trims |

There is no accessor for handlers and no `Len`: the server doesn't know
what the browser holds.

### Insert Means Upsert {#upsert}

The server doesn't know which keys the browser holds, so an insert carries
the row and where it would go, and the browser decides:

- a row with the key is morphed in place — focus, a field being typed in
  and what JS commands changed survive — and **stays where it is**;
- else the row goes where the operation says: the end (`Insert`), the
  start (`Prepend`) or before row `i` (`InsertAt`).

So "update a row" is `Insert(row)`, whether or not the browser still holds
it (a trimmed row comes back at the end). To move a row, `Delete` it and
insert it again in the same batch: operations apply in the order queued.
Within a batch a second insert of a key replaces the first's row in its
place, and a `Delete` after an insert of the key drops the insert.

`Reset` keeps the elements whose keys it inserts, morphed and moved into
its order, so rows a page keeps across a reconnect keep their focus.

### Limit {#limit}

`Limit(n)` is the most rows the browser keeps. A batch that adds rows trims
from **the end away from where they went in**: `Insert` trims from the
start, `Prepend` and `InsertAt` from the end. A row replaced in place trims nothing. A
`Reset` with more than `n` rows keeps the first `n`.

So `Limit(200)` with `Insert` is a chat or a log — newest at the bottom,
the oldest dropping off the top — and `Limit(50)` with `Prepend` a
newest-first feed of the last 50, the oldest dropping off the bottom.

The limit goes with each batch, so changing it takes effect at the next
one. With a limit, the queue of a stream whose container isn't rendered
keeps at most `n` inserts at each end; without one, or holding
`InsertAt`'s rows (which land where the browser's rows say), past 10,000
queued operations the session logs a warning in development and tests.

`InsertAt(i, rows...)` counts the rows the browser holds, which the server
doesn't know: it suits a position the page knows from what it shows — a
row the visitor dropped between two others, its index sent with the drop.
A negative `i` is 0, so `InsertAt(0, rows...)` is `Prepend(rows...)`.

### Which End a Limit Trims {#trim}

The end away from where rows went in suits a stream that grows at one end.
A newest-first feed that also loads older rows at the bottom ("show older")
grows at both: there, `Insert` would trim the newest rows off the top. Fix
the end the limit trims instead, whichever end a batch goes in at:

```html
<script lang="go">
package feed

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Feed struct {
    ui.Live
    events ui.Stream[models.Event]
    store  *services.Events
}

@ui.Mount
func (f *Feed) mount(ctx context.Context) error {
    f.events.Limit(100).TrimEnd() // the newest 100, at the top
    evs, err := f.store.Latest(ctx, 50)
    f.events.Reset(evs...)
    f.Subscribe("events")
    return err
}

func (f *Feed) Update(e models.Happened) { f.events.Prepend(e.Event) } // the bottom trimmed

func (f *Feed) Older(ctx context.Context, last int64) error {
    evs, err := f.store.Before(ctx, last, 50)
    f.events.Insert(evs...) // goes in at the bottom, and is trimmed there once 100 are shown
    return err
}
</script>

<ul onViewportBottom={Older}>
  for _, e := range events.Rows() {
    <li key={e.ID}>{e.Text}</li>
  }
</ul>
```

| | A batch's rows at the end (`Insert`) | at the start (`Prepend`, `InsertAt`) | A `Reset` past the limit keeps |
|---|---|---|---|
| `Limit(n)` | trim the start | trim the end | the first `n` |
| `Limit(n).TrimEnd()` | trim the end: past `n`, they don't show | trim the end | the first `n` |
| `Limit(n).TrimStart()` | trim the start | trim the start: past `n`, they don't show | the last `n` |

A row the browser holds is still morphed in place and trims nothing. Calling
`Limit` again goes back to the end away from the rows until `TrimStart` or
`TrimEnd` follows it. The queue of a container that isn't rendered is
bounded the same way: it keeps the inserts the browser would show.

## Infinite Scroll {#infinite-scroll}

A container's `onViewportBottom` fires when its last row comes into view,
and `onViewportTop` when its first does. The event's value is that row's
key, so the handler loads what comes after it — or before it — and the
server keeps no cursor:

```html
<script lang="go">
package orders

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Orders struct {
    ui.Live
    orders ui.Stream[models.Order]
    store  *services.Orders
}

@ui.Mount
func (o *Orders) mount(ctx context.Context) error {
    page, err := o.store.After(ctx, 0, 50)
    o.orders.Reset(page...)
    return err
}

func (o *Orders) More(ctx context.Context, last int64) error {
    page, err := o.store.After(ctx, last, 50)
    o.orders.Insert(page...)
    return err
}
</script>

<table>
  <tbody onViewportBottom={More}>
    for _, ord := range orders.Rows() {
      <tr key={ord.ID}><td>{ord.Number}</td><td>{ord.Total}</td></tr>
    }
  </tbody>
</table>
```

The handler takes the key as the type the rows' `key` renders — parsed, as
any event's value is — or as a `string`; the compiler checks it
([E0361](/errors/E0361)), and that the events are on a stream's container
([E0360](/errors/E0360)).

### When the Events Fire {#viewport-events}

- An edge fires when its row comes within 400px of its scroller's edge —
  the nearest ancestor that scrolls, or the container itself when it has
  `overflow: auto`, else the page. `data-vk-viewport-margin="800px"` on the
  container sets the distance.
- It fires once, then waits for its reply: the container is `aria-busy`
  meanwhile, and scrolling sends nothing more. Checks are throttled, so
  however fast the visitor scrolls, an edge sends at most one event at a
  time.
- After the reply it fires again only if another row is at that edge and in
  view. A reply that adds no rows at that end is the end of the list:
  nothing fires there again until the rows change — a reset, say.
- A page that opens with a short first page fires at once, and keeps
  loading until the edge row is out of reach, so the list fills the
  scroller.
- A container with no rows fires nothing: there is no key to send. Load
  the first page in the mount hook.

### Both Directions {#both-directions}

A window over a long history opens somewhere in it, loads older rows at the
top and newer ones at the bottom, and keeps about 150 rows, the limit
trimming the end away from each load:

```html
<script lang="go">
package threads

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Thread struct {
    ui.Live
    ID       int64
    At       int64 // ?at=, a message to open at
    messages ui.Stream[models.Message]
    store    *services.Messages
}

@ui.Mount
func (t *Thread) mount(ctx context.Context) error {
    t.messages.Limit(150)
    msgs, err := t.store.Around(ctx, t.ID, t.At, 50)
    t.messages.Reset(msgs...)
    return err
}

func (t *Thread) Older(ctx context.Context, first int64) error {
    msgs, err := t.store.Before(ctx, t.ID, first, 50)
    t.messages.Prepend(msgs...) // trims the newest, at the bottom, past 150
    return err
}

func (t *Thread) Newer(ctx context.Context, last int64) error {
    msgs, err := t.store.After(ctx, t.ID, last, 50)
    t.messages.Insert(msgs...) // trims the oldest, at the top, past 150
    return err
}
</script>

<ol class="thread" style="height: 32rem; overflow: auto"
    data-vk-viewport-margin="600px" onViewportTop={Older} onViewportBottom={Newer}>
  for _, m := range messages.Rows() {
    <li key={m.ID}><b>{m.Who}</b> {m.Text}</li>
  }
</ol>
```

The server doesn't keep the edges: the browser's trimming moves them
without telling it, so they would go stale after the first trim. The
event's value is always the edge the browser has. What the server may keep
is what only it knows — `atNewest`, set when `After` returns nothing, to
stop inserting live messages at the bottom while the window shows older
ones.

### Scroll Position and Focus {#anchoring}

Rows added, changed or trimmed above what the visitor is reading would push
it down the page. The runtime keeps it where it is: before a batch it notes
where the first rows fully in view are, and after it scrolls by however far
the first of them still there moved — nothing, where the browser's own
scroll anchoring already kept it. So `Older` above loads 50 rows above the
reader and the row they were reading doesn't move, and `Newer`'s trim of
the top doesn't make the page jump. A scroller at its very top isn't
anchored, so a newest-first feed's new rows show; one answering the
container's `onViewportTop` is, as its rows go above the reader.

A row that holds focus — its button, a field in it — keeps it when the row
is updated in place. One that a batch removes (deleted, trimmed, left out
of a reset) hands focus to the nearest row still there, the next, else the
previous, else the container; the runtime gives the row or the container
`tabindex="-1"` so it can take it. Focus anywhere else is left alone.

## Events in Rows {#events}

A row's server handler must say which row it is for without the server
holding the row. Inside a stream row it is a `Push` of a method of the
stream's owner, with the row's values as arguments:

```html
<button onClick={ui.JS.Push(Remove, m.ID)}>Delete</button>
<input type="checkbox" checked={m.Done} onChange={ui.JS.Push(SetDone, m.ID, ui.This.Checked())} />
```

The push names the method (`c1:Remove`), the arguments travel in the
attribute, and the instance takes pushes of the methods its rows push. A
row rendered an hour ago still works. The arguments are the browser's, as
every push argument is: a handler checks that the visitor may touch the
record the key names.

A closure, a plain method value (`onClick={Remove}`) or a func prop set to
one in a row is an error ([E0358](/errors/E0358)): the server would have to
keep it for the row. A func prop taking `ui.Commands` is fine.

A row may render stateless shapes freely. A live component in a row is an
error ([E0359](/errors/E0359)): its instance would be held for every row
ever shown, which is what a stream exists to avoid. Per-row state belongs in
the record or in the browser (`ui.State`); per-row server work belongs to
the owner's methods, which reload the record and insert the row again.

## Forms in Rows {#forms}

A row may hold a [struct form](/ui/forms). Its submit is a push of a
method of the owner, the row's key among its arguments and the form's
fields filling the method's last parameter, the form:

```html
<script lang="go">
package tasks

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Rename struct {
    ui.FormState
    Name string `validate:"required,max=80"`
}

func renameOf(t models.Task) *Rename { return &Rename{Name: t.Name} }

type Tasks struct {
    ui.Live
    tasks ui.Stream[models.Task]
    store *services.Tasks
}

func (p *Tasks) Rename(ctx context.Context, id int64, f Rename) error {
    if p.store.Taken(ctx, f.Name) {
        return f.Fail("Another task has that name")
    }
    task, err := p.store.Rename(ctx, id, f.Name)
    if err != nil {
        return err
    }
    p.tasks.Insert(task)
    return nil
}
</script>

<ul>
  for _, t := range tasks.Rows() {
    <li key={t.ID}>
      <ui.Form for={renameOf(t)} onSubmit={ui.JS.Push(Rename, t.ID)} submitLabel="Rename" noSummary/>
    </li>
  }
</ul>
```

The server forgets the row, so the form keeps nothing on the server
either: each render builds it from the record (`renameOf`), and a submit
binds what the browser sent into a fresh `Rename` and runs its rules and
`Clean` methods, as any struct form's submit does. Then:

- **It fails its checks** — the method isn't called — or the method returns
  a form error (`f.Fail`, a `forms.Error`, a public error): nothing
  renders. The reply carries the form's report, and the
  browser puts each error at its field and the summary in place on that
  row's form, as for a [form on a page that isn't live](/ui/forms#http):
  the fields aren't rendered again, so what was typed, the caret and the
  focus stay. Focus moves to the summary (or the first field in error)
  only when it was in that form: a visitor typing in another row keeps
  typing.
- **It succeeds**: the method inserts the row again, which morphs in place,
  and the form's errors clear. A field the visitor typed in since the
  submit keeps what they typed; the others take the server's values where
  those changed.

A row's form is keyed by its row (`rename-42`, its controls `rename-42-name`)
unless it has a `key` of its own, so each row's ids are its own. It checks
as it is submitted: there is no form on the server to check against as the
visitor types, so `onChange` in a row is an error ([E0363](/errors/E0363)),
and so is a row's form with no `onSubmit` or one pushing a method that
doesn't take the form ([E0362](/errors/E0362)). With `noSummary` the
report shows no summary, focus going to the first field in error.

## Counting and Empty States {#counting}

The server can't count a stream. Counts come from where the records are,
kept in fields beside the stream and changed by the same code that changes
the records:

```html
<script lang="go">
package inbox

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Inbox struct {
    ui.Live
    mail   ui.Stream[models.Mail]
    total  int
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
</script>

<section>
  <p>Showing the latest {min(total, 100)} of {total}, {unread} unread</p>
  <ul class="mail">
    for _, m := range mail.Rows() {
      <li key={m.ID}>{m.Subject}</li>
    }
  </ul>
  <p class="none">No mail</p>
</section>
```

For what only the browser knows, the container carries `data-vk-rows="<n>"`
and, with no rows, `data-vk-empty`: `ul[data-vk-empty] + .none { display:
block }` shows an empty message without the server.

## Filters, Navigation and Deferred Loading {#updates}

Stream operations are ordinary state changes: they go in the mount hook,
event handlers, `Update` overloads for messages, and `Update` taking the
page's parameters for a patch. A filter in the URL resets the stream:

```html
<script lang="go">
package logs

import (
    "context"
    "example.com/app/models"
    "example.com/app/services"
    "github.com/vuka-lang/ui/v2"
)

type Log struct {
    ui.Live
    Level string
    lines ui.Stream[models.Line]
    logs  *services.Logs
}

func (l *Log) Update(ctx context.Context, p models.LogParams) error {
    lines, err := l.logs.Tail(ctx, p.Level, 500)
    l.lines.Reset(lines...)
    return err
}
</script>

<pre class="tail">
  for _, ln := range lines.Rows() {
    <span key={ln.Seq}>{ln.Text}</span>
  }
</pre>
```

A navigation to another page unmounts the instance, and its stream goes
with it.

A stream may be deferred, its loader filling it. The loader runs on a copy
whose stream starts with nothing queued; on apply its operations go before
those the page queued meanwhile — messages that arrived during the load are
newer than its snapshot. Render the container once `Loading("events")` is
false: a stream whose container isn't rendered keeps its operations, so
nothing queued during the load is lost.

## First Render, Join and Reconnect {#reconnect}

The page's HTTP render holds the rows the mount hook queued as plain
children — the first batch — so it reads as a list with or without script.

The join adopts the GET's session when it can
([the mount hook runs once per view](/web/live#the-mount-hook-runs-once-per-view)):
the GET's render sent the first batch, so the join's render has no rows,
and the runtime, which takes a container it meets for the first time as
holding the batch its `data-vk-batch` names, leaves the rows alone. Nothing
is sent twice.

A join that mounts fresh — adoption off or expired, another identity, a
server restart, and every reconnect — runs the mount hook again: the stream
gets a new generation whose first batch starts with a reset, and the
container's rows become what the mount hook loaded, kept elements morphed
rather than replaced. Rows the page gained from messages since are gone
unless the mount hook loads them: a stream is never a replay log. Load the
snapshot from the store in the mount hook, and the reset is exactly right.

A container that stops being rendered while its instance stays (an `if`
around it) loses its rows in the browser; when it renders again, `Reset`
the stream. In tests and with `VUKA_LIVE_VERIFY=1` a render showing the
container again with no `Reset` since fails.

## In Go {#go}

A component written in Go renders the container with `List`: the element's
tag and attributes, and what each row becomes — one element with a `key`.
The stream's name on the page is its field's name.

<!-- go:context
type Message struct {
	ID        int64
	Who, Text string
}

type Said struct{ Message }
-->

```go
package chat

import "github.com/vuka-lang/ui/v2"

type Chat struct {
	ui.Live
	messages ui.Stream[Message]
}

func (c *Chat) Remove(id int64) { c.messages.Delete(id) }

func (c *Chat) Render() ui.Node {
	return ui.El("section", nil,
		c.messages.List("ul", []ui.Attr{{Name: "class", Value: "log"}}, func(m Message) ui.Node {
			return ui.El("li", []ui.Attr{{Name: "key", Value: m.ID}},
				ui.El("b", nil, ui.Text(m.Who)), ui.Text(" "+m.Text),
				ui.El("button", []ui.Attr{{Name: "onClick", Value: ui.JS.Push(c.Remove, m.ID)}}, ui.Text("Delete")))
		}))
}
```

The compiler lowers a container to `Block`, which `List` is built on: the
stream's rows block for the render, whose `Attrs` go on the container.
A container's viewport handlers are among `List`'s attributes, as any
element's handler is: `{Name: "onViewportBottom", Value: ui.On(c.More)}`.

## Testing {#testing}

The reference client, `ui/live/livetest`, applies batches as the browser
does; `Stream` reads the rows a container holds, in the page's order:

```go
package chat

import (
	"context"
	"testing"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
	"github.com/vuka-lang/ui/v2/live/livetest"
)

func TestChat(t *testing.T) {
	s := live.NewSession(context.Background(), func(context.Context) ui.Node {
		return ui.Component("chat", nil, &Chat{})
	})
	c, err := livetest.Join(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Info("chat:lobby", Said{Message{ID: 9031, Who: "ana", Text: "hello"}}); err != nil {
		t.Fatal(err)
	}
	rows := c.Stream("c1", "messages") // []livetest.Row{Key, HTML, Text}
	if len(rows) == 0 || rows[len(rows)-1].Key != "9031" {
		t.Fatal(rows)
	}
}
```

`Stream` is the only way to see rows, as in the browser: the component's
`Rows()` in a test is what is queued. `c.Viewport("ul.log", "bottom")`
fires the container's viewport event with its edge row's key, as a browser
does when the row comes into view (scrolling, anchoring and focus are a
browser's: test them in Chrome, [Testing](/web/testing)). A click in a row, `` c.Click(`li[data-vk-row="9031"] button`) ``, sends its
push with the key. `c.Reconnect(s2)` joins a new session as a
browser does after a dropped connection, keeping the rows the page holds.

The [development toolbar](/web/toolbar)'s Live panel shows a line per stream
batch under the message that carried it:
`c1 Chat messages q7.12: +1 −1, limit 200, 312 B`. A batch the browser
ignored — one it had applied already, delivered again — or found out of step
(a reply lost on the way, after which the page joins again) is a red line
under the message that carried it, from what the runtime reports:
`c1 messages q7.14: out of step, the browser applied q7.12`. The runtime
dispatches each as a `vk:stream` event on the document, `{id, stream,
batch, applied, problem, ref}`.

## The Wire {#the-wire}

A stream adds no message types: it is markup with a contract, the same in
an HTTP render, a v1 patch and a v2 tree.

```html
<ul class="log" data-vk-stream="messages" data-vk-batch="q7.12" data-vk-limit="200"
    data-vk-ops="[[&#34;delete&#34;,&#34;9030&#34;],[&#34;insert&#34;,&#34;9031&#34;,&#34;end&#34;]]">
  <li data-vk-row="9031"><b>ana</b> hello <button data-vk-js-click="…">Delete</button></li>
</ul>
```

| Attribute | |
|---|---|
| `data-vk-stream` | the stream's name in its instance: the field's |
| `data-vk-batch` | `<generation>.<n>`: a generation is random per stream per mount; `n` counts its batches from 1 |
| `data-vk-limit` | the limit, absent for none |
| `data-vk-trim` | `start` or `end`: the end the limit trims, when `TrimStart` or `TrimEnd` fixed it |
| `data-vk-ops` | the batch's operations in order: `["reset"]`, `["delete", key]`, `["insert", key, "end" \| "start" \| i]`; absent when the render has none |
| `data-vk-rows`, `data-vk-empty` | how many rows the container holds, and whether none: the server's for a batch starting with a reset, the runtime's after it applies one |
| children | the batch's inserted rows, each with `data-vk-row`, the key as `fmt.Sprint` writes it |
| `data-vk-on-viewporttop`, `-bottom` | the container's viewport handlers, as any element's handler renders; the event's value is the edge row's `data-vk-row` |
| `data-vk-viewport-margin` | the app's: how near the edge row must come, 400px if absent |

The browser keeps, per container, the batch it applied, and applies a
batch only when it is the next one: one it applied (the container morphed
again because something else in its instance changed) leaves the rows
alone; another generation's starts with a reset; one further ahead is out of
step, and the runtime joins again. There are no acknowledgements: replies
travel in order on one socket, a batch counts as delivered once its reply
is written, and a lost reply is rebuilt by the next join.

### The Compact Form {#compact}

On a v2 connection the runtime joins with the `stream` extension
(`{"type":"join","v":2,"ext":["stream"]}`), and the container's stream
attributes — one dynamic of its frame — go as a dynamic of their own,
their operations plain JSON with one-letter names, rather than JSON inside
an HTML attribute inside a JSON string:

```json
{"st":{"n":"messages","b":"q7.12","l":200,"o":[["d","9030"],["e","9031"]]}}
```

| | |
|---|---|
| `n`, `b`, `l` | the stream's name, its batch and its limit |
| `tr` | `"s"` or `"e"`: the end the limit trims |
| `o` | the operations: `["r"]` a reset, `["d", key]` a delete, `["e", key]` and `["s", key]` an insert at the end or the start, `[i, key]` one before row `i` |

The runtime writes the attributes back from it — `data-vk-rows` and
`data-vk-empty` counted from a reset's inserts, as the server counts them —
so the container's HTML, and the batch rule above, are the attribute
form's. The rows are a block's frames as before, their statics sent once
per connection. The page's HTTP render, v1 and a client joining without the
extension get the attributes as they are.

Once a reply is written the session empties the stream's rows from the
render it keeps and drops what the batch added to the container's
attributes, so the next render diffs against no rows. Rows' markup is sent
inline, never kept by reference on the connection: no row is sent twice.

## Errors {#errors}

| Code | |
|---|---|
| [E0353](/errors/E0353) | the loop isn't the only content of an element, is inside another loop, or a stream has two |
| [E0354](/errors/E0354) | `Rows()` read outside its container's loop |
| [E0355](/errors/E0355) | a row isn't one element with a key |
| [E0356](/errors/E0356) | `Delete` given another type than the rows' key |
| [E0357](/errors/E0357) | an exported stream, or one bound with `@ui.Bind` or `@ui.URL` |
| [E0358](/errors/E0358) | a handler in a row that isn't a `Push` of the owner's method |
| [E0359](/errors/E0359) | a live component in a row |
| [E0360](/errors/E0360) | `onViewportTop` or `onViewportBottom` on an element that isn't a stream's container |
| [E0361](/errors/E0361) | a viewport handler that doesn't take the edge row's key |
| [E0362](/errors/E0362) | a form in a row that doesn't submit to a method taking it |
| [E0363](/errors/E0363) | a form in a row with `onChange` |
