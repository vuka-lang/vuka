# UI Components (Vendored)

[[toc]]

## Introduction

`vuka ui add button card dialog` writes a button, a card and a dialog into
your app as `.vuka.html` files under `components/ui/`. From then on they are
your source: change a class, add a variant, rename a prop. Nothing in the
app depends on the command afterwards, and nothing updates the files behind
your back.

They are drawn with Tailwind utilities over a small set of design tokens
(`--primary`, `--border`, `--radius`, …) that `vuka ui init` writes into the
app's stylesheet. Change a token and every component follows; paste another
theme and the whole app takes it.

The [ui kit](/ui/components#kit) (`<ui.Button>`, `<ui.Modal>`, …) stays
what it is: a dependency with its own stylesheet, upgraded with the rest of
Vuka. Reach for these vendored components when the app's look is yours to
own — a design system of your own, a brand, classes you want to read and
edit in place.

## Starting {#init}

In an app made by `vuka new`:

```sh
vuka ui init
```

```text
wrote styles/app.css: the design tokens
wrote components/ui/cn.go
wrote components/ui/cn_test.go
wrote vuka.toml: [ui]
```

It writes three things:

- **The tokens**, into the stylesheet Vuka builds with Tailwind (`app.css`
  or `styles/app.css`, else the one `web.Tailwind` names; `--css` another):
  a Tailwind `@theme` mapping each token to a utility (`bg-primary`,
  `text-muted-foreground`, `rounded-lg`), their light values on `:root`, and
  their dark values. The block goes after the stylesheet's imports, between
  two `vuka ui` comments; the rest of the file stays as it was.
- **`components/ui/cn.go`**, the class helper every component uses, and its
  tests.
- **A `[ui]` table in `vuka.toml`**, next to `go.mod` and its `[fmt]`
  table, recording where the components go:

```toml
[ui]
style = "default"
components = "components/ui"
package = "design"
css = "styles/app.css"
```

The components' package is `design` (`--package` names another; `--dir`
puts them elsewhere), so a page imports it beside Vuka's `ui` without a
clash: `<design.Button>` next to `<ui.Link>`.

Running `vuka ui init` again changes nothing. Tokens already in the
stylesheet are left as they are; `-f` writes them again in place.
`--dry-run` says what it would write.

## Adding Components {#add}

```sh
vuka ui add dialog
```

```text
wrote components/ui/button.vuka.html
wrote components/ui/dialog.vuka.html

Use them as tags (import design "example.com/shop/components/ui"):

	<design.Button on={design.OpenDialog("confirm")}>Open</design.Button> <design.Dialog id="confirm" title="Sure?">…</design.Dialog>
```

A component brings what it uses: the dialog's close button is a `Button`,
so it comes too. `vuka ui list` lists the components; a name it doesn't know
is answered with the list and the nearest name.

A file the app has already is never overwritten without being asked:

- the same as the registry's: nothing to do;
- one you named that differs: `vuka ui add` prints the difference, as a
  unified diff from yours to the registry's, writes nothing and fails; `-f`
  overwrites it;
- one a component uses that differs (your edited `button.vuka.html` when you
  add the dialog): yours is kept, and said so.

So `vuka ui add button` is also how to see where your button has moved
away from the registry's. `--dry-run` shows what would be written and the
differences, writing nothing.

## Using Them {#using}

Import the package by its path; its name is `design`:

```html
<script lang="go">
package settings

import (
	"github.com/vuka-lang/ui/v2"

	design "example.com/shop/components/ui"
)

// Settings is the settings page.
type Settings struct {
	ui.Live
	saved bool
}

// Save saves.
func (s *Settings) Save() { s.saved = true }
</script>

<design.Card title="Profile" description="What others see.">
  <div class="grid gap-2">
    <design.Label for="name">Name</design.Label>
    <design.Input id="name" name="name" placeholder="Ada Lovelace" />
  </div>
  <slot:footer>
    <design.Button on={Save}>Save</design.Button>
    if saved {
      <design.Badge variant={design.BadgeSecondary} class="ml-2">Saved</design.Badge>
    }
  </slot:footer>
</design.Card>
```

Every component takes `class`, added after its own classes and winning
over them: `class="px-0"` on a button replaces its padding instead of
fighting it. `attrs` passes any other attribute (`aria-*`, `data-*`, `name`).

## Theming {#theming}

The tokens are CSS variables; the components only name them through
Tailwind utilities (`bg-primary`, `border-input`, `ring-ring/50`). To change
the look, change the values in the block `vuka ui init` wrote:

```css
:root {
  --radius: 0.375rem;
  --primary: oklch(0.55 0.2 260);
  --primary-foreground: oklch(0.98 0 0);
}
```

The token names are shadcn's, so a theme made for it pastes in: its `:root`
values over these, and its `.dark` values into both dark blocks.

The theme is light by default and dark when the visitor prefers it
(`prefers-color-scheme`); `data-theme` on the `<html>` element overrides
that either way, so a theme switch is one attribute:

```html
<button onClick={ui.JS.SetAttr(ui.Sel("html"), "data-theme", "dark")}>Dark</button>
```

## Behaviour: Three Tiers {#tiers}

How a component behaves decides where its code runs:

