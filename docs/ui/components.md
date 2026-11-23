# Components

[[toc]]

## Introduction

Components are the reusable pieces of your pages: a notice, a card, a menu. A
component is a `.vuka.html` shape with one struct and its markup in the same
file. Exported fields are props; content and named slots are node props,
filled with markup by the shape that uses the component.

## Writing a Component {#writing}

This shape declares a notice with a title, a flag and children:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Notice struct {
    Title string
    Important bool
    Children ui.Node
}
</script>

<aside class:important={Important}>
  <h2>{Title}</h2>
  {Children}
</aside>
```

Use it as a tag from another shape:
`<components.Notice title="Saved" important><p>Your changes are saved.</p></components.Notice>`.
A bare bool attribute is true. HTML attributes retain HTML names, including
`class`, `for`, `aria-*` and `data-*`.

## Filling a Component's Slots {#slots}

A `ui.Node` field other than Children is a slot, filled with
`<slot:name>…</slot:name>` directly inside the tag. A func field returning a
`ui.Node` is a slot the component passes values to, which the caller names
with `let`; a slice of structs takes one `<slot:name>` per element:

```html
<script lang="go">
package pages

import (
    "example.com/app/models"
    "github.com/vuka-lang/ui/v2"
)

type Members struct { People []models.Person }
</script>

<ui.Card title="Members">
  <slot:header><button>Invite</button></slot:header>
  <ui.Table rows={People}>
    <slot:column header="Name" let={p}>{p.Name}</slot:column>
    <slot:column header="Email" let={p}><a href={"mailto:" + p.Email}>{p.Email}</a></slot:column>
  </ui.Table>
</ui.Card>
```

A func field is also set as a prop, to a value of its type. A func literal
set on it takes markup for its body, so a slot used once needs no
`<slot:…>` tag, and a [markup func](/ui/templates#markup-funcs) bound once
fills the same slot of several tags:

```html
<script lang="go">
package pages

import (
    "example.com/app/components"
    "example.com/app/models"
)

type Members struct { Active, Invited []models.Person }
</script>

card := func(p models.Person) { <b>{p.Name}</b> <small>{p.Email}</small> }

<components.List items={Active} row={card} />
<components.List items={Invited} row={func(p models.Person) { <i>{p.Email}</i> }} />
```

Inside the component, `<slot:actions>fallback</slot:actions>` renders the
slot, or the fallback when the caller left it out; `ui.Has(Actions)` reports
whether it was filled; `vuka:"required"` on the field makes every tag fill
it. See [Components and Slots](/ui/templates#components-and-slots).

## Optional Props {#optional}

A prop declared `Option[T]` takes a plain `T` too, and the tag wraps it:
`<Note text="plain"/>` and `<Note text={name + "!"}/>` set `Some(…)`. An
`Option` given as it is stays as it is, and a tag that leaves the prop out
leaves it `None`.

## Props Taking a Type's Constants {#constants}

A quoted value on a prop of a defined string type is checked against the
constants the type's package declares: `<design.Button variant="outline">`
compiles because `ButtonOutline ButtonVariant = "outline"` exists, and
`variant="outlien"` is [E0196](/errors/E0196), with the closest value
suggested. The value is what you write, never the constant's Go name, and
the editor completes the values inside the quotes. A number on a prop of a
defined integer type, `cols={3}`, is checked the same way. A hole is never
checked: `variant={design.ButtonVariant(name)}` passes any value.

A type with no exported constants takes any value. For a type whose
constants are only presets, mark its doc comment `//vuka:open` and quoted
values outside them compile too:

```go
package design

// Accent is a colour: a preset or any CSS colour.
//
//vuka:open
type Accent string

const AccentBrand Accent = "brand"
```

## Required Props {#required}

Tag any field `vuka:"required"` and every tag must set it, whatever its
type: a tag that leaves it out is [E0158](/errors/E0158), at the tag. Tag the
func and handler props a component can't work without, so a missing one
fails when the page builds, not when the component renders:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type DeleteModal struct {
	Title string
	Func  ui.Action `vuka:"required"`
}
</script>

