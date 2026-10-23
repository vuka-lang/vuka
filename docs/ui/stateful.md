# Stateful Components

[[toc]]

## Introduction

A stateful component keeps server state between renders and answers browser
events with Go methods. You reach for one when a part of a page has to change
in response to the visitor, such as a counter, a form or a chat.

A `.vuka.html` shape is live when it embeds `ui.Live`, declares a decorated
hook, or wires a server handler in markup. Its exported fields are props,
unexported fields are tracked server state (or provided dependencies), and
`ui.State[T]` fields are browser state. The struct, hooks, methods and markup
live together in one shape file.

## Writing a Stateful Component

This counter takes a starting value as a prop and keeps its count as state:

```html
<script lang="go">
// counter.vuka.html
package app

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Counter struct {
	ui.Live
	Start int // a prop: <Counter start={5} />
	n     int // state
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }
</script>

<div><span>{n}</span><button onClick={Inc}>+</button></div>
```

Use `<Counter start={5}/>` in another shape. Attributes bind exported fields
by name (`start` and `Start` find Start; promoted fields count, but ui.Live
itself does not). Children fill Children ui.Node, and key identifies the child
among sibling placements.

An attribute naming an unexported field is a compile error: server state is
not a prop. The compiler supplies the node and live
interfaces; no render method needs to be authored.

## Static and Live Rendering {#static-and-live}

Rendered by `ui.String`, `ui.Handler` or any templ code, a stateful
component is plain HTML: a copy of the tag's value is mounted, rendered once,
and its event attributes write nothing. That is the first, HTTP render of a
page.

Under a [live session](/ui/live) it is an **instance**: created the first
time its tag renders, mounted once, then kept between renders with its
state. An instance not rendered any more is dropped, after its optional
`Unmount()` runs.

### Whose Fields Are Whose {#whose-fields}

An instance's unexported fields are always its own. Its exported fields
belong to whoever sets them:

| | exported fields |
|---|---|
| a component a tag renders | the parent's: each render of the tag sets them again, so the component changes only its unexported ones (a field marked `@ui.Bind` it sets is written to the tag's value too, until the parent renders the tag with another) |
| a routed live page (a shape under `pages/`) | the page's own: built for its URL when it mounts, then changed only by the page — its handlers, hooks, `Update` methods and fields marked `@ui.Bind` — and kept as it changes them, so `p.Reply = ""` in a submit's handler clears the input |
| that page on a [patch](/web/navigation) | the fields its URL binds (path segments and query parameters, as when it mounted) are set from the new URL before its `Update` takes it; the others stay the page's |

In the browser a field shows the server's value when that changes: a field
with focus too, when it still shows what it showed when the page last sent
an event (a form cleared once sent); one the user has typed into since keeps
what is typed.

### Instance Methods

A component's markup and optional methods are what the runtime uses:

