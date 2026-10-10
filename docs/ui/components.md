# Components

A component is a function returning `ui.Node`; the [JSX](/features/jsx)
page has how tags bind to it. This page is what ui does with the markup.

## Nodes

`ui.Node` is `templ.Component`: anything with
`Render(ctx context.Context, w io.Writer) error`. So a Vuka component is a
templ component and a templ component is a Vuka child, with nothing in
between. The nodes JSX builds (`*ui.Element`, text, groups, blocks) are also
a tree a [renderer](/ui/rendering#walking-a-tree) can walk.

| | |
|---|---|
| `ui.El(tag, attrs, children...)` | an element — what `<tag …>` becomes |
| `ui.Text(v)` | text, escaped |
| `ui.Child(v)` | a `{v}` child: a node as is, `nil` as nothing, an `error` as its message, a `[]ui.Node` or `[]any` as each element, anything else as text (`fmt.Sprint`) |
| `ui.Fragment(children...)` | children with no element around them |
| `ui.Safe(html)` | trusted HTML, written as is; never user input |
| `ui.ErrorBoundary(fallback, children...)` | the children, or `fallback(err)` in place of all they rendered when one fails |
| `ui.NodeFunc` | a function rendering HTML: `templ.ComponentFunc` |

## Attributes

An attribute's value renders the way templ renders the same attribute
written as an expression:

- `nil` and `false` leave it out, `true` writes it bare;
- `class` takes a string or anything `templ.Classes` does (`[]string`,
  `map[string]bool`, `templ.KV`);
- `style` takes a `ui.Style{"color": "red"}` or anything templ's style
  attributes do, sanitized: an unsafe property or value becomes templ's
  placeholder;
- `href` on `<a>` and `<link>`, `action` on `<form>`, `data` on `<object>` are
  URLs: relative ones and http, https, mailto, tel, ftp and ftps pass;
  anything else (a `javascript:` URL) becomes
  `about:invalid#TemplFailedSanitizationURL`; a `templ.SafeURL` passes as is;
- a node is rendered and escaped; anything else goes through `fmt.Sprint`,
  escaped.

`className` and `htmlFor` are `class` and `for`, as in React. Text inside
`<script>` is JSON-encoded, as templ does with expressions in a script; text
in `<style>` is written as is with `</` escaped. `<html>` gets the doctype.
A void element (`<br>`, `<img>`, `<input>`, …) with children is a render
error.

## Children and errors

Children go to a `children ui.Node` parameter or a `Children ui.Node`
field. A component returning `(ui.Node, error)` fails the render with its
error — `ui.String` and `ui.Write` return it, `ui.Handler` answers 500 —
unless an `ui.ErrorBoundary` above it renders a fallback instead:

```vuka
{ui.ErrorBoundary(func(err error) ui.Node { return <p className="error">{err}</p> },
	<Avatar url={u.Avatar} />)}
```

## key

`key={…}` on an element names it among its siblings. It is never written to
the HTML of a plain render; under a live session it is written as
`data-vk-key="…"` (its value as `fmt.Sprint` prints it), which the browser
runtime's morph uses to pair rows, and it scopes the stateful components
inside the element (their instances follow the key when the list is
reordered).

## Picking ui

A `.vuka` file uses ui by importing it; any name works, a dot import too.
Its components can still take and return other packages' types: a templ
component, a templUI component, a `templ.Component` from anywhere is a
`ui.Node`.
