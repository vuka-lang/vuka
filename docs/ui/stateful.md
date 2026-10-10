# Stateful components

A struct embedding `ui.Live`, whose pointer has a `Render() ui.Node` method,
is a stateful component. Its exported fields are its props, set from the
tag's attributes; its unexported fields are its state; its methods are its
event handlers.

```vuka
type Counter struct {
	ui.Live
	Start int // a prop: <Counter Start={5} />
	n     int // state
}

func (c *Counter) Mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }

func (c *Counter) Render() ui.Node {
	return <div><span>{c.n}</span><button onClick={c.Inc}>+</button></div>
}

<Counter Start={5} />
```

The tag works in any JSX. Attributes bind to the exported fields by name
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
state. Each render of the parent sets its props again — exported fields
belong to the parent; a component changes only its unexported ones. An
instance not rendered any more is dropped, after its optional `Unmount()`
runs.

| Method | |
|---|---|
| `Render() ui.Node` | required; its output gets the instance's id as `data-vk-id` on its root element, or in a `<vk-c style="display:contents">` around it when it isn't one element; a tag with a `key` puts it there too, as `data-vk-key` |
| `Mount()` | optional, once per instance, before its first render: `Mount()`, `Mount() error`, `Mount(ctx)`, `Mount(ctx) error`; an error fails the render, and so does a `Mount` of another shape |
| `Unmount()` | optional, when the instance is dropped |
| `Update(msg T)` | optional, overloadable: the messages the session delivers (below) |

`ui.Live` gives the component `ID()` (its instance id, `""` when static)
and `Subscribe(topics...)` (call it in `Mount`). Embed it by value: a
`*ui.Live` fails the render.

## Which instance?

A tag's place in its parent's render: the component rendering it, the keys
of the elements around it, the tag itself (each tag in the source is its own
place), and its `key`, or else how many times that tag has rendered before
it in this render. So `{if}` around one tag never shifts another's
instance, a loop without keys pairs instances by position, and a loop with
keys follows them:

```vuka
{for _, it := range t.items {
	<li key={it.ID}><Counter Label={it.Name} /></li>   // or <Counter key={it.ID} … />
}}
```

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
| `func(T)`, `func(*T)` with `T` a struct | the form's fields bound to `T`'s: a field is named by its `form` tag, else its `json` tag, else its name, matched ignoring case; embedded structs' fields count as `T`'s; a slice takes every value |

Any of them may take a leading `context.Context` (the session's) and may
return an `error`. The compiler checks that the value is a function; the
payload rules are checked when the element first renders, which fails with
the expected shapes for any other signature (`ui.CheckHandler` is the
check). A handler's error doesn't stop the session: the page renders again
(the handler may have changed state before failing) and the reply carries
the message. A `templ.ComponentScript` is still a script.

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

Under a session each handler renders as `data-vk-on-click="c2:0"`: the
instance's id and the handler's index in that instance's render. The
handlers of elements outside any stateful component belong to the page
itself, `c0`. Without a session the attribute isn't written.

After any event the session renders the page again — so a handler may change
any state it can reach, a parent's through a func prop included — and
replies with what changed.

## Messages

A component subscribed to a topic receives what the transport publishes on
it, through `Update` methods taking it — overloads included, each picked by
the message's dynamic type:

```vuka
func (c *Chat) Mount()                               { c.Subscribe("room:" + c.Room) }
func (c *Chat) Update(m NewMessage)                  { c.log = append(c.log, m.Text) }
func (c *Chat) Update(ctx context.Context, t Typing) { c.typing = t.Who }
```

An `Update` takes an optional `context.Context` and the message, and returns
nothing or an error. One taking the message's type exactly wins over one it
is assignable to, and those over one taking `any`.

## Change tracking

A state field may be a `ui.Assign[T]`, read with `Get()` and changed with
`Set(v)` or `Update(func(T) T)`. A component whose state fields are all
Assigns (fields tagged `` `vuka:"-"` `` don't count) is rendered again only
when one of them was set or a prop changed: otherwise the session keeps its
last render, its handlers and the instances it rendered, and calls no
`Render` — instances inside it still render when their own state changed.
Props compare by value; a pointer, slice, map, func or interface prop
(children included) counts as changed unless both are nil.

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

A click in one row of a thousand renders that row only. `Render` must read
nothing but props, Assigns and `vuka:"-"` fields that don't change its
output; [verification](/ui/live#verification) catches one that does.

## What it becomes

```go
ui.Component("todo.vuka#3", nil, &Counter{Label: it.Name, })
ui.El("button", []ui.Attr{{Name: "onClick", Value: ui.On(func() { t.Remove(it.ID) })}, }, ui.Text("x"), )
```

The site string is the file and the tag's ordinal among its component tags.
