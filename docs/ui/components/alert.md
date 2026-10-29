---
title: Alert
description: "A callout with an icon, a title and a message."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Alert

A callout with an icon, a title and a message.

<div class="vk-ui-shots">
  <img src="/ui/components/alert-light.png" alt="Alert in the light theme">
  <img src="/ui/components/alert-dark.png" alt="Alert in the dark theme">
</div>

## Adding It

```sh
vuka ui add alert
```

It writes `components/ui/alert.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Alert title="Heads up">The site is read-only tonight.</design.Alert>
<design.Alert variant={design.AlertDestructive} title="Payment failed">
  Your card was declined.
</design.Alert>
```

An optional `icon` (an `<svg>`) goes before the title. It renders
`role="alert"`.

## Props

| Prop | Type | |
|---|---|---|
| `variant` | `AlertVariant` | One of `AlertDefault`, `AlertDestructive`. |
| `title` | `string` |  |
| `<slot:icon>` | `ui.Node` | Icon is an optional icon before the title, such as an &lt;svg&gt;. |
| `class` | `string` | Class is added to the alert's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [alert.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// AlertVariant is an Alert's look.
type AlertVariant string

const (
	AlertDefault     AlertVariant = ""
	AlertDestructive AlertVariant = "destructive"
)

// Alert is a callout: an icon, a title and the message under it.
//
//	<design.Alert variant={design.AlertDestructive} title="Payment failed">
//	  Your card was declined.
//	</design.Alert>
type Alert struct {
	Variant AlertVariant
	Title   string
	// Icon is an optional icon before the title, such as an <svg>.
	Icon ui.Node
	// Class is added to the alert's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

const alertBase = "relative grid w-full grid-cols-[0_1fr] items-start gap-y-0.5 rounded-lg border px-4 py-3 text-sm " +
	"has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current"

var alertVariant = map[AlertVariant]string{
	AlertDefault:     "bg-card text-card-foreground",
	AlertDestructive: "bg-card text-destructive *:data-[slot=alert-description]:text-destructive/90",
}

// alertVariants are the classes of an alert of variant v; an unknown one is
// the default.
func alertVariants(v AlertVariant) string {
	variant, ok := alertVariant[v]
	if !ok {
		variant = alertVariant[AlertDefault]
	}
	return cn(alertBase, variant)
}
</script>

<div role="alert" class={cn(alertVariants(Variant), Class)} {Attrs...}>
  {Icon}
  if Title != "" {
    <div class="col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight">{Title}</div>
  }
  if ui.Has(Children) {
    <div
      data-slot="alert-description"
      class="col-start-2 grid justify-items-start gap-1 text-sm text-muted-foreground [&_p]:leading-relaxed"
    >{Children}</div>
  }
</div>
```

```go [alert_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// AlertVariant is an Alert's look.
type AlertVariant string

const (
	AlertDefault     AlertVariant = ""
	AlertDestructive AlertVariant = "destructive"
)

// Alert is a callout: an icon, a title and the message under it.
//
//	<design.Alert variant={design.AlertDestructive} title="Payment failed">
//	  Your card was declined.
//	</design.Alert>
type Alert struct {
	Variant AlertVariant
	Title   string
	// Icon is an optional icon before the title, such as an <svg>.
	Icon ui.Node
	// Class is added to the alert's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

const alertBase = "relative grid w-full grid-cols-[0_1fr] items-start gap-y-0.5 rounded-lg border px-4 py-3 text-sm " +
	"has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current"

var alertVariant = map[AlertVariant]string{
	AlertDefault:     "bg-card text-card-foreground",
	AlertDestructive: "bg-card text-destructive *:data-[slot=alert-description]:text-destructive/90",
}

// alertVariants are the classes of an alert of variant v; an unknown one is
// the default.
func alertVariants(v AlertVariant) string {
	variant, ok := alertVariant[v]
	if !ok {
		variant = alertVariant[AlertDefault]
	}
	return cn(alertBase, variant)
}

func (a Alert) View() ui.Node {
	var div ui.Node
	if a.Title != "" {
		div = ui.HTML(`<div class="col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight">{a.Title}</div>`, a.Title)
	}
	var div2 ui.Node
	if ui.Has(a.Children) {
		div2 = ui.HTML(`<div data-slot="alert-description" class="col-start-2 grid justify-items-start gap-1 text-sm text-muted-foreground [&amp;_p]:leading-relaxed">{a.Children}</div>`, a.Children)
	}
	return ui.HTML(`
<div role="alert" class={cn(alertVariants(a.Variant), a.Class)} {a.Attrs...}>
  {a.Icon}
  {div}
  {div2}
</div>`, cn(alertVariants(a.Variant), a.Class), a.Attrs, a.Icon, div, div2)
}

func (a Alert) Render(ctx context.Context, w io.Writer) error { return a.View().Render(ctx, w) }
```

:::

</div>
