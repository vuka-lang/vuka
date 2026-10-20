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

::: vuka
```vuka
type Counter struct {
	ui.Live
	Start int // a prop: <Counter start={5} />
	n     int // state
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }

func (c *Counter) Render() ui.Node {
	return <div><span>{c.n}</span><button onClick={c.Inc}>+</button></div>
}

<Counter start={5} />
```
:::

::: go
```go
// counter.go
type Counter struct {
	ui.Live
	Start int    // a prop: <Counter start={5} />, or &Counter{Start: 5} in Go
	Label string // a prop
	n     int    // state
}

func (c *Counter) LiveSchema(l *ui.LiveSchema) { l.Mount(c.mount) }

func (c *Counter) mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }
```

```html
<!-- counter.vuka.html: its Render -->
<div><span>{n}</span><button onClick={Inc}>+</button></div>
```
:::

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
| <span class="vuka-only">`@ui.Mount`</span><span class="go-only">`l.Mount(fn)`</span> | optional, once per instance, before its first render: a method `func(ctx context.Context) error`, which may be unexported; an error fails the render. A method named `Mount` (`Mount()`, `Mount() error`, `Mount(ctx)`, `Mount(ctx) error`) is the form of earlier releases, kept for one minor release (`vuka vet`: W0303, `vuka fix hooks`) |
| `Unmount()` | optional, when the instance is dropped |
| <span class="vuka-only">`Update(msg T)`</span><span class="go-only">`UpdateT(msg T)`</span> | optional<span class="vuka-only">, overloadable</span>: the messages the session delivers (below) |

`ui.Live` gives the component `ID()` (its instance id, `""` when static),
`Subscribe(topics...)` (call it in the Mount hook), `PushJS(chain)` ([from the
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

The methods the runtime calls are named in the component's `LiveSchema`
method, so they may be unexported and the browser can call only what isn't
one: `l.Mount` (`func(ctx context.Context) error`, before the first render),
`l.OnChange` (`func(ctx context.Context, c ui.Changed) error`, after the
component sets bound fields) and `l.Loads("group", fn)` (the loader of
[deferred fields](/web/islands#deferred-fields)); `l.Field(&d.X, …)` gives a
field its attributes (`ui.Bind`, `ui.URL`, `ui.Defer`, `validate.…`).<span class="vuka-only">
In a `.vuka` file they are hook and field attributes — `@ui.Mount`,
`@ui.OnChange`, `@ui.Loads("group")`, `@ui.Bind`, … — which become this
method:</span>

```go
func (d *Dashboard) LiveSchema(l *ui.LiveSchema) {
	l.Field(&d.Q, ui.Bind{Debounce: 300 * time.Millisecond}, ui.URL("q"), validate.Max(100))
	l.Field(&d.Rows, ui.Defer("lists"))
	l.Loads("lists", d.loadLists)
	l.OnChange(d.changed)
	l.Mount(d.mount)
}
```

A hook's signature is checked by the compiler (it is a method value); a
field that isn't the page's own (E0335) or a hook that doesn't fit
(E0336) stops the app at start, and `vukavet` reports both. Writing the
`LiveSchema` method and attributes too is E0129.

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
func (c *Chat) mount(ctx context.Context) error            { c.Subscribe("room:" + c.Room); return nil }
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

A state field may be a `ui.Assign[T]`, read with `Get()` and changed with
`Set(v)` or `Update(func(T) T)`. A component whose state fields are all
Assigns (fields tagged `` `vuka:"-"` `` don't count) is rendered again only
when one of them was set or a prop changed: otherwise the session keeps its
last render, its handlers and the instances it rendered, and calls no
`Render` — instances inside it still render when their own state changed.
Props compare by value; a pointer, slice, map, func or interface prop
(children included) counts as changed unless both are nil.

::: vuka
```vuka
type Row struct {
	ui.Live
	ID   int            // props
	Name string
	qty  ui.Assign[int] // state
}

func (r *Row) Inc() { r.qty.Update(func(n int) int { return n + 1 }) }

func (r *Row) Render() ui.Node {
	return <tr key={r.ID}><td>{r.Name}</td><td>{r.qty.Get()}</td><td><button onClick={r.Inc}>+</button></td></tr>
}
```
:::

::: go
```go
// row.go
type Row struct {
	ui.Live
	ID   int            // props
	Name string
	qty  ui.Assign[int] // state
}

func (r *Row) Inc() { r.qty.Update(func(n int) int { return n + 1 }) }
```

```html
<!-- row.vuka.html -->
package app

<tr key={ID}><td>{Name}</td><td>{qty.Get()}</td><td><button onClick={Inc}>+</button></td></tr>
```
:::

A click in one row of a thousand renders that row only. `Render` must read
nothing but props, Assigns and `vuka:"-"` fields that don't change its
output; [verification](/ui/live#verification) catches one that does.

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
