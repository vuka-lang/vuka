---
title: Button
description: "A button: six variants, four sizes, a ui.JS chain or a live handler on click."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Button

A button: six variants, four sizes, a ui.JS chain or a live handler on click.

<div class="vk-ui-shots">
  <img src="/ui/components/button-light.png" alt="Button in the light theme">
  <img src="/ui/components/button-dark.png" alt="Button in the dark theme">
</div>

## Adding It

```sh
vuka ui add button
```

It writes `components/ui/button.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.17.0 or newer.

## Using It

```vuka-html
<design.Button on={Save}>Save</design.Button>
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
<design.Button variant={design.ButtonGhost} size={design.SizeIcon} attrs={closeLabel}><svg>…</svg></design.Button>
<design.Button type="submit" disabled={busy}>Send</design.Button>
<design.Button on={ui.JS.Toggle(ui.Sel("#menu"))}>Menu</design.Button>
```

`on` takes a live handler or a `ui.JS` chain. The variant can be written as
its string too: `variant="outline"`.

The classes are data: `buttonVariants`, a [`ui.Variants`](/ui/classes),
holds each variant's and size's classes, and
`buttonVariants.Class("variant", …, "size", …, "class", Class)` merges them
with yours last, so your `class` wins. A new variant is a constant and an
entry under `"variant"`; the editor completes `variant="…"` from them.

## Props

| Prop | Type | |
|---|---|---|
| `id` | `string` | ID is the button's id, e.g. for a JS command to target it. |
| `variant` | `ButtonVariant` | One of `ButtonDefault`, `ButtonDestructive`, `ButtonOutline`, `ButtonSecondary`, `ButtonGhost`, `ButtonLink`. |
| `size` | `ButtonSize` | One of `SizeDefault`, `SizeSm`, `SizeLg`, `SizeIcon`. |
| `on` | `any` | On is what a click does: a ui.JS chain, or a live handler. |
| `type` | `string` | Type is "button" (the default), "submit" or "reset". |
| `disabled` | `bool` |  |
| `class` | `string` | Class is added to the button's classes, winning over them (ui.MergeClasses). |
| `attrs` | `[]ui.Attr` | Attrs are any other attributes: id, name, value, aria-*, data-*. |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [button.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// ButtonVariant is a Button's look.
type ButtonVariant string

const (
	ButtonDefault     ButtonVariant = "" // the primary colour
	ButtonDestructive ButtonVariant = "destructive"
	ButtonOutline     ButtonVariant = "outline"
	ButtonSecondary   ButtonVariant = "secondary"
	ButtonGhost       ButtonVariant = "ghost"
	ButtonLink        ButtonVariant = "link"
)

// ButtonSize is a Button's size.
type ButtonSize string

const (
	SizeDefault ButtonSize = ""
	SizeSm      ButtonSize = "sm"
	SizeLg      ButtonSize = "lg"
	SizeIcon    ButtonSize = "icon" // a square for an icon; give it an aria-label
)

// Button is a button:
//
//	<design.Button on={p.Save}>Save</design.Button>
//	<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
//	<design.Button on={ui.JS.Toggle(ui.Sel("#menu"))}>Menu</design.Button>
type Button struct {
	// ID is the button's id, e.g. for a JS command to target it.
	ID      string
	Variant ButtonVariant
	Size    ButtonSize
	// On is what a click does: a ui.JS chain, or a live handler.
	On any
	// Type is "button" (the default), "submit" or "reset".
	Type     string
	Disabled bool
	// Class is added to the button's classes, winning over them (ui.MergeClasses).
	Class string
	// Attrs are any other attributes: id, name, value, aria-*, data-*.
	Attrs    []ui.Attr
	Children ui.Node
}

const buttonBase = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all outline-none " +
	"disabled:pointer-events-none disabled:opacity-50 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 " +
	"aria-invalid:border-destructive aria-invalid:ring-destructive/20 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

var buttonVariants = ui.Variants{
	Base: buttonBase,
	Variants: map[string]map[string]string{
		"variant": {
			"default":     "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90",
			"destructive": "bg-destructive text-white shadow-xs hover:bg-destructive/90 focus-visible:ring-destructive/20",
			"outline":     "border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground",
			"secondary":   "bg-secondary text-secondary-foreground shadow-xs hover:bg-secondary/80",
			"ghost":       "hover:bg-accent hover:text-accent-foreground",
			"link":        "text-primary underline-offset-4 hover:underline",
		},
		"size": {
			"default": "h-9 px-4 py-2 has-[>svg]:px-3",
			"sm":      "h-8 gap-1.5 rounded-md px-3 has-[>svg]:px-2.5",
			"lg":      "h-10 rounded-md px-6 has-[>svg]:px-4",
			"icon":    "size-9",
		},
	},
	Defaults: map[string]string{"variant": "default", "size": "default"},
}

func (b Button) id() any {
	if b.ID == "" {
		return nil
	}
	return b.ID
}

func (b Button) typ() string {
	if b.Type == "" {
		return "button"
	}
	return b.Type
}

// on is the click handler's attribute, none without one.
func (b Button) on() []ui.Attr {
	if b.On == nil {
		return nil
	}
	return []ui.Attr{{Name: "onClick", Value: ui.On(b.On)}}
}
</script>

<button
  id={b.id()}
  type={b.typ()}
  class={buttonVariants.Class("variant", string(b.Variant), "size", string(b.Size), "class", b.Class)}
  disabled={b.Disabled}
  {b.on()...}
  {b.Attrs...}
>{b.Children}</button>
```

