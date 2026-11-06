---
title: Dropdown Menu
description: "A menu under a button, shown while a browser state holds."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Dropdown Menu

A menu under a button, shown while a browser state holds.

<div class="vk-ui-shots">
  <img src="/ui/components/dropdown-menu-light.png" alt="Dropdown Menu in the light theme">
  <img src="/ui/components/dropdown-menu-dark.png" alt="Dropdown Menu in the dark theme">
</div>

## Adding It

```sh
vuka ui add dropdown-menu
```

It writes `components/ui/dropdown_menu.vuka.html` and `components/ui/dropdown_menu.go`, with what it uses: [Button](/ui/components/button), [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` at commit `c2870b7828d7` or newer.

## Using It

```vuka-html
<script lang="go">
package projects

import (
	"github.com/vuka-lang/ui/v2"

	design "example.com/shop/components/ui"
)

// Row is a project's row and its actions.
type Row struct {
	ui.Live
	menu ui.State[bool]
}

func (r *Row) Duplicate() {}

func (r *Row) Archive() {}
</script>

<div>
  <design.DropdownMenu show={menu}>
    <slot:trigger>Actions</slot:trigger>
    <slot:item onClick={Duplicate}>Duplicate</slot:item>
    <slot:item onClick={Archive} destructive>Archive</slot:item>
  </design.DropdownMenu>
  <design.Button variant={design.ButtonLink} onClick={menu = true}>More…</design.Button>
</div>
```

A menu under its button, shown while the [browser state](/ui/state) passed
as `show` holds; without one it keeps its own. Its button toggles the
state, and anything else writing it shows or hides the menu too. Escape, a
click outside and a click on an item write `false` into it.

Showing scales the menu in over 100ms ([transitions](/ui/state#show) a
browser preferring reduced motion skips) and focuses its first item; the
arrow keys, Home and End move between the enabled items; hiding gives the
focus back to what had it. An item's `onClick` takes a live handler,
statements writing state or a `ui.JS` chain. The markup is a button with
`aria-haspopup="menu"` and `aria-expanded`, and a `role="menu"` of
`role="menuitem"` buttons.

## Props

| Prop | Type | |
|---|---|---|
| `show` | `ui.State[bool]` | Show is the browser state the menu follows: pass the page's to show it from elsewhere, or leave it to the menu's own. |
| `<slot:trigger>` | `ui.Node` | Trigger is the button's content, a &lt;slot:trigger&gt;. |
| `items` | `[]DropdownItem` | Items are the menu's items, a &lt;slot:item&gt; each, in order. |
| `class` | `string` | Class is added to the menu's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` | Attrs are any other attributes of the root: id, aria-*, data-*. |

### DropdownItem

DropdownItem is an item of a DropdownMenu, a &lt;slot:item&gt;: what a click does, and the slot's content as its label.

| Prop | Type | |
|---|---|---|
| `onClick` | `any` | OnClick is what a click does: statements, a ui.JS chain or a live handler, onClick={p.Edit}. |
| `destructive` | `bool` |  |
| `disabled` | `bool` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [dropdown_menu.vuka.html]
<script lang="go">
package design

import (
	"time"

	"github.com/vuka-lang/ui/v2"
)

// DropdownMenu is a menu under a button, shown while its Show state holds:
//
//	<design.DropdownMenu>
//	  <slot:trigger>Options</slot:trigger>
//	  <slot:item onClick={p.Edit}>Edit</slot:item>
//	  <slot:item onClick={p.Delete} destructive>Delete</slot:item>
//	</design.DropdownMenu>
//
// Its button toggles it; showing focuses the first item, and the arrow
// keys, Home and End move between them. Escape, a click outside and a
// click on an item write false into Show, and the focus goes back to the
// button.
type DropdownMenu struct {
	// Show is the browser state the menu follows: pass the page's to show
	// it from elsewhere, or leave it to the menu's own.
	Show ui.State[bool]
	// Trigger is the button's content, a <slot:trigger>.
	Trigger ui.Node
	// Items are the menu's items, a <slot:item> each, in order.
	Items []DropdownItem
	// Class is added to the menu's classes, winning over them (see cn).
	Class string
	// Attrs are any other attributes of the root: id, aria-*, data-*.
	Attrs []ui.Attr
}

// trigger is the button's attributes: it says the menu it opens and
// whether it is shown.
func (m *DropdownMenu) trigger() []ui.Attr {
	return []ui.Attr{{Name: "aria-haspopup", Value: "menu"}, ui.BindAttr("aria-expanded", m.Show)}
}

// item is an item's click, none without one.
func (m *DropdownMenu) item(it DropdownItem) []ui.Attr {
	if it.OnClick == nil {
		return nil
	}
	return []ui.Attr{{Name: "onClick", Value: ui.On(it.OnClick)}}
}

// itemClass is an item's classes.
func (m *DropdownMenu) itemClass(it DropdownItem) string {
	if it.Destructive {
		return cn(menuItem, menuItemDestructive)
	}
	return menuItem
}

const menuItem = "relative flex w-full cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm outline-none select-none " +
	"hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground " +
	"disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

const menuItemDestructive = "text-destructive hover:bg-destructive/10 hover:text-destructive focus-visible:bg-destructive/10 focus-visible:text-destructive"
</script>

<div class="relative inline-block" onKeyDown={ui.JS.Key("Escape").Set(Show, false)} {m.Attrs...}>
  if ui.Has(m.Trigger) {
    <Button variant={ButtonOutline} onClick={Show = !Show} attrs={m.trigger()}>{m.Trigger}</Button>
  }
  <div
    role="menu"
    show={Show}
    transition={ui.Scale(100 * time.Millisecond)}
    class={cn("absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md", m.Class)}
  >
    for i, it := range m.Items {
      <button
        key={i}
        type="button"
        role="menuitem"
        tabindex="-1"
        disabled={it.Disabled}
        class={m.itemClass(it)}
        {m.item(it)...}
      >{it.Label}</button>
    }
  </div>
</div>

<script lang="ts">
import { component, state, listen } from 'vuka:component'

// Showing focuses the first item, hiding gives the focus back; a click
// outside or on an item hides it, and the arrow keys move between items.
export default component<DropdownMenu>((el) => {
  const show = state('show')
  const items = () => [...el.querySelectorAll<HTMLButtonElement>('[role=menuitem]:not([disabled])')]
  let back: HTMLElement | null = null
  show.watch((shown) => {
    if (shown) {
      back = document.activeElement instanceof HTMLElement ? document.activeElement : null
      items()[0]?.focus()
      return
    }
    const lost = !document.activeElement || document.activeElement === document.body || el.contains(document.activeElement)
    if (lost && back?.isConnected) back.focus()
    back = null
  })
  listen(document, 'pointerdown', (e) => {
    if (show.get() && !el.contains(e.target as Node)) show.set(false)
  })
  listen(el, 'click', (e) => {
    if ((e.target as Element).closest('[role=menuitem]')) show.set(false)
  })
  listen(el, 'keydown', (e) => {
    const all = items(), at = all.indexOf(document.activeElement as HTMLButtonElement)
    const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: all.length - 1 }[e.key]
    if (to === undefined || !show.get() || !all.length) return
    e.preventDefault()
    all[(to + all.length) % all.length].focus()
  })
})
</script>
```

```go [dropdown_menu_html.go]
package design

