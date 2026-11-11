# Button

A button, or something that looks like one: six variants, four sizes, and a click that runs in the browser or on the server.

```vuka-html
<design.Button variant={design.ButtonOutline}>Button</design.Button>
```

## Installation

```sh
vuka ui add button
```

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

```vuka-html
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

## Default

The default variant fills the button with the primary colour.

```vuka-html
<design.Button>Button</design.Button>
```

## Outline

```vuka-html
<design.Button variant={design.ButtonOutline}>Outline</design.Button>
```

## Secondary

```vuka-html
<design.Button variant={design.ButtonSecondary}>Secondary</design.Button>
```

## Ghost

```vuka-html
<design.Button variant={design.ButtonGhost}>Ghost</design.Button>
```

## Destructive

For an action that deletes or cannot be undone.

```vuka-html
<design.Button variant={design.ButtonDestructive}>Destructive</design.Button>
```

## Link

Drawn as a link, with an underline on hover.

```vuka-html
<design.Button variant={design.ButtonLink}>Link</design.Button>
```

## Icon

`SizeIcon` makes a square button for an icon alone. Give it an `aria-label`
through `attrs`, since it has no text.

```vuka-html
<design.Button variant={design.ButtonOutline} size={design.SizeIcon} attrs={[]ui.Attr{{Name: "aria-label", Value: "Next"}}}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
</design.Button>
```

## With Icon

An `<svg>` among the children is sized and spaced for you: the button
leaves it at the size of the text and pulls in its padding on that side.

```vuka-html
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>
  Download
</design.Button>
```

## Rounded

`class` is merged last, so it wins over the component's own classes:
`rounded-full` makes a round button.

```vuka-html
<design.Button variant={design.ButtonOutline} size={design.SizeIcon} class="rounded-full" attrs={[]ui.Attr{{Name: "aria-label", Value: "Scroll up"}}}>
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"/><path d="M12 19V5"/></svg>
</design.Button>
```

## Spinner

Show work in progress with a spinning icon and `disabled`, so the button
cannot be pressed twice.

```vuka-html
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

## RTL

The button follows the text direction of the page: in a right-to-left
block its content reads from the right, and a directional icon is flipped
with `rotate-180`.

```vuka-html
<div dir="rtl" class="flex gap-2">
  <design.Button variant={design.ButtonOutline}>إلغاء</design.Button>
  <design.Button>
    التالي
    <svg class="rotate-180" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
  </design.Button>
</div>
```

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

