# Live protocol

For authors of transports and browser runtimes: how a
[`live.Session`](/ui/live) is driven, what travels on the wire, and the
attributes the HTML carries. The web framework's
[live pages](/web/live) are one implementation; the
[reference client](/ui/testing#stateful-components)
(`github.com/vuka-lang/ui/live/livetest`) is another, in Go.

## The session

```go
func NewSession(ctx context.Context, render func(ctx context.Context) ui.Node) *Session

func (s *Session) Render() (string, error)          // v1: the container's HTML, mounting what renders
func (s *Session) RenderTrees() ([]TreeUpdate, error) // v2: every instance's render tree; the session is v2 from then on
func (s *Session) HTML() string                      // the page as last rendered, without rendering again
func (s *Session) Event(ev ui.Event) (Patch, error)  // run a handler, render, diff
func (s *Session) Info(topic string, msg any) (Patch, error) // deliver a message to subscribed instances' Update
func (s *Session) Topics() []string                  // the topics instances subscribed to
func (s *Session) Close()                            // unmount everything

func (s *Session) Handle(m ClientMessage) ServerMessage // the protocol, as values
func (s *Session) HandleJSON(data []byte) []byte        // the protocol, as JSON
func Marshal(m ServerMessage) []byte
func PatchMessage(ref int, p Patch) ServerMessage        // a broadcast's patch (ref 0)
```

A `Patch` is `Updates []Update{ID, HTML}` (v1) or `Trees []TreeUpdate` (v2),
and `Error`, a handler's message. The `error` results are the session's own:
an unknown handler, a failed render. Calls are safe from several goroutines
and run one at a time. A transport keeps one session per connection, calls
`HandleJSON` for each message, routes `Topics()` to `Info`, and sends
`PatchMessage` for what `Info` returns.

## The HTML contract

| | |
|---|---|
| `data-vk-id="c2"` | an instance's root element (the page is `c0`, the container); a render that isn't one element is wrapped in `<vk-c data-vk-id="…" style="display:contents">` |
| `data-vk-key="…"` | an element's or instance's `key`, for pairing siblings in a morph |
| `data-vk-on-<event>="c2:0"` | a handler for the DOM event `<event>` (`click`, `keydown`, `submit`, …): the instance's id and the handler's index in its render |

A runtime listens, delegated, for each event type a `data-vk-on-<event>`
names, sends the nearest such element's handler id, and `preventDefault`s a
submit. Ids are per render: after a patch, use the new HTML's.

## Protocol v1

A browser runtime and a WebSocket transport speak JSON, one message per frame; `live.ClientMessage`,
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
| `join` | the first message: the server renders the page; the reply is `render`. `"v":2` asks for [version 2](#protocol-v2-render-trees) |
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

## Protocol v2: render trees

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
| `"…"` | markup: text, an attribute (with its leading space and name, `" class=\"on\""`, or `""` when omitted), or anything rendered opaquely (a hand-written `ui.El` tree, a templ component) |
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
(a parent before the instances it renders); a patch where nothing changed
has none:

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
	ui.Live
	rows []Row // Row{ID int; Name string; Qty int}
	sel  int
}

func (t *Table) Render() ui.Node {
	return <table>
		{for _, r := range t.rows {
			<tr key={r.ID}><td>{r.Name}</td><td>{r.Qty}</td></tr>
		}}
		{if t.sel > 0 { <p>row {t.sel}</p> } else { <p>none</p> }}
	</table>
}

func (c *Counter) Render() ui.Node {
	return <span>{c.Label}: {c.n} <button onClick={c.Inc}>+</button></span>
}

func page(ctx context.Context) ui.Node {
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
{"u":{"1":{"k":[{"m":3},1,{"t":2,"d":[" data-vk-key=\"5\"","plums","1"]},-1,1]}}}
```

— old item 3 (key 4) first, then from the cursor: key 1 kept, the new row,
key 2 skipped, key 3 kept; key 4, already taken, is left behind at the cursor
and the list ends there.

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


## JS commands

[JS commands](/ui/js) are DOM changes the browser makes on an event without
asking the server: show a menu, toggle a class, move focus. A runtime
implements them from two places: the `data-vk-js-<event>` attributes of the
HTML, and the `js` field of server messages. Neither needs a session — a
statically rendered page carries its commands too.

**On an element.** `data-vk-js-<event>` holds the commands to run when the
DOM event `<event>` fires (`click`, `input`, `keydown`, `submit`, … — the
lowercased name after `on` in the JSX attribute), as JSON (HTML-escaped, in
double quotes like every attribute):

```html
<button data-vk-js-click="[[&#34;remove_class&#34;,{&#34;to&#34;:{&#34;ref&#34;:&#34;menu&#34;},&#34;names&#34;:[&#34;hidden&#34;]}],[&#34;push&#34;,{&#34;h&#34;:&#34;c3:1&#34;}]]">Open</button>
```

that is `[["remove_class",{"to":{"ref":"menu"},"names":["hidden"]}],["push",{"h":"c3:1"}]]`:
a list of operations `[name, args]`, run in order. The runtime listens,
delegated, for each event type a `data-vk-js-<event>` names (as it does for
`data-vk-on-<event>`), runs the commands of the nearest element carrying
either attribute for that event, and `preventDefault`s a submit. An element
has one or the other for an event, never both. *this* is the element
carrying the attribute.

| Operation | Args | |
|---|---|---|
| `add_class`, `remove_class`, `toggle_class` | `to`, `names` | each class name in turn |
| `set_attr` | `to`, `name`, `value` | |
| `remove_attr` | `to`, `name` | |
| `toggle_attr` | `to`, `name`, `values` | `[a, b]`: sets `a` unless the attribute is `a`, then `b` |
| `show`, `hide`, `toggle` | `to`, `display`?, `transition`?, `time`? | the `display` declaration of the `style` attribute: `show` sets `display` (else removes the declaration, and sets `block` when the element is still hidden) and removes a `hidden` attribute; `hide` sets `display: none`; `toggle` shows a hidden element, else hides it. `transition` is `[during, from, to]`, three class lists (space-separated): `during` and `from` are added, then `from` swapped for `to` on the next frame, all removed after `time` ms (default 200) |
| `transition` | `to`, `transition`, `time`? | the transition alone, nothing shown or hidden |
| `focus`, `focus_first` | `to` | focus the element; its first focusable descendant |
| `push_focus` | `to` | remember the element (default: the focused one) on a stack |
| `pop_focus` | | focus the element on the top of the stack, and drop it |
| `dispatch` | `to`, `event`, `detail`?, `bubbles` | a `CustomEvent` named `event` with `detail` |
| `exec` | `to`, `attr` | run the commands JSON held in the element's attribute `attr`, *this* being that element |
| `push` | `h`, `value`? | send an `event` message whose `target` is `h` (below) |
| `debounce`, `throttle` | `ms` | the steps after it run debounced or throttled, per element and event type; a throttled `input` runs its last call at the end of the window |
| `key` | `keys` | stop unless the event is a keyboard event whose `key` is one of `keys` |
| `confirm` | `msg` | `window.confirm(msg)`; stop on cancel |
| `if`, `else_if` | `cond` | a branch: the steps after it run while the condition holds, up to its `else_if`, `else` or `end` |
| `else` | | the branch taken when no `if`/`else_if` before it at its level did |
| `end` | | ends the `if` at its level; without one, the branch runs to the end of the list (or of the enclosing branch) |
| `copy` | `to` | the clipboard gets the element's value (an `input`, `textarea`, `select`) or else its text |
| `copy_text` | `text` | the clipboard gets `text` |
| `scroll_to` | `to` | scroll the element into view |
| `set_value` | `to`, `value` | set the element's value (a checkbox's `checked` from `"true"`), then fire `input` and `change` on it |

Branches nest: an `if` inside a branch opens a level of its own, and
`else_if`, `else` and `end` belong to the innermost `if` not yet ended. A
step a branch skips doesn't run — a skipped `debounce` defers nothing, a
skipped `if` takes no branch.

**Targets.** `to` is the elements an operation acts on; omitted, *this*:

| `to` | |
|---|---|
| omitted | *this* |
| `{"ref":"menu"}` | the elements with `data-vk-ref="menu"` in *this*'s instance: the scope is the nearest ancestor-or-self of *this* carrying `data-vk-id`, and an element is in it when its own nearest ancestor-or-self carrying `data-vk-id` is the scope (so refs of nested instances are theirs, not their parent's). Without a `data-vk-id` ancestor (a static page), the scope is the document and every element with that ref is in it |
| `{"sel":"#x"}` | `document.querySelectorAll` |
| `{"closest":".card"}` | `this.closest(…)` |
| `{"inner":".x"}` | `this.querySelectorAll(…)` |

An operation on no element does nothing. `data-vk-ref` comes from the JSX
attribute `ref="menu"`.

**Conditions.** `cond` reads a target (the first element it names) when the
`if` is reached:

| `cond` | holds when |
|---|---|
| `{"to":T,"checked":true}` | the element is a checked checkbox or radio |
| `{"to":T,"value":true}` | its value is not empty |
| `{"to":T,"value_eq":"a"}` | its value is `"a"` |
| `{"to":T,"attr":"name"}` | it has the attribute |
| `{"to":T,"is":".open"}` | it matches the selector (`element.matches`) |
| `{"to":T,"exists":true}` | there is such an element |

Only `exists` holds without an element.

**Push.** `["push",{"h":"c3:1"}]` sends `{"type":"event","target":"c3:1",…}`
like a `data-vk-on` handler: `event` the DOM event's type, `key` a keyboard
event's key, `form` a submit's fields, and `value` — the push's `value` when
it has one (a JSON string as is, anything else as its JSON text: `3`,
`true`, `{"a":1}`), else *this*'s value. `h` is a handler id of the same
render, counted with the `data-vk-on-…` ids of its instance. A push without
`h` (rendered without a session) does nothing.

**Pushed by the server.** `render` (v1 and v2) and `patch` messages may carry

```text
"js":[{"id":"c3","ops":[…]}, …]
```

each entry run in order after the message's markup is applied, with *this*
the element whose `data-vk-id` is `id` (`c0`: the container), refs resolving
in that instance. An entry whose instance isn't on the page is skipped. An
`error` message carries none.

**Reconciliation.** What commands change on an element — a class, an
attribute, the `style` display — stays as the browser set it across the
server's patches as long as the server keeps rendering that attribute as it
did when the command ran: a runtime records, per element and attribute it
changed, the server's value at that time, and after a morph sets the
attribute back to the browser's value while the server's value is
unchanged. When the server renders a different value, the server's wins and
the record is dropped. Classes count as the `class` attribute, `show`/`hide`
as `style` (and `hidden`); `set_value` changes the value property, not an
attribute, and a field's value follows the runtime's usual rules.
