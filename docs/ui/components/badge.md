---
title: Badge
description: "A small label: a status or a count."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Badge

A small label: a status or a count.

<div class="vk-ui-shots">
  <img src="/ui/components/badge-light.png" alt="Badge in the light theme">
  <img src="/ui/components/badge-dark.png" alt="Badge in the dark theme">
</div>

## Adding It

```sh
vuka ui add badge
```

It writes `components/ui/badge.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Badge>New</design.Badge>
<design.Badge variant={design.BadgeSecondary}>Draft</design.Badge>
<design.Badge variant={design.BadgeDestructive}>Failed</design.Badge>
<design.Badge variant={design.BadgeOutline} class="rounded-full">{count}</design.Badge>
```

## Props

| Prop | Type | |
|---|---|---|
| `variant` | `BadgeVariant` | One of `BadgeDefault`, `BadgeSecondary`, `BadgeDestructive`, `BadgeOutline`. |
| `class` | `string` | Class is added to the badge's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [badge.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// BadgeVariant is a Badge's look.
type BadgeVariant string

const (
	BadgeDefault     BadgeVariant = "" // the primary colour
	BadgeSecondary   BadgeVariant = "secondary"
	BadgeDestructive BadgeVariant = "destructive"
	BadgeOutline     BadgeVariant = "outline"
)

// Badge is a small label, a status or a count:
//
//	<design.Badge variant={design.BadgeSecondary}>Draft</design.Badge>
type Badge struct {
	Variant BadgeVariant
	// Class is added to the badge's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

const badgeBase = "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-md border px-2 py-0.5 " +
	"text-xs font-medium transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 " +
	"[&>svg]:pointer-events-none [&>svg]:size-3"

var badgeVariant = map[BadgeVariant]string{
	BadgeDefault:     "border-transparent bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
	BadgeSecondary:   "border-transparent bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90",
	BadgeDestructive: "border-transparent bg-destructive text-white [a&]:hover:bg-destructive/90",
	BadgeOutline:     "text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
}

// badgeVariants are the classes of a badge of variant v; an unknown one is
// the default.
func badgeVariants(v BadgeVariant) string {
	variant, ok := badgeVariant[v]
	if !ok {
		variant = badgeVariant[BadgeDefault]
	}
	return cn(badgeBase, variant)
}
</script>

<span class={cn(badgeVariants(Variant), Class)} {Attrs...}>{Children}</span>
```

```go [badge_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// BadgeVariant is a Badge's look.
type BadgeVariant string

const (
	BadgeDefault     BadgeVariant = "" // the primary colour
	BadgeSecondary   BadgeVariant = "secondary"
	BadgeDestructive BadgeVariant = "destructive"
	BadgeOutline     BadgeVariant = "outline"
)

// Badge is a small label, a status or a count:
//
//	<design.Badge variant={design.BadgeSecondary}>Draft</design.Badge>
type Badge struct {
	Variant BadgeVariant
	// Class is added to the badge's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

const badgeBase = "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-md border px-2 py-0.5 " +
	"text-xs font-medium transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 " +
	"[&>svg]:pointer-events-none [&>svg]:size-3"

var badgeVariant = map[BadgeVariant]string{
	BadgeDefault:     "border-transparent bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
	BadgeSecondary:   "border-transparent bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90",
	BadgeDestructive: "border-transparent bg-destructive text-white [a&]:hover:bg-destructive/90",
	BadgeOutline:     "text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
}

// badgeVariants are the classes of a badge of variant v; an unknown one is
// the default.
func badgeVariants(v BadgeVariant) string {
	variant, ok := badgeVariant[v]
	if !ok {
		variant = badgeVariant[BadgeDefault]
	}
	return cn(badgeBase, variant)
}

func (b Badge) View() ui.Node {
	return ui.HTML(`<span class={cn(badgeVariants(b.Variant), b.Class)} {b.Attrs...}>{b.Children}</span>`, cn(badgeVariants(b.Variant), b.Class), b.Attrs, b.Children)
}

func (b Badge) Render(ctx context.Context, w io.Writer) error { return b.View().Render(ctx, w) }
```

:::

</div>
