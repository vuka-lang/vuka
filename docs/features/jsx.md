# UI Markup

[[toc]]

## Introduction

Vuka renders UI from `.vuka.html` shape files. A shape contains a
`<script lang="go">` with the struct it renders, followed by markup. Logic-only `.go` and `.vuka`
files contain services, data types and helpers.

## Writing a Shape

This shape declares a `Greeting` component with one prop:

```html
<script lang="go">
package components

type Greeting struct { Name string }
</script>

<section><h1>Hello, {Name}!</h1></section>
```

The markup follows `</script>`. Exported fields are props;
unexported fields hold tracked server state or provided dependencies.
`ui.State[T]` holds browser state, written by assignments in `on…` attributes.

## Markup

Use holes for values, normal HTML attribute names, `class:x={condition}`,
`{attrs...}`, `bind:value` and `style:property`. Strings are escaped.

Go `if`, `for` and `switch` statements work inside the markup root.

## Using Components

Capitalized or dotted tags name components. Props bind exported fields,
children fill `Children ui.Node` and named slots fill node fields. Each
rendering component lives in its own shape file.

A generic component's tag infers its type arguments from its props, as Go
infers a call's: `<components.DataTable rows={orders}>` is a
`DataTable[Order]`, and its scoped slots' `let` values take the inferred
types ([Using Component Tags](/ui/templates#using-component-tags),
[E0181](/errors/E0181)). A `(T, error)` call, a `vuka.Result[T]` or a
`vuka.Option[T]` fills a hole or a prop as its `T`
([Fallible and Optional Values](/ui/templates#calls-returning-an-error)).

## What It Becomes {#what-it-becomes}

The compiler emits ordinary Go rendering code and render-tree metadata.
You author the shape, not the generated calls. See [Templates](/ui/templates),
[Live Components](/ui/stateful), [Client State](/ui/state) and
[Folder Routes](/web/folder-routes).
