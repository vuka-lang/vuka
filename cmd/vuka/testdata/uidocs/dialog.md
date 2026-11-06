```vuka-html
<script lang="go">
package projects

import "github.com/vuka-lang/ui/v2"

// Share is a project's share button and dialog.
type Share struct {
	open ui.State[bool]
	Link string
}
</script>

<div>
  <design.Button variant="outline" onClick={open = true}>Share…</design.Button>
  <design.Dialog show={open} title="Share the project" description="Anyone with the link can view it.">
    <design.Input id="link" value={Link} />
    <slot:footer>
      <design.Button onClick={open = false}>Done</design.Button>
    </slot:footer>
  </design.Dialog>
</div>
```

A `<dialog>` element over a backdrop, shown while the
[browser state](/ui/state) passed as `show` holds. The state is the one
source of truth: anything that writes it shows or hides the dialog — a
button, a keyboard shortcut, a live handler's `p.open = true` — and
anything that reads it follows. Escape, a click on the backdrop and its
close button write `false` into it.

Showing fades the backdrop in and scales the panel up, a 150ms
[transition](/ui/state#show) that a browser preferring reduced motion
skips; it remembers what had the focus and focuses the first control (one
marked `autofocus` first), and hiding gives the focus back. That is a
[watch](/ui/state#watching) in the component's script, so it runs however
the state was written. A live render while it is shown keeps it shown.

## Three Ways to Show It {#tiers}

| | Shows it | Hides it | For |
|---|---|---|---|
| Browser state (`show={open}`) | a write of `true` | a write of `false` | most dialogs |
| A server bool (`open={editing.ID != 0}`) | a live render with it true | a live render with it false: `onClose` clears it | a dialog a live page's data decides |
| Commands (`OpenDialog(id)`, `CloseDialog(id)`) | `OpenDialog` | `CloseDialog` | a handler's `PushJS`, or a chain built in Go |

A dialog given no state keeps its own, which the commands write, so they
show and focus it the same way:

```vuka-html
<script lang="go">
package projects

import (
	"github.com/vuka-lang/ui/v2"

	design "example.com/shop/components/ui"
)

// Project is a project's page.
type Project struct {
	ui.Live
	deleted bool
}

// AskDelete shows the confirmation.
func (p *Project) AskDelete() { p.PushJS(design.OpenDialog("delete")) }

// Delete deletes the project and hides it.
func (p *Project) Delete() {
	p.deleted = true
	p.PushJS(design.CloseDialog("delete"))
}

// Kept hears that the visitor closed it.
func (p *Project) Kept() {}
</script>

<div>
  <design.Button variant={design.ButtonDestructive} on={AskDelete}>Delete…</design.Button>
  <design.Dialog id="delete" title="Delete the project?" description="This can't be undone." onClose={Kept}>
    <slot:footer>
      <design.Button variant={design.ButtonDestructive} on={Delete}>Delete</design.Button>
    </slot:footer>
  </design.Dialog>
</div>
```

`onClose` tells a live page when the visitor closed the dialog, in any of
the three. A dialog without an `id` is named by its title for assistive
technology; give it an `id` to label it by its title and description
elements, and for the commands.