```go [button_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// ButtonVariant is a Button's look.
type ButtonVariant string

const (
	ButtonDefault     ButtonVariant = "" // the primary colour
	ButtonDestructive ButtonVariant = "destructive"
	ButtonOutline     ButtonVariant = "outline"
	ButtonSecondary   ButtonVariant = "secondary"
	ButtonGhost       ButtonVariant = "ghost"
	ButtonLink        ButtonVariant = "link"
)

// ButtonSize is a Button's size.
type ButtonSize string

const (
	SizeDefault ButtonSize = ""
	SizeSm      ButtonSize = "sm"
	SizeLg      ButtonSize = "lg"
	SizeIcon    ButtonSize = "icon" // a square for an icon; give it an aria-label
)

// Button is a button:
//
//	<design.Button on={p.Save}>Save</design.Button>
//	<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
//	<design.Button on={ui.JS.Toggle(ui.Sel("#menu"))}>Menu</design.Button>
type Button struct {
	// ID is the button's id, e.g. for a JS command to target it.
	ID      string
	Variant ButtonVariant
	Size    ButtonSize
	// On is what a click does: a ui.JS chain, or a live handler.
	On any
	// Type is "button" (the default), "submit" or "reset".
	Type     string
	Disabled bool
	// Class is added to the button's classes, winning over them (ui.MergeClasses).
	Class string
	// Attrs are any other attributes: id, name, value, aria-*, data-*.
	Attrs    []ui.Attr
	Children ui.Node
}

const buttonBase = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all outline-none " +
	"disabled:pointer-events-none disabled:opacity-50 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 " +
	"aria-invalid:border-destructive aria-invalid:ring-destructive/20 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

var buttonVariants = ui.Variants{
	Base: buttonBase,
	Variants: map[string]map[string]string{
		"variant": {
			"default":     "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90",
			"destructive": "bg-destructive text-white shadow-xs hover:bg-destructive/90 focus-visible:ring-destructive/20",
			"outline":     "border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground",
			"secondary":   "bg-secondary text-secondary-foreground shadow-xs hover:bg-secondary/80",
			"ghost":       "hover:bg-accent hover:text-accent-foreground",
			"link":        "text-primary underline-offset-4 hover:underline",
		},
		"size": {
			"default": "h-9 px-4 py-2 has-[>svg]:px-3",
			"sm":      "h-8 gap-1.5 rounded-md px-3 has-[>svg]:px-2.5",
			"lg":      "h-10 rounded-md px-6 has-[>svg]:px-4",
			"icon":    "size-9",
		},
	},
	Defaults: map[string]string{"variant": "default", "size": "default"},
}

func (b Button) id() any {
	if b.ID == "" {
		return nil
	}
	return b.ID
}

func (b Button) typ() string {
	if b.Type == "" {
		return "button"
	}
	return b.Type
}

// on is the click handler's attribute, none without one.
func (b Button) on() []ui.Attr {
	if b.On == nil {
		return nil
	}
	return []ui.Attr{{Name: "onClick", Value: ui.On(b.On)}}
}

func (b Button) View() ui.Node {
	return ui.HTML(`<button id={b.id()} type={b.typ()} class={buttonVariants.Class("variant", string(b.Variant), "size", string(b.Size), "class", b.Class)} disabled={b.Disabled} {b.on()...} {b.Attrs...}>{b.Children}</button>`,
		b.id(),
		b.typ(),
		buttonVariants.Class("variant", string(b.Variant), "size", string(b.Size), "class", b.Class),
		b.Disabled,
		b.on(),
		b.Attrs,
		b.Children,
	)
}

func (b Button) Render(ctx context.Context, w io.Writer) error { return b.View().Render(ctx, w) }
```

:::

</div>
