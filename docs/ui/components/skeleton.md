---
title: Skeleton
description: "Placeholder bones while content loads: shapes, lines of text, a wrapper around the real content, and a component drawn as bones."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Skeleton

Placeholder bones while content loads: shapes, lines of text, a wrapper around the real content, and a component drawn as bones.

<div class="vk-ui-shots">
  <img src="/ui/components/skeleton-light.png" alt="Skeleton in the light theme">
  <img src="/ui/components/skeleton-dark.png" alt="Skeleton in the dark theme">
</div>

## Adding It

```sh
vuka ui add skeleton
```

It writes `components/ui/skeleton.vuka.html` and `components/ui/skeleton_of.vuka.html` and `components/ui/skeleton.go`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<div class="flex items-center gap-4">
  <design.Skeleton variant={design.SkeletonCircular} class="size-12"/>
  <div class="grid flex-1 gap-2">
    <design.Skeleton variant={design.SkeletonRounded} class="w-2/3"/>
    <design.Skeleton lines={2}/>
  </div>
</div>
```

A bone is a muted block in the theme's `--skeleton` colour. `Rectangular`
(the default) and `Rounded` are `h-4` across their row, `Circular` is
`size-10`, and `Text` is `1em` tall at the line's height, so a bone in a
`text-sm` paragraph is the paragraph's line. `Class` sizes it, winning over
the defaults through `cn`. `lines={n}` stacks text lines, the last one
shorter. Bones pulse; `animation={design.SkeletonWave}` sweeps a highlight
across them instead (positioned on the viewport, so every wave on screen
is one), `design.SkeletonNone` keeps them still, and under
`prefers-reduced-motion` nothing moves. A bone is `aria-hidden`.

### Wrapping the Real Content

With children, a skeleton wraps the content it stands for. While `loading`
is true the children are laid out but hidden, under a bone of their exact
size; when it turns false they show in the same box, so nothing moves:

```vuka-html
<design.Skeleton loading={p.Saving} variant={design.SkeletonRounded}>
  <design.Button on={p.Publish}>Publish</design.Button>
</design.Skeleton>
```

The wrapper is `w-fit`, the children's width (`class="w-full"` for a block
that fills its row). While loading it is a `role="status"`, `aria-busy`
region with a screen-reader label (`label=`, "Loading…" by default), and the
hidden children are `inert`.

### Drawing a Component as Bones

`design.SkeletonOf` draws its children — the real component, given
placeholder data — as bones, so the placeholder is the component's own
layout and can't drift from it:

```vuka-html
<design.SkeletonOf repeat={3} class="grid gap-4">
  <PostCard post={Post{Title: design.Words(5), Author: design.Chars(12), Excerpt: design.Words(24)}}/>
</design.SkeletonOf>
```

The stylesheet `vuka ui init` writes does the drawing: text becomes bars as
long as the text, line by line; images, icons, videos, canvases, buttons and
inputs become blocks of their size; borders and spacing stay. Every box is
the real component's. `design.Words(n)` and `design.Chars(n)` make
placeholder text of a length that reads right. The copy is `inert` and
`aria-hidden`; the wrapper is a busy status with `label=` for screen
readers. Ids in the component are repeated in the copy: give it data
without them, or leave the real component out of the page meanwhile.

An element of the real markup can say how it is drawn, with no effect
outside a skeleton:

| `data-skeleton` | Drawn |
|---|---|
| `keep` | as it is: a fixed label, an icon that means something |
| `ignore` | not at all, its space kept |
| `replace` | as one bone of its box: a coloured badge, a chart |
| `unite` | as one bone of its content box, its border and padding kept: a tag, an avatar group |

### Fallbacks of Parts

A [deferred part](/web/parts) or a [`<ui.Suspense>`](/web/parts#suspense)
shows its fallback until its data arrives; a skeleton is the usual one.
`<ui.Skeleton>` from ui needs no setup; this component draws with the
app's tokens:

```vuka-html
<ul>
  for _, p := range RecentPosts() {
    <li>{p.Title}</li>
  }
