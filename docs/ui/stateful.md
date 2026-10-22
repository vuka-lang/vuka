# Stateful components

<!-- go:context
type Item struct {
	ID   int
	Name string
}

type Todo struct {
	ui.Live
	items []Item
}

func (t *Todo) SetQuery(q string) {}
func (t *Todo) Remove(id int)     {}

type Counter struct {
	ui.Live
	Start int
	Label string
	n     int
}

func (c *Counter) Render() ui.Node { return ui.HTML(`<span>{c.n}</span>`, c.n) }

type Dashboard struct {
	ui.Live
	Q    string
	Rows []Item
}

func (d *Dashboard) loadLists(ctx context.Context) error                { return nil }
func (d *Dashboard) changed(ctx context.Context, c ui.Changed) error   { return nil }
func (d *Dashboard) mount(ctx context.Context) error                    { return nil }

type NewMessage struct{ Text string }
type Typing struct{ Who string }

type Chat struct {
	ui.Live
	Room   string
	log    []string
	typing string
}

var (
	t  = &Todo{}
	it Item
)
-->

A struct embedding `ui.Live`, whose pointer has a `Render() ui.Node` method,
is a stateful component. Its exported fields are its props, set from the
tag's attributes; its unexported fields are its state; its methods are its
event handlers.

```html
<!-- counter.vuka.html -->
package app

import (
	"context"

	"github.com/vuka-lang/ui"
)

type Counter struct {
	ui.Live
	Start int // a prop: <Counter start={5} />
	n     int // state
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }
<div><span>{n}</span><button onClick={Inc}>+</button></div>
```

The tag works in any <span class="vuka-only">JSX</span><span class="go-only">template; in Go, `&Counter{Start: 5}` in a `ui.HTML` hole is the same</span>. Attributes bind to the exported fields by name
(`start` and `Start` both find `Start`; promoted fields too, but not
`ui.Live` itself), children go to a `Children ui.Node` field, and `key`
names the instance among the tags it is rendered with. Naming an unexported
field is a compile error that says it is state. The compiler checks that
`*Counter` implements `ui.Stateful`; a struct missing `ui.Live` is told to
embed it.

## Static and live

Rendered by `ui.String`, `ui.Handler` or any templ code, a stateful
component is plain HTML: a copy of the tag's value is mounted, rendered once,
and its event attributes write nothing. That is the first, HTTP render of a
page.

Under a [live session](/ui/live) it is an **instance**: created the first
time its tag renders, mounted once, then kept between renders with its
state. An instance not rendered any more is dropped, after its optional
`Unmount()` runs.

### Whose fields

An instance's unexported fields are always its own. Its exported fields
belong to whoever sets them:

| | exported fields |
|---|---|
| a component a tag renders | the parent's: each render of the tag sets them again, so the component changes only its unexported ones (a `bind:` field it sets is written to the tag's value too, until the parent renders the tag with another) |
| a routed live page (<span class="vuka-only">`@web.Live`</span><span class="go-only">`web.Routes.Live`</span>) | the page's own: built for its URL when it mounts, then changed only by the page — its handlers, hooks, `Update` methods and `bind:` fields — and kept as it changes them, so `p.Reply = ""` in a submit's handler clears the input |
| that page on a [patch](/web/navigation) | the fields its URL binds (path segments and query parameters, as when it mounted) are set from the new URL before its `Update` takes it; the others stay the page's |

In the browser a field shows the server's value when that changes: a field
with focus too, when it still shows what it showed when the page last sent
an event (a form cleared once sent); one the user has typed into since keeps
what is typed.

