---
title: Input
description: "A text field."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Input

A text field.

<div class="vk-ui-shots">
  <img src="/ui/components/input-light.png" alt="Input in the light theme">
  <img src="/ui/components/input-dark.png" alt="Input in the dark theme">
</div>

## Adding It

```sh
vuka ui add input
```

It writes `components/ui/input.vuka.html`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Label for="email">Email</design.Label>
<design.Input id="email" name="email" type="email" required invalid={err != ""} />
```

`invalid` sets `aria-invalid` and draws the field in the destructive
colour.

## Props

| Prop | Type | |
|---|---|---|
| `id` | `string` |  |
| `name` | `string` |  |
| `type` | `string` | "text" by default |
| `value` | `string` |  |
| `placeholder` | `string` |  |
| `autocomplete` | `string` |  |
| `required` | `bool` |  |
| `disabled` | `bool` |  |
| `invalid` | `bool` |  |
| `class` | `string` | Class is added to the input's classes, winning over them (see cn). |
| `attrs` | `[]ui.Attr` | Attrs are any other attributes: min, max, pattern, onInput, data-*. |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [input.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Input is a text field:
//
//	<design.Label for="email">Email</design.Label>
//	<design.Input id="email" name="email" type="email" placeholder="you@example.com"/>
//
// Invalid marks it (aria-invalid) and draws it in the destructive colour.
type Input struct {
	ID           string
	Name         string
	Type         string // "text" by default
	Value        string
	Placeholder  string
	Autocomplete string
	Required     bool
	Disabled     bool
	Invalid      bool
	// Class is added to the input's classes, winning over them (see cn).
	Class string
	// Attrs are any other attributes: min, max, pattern, onInput, data-*.
	Attrs []ui.Attr
}

const inputBase = "flex h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none " +
	"selection:bg-primary selection:text-primary-foreground placeholder:text-muted-foreground " +
	"file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground " +
	"disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm " +
	"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20"

func (in Input) typ() string {
	if in.Type == "" {
		return "text"
	}
	return in.Type
}

// inputAttr is s, or nil to leave its attribute out when s is empty.
func inputAttr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// invalid is aria-invalid's value, none unless the field is invalid.
func (in Input) invalid() any {
	if in.Invalid {
		return "true"
	}
	return nil
}
</script>

<input
  type={in.typ()}
  id={inputAttr(in.ID)}
  name={inputAttr(in.Name)}
  value={inputAttr(in.Value)}
  placeholder={inputAttr(in.Placeholder)}
  autocomplete={inputAttr(in.Autocomplete)}
  required={in.Required}
  disabled={in.Disabled}
  aria-invalid={in.invalid()}
  class={cn(inputBase, in.Class)}
  {in.Attrs...}
/>
```

```go [input_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// Input is a text field:
//
//	<design.Label for="email">Email</design.Label>
//	<design.Input id="email" name="email" type="email" placeholder="you@example.com"/>
//
// Invalid marks it (aria-invalid) and draws it in the destructive colour.
type Input struct {
	ID           string
	Name         string
	Type         string // "text" by default
	Value        string
	Placeholder  string
	Autocomplete string
	Required     bool
	Disabled     bool
	Invalid      bool
	// Class is added to the input's classes, winning over them (see cn).
	Class string
	// Attrs are any other attributes: min, max, pattern, onInput, data-*.
	Attrs []ui.Attr
}

const inputBase = "flex h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none " +
	"selection:bg-primary selection:text-primary-foreground placeholder:text-muted-foreground " +
	"file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground " +
	"disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm " +
	"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20"

func (in Input) typ() string {
	if in.Type == "" {
		return "text"
	}
	return in.Type
}

// inputAttr is s, or nil to leave its attribute out when s is empty.
func inputAttr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// invalid is aria-invalid's value, none unless the field is invalid.
func (in Input) invalid() any {
	if in.Invalid {
		return "true"
	}
	return nil
}

func (in Input) View() ui.Node {
	return ui.HTML(`<input type={in.typ()} id={inputAttr(in.ID)} name={inputAttr(in.Name)} value={inputAttr(in.Value)} placeholder={inputAttr(in.Placeholder)} autocomplete={inputAttr(in.Autocomplete)} required={in.Required} disabled={in.Disabled} aria-invalid={in.invalid()} class={cn(inputBase, in.Class)} {in.Attrs...}/>`,
		in.typ(),
		inputAttr(in.ID),
		inputAttr(in.Name),
		inputAttr(in.Value),
		inputAttr(in.Placeholder),
		inputAttr(in.Autocomplete),
		in.Required,
		in.Disabled,
		in.invalid(),
		cn(inputBase, in.Class),
		in.Attrs,
	)
}

func (in Input) Render(ctx context.Context, w io.Writer) error { return in.View().Render(ctx, w) }
```

:::

</div>
