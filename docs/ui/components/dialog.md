---
title: Dialog
description: "A <dialog> over the page, shown while a browser state holds."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Dialog

A &lt;dialog&gt; over the page, shown while a browser state holds.

<div class="vk-ui-shots">
  <img src="/ui/components/dialog-light.png" alt="Dialog in the light theme">
  <img src="/ui/components/dialog-dark.png" alt="Dialog in the dark theme">
</div>

## Adding It

```sh
vuka ui add dialog
```

It writes `components/ui/dialog.vuka.html`, with what it uses: [Button](/ui/components/button), [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` at commit `c2870b7828d7` or newer.

## Using It

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

## Props

| Prop | Type | |
|---|---|---|
| `show` | `ui.State[bool]` | Show is the browser state the dialog follows: pass the page's, or leave it to the dialog's own, which OpenDialog and CloseDialog set. |
| `id` | `string` | ID is the dialog element's id, what OpenDialog and CloseDialog name; unique on the page. |
| `title` | `string` |  |
| `description` | `string` |  |
| `<slot:footer>` | `ui.Node` | Footer is a slot under the content, for the dialog's buttons. |
| `open` | `bool` | Open shows the dialog while it holds: a live page's server state. |
| `onClose` | `any` | OnClose, when set, is called when the visitor closes the dialog (Escape, the backdrop, the close button): a live handler. |
| `class` | `string` | Class is added to the panel's classes, winning over them (see cn). |
| the content | `ui.Node` |  |

### Functions

- `OpenDialog(id string) ui.Commands`: OpenDialog shows the dialog whose ID is id, writing true into its Show.
- `CloseDialog(id string) ui.Commands`: CloseDialog hides the dialog whose ID is id, writing false into its Show.

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [dialog.vuka.html]
<script lang="go">
package design

import (
	"time"

	"github.com/vuka-lang/ui/v2"
)

// Dialog is a window over the page, a <dialog> element shown while its
// Show state holds:
//
//	open ui.State[bool]
//
//	<design.Button onClick={open = true}>Add scale</design.Button>
//	<design.Dialog show={open} title="Add scale">
//	  …
//	  <slot:footer>
//	    <design.Button variant={design.ButtonOutline} onClick={open = false}>Cancel</design.Button>
//	  </slot:footer>
//	</design.Dialog>
//
// Showing remembers what had the focus and focuses the dialog's first
// control (one marked autofocus first); hiding gives the focus back.
// Escape, a click on the backdrop and the close button write false into
// Show, so anything reading the state follows. A live page's server bool
// shows it with Open, and the commands OpenDialog and CloseDialog show and
// hide a dialog by its ID; OnClose tells a live page when the visitor
// closed it.
type Dialog struct {
	// Show is the browser state the dialog follows: pass the page's, or
	// leave it to the dialog's own, which OpenDialog and CloseDialog set.
	Show ui.State[bool]
	// ID is the dialog element's id, what OpenDialog and CloseDialog name;
	// unique on the page.
	ID          string
	Title       string
	Description string
	// Footer is a slot under the content, for the dialog's buttons.
	Footer ui.Node
	// Open shows the dialog while it holds: a live page's server state.
	Open bool
	// OnClose, when set, is called when the visitor closes the dialog
	// (Escape, the backdrop, the close button): a live handler.
	OnClose any
	// Class is added to the panel's classes, winning over them (see cn).
	Class    string
	Children ui.Node
}

// OpenDialog shows the dialog whose ID is id, writing true into its Show.
func OpenDialog(id string) ui.Commands {
	return ui.JS.Dispatch(ui.Sel("#"+id), "dialogopen", ui.NoBubble())
}

// CloseDialog hides the dialog whose ID is id, writing false into its
// Show.
func CloseDialog(id string) ui.Commands {
	return ui.JS.Dispatch(ui.Sel("#"+id), "dialogclose", ui.NoBubble())
}

// close is what the backdrop and the close button do.
func (d Dialog) close() ui.Commands { return d.then(d.Show.Set(false)) }

// escape is the dialog's keydown handler: Escape closes it.
func (d Dialog) escape() ui.Commands { return d.then(ui.JS.Key("Escape").Set(d.Show, false)) }

// then is c, then OnClose.
func (d Dialog) then(c ui.Commands) ui.Commands {
	if d.OnClose != nil {
		return c.Push(d.OnClose)
	}
	return c
}

func (d Dialog) id() any {
	if d.ID == "" {
		return nil
	}
	return d.ID
}

// part is the id of the dialog's title or description, nil when it has
// none or the dialog has no ID.
func (d Dialog) part(name, text string) any {
	if text == "" || d.ID == "" {
		return nil
	}
	return d.ID + "-" + name
}

// label names a dialog without an ID by its title.
func (d Dialog) label() any {
	if d.ID != "" || d.Title == "" {
		return nil
	}
	return d.Title
}

// closeLabel names the close button for assistive technology.
var closeLabel = []ui.Attr{{Name: "aria-label", Value: "Close"}}

const dialogTime = 150 * time.Millisecond
</script>

<dialog
  id={d.id()}
  open
  show={Show || Open}
  transition={ui.Fade(dialogTime)}
  aria-modal="true"
  aria-label={d.label()}
  aria-labelledby={d.part("title", d.Title)}
  aria-describedby={d.part("description", d.Description)}
  class="fixed inset-0 z-50 m-0 size-full max-h-none max-w-none border-0 bg-transparent p-0 text-foreground"
  onDialogOpen={Show = true}
  onDialogClose={Show = false}
  onKeyDown={d.escape()}
>
  <div aria-hidden="true" class="absolute inset-0 bg-black/50" onClick={d.close()}></div>
  <div
    show={Show || Open}
    transition={ui.Scale(dialogTime)}
    class={cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class)}
  >
    if d.Title != "" || d.Description != "" {
      <div class="flex flex-col gap-2 pr-6 text-center sm:text-left">
        if d.Title != "" {
          <h2 id={d.part("title", d.Title)} class="text-lg leading-none font-semibold">{d.Title}</h2>
        }
        if d.Description != "" {
          <p id={d.part("description", d.Description)} class="text-sm text-muted-foreground">{d.Description}</p>
        }
      </div>
    }
    {d.Children}
    if ui.Has(d.Footer) {
      <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">{d.Footer}</div>
    }
    <Button
      variant={ButtonGhost}
      size={SizeIcon}
      class="absolute top-3 right-3 size-7 opacity-70 hover:opacity-100"
      on={d.close()}
      attrs={closeLabel}
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      ><path d="M18 6 6 18" /><path d="m6 6 12 12" /></svg>
    </Button>
  </div>
</dialog>

<script lang="ts">
import { component, state } from 'vuka:component'

const focusable = 'a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Showing focuses the first control, hiding gives the focus back.
export default component<Dialog>((el) => {
  let back: HTMLElement | null = null
  state('show').watch((shown) => {
    if (shown) {
      back = document.activeElement instanceof HTMLElement ? document.activeElement : null
      const first = el.querySelector<HTMLElement>('[autofocus]') ?? el.querySelector<HTMLElement>(focusable)
      first?.focus()
      return
    }
    if (back?.isConnected) back.focus()
    back = null
  })
})
</script>
```

```go [dialog_html.go]
package design

import (
	"context"
	"io"
	"time"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

// Dialog is a window over the page, a <dialog> element shown while its
// Show state holds:
//
//	open ui.State[bool]
//
//	<design.Button onClick={open = true}>Add scale</design.Button>
//	<design.Dialog show={open} title="Add scale">
//	  …
//	  <slot:footer>
//	    <design.Button variant={design.ButtonOutline} onClick={open = false}>Cancel</design.Button>
//	  </slot:footer>
//	</design.Dialog>
//
// Showing remembers what had the focus and focuses the dialog's first
// control (one marked autofocus first); hiding gives the focus back.
// Escape, a click on the backdrop and the close button write false into
// Show, so anything reading the state follows. A live page's server bool
// shows it with Open, and the commands OpenDialog and CloseDialog show and
// hide a dialog by its ID; OnClose tells a live page when the visitor
// closed it.
type Dialog struct {
	// Show is the browser state the dialog follows: pass the page's, or
	// leave it to the dialog's own, which OpenDialog and CloseDialog set.
	Show ui.State[bool]
	// ID is the dialog element's id, what OpenDialog and CloseDialog name;
	// unique on the page.
	ID          string
	Title       string
	Description string
	// Footer is a slot under the content, for the dialog's buttons.
	Footer ui.Node
	// Open shows the dialog while it holds: a live page's server state.
	Open bool
	// OnClose, when set, is called when the visitor closes the dialog
	// (Escape, the backdrop, the close button): a live handler.
	OnClose any
	// Class is added to the panel's classes, winning over them (see cn).
	Class    string
	Children ui.Node
}

// OpenDialog shows the dialog whose ID is id, writing true into its Show.
func OpenDialog(id string) ui.Commands {
	return ui.JS.Dispatch(ui.Sel("#"+id), "dialogopen", ui.NoBubble())
}

// CloseDialog hides the dialog whose ID is id, writing false into its
// Show.
func CloseDialog(id string) ui.Commands {
	return ui.JS.Dispatch(ui.Sel("#"+id), "dialogclose", ui.NoBubble())
}

// close is what the backdrop and the close button do.
func (d Dialog) close() ui.Commands { return d.then(d.Show.Set(false)) }

// escape is the dialog's keydown handler: Escape closes it.
func (d Dialog) escape() ui.Commands { return d.then(ui.JS.Key("Escape").Set(d.Show, false)) }

// then is c, then OnClose.
func (d Dialog) then(c ui.Commands) ui.Commands {
	if d.OnClose != nil {
		return c.Push(d.OnClose)
	}
	return c
}

func (d Dialog) id() any {
	if d.ID == "" {
		return nil
	}
	return d.ID
}

// part is the id of the dialog's title or description, nil when it has
// none or the dialog has no ID.
func (d Dialog) part(name, text string) any {
	if text == "" || d.ID == "" {
		return nil
	}
	return d.ID + "-" + name
}

// label names a dialog without an ID by its title.
func (d Dialog) label() any {
	if d.ID != "" || d.Title == "" {
		return nil
	}
	return d.Title
}

// closeLabel names the close button for assistive technology.
var closeLabel = []ui.Attr{{Name: "aria-label", Value: "Close"}}

const dialogTime = 150 * time.Millisecond

func (d *Dialog) View() ui.Node {
	uigen.States(d)
	value2 := uigen.Or(d.Show, d.Open)
	var div ui.Node
	if d.Title != "" || d.Description != "" {
		var h2 ui.Node
		if d.Title != "" {
			h2 = ui.HTML(`<h2 id={d.part("title", d.Title)} class="text-lg leading-none font-semibold">{d.Title}</h2>`, d.part("title", d.Title), d.Title)
		}
		var p2 ui.Node
		if d.Description != "" {
			p2 = ui.HTML(`<p id={d.part("description", d.Description)} class="text-sm text-muted-foreground">{d.Description}</p>`, d.part("description", d.Description), d.Description)
		}
		div = ui.HTML(`
<div class="flex flex-col gap-2 pr-6 text-center sm:text-left">
  {h2}
  {p2}
</div>`, h2, p2)
	}
	var div2 ui.Node
	if ui.Has(d.Footer) {
		div2 = ui.HTML(`<div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">{d.Footer}</div>`, d.Footer)
	}
	button := Button{Variant: ButtonGhost, Size: SizeIcon, Class: "absolute top-3 right-3 size-7 opacity-70 hover:opacity-100", On: d.close(), Attrs: closeLabel, Children: ui.HTML(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  <path d="M18 6 6 18"/>
  <path d="m6 6 12 12"/>
</svg>`)}
	return ui.HTML(`
<dialog data-vk-p={ui.Props(d)} data-vk-st={ui.States(d)} id={d.id()} open show={uigen.Or(d.Show, d.Open)} transition={ui.Fade(dialogTime)} aria-modal="true" aria-label={d.label()} aria-labelledby={d.part("title", d.Title)} aria-describedby={d.part("description", d.Description)} class="fixed inset-0 z-50 m-0 size-full max-h-none max-w-none border-0 bg-transparent p-0 text-foreground" onDialogOpen={ui.JS.Set(d.Show, true)} onDialogClose={ui.JS.Set(d.Show, false)} onKeyDown={d.escape()}>
  <div aria-hidden="true" class="absolute inset-0 bg-black/50" onClick={d.close()}/>
  <div show={value2} transition={ui.Scale(dialogTime)} class={cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class)}>
    {div}
    {d.Children}
    {div2}
    {button}
  </div>
</dialog>`,
		ui.Props(d),
		ui.States(d),
		d.id(),
		uigen.Or(d.Show, d.Open),
		ui.Fade(dialogTime),
		d.label(),
		d.part("title", d.Title),
		d.part("description", d.Description),
		ui.JS.Set(d.Show, true),
		ui.JS.Set(d.Show, false),
		d.escape(),
		d.close(),
		value2,
		ui.Scale(dialogTime),
		cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class),
		div,
		d.Children,
		div2,
		button,
	)
}

func (d *Dialog) Render(ctx context.Context, w io.Writer) error { return d.View().Render(ctx, w) }
```

:::

</div>
