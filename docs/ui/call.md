# Opening Components

[[toc]]

## Introduction

A row's Edit button opens an editor; a Details link opens a panel. Without
help, the page declares state for each (`editing`, `editingID`), renders
the component somewhere in its markup, and writes the methods that fill it.
`ui.JS.Call` opens a component from any template instead: the component
comes up over the page, in a dialog, with the props the call gives it, and
goes when it closes.

```html
<script lang="go">
package pages

import (
	"example.com/shop/components"
	"example.com/shop/models"
	"github.com/vuka-lang/ui/v2"
)

type Products struct {
	Items []models.Product
}

func (p *Products) Reload() { /* … */ }
</script>

<table>
  for _, it := range Items {
    <tr key={it.ID}>
      <td>{it.Name}</td>
      <td><button onClick={ui.JS.Call(components.EditProduct{ID: it.ID, OnSave: Reload})}>Edit</button></td>
    </tr>
  }
</table>
```

The component is any component, of this package or another: a live one
becomes an instance with its own state for as long as it is open; one
without state renders once. The same component can still be placed in a
page's markup, with its own state there.

## How It Appears {#presentation}

The component decides how it shows. When its markup's root is a
`<ui.Dialog>`, that dialog is its presentation; any other component shows
in a dialog frame, with a close button, so any component works:

```html
<script lang="go">
package components

import (
	"context"

	"example.com/shop/models"
	"github.com/vuka-lang/ui/v2"
)

type EditProduct struct {
	ui.Live
	ID       int64
	OnSave   ui.Action
	products *models.Products // injected
	product  models.Product
}

@ui.Mount
func (e *EditProduct) mount(ctx context.Context) error {
	p, err := e.products.Editable(ctx, e.ID) // checks the visitor may edit it
	e.product = p
	return err
}

func (e *EditProduct) Save(ctx context.Context) error {
	if err := e.products.Save(ctx, e.product); err != nil {
		return err
	}
	ui.Close(ctx)
	return nil
}
</script>

<ui.Dialog title={ui.Text("Edit " + product.Name)}>
  <form onSubmit={ui.JS.Push(Save).Success(OnSave)}>
    …
    <button>Save</button>
    <button type="button" onClick={ui.JS.Close()}>Cancel</button>
  </form>
</ui.Dialog>
```

It opens in the page's overlay host, an element at the end of `<body>`,
outside the page's own markup, which the document shell holds; a layout may
place its own (`<div id="vk-overlays" data-vk-overlays></div>`). It shows
modal, a native `<dialog>`: Escape closes the top one, and when it closes
the focus goes back to what had it when it opened. A second `Call` opens a
second component over the first.

## Presentations and Options {#options}

A component shows in its root's presentation — `<ui.Dialog>`, `<ui.Drawer>`
(sliding in from an edge, `side="left"`), `<ui.Popover>` (a small panel by
the element that opened it, not modal: Escape or a press elsewhere closes
it) or `ui.Notice` (a toast) — else in a dialog frame. The call can choose
instead, with options after the component:

| Option | |
|---|---|
| `ui.As(ui.Drawer{Side: "right"})` | show it in that presentation, whatever its root: the root's title and footer, when it has one, and its markup go in it. `As` takes the presentation's settings only |
| `ui.Into("#side")` | open it in place in the element of that id, no dialog, replacing what another `Call` put there; on a live page it belongs to the instance whose markup ran the call, and goes when that one is unmounted |
| `ui.CallKey("cart")` | name it: a second `Call` with the key, while the first is open, gives it the new props instead of opening another — a live one keeps its state |
| `ui.SizeSm`, `ui.SizeLg`, `ui.SizeXl`, `ui.SizeFull` | its width (`ui.SizeMd` is the default) |

```html
<button onClick={ui.JS.Call(components.OrderDetails{ID: o.ID}, ui.As(ui.Drawer{}), ui.SizeLg)}>Details</button>
<button onClick={ui.JS.Call(components.ProductPreview{ID: p.ID}, ui.Into("#preview"))}>Preview</button>
<button onClick={ui.JS.Call(components.Cart{Items: Count}, ui.CallKey("cart"))}>Cart</button>
```

The options travel in the call's token with its props, so a browser can't
change them. Modal presentations stack: a `Call` from inside an opened
component opens over it, Escape closes the top one, and the focus goes back
to what had it when each opened — the button in the dialog below, then the
page's.

