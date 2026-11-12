---
title: Button
description: "A button, or something that looks like one: six variants, four sizes, and a click that runs in the browser or on the server."
pageClass: vk-ui-component vk-ui-showcase
outline: [2, 3]
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs/button. -->

# Button

<UiPageActions copy="/ui/pages/button.md" prev="/ui/components/badge" prev-title="Badge" next="/ui/components/card" next-title="Card" />

A button, or something that looks like one: six variants, four sizes, and a click that runs in the browser or on the server.

<UiExample src="/ui/previews/button/demo.html" hero>

```vuka-html:line-numbers
<design.Button variant={design.ButtonOutline}>Button</design.Button>
```

</UiExample>

## Installation

::: code-group

```sh [Command]
vuka ui add button
```

```vuka-html:line-numbers [Manual]
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
//	<design.Button onClick={open = !open}>Menu</design.Button>
type Button struct {
	// ID is the button's id, e.g. for a JS command to target it.
	ID      string
	Variant ButtonVariant
	Size    ButtonSize
	// On is what a click does: a ui.JS chain, or a live handler.
	On any
	// OnClick is what a click does in the browser, written as an on…
	// attribute is: onClick={open = true}, onClick={open = false; Save()}.
	// It wins over On.
	OnClick ui.JSChain
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
	h := b.On
	if b.OnClick != nil {
		h = b.OnClick
	}
	if h == nil {
		return nil
	}
	return []ui.Attr{{Name: "onClick", Value: ui.On(h)}}
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

:::

The command writes `components/ui/button.vuka.html` into the app as its own source, with what it uses: [cn](/ui/vendored#cn). To copy it by hand, save the file there and add what it uses; [UI Components (Vendored)](/ui/vendored) covers the design tokens they draw with.

## Usage

Import the package `vuka ui add` wrote the component into, then write the tag:

```vuka-html
<script lang="go">
package examples

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/app/components/ui"
)

type Toolbar struct{}
</script>

<design.Button variant={design.ButtonOutline}>Button</design.Button>
```

## Clicks

`onClick` runs in the browser: it takes statements writing
[browser state](/ui/state), calls among them, or a `ui.JS` chain, and needs
no round trip. `on` takes a live handler or a `ui.JS` chain. When both are
set, `onClick` wins.

```vuka-html
<script lang="go">
package examples

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/app/components/ui"
)

type Menu struct {
	open ui.State[bool]
}
</script>

<design.Button onClick={open = !open}>Menu</design.Button>
<ul show={open}>
  <li>Profile</li>
  <li>Sign out</li>
</ul>
```

## Size

`size` takes `SizeSm`, `SizeLg` or `SizeIcon`; leave it out for the default.

<UiExample src="/ui/previews/button/size.html">

```vuka-html:line-numbers
<div class="flex flex-col items-start gap-8 sm:flex-row">
  <div class="flex items-start gap-2">
    <design.Button size={design.SizeSm} variant={design.ButtonOutline}>Small</design.Button>
    <design.Button size={design.SizeIcon} variant={design.ButtonOutline} attrs={[]ui.Attr{{Name: "aria-label", Value: "Submit"}}}>
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7"/><path d="M8 7h9v9"/></svg>
    </design.Button>
  </div>
  <div class="flex items-start gap-2">
    <design.Button variant={design.ButtonOutline}>Default</design.Button>
  </div>
  <div class="flex items-start gap-2">
    <design.Button size={design.SizeLg} variant={design.ButtonOutline}>Large</design.Button>
  </div>
</div>
```

</UiExample>

## Default

The default variant fills the button with the primary colour.

<UiExample src="/ui/previews/button/default.html">

```vuka-html:line-numbers
<design.Button>Button</design.Button>
```

</UiExample>

## Outline

<UiExample src="/ui/previews/button/outline.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonOutline}>Outline</design.Button>
```

</UiExample>

## Secondary

<UiExample src="/ui/previews/button/secondary.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonSecondary}>Secondary</design.Button>
```

</UiExample>

## Ghost

<UiExample src="/ui/previews/button/ghost.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonGhost}>Ghost</design.Button>
```

</UiExample>

## Destructive

For an action that deletes or cannot be undone.

<UiExample src="/ui/previews/button/destructive.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonDestructive}>Destructive</design.Button>
```

</UiExample>

## Link

Drawn as a link, with an underline on hover.

<UiExample src="/ui/previews/button/link.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonLink}>Link</design.Button>
```

</UiExample>

## Icon

`SizeIcon` makes a square button for an icon alone. Give it an `aria-label`
through `attrs`, since it has no text.

<UiExample src="/ui/previews/button/icon.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonOutline} size={design.SizeIcon} attrs={[]ui.Attr{{Name: "aria-label", Value: "Next"}}}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
</design.Button>
```

</UiExample>

## With Icon

An `<svg>` among the children is sized and spaced for you: the button
leaves it at the size of the text and pulls in its padding on that side.

<UiExample src="/ui/previews/button/with_icon.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
  Download
</design.Button>
```

</UiExample>

## Rounded

`class` is merged last, so it wins over the component's own classes:
`rounded-full` makes a round button.

<UiExample src="/ui/previews/button/rounded.html">

```vuka-html:line-numbers
<design.Button variant={design.ButtonOutline} size={design.SizeIcon} class="rounded-full" attrs={[]ui.Attr{{Name: "aria-label", Value: "Scroll up"}}}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"/><path d="M12 19V5"/></svg>
</design.Button>
```

</UiExample>

## Spinner

Show work in progress with a spinning icon and `disabled`, so the button
cannot be pressed twice.

<UiExample src="/ui/previews/button/spinner.html">

```vuka-html:line-numbers
<div class="flex gap-2">
  <design.Button variant={design.ButtonOutline} disabled>
    <svg class="animate-spin" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke-dasharray="42 15"/></svg>
    Generating
  </design.Button>
  <design.Button variant={design.ButtonSecondary} disabled>
    Downloading
    <svg class="animate-spin" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke-dasharray="42 15"/></svg>
  </design.Button>
</div>
```

</UiExample>

## RTL

The button follows the text direction of the page: in a right-to-left
block its content reads from the right, and a directional icon is flipped
with `rotate-180`.

<UiExample src="/ui/previews/button/rtl.html">

```vuka-html:line-numbers
<div dir="rtl" class="flex gap-2">
  <design.Button variant={design.ButtonOutline}>إلغاء</design.Button>
  <design.Button>
    التالي
    <svg class="rotate-180" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
  </design.Button>
</div>
```

</UiExample>

## API Reference

### Button

A button, or something that looks like one: six variants, four sizes, and a click that runs in the browser or on the server.

| Prop | Type | Default |
|---|---|---|
| `id` | `string` | - |
| `variant` | `ButtonVariant` | `ButtonDefault` |
| `size` | `ButtonSize` | `SizeDefault` |
| `on` | `any` | - |
| `onClick` | `ui.JSChain` | - |
| `type` | `string` | `"button"` |
| `disabled` | `bool` | `false` |
| `class` | `string` | - |
| `attrs` | `[]ui.Attr` | - |
| `children` | `ui.Node` | - |

## Related

[Badge](/ui/components/badge) · [Card](/ui/components/card) · [Dialog](/ui/components/dialog) · [Dropdown Menu](/ui/components/dropdown-menu)
