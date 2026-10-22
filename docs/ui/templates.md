# Templates

A `.vuka.html` file is one complete rendering shape: Go header, then markup.
The header declares exactly one struct. Put form DTOs and other data types
in adjacent logic files, not additional structs in this header.

## A component

Save as `components/sales.vuka.html`:

```html
package components

import "github.com/vuka-lang/ui"

type Sales struct {
    Title string
    Week []int
    Children ui.Node
}

func (s Sales) Total() int {
    n := 0
    for _, d := range s.Week { n += d }
    return n
}
<section>
  <h2>{Title}</h2>
  for _, d := range Week {
    <span class:big={d > 10}>{d}</span>
  }
  <p>Total: {Total()}</p>
  {Children}
</section>
```

The first line starting with `<` begins markup. Keep package, imports,
struct, hooks and Go methods above it. The struct's name need not match
its filename. A shape with no struct, multiple structs, or no markup is an error.
A reusable partial gets its own shape file.

## Holes

| Form | Meaning |
|---|---|
| `{Title}` | Escaped child text or a node |
| `title={Title}` | Attribute value |
| `href="/pets/{ID}"` | Interpolated value |
| `class:active={Active}` | Conditional class |
| `{attrs...}` | Attribute spread |
| `style:--accent={Color}` | Sanitized style property |
| `onClick={Save}` | Server event handler |
| `onClick={open = !open}` | Browser-state assignment |
| `bind:value={Q}` | Two-way binding to a field marked `@ui.Bind` |

Fields and methods are available by bare name in markup. Locals shadow
fields. The receiver name used by header methods is also available.
Strings and numbers are escaped; nodes render as nodes; nil renders nothing.

## Statements

Inside the markup root, use Go control flow, without an extra expression wrapper:

```html
<ul>
  if len(Week) == 0 {
    <li>No sales yet.</li>
  } else {
    for i, amount := range Week {
      <li key={i}>{amount}</li>
    }
  }
</ul>
```

`if`, `for`, `switch`, local declarations and assignments run in order.
Keep rendering free of writes to persistent state and database work.

## Components and slots

A capitalized or dotted tag names a component. Attributes set exported
fields by camelCase: `onSelect` sets `OnSelect`. Content fills
`Children ui.Node`; `<slot:actions>` fills `Actions ui.Node`.

```html
package components

import "github.com/vuka-lang/ui"

type Panel struct {
    Title string
    Actions ui.Node
    Children ui.Node
}
<section>
  <header><h2>{Title}</h2>{Actions}</header>
  {Children}
</section>
```

Use `<components.Panel title="Orders"><slot:actions><button>New</button></slot:actions><p>Orders</p></components.Panel>`
in a consuming shape file. Give repeated live children stable keys.

## Scripts and styles

A shape can include one `<style scoped>` and a bundled `<script lang="ts">`
after its root. Scoped CSS applies to that shape's elements; `:global(...)`
escapes it. A component script receives instance props, update/destroy
callbacks and live/state handles. See [Styles and scripts](/web/assets).
`<script is:inline>` stays inline; `{json(value)}` emits script-safe JSON.
Imported `.tsx`, `.ts` and `.vue` components are [islands](/web/islands).

## Render context

Markup can read the implicit `ctx`, the current render's `context.Context`.
Do not store it on the component or expose secrets in browser props. Fetch
expensive data in hooks or handlers. Presentation checks do not replace
server authorization.

## Building

`vuka run`, `vuka build` and `vuka test` compile shape files through the
overlay. `vuka gen` writes generated Go for ordinary Go tooling. Generated
rendering methods are compiler output; write the shape file itself.
