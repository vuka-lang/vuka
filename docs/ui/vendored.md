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
  table, recording where the components go
  ([every key](/reference/vuka-toml#ui)):

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

`vuka ui diff` shows the same differences without adding anything
([Keeping Up](#diff)). `--dry-run` shows what would be written and the
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

### Themes {#themes}

The registry has themes, base palettes as shadcn's: `neutral` (the tokens'
own colours), `zinc`, `slate`, `stone`, `gray` and `blue`. `vuka ui theme`
lists them, `vuka ui init --theme zinc` writes the tokens with one, and
`vuka ui theme slate` switches an app to another later:

```text
$ vuka ui theme slate
kept the app's --primary (its value isn't the theme's it replaces)
wrote styles/app.css: the slate theme's colours
wrote vuka.toml: [ui] theme
```

A theme writes the colours, light and dark, into the block `vuka ui init`
wrote and nowhere else: the rest of the stylesheet, your own `:root` and
`@theme` included, isn't touched. A token whose value in the block isn't the
one the old theme gave it is yours, and keeps its value. `[ui] theme` in
`vuka.toml` records the theme, which is how the next switch knows which
values were the theme's. `--dry-run` says what it would write. A theme is a
stylesheet in the registry of a `:root` and a `.dark` block, so a registry
of your own can have its own.

### Undefined Tokens {#undefined-tokens}

A component naming a token no stylesheet defines renders unstyled, without
an error: a token deleted from the block, a typo in a class you edited, a
library's component using a colour its theme lacks. `vuka vet` warns of it
([W0601](/errors/W0601)), for every component of the components directory:
a `var(--name)`, a colour utility (`bg-background`, `ring-ring/50`, under
any variant) and Tailwind's `bg-(--name)`, against the custom properties the
build's stylesheets declare — the one holding the tokens, Tailwind's, and
the local files they import. It names the stylesheet that was expected to
define the token, and the nearest one defined:

```text
warning[W0601]: chip uses bg-rnig, which no stylesheet of the build defines: styles/app.css (the design tokens) was expected to define --color-rnig
  --> components/ui/chip.vuka.html:8:26
  = help: define --color-rnig in styles/app.css, or use a token it defines
  = help: did you mean bg-ring?
```

Tailwind's own colours (`text-red-500`, `bg-white`) and variables
(`--tw-…`, `--spacing`) are defined; so is a custom property the component
sets itself (`style="--progress: 40%"`).

The skeletons' tokens are there too: `--skeleton` (the `muted` token by
default), `--skeleton-highlight` (the wave's highlight) and
`--skeleton-duration`. A stylesheet `vuka ui init` wrote before the
skeleton existed lacks them: `vuka ui init -f` writes the block again.

The theme is light by default and dark when the visitor prefers it
(`prefers-color-scheme`); `data-theme` on the `<html>` element overrides
that either way, so a theme switch is one attribute:

```html
<button onClick={ui.JS.SetAttr(ui.Sel("html"), "data-theme", "dark")}>Dark</button>
```

## Behaviour: Three Tiers {#tiers}

How a component behaves decides where its code runs:

| Tier | Where it runs | Components |
|---|---|---|
| 1. The browser | HTML and [JS commands](/ui/js), no round trip, no framework script | Button, Badge, Card, Alert, Input, Label, Dialog, Skeleton |
| 2. The server | state on the server over the [live connection](/ui/live) | Tabs |
| 3. An island | a client component through the [islands](/web/islands) adapters | later: a combobox, a date picker |

A tier-1 component works on any page, live or not. A tier-2 component keeps
its state on a live page; rendered on a page that isn't live, it shows its
first state.

## The Components {#components}

Each component has a page with its props, examples, screenshots in both
themes, and its source beside the Go it ejects to:

| Component | Runs | |
|---|---|---|
| [Alert](/ui/components/alert) | in the browser | a callout with an icon, a title and a message |
| [Badge](/ui/components/badge) | in the browser | a small label: a status or a count |
| [Button](/ui/components/button) | in the browser | six variants, four sizes, a `ui.JS` chain or a live handler on click |
| [Card](/ui/components/card) | in the browser | a panel with a title, a description, an action, its content and a footer |
| [Dialog](/ui/components/dialog) | in the browser | a `<dialog>` over the page, opened and closed by JS commands |
| [Input](/ui/components/input) | in the browser | a text field |
| [Label](/ui/components/label) | in the browser | a form field's label |
| [Skeleton](/ui/components/skeleton) | in the browser | placeholder bones while content loads, or a component drawn as bones |
| [Tabs](/ui/components/tabs) | on the server | panels shown one at a time, the tab shown kept on the server |

## The Registry {#registry}

`vuka ui add` reads the components from a registry: an `index.json` naming
each component's files (with their sha256), the components it uses and
the versions of `ui` and `web` it needs, beside the files themselves.

By default it is Vuka's own, fetched from its repository at the version of
your `vuka` command, so the components match the libraries your app builds
against. Every file is checked against the digest in the index. Without a
network, `--offline` uses the copy built into the command; a fetch that
fails falls back to it too, and says so:

```text
couldn't fetch the registry (GET https://raw.githubusercontent.com/vuka-lang/vuka/v2.11.0/cmd/vuka/registry/index.json: 404 Not Found): used the copy built into vuka
wrote components/ui/badge.vuka.html
```

A component that needs a newer library than your `go.mod` requires writes
nothing and says what to run:

```text
vuka ui add: tabs needs github.com/vuka-lang/ui/v2 v2.12.0 or newer; the app requires v2.10.0. Upgrade first, then add again:

	go get github.com/vuka-lang/ui/v2@v2.12.0
```

A registry of your own, a company's design system, say, has the same
format: serve the directory over HTTPS or keep it in the repository, and
name it with `--registry`, or once for the app in `vuka.toml`:

```toml
[ui]
registry = "https://design.example.com/registry"   # or a directory, relative to the module
```

## What a Component Takes {#component-doc}

`vuka ui list` with a component's name prints what it takes, read from its
source: its attributes and their types, the values a variant takes, each
attribute's doc, its slots and its events. A library's component is
`library/name`, read at the version the app has it at:

```text
$ vuka ui list button
Button: A button: six variants, four sizes, a ui.JS chain or a live handler on click.

<design.Button>  component, button.vuka.html:33
  id        string
  variant   ButtonVariant  design.ButtonDefault (""), design.ButtonDestructive ("destructive"), design.ButtonOutline ("outline"), …
  size      ButtonSize     design.SizeDefault (""), design.SizeSm ("sm"), design.SizeLg ("lg"), design.SizeIcon ("icon")
  on        any            event
  type      string
  disabled  bool
  class     string
  attrs     []ui.Attr
  on: On is what a click does: a ui.JS chain, or a live handler.
  …
  slots: children
  events: on
```

A variant's values are the constants of its type, or the values of the
`ui.Variants` variant named like the attribute. The editor's hover on the
component's tag shows the same ([Writing Component Tags](/tools/editors#component-tags)).

## Keeping Up {#diff}

The components are yours, so nothing updates them. `vuka ui diff` shows
where yours and the registry's differ, for every component the app has or
the ones named:

```text
--- components/ui/button.vuka.html
+++ components/ui/button.vuka.html (vuka ui add)
@@ -48,7 +48,7 @@
-const buttonBase = "inline-flex … rounded-full text-sm font-semibold …"
+const buttonBase = "inline-flex … rounded-md text-sm font-medium …"

vuka ui add -f button takes the registry's; cn, dialog: the same as the registry's
```

`vuka ui add -f button` takes the registry's copy; or bring the lines you
want over by hand.

## From a templ Component Library {#libraries}

`vuka ui add` also writes components of a templ component library —
shadcn-templ and libraries like it, a Go package of `.templ` files per
component — into the app, as its own source. A library's component is
named `library/name`:

```sh
vuka ui add shadcn-templ/dialog shadcn-templ/select shadcn-templ/button
```

```text
wrote components/ui/dialog/dialog.templ
wrote components/ui/dialog/dialog.js
wrote components/ui/button/button.templ
wrote components/ui/icon/icon.go
wrote components/ui/utils/shadcn-templ.go
wrote components/ui/select/select.js
wrote components/ui/select/select.templ
wrote components/ui/baseui/portal.js
…
wrote styles/tw-animate.css
wrote styles/shadcn-tailwind.css
wrote styles/shadcn-templ.css: shadcn-templ's theme and styles
wrote styles/app.css: imports shadcn-templ.css
wrote pages/_layout.vuka.html: the document's colours from the tokens (bg-background, text-foreground)
wrote vuka.toml: [ui.sources]
go get github.com/Oudwins/tailwind-merge-go@v0.2.2

Use a library's components as tags, importing each package:

	import "example.com/shop/components/ui/dialog"
	import "example.com/shop/components/ui/select"
	import "example.com/shop/components/ui/button"
```

One command does all of it, in an app `vuka new` made (an app without
`[ui]` gets `vuka ui init` first):

- **The packages.** Each component is its own package under
  `components/ui` (`components/ui/dialog`), with the packages of the library
  it imports (the dialog's close button is a `button.Button`, its icon an
  `icon`, its classes joined by `utils`), their imports changed to the
  app's. vuka compiles the `.templ` files itself, so the library's generated
  `_templ.go` files, its tests and its minified scripts are left out.
- **Go's requirements.** The modules the components import — templ,
  tailwind-merge-go — become direct requirements of the app's `go.mod`
  (`go get`), templ at the version vuka builds with.
- **The scripts.** A component's script (`dialog.js`) and the scripts the
  library's components share (`baseui`, `floatingui`) are bundled into one
  module by `vuka build` and `vuka run -watch`, and loaded by the pages that
  show one of its components with a script: no script tag, no static
  mount, nothing generated on disk for `//go:embed` to need. A page with
  only a button loads none.
- **The theme.** `styles/shadcn-templ.css`, imported by the app's
  stylesheet, holds the library's theme tokens the app's
  ([design tokens](#theming)) don't define — sidebar and chart colours, the
  larger radii — and the styles of the classes the app's components of it
  use. Dark follows the visitor's preference, or `data-theme`, as the
  tokens do: the library's `.dark` class variant isn't taken. `vuka ui add`
  writes the file again as components are added; change the tokens in
  `styles/app.css`. The document of a layout `vuka new` wrote is drawn
  with the tokens' `bg-background` and `text-foreground`, so the page and
  the library's components follow one theme.
- **Where each came from.** `vuka.toml` records it:

```toml
[ui.sources]
baseui = "shadcn-templ@v2.0.0-beta.16"
button = "shadcn-templ@v2.0.0-beta.16"
dialog = "shadcn-templ@v2.0.0-beta.16"
…
```

The components are then used as their library documents them, on static
and live pages alike ([templ Component Libraries](/ui/templ#component-libraries)).
`vuka ui diff` compares each with its library at the version it came from
(`vuka ui diff shadcn-templ/dialog` for one); `vuka ui add -f
shadcn-templ/dialog` takes the library's again. `vuka ui list` lists the
app's sources with vuka's components, and `vuka ui list shadcn-templ` the
library's components.

### Upgrading a Library {#upgrade}

When vuka knows a newer version of a library (or `[ui.libraries.<name>]`
names one), `vuka ui upgrade` takes the app's components of it there
without losing your edits (`vuka ui upgrade shadcn-templ/dialog` for one,
every directory of `[ui.sources]` when none is named). Each file is merged
three ways: the library's at the version `[ui.sources]` records, the
library's at the new version, and yours. A change only the library made is
taken, a change only you made stays, and the same change on both sides is
made once:

```text
wrote components/ui/button/button.templ: shadcn-templ@v2.0.0, merged with the app's changes
wrote components/ui/button/group.templ: new in shadcn-templ@v2.0.0
wrote components/ui/dialog/dialog.templ: 1 conflict to resolve
wrote vuka.toml: [ui.sources]
vuka ui upgrade: shadcn-templ/dialog has conflicts (marked <<<<<<< ======= >>>>>>> in the files above): resolve them, then run vuka ui upgrade again, which records the new version
```

Where you and the library changed the same lines differently, the file
gets both, between markers, as git writes them:

```text
<<<<<<< yours
	ID    string
	Title string
=======
	ID    string
	Class string
>>>>>>> shadcn-templ@v2.0.0
```

A component with a conflict keeps its old version in `[ui.sources]`, and
`[ui.upgrading]` notes the version it is being taken to. Edit the markers
away, then run `vuka ui upgrade` again: it keeps your resolution and
records the new version. Packages the components need at the new version
and the app hasn't are written, the library's theme stylesheet is written
again, and the modules the components import are got as with `vuka ui add`.

`vuka ui upgrade --check` changes nothing: it lists each component with its
versions, the files changed upstream and the files you edited, and fails
when there are changes to take, for CI:

```text
shadcn-templ/button: v2.0.0-beta.16 → v2.0.0, changed upstream: button.templ; edited in the app: button.templ
shadcn-templ/utils: v2.0.0-beta.16 → v2.0.0, no changes upstream
```

A library read from a directory (`dir` in `[ui.libraries.<name>]`) has no
version to merge from: `vuka ui diff` shows how yours differ. vuka's own
components aren't versioned in `[ui.sources]`; `vuka ui diff` and `vuka ui
add -f` keep them up to date.

### Icons {#icons}

A library's components draw its own icons (shadcn-templ's are lucide's,
from its `icon` package). To draw yours instead, name a set of the app's in
`[ui] icons` and map its icons by role:

```toml
[ui]
icons = "app"

[ui.icons.app]
package = "components/icons"   # the app's icon package
close = "Close"
chevron-down = "ChevronDown"
```

Each icon of the library has a role: its name in the library (`check`,
`chevron-down`, `search`), or the role vuka gives it — `x` is `close`,
`loader-circle` `spinner`, `triangle-alert` `warning`, `octagon-x`
`error`, `circle-check` `success`, `panel-left` `sidebar`, `ellipsis`
`more`. `vuka ui add` writes the components calling the set's function for
each role it maps — `@icon.X(icon.Props{Class: "size-4"})` becomes
`@icons.Close(icon.Props{Class: "size-4"})` — and imports the set's
package, dropping the library's icon import where nothing else uses it. The
set's functions take the library's icon props:

```go
package icons

func Close(props ...icon.Props) templ.Component { … }
```

A role the set doesn't map keeps the library's icon, and `vuka ui add`
says so (`the icon set app has no chevron-down icon: kept shadcn-templ's
(icon.ChevronDown)`). `vuka ui diff` and `vuka ui upgrade` compare with the
library's files as `vuka ui add` writes them, so the icons are never a
difference of yours, nor a conflict on an upgrade. Without `[ui] icons` (or
with the library's own set, `lucide` for shadcn-templ) the components keep
the library's icons.

### Other Libraries {#other-libraries}

vuka reads a library from its Go module, at a version, the way `go get`
finds it: where its components, shared scripts and theme are in the module
is vuka's own description of the library, not a registry format of the
library's. vuka knows shadcn-templ; `vuka.toml` describes another, or
changes the version of one it knows:

```toml
[ui.libraries.acme]
module = "github.com/acme/templ-ui"
version = "v1.4.0"
components = "components"              # a package per component (the default)
scripts = ["shared"]                   # script directories its components share, bundled first
theme = "assets/css/theme.css"         # its :root, .dark and @theme inline tokens
css = ["assets/css/animations.css"]    # stylesheets its classes need, copied beside the app's
style = "assets/css/style.css"         # rules for its components' own classes
```

`dir = "../acme-ui"` in place of `module` and `version` reads a library
being written beside the app.

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
