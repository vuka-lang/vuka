# Live Protocol

[[toc]]

## Introduction

This page is for authors of transports and browser runtimes. It describes
how a [`live.Session`](/ui/live) is driven, what travels on the wire, and the
attributes the HTML carries.

The web framework's [Live Pages](/web/live) are one implementation; the
[reference client](/ui/testing#stateful-components)
(`github.com/vuka-lang/ui/v2/live/livetest`) is another, in Go.

## The Session {#the-session}

A transport drives a session through these functions and methods:

<!-- go:context
type (
	Session       struct{}
	TreeUpdate    struct{}
	Patch         struct{}
	Route         struct{}
	Router        interface{}
	ClientMessage struct{}
	ServerMessage struct{}
)
-->

```go
func NewSession(ctx context.Context, render func(ctx context.Context) ui.Node) *Session

func (s *Session) Render() (string, error)          // v1: the container's HTML, mounting what renders
func (s *Session) RenderTrees() ([]TreeUpdate, error) // v2: every instance's render tree; the session is v2 from then on
func (s *Session) HTML() string                      // the page as last rendered, without rendering again
func (s *Session) Event(ev ui.Event) (Patch, error)  // run a handler, render, diff
func (s *Session) Info(topic string, msg any) (Patch, error) // deliver a message to subscribed instances' Update
func (s *Session) Topics() []string                  // the topics instances subscribed to
func (s *Session) Navigate(rt Route, patch bool) (Patch, error) // another URL's page (below)
func (s *Session) SetRouter(r Router)                // what the navigate message renders
func (s *Session) Close()                            // unmount everything

func (s *Session) Handle(m ClientMessage) ServerMessage // the protocol, as values
func (s *Session) HandleJSON(data []byte) []byte        // the protocol, as JSON
func Marshal(m ServerMessage) []byte
func PatchMessage(ref int, p Patch) ServerMessage        // a broadcast's patch (ref 0)
```

A `Patch` is `Updates []Update{ID, HTML}` (v1) or `Trees []TreeUpdate` (v2),
and `Error`, a handler's message. The `error` results are the session's own:
an unknown handler, a failed render.

Calls are safe from several goroutines and run one at a time. A transport keeps one session per connection, calls
`HandleJSON` for each message, routes `Topics()` to `Info`, and sends
`PatchMessage` for what `Info` returns.

## The HTML Contract {#the-html-contract}

The HTML a session renders carries these attributes:

| | |
|---|---|
| `data-vk-id="c2"` | an instance's root element (the page is `c0`, the container); a render that isn't one element is wrapped in `<vk-c data-vk-id="…" style="display:contents">` |
| `data-vk-key="…"` | an element's or instance's `key`, for pairing siblings in a morph |
| `data-vk-on-<event>="c2:0"` | a handler for the DOM event `<event>` (`click`, `keydown`, `submit`, …): the instance's id and the handler's index in its render |
| `data-vk-link`, `data-vk-link="patch"` | on an `<a href>`: a link the runtime follows over the connection ([navigation](#navigation)) |

A runtime listens, delegated, for each event type a `data-vk-on-<event>`
names, sends the nearest such element's handler id, and `preventDefault`s a
submit. Ids are per render: after a patch, use the new HTML's.

## Protocol v1

A browser runtime and a WebSocket transport speak JSON, one message per frame; `live.ClientMessage`,
`live.ServerMessage` and `Session.Handle`/`HandleJSON` implement the server
side. A join, events, patches, a broadcast and an error look like this:

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

Each message type means:

| | |
|---|---|
| `join` | the first message: the server renders the page; the reply is `render`. `"v":2` asks for [version 2](#protocol-v2-render-trees) |
| `event` | `target` is the handler's id from its `data-vk-on-…`; `event` the DOM event; `value` the element's value (a checkbox's `checked` as `"true"`/`"false"`); `key` a keyboard event's key; `form` a submit's fields (`FormData`, each name to its values) |
| `render` | `html` replaces the page container's content |
| `patch` | for each update, the element whose `data-vk-id` is `id` is morphed into `html`; `id` `"c0"` is the page itself: its `html` replaces the container's content. The morph matches `data-vk-key` among siblings, so keyed rows move instead of being rewritten. `error`, when set, is a handler's message, for the page to show |
| `error` | the message failed (an unknown handler, a render error); the session goes on |
| `navigate` | move the page to `url`, a patch when `patch` is true; the reply is a `patch` or a `redirect` ([navigation](#navigation)) |
| `redirect` | load `url` as an ordinary page load |

`ref` is any number the client picks; the reply to a message carries it, and
a broadcast has none.

The runtime listens, delegated, for each event type a
`data-vk-on-<event>` names, sends the nearest such element's id, and
`preventDefault`s a submit. Ids are per render: after a patch, use the new
HTML's.

On reconnect the client joins again — state lives in the server's
session, so a transport that keeps sessions across short disconnects keeps
state; one that doesn't starts the page over, as the HTTP render did.

The first HTTP render is static (a fresh mount, no ids); the `join` mounts
the page's live instances.

## Protocol v2: Render Trees {#protocol-v2-render-trees}

A client that sends `{"type":"join","v":2}` speaks version 2: the server
sends each live instance's **render tree** instead of its HTML, and after an
event only what changed in it. A `join` without `v` (or `"v":1`) gets the v1
messages above, unchanged.

A tree separates what a render fixes (statics,
sent once) from what it computes (dynamics, sent when they change), one tree
per live instance; the details are at the end.

### The Tree {#the-tree}

The compiler knows which parts of a shape template are fixed
markup and which are Go expressions. A *frame* is the markup of one piece of
markup: its *statics*, the fixed strings, around its *dynamics*, what the
expressions rendered — `len(s) == len(d) + 1`, and the frame's HTML is
`s[0] + d[0] + s[1] + … + d[n-1] + s[n]`. A dynamic is one of:

| JSON | |
|---|---|
| `"…"` | markup: text, an attribute (with its leading space and name, `" class=\"on\""`, or `""` when omitted), or anything rendered opaquely (a hand-written `ui.El` tree, a templ component not compiled by the vuka command) |
| `{"t":id,"s":[…],"d":[…]}` | a nested frame: a shape node in an `{expr}`, a component tag's children. `t` is its statics' id; `s` the statics themselves, present the first time the connection meets that id and omitted after |
| `{"c":[frame,…]}` | a block, `{for}`, `{if}` or `{match}`: one item frame per loop iteration; an `{if}`/`{match}` has zero items or one, whose statics id says which branch |
| `{"r":id,"v":"…"}` | markup of 64 bytes or more, which the connection keeps under `id` (a space of its own, not the statics'); later the same markup is `{"r":id}` |
| `{"id":"c3"}` | a stateful component instance: its HTML is that instance's tree's HTML |

### templ Components {#templ-components}

The vuka command compiles a module's `.templ` files to produce
the same frames: a templ component's fixed markup is its statics; its
`{ expr }` text and attribute values (an attribute value alone, between the
statics `…class="` and `"…`) are dynamics; a `for` is a block, an `if` or
`switch` a frame for the branch taken, and an `@component` call or
`{ children... }` a nested frame (or an instance's `{"id"}`).

Two frames
with the same statics share a statics id, whichever component made them, so
pages sharing a templ layout share its frames. A frame whose statics are
all empty and that holds one frame is sent as that frame. The wire format
is unchanged: a client needs nothing new for templ.

Statics ids and string ids are numbers the server allocates per connection
(0, 1, 2, … in order of first use); a `render` starts both afresh. The
client keeps a table of each.

### Messages {#messages}

A v2 join, an event and its patch, a broadcast and a patch carrying a
handler's error look like this:

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
has none. An entry is one of:

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
longer reached from `c0` can be dropped.

A component's root carries
`data-vk-id` (and `data-vk-key`) as a dynamic of its root frame, so the HTML a
client renders from a tree is the HTML v1 would have sent.

### Changes {#changes}

A change `{"u":{"i":X,…}}` names the dynamics of a frame that
changed, by index; the others stay. X is:

| X | |
|---|---|
| `"…"`, `{"r":…}` | the dynamic's new markup |
| a frame `{"t":…}` or loop `{"c":…}` or `{"id":…}` | replaces the dynamic |
| `{"u":…}` | the dynamic is a frame with the same statics; change it |
| `{"k":[step,…]}` | the dynamic is a loop: build its new items from the old ones |
| `{"p":[step,…]}` | the dynamic is markup of 1024 bytes or more: a token patch against it (below) |

Loop steps run over the old items with a cursor starting at 0, appending to
the new list. A step is one of:

| Step | |
|---|---|
| `n` (> 0) | the next `n` old items, unchanged |
| `-n` | skip the next `n` old items |
| a frame `{"t":…}` | a new item |
| `{"u":…}` | the next old item, changed |
| `{"m":i}`, `{"m":i,"u":…}` | the old item at index `i` (counting from 0 in the old list; the cursor doesn't move), unchanged or changed: a keyed item that moved |

Old items no step takes are gone. Items pair by `key` when the body's first
element (or component tag) has one, else by position.

A token patch cuts both strings before every `<` and after every
`>`, `"` and `&#34;`; steps `n` copy the next `n` old tokens, `-n` skip them,
`"…"` inserts text, `[p,n]` inserts `n` tokens copied from position `p` of the
old string. (Tree token patches use no dictionary.)

### An Example {#an-example}

This example follows a page rendering a table and a counter.

The table and counter are separate `.vuka.html` shapes. The table loops over
records with stable `key={row.ID}` values; the counter has an unexported `n`
field and an Increment handler. Their parent shape contains the two component
tags. See [Templates](/ui/templates) and [Stateful Components](/ui/stateful)
for the source form. The protocol below describes the resulting render trees.

The join's reply, with two rows (whitespace added), holds the page's tree
and both components' trees:

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

The steps take old item 3 (key 4) first, then from the cursor: key 1 kept, the new row,
key 2 skipped, key 3 kept; key 4, already taken, is left behind at the cursor
and the list ends there.

A branch switch: `t.sel` becomes 2, so the `{if}` renders its other branch,
whose statics are new to the connection:

```json
{"u":{"2":{"c":[{"t":5,"s":["<p>row ","</p>"],"d":["2"]}]}}}
```

The change back to 0 later is `{"u":{"2":{"c":[{"t":3,"d":[]}]}}}`. While the same
branch stays, changes are `{"k":[{"u":…}]}`.

A nested component's event: a click on the counter's button (`c2:0`)
changes only c2's tree; the page and the table aren't sent:

```json
{"type":"patch","ref":3,"trees":[{"id":"c2","tree":{"u":{"2":"1"}}}]}
```

When an event changes a parent and a child, both entries come, parent first;
a child the parent renders for the first time comes as `full` — and a removed
child is just no longer referenced.

### Details {#details}

There is one tree per instance (`trees`, by `id`), and a frame
dynamic may be an instance reference `{"id":…}`; each entry has its `tree`
and whether it is `full`, and a `render` is always a reset. Loops have a move step
`{"m":i}` for keyed items. Every block is a loop (`{"c":…}`), a branch being
a loop of zero or one item. An entry may be plain `html`.

Handler ids
(`data-vk-on-click="c1:4"`) are dynamics like any attribute; they count a
component's handlers in render order, so inserting a row with handlers
changes the ids of the handlers after it, which then travel as changed
dynamics.

## JS Commands {#js-commands}

[JS commands](/ui/js) are DOM changes the browser makes on an event without
asking the server: show a menu, toggle a class, move focus. A runtime
implements them from two places: the `data-vk-js-<event>` attributes of the
HTML, and the `js` field of server messages. Neither needs a session — a
statically rendered page carries its commands too.

### Commands on an Element {#commands-on-an-element}

`data-vk-js-<event>` holds the commands to run when the
DOM event `<event>` fires (`click`, `input`, `keydown`, `submit`, … — the
lowercased name after `on` in the markup attribute), as JSON (HTML-escaped, in
double quotes like every attribute). This button shows its instance's menu (removing its `hidden`
class) and pushes an event:

```html
<button data-vk-js-click="[[&#34;remove_class&#34;,{&#34;to&#34;:{&#34;ref&#34;:&#34;menu&#34;},&#34;names&#34;:[&#34;hidden&#34;]}],[&#34;push&#34;,{&#34;h&#34;:&#34;c3:1&#34;}]]">Open</button>
```

The attribute's value is `[["remove_class",{"to":{"ref":"menu"},"names":["hidden"]}],["push",{"h":"c3:1"}]]`:
a list of operations `[name, args]`, run in order.

The runtime listens,
delegated, for each event type a `data-vk-js-<event>` names (as it does for
`data-vk-on-<event>`), runs the commands of the nearest element carrying
either attribute for that event, and `preventDefault`s the event when the
list has a top-level `push` (a submit, a link's click, a form button's
click).

A list without one leaves the event to the browser: a form's
commands run as the browser submits it — after its constraint validation,
which fires no `submit` when it fails — and the form posts as usual. An
element has one or the other for an event, never both. *this* is the element
carrying the attribute.

The operations, their arguments and what they do:

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
| `push` | `h`, `value`?, `on_ok`?, `on_error`?, `timeout`?, `keep`? | send an `event` message whose `target` is `h` (below); the other fields make it [optimistic](#optimistic-pushes) |
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
| `set_text` | `to`, `text` | set the element's `textContent` to `text` |
| `call` | `fn`, `to`?, `args`? | call a JavaScript function ([below](#calling-javascript)) |
| `busy` | `to`, `timeout`? | mark the elements busy until undone ([below](#busy)) |
| `patch`, `navigate` | `href` | move the page to `href` over the connection, as a click on a `data-vk-link` (`="patch"`) link would ([navigation](#navigation)); a page without a connection loads `href` |

Branches nest: an `if` inside a branch opens a level of its own, and
`else_if`, `else` and `end` belong to the innermost `if` not yet ended. A
step a branch skips doesn't run — a skipped `debounce` defers nothing, a
skipped `if` takes no branch.

### Targets {#targets}

`to` is the elements an operation acts on; omitted, *this*:

| `to` | |
|---|---|
| omitted | *this* |
| `{"ref":"menu"}` | the elements with `data-vk-ref="menu"` in *this*'s instance: the scope is the nearest ancestor-or-self of *this* carrying `data-vk-id`, and an element is in it when its own nearest ancestor-or-self carrying `data-vk-id` is the scope (so refs of nested instances are theirs, not their parent's). Without a `data-vk-id` ancestor (a static page), the scope is the document and every element with that ref is in it |
| `{"ref":["user","pass"]}` | the elements with any of the refs, in the same scope |
| `{"attr":{"name":"name","is":["user",{"prefix":"step-"}],"page":true}}` | the elements with the attribute `name`, in the scope `ref` has, or the document with `page`. Without `is`, any value; else one of `is`: a string is an exact value, `{"prefix":s}`, `{"suffix":s}`, `{"contains":s}` and `{"word":s}` match as CSS's `^=`, `$=`, `*=` and `~=` (an empty `s`, or an unknown kind, matches nothing). A runtime builds the selector with `CSS.escape` of the name and each value, so no value is ever read as selector syntax |
| `{"sel":"#x"}` | `document.querySelectorAll` |
| `{"sel":["#x",".y"]}` | the elements any of the selectors matches |
| `{"closest":".card"}` | `this.closest(…)` |
| `{"inner":".x"}` | `this.querySelectorAll(…)` |

A target of several parts finds every element any part finds, in document
order, each once. `focus`, `focus_first`, `scroll_to`, `copy`, a `call`'s
`to` and a read passed to `call` take the first element a target finds.
An operation on no element does nothing.

`data-vk-ref` comes from the shape markup
attribute `ref="menu"`. In protocol v2 a `data-vk-js-…` attribute is a dynamic
like any attribute written with an expression: it is sent again only when
its JSON changes (a push's handler id moving, a value).

### Conditions {#conditions}

`cond` reads a target when the `if` is reached:

| `cond` | holds when |
|---|---|
| `{"to":T,"checked":true}` | the element is a checked checkbox or radio |
| `{"to":T,"value":true}` | its value is not empty |
| `{"to":T,"value_eq":"a"}` | its value is `"a"` |
| `{"to":T,"attr":"name"}` | it has the attribute |
| `{"to":T,"is":".open"}` | it matches the selector (`element.matches`) |
| `{"to":T,"exists":true}` | there is such an element |
| `{"and":[c, …]}` | every condition holds (`[]`: true) |
| `{"or":[c, …]}` | one condition holds (`[]`: false) |
| `{"not":c}` | the condition doesn't hold |

A read holds when every element its target finds passes it; with
`"some":true` (`{"to":T,"value":true,"some":true}`), when at least one
does. A target that finds no element fails every read but `exists`.

### Push {#push}

`["push",{"h":"c3:1"}]` sends `{"type":"event","target":"c3:1",…}`
like a `data-vk-on` handler: `event` the DOM event's type, `key` a keyboard
event's key, `form` a submit's fields, and `value` — the push's `value` when
it has one (a JSON string as is, anything else as its JSON text: `3`,
`true`, `{"a":1}`), else *this*'s value.

`h` is a handler id of the same
render, counted with the `data-vk-on-…` ids of its instance. A push in a
command the server pushed (a `js` entry, below) has an id of its own
instead, `c3:0p`, `c3:1p`, …: its number among the instance's pushes,
which stays valid across the instance's renders until 64 later pushes
have replaced it, so a render between the push and its event — the join
after an HTTP render, another event, a message — leaves it working. A push
without `h` (rendered without a session) does nothing.

### Commands Pushed by the Server {#commands-pushed-by-the-server}

`render` (v1 and v2) and `patch` messages may carry a `js` field:

```text
"js":[{"id":"c3","ops":[…]}, …]
```

each entry run in order after the message's markup is applied, with *this*
the element whose `data-vk-id` is `id` (`c0`: the container), refs resolving
in that instance. An entry whose instance isn't on the page is skipped. An
`error` message carries none.

### Reconciliation {#reconciliation}

What commands change on an element — a class, an
attribute, the `style` display — stays as the browser set it across the
server's patches as long as the server keeps rendering that attribute as it
did when the command ran: a runtime records, per element and attribute it
changed, the server's value at that time, and after a morph sets the
attribute back to the browser's value while the server's value is
unchanged. When the server renders a different value, the server's wins and
the record is dropped.

Classes count as the `class` attribute, `show`/`hide`
as `style` (and `hidden`); `set_value` changes the value property, not an
attribute, and a field's value follows the runtime's usual rules.
`set_text` is recorded as the element's text: the override stays while the
server keeps rendering the same text in the element, and is dropped when
the server's text changes.

### Optimistic Pushes {#optimistic-pushes}

A push can show its outcome before the server answers — a like counted, a
row gone — and take it back if the server refuses. The operations before a
`push` in a list are its **predictions**; the push's reply settles them.
This list marks a like and updates its count before the push, with hooks for
either outcome:

```text
[["add_class",{"names":["liked"]}],
 ["set_text",{"to":{"ref":"likes"},"text":"8"}],
 ["push",{"h":"c3:1","on_error":[["show",{"to":{"ref":"like-error"}}]],"on_ok":[["dispatch",{"event":"liked","bubbles":true}]]}]]
```

The push takes these fields:

| `push` field | |
|---|---|
| `h`, `value` | as before |
| `on_ok` | operations run when the push succeeds, *this* as for the list |
| `on_error` | operations run when it fails |
| `timeout` | ms to wait for the reply before the push fails; default `10000` |
| `keep` | `true`: a failure reverts nothing (`on_error` still runs) |

#### Predictions {#predictions}

A list is cut into segments at its `push` operations: the
operations from the list's start, or from the previous `push`, up to a
`push` are that push's predictions. The runtime records every DOM change
they make — an attribute, a class (as `class`), the `style` display, a
`hidden` attribute, the text (`set_text`), a field's value or checked state
(`set_value`) — with, for each, the value before the change, the value it
set, and the server's value at that moment (what the server's last markup
renders there).

An `exec`'d list runs within the segment it is called
in: its changes join that segment, and a push in it closes it. A segment closed by a
push that doesn't run (a skipped branch, a push without `h`) or not closed
at all (operations after the last push, a chain stopped by `key`,
`confirm` or a `call`) is not a prediction: its changes stay as ordinary
command changes.

#### Settling {#settling}

Every event message the runtime sends has a `ref`, and the
server answers each with exactly one message carrying that `ref` — a
`patch` (with `error` when the handler failed; with no `updates`/`trees`
when nothing changed, `{"type":"patch","ref":4}`) or an `error` message
(an unknown handler, a failed render). The push is settled by the first of:

- the reply, without `error`: **success**. Nothing is reverted;
  reconciliation goes on as for any command change, so wherever the
  server's markup differs from what was predicted, the server's wins.
- the reply with `error`, or an `error` message: **failure**.
- `timeout` ms passing with no reply: **failure**.

On failure, unless `keep`, each recorded change is reverted, latest first,
to its value before the change — where the server's value is still the one
recorded and the element still shows the value the prediction set.

Where
the server has rendered a different value since, or a later command changed
it, the current value stays. Reverting to the server's own value drops the
command record; to an earlier command's value, keeps that record.

A reply is applied in this order: its markup (and reconciliation), the
push's reverts on failure, the message's `js` entries, then `on_ok` or
`on_error`. A reply arriving after its push timed out is applied as usual,
minus the settling: the push was already reverted and its `on_error` run.

Pushes settle independently: a list with two pushes has two segments, each
reverted only by its own push's failure.

Server-pushed lists (`"js"` entries) may carry optimistic pushes too, with
the same semantics, *this* being the entry's instance root.

### Busy

`["busy",{"to":T,"timeout":5000}]` marks each element `to` finds (*this*
without one) busy, unless it is marked already:

- `aria-busy="true"` on it;
- `readonly` on each `input`, `textarea` and `select` among it and its
  descendants, except the button kinds (`readonly` already there stays);
- `aria-disabled="true"` and `tabindex="-1"` on each `button`, `a[href]`
  and `input` of type `submit`, `button`, `reset` or `image`;
- every `click` and `submit` whose target is the element or inside it is
  `preventDefault`ed and stopped before any other listener — except the
  `submit` a click marking the element goes on to in the same task, so a
  submit button whose click marks its form still submits it.

Each change is recorded as a command's change is (reconciliation keeps it
across patches), with the value before it. A mark is undone — each change
reverted where the server's value and the element's are still the
recorded ones — by the first of:

- the settling of a push of the same list: the next `push` after the
  `busy`, or, with none after it, the last one before it; on success or
  failure, before `on_ok`/`on_error` run (a push failing unsent fails its
  marks too);
- `timeout` ms passing;
- a `pageshow` event with `persisted` (the page restored from the
  back/forward cache);
- a navigation to another page.

A mark that none of them reach — a plain form post — lasts until the page
is left. An element waiting for a reply (`aria-busy` the runtime sets on a
`data-vk-on` or push's element) keeps a mark's `aria-busy` when the reply
comes.

### Calling JavaScript {#calling-javascript}

`["call",{"fn":"confetti","to":T,"args":[…]}]` calls a page function.
`fn` is a name registered with `vuka.register(name, fn)`, else a dotted
path looked up on `window` (`"Picker.refresh"`); it is never evaluated as
code. The function is called as `fn(ctx, ...args)`:

| `ctx` | |
|---|---|
| `el` | the first element `to` names (*this* without `to`) |
| `event` | the DOM event the list runs for (`null` for a server-pushed list) |
| `root` | *this*'s instance root, the nearest ancestor-or-self with `data-vk-id` |
| `target(spec)` | the elements a target spec (`{"ref":"x"}`, …) names, from *this* |

`args` are JSON values, each passed as is, except **element reads**,
objects of one key `$read` whose value is a condition's JSON
([Conditions](#conditions)), read when the `call` is reached:

| `{"$read":…}` | passes |
|---|---|
| `{"to":T,"value":true}` | the element's value, a string |
| `{"to":T,"checked":true}` | a checkbox's or radio's checked state |
| `{"to":T,"attr":"name"}` | the attribute's value, or `null` without it |
| `{"to":T,"value_eq":"a"}`, `{"to":T,"is":".x"}`, `{"to":T,"exists":true}` | whether the condition holds |

A read of no element passes `null` (`exists`: `false`). A `$read` nested in
another value is not a read.

The list waits while a returned promise is pending. A result of `false`
stops the list, as a declined `confirm` does.

An unknown function, a throw
or a rejected promise stops the list and fires a `vk:error` event on *this*
(`detail`: `{"error": message, "fn": name}`); when the `call` is among a
push's predictions, that push fails without being sent — its predictions so
far are reverted (unless `keep`) and its `on_error` runs.

## Client State {#client-state}

[Client State](/ui/state) rides the same attributes. A state reference is
`{"s": key, "i": initial, "p": [field…]}`: the state's key (`c3:s1x9k2:1` —
the instance, the declaration's site and its count there; `g:<site>` for a
package-level state), its whole initial value, which sets the state up the
first time the browser meets it, and, for a struct state's field, the path of
JSON keys.

Three commands write:

| | |
|---|---|
| `["state_set", {s, i, p?, v}]` | set the state (or its field) to `v` |
| `["state_toggle", {s, i, p?}]` | flip a bool |
| `["state_inc", {s, i, p?, by}]` | add `by` (1 or -1) |

A chain's writes apply in order; the bindings they reach update before the
chain's next other step and at its end, once each.

Bindings are attributes holding an expression: `data-vk-b-<attr>` on an
element (the attribute `<attr>`), `data-vk-b` on a `<vk-t>` (its text) and
`data-vk-show` on a `<vk-show>` (whether its children show, with the
`data-closed`, `data-enter` and `data-leave` phases).

An expression is a
literal (a string, number, bool or null), `{"v": value}` (a literal object
or list), a state reference, or `[op, operands…]` with op `!`, `neg`, `&&`,
`||`, `==`, `!=`, `<`, `<=`, `>`, `>=`, `+`, `-`, `len` (Go's: a string's
UTF-8 bytes), or `["class", {"names": expr, …}]`. The runtime evaluates it
again whenever a state it references changes, and after a patch morphs the
element, so the browser's values outlive the server's renders.

## Navigation

A connected page moves to another URL over its connection: a paged list,
a filter or tab kept in the URL, a list and its rows' pages. Two kinds:

- a **patch** stays on the page: the instances the new URL renders where
  the old one did stay mounted with their state, their props set again —
  those the server lets stay (an `Update` of theirs takes the URL; a client
  asking for a patch can't keep the rest);
- a **navigate** goes to another page: every instance but the page itself
  (`c0`) unmounts, and the new page's mount.

A patch, a navigate and a navigation answered with a redirect look like
this:

```text
→ {"type":"navigate","ref":5,"url":"/books?page=2","patch":true}
← {"type":"patch","ref":5,"url":"/books?page=2","trees":[{"id":"c3","tree":{"u":{…}}}]}
→ {"type":"navigate","ref":6,"url":"/books/7"}
← {"type":"patch","ref":6,"url":"/books/7","trees":[{"id":"c0","tree":{"u":{"3":{"id":"c4"}}}},{"id":"c4","full":true,"tree":…}]}
→ {"type":"navigate","ref":7,"url":"/books.json"}
← {"type":"redirect","ref":7,"url":"/books.json"}
```

The `navigate` message has two fields:

| `navigate` field | |
|---|---|
| `url` | the URL to move to, a path with its query (never a `#hash`, never another origin) |
| `patch` | `true` for a patch |

The server resolves `url` to a page (a `live.Router`; [the session](/ui/live#navigation)).
When it is one, the page becomes the session's, rendered and diffed as after
an event: the reply is a `patch` (v1 `updates`, v2 `trees` — the page `c0`
is the same instance, so a v2 client gets only what changed of its tree,
against statics it already holds), carrying `url`, and `js` when the new
page's mount hook pushed commands.

When `url` isn't a page (a redirect, an
error, a non-HTML answer), the reply is `redirect`, whose `url` the client
loads as an ordinary page — never one that isn't a path or an `http` or
`https` URL, which the client refuses to load as well.

An `error` reply
means the navigation failed — a mount hook or `Update` refused the URL, say —
and nothing of the page stays mounted; the client loads `url`. Either
version supports navigation.

A transport may add fields to a navigation's `patch`; the web framework's
`token` is the new page's join token, which the client uses when it
reconnects, and `title` the new page's title, which the client gives the
document.

### The Client {#the-client}

A runtime takes clicks on `<a data-vk-link>` (bubbling, so
an element's own commands and handlers run first), unless the event's
default is already prevented, the click is not the main button or has a
modifier key (`ctrl`, `meta`, `shift`, `alt`), the link has a `target`
other than `_self` or a `download` attribute, `href` is of another origin,
or `href` is the page's own URL with a `#hash`; those stay the browser's.
It also moves on `patch` and `navigate` commands, and on `popstate`.

While a navigation is in flight it sets `data-vk-navigating` on `<html>` and
sends no events. Then:

| | |
|---|---|
| the `patch` reply | applied as any patch; then the address changes (`pushState`, or the entry's state updated for back and forward), scroll and focus follow, and `vk:navigate` fires on `window` with `detail: {url, patch}` |
| `redirect`, `error`, no reply in 10s, the connection lost | an ordinary page load of the URL (`location.replace` for back and forward) |
| not connected | an ordinary page load, nothing sent |

History entries carry a page id in their state: a patch keeps the page's, a
navigate makes a new one. Back or forward to an entry of the page shown
sends a patch; to another page's, a navigate; to the same URL with only a
different `#hash`, nothing.

Scroll: a navigate goes to the top, or to the
element whose id the `#hash` names; a patch stays; back and forward go to
the position kept in the entry's state.

Focus, after a navigate: the
`[autofocus]` element, else `<main>`, else the first element with
`data-vk-id`, given `tabindex="-1"` when it isn't focusable.

A whole-document
`c0` sets `document.title` and adds the `<head>` children the document lacks
(compared by their markup; a script is recreated so that it runs). A
navigate drops the optimistic pushes still waiting, without their hooks.