<design.Modal title={Title}>
  <button onClick={Func}>Yes, delete it</button>
</design.Modal>
```

## Prop Defaults {#defaults}

A prop declares its default in the tag that carries `required`, and a tag
that leaves the prop out gets it:

```go
package design

import "github.com/vuka-lang/ui/v2"

type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneDanger  Tone = "danger"
)

type Button struct {
	Type     string `vuka:"default=button"`
	Tone     Tone   `vuka:"default=ToneNeutral"`
	Size     int    `vuka:"default=2"`
	Children ui.Node
}
```

The value is read as the field's type: a string's text, a number, `true` or
`false`, or a constant of the field's package by name. `<Button>Save</Button>`
is a `button` of tone `ToneNeutral`; a tag that sets the prop, even to the
zero value, sets what it says, so `type=""` stays `""`. A default that
doesn't parse, names no constant, or sits beside `required` is
[E0136](/errors/E0136), at the field.

## Ids That Don't Collide {#ids}

A field of type `ui.AutoID` is an id the tag fills when it leaves it out:
unique on the page, and the same in every render of it, so a patch never
renames it and two dialogs never share one. `Suffix` names its parts:

```html
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Dialog struct {
	ID    ui.AutoID
	Title string
}
</script>

<dialog id={ID} aria-labelledby={ID.Suffix("title")}>
  <h2 id={ID.Suffix("title")}>{Title}</h2>
