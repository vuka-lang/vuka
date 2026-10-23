# Shape Files

[[toc]]

## Introduction

You write everything that renders in a `.vuka.html` shape file. A shape file
pairs a small `<script lang="go">` with the markup it renders, so a component's props,
state and markup sit together in one place.

Logic without markup belongs in `.go` or `.vuka` files. This page shows the
layout of a shape file; [Templates](/ui/templates) covers the markup itself.

## Writing a Shape File

A shape's `<script lang="go">` comes first and contains a package clause,
imports, exactly one struct, hooks and methods. The markup follows
`</script>`.

Save this as `components/greeting.vuka.html`:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Greeting struct {
    Name string
    Children ui.Node
}
</script>

<section>
  <h1>Hello, {Name}!</h1>
  {Children}
</section>
```

Use it as a tag in another shape file:
`<components.Greeting name="Ada"><p>Welcome.</p></components.Greeting>`.

## What the Struct's Fields Mean

The kind of each field decides what it does:

| Field | Meaning |
|---|---|
| Exported field | Prop, supplied by the parent; on pages, a path or query value |
| Unexported field | Tracked server state, or an injected dependency when its type is provided |
| `ui.State[T]` | Browser state, read normally and written by assignment in event attributes |
| `Children ui.Node` | Content between a component's tags |

## Static and Live Components

A component is live when it embeds `ui.Live`, declares a decorated hook, or
wires a server method to an event. Browser-only commands keep a component
static. See [Stateful Components](/ui/stateful) for live components.

## Where to Go Next

`ui.Node` interoperates with templ (see [templ Interop](/ui/templ)).
Rendering, live sessions, client state, terminal rendering and testing live in
the ui module and its subpackages.

Start with [Templates](/ui/templates), [Components](/ui/components),
[Stateful Components](/ui/stateful) and [Client State](/ui/state).