</ul>
<ui.Fallback for={RecentPosts}><design.Skeleton lines={3}/></ui.Fallback>

<ui.Suspense>
  <slot:fallback><design.SkeletonOf repeat={3}><PostRow post={placeholderPost}/></design.SkeletonOf></slot:fallback>
  for _, p := range Posts() {
    <PostRow post={p}/>
  }
</ui.Suspense>
```

<div class="vk-ui-shots">
  <img src="/ui/components/skeleton-parts-light.png" alt="A page's fallbacks while its parts load, in the light theme">
  <img src="/ui/components/skeleton-parts-dark.png" alt="A page's fallbacks while its parts load, in the dark theme">
</div>

### Recipes

Skeletons are composed rather than chosen from presets. A card:

```vuka-html
<design.Card>
  <div class="grid gap-4">
    <div class="flex items-center gap-3">
      <design.Skeleton variant={design.SkeletonCircular}/>
      <div class="grid flex-1">
        <design.Skeleton variant={design.SkeletonText} class="w-1/2"/>
        <design.Skeleton variant={design.SkeletonText} class="w-1/3 text-sm"/>
      </div>
    </div>
    <design.Skeleton variant={design.SkeletonRounded} class="h-32"/>
    <design.Skeleton lines={2} class="text-sm"/>
  </div>
</design.Card>
```

A list row:

```vuka-html
<li class="flex items-center gap-3 py-3">
  <design.Skeleton variant={design.SkeletonCircular} class="size-8"/>
  <design.Skeleton variant={design.SkeletonText} class="w-40"/>
  <design.Skeleton variant={design.SkeletonRounded} class="ml-auto h-6 w-16"/>
</li>
```

A table, five rows of the real columns:

```vuka-html
<tbody>
  for range 5 {
    <tr>
      <td><design.Skeleton variant={design.SkeletonText} class="w-32"/></td>
      <td><design.Skeleton variant={design.SkeletonText} class="w-48"/></td>
      <td class="text-right"><design.Skeleton variant={design.SkeletonText} class="ml-auto w-12"/></td>
    </tr>
  }
</tbody>
```

A profile:

```vuka-html
<div class="flex flex-col items-center gap-3">
  <design.Skeleton variant={design.SkeletonCircular} class="size-20"/>
  <design.Skeleton variant={design.SkeletonText} class="w-40 text-lg"/>
  <design.Skeleton variant={design.SkeletonText} class="w-56 text-sm"/>
  <design.Skeleton variant={design.SkeletonRounded} class="h-9 w-28"/>
