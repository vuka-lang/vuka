# Styles and scripts

Keep a component's CSS and browser behavior in its `.vuka.html` shape after
the markup. The Go header still comes first and declares exactly one struct.

```html
package components

type Counter struct { n int }

func (c *Counter) Increment() { c.n++ }
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

## Scoped stylesheets

`<style scoped>` rewrites selectors to the shape's elements. Child components
carry their own scope; passed children retain the caller's scope. `:global(...)`
escapes scoping and `:deep(...)` reaches child content. Keyframe names are
scoped too. CSS syntax errors report their source file and line.

Tailwind utilities and `@apply` work with the app stylesheet. Global document
styles belong in the app stylesheet rather than a reusable component.

## Scripts

A bundled `<script lang="ts">` runs per component instance. The component
helper supplies root element, props and context. `update` responds to changed
props and `destroy` releases resources when the instance leaves. Unexported,
function and node fields are not JSON props. The generated declarations supply
TypeScript with the shape's exported data, state and handler types.

The context exposes live methods, state handles and navigation. Clean up
listeners and timers on destroy. Browser behavior cannot replace server validation.

## Inline scripts

`<script is:inline>` is emitted directly. Use `{json(value)}` for its
script-safe JSON hole, never unescaped string concatenation.

## Building

`vuka run` and `vuka build` bundle scripts and scope CSS. Dependencies imported
by component scripts must be available to the frontend build. For npm frameworks
and hydration policies, see [Islands](/web/islands).