</dialog>
```

The id comes from the tag's place: the component instance rendering it, the
keys around it and how many tags of that place came before, never a count of
the page's ids, so a branch shown above it or a keyed row removed before it
leaves it as it was. A caller may still set it, `<Dialog id="confirm">`, and
that value wins. A live component's own `AutoID` field, one embedding
`ui.Live`, is its instance's id when its tag leaves it out, the same for the
instance's life. An `AutoID` built in Go rather than by a tag is empty, and
an attribute set to it is left out.

## What a Hole Accepts {#nodes}

Child holes accept nodes, slices of nodes, escaped strings and numbers,
errors and nil. `ui.Node` is compatible with templ components. A static
shape is created and rendered per request; a live shape retains its server
instance for the connected page.

## Dynamic Attributes {#attributes}

Use `class:selected={Selected}`, `{attrs...}` and `style:color={Color}` for
dynamic classes, attribute sets and sanitized styles. Event attributes accept
server methods or browser commands. See [Templates](/ui/templates#holes).
An `on…` attribute a component doesn't declare goes on to the element it
spreads its `Attrs` on: `<Combobox onChange={Choose(this.value)}/>`
([Events on Components](/ui/events#components)).

`attr:name` sets any attribute by its HTML name. On a component it is an
entry of its `Attrs`, or what it passes on to its
[forward element](#fallthrough), never a prop, whatever the name, so it
needs no `[]ui.Attr` literal; a component taking neither is
[E0106](/errors/E0106). On an element it is the attribute as written, for a
name the compiler would read otherwise:

```vuka
<design.Button type="submit" attr:form={"rule-" + r.Target}>Save changes</design.Button>
<design.Button attr:data-tester-open={tester("handoff")}>Open tester</design.Button>
```

`name?={v}` writes the attribute only when `v` isn't its type's zero value:

```vuka
<input id?={ID} placeholder?={Hint} />
<Card id?={ID}>…</Card>
```

On a component's prop it leaves the prop unset when `v` is zero, so the
prop keeps its [default](#defaults). A bool needs no `?=` — `disabled={Busy}`
is left out when false — and `disabled?={Busy}` is
[E0135](/errors/E0135); `Option[T]` stays the way to pass a zero that is a
real value.

## Fallthrough {#fallthrough}

A component's tag takes its props, and what no prop takes goes on to one
element of the component's markup, its **forward element**: its one
top-level element, or, when it has several or the right spot is inside,
the element marked with a bare `forward`:

```vuka
<div class="rounded-xl border bg-card shadow-xs">{Children}</div>
```

```vuka
<design.Card class="border-red-200 mt-4" id="billing" aria-label="Billing">…</design.Card>
```

renders `<div class="rounded-xl border bg-card shadow-xs border-red-200 mt-4"
id="billing" aria-label="Billing">`. What falls through:

- **`class`**, merged with the element's own by
  [MergeClasses](/ui/classes#merging): the caller's utilities replace the
  component's of the same group (`bg-muted` replaces `bg-card`), the rest
  stay;
- **HTML, ARIA and data attributes** — `id`, `title`, `role`, `tabindex`,
  `disabled`, `aria-*`, `data-*`, an element's own (`placeholder`,
  `autocomplete`) — replacing the element's own of that name when it
  writes it as text or bare (`role="region"`); one it computes
  (`aria-label={Label}`) is the component's, and a caller setting it is
  [E0139](/errors/E0139);
- **DOM events**: `onClick={Save()}` on a component with no handler prop
  for it is the element's `onClick`;
- **`bind:value`** when the forward element is an `input`, `select` or
  `textarea`: it binds the control as it would written on it;
- **`ref`**, naming the element for JS commands.

```vuka
<label>{Label} <input class="h-10 rounded-md border" forward/></label>
```

```vuka
<components.Input label="Email" bind:value={Email} placeholder="you@example.com" disabled={Busy}/>
```

The order of the checks is the design: an attribute naming a prop sets the
prop; one a letter or two off a prop's name is a did-you-mean error,
never a silent attribute; then it falls through when it is an attribute of
HTML — anything else is an error. `attr:name` passes an attribute on as
written, skipping the checks. A component declaring `Attrs []ui.Attr`
takes everything there instead, wherever its markup spreads it, and a
caller's `id` sets an `ID` prop — a [ui.AutoID](#ids) one included, so the
caller's id wins over the generated one.

The compiler gives the component a field `Fallthrough ui.Fallthrough`
holding what its tag passed on, which Go code sets like any field. A
component's struct declared in a `.go` file beside its template passes
nothing on; a forward marker there is [E0137](/errors/E0137), as are two
markers or one inside a `for` loop. A tag passing something on to a
component with no forward element — several roots and none marked, a root
that is `{Children}` or a slot, or a component's tag — is
[E0138](/errors/E0138); `bind:value` on a component whose forward element
isn't a control, or an attribute the element sets in a way a caller's
can't replace (its own handler for the event, its `style`), is
[E0139](/errors/E0139).

A component that computes with the caller's classes declares
`Class string` tagged `vuka:"class"`: the class attribute using it merges
it last, `class={"inline-flex rounded-md " + Class}` written as
`ui.MergeClasses("inline-flex rounded-md ", Class)`, and a tagged `Class`
no class attribute uses is [E0123](/errors/E0123).

### asChild {#aschild}

A bare `asChild` runs fallthrough in reverse: the tag's one element child
becomes the component's root. The component renders as written, with the
child in its forward element's place, and the child takes the forward
element's attributes:

```vuka
<design.Button asChild variant="outline">
  <a href="/reports/42" target="_blank" download>Report</a>