</div>
```

Or, where the real component exists, `SkeletonOf` it with placeholder data:
the recipe is the component.

## Props

| Prop | Type | |
|---|---|---|
| `variant` | `SkeletonVariant` | One of `SkeletonRectangular`, `SkeletonRounded`, `SkeletonCircular`, `SkeletonText`. |
| `lines` | `int` | Lines stacks that many lines of text, the last one shorter. |
| `animation` | `SkeletonAnimation` | One of `SkeletonPulse`, `SkeletonWave`, `SkeletonNone`. |
| `loading` | `bool` | Loading hides the children under the bone; without children it is ignored. |
| `label` | `string` | Label is what a screen reader hears while Loading; "Loading…" by default. |
| `class` | `string` | Class is added to the bone's classes (with children or Lines, the wrapper's), winning over them (see cn). |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

### SkeletonOf

SkeletonOf draws its children — a real component given placeholder data — as bones, so the placeholder is the component's own layout:

| Prop | Type | |
|---|---|---|
| `repeat` | `int` | Repeat draws the children that many times; once by default. |
| `animation` | `SkeletonAnimation` | One of `SkeletonPulse`, `SkeletonWave`, `SkeletonNone`. |
| `label` | `string` | Label is what a screen reader hears; "Loading…" by default. |
| `class` | `string` | Class is added to the wrapper's classes, winning over them (see cn): a grid or a gap for Repeat. |
| `attrs` | `[]ui.Attr` |  |
| the content | `ui.Node` |  |

### Functions

- `Words(n int) string`: Words is n words of placeholder text, for the data of a component SkeletonOf draws: its text bars are as long as the text.
- `Chars(n int) string`: Chars is n characters of placeholder text, for a field whose length is known (a date, a price, a code).

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [skeleton.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// SkeletonVariant is a Skeleton's shape.
type SkeletonVariant string

const (
	SkeletonRectangular SkeletonVariant = ""         // square corners
	SkeletonRounded     SkeletonVariant = "rounded"  // rounded corners
	SkeletonCircular    SkeletonVariant = "circular" // a circle, size-10 unless Class sizes it
	SkeletonText        SkeletonVariant = "text"     // a line of text: 1em tall at the line's height
)

// Skeleton is a placeholder bone, the shape of content still loading:
//
//	<design.Skeleton variant={design.SkeletonCircular} class="size-12"/>
//	<design.Skeleton lines={3}/>
//
// With children it wraps the real content: while Loading the children are
// laid out but hidden, under a bone of their exact size, and when it isn't
// they show in the same box, so nothing moves when the data arrives:
//
//	<design.Skeleton loading={p.Saving} variant={design.SkeletonRounded}>
//	  <design.Button>Publish</design.Button>
//	</design.Skeleton>
type Skeleton struct {
	Variant SkeletonVariant
	// Lines stacks that many lines of text, the last one shorter.
	Lines     int
	Animation SkeletonAnimation
	// Loading hides the children under the bone; without children it is
	// ignored.
	Loading bool
	// Label is what a screen reader hears while Loading; "Loading…" by
	// default.
	Label string
	// Class is added to the bone's classes (with children or Lines, the
	// wrapper's), winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

var skeletonShape = map[SkeletonVariant]string{
	SkeletonRectangular: "h-4 w-full",
	SkeletonRounded:     "h-4 w-full rounded-md",
	SkeletonCircular:    "size-10 rounded-full",
	SkeletonText:        "my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm",
}

// over is the shape of the bone over children, filling their box.
var skeletonOver = map[SkeletonVariant]string{
	SkeletonRectangular: "inset-0",
	SkeletonRounded:     "inset-0 rounded-md",
	SkeletonCircular:    "inset-0 rounded-full",
	SkeletonText:        "inset-x-0 inset-y-[calc((1lh-1em)/2)] rounded-sm",
}

func (s Skeleton) bone(shape string) string {
	return cn("block bg-skeleton", shape, s.Animation.class())
}

// wraps reports whether the skeleton wraps real content.
func (s Skeleton) wraps() bool { return ui.Has(s.Children) }

func (s Skeleton) classes() string {
	switch {
	case s.wraps():
		return cn("relative w-fit", s.Class)
	case s.Lines > 0:
		return cn("flex flex-col", s.Class)
	}
	shape, ok := skeletonShape[s.Variant]
	if !ok {
		shape = skeletonShape[SkeletonRectangular]
	}
	return cn(s.bone(shape), s.Class)
}

// line is the classes of line i, the last of several shorter.
func (s Skeleton) line(i int) string {
	c := s.bone(skeletonShape[SkeletonText])
	if s.Lines > 1 && i == s.Lines-1 {
		c = cn(c, "w-4/5")
	}
	return c
}

func (s Skeleton) over() string {
	shape, ok := skeletonOver[s.Variant]
	if !ok {
		shape = skeletonOver[SkeletonRectangular]
	}
	return cn(s.bone(shape), "absolute")
}

// content is the classes of the children's wrapper, laid out as if it
// weren't there, its children hidden while Loading.
func (s Skeleton) content() string {
	if s.Loading {
		return "contents invisible"
	}
	return "contents"
}

// a11y are the root's attributes for assistive technology: a bone is
// hidden; a wrapper loading is a status, busy.
func (s Skeleton) a11y() []ui.Attr {
	switch {
	case !s.wraps():
		return []ui.Attr{{Name: "aria-hidden", Value: "true"}}
	case s.Loading:
		return []ui.Attr{{Name: "role", Value: "status"}, {Name: "aria-busy", Value: "true"}}
	}
	return nil
}
</script>

<div class={s.classes()} {s.a11y()...} {s.Attrs...}>
  if s.wraps() {
    if s.Loading {
      <span class="sr-only">{skeletonLabel(s.Label)}</span>
    }
    <div class={s.content()} inert={s.Loading}>{s.Children}</div>
    if s.Loading {
      <div aria-hidden="true" class={s.over()}></div>
    }
  } else {
    for i := range s.Lines {
      <div class={s.line(i)}></div>
    }
  }
</div>
```

