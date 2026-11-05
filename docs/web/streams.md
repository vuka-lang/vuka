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
| A stream | Lists that grow while the page is open or that the browser accumulates: a chat, a log tail, a notification feed, a table of thousands of rows loaded in chunks. The server holds nothing between replies. |

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
| `Delete(keys ...any)` | remove the rows with these keys; a key the browser doesn't hold is ignored |
| `Reset(rows ...T)` | replace every row with these, in order |
| `Limit(n int)` | the browser keeps at most `n` rows (0: no limit, the default) |

There is no accessor for handlers and no `Len`: the server doesn't know
what the browser holds.

### Insert Means Upsert {#upsert}

The server doesn't know which keys the browser holds, so an insert carries
the row and where it would go, and the browser decides:

- a row with the key is morphed in place — focus, a field being typed in
  and what JS commands changed survive — and **stays where it is**;
- else the row goes where the operation says: the end (`Insert`) or the
  start (`Prepend`).

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
start, `Prepend` from the end. A row replaced in place trims nothing. A
`Reset` with more than `n` rows keeps the first `n`.

So `Limit(200)` with `Insert` is a chat or a log — newest at the bottom,
the oldest dropping off the top — and `Limit(50)` with `Prepend` a
newest-first feed of the last 50, the oldest dropping off the bottom.

The limit goes with each batch, so changing it takes effect at the next
one. With a limit, the queue of a stream whose container isn't rendered
keeps at most `n` inserts at each end; without one, past 10,000 queued
operations the session logs a warning in development and tests.

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

A form in a row submits as a push too, the record's key a hidden field of
its payload; a handler validates and returns an error the page shows. A
success inserts the row again, which morphs in place.

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
`Rows()` in a test is what is queued. A click in a row, `` c.Click(`li[data-vk-row="9031"] button`) ``, sends its
push with the key. `c.Reconnect(s2)` joins a new session as a
browser does after a dropped connection, keeping the rows the page holds.

The [development toolbar](/web/toolbar)'s Live panel shows a line per stream
batch under the message that carried it:
`c1 Chat messages q7.12: +1 −1, limit 200, 312 B`.

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
| `data-vk-ops` | the batch's operations in order: `["reset"]`, `["delete", key]`, `["insert", key, "end" \| "start"]`; absent when the render has none |
| `data-vk-rows`, `data-vk-empty` | how many rows the container holds, and whether none: the server's for a batch starting with a reset, the runtime's after it applies one |
| children | the batch's inserted rows, each with `data-vk-row`, the key as `fmt.Sprint` writes it |

The browser keeps, per container, the batch it applied, and applies a
batch only when it is the next one: one it applied (the container morphed
again because something else in its instance changed) leaves the rows
alone; another generation's starts with a reset; one further ahead is out of
step, and the runtime joins again. There are no acknowledgements: replies
travel in order on one socket, a batch counts as delivered once its reply
is written, and a lost reply is rebuilt by the next join.

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