</design.Button>
```

The link gets the button's classes, merged with its own — the child's
utilities win (`px-2` on the link replaces the button's `px-4`) — and the
button's other attributes and handlers; an attribute the child sets is its
own, and of two handlers for one event both run, the child's first. The
child keeps its content; the forward element's own content is replaced
by it. Props stay props (`variant` above), and what else the tag passes
on goes to the forward element as without `asChild`, so to the child. The
component's author changes nothing.

The child is one element written in the tag — none, several, text or a
component's tag is [E0171](/errors/E0171) — and the component needs a
forward element to give ([E0138](/errors/E0138)), which a component
declaring `Attrs` or a live component doesn't have. On a live page the
child is the page's element like any other: its handlers run and each
render patches it in place.

## Linking Between Pages {#links}

`<ui.Link href="/pets">Pets</ui.Link>` participates in live navigation.
Ordinary `<a>` elements remain full-document links. `active="prefix"` (or
`"exact"`) with `activeClass` marks the link to the current page in the
browser, the way to mark a layout's menu, and a `<details
data-vk-active-within>` around such links opens on the current page
([the current group](/web/navigation#the-current-group)); `prefetch` tunes
fetching its page ahead. See [Navigation](/web/navigation).

## Keying Repeated Components {#key}

Give siblings stable `key={record.ID}` values. A key identifies the same
logical item across renders, preserving its live instance.

> [!WARNING]
> Array positions are unsuitable when items can be reordered.

## Overlays Outside Clipping Ancestors {#teleport}

A `position: fixed` element is fixed to the viewport only while no ancestor
creates a containing block or a clip for it. A `mask-image`, a `transform`,
a `filter`, `contain`, `will-change: transform` or a `backdrop-filter` on
any ancestor confines it to that ancestor: a layout that fades its scroll
area with a mask shows a modal's backdrop and a toast inside the scroll area
only, and no `z-index` lifts them out of the ancestor's stacking context.

`<ui.Teleport>` renders its content where it is written and has the runtime
move it to the end of `<body>`, out of reach of every ancestor:

```html
<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

type Products struct {
	ui.Live
	editing bool
}

func (p *Products) Edit()  { p.editing = true }
func (p *Products) Close() { p.editing = false }
</script>

<main class="[mask-image:linear-gradient(black_90%,transparent)] overflow-auto">
  <button onClick={Edit()}>Edit product</button>
  if editing {
    <ui.Teleport>
      <div class="fixed inset-0 z-50 grid place-items-center bg-black/50" onKeyDown.esc={Close()}>
        <div role="dialog" aria-modal="true" class="rounded-lg bg-white p-6">
          …
          <button onClick={Close()}>Done</button>
        </div>
      </div>
    </ui.Teleport>
  }
</main>
```

`to="#id"` moves it to the end of an element the layout owns instead, a
region for overlays:

```html
<ui.Teleport to="#overlays">…</ui.Teleport>
```

The content is the page's markup as any other: it has the same scope, the
same state and the same handlers, and a field in it still belongs to the
form around it, so a `model=` in it binds to the enclosing `for={Form}`.
The runtime keeps treating it as written in place:

- **Live patches** apply to it where it was moved. A re-render never moves
  it back, and it goes when the server stops rendering the `<ui.Teleport>`
  (the `if` above turning false) or the component holding it.
- **Events** reach vuka's handlers as if they came from where the content
  is written: an `onKeyDown.esc` on an ancestor still sees a key pressed in
  a teleported field. A script's own `addEventListener` sees the DOM's
  bubbling, which follows where the content is, at the end of the body.
- **Commands** find it in its component: `ui.Ref("panel")` from a button on
  the page reaches an element with `ref="panel"` inside the teleport.
- **Forms**: fields teleported out of a `<form>` get a `form` attribute that
  keeps them its fields, sent with its submit; a form written entirely
  inside a teleport works as any form.
- **Focus**: a focused field keeps its focus and what it shows when it is
  moved, and across patches, since it is moved only once.

Without JavaScript, and until the runtime starts, the content renders in
place: the `<vk-teleport>` element holding it has `display: contents`, so it
adds no box to the layout. A component can teleport itself by making
`<ui.Teleport>` the root of its markup. The kit's `<ui.Modal>` and
`<ui.ToastContainer>` render in one, and so does the vendored
[Dialog](/ui/components/dialog).

Give a teleport an `id` when teleports around it come and go, as you key
repeated components: it keeps the same element across renders.

A component the page doesn't place at all — an editor a row's button opens,
a details panel — opens with `ui.JS.Call(component)` instead, in the
page's overlay host, until it closes ([Opening Components](/ui/call)).

## The Component Kit {#kit}

The ui kit exposes components as tags, such as `<ui.Card>` and `<ui.Button>`.
Use their exported fields as attributes, their Children field for content,
and their named node fields as slots. `ui.Table` and `ui.DataTable` take
their columns as repeated `<slot:column>` slots, or as a `columns={…}` slice
built in Go. The kit uses the same node tree and
live protocol as application components; no second rendering syntax is needed.
