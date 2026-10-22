# UI

Write everything that renders in a `.vuka.html` shape file. Its Go header
contains a package clause, imports, exactly one struct, hooks and methods.
Markup begins on the first line whose first non-space character is `<`.
Logic without markup belongs in `.go` or `.vuka` files.

```html
package components

import "github.com/vuka-lang/ui"

type Greeting struct {
    Name string
    Children ui.Node
}
<section>
  <h1>Hello, {Name}!</h1>
  {Children}
</section>
```

Save this as `components/greeting.vuka.html`. Use it as a tag in another
shape file: `<components.Greeting name="Ada"><p>Welcome.</p></components.Greeting>`.

| Field | Meaning |
|---|---|
| Exported field | Prop, supplied by the parent; on pages, a path or query value |
| Unexported field | Tracked server state, or an injected dependency when its type is provided |
| `ui.State[T]` | Browser state, read normally and written by assignment in event attributes |
| `Children ui.Node` | Content between a component's tags |

A component is live when it embeds `ui.Live`, declares a decorated hook,
or wires a server method to an event. Browser-only commands keep a component static.

`ui.Node` interoperates with templ. Rendering, live sessions, client state,
terminal rendering and testing live in the ui module and its subpackages.

Start with [Templates](/ui/templates), [Components](/ui/components),
[Stateful components](/ui/stateful) and [Client state](/ui/state).