```go [skeleton_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// SkeletonVariant is a Skeleton's shape.
type SkeletonVariant string

const (
	SkeletonRectangular SkeletonVariant = ""         // square corners
	SkeletonRounded     SkeletonVariant = "rounded"  // rounded corners
	SkeletonCircular    SkeletonVariant = "circular" // a circle, size-10 unless Class sizes it
	SkeletonText        SkeletonVariant = "text"     // a line of text: 1em tall at the line's height
)

// Skeleton is a placeholder bone, the shape of content still loading:
//
//	<design.Skeleton variant={design.SkeletonCircular} class="size-12"/>
//	<design.Skeleton lines={3}/>
//
// With children it wraps the real content: while Loading the children are
// laid out but hidden, under a bone of their exact size, and when it isn't
// they show in the same box, so nothing moves when the data arrives:
//
//	<design.Skeleton loading={p.Saving} variant={design.SkeletonRounded}>
//	  <design.Button>Publish</design.Button>
//	</design.Skeleton>
type Skeleton struct {
	Variant SkeletonVariant
	// Lines stacks that many lines of text, the last one shorter.
	Lines     int
	Animation SkeletonAnimation
	// Loading hides the children under the bone; without children it is
	// ignored.
	Loading bool
	// Label is what a screen reader hears while Loading; "Loading…" by
	// default.
	Label string
	// Class is added to the bone's classes (with children or Lines, the
	// wrapper's), winning over them (see cn).
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

var skeletonShape = map[SkeletonVariant]string{
	SkeletonRectangular: "h-4 w-full",
	SkeletonRounded:     "h-4 w-full rounded-md",
	SkeletonCircular:    "size-10 rounded-full",
	SkeletonText:        "my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm",
}

// over is the shape of the bone over children, filling their box.
var skeletonOver = map[SkeletonVariant]string{
	SkeletonRectangular: "inset-0",
	SkeletonRounded:     "inset-0 rounded-md",
	SkeletonCircular:    "inset-0 rounded-full",
	SkeletonText:        "inset-x-0 inset-y-[calc((1lh-1em)/2)] rounded-sm",
}

func (s Skeleton) bone(shape string) string {
	return cn("block bg-skeleton", shape, s.Animation.class())
}

// wraps reports whether the skeleton wraps real content.
func (s Skeleton) wraps() bool { return ui.Has(s.Children) }

func (s Skeleton) classes() string {
	switch {
	case s.wraps():
		return cn("relative w-fit", s.Class)
	case s.Lines > 0:
		return cn("flex flex-col", s.Class)
	}
	shape, ok := skeletonShape[s.Variant]
	if !ok {
		shape = skeletonShape[SkeletonRectangular]
	}
	return cn(s.bone(shape), s.Class)
}

// line is the classes of line i, the last of several shorter.
func (s Skeleton) line(i int) string {
	c := s.bone(skeletonShape[SkeletonText])
	if s.Lines > 1 && i == s.Lines-1 {
		c = cn(c, "w-4/5")
	}
	return c
}

func (s Skeleton) over() string {
	shape, ok := skeletonOver[s.Variant]
	if !ok {
		shape = skeletonOver[SkeletonRectangular]
	}
	return cn(s.bone(shape), "absolute")
}

// content is the classes of the children's wrapper, laid out as if it
// weren't there, its children hidden while Loading.
func (s Skeleton) content() string {
	if s.Loading {
		return "contents invisible"
	}
	return "contents"
}

// a11y are the root's attributes for assistive technology: a bone is
// hidden; a wrapper loading is a status, busy.
func (s Skeleton) a11y() []ui.Attr {
	switch {
	case !s.wraps():
		return []ui.Attr{{Name: "aria-hidden", Value: "true"}}
	case s.Loading:
		return []ui.Attr{{Name: "role", Value: "status"}, {Name: "aria-busy", Value: "true"}}
	}
	return nil
}

func (s Skeleton) View() ui.Node {
	var div ui.Node
	if s.wraps() {
		var span ui.Node
		if s.Loading {
			span = ui.HTML(`<span class="sr-only">{skeletonLabel(s.Label)}</span>`, skeletonLabel(s.Label))
		}
		var div2 ui.Node
		if s.Loading {
			div2 = ui.HTML(`<div aria-hidden="true" class={s.over()}/>`, s.over())
		}
		div = ui.HTML(`
{span}
<div class={s.content()} inert={s.Loading}>{s.Children}</div>
{div2}`, span, s.content(), s.Loading, s.Children, div2)
	} else {
		var divs []ui.Node
		for i := range s.Lines {
			divs = append(divs, ui.HTML(`<div class={s.line(i)}/>`, s.line(i)))
		}
		div = ui.HTML(`{divs}`, divs)
	}
	return ui.HTML(`<div class={s.classes()} {s.a11y()...} {s.Attrs...}>{div}</div>`, s.classes(), s.a11y(), s.Attrs, div)
}

func (s Skeleton) Render(ctx context.Context, w io.Writer) error { return s.View().Render(ctx, w) }
```

