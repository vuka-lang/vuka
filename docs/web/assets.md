# Styles & Scripts

[[toc]]

## Introduction

You keep a component's CSS and browser behavior in its `.vuka.html` shape,
after the markup. The `<script lang="go">` still comes first and declares
exactly one struct.

Styles are scoped to the component, and scripts run once per instance. For
utility classes across the app, see [Tailwind CSS](/web/styling); for
images, fonts and files served as they are, [Static Files](/web/static-files).

## A Component With Style and Script

This counter carries a scoped stylesheet and a script whose `update` replays
an animation on its output:

```html
<script lang="go">
package components

type Counter struct { n int }

func (c *Counter) Increment() { c.n++ }
</script>

<section>
  <output>{n}</output>
  <button onClick={Increment}>+</button>
</section>

<style scoped>
output { display: inline-block; font-weight: bold; }
output.bump { animation: bump 320ms ease-out; }
@keyframes bump { 40% { transform: scale(1.4); } }
</style>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Counter>((el) => ({
  update() {
    const out = el.querySelector('output')!
    out.classList.remove('bump')
    void out.offsetWidth
    out.classList.add('bump')
  },
}))
</script>
```

## Scoped Stylesheets

`<style scoped>` rewrites selectors to the shape's elements: each element
its markup writes carries the shape's scope attribute, `data-vk-s`, an id
derived from the package and the struct's name, so it is the same in every
build and on every replica. Child components carry their own scope, and
children passed in a slot keep the caller's scope. Keyframe names are scoped
too. The attribute is part of the rendered markup, so a live re-render keeps
it.

A `<style>` without `scoped` is global CSS: it is bundled once into the
app's stylesheet, however many shapes or pages use the component, and the
same stylesheet written in two shapes is included once.

`:global(...)` escapes scoping, and `:deep(...)` reaches child content. CSS
syntax errors report their source file and line.

### Tailwind in Components

Tailwind utilities and `@apply` work with the app stylesheet. Global
document styles belong in the app stylesheet rather than a reusable
component.

## Scripts

A bundled `<script lang="ts">` runs per component instance. The component
helper supplies the root element, props and context.

`update` responds to changed props, and `destroy` releases resources when
the instance leaves. Unexported, function and node fields are not JSON
props. The generated declarations supply TypeScript with the shape's
exported data, state and handler types.

The context exposes live methods, state handles, refs, the events the Go
pushes and navigation. Clean up listeners and timers on destroy. See
[Component Scripts](/ui/scripts) for the whole `ctx` and worked examples.

> [!WARNING]
> Browser behavior cannot replace server validation.

## Inline Scripts

`<script is:inline>` is emitted directly. Use `{json(value)}` for its
script-safe JSON hole, never unescaped string concatenation.

## Building Assets {#building}

`vuka run` and `vuka build` bundle scripts and scope CSS. `vuka build` also
embeds each stylesheet, script and island chunk's brotli and gzip with it,
so the binary sends them compressed with no work while it runs
([Compression and Caching](/web/deployment#compression)). Dependencies
imported by component scripts must be available to the frontend build. For
npm frameworks and hydration policies, see [Islands](/web/islands).
