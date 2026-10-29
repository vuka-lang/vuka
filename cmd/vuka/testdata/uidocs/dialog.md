```vuka-html
<design.Button variant="outline" on={design.OpenDialog("share")}>Share…</design.Button>

<design.Dialog id="share" title="Share the project" description="Anyone with the link can view it.">
  <design.Input id="link" value={link} />
  <slot:footer>
    <design.Button on={design.CloseDialog("share")}>Done</design.Button>
  </slot:footer>
</design.Dialog>
```

A `<dialog>` element over a backdrop, opened and closed by JS commands in
the browser: `OpenDialog(id)` remembers what had the focus, opens it and
focuses its first control (one marked `autofocus` first); Escape, a click on
the backdrop, its close button and `CloseDialog(id)` close it and give the
focus back. A live render while it is open keeps it open.

A live page's handlers drive it with the same commands, through `PushJS`,
and `onClose` tells the page when the visitor closed it:

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

// AskDelete opens the confirmation.
func (p *Project) AskDelete() { p.PushJS(design.OpenDialog("delete")) }

// Delete deletes the project and closes it.
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
