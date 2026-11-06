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