| Method | |
|---|---|
| Shape markup | its output gets the instance's id as `data-vk-id` on its root element, or in a `<vk-c style="display:contents">` around it when it isn't one element; a tag with a `key` puts it there too, as `data-vk-key` |
| `@ui.Mount` | optional, once per instance, before its first render; an error fails the render. The method the attribute marks ([Hooks](#hooks)) |
| `Unmount()` | optional, when the instance is dropped |
| `Update(msg T)` | optional, overloadable: the messages the session delivers (below) |

`ui.Live` gives the component `ID()` (its instance id, `""` when static),
`Subscribe(topics...)` (call it in the mount hook), `PushJS(chain)` ([from the
server](/ui/js#from-the-server)), and `PushPatch(href)` and
`PushNavigate(href)`, which move the page to another URL once the reply is
applied ([navigation](/web/navigation)), and `PushEvent(name, payload)`,
which hands `payload` to the component's [scripts](/web/assets#scripts)
(`ctx.on(name, fn)`).

> [!WARNING]
> Embed `ui.Live` by value: a `*ui.Live` fails the render.

## Which Instance a Tag Gets {#which-instance}

A tag's place in its parent's markup: the component rendering it, the keys
of the elements around it, the tag itself (each tag in the source is its own
place), and its `key`, or else how many times that tag has rendered before
it in this render. So `if` around one tag never shifts another's
instance, a loop without keys pairs instances by position, and a loop with
keys follows them.

Inside a shape's markup root:

```html
<ul>
  for _, it := range items {
    <li key={it.ID}><Counter label={it.Name} /></li>
  }
</ul>
```

Two tags in one place with the same key fail the render. Components nest
freely: a stateful component renders others, each its own instance with its
own state, and a parent passes a child func props to hear from it.

## Handling Events {#events}

An `on…` attribute with an expression on an element is an event handler:
`onClick` handles `click`, `onKeyDown` `keydown` — any `onXxx`, lowercased
after `on`. The expression is any function value: a method value, a closure
(Go's per-iteration loop variables make `onClick={func() { t.Remove(it.ID) }}`
right in a loop), a func prop passed down. What it is passed follows from
its type:

| Handler | Gets |
|---|---|
| `func()` | nothing |
| `func(string)` | the value: for a keyboard event the key (`"Enter"`), else the element's value |
| `func(bool)`, `func(int)`, `func(float64)`, … | the value, parsed: a checkbox sends `"true"`/`"false"`, `"on"` is true; a bad number is the handler's error |
| `func(ui.Event)` | the whole event: `Type`, `Value`, `Key`, `Form` |
| `func(url.Values)` | the form's fields as sent (any `map[string][]string`) |
| `func(T)`, `func(*T)` with `T` a struct | the form's fields bound to `T`'s: a field is named by its `form` tag, else its `json` tag, else its name, matched ignoring case; embedded structs' fields count as `T`'s; a slice takes every value. Its `validate:` tags are checked as a route checks what it binds: a field breaking one is a 422 `Invalid` naming it (`text: required`), the reply's error, and the handler isn't called |

### Handler Parameters

The event fills one of those parameters, its payload. A handler's other
parameters are filled [by type](/web/dependency-injection#parameters-by-type)
too: a `context.Context` (the session's), and what the app injects — the
request the page joined with, the signed-in user (`*auth.Identity`), a
service registered with `app.Service` — so a method takes the store it
writes to as `func (t *Todo) Add(ctx context.Context, f NewItem, store
*Items) error`. ui fills them through the `ui.Injector` the session's
context carries, which package web gives it. A handler returns nothing or
an `error`.

### Checking Handlers

The compiler checks that the value is a function; the payload
rules are checked when the element first renders, which fails with the
expected shapes for any other signature (`ui.CheckHandler` is the check),
and an app checks its pages' hooks and exported methods when it is built:
a parameter nothing fills is a Build error at its file and line
([E0407](/errors/E0407), [E0405](/errors/E0405); `ui.CheckParams` is the
check).

A handler's error doesn't stop the session: the page renders again
(the handler may have changed state before failing) and the reply carries
the message. A chain of [JS commands](/ui/js) runs in the browser instead
(and calls handlers with `Push`); a `templ.ComponentScript` is still a script.

### A Form Example

The form DTO belongs in a logic file:

```go
// forms.go
package components

type NewItem struct { Name string `form:"name" validate:"required"` }
```

Save the rendering component as `todo.vuka.html` in the same package:

```html
<script lang="go">
package components

import "context"

type Todo struct {
    names []string
    query string
}

func (t *Todo) Add(ctx context.Context, f NewItem) error {
    t.names = append(t.names, f.Name)
    return nil
}

func (t *Todo) SetQuery(q string) { t.query = q }
</script>

<section>
  <form onSubmit={Add}><input name="name" onInput={SetQuery} /><button>Add</button></form>
  for _, name := range names {
    <p>{name}</p>
  }
</section>
```

### How Handlers Render

Under a session each handler renders as `data-vk-on-click="c2:0"`: the
instance's id and the handler's index in that instance's render. The
handlers of elements outside any stateful component belong to the page
itself, `c0`. Without a session the attribute isn't written.

After any event the session renders the page again — so a handler may change
any state it can reach, a parent's through a func prop included — and
replies with what changed.

## Hooks {#hooks}

The methods the runtime calls on a component are its hooks, each declared
with an attribute on the method:

| Attribute | Runs | Signature |
|---|---|---|
| `@ui.Mount` | once per instance, before its first render | `func(ctx context.Context) error` |
| `@ui.OnChange` | after the component sets bound fields; `c` says which | `func(ctx context.Context, c ui.Changed) error` |
| `@ui.Loads("group")` | after the first render, loading a group of [deferred fields](/web/islands#deferred-fields) | `func(ctx context.Context) error` |

Fields take attributes too: `@ui.Bind` (the component may set it),
`@ui.URL("q")` (the page's address keeps it: [URL State](/web/url-state)), `@ui.Defer("group")` (loaded
after the first render), and the rules of package `validate`. This dashboard
uses each hook and field attribute:

```html
<script lang="go">
// dashboard.vuka.html
package app

import (
	"context"
	"time"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/validate"
)

type Dashboard struct {
	ui.Live
	Q    string @ui.Bind{Debounce: 300 * time.Millisecond} @ui.URL("q") @validate.Max(100)
	Rows []Item @ui.Defer("lists")
}

@ui.Mount
func (d *Dashboard) mount(ctx context.Context) error { return nil }

@ui.OnChange
func (d *Dashboard) changed(ctx context.Context, c ui.Changed) error { return nil }

@ui.Loads("lists")
func (d *Dashboard) loadLists(ctx context.Context) error { return nil }
</script>

<input bind:value={Q} />
```

A hook may be unexported, and usually is: the browser calls exported event
methods, not hooks. Only a decorator marks a hook; an ordinary method name
does not.

The signatures in the table are common forms, not positional
requirements: context, request, signed-in identity, provided services and
ui.Changed are supplied by type. Hooks return nothing or error.

### Hook Errors at Startup

An unfillable parameter is a startup error at the method's source location.
A deferred group without a loader ([E0324](/errors/E0324)) or a malformed hook
([E0336](/errors/E0336)) also stops startup. Field attributes remain in the
shape header; the generated hook metadata is compiler output.

## Receiving Messages {#messages}

A component subscribed to a topic receives what the transport publishes on
it, through `Update` methods taking it — overloads included, each picked by
the message's dynamic type.

Declare NewMessage and Typing as data types in a logic file. The shape itself
holds only its one render struct:

```html
<script lang="go">
package components

import (
    "context"
    "github.com/vuka-lang/ui/v2"
)

type Chat struct {
    ui.Live
    Room string
    log []string
    typing string
}

@ui.Mount
func (c *Chat) mount(ctx context.Context) error { c.Subscribe("room:" + c.Room); return nil }

func (c *Chat) Update(m NewMessage) { c.log = append(c.log, m.Text) }
func (c *Chat) Update(ctx context.Context, t Typing) { c.typing = t.Who }
</script>

<section>
  for _, line := range log {
    <p>{line}</p>
  }
  <small>{typing}</small>
</section>
```

An `Update` takes an optional `context.Context` and the message, and returns
nothing or an error. One taking the message's type exactly wins over one it
is assignable to, and those over one taking `any`. The compiler gives the overloads distinct Go names (see [Overloading](/features/overloading)).

Message update methods are dispatched by message type, not exposed as browser events.

## Change Tracking {#change-tracking}

A `.vuka.html` file's struct tracks its server state: each unexported field
that isn't a `ui.State` or a provided dependency — and isn't tagged `` `vuka:"-"` `` — is tracked, its writes marked.
A component whose state fields are all tracked is rendered again only when
one of them was written or a prop changed: otherwise the session keeps its
last render, its handlers and the instances it rendered, and does not evaluate
its markup — instances inside it still render when their own state changed.
Props compare by value; a pointer, slice, map, func or interface prop
(children included) counts as changed unless both are nil.

In this row, `ID` and `Name` are props and `qty` is tracked state:

```html
<script lang="go">
// row.vuka.html
package app

import "github.com/vuka-lang/ui/v2"

type Row struct {
	ui.Live
	ID   int    // props
	Name string
	qty  int    // state, tracked
}

func (r *Row) Inc() { r.qty++ }
</script>

<tr key={ID}><td>{Name}</td><td>{qty}</td><td><button onClick={Inc}>+</button></td></tr>
```

The field is read and written as any field is — `r.qty++`, `r.rows =
append(r.rows, x)`, `r.rows[i].Done = true`, `r.employer.Addr.City` in a
method or `{employer.Label()}` in the markup, `r.employer.Name = x` — and
the compiler records reads and writes for the whole tracked value;
a method with a pointer receiver called
as a statement (`r.employer.Rename(x)`) is a write too. Taking a tracked
field's address, or calling such a method inside an expression, is
[E0150](/errors/E0150): a change through the pointer wouldn't be seen.

A click in one row of a thousand renders that row only. Markup must read
nothing but props, tracked fields and `vuka:"-"` fields that don't change
its output; [verification](/ui/live#verification) catches one that does.

### Where Its Methods Live {#where-its-methods-live}

A shape's methods may sit in its header or in a `.vuka` file of the same
package beside it: the compiler lowers tracked fields' reads and writes in
both alike, so a larger page keeps its markup and fields in the
`.vuka.html` file and its logic next to it. This `.vuka` file reads and
writes the tracked fields of a shape declared beside it:

<!-- go:context
type Rows struct {
	rows  []string
	count int
}
-->

```go
// rows.vuka — beside rows.vuka.html, which declares Rows and its fields
package components

func (r *Rows) load(all []string) {
	r.rows = all             // a write, as in the header
	r.count = len(r.rows)    // a read
}
```

A `.go` file is plain Go, which the compiler doesn't rewrite: there a
tracked field is not its declared type, and reading or writing it as one is
[E0155](/errors/E0155), naming the field and the shape file declaring it.
Move the method to a `.vuka` file, or into the header.

### Selectors and Mutable Values {#selectors-and-mutable-values}

A tracked struct field can be read through selectors and methods without
unwrapping it: `{employer.Addr.City}` and `{employer.Label()}` observe the
tracked employer value. Writes such as `p.employer.Addr.City = city`,
`p.rows[i].Done = true` and `p.rows = append(p.rows, row)` mark the enclosing
tracked field. A pointer-receiver mutator used as a statement, such as
`p.employer.Rename(name)`, is tracked as a write too.

Several of them may be assigned at once, as Go assigns them: every value is
evaluated first, then each place written, and a value with no type of its
own (`""`, `nil`, `-1`) takes its place's type:

<!-- go:context
type Query struct {
	Term string
	Page int
}

type Search struct {
	q     Query
	note  string
	tags  []string
	limit int64
}
-->

```go
// search.vuka — beside search.vuka.html, which declares Search
package components

func (s *Search) Find(term string) { s.q.Term, s.q.Page = term, 1 }

func (s *Search) Clear() { s.note, s.tags, s.limit = "", nil, -1 }
```

The builtins that change their first argument in place, `delete(p.items,
k)`, `clear(p.items)` and `copy(p.slots, src)`, are writes of that field
when called as a statement, as `p.items[k] = v` is: no copy to assign back.

> [!WARNING]
> Do not retain an address into tracked state, mutate through an escaped pointer,
> or call a pointer-receiver mutator inside an expression: the compiler cannot
> observe those writes and reports [E0150](/errors/E0150).

Treat a tracked
container's nested mutation as an update of that value, not independently
tracked storage for every element. Provided services keep shared application
state; their private mutations do not automatically write a component field.
Refresh the component's snapshot when service data changes.

### Browser State and Shared State {#browser-state-and-shared-state}

Browser-only state belongs in ui.State[T] fields (see [Client State](/ui/state)). Assign it in event
attributes (`onClick={open = !open}`) or in live methods. It does not require
a server re-render for a browser-side change.

Shared or durable state belongs
in services or persistence, not in one component instance. Passing callbacks
lets a child request a parent update without giving it ownership of parent props.
