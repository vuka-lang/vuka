# UI Markup

[[toc]]

## Introduction

Vuka renders UI from `.vuka.html` shape files. A shape contains a Go header
with exactly one struct followed by markup. Logic-only `.go` and `.vuka`
files contain services, data types and helpers.

## Writing a Shape

This shape declares a `Greeting` component with one prop:

```html
package components

type Greeting struct { Name string }
<section><h1>Hello, {Name}!</h1></section>
```

The first line starting with `<` begins markup. Exported fields are props;
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

## What It Becomes {#what-it-becomes}

The compiler emits ordinary Go rendering code and render-tree metadata.
You author the shape, not the generated calls. See [Templates](/ui/templates),
[Stateful Components](/ui/stateful), [Client State](/ui/state) and
[Folder Routes](/web/folder-routes).
