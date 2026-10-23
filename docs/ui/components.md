# Components

[[toc]]

## Introduction

Components are the reusable pieces of your pages: a notice, a card, a menu. A
component is a `.vuka.html` shape with one struct and its markup in the same
file. Exported fields are props; content and named slots are node props.

## Writing a Component {#writing}

This shape declares a notice with a title, a flag and children:

```html
package components

import "github.com/vuka-lang/ui/v2"

type Notice struct {
    Title string
    Important bool
    Children ui.Node
}
<aside class:important={Important}>
  <h2>{Title}</h2>
  {Children}
</aside>
```

Use it as a tag from another shape:
`<components.Notice title="Saved" important><p>Your changes are saved.</p></components.Notice>`.
A bare bool attribute is true. HTML attributes retain HTML names, including
`class`, `for`, `aria-*` and `data-*`.

## What a Hole Accepts {#nodes}

Child holes accept nodes, slices of nodes, escaped strings and numbers,
errors and nil. `ui.Node` is compatible with templ components. A static
shape is created and rendered per request; a live shape retains its server
instance for the connected page.

## Dynamic Attributes {#attributes}

Use `class:selected={Selected}`, `{attrs...}` and `style:color={Color}` for
dynamic classes, attribute sets and sanitized styles. Event attributes accept
server methods or browser commands. See [Templates](/ui/templates#holes).

## Linking Between Pages {#links}

`<ui.Link href="/pets">Pets</ui.Link>` participates in live navigation.
Ordinary `<a>` elements remain full-document links. `active="prefix"` (or
`"exact"`) with `activeClass` marks the link to the current page in the
browser, the way to mark a layout's menu; `prefetch` tunes fetching its
page ahead. See [Navigation](/web/navigation).

## Keying Repeated Components {#key}

Give siblings stable `key={record.ID}` values. A key identifies the same
logical item across renders, preserving its live instance.

> [!WARNING]
> Array positions are unsuitable when items can be reordered.

## The Component Kit {#kit}

The ui kit exposes components as tags, such as `<ui.Card>` and `<ui.Button>`.
Use their exported fields as attributes, their Children field for content,
and their named node fields as slots. The kit uses the same node tree and
live protocol as application components; no second rendering syntax is needed.
