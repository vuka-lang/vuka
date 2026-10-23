# templ Interop

[[toc]]

## Introduction

`ui.Node` is compatible with `templ.Component`. You can use existing templ
libraries inside a shape without changing its one-struct Go-header form.

## Using a templ Component

This shape renders a component from an imported templ library:

```html
<script lang="go">
package components

import "example.com/app/legacy"

type Welcome struct { Name string }
</script>

<section><legacy.Greeting name={Name} /></section>
```

The imported library supplies the templ component. Content between its tags
is passed through templ's children context.

`.templ` files in a module that requires templ are compiled by the vuka
command alongside the other files.

## templ on Live Pages {#on-live-pages}

templ compiled through vuka participates in render-tree recording.
Expressions, branches and loops form dynamic frames, so changes can be patched
inside the component.

Externally generated templ components still render, but opaque HTML may update
as a larger unit. Shared layouts for application pages belong in
`pages/_layout.vuka.html`.

## Passing Children {#children}

`templx.WithChildren` bridges children for library-level Go integration. This
is interop with an existing node library; application markup remains in shape
files. See [Templates](/ui/templates#components-and-slots).
