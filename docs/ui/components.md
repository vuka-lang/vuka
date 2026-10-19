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
| `ui.Template(markup)` | markup with numbered holes, parsed once — what each piece of JSX becomes; `Render(args...)` fills them |
| `ui.El(tag, attrs, children...)` | an element built with calls |
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
- URL attributes are sanitized: `href` on any element (`<a>`, `<link>`,
  `<area>`, `<base>`, SVG's `<use>`, `<image>`, …), `xlink:href`, `action`
  on `<form>`, `data` on `<object>`, `src` on `<iframe>`, `<frame>`,
  `<embed>`, `<source>`, `<track>`, `<video>`, `<audio>`, `<img>`, `<input>`
  and `<script>`, `poster` on `<video>`, and `formaction` on `<button>` and
  `<input>`, whatever the case of the name. Relative URLs and http, https,
  mailto, tel, ftp and ftps pass; anything else (a `javascript:` URL)
  becomes `about:invalid#TemplFailedSanitizationURL`. A media source (`src`
  on `<img>`, `<source>`, `<video>`, `<audio>`, `<input>`, `<track>`, a
  video's `poster`) also keeps a `data:` URL of its kind (`data:image/…`,
  `data:audio/…`, `data:video/…`, `data:text/vtt`). A `templ.SafeURL`
  passes as is;
- a node is rendered and escaped; anything else goes through `fmt.Sprint`,
  escaped.

`className` and `htmlFor` are `class` and `for`, as in React. Text inside
`<script>` is JSON-encoded, as templ does with expressions in a script; text
in `<style>` is written as is with `</` escaped. `<html>` gets the doctype.
A void element (`<br>`, `<img>`, `<input>`, …) with children is a render
error.

## Links

`<ui.Link href="/books/7">Dune</ui.Link>` is an `<a href>` a live page
follows over its connection instead of reloading; `patch` makes it a move
to the same page with other parameters. It is a plain link everywhere else.
See [navigation](/web/navigation).

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

## Kit

ui ships a set of token-styled components in the package itself, so
`import "github.com/vuka-lang/ui"` brings both the primitives above and these.
Each takes one props struct (its `Children` field is the tag's children), so
it is used as a tag:

```vuka
<ui.Button Variant={ui.Primary} Icon={ui.Icon("plus")} On={c.New}>New</ui.Button>
<ui.Card Title={ui.Text("Members")} Header={header}>{table}</ui.Card>
```

| | |
|---|---|
| `ui.Button` | a button, or a link when `Href` is set; a click `On` handler spins it until its reply |
| `ui.Submit` | a form submit button (a `Button` of `type=submit`) |
| `ui.Actions` | a right-aligned row of buttons, for a card header or modal footer |
| `ui.Cancel` | a dismiss button for a modal footer |
| `ui.Card` | a bordered surface with an optional header (title left, actions right) and a body |
| `ui.Tile` / `ui.TileGrid` | a dashboard stat card (icon, big number, label) and a responsive row of them |
| `ui.Tabs` | a tab bar underlining the active tab; each tab may carry a count |
| `ui.Badge` | a small status pill |
| `ui.Alert` | an inline message box in four tones |
| `ui.Toast` / `ui.ToastContainer` | one notification, and the stack a live view renders them into |
| `ui.Modal` | a dialog over a backdrop — server-owned through `Open`/`OnClose`, or browser-only via `OpenModal` |
| `ui.Form`, `ui.Field`/`ui.Input`/`ui.Password`/`ui.Select`/`ui.Checkbox` | a live-wired form and its fields, errors drawn from the view |
| `ui.Table` | rows of type `T` as a table; presentational |
| `ui.DataTable` | a `Table` with a search box, sortable headers and a pager, driven from a live view's events |
| `ui.Icon`, `ui.Spinner` | an icon by name, and a loading spinner |

Tone and size are shared constants: `ui.Default`, `ui.Primary`, `ui.Success`,
`ui.Danger`, `ui.Warning`, `ui.Info`, `ui.Ghost`; `ui.SizeSm`, `ui.SizeMd`,
`ui.SizeLg`. Everything is styled with the `--ui-*`
[tokens](/ui/rendering), light and dark.

`Table` and `DataTable` are generic in the row type. Their type argument is
[inferred](/features/jsx#components) from the attributes — `Columns` and
`Rows` pin it — so a bare tag is enough:

```vuka
<ui.Table
	Columns={cols}
	Rows={d.rows}
	RowKey={func(t Task) any { return t.ID }}
	Selectable={true}
	RowSelected={func(t Task) bool { return d.picked(t.ID) }}
	OnSelect={func(t Task) any { id := t.ID; return func(on bool) { d.pick(id, on) } }}
	OnSelectAll={d.pickPage}
	AllSelected={d.allPicked()}
/>
```

A selection checkbox is a live field: it fires `onChange` with its new
checked state, so `OnSelect` returns a handler taking that `bool` and
`OnSelectAll` takes one directly — set the row to what the box now is rather
than toggling a server-side guess, which keeps the box and the server in sync
(see [field sync](/web/live)).

## Picking ui

A `.vuka` file uses ui by importing it; any name works, a dot import too.
Its components can still take and return other packages' types: a templ
component, a templUI component, a `templ.Component` from anywhere is a
`ui.Node`.
