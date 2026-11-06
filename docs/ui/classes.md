# Component Classes

[[toc]]

## Introduction

Components styled with Tailwind repeat long class strings: an alert, a badge
or a button carries the same dozen utilities wherever it is used, with a few
of them changed by its props. `ui.Variants` keeps those strings in one place,
keyed by the props that pick them, and `ui.MergeClasses` lets a caller's
`class` replace the component's own utilities instead of fighting them in the
stylesheet. Both run on the server, in Go: the browser gets one finished
`class` attribute.

## Variants {#variants}

A `ui.Variants` value is a component's base classes, the classes each value
of each variant adds, and the value a variant takes when the tag gives none.
In `components/alert.vuka.html`:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

var alert = ui.Variants{
    Base: "flex items-start gap-2 rounded-lg border px-4 py-3 text-sm",
    Variants: map[string]map[string]string{
        "tone": {
            "error": "border-red-200 bg-red-50 text-red-800",
            "info":  "border-border bg-gray-50",
        },
    },
    Defaults: map[string]string{"tone": "info"},
}

// Alert is a message box in one of its tones.
type Alert struct {
    Tone     string
    Class    string
    Children ui.Node
}
</script>

<div class={alert.Class("tone", Tone, "class", Class)} role="alert">{Children}</div>
```

`Class` takes names and values in pairs. A variant given `""`, or not named,
takes its default, else adds nothing; `"class"` names classes added last, the
caller's. The page needs no class string:

```html
<components.Alert tone="error">The card was declined.</components.Alert>
<components.Alert>Your order ships tomorrow.</components.Alert>
```

The classes are merged as [MergeClasses](#merging) merges them: a variant's
`bg-red-50` replaces nothing in `Base` here, but a variant setting `px-2`
would replace its `px-4`, and the caller's `class="px-6"` both.

### Mistakes {#mistakes}

A name that isn't a variant, or a value a variant doesn't have, fails the
render with [E0371](/errors/E0371) and the nearest name: `the variant tone has
no value "eror": it is "error" or "info"; did you mean "error"?`.
[vukavet](/tools/vukavet)'s `variants` check reports the same before the app
runs — in the `Defaults`, in a constant passed to `Class`, and in a tag's
attribute setting a prop the component passes to `Class`
(`<components.Alert tone="eror">`), across packages. The editor completes a
tag's `tone="…"` with the variant's values.

## Merging Classes {#merging}

`ui.MergeClasses(classes...)` joins class lists, a later one winning where two
Tailwind utilities set the same thing, as tailwind-merge does:

```go
var classes = ui.MergeClasses("px-4 py-2 text-sm text-gray-700", "px-2 text-red-800")

// classes is "py-2 text-sm px-2 text-red-800"
```

| Rule | Example | Result |
|---|---|---|
| A utility replaces one of its group | `px-4` + `px-2` | `px-2` |
| Groups apart stay | `text-sm text-gray-700` + `text-red-800` | `text-sm text-red-800` |
| A whole replaces its sides | `px-2 pt-1` + `p-4` | `p-4` |
| A side refines the whole | `p-4` + `px-2` | `p-4 px-2` |
| Only under the same variants | `bg-gray-50` + `hover:bg-red-50` | both |
| Variants in any order | `dark:hover:bg-gray-800` + `hover:dark:bg-black` | `hover:dark:bg-black` |
| Important apart | `!px-4` + `px-2` | both |
| Arbitrary values are their utility's | `w-4` + `w-[calc(100%-2rem)]` | `w-[calc(100%-2rem)]` |
| Other classes stay | `vk-alert card` + `px-2` | `vk-alert card px-2` |

The groups are the common ones: padding, margin, gap and space; width,
height and size; colours of text, background, border, ring, outline, shadow
and gradient stops; display, position, inset, z-index, overflow; flex, grid
and alignment; font size, weight and family, line height, letter spacing,
text alignment, transform and decoration; border width, style and radius,
corner by corner; opacity, shadows, transitions, animations, transforms and
cursors. A value is read the way Tailwind reads it: `text-sm` is a size,
`text-left` an alignment and `text-brand` (a theme colour) a colour;
`border-2` a width and `border-border` a colour.

A merge is cached by its input: a component's classes repeat from render to
render, so a merge after the first costs a map lookup.

## The Kit's Classes {#kit}

The kit's components take a `class` the same way: `<ui.Skeleton
class="h-32 rounded-full">` replaces the bone's `rounded-md`, and a caller's
utility wins over a kit component's own wherever both set one.