| Tier | Where it runs | M1 components |
|---|---|---|
| 1. The browser | HTML and [JS commands](/ui/js), no round trip, no framework script | Button, Badge, Card, Alert, Input, Label, Dialog |
| 2. The server | state on the server over the [live connection](/ui/live) | Tabs |
| 3. An island | a client component through the [islands](/web/islands) adapters | later: a combobox, a date picker |

A tier-1 component works on any page, live or not. A tier-2 component keeps
its state on a live page; rendered on a page that isn't live, it shows its
first state.

## The Components {#components}

### Button

```html
<design.Button on={Save}>Save</design.Button>
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
<design.Button variant={design.ButtonGhost} size={design.SizeIcon} attrs={closeLabel}><svg>…</svg></design.Button>
<design.Button type="submit" disabled={busy}>Send</design.Button>
```

| Prop | |
|---|---|
| `Variant` | `ButtonDefault`, `ButtonDestructive`, `ButtonOutline`, `ButtonSecondary`, `ButtonGhost`, `ButtonLink` (or the string: `variant="outline"`) |
| `Size` | `SizeDefault`, `SizeSm`, `SizeLg`, `SizeIcon` |
| `On` | what a click does: a live handler, or a `ui.JS` chain |
| `Type` | `"button"` (the default), `"submit"`, `"reset"` |
| `Disabled`, `ID`, `Class`, `Attrs`, `Children` | |

The classes live in two Go maps in the file, `buttonVariant` and
`buttonSize`, joined by `buttonVariants(v, s)`: a new variant is a constant
and a map entry.

### Badge

```html
<design.Badge>New</design.Badge>
<design.Badge variant={design.BadgeDestructive}>Failed</design.Badge>
```

`Variant` is `BadgeDefault`, `BadgeSecondary`, `BadgeDestructive` or
`BadgeOutline`; `Class`, `Attrs`, `Children`.

### Card

```html
<design.Card title="Team" description="Who can see this project.">
  <slot:action><design.Button size={design.SizeSm}>Invite</design.Button></slot:action>
  <p>Three members.</p>
  <slot:footer>Updated today</slot:footer>
</design.Card>
```

`Title`, `Description`, the `Action` slot beside them, the content, and
the `Footer` slot; each part is left out when empty. `Class`, `Attrs`.

### Alert

```html
<design.Alert title="Heads up">The site is read-only tonight.</design.Alert>
<design.Alert variant={design.AlertDestructive} title="Payment failed">
  Your card was declined.
</design.Alert>
```

`Variant` is `AlertDefault` or `AlertDestructive`; `Title`, an optional
`Icon` (an `<svg>`), the message as its content. It renders `role="alert"`.

### Input and Label

```html
<design.Label for="email">Email</design.Label>
<design.Input id="email" name="email" type="email" required invalid={err != ""} />
```

Input takes `ID`, `Name`, `Type` (`"text"` by default), `Value`,
`Placeholder`, `Autocomplete`, `Required`, `Disabled`, and `Invalid`, which
sets `aria-invalid` and draws the field in the destructive colour. Label
takes `For` and its content. Both take `Class` and `Attrs`.

### Tabs

```html
<design.Tabs id="plan" default="monthly">
  <slot:tab value="monthly" label="Monthly"><p>Billed every month.</p></slot:tab>
  <slot:tab value="yearly" label="Yearly"><p>Billed once a year.</p></slot:tab>
  <slot:tab value="lifetime" label="Lifetime" disabled><p>Not offered.</p></slot:tab>
</design.Tabs>
```

A [stateful component](/ui/stateful): the tab shown is its own state on the
server, so a click switches it over the live connection and the rest of the
page keeps its state. `ID` prefixes its elements' ids (one per Tabs on a
page); `Default` is the tab shown first. Each `<slot:tab>` is a `Tab` —
`Value`, `Label`, `Disabled` and its panel, the slot's content (the type is
in `tabs.go`, beside the component). The markup is `role="tablist"`,
`role="tab"` buttons with `aria-selected` and `aria-controls`, and the
`role="tabpanel"` labelled by its tab.

### Dialog

```html
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

A live page's handlers drive it with the same commands, through
`PushJS`, and `OnClose` tells the page when the visitor closed it:

```html
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

`ID` is required and unique on the page; `Title`, `Description`, the
content, a `Footer` slot, `Open` (rendered open as the page loads),
`OnClose`, and `Class` for its panel.

## cn: Merging Classes {#cn}

`cn(...)` in `components/ui/cn.go` joins class lists, a later class winning
over an earlier one that sets the same thing, so a component's defaults
give way to its `Class`:

```text
cn("px-4 py-2 bg-primary", "px-2 bg-red-600")   →  "py-2 px-2 bg-red-600"
cn("p-4", "px-2")                                 →  "p-4 px-2"
cn("hover:bg-accent bg-primary", "bg-muted")     →  "hover:bg-accent bg-muted"
```

It knows a deliberately small part of Tailwind, listed in its doc comment:
padding and margin per side (`p-4` drops an earlier `px-2`), background
colour, text colour apart from text size, rounded corners, font weight and
family, display, width, height and `size-*`, and gap. Classes conflict only
under the same variants and importance; a class it doesn't know is kept as
written. It is plain Go with tests beside it (`cn_test.go`): when your app
needs another group, add it there.
