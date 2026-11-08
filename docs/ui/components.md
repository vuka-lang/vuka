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

Inside the component, `<slot:actions>fallback</slot:actions>` renders the
slot, or the fallback when the caller left it out; `ui.Has(Actions)` reports
whether it was filled; `vuka:"required"` on the field makes every tag fill
it. See [Components and Slots](/ui/templates#components-and-slots).

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

## The Component Kit {#kit}

The ui kit exposes components as tags, such as `<ui.Card>` and `<ui.Button>`.
Use their exported fields as attributes, their Children field for content,
and their named node fields as slots. `ui.Table` and `ui.DataTable` take
their columns as repeated `<slot:column>` slots, or as a `columns={…}` slice
built in Go. The kit uses the same node tree and
live protocol as application components; no second rendering syntax is needed.
