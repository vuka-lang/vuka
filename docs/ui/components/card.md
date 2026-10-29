---
title: Card
description: "A panel with a title, a description, an action, its content and a footer."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Card

A panel with a title, a description, an action, its content and a footer.

<div class="vk-ui-shots">
  <img src="/ui/components/card-light.png" alt="Card in the light theme">
  <img src="/ui/components/card-dark.png" alt="Card in the dark theme">
</div>

## Adding It

```sh
vuka ui add card
```

It writes `components/ui/card.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Card title="Team" description="Who can see this project.">
  <slot:action><design.Button size={design.SizeSm}>Invite</design.Button></slot:action>
  <p>Three members.</p>
  <slot:footer>Updated today</slot:footer>
</design.Card>
```

The header (title, description and the `action` slot beside them), the
content and the `footer` slot: each part is left out when empty.

## Props

| Prop | Type | |
|---|---|---|
| `title` | `string` |  |
| `description` | `string` |  |
| `<slot:action>` | `ui.Node` | Action is a slot beside the title, such as a button. |
| `<slot:footer>` | `ui.Node` | Footer is a slot under the content. |
| `class` | `string` | Class is added to the card's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [card.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Card is a panel: a header with a title, a description and an action
// beside them, its content, and a footer, each left out when empty:
//
//	<design.Card title="Team" description="Who can see this project.">
//	  <slot:action><design.Button size={design.SizeSm}>Invite</design.Button></slot:action>
//	  …
//	  <slot:footer>3 members</slot:footer>
//	</design.Card>
type Card struct {
	Title       string
	Description string
	// Action is a slot beside the title, such as a button.
	Action ui.Node
	// Footer is a slot under the content.
	Footer ui.Node
	// Class is added to the card's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// header reports whether the card has a header.
func (c Card) header() bool { return c.Title != "" || c.Description != "" || ui.Has(c.Action) }
</script>

<div
  class={cn("flex flex-col gap-6 rounded-xl border bg-card py-6 text-card-foreground shadow-sm", c.Class)}
  {c.Attrs...}
>
  if c.header() {
    <div class="grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[>[data-slot=card-action]]:grid-cols-[1fr_auto]">
      if c.Title != "" {
        <h3 class="leading-none font-semibold">{c.Title}</h3>
      }
      if c.Description != "" {
        <p class="text-sm text-muted-foreground">{c.Description}</p>
      }
      if ui.Has(c.Action) {
        <div
          data-slot="card-action"
          class="col-start-2 row-span-2 row-start-1 self-start justify-self-end"
        >{c.Action}</div>
      }
    </div>
  }
  if ui.Has(c.Children) {
    <div class="px-6">{c.Children}</div>
  }
  if ui.Has(c.Footer) {
    <div class="flex items-center px-6">{c.Footer}</div>
  }
</div>
```

```go [card_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// Card is a panel: a header with a title, a description and an action
// beside them, its content, and a footer, each left out when empty:
//
//	<design.Card title="Team" description="Who can see this project.">
//	  <slot:action><design.Button size={design.SizeSm}>Invite</design.Button></slot:action>
//	  …
//	  <slot:footer>3 members</slot:footer>
//	</design.Card>
type Card struct {
	Title       string
	Description string
	// Action is a slot beside the title, such as a button.
	Action ui.Node
	// Footer is a slot under the content.
	Footer ui.Node
	// Class is added to the card's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// header reports whether the card has a header.
func (c Card) header() bool { return c.Title != "" || c.Description != "" || ui.Has(c.Action) }

func (c Card) View() ui.Node {
	var div ui.Node
	if c.header() {
		var h3 ui.Node
		if c.Title != "" {
			h3 = ui.HTML(`<h3 class="leading-none font-semibold">{c.Title}</h3>`, c.Title)
		}
		var p2 ui.Node
		if c.Description != "" {
			p2 = ui.HTML(`<p class="text-sm text-muted-foreground">{c.Description}</p>`, c.Description)
		}
		var div2 ui.Node
		if ui.Has(c.Action) {
			div2 = ui.HTML(`<div data-slot="card-action" class="col-start-2 row-span-2 row-start-1 self-start justify-self-end">{c.Action}</div>`, c.Action)
		}
		div = ui.HTML(`
<div class="grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-[>[data-slot=card-action]]:grid-cols-[1fr_auto]">
  {h3}
  {p2}
  {div2}
</div>`, h3, p2, div2)
	}
	var div3 ui.Node
	if ui.Has(c.Children) {
		div3 = ui.HTML(`<div class="px-6">{c.Children}</div>`, c.Children)
	}
	var div4 ui.Node
	if ui.Has(c.Footer) {
		div4 = ui.HTML(`<div class="flex items-center px-6">{c.Footer}</div>`, c.Footer)
	}
	return ui.HTML(`
<div class={cn("flex flex-col gap-6 rounded-xl border bg-card py-6 text-card-foreground shadow-sm", c.Class)} {c.Attrs...}>
  {div}
  {div3}
  {div4}
</div>`, cn("flex flex-col gap-6 rounded-xl border bg-card py-6 text-card-foreground shadow-sm", c.Class), c.Attrs, div, div3, div4)
}

func (c Card) Render(ctx context.Context, w io.Writer) error { return c.View().Render(ctx, w) }
```

:::

</div>
