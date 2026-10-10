# Rendering

| | |
|---|---|
| `ui.Handler(f)` | an `http.Handler` rendering the node `f` builds per request (through `templ.Handler`); an error answers 500 with only the status text |
| `ui.Write(w, r, n)` | render into a response, all at once, so an error still gets a clean 500 |
| `ui.String(ctx, n)` | render to a string |
| `n.Render(ctx, w)` | templ's own method, to any `io.Writer` |

```vuka
http.Handle("/pets", ui.Handler(func(r *http.Request) (ui.Node, error) {
	return Pets(store.All(), true), nil
}))
```

Escaping and sanitizing are templ's: [attributes](/ui/components#attributes)
render as templ renders the same expression.

## Walking a tree

A JSX tree is a value you can walk, not only HTML: `ui.Walk(ctx, n, r)`
hands its elements, text and raw HTML to a `ui.Renderer`:

```go
type Renderer interface {
	Open(e *ui.Element) error  // before children
	Close(e *ui.Element) error // after children (not for void elements)
	Text(s string) error
	Raw(html string) error                  // Safe content
	Opaque(ctx context.Context, n ui.Node) error // a component that only renders HTML (templ, NodeFunc)
}
```

Fragments, blocks and `{expr}` conversions are flattened; a failed `Try`
stops the walk with its error; an `ErrorBoundary` walks its children into a
recording first and replays it — or walks its fallback instead, so a
renderer never sees the failed part. The HTML renderer behind every node's
`Render` is one such renderer (`ui.NewHTMLRenderer`).

## The terminal

`github.com/vuka-lang/ui/term` renders the same components as terminal text:
headings, paragraphs word-wrapped at a width, lists, aligned tables, quotes,
code, links, and inline bold/italic/underline as ANSI styles when `Color`
is on.

```vuka
term.Render(ctx, os.Stdout, Pets(pets, false), term.Options{Width: 80, Color: true})
s, err := term.String(ctx, Pets(pets, false), term.Options{})
```

`term` reads the HTML of templ components and `Safe` content too, parsing
it into the same tree, so a page inside a templ layout keeps its headings,
tables and lists. Whether to use color (`NO_COLOR`, a tty) is the caller's
decision.