### Confirming and Notifying {#confirm}

Two components come with the kit, opened like any other:

```html
<button onClick={ui.JS.Call(ui.ConfirmBox{Title: "Delete order?", Text: "It can't be undone.", Danger: true, OnYes: Remove})}>Delete</button>
<button onClick={ui.JS.Push(Save).Success(ui.JS.Call(ui.Notice{Text: "Saved", Variant: ui.AlertSuccess}))}>Save</button>
```

`ConfirmBox` runs `OnYes` or `OnNo` (both `ui.Action`) and closes; `Yes` and
`No` change its buttons' words. `Notice` shows a toast in the corner of the
window, closing itself after its `Timeout` (4s).

`ui.JS.Confirm(msg)` asks in a ConfirmBox too where the page has an overlay
host — which the document shell writes on a page that renders a `Call` or
a `Confirm` — and waits for the answer; `ui.JS.Confirm(msg,
ui.NativeConfirm())` asks in the browser's own dialog. The browser's is
kept where the event needs the answer at once: a submit, a link, a form's
submit button, an element whose event also goes to the server.

## Props and Results {#props}

The call sends the component's props, so they are what a page can send:

- values JSON carries: numbers, strings, structs of them, slices;
- element reads, in a prop of type `any`, made when the call runs:
  `components.Search{Query: ui.Sel("#q").Value()}`;
- `ui.Action` props: a chain of commands, a method of the page, or a
  closure.

A prop holding markup — a `ui.Node`, a slot, `Children` — can't be sent:
the compiler says so, naming the prop ([E0191](/errors/E0191)). Pass what
the markup is made of, and let the component render it.

Its unexported fields of types the app provides are injected before its
`Mount` runs, as a page's [dependency fields](/web/dependency-injection#dependency-fields)
are, and its handlers' parameters are filled by type as anywhere.

A component that reports back declares an `ui.Action` prop (`OnSave
ui.Action`) and runs it where its user's action goes, `.Success(OnSave)`
or `onClick={OnSave}`; the call site passes a method or a closure, which
runs where the page runs it, as a push of the page would. There is no
result type, so a component from a library reports back the same way. A
closure captures its loop variables when the page renders, as any
handler in a loop does.

`ui.Close(ctx)` in the component's handler, and `ui.JS.Close()` in its
markup, close it; so do its dialog's close button and Escape. Closing a
component destroys it: a live one's instance is unmounted.

A component whose `Mount` fails shows an error state in its dialog, and its
`OnError` prop, when it has one, runs.

## The Call's Lifecycle {#lifecycle}

`Before`, `Success`, `Error` and `After` work on a `Call` as on a
[`Push`](/ui/js#push-lifecycle):

```
Before → open → Success | Error → After … Closed
```

`Success` runs once the component is on screen; `Error` when the open is
refused, the component's `Mount` fails, or no reply comes in time
(`ui.LoadError` reads why); `Closed(a)` once the component has closed, for
whatever reason, where the call ran:

```html
<button onClick={
  ui.JS.Call(components.OrderDetails{ID: o.ID})
    .Before(ui.JS.AddClass(ui.This, "opening"))
    .After(ui.JS.RemoveClass(ui.This, "opening"))
    .Closed(RefreshTotals)
}>Details</button>
```

On the server, `ui.Call(ctx, components.OrderDetails{ID: id})` opens one
from a handler or a pushed method, once its reply is applied.

## Live Pages and Pages That Aren't {#transports}

A call goes over a live page's connection: the session mounts the
component and renders it with the page, so it patches like any other part
of the page while it is open. On a page that isn't live it goes over HTTP
to the page's own URL, which renders the component once; its pushes reach
its methods over HTTP, as the page's do. A live component makes the page
that calls it live; a component without state, and `Action` props that are
methods, leave it as it is.

## Safety {#safety}

The compiler registers every component a `Call` in the markup names; no
other can be opened. The call carries a token: the component's name, its
props, what it is bound to — the live page's session, or the visitor's
session cookie and who is signed in — and an expiry, signed with the app's
key. A browser can't change the props, name another component, or use a
token another visitor's page rendered.

> [!WARNING]
> The token proves only what the page rendered. The component's `Mount`
> must still check that the visitor may see — and edit — the record its
> props name, as a route does.