import (
	"context"
	"io"
	"time"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

// DropdownMenu is a menu under a button, shown while its Show state holds:
//
//	<design.DropdownMenu>
//	  <slot:trigger>Options</slot:trigger>
//	  <slot:item onClick={p.Edit}>Edit</slot:item>
//	  <slot:item onClick={p.Delete} destructive>Delete</slot:item>
//	</design.DropdownMenu>
//
// Its button toggles it; showing focuses the first item, and the arrow
// keys, Home and End move between them. Escape, a click outside and a
// click on an item write false into Show, and the focus goes back to the
// button.
type DropdownMenu struct {
	// Show is the browser state the menu follows: pass the page's to show
	// it from elsewhere, or leave it to the menu's own.
	Show ui.State[bool]
	// Trigger is the button's content, a <slot:trigger>.
	Trigger ui.Node
	// Items are the menu's items, a <slot:item> each, in order.
	Items []DropdownItem
	// Class is added to the menu's classes, winning over them (see cn).
	Class string
	// Attrs are any other attributes of the root: id, aria-*, data-*.
	Attrs []ui.Attr
}

// trigger is the button's attributes: it says the menu it opens and
// whether it is shown.
func (m *DropdownMenu) trigger() []ui.Attr {
	return []ui.Attr{{Name: "aria-haspopup", Value: "menu"}, ui.BindAttr("aria-expanded", m.Show)}
}

// item is an item's click, none without one.
func (m *DropdownMenu) item(it DropdownItem) []ui.Attr {
	if it.OnClick == nil {
		return nil
	}
	return []ui.Attr{{Name: "onClick", Value: ui.On(it.OnClick)}}
}

// itemClass is an item's classes.
func (m *DropdownMenu) itemClass(it DropdownItem) string {
	if it.Destructive {
		return cn(menuItem, menuItemDestructive)
	}
	return menuItem
}

const menuItem = "relative flex w-full cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm outline-none select-none " +
	"hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground " +
	"disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

const menuItemDestructive = "text-destructive hover:bg-destructive/10 hover:text-destructive focus-visible:bg-destructive/10 focus-visible:text-destructive"

func (m *DropdownMenu) View() ui.Node {
	uigen.States(m)
	var button ui.Node
	if ui.Has(m.Trigger) {
		button = Button{Variant: ButtonOutline, OnClick: uigen.Toggle(ui.JS, m.Show), Attrs: m.trigger(), Children: m.Trigger}
	}
	var buttons []ui.Node
	for i, it := range m.Items {
		buttons = append(buttons, ui.HTML(`<button key={i} type="button" role="menuitem" tabindex="-1" disabled={it.Disabled} class={m.itemClass(it)} {m.item(it)...}>{it.Label}</button>`, i, it.Disabled, m.itemClass(it), m.item(it), it.Label))
	}
	return ui.HTML(`
<div data-vk-p={ui.Props(m)} data-vk-st={ui.States(m)} class="relative inline-block" onKeyDown={ui.JS.Key("Escape").Set(m.Show, false)} {m.Attrs...}>
  {button}
  <div role="menu" show={m.Show} transition={ui.Scale(100 * time.Millisecond)} class={cn("absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md", m.Class)}>{buttons}</div>
</div>`,
		ui.Props(m),
		ui.States(m),
		ui.JS.Key("Escape").Set(m.Show, false),
		m.Attrs,
		button,
		m.Show,
		ui.Scale(100*time.Millisecond),
		cn("absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md", m.Class),
		buttons,
	)
}

func (m *DropdownMenu) Render(ctx context.Context, w io.Writer) error { return m.View().Render(ctx, w) }
```

:::

</div>

Beside it, the same in both: `dropdown_menu.go`.

```go
package design

import "github.com/vuka-lang/ui/v2"

// DropdownItem is an item of a DropdownMenu, a <slot:item>: what a click
// does, and the slot's content as its label.
type DropdownItem struct {
	// OnClick is what a click does: statements, a ui.JS chain or a live
	// handler, onClick={p.Edit}.
	OnClick     any
	Destructive bool
	Disabled    bool
	Label       ui.Node
}
```