```vuka-html [skeleton_of.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// SkeletonOf draws its children — a real component given placeholder data
// — as bones, so the placeholder is the component's own layout:
//
//	<design.SkeletonOf repeat={3} class="grid gap-4">
//	  <PostCard post={Post{Title: design.Words(6), Author: design.Chars(12)}}/>
//	</design.SkeletonOf>
//
// The stylesheet vuka ui init writes does it: text becomes bars as long as
// the text, images, icons, videos, canvases, buttons and inputs blocks of
// their size, and borders and spacing stay, so the bones sit exactly where
// the content will. The copy is inert and hidden from assistive
// technology; the wrapper is a busy status with Label for screen readers.
//
// An element of the real markup may say how it is drawn with
// data-skeleton: "keep" draws it as it is, "ignore" leaves its space
// empty, "replace" makes it one bone of its size, and "unite" one bone of
// its content box, its border and padding kept.
type SkeletonOf struct {
	// Repeat draws the children that many times; once by default.
	Repeat    int
	Animation SkeletonAnimation
	// Label is what a screen reader hears; "Loading…" by default.
	Label string
	// Class is added to the wrapper's classes, winning over them (see cn):
	// a grid or a gap for Repeat.
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// copies are the children, Repeat times.
func (s SkeletonOf) copies() []ui.Node {
	out := make([]ui.Node, max(s.Repeat, 1))
	for i := range out {
		out[i] = s.Children
	}
	return out
}
</script>

<div role="status" aria-busy="true" class={cn("relative", s.Class)} {s.Attrs...}>
  <span class="sr-only">{skeletonLabel(s.Label)}</span>
  <div class="contents" data-skeleton-of={s.Animation.value()} inert aria-hidden="true">
    {s.copies()}
  </div>
</div>
```