| Method | |
|---|---|
| `Render() ui.Node` | required; its output gets the instance's id as `data-vk-id` on its root element, or in a `<vk-c style="display:contents">` around it when it isn't one element; a tag with a `key` puts it there too, as `data-vk-key` |
| `@ui.Mount` | optional, once per instance, before its first render; an error fails the render. The method the attribute marks ([Hooks](#hooks)) |
| `Unmount()` | optional, when the instance is dropped |
| <span class="vuka-only">`Update(msg T)`</span><span class="go-only">`UpdateT(msg T)`</span> | optional<span class="vuka-only">, overloadable</span>: the messages the session delivers (below) |

`ui.Live` gives the component `ID()` (its instance id, `""` when static),
`Subscribe(topics...)` (call it in the mount hook), `PushJS(chain)` ([from the
server](/ui/js#from-the-server)), and `PushPatch(href)` and
`PushNavigate(href)`, which move the page to another URL once the reply is
applied ([navigation](/web/navigation)), and `PushEvent(name, payload)`,
which hands `payload` to the component's [scripts](/web/assets#scripts)
(`ctx.on(name, fn)`). Embed it by value: a
`*ui.Live` fails the render.

## Which instance?

A tag's place in its parent's render: the component rendering it, the keys
of the elements around it, the tag itself (each tag in the source is its own
place), and its `key`, or else how many times that tag has rendered before
it in this render. So `{if}` around one tag never shifts another's
instance, a loop without keys pairs instances by position, and a loop with
keys follows them:

::: vuka
```vuka
{for _, it := range t.items {
	<li key={it.ID}><Counter label={it.Name} /></li>   // or <Counter key={it.ID} … />
}}
```
:::

::: go
```html
for _, it := range items {
  <li key={it.ID}><Counter label={it.Name} /></li>   {/* or <Counter key={it.ID} … /> */}
}
```
:::

Two tags in one place with the same key fail the render. Components nest
freely: a stateful component renders others, each its own instance with its
own state, and a parent passes a child func props to hear from it.

## Events

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

Any of them may take a leading `context.Context` (the session's) and may
return an `error`. The compiler checks that the value is a function; the
payload rules are checked when the element first renders, which fails with
the expected shapes for any other signature (`ui.CheckHandler` is the
check). A handler's error doesn't stop the session: the page renders again
(the handler may have changed state before failing) and the reply carries
the message. A chain of [JS commands](/ui/js) runs in the browser instead
(and calls handlers with `Push`); a `templ.ComponentScript` is still a script.

::: vuka
```vuka
type NewItem struct{ Name string `form:"name"` }

func (t *Todo) Add(ctx context.Context, f NewItem) error {
	if f.Name == "" {
		return errors.New("a name, please")
	}
	t.items = append(t.items, f.Name)
	return nil
}

<form onSubmit={t.Add}><input name="name" onInput={t.SetQuery} /></form>
```
:::

::: go
```go
type NewItem struct{ Name string `form:"name"` }

func (t *Todo) Add(ctx context.Context, f NewItem) error {
	if f.Name == "" {
		return errors.New("a name, please")
	}
	t.items = append(t.items, Item{ID: len(t.items) + 1, Name: f.Name})
	return nil
}
```

```html
<form onSubmit={Add}><input name="name" onInput={SetQuery} /></form>
```
:::

Under a session each handler renders as `data-vk-on-click="c2:0"`: the
instance's id and the handler's index in that instance's render. The
handlers of elements outside any stateful component belong to the page
itself, `c0`. Without a session the attribute isn't written.

After any event the session renders the page again — so a handler may change
any state it can reach, a parent's through a func prop included — and
replies with what changed.

## Hooks

The methods the runtime calls on a component are its hooks, each declared
with an attribute on the method:

| Attribute | Runs | Signature |
|---|---|---|
| `@ui.Mount` | once per instance, before its first render | `func(ctx context.Context) error` |
| `@ui.OnChange` | after the component sets bound fields; `c` says which | `func(ctx context.Context, c ui.Changed) error` |
| `@ui.Loads("group")` | after the first render, loading a group of [deferred fields](/web/islands#deferred-fields) | `func(ctx context.Context) error` |

Fields take attributes too: `@ui.Bind` (the component may set it),
`@ui.URL("q")` (the page's address keeps it), `@ui.Defer("group")` (loaded
after the first render), and the rules of package `validate`.

```html
<!-- dashboard.vuka.html -->
package app

import (
	"context"
	"time"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/ui/validate"
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
<input bind:value={Q} />
```

A hook may be unexported, and usually is: the browser calls a component's
exported methods, never its hooks. A method is a hook only when an
attribute marks it — a method named `Mount` or `Changed` that none marks is
an error ([E0337](/errors/E0337)), as is a hook declared by a constructor
on its `ui.Live` ([E0338](/errors/E0338)), the forms of earlier releases;
`vuka fix hooks` adds the attributes to `Mount`, `Changed` and
`Load<Group>` methods whose signatures fit.

The compiler writes the attributes into the component's `LiveSchema`
method, `l.Mount(d.mount)`, `l.Field(&d.Q, ui.Bind{…})`, …, so a hook's
signature is checked by the Go compiler. `ui.LiveSchema` is that generated
code's, not an API to write: `vukavet` reports a hand-written one
([W0306](/errors/W0306)). A deferred group without a loader
([E0324](/errors/E0324)) or a hook that doesn't fit ([E0336](/errors/E0336))
stops the app at start, and `vukavet` reports both.

## Messages

A component subscribed to a topic receives what the transport publishes on
it, through `Update` methods taking it — overloads included, each picked by
the message's dynamic type:

::: vuka
```vuka
@ui.Mount
func (c *Chat) mount(ctx context.Context) error      { c.Subscribe("room:" + c.Room); return nil }
func (c *Chat) Update(m NewMessage)                  { c.log = append(c.log, m.Text) }
func (c *Chat) Update(ctx context.Context, t Typing) { c.typing = t.Who }
```
:::

::: go
```go
// Its mount hook subscribes, as on the Vuka side.
func (c *Chat) UpdateNewMessage(m NewMessage)              { c.log = append(c.log, m.Text) }
func (c *Chat) UpdateTyping(ctx context.Context, t Typing) { c.typing = t.Who }
```
:::

An `Update` takes an optional `context.Context` and the message, and returns
nothing or an error. One taking the message's type exactly wins over one it
is assignable to, and those over one taking `any`. <span class="vuka-only">In Go</span><span class="go-only">Each</span> <span class="vuka-only">an overload</span><span class="go-only">such method</span> is
`Update` followed by the message type's name (`ui.TypeName`: `UpdateOrders`
for `[]Order`, `UpdateAny` for `any`), what vuka names `.vuka`'s overloads;
`ui.IsUpdate` tells one, and the browser can't call it.

## Change tracking

A `.vuka.html` file's struct tracks its server state: each unexported field
that isn't a `ui.State` or a dependency (a pointer, an interface, a func, a
chan) — and isn't tagged `` `vuka:"-"` `` — is tracked, its writes marked.
A component whose state fields are all tracked is rendered again only when
one of them was written or a prop changed: otherwise the session keeps its
last render, its handlers and the instances it rendered, and calls no
`Render` — instances inside it still render when their own state changed.
Props compare by value; a pointer, slice, map, func or interface prop
(children included) counts as changed unless both are nil.

```html
<!-- row.vuka.html -->
package app

type Row struct {
	ui.Live
	ID   int    // props
	Name string
	qty  int    // state, tracked
}

func (r *Row) Inc() { r.qty++ }
<tr key={ID}><td>{Name}</td><td>{qty}</td><td><button onClick={Inc}>+</button></td></tr>
```

The field is read and written as any field is — `r.qty++`, `r.rows =
append(r.rows, x)`, `r.rows[i].Done = true`, `r.employer.Addr.City` in a
method or `{employer.Label()}` in the markup, `r.employer.Name = x` — and
the compiler writes it as a `uigen.Assign` of its type, each read a `Get`,
each write a `Set` or an `Update`; a method with a pointer receiver called
as a statement (`r.employer.Rename(x)`) is a write too. Taking a tracked
field's address, or calling such a method inside an expression, is
[E0150](/errors/E0150): a change through the pointer wouldn't be seen.

A click in one row of a thousand renders that row only. `Render` must read
nothing but props, tracked fields and `vuka:"-"` fields that don't change
its output; [verification](/ui/live#verification) catches one that does.

## What it becomes

<!-- go:fragment -->
```go
counter := &Counter{Label: it.Name}
remove := func() { t.Remove(it.ID) }
li := ui.HTML(`<li>{counter} <button onClick={remove}>x</button></li>`, counter, remove)
```

The tag is a hole of a `ui.HTML` call, and the hole's place — the call's
file and line, and the hole — tells the session which tag an instance
comes from; `ui.Key(k, &Counter{…})` gives it a key.
