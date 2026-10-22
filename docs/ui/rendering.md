# Rendering

Author a node in a `.vuka.html` shape. Its generated Go implements the node
interface, so it can be rendered through ui or any templ-compatible renderer.

| Function | Purpose |
|---|---|
| `ui.String(ctx, node)` | Render to a string |
| `ui.Write(w, request, node)` | Buffer an HTTP response so failures can answer cleanly |
| `ui.Handler(factory)` | Adapt a node-producing factory to `http.Handler` |
| `node.Render(ctx, writer)` | Render through templ's component interface |

For web pages, place the shape in `pages/` and let the generated routes serve
it. Direct rendering is useful for tests, email and non-web outputs.

## Walking a tree

`ui.Walk(ctx, node, renderer)` visits elements, text and opaque components.
An implementation supplies Open, Close, Text, Raw and Opaque operations.
Escaping and sanitization remain the HTML renderer's responsibility. Keep
rendering side-effect free and propagate context cancellation.

## The terminal

`github.com/vuka-lang/ui/v2/term` renders the same nodes as text, including lists,
tables, headings and links. Its Options select width and ANSI color. Respect
`NO_COLOR` and terminal detection in your caller. Visitor control characters
must not become terminal commands.
