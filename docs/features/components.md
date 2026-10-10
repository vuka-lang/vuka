# Components and JSX

**Go:** HTML comes from `html/template`, string building, or a template
language with its own files and its own generator.

**Vuka adds** JSX: markup is an expression in a `.vuka` file, and a component is
a function returning a `vuka.Node`.

```vuka
type Pet struct {
	Name string
	Age  int
}

func PetRow(pet Pet) vuka.Node {
	return <tr><td>{pet.Name}</td><td>{pet.Age}</td></tr>
}

func Pets(pets []Pet, admin bool) vuka.Node {
	return <section className="pets">
		<h1>Pets ({len(pets)})</h1>
		<table>
			{for _, p := range pets {
				<PetRow pet={p} />
			}}
		</table>
		{if admin { <button>Add a pet</button> }}
	</section>
}
```

Rendering is the runtime's job, and it is built on
[templ](https://templ.guide): `vuka.Node` is `templ.Component`, so a Vuka
component works anywhere a templ component does, and the other way round.

```vuka
http.Handle("/pets", vuka.Handler(func(r *http.Request) (vuka.Node, error) {
	return Pets(store.All(), true), nil
}))
```

## Markup

| JSX | |
|---|---|
| `<div a="x">` | a string attribute; HTML entities (`&amp;`) are decoded |
| `<div a={expr}>` | any Go expression |
| `<input checked>` | `true`: written bare |
| `className`, `htmlFor` | `class` and `for`, as in React |
| `{expr}` | a child: a `vuka.Node` as is, a string or number as text, an `error` as its message, a `[]vuka.Node` as each node, `nil` as nothing |
| `<>…</>` | a fragment: children with no element around them |
| `{/* … */}` | a comment |

Text is escaped, and whitespace follows React: text on one line keeps its
spaces, a line break between text and a tag disappears, and lines of text are
joined by one space.

An attribute's value renders the way templ renders it: `nil` and `false` leave
it out, `class` takes anything `templ.Classes` does, `style` takes a
`vuka.Style{"color": "red"}`, and `href` on `<a>`, `action` on `<form>` are
checked as URLs (a `javascript:` URL becomes a harmless placeholder; wrap a
trusted one in `templ.SafeURL`). `vuka.Safe(html)` writes HTML unescaped.

## Blocks

`for`, `if` and `match` go inside braces, and their bodies are markup:

```vuka
<ul>
	{for _, u := range users {
		<li key={u.ID}>{u.Name}</li>
	}}
</ul>
{if len(users) == 0 {
	<p>nobody yet</p>
} else {
	{len(users)} users
}}
{match lookup(id) {
case Ok(u):
	<p>{u.Name}</p>
case Err(e):
	<p className="error">{e}</p>
}}
```

`match` is the [same match](/features/match), exhaustive.

Inside a block's body, a block nests without its braces: a body is already
in braces, so `if`, `for` and `match` starting a child there open a nested
block, with the same headers and `else if`/`else` chains:

```vuka
{for _, it := range items {
	if it.Done {
		<li className="done">{it.Name}</li>
	} else {
		<li>{it.Name}</li>
	}
	for _, t := range it.Tags { <i>{t}</i> }
	match it.Owner {
	case Some(o):
		<b>{o}</b>
	case None:
		<i>unassigned</i>
	}
}}
```

A word starts a nested block when it is `if` or `for` followed by a space or
`(`, or `match` followed by an expression, at the start of a child: after the
body's `{`, or after another child. Anywhere else it is text: in an element
(`<p>if you like</p>`), in the middle of text, or not followed that way
(`iffy`, `if-then`). Write `{"if"}` for the word itself at the start of a
child in a body. Only `if`, `for` and `match` nest; other Go statements
still go in a function literal.

`key` names an element among its siblings, for [stateful
components](#stateful-components) — the instances inside a keyed element
follow its key when the list is reordered — and for the live runtime's
morph, which pairs keyed rows by it. Under a live session it is written as
`data-vk-key="…"` (its value as `fmt.Sprint` prints it); rendered statically
it is never written to the HTML.

The Go inside braces is Vuka too: a function literal there takes `?` and
`match` like any other function, at any depth.

```vuka
{vuka.Try(func() (vuka.Node, error) {
	n := strconv.Atoi(s)?
	return <b>{n}</b>, nil
}())}
```

## Components

A tag that starts with a capital letter, or has a dot (`<theme.Card>`), is a
function or method call:

```vuka
func UserCard(user User, admin bool, children vuka.Node) vuka.Node { … }

<UserCard user={u} admin>since {u.Year}</UserCard>
```

- Attributes bind to **parameters by name**, in any order; `user` and `User`
  both find a parameter `user`. A parameter left out gets its zero value.
- Children go to a parameter named `children`. A component of your own with
  none takes no children: `<Layout title="x"><p>…</p></Layout>` for `func
  Layout(title string) vuka.Node` is an error, not children silently dropped.
- A function taking **one struct**, or a variadic of one (`func Button(props
  ...ButtonProps)`, templUI's shape), takes the attributes as its fields, and
  the children as a `Children` field. An attribute naming the parameter itself
  passes the struct whole: `<PetRow pet={p}/>` for `func PetRow(pet Pet)`.
- A component returning `(vuka.Node, error)` renders its error: the page fails
  to render with that error, unless a `vuka.ErrorBoundary` above it shows a
  fallback instead.
- A **generic** component's type arguments are inferred from its attributes,
  as a call's are from its arguments, or given on the tag: `<List[User] …>`. A
  generic props struct needs them given.
- An **overloaded** component's overload is the one its attributes' types fit
  best, as for a call.
- A props struct's **promoted fields** are attributes too: a struct embedding
  `Base{ID int}` takes `id={…}`, written into the literal as `Base: Base{ID: …}`
  (`&Base{…}` for an embedded pointer). A field two embedded structs both have
  at the same depth is ambiguous, as in Go, and an error.

```vuka
func List[T any](items []T, render func(T) vuka.Node) vuka.Node { … }

<List items={users} render={func(u User) vuka.Node { return <li>{u.Name}</li> }} />
```

Mistakes are compile errors at the tag:

```text
main.vuka:12:14: <UserCard> has no attribute usr; did you mean user?
main.vuka:20:3: expected </p> to close <p> at line 18, found </div>
```

## templ, both ways

`.templ` files sit in the same packages as `.vuka` and `.go` files. Vuka
compiles them with templ's own parser and generator, in memory, so there is no
`templ generate` step and nothing extra to commit. Their errors and go to
definition point into the `.templ` file.

```templ
// layout.templ
package main

templ Shell(title string) {
	<main><header>{ title }</header>{ children... }</main>
}

templ Footer(pets []Pet) {
	@PetRow(pets[0])        // a Vuka component, called from templ
}
```

```vuka
// pets.vuka
func Page(pets []Pet) vuka.Node {
	return <Shell title="Pets">        // a templ component, called from JSX
		<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
		<Footer pets={pets} />
	</Shell>
}
```

A templ component has no `children` parameter: JSX children reach its
`{ children... }` (through `templx.WithChildren`). So do the children of a
component from another package, which may be a templ library: templUI works as
it is.

## Rendering

| | |
|---|---|
| `vuka.Handler(f)` | an `http.Handler` rendering the node `f` builds per request |
| `vuka.Write(w, r, n)` | render into a response, all at once, so an error still gets a clean 500 |
| `vuka.String(ctx, n)` | render to a string |
| `n.Render(ctx, w)` | templ's own method, to any `io.Writer` |

A JSX tree is a value you can walk, not only HTML: `vuka.Walk` hands its
elements, text and raw HTML to a `vuka.Renderer`. The package
`github.com/vuka-lang/vuka/term` renders the same components as terminal text:
headings, lists, aligned tables, wrapped paragraphs and optional color.

```vuka
term.Render(ctx, os.Stdout, Pets(pets, false), term.Options{Width: 80, Color: true})
```

`term` reads the HTML of templ components too, so a page inside a templ
layout keeps its headings, tables and lists.

## What it becomes

Calls to the runtime, with your expressions where you wrote them:

```go
return vuka.El("section", []vuka.Attr{{Name: "className", Value: "pets"}},
	vuka.El("h1", nil, vuka.Text("Pets ("), vuka.Child(len(pets)), vuka.Text(")")),
	vuka.El("table", nil, vuka.Nodes(func(__add func(vuka.Node)) {
		for _, p := range pets { __add(PetRow(p)) }
	})),
	…)
```

`vuka explain` shows it line by line.

## Stateful components

A struct embedding `vuka.Live`, whose pointer has a `Render() vuka.Node`
method, is a stateful component. Its exported fields are its props, set from
the tag's attributes; its unexported fields are its state; its methods are its
event handlers.

```vuka
type Counter struct {
	vuka.Live
	Start int // a prop: <Counter Start={5} />
	n     int // state
}

func (c *Counter) Mount(ctx context.Context) error { c.n = c.Start; return nil }

func (c *Counter) Inc() { c.n++ }

func (c *Counter) Render() vuka.Node {
	return <div><span>{c.n}</span><button onClick={c.Inc}>+</button></div>
}

<Counter Start={5} />
```

The tag works in any JSX. Attributes bind to the exported fields by name
(`start` and `Start` both find `Start`; promoted fields too), children go to a
`Children vuka.Node` field, and `key` names the instance among the tags it is
rendered with. Naming an unexported field is an error that says it is state.

### Rendering, static and live

Rendered by `vuka.String`, `vuka.Handler` or any templ code, a stateful
component is plain HTML: a copy of the tag's value is mounted, rendered once,
and its event attributes write nothing. That is the first, HTTP render of a
page.

Under a live session (package `github.com/vuka-lang/vuka/live`) it is an
**instance**: created the first time its tag renders, mounted once, then kept
between renders with its state. Each render of the parent sets its props
again — exported fields belong to the parent; a component changes only its
unexported ones. An instance not rendered any more is dropped, after its
optional `Unmount()` runs.

| Method | |
|---|---|
| `Render() vuka.Node` | required; its output gets the instance's id as `data-vk-id` on its root element, or in a `<vk-c style="display:contents">` around it when it isn't one element; a tag with a `key` puts it there too, as `data-vk-key` |
| `Mount()` | optional, once per instance, before its first render: `Mount()`, `Mount() error`, `Mount(ctx)`, `Mount(ctx) error`; an error fails the render |
| `Unmount()` | optional, when the instance is dropped |
| `Update(msg T)` | optional, overloadable: the messages the session delivers (below) |

`vuka.Live` gives the component `ID()` (its instance id, `""` when static)
and `Subscribe(topics...)` (call it in `Mount`).

**Which instance?** A tag's place in its parent's render: the component
rendering it, the keys of the elements around it, the tag itself (each tag in
the source is its own place), and its `key`, or else how many times that tag
has rendered before it in this render. So `{if}` around one tag never shifts
another's instance, a loop without keys pairs instances by position, and a
loop with keys follows them:

```vuka
{for _, it := range t.items {
	<li key={it.ID}><Counter Label={it.Name} /></li>   // or <Counter key={it.ID} … />
}}
```

Two tags in one place with the same key fail the render.

### Events

An `on…` attribute with an expression on an element is an event handler:
`onClick` handles `click`, `onKeyDown` `keydown` — any `onXxx`, lowercased
after `on`. The expression is any function value: a method value, a closure
(Go's per-iteration loop variables make `onClick={func() { t.Remove(it.ID) }}`
right in a loop), a func prop passed down. What it is passed follows from its
type, checked at compile time:

| Handler | Gets |
|---|---|
| `func()` | nothing |
| `func(string)` | the value: for a keyboard event the key (`"Enter"`), else the element's value |
| `func(bool)`, `func(int)`, `func(float64)`, … | the value, parsed: a checkbox sends `"true"`/`"false"`, `"on"` is true; a bad number is the handler's error |
| `func(vuka.Event)` | the whole event: `Type`, `Value`, `Key`, `Form` |
| `func(url.Values)` | the form's fields as sent (any `map[string][]string`) |
| `func(T)`, `func(*T)` with `T` a struct | the form's fields bound to `T`'s: a field is named by its `form` tag, else its `json` tag, else its name, matched ignoring case; embedded structs' fields count as `T`'s; a slice takes every value |

Any of them may take a leading `context.Context` (the session's) and may
return an `error`. A handler's error doesn't stop the session: the page
renders again (the handler may have changed state before failing) and the
reply carries the message. A `templ.ComponentScript` is still a script.

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
instance's id and the handler's index in that instance's render. The handlers
of elements outside any stateful component belong to the page itself,
`c0`. Without a session the attribute isn't written.

After any event the session renders the whole page again — so a handler may
change any state it can reach, a parent's through a func prop included — and
replies with the components whose HTML changed: each changed instance,
outermost first, a child's change not repeated inside a changed parent.
Instances are compared by their own HTML, children excluded, so a click in
one counter of a hundred sends one counter.

### Messages

A component subscribed to a topic receives what the transport publishes on
it, through `Update` methods taking it — overloads included, each picked by
the message's dynamic type:

```vuka
func (c *Chat) Mount()                                { c.Subscribe("room:" + c.Room) }
func (c *Chat) Update(m NewMessage)                   { c.log = append(c.log, m.Text) }
func (c *Chat) Update(ctx context.Context, t Typing) { c.typing = t.Who }
```

An `Update` takes an optional `context.Context` and the message, and returns
nothing or an error. One taking the message's type exactly wins over one it
is assignable to, and those over one taking `any`.

### The session

`live.Session` is one connected page, with no transport of its own:

```go
s := live.NewSession(ctx, func(ctx context.Context) vuka.Node { return <App user={u} /> })
html, err := s.Render()                               // the container's HTML
p, err := s.Event(vuka.Event{Target: "c2:0", Type: "click"})
p, err := s.Info("room:go", NewMessage{"hi"})          // "" reaches every instance
topics := s.Topics()                                  // what to route to Info
s.Close()                                             // unmounts everything
```

A `live.Patch` is `Updates []live.Update{ID, HTML}` and `Error`, the handler's
message. The `error` results are the session's own: an unknown handler, a
failed render. Calls are safe from several goroutines and run one at a time.
`ctx` is the context of every render, `Mount`, handler and `Update`.

### The wire protocol

A browser runtime and a WebSocket transport (a library's job, not the
language runtime's) speak JSON, one message per frame; `live.ClientMessage`,
`live.ServerMessage` and `Session.Handle`/`HandleJSON` implement the server
side.

```text
→ {"type":"join"}
← {"type":"render","html":"<main>…</main>"}
→ {"type":"event","ref":1,"target":"c2:0","event":"click"}
← {"type":"patch","ref":1,"updates":[{"id":"c2","html":"<div data-vk-id=\"c2\">…</div>"}]}
→ {"type":"event","ref":2,"target":"c1:0","event":"submit","form":{"name":["milk"]}}
← {"type":"patch","ref":2,"updates":[…],"error":"a name, please"}
← {"type":"patch","updates":[…]}                                   a broadcast
← {"type":"error","ref":3,"error":"live: no handler \"c9:0\""}
```

| | |
|---|---|
| `join` | the first message: the server renders the page; the reply is `render` |
| `event` | `target` is the handler's id from its `data-vk-on-…`; `event` the DOM event; `value` the element's value (a checkbox's `checked` as `"true"`/`"false"`); `key` a keyboard event's key; `form` a submit's fields (`FormData`, each name to its values) |
| `render` | `html` replaces the page container's content |
| `patch` | for each update, the element whose `data-vk-id` is `id` is morphed into `html`; `id` `"c0"` is the page itself: its `html` replaces the container's content. The morph matches `data-vk-key` among siblings, so keyed rows move instead of being rewritten. `error`, when set, is a handler's message, for the page to show |
| `error` | the message failed (an unknown handler, a render error); the session goes on |
| `redirect` | reserved: navigate to `url` |

`ref` is any number the client picks; the reply to a message carries it, and
a broadcast has none. The runtime listens, delegated, for each event type a
`data-vk-on-<event>` names, sends the nearest such element's id, and
`preventDefault`s a submit. Ids are per render: after a patch, use the new
HTML's. On reconnect the client joins again — state lives in the server's
session, so a transport that keeps sessions across short disconnects keeps
state; one that doesn't starts the page over, as the HTTP render did.

The first HTTP render is static (a fresh mount, no ids); the `join` mounts
the page's live instances, LiveView-style.

### Protocol v2: render trees

A client that sends `{"type":"join","v":2}` speaks version 2: the server
sends each live instance's **render tree** instead of its HTML, and after an
event only what changed in it. A `join` without `v` (or `"v":1`) gets the v1
messages above, unchanged. The format is nexus's live-view render tree
(LiveView's statics and dynamics), adapted to Vuka's per-instance model; the
differences are listed at the end.

**The tree.** The compiler knows which parts of a JSX expression are fixed
markup and which are Go expressions. A *frame* is the markup of one piece of
JSX: its *statics*, the fixed strings, around its *dynamics*, what the
expressions rendered — `len(s) == len(d) + 1`, and the frame's HTML is
`s[0] + d[0] + s[1] + … + d[n-1] + s[n]`. A dynamic is one of

| JSON | |
|---|---|
| `"…"` | markup: text, an attribute (with its leading space and name, `" class=\"on\""`, or `""` when omitted), or anything rendered opaquely (a hand-written `vuka.El` tree, a templ component) |
| `{"t":id,"s":[…],"d":[…]}` | a nested frame: a JSX value in an `{expr}`, a component tag's children. `t` is its statics' id; `s` the statics themselves, present the first time the connection meets that id and omitted after |
| `{"c":[frame,…]}` | a block, `{for}`, `{if}` or `{match}`: one item frame per loop iteration; an `{if}`/`{match}` has zero items or one, whose statics id says which branch |
| `{"r":id,"v":"…"}` | markup of 64 bytes or more, which the connection keeps under `id` (a space of its own, not the statics'); later the same markup is `{"r":id}` |
| `{"id":"c3"}` | a stateful component instance: its HTML is that instance's tree's HTML |

Statics ids and string ids are numbers the server allocates per connection
(0, 1, 2, … in order of first use); a `render` starts both afresh. The
client keeps a table of each.

**Messages.**

```text
→ {"type":"join","v":2}
← {"type":"render","v":2,"trees":[{"id":"c0","full":true,"tree":FRAME}, {"id":"c1","full":true,"tree":FRAME}, …]}
→ {"type":"event","ref":1,"target":"c1:0","event":"click"}
← {"type":"patch","ref":1,"trees":[{"id":"c1","tree":CHANGE}]}
← {"type":"patch","trees":[…]}                                    a broadcast
← {"type":"patch","ref":2,"trees":[…],"error":"a name, please"}
```

`trees` holds one entry per instance whose tree changed, in render order
(a parent before the instances it renders):

| Entry | |
|---|---|
| `{"id":…,"full":true,"tree":FRAME}` | the instance's whole tree (its first render on this connection, or a fresh mount) |
| `{"id":…,"tree":{"u":…}}` | the change to the tree the client holds for it |
| `{"id":…,"html":"…"}` | the instance's whole HTML, sent when it is smaller than the change; only for an instance rendering no other instance. The client holds that HTML for it until a later `full` entry |

`render` lists every instance, the page `c0` first; its HTML (with each
`{"id"}` replaced by that instance's HTML) replaces the container's content.
On a `patch` the client first applies every entry to its trees, then morphs
the updated instances outermost first, as v1 does — the element whose
`data-vk-id` is the entry's `id` into that instance's new HTML, `c0` the
container's content — skipping an entry whose element is inside one it has
already morphed in this message (that morph already carried it). Instances no
longer reached from `c0` can be dropped. A component's root carries
`data-vk-id` (and `data-vk-key`) as a dynamic of its root frame, so the HTML a
client renders from a tree is the HTML v1 would have sent.

**Changes.** A change `{"u":{"i":X,…}}` names the dynamics of a frame that
changed, by index; the others stay. X is

| X | |
|---|---|
| `"…"`, `{"r":…}` | the dynamic's new markup |
| a frame `{"t":…}` or loop `{"c":…}` or `{"id":…}` | replaces the dynamic |
| `{"u":…}` | the dynamic is a frame with the same statics; change it |
| `{"k":[step,…]}` | the dynamic is a loop: build its new items from the old ones |
| `{"p":[step,…]}` | the dynamic is markup of 1024 bytes or more: a token patch against it (below) |

Loop steps run over the old items with a cursor starting at 0, appending to
the new list:

| Step | |
|---|---|
| `n` (> 0) | the next `n` old items, unchanged |
| `-n` | skip the next `n` old items |
| a frame `{"t":…}` | a new item |
| `{"u":…}` | the next old item, changed |
| `{"m":i}`, `{"m":i,"u":…}` | the old item at index `i` (counting from 0 in the old list; the cursor doesn't move), unchanged or changed: a keyed item that moved |

Old items no step takes are gone. Items pair by `key` when the body's first
element (or component tag) has one, else by position.

A token patch is nexus's: both strings cut before every `<` and after every
`>`, `"` and `&#34;`; steps `n` copy the next `n` old tokens, `-n` skip them,
`"…"` inserts text, `[p,n]` inserts `n` tokens copied from position `p` of the
old string. (Tree token patches use no dictionary.)

**An example.** A page rendering a table and a counter:

```vuka
type Table struct {
	vuka.Live
	rows []Row // Row{ID int; Name string; Qty int}
	sel  int
}

func (t *Table) Render() vuka.Node {
	return <table>
		{for _, r := range t.rows {
			<tr key={r.ID}><td>{r.Name}</td><td>{r.Qty}</td></tr>
		}}
		{if t.sel > 0 { <p>row {t.sel}</p> } else { <p>none</p> }}
	</table>
}

func (c *Counter) Render() vuka.Node {
	return <span>{c.Label}: {c.n} <button onClick={c.Inc}>+</button></span>
}

func page(ctx context.Context) vuka.Node {
	return <main><Table /><Counter Label="solo" /></main>
}
```

The join's reply, with two rows (whitespace added):

```json
{"type":"render","v":2,"trees":[
 {"id":"c0","full":true,"tree":{"t":0,"s":["<main>","","</main>"],"d":[{"id":"c1"},{"id":"c2"}]}},
 {"id":"c1","full":true,"tree":{"t":1,"s":["<table",">","","</table>"],"d":[
   " data-vk-id=\"c1\"",
   {"c":[{"t":2,"s":["<tr","><td>","</td><td>","</td></tr>"],"d":[" data-vk-key=\"1\"","apples","3"]},
         {"t":2,"d":[" data-vk-key=\"2\"","pears","5"]}]},
   {"c":[{"t":3,"s":["<p>none</p>"],"d":[]}]}]}},
 {"id":"c2","full":true,"tree":{"t":4,"s":["<span",">",": "," <button",">+</button></span>"],
   "d":[" data-vk-id=\"c2\"","solo","0"," data-vk-on-click=\"c2:0\""]}}]}
```

The second `<tr>` sends no statics: id 2 is known. c1's HTML is
`<table data-vk-id="c1"><tr data-vk-key="1"><td>apples</td><td>3</td></tr><tr data-vk-key="2"><td>pears</td><td>5</td></tr><p>none</p></table>`.

One cell changes (pears' quantity to 6): the change reaches dynamic 1 of
c1's frame (the loop), keeps the first item and changes the second's
dynamic 2:

```json
{"type":"patch","ref":1,"trees":[{"id":"c1","tree":{"u":{"1":{"k":[1,{"u":{"2":"6"}}]}}}}]}
```

A keyed insert, removal and move: the rows go from keys 1 2 3 4 to 4 1 5 3
(4 moves to the front, 2 is removed, 5 is new):

```json
{"u":{"1":{"k":[{"m":3},1,-1,{"t":2,"d":[" data-vk-key=\"5\"","plums","1"]},1]}}}
```

— old item 3 (key 4) first, then from the cursor: key 1 kept, key 2 skipped,
the new row, key 3 kept; key 4 is not taken again at the cursor and the list
ends there.

A branch switch: `t.sel` becomes 2, so the `{if}` renders its other branch,
whose statics are new to the connection:

```json
{"u":{"2":{"c":[{"t":5,"s":["<p>row ","</p>"],"d":["2"]}]}}}
```

and back to 0 later: `{"u":{"2":{"c":[{"t":3,"d":[]}]}}}`. While the same
branch stays, changes are `{"k":[{"u":…}]}`.

A nested component's event: a click on the counter's button (`c2:0`)
changes only c2's tree; the page and the table aren't sent:

```json
{"type":"patch","ref":3,"trees":[{"id":"c2","tree":{"u":{"2":"1"}}}]}
```

When an event changes a parent and a child, both entries come, parent first;
a child the parent renders for the first time comes as `full` — and a removed
child is just no longer referenced.

**Differences from nexus.** The tree is per instance (`trees`, by `id`)
instead of one tree per page, and a frame dynamic may be an instance
reference `{"id":…}`; nexus's `tree`/`full`/`reset` fields become the entries'
`tree`/`full`, and a `render` is always a reset. Loops have a move step
`{"m":i}` for keyed items. Every block is a loop (`{"c":…}`), a branch being
a loop of zero or one item. An entry may be plain `html`. Handler ids
(`data-vk-on-click="c1:4"`) are dynamics like any attribute; they count a
component's handlers in render order, so inserting a row with handlers
changes the ids of the handlers after it, which then travel as changed
dynamics.

### What it becomes

```go
vuka.Component("todo.vuka#3", nil, &Counter{Label: it.Name, })
vuka.El("button", []vuka.Attr{{Name: "onClick", Value: vuka.On(func() { t.Remove(it.ID) })}, }, vuka.Text("x"), )
```

The site string is the file and the tag's ordinal among its component tags.

### Serving live pages

See also [Live components](/web/live): the web framework serves stateful
components as live pages over a WebSocket.

### Next

Patches today are a component's whole HTML, for the client to morph. The
compiler knows which parts of a tag's markup are static and which are Go
expressions, so the next step is LiveView's: render a component to its
statics (sent once) and dynamics, and send only the dynamics that changed;
`live.Update` is where that tree will go. Change tracking (rendering only the
components whose state changed instead of the whole page), streams, presence
and flash messages come after.

## Known limits

- A stateful component re-renders with its whole page on every event; cheap
  for ordinary pages, and the reply is still only what changed.
- A generic stateful component needs its type arguments on the tag:
  `<Box[int] Value={7} />`.
- JSX goes in function bodies and package-level variable initialisers, not
  in constant declarations or struct tags (Go allows neither an expression
  there).
