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
