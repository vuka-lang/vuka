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
| Tagged `` `vuka:"-"` `` | Left out of change tracking: state the render doesn't read |

A struct's methods work the same way:

| Method | Meaning |
|---|---|
| Exported, sent from an event attribute | An event: the browser can call it, and the struct must embed `ui.Live` ([E0169](/errors/E0169)) |
| Named by a struct form's `onSubmit` | The form's handler, exported or not: over HTTP unless the page embeds `ui.Live` |
| Exported, not sent from markup | An ordinary Go method, unless its name means something below |
| Unexported | Not callable from the browser, except as a form's handler or a [`Push`](/ui/js#push) step |
| Decorated, such as `@ui.Mount` | A hook, unexported, run by vuka; the struct must embed `ui.Live` |
| `Title`, `Crumbs` | The page's title and breadcrumbs |
| `Post`, `Put`, `Patch`, `Delete` | On a page, [answer those HTTP methods](/web/folder-routes#several-methods-on-one-page) on its path |
| `Load` | On a layout or guard, runs before the page. On a page it never runs |

A dependency is only injected into an unexported field. An exported field of a
provided type is a prop, set by the parent or by the page's constructor.

## Static and Live Components

A component is live when it embeds `ui.Live`. It must if it declares a decorated
hook or wires a server method to an event, other than as a
[`Push`](/ui/js#push) step's handler or a struct form's `onSubmit`, which a page
that isn't live calls over HTTP. Browser-only commands keep a component static. See [Live Components](/ui/stateful) for live components.

## Where to Go Next

`ui.Node` interoperates with templ (see [templ Interop](/ui/templ)).
Rendering, live sessions, client state, terminal rendering and testing live in
the ui module and its subpackages.

Start with [Templates](/ui/templates), [Components](/ui/components),
[Live Components](/ui/stateful) and [Client State](/ui/state).
