---
title: Label
description: "A form field's label."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Label

A form field's label.

<div class="vk-ui-shots">
  <img src="/ui/components/label-light.png" alt="Label in the light theme">
  <img src="/ui/components/label-dark.png" alt="Label in the dark theme">
</div>

## Adding It

```sh
vuka ui add label
```

It writes `components/ui/label.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Label for="email">Email</design.Label>
<design.Input id="email" name="email" type="email" />
```

## Props

| Prop | Type | |
|---|---|---|
| `for` | `string` |  |
| `class` | `string` | Class is added to the label's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [label.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Label names a form field, For its id:
//
//	<design.Label for="email">Email</design.Label>
type Label struct {
	For string
	// Class is added to the label's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// target is the for attribute, none without one.
func (l Label) target() any {
	if l.For == "" {
		return nil
	}
	return l.For
}
</script>

<label
  for={l.target()}
  class={cn("flex items-center gap-2 text-sm leading-none font-medium select-none peer-disabled:cursor-not-allowed peer-disabled:opacity-50", l.Class)}
  {l.Attrs...}
>{l.Children}</label>
```

```go [label_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// Label names a form field, For its id:
//
//	<design.Label for="email">Email</design.Label>
type Label struct {
	For string
	// Class is added to the label's classes, winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// target is the for attribute, none without one.
func (l Label) target() any {
	if l.For == "" {
		return nil
	}
	return l.For
}

func (l Label) View() ui.Node {
	return ui.HTML(`<label for={l.target()} class={cn("flex items-center gap-2 text-sm leading-none font-medium select-none peer-disabled:cursor-not-allowed peer-disabled:opacity-50", l.Class)} {l.Attrs...}>{l.Children}</label>`, l.target(), cn("flex items-center gap-2 text-sm leading-none font-medium select-none peer-disabled:cursor-not-allowed peer-disabled:opacity-50", l.Class), l.Attrs, l.Children)
}

func (l Label) Render(ctx context.Context, w io.Writer) error { return l.View().Render(ctx, w) }
```

:::

</div>