```go [skeleton_of_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// SkeletonOf draws its children — a real component given placeholder data
// — as bones, so the placeholder is the component's own layout:
//
//	<design.SkeletonOf repeat={3} class="grid gap-4">
//	  <PostCard post={Post{Title: design.Words(6), Author: design.Chars(12)}}/>
//	</design.SkeletonOf>
//
// The stylesheet vuka ui init writes does it: text becomes bars as long as
// the text, images, icons, videos, canvases, buttons and inputs blocks of
// their size, and borders and spacing stay, so the bones sit exactly where
// the content will. The copy is inert and hidden from assistive
// technology; the wrapper is a busy status with Label for screen readers.
//
// An element of the real markup may say how it is drawn with
// data-skeleton: "keep" draws it as it is, "ignore" leaves its space
// empty, "replace" makes it one bone of its size, and "unite" one bone of
// its content box, its border and padding kept.
type SkeletonOf struct {
	// Repeat draws the children that many times; once by default.
	Repeat    int
	Animation SkeletonAnimation
	// Label is what a screen reader hears; "Loading…" by default.
	Label string
	// Class is added to the wrapper's classes, winning over them (see cn):
	// a grid or a gap for Repeat.
	Class    string
	Attrs    []ui.Attr
	Children ui.Node
}

// copies are the children, Repeat times.
func (s SkeletonOf) copies() []ui.Node {
	out := make([]ui.Node, max(s.Repeat, 1))
	for i := range out {
		out[i] = s.Children
	}
	return out
}

func (s SkeletonOf) View() ui.Node {
	return ui.HTML(`
<div role="status" aria-busy="true" class={cn("relative", s.Class)} {s.Attrs...}>
  <span class="sr-only">{skeletonLabel(s.Label)}</span>
  <div class="contents" data-skeleton-of={s.Animation.value()} inert aria-hidden="true">{s.copies()}</div>
</div>`, cn("relative", s.Class), s.Attrs, skeletonLabel(s.Label), s.Animation.value(), s.copies())
}

func (s SkeletonOf) Render(ctx context.Context, w io.Writer) error { return s.View().Render(ctx, w) }
```

:::

</div>

Beside it, the same in both: `skeleton.go`.

```go
package design

import "strings"

// SkeletonAnimation is how a skeleton's bones move.
type SkeletonAnimation string

const (
	SkeletonPulse SkeletonAnimation = ""     // the bones fade in and out
	SkeletonWave  SkeletonAnimation = "wave" // a highlight sweeps across every bone on screen as one
	SkeletonNone  SkeletonAnimation = "none" // still
)

// animation is the class of a bone moving as a.
func (a SkeletonAnimation) class() string {
	switch a {
	case SkeletonWave:
		return "skeleton-wave"
	case SkeletonNone:
		return ""
	}
	return "skeleton-pulse"
}

// value is a SkeletonOf's data-skeleton-of.
func (a SkeletonAnimation) value() string {
	if a == SkeletonPulse {
		return "pulse"
	}
	return string(a)
}

// skeletonLabel is what a screen reader hears while a skeleton shows.
func skeletonLabel(label string) string {
	if label == "" {
		return "Loading…"
	}
	return label
}

var placeholderWords = strings.Fields("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua")

// Words is n words of placeholder text, for the data of a component
// SkeletonOf draws: its text bars are as long as the text.
func Words(n int) string {
	w := make([]string, max(n, 0))
	for i := range w {
		w[i] = placeholderWords[i%len(placeholderWords)]
	}
	return strings.Join(w, " ")
}

// Chars is n characters of placeholder text, for a field whose length is
// known (a date, a price, a code).
func Chars(n int) string { return strings.Repeat("x", max(n, 0)) }
```
