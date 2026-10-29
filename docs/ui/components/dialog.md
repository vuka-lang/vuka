---
title: Dialog
description: "A <dialog> over the page, opened and closed by JS commands."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Dialog

A &lt;dialog&gt; over the page, opened and closed by JS commands.

<div class="vk-ui-shots">
  <img src="/ui/components/dialog-light.png" alt="Dialog in the light theme">
  <img src="/ui/components/dialog-dark.png" alt="Dialog in the dark theme">
</div>

## Adding It

```sh
vuka ui add dialog
```

It writes `components/ui/dialog.vuka.html`, with what it uses: [Button](/ui/components/button), [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

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

## Props

| Prop | Type | |
|---|---|---|
| `id` | `string` | Required. ID is the dialog element's id, what OpenDialog and CloseDialog name; unique on the page. |
| `title` | `string` |  |
| `description` | `string` |  |
| `<slot:footer>` | `ui.Node` | Footer is a slot under the content, for the dialog's buttons. |
| `open` | `bool` | Open renders the dialog open, as the page loads. |
| `onClose` | `any` | OnClose, when set, is called when the visitor closes the dialog (Escape, the backdrop, the close button): a live handler. |
| `class` | `string` | Class is added to the panel's classes, winning over them (see cn). |
| the content | `ui.Node` |  |

### Functions

- `OpenDialog(id string) ui.Commands`: OpenDialog opens the dialog whose ID is id: it remembers the focused element and focuses the dialog's first control.
- `CloseDialog(id string) ui.Commands`: CloseDialog closes the dialog whose ID is id, giving the focus back to what had it before it opened.

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [dialog.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Dialog is a window over the page, a <dialog> element: hidden until
// OpenDialog opens it, from a button anywhere on the page.
//
//	<design.Button variant={design.ButtonDestructive} on={design.OpenDialog("delete")}>Delete…</design.Button>
//	<design.Dialog id="delete" title="Delete the project?" description="This can't be undone.">
//	  <slot:footer>
//	    <design.Button variant={design.ButtonOutline} on={design.CloseDialog("delete")}>Cancel</design.Button>
//	    <design.Button variant={design.ButtonDestructive} on={p.Delete}>Delete</design.Button>
//	  </slot:footer>
//	</design.Dialog>
//
// Opening remembers what had the focus and focuses the dialog's first
// control (one marked autofocus first); Escape, a click on the backdrop and
// the close button close it, and the focus goes back. That all happens in
// the browser, JS commands with no round trip. A live page's handler opens
// and closes it with the same commands, p.PushJS(design.OpenDialog("delete")),
// and OnClose tells it when the visitor closed it.
type Dialog struct {
	// ID is the dialog element's id, what OpenDialog and CloseDialog name;
	// unique on the page.
	ID          string `vuka:"required"`
	Title       string
	Description string
	// Footer is a slot under the content, for the dialog's buttons.
	Footer ui.Node
	// Open renders the dialog open, as the page loads.
	Open bool
	// OnClose, when set, is called when the visitor closes the dialog
	// (Escape, the backdrop, the close button): a live handler.
	OnClose any
	// Class is added to the panel's classes, winning over them (see cn).
	Class    string
	Children ui.Node
}

// OpenDialog opens the dialog whose ID is id: it remembers the focused
// element and focuses the dialog's first control.
func OpenDialog(id string) ui.Commands {
	d := ui.Sel("#" + id)
	return ui.JS.PushFocus().SetAttr(d, "open", "").FocusFirst(d)
}

// CloseDialog closes the dialog whose ID is id, giving the focus back to
// what had it before it opened.
func CloseDialog(id string) ui.Commands {
	return ui.JS.RemoveAttr(ui.Sel("#"+id), "open").PopFocus()
}

// close is what the backdrop and the close button do.
func (d Dialog) close() ui.Commands { return d.then(CloseDialog(d.ID)) }

// escape is the dialog's keydown handler: Escape closes it.
func (d Dialog) escape() ui.Commands {
	return d.then(ui.JS.Key("Escape").RemoveAttr(ui.Sel("#"+d.ID), "open").PopFocus())
}

// then is c, then OnClose.
func (d Dialog) then(c ui.Commands) ui.Commands {
	if d.OnClose != nil {
		return c.Push(d.OnClose)
	}
	return c
}

// part is the id of the dialog's title or description, nil when it has
// none.
func (d Dialog) part(name, text string) any {
	if text == "" {
		return nil
	}
	return d.ID + "-" + name
}

// closeLabel names the close button for assistive technology.
var closeLabel = []ui.Attr{{Name: "aria-label", Value: "Close"}}
</script>

<dialog
  id={d.ID}
  open={d.Open}
  aria-modal="true"
  aria-labelledby={d.part("title", d.Title)}
  aria-describedby={d.part("description", d.Description)}
  class="fixed inset-0 z-50 m-0 size-full max-h-none max-w-none border-0 bg-transparent p-0 text-foreground"
  onKeyDown={d.escape()}
>
  <div aria-hidden="true" class="absolute inset-0 bg-black/50" onClick={d.close()}></div>
  <div class={cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class)}>
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
```

```go [dialog_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// Dialog is a window over the page, a <dialog> element: hidden until
// OpenDialog opens it, from a button anywhere on the page.
//
//	<design.Button variant={design.ButtonDestructive} on={design.OpenDialog("delete")}>Delete…</design.Button>
//	<design.Dialog id="delete" title="Delete the project?" description="This can't be undone.">
//	  <slot:footer>
//	    <design.Button variant={design.ButtonOutline} on={design.CloseDialog("delete")}>Cancel</design.Button>
//	    <design.Button variant={design.ButtonDestructive} on={p.Delete}>Delete</design.Button>
//	  </slot:footer>
//	</design.Dialog>
//
// Opening remembers what had the focus and focuses the dialog's first
// control (one marked autofocus first); Escape, a click on the backdrop and
// the close button close it, and the focus goes back. That all happens in
// the browser, JS commands with no round trip. A live page's handler opens
// and closes it with the same commands, p.PushJS(design.OpenDialog("delete")),
// and OnClose tells it when the visitor closed it.
type Dialog struct {
	// ID is the dialog element's id, what OpenDialog and CloseDialog name;
	// unique on the page.
	ID          string `vuka:"required"`
	Title       string
	Description string
	// Footer is a slot under the content, for the dialog's buttons.
	Footer ui.Node
	// Open renders the dialog open, as the page loads.
	Open bool
	// OnClose, when set, is called when the visitor closes the dialog
	// (Escape, the backdrop, the close button): a live handler.
	OnClose any
	// Class is added to the panel's classes, winning over them (see cn).
	Class    string
	Children ui.Node
}

// OpenDialog opens the dialog whose ID is id: it remembers the focused
// element and focuses the dialog's first control.
func OpenDialog(id string) ui.Commands {
	d := ui.Sel("#" + id)
	return ui.JS.PushFocus().SetAttr(d, "open", "").FocusFirst(d)
}

// CloseDialog closes the dialog whose ID is id, giving the focus back to
// what had it before it opened.
func CloseDialog(id string) ui.Commands {
	return ui.JS.RemoveAttr(ui.Sel("#"+id), "open").PopFocus()
}

// close is what the backdrop and the close button do.
func (d Dialog) close() ui.Commands { return d.then(CloseDialog(d.ID)) }

// escape is the dialog's keydown handler: Escape closes it.
func (d Dialog) escape() ui.Commands {
	return d.then(ui.JS.Key("Escape").RemoveAttr(ui.Sel("#"+d.ID), "open").PopFocus())
}

// then is c, then OnClose.
func (d Dialog) then(c ui.Commands) ui.Commands {
	if d.OnClose != nil {
		return c.Push(d.OnClose)
	}
	return c
}

// part is the id of the dialog's title or description, nil when it has
// none.
func (d Dialog) part(name, text string) any {
	if text == "" {
		return nil
	}
	return d.ID + "-" + name
}

// closeLabel names the close button for assistive technology.
var closeLabel = []ui.Attr{{Name: "aria-label", Value: "Close"}}

func (d Dialog) View() ui.Node {
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
<dialog id={d.ID} open={d.Open} aria-modal="true" aria-labelledby={d.part("title", d.Title)} aria-describedby={d.part("description", d.Description)} class="fixed inset-0 z-50 m-0 size-full max-h-none max-w-none border-0 bg-transparent p-0 text-foreground" onKeyDown={d.escape()}>
  <div aria-hidden="true" class="absolute inset-0 bg-black/50" onClick={d.close()}/>
  <div class={cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class)}>
    {div}
    {d.Children}
    {div2}
    {button}
  </div>
</dialog>`,
		d.ID,
		d.Open,
		d.part("title", d.Title),
		d.part("description", d.Description),
		d.escape(),
		d.close(),
		cn("absolute top-1/2 left-1/2 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-4 rounded-lg border bg-background p-6 shadow-lg sm:max-w-lg", d.Class),
		div,
		d.Children,
		div2,
		button,
	)
}

func (d Dialog) Render(ctx context.Context, w io.Writer) error { return d.View().Render(ctx, w) }
```

:::

</div>
