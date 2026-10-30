# templ Interop

[[toc]]

## Introduction

`ui.Node` is compatible with `templ.Component`. You can use existing templ
libraries inside a shape without changing its one-struct Go-header form.

## Using a templ Component

This shape renders a component from an imported templ library:

```html
<script lang="go">
package components

import "example.com/app/legacy"

type Welcome struct { Name string }
</script>

<section><legacy.Greeting name={Name} /></section>
```

The imported library supplies the templ component. Content between its tags
is passed through templ's children context.

`.templ` files in a module that requires templ are compiled by the vuka
command alongside the other files.

## templ on Live Pages {#on-live-pages}

templ compiled through vuka participates in render-tree recording.
Expressions, branches and loops form dynamic frames, so changes can be patched
inside the component.

Externally generated templ components still render, but opaque HTML may update
as a larger unit. Shared layouts for application pages belong in
`pages/_layout.vuka.html`.

## Component Libraries {#component-libraries}

A templ component library — templUI, shadcn-templ and the like — gives each
component a props struct, `func Button(props ...ButtonProps)`, and its tag's
attributes set the fields: `variant`, `size`, `id` and `class` are
`Variant`, `Size`, `ID` and `Class`. This live page uses a dialog and a
button from one, vendored into the app under `components/ui`:

```html
<script lang="go">
package products

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/app/components/ui/button"
	"example.com/app/components/ui/dialog"
)

type Index struct {
	ui.Live
	stock int
}

func (p *Index) Restock() { p.stock++ }
</script>

<p>In stock: {stock}</p>
<dialog.Dialog>
  <button.Button attributes={dialog.Trigger(ctx)}>Edit stock</button.Button>
  <dialog.Content>
    <dialog.Title>Edit stock</dialog.Title>
    <p>In stock: {stock}</p>
    <button.Button variant={button.VariantOutline} aria-label="Add one to the stock" onClick={Restock}>Restock</button.Button>
  </dialog.Content>
</dialog.Dialog>
```

### Extra Attributes {#extra-attributes}

An attribute that isn't one of the props' fields — `aria-label`, `data-id`,
`role`, `title`, `hx-get` — goes into the struct's `Attributes
templ.Attributes` field, which the component spreads on its element. An
`attributes={…}` on the same tag is merged with them. A name a letter or two
from a field's (`varient`) stays [E0106](/errors/E0106), since it is likely
that field misspelt; an HTML attribute of that name goes through
`attributes={templ.Attributes{"varient": "…"}}`. A props struct without an
`Attributes` field takes its fields only.

### Events {#events}

`onClick={Restock}` on such a component binds the live event, as on an
element: the session calls `Restock` when the component's element is
clicked. A handler inside `attributes={templ.Attributes{"onClick": Restock}}`
binds the same way, and so do JS commands. templ itself renders no function,
so a handler in a `templ.Attributes` that reaches a component some other way
— its own parameter, a props struct without `Attributes` — would be dropped:
the compiler reports it ([W0112](/errors/W0112)).

### The Component's Context {#context}

A library's helpers read what its components put in the context:
`dialog.Trigger(ctx)` is the trigger of the Dialog around it. In a tag
between a component's tags, `ctx` is the context the tag renders in there —
the Dialog's — so the trigger opens its own dialog, whatever its id.

### On Live Pages {#libraries-on-live-pages}

Library components work on a live page through every server update:

- **Ids stay the same.** A component given no id makes one at random
  (`id-` and 26 letters). The page's session renames those after their
  place — the live component rendering them and how many came before —
  so the HTTP render, the join and every patch agree, and the library's
  script keeps finding its elements. Inserting a component that makes its
  own id before others renumbers theirs: give a list's rows an `id` of
  their own, or make each row a live component.
- **A moved overlay is patched where it is.** A dialog, popover or listbox
  a script moves to `<body>` while open is matched at the place the page
  declares it, updated in place — its server values current while it is
  open — and never put back; the page holds one of it. It goes when the
  server stops rendering it.
- **What the browser changed stays.** An attribute a script set (`open`,
  `aria-expanded`, `data-open`, a position) and the children it changed (a
  select's shown value) keep the script's version while the server renders
  what it rendered before; once the server renders something else there,
  it wins, as it does for a form field's value. A select's hidden input
  keeps the chosen value by the form field rules.

No `data-vk-ignore` wrapper is needed, and the server's values inside a
library component keep updating.

## Passing Children {#children}

`templx.WithChildren` bridges children for library-level Go integration. This
is interop with an existing node library; application markup remains in shape
files. See [Templates](/ui/templates#components-and-slots).
