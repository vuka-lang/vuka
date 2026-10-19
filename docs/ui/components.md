# Components

A component is a struct whose fields are its props — itself a `ui.Node`,
its markup its `View` — or a function returning `ui.Node`; the
[JSX](/features/jsx) page has how tags bind to it, [templates](/ui/templates)
how a struct's markup is written in a `.vuka.html` file. This page is what ui does with the markup.

## Nodes

`ui.Node` is `templ.Component`: anything with
`Render(ctx context.Context, w io.Writer) error`. So a Vuka component is a
templ component and a templ component is a Vuka child, with nothing in
between. The nodes JSX builds (`*ui.Element`, text, groups, blocks) are also
a tree a [renderer](/ui/rendering#walking-a-tree) can walk.

| | |
|---|---|
| `ui.HTML(markup, args...)` | markup whose holes are labelled with the Go of their values, parsed once — what JSX and `.vuka.html` [templates](/ui/templates) compile to; see [markup in Go](/ui/#markup-in-go) |
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
Each is a struct whose fields are its props, and a `ui.Node` as it is: a
tag's attributes are its
fields, camelCase (`noPad` is `NoPad`), its children the `Children` field
and `<slot:header>` the `Header` one.

::: vuka
```html
<ui.Card title={ui.Text("Members")} noPad>
  <slot:header>
    <ui.Button variant={ui.Primary} icon={ui.Icon("plus")} on={c.New}>New</ui.Button>
  </slot:header>
  {table}
</ui.Card>
```
:::

::: go
```go
ui.Card{
	Title:    ui.Text("Members"),
	NoPad:    true,
	Header:   ui.Button{Variant: ui.Primary, Icon: ui.Icon("plus"), On: c.New, Children: ui.Text("New")},
	Children: table,
}
```
:::

| | |
|---|---|
| `ui.Button` | a button, or a link when `Href` is set; a click `On` handler spins it until its reply |
| `ui.Submit` | a form submit button (a `Button` of `type=submit`) |
| `ui.Actions(buttons...)` | a right-aligned row of buttons, for a card header or modal footer |
| `ui.Cancel(on, label...)` | a dismiss button for a modal footer |
| `ui.Card` | a bordered surface with an optional header (title left, actions right) and a body |
| `ui.Tile` / `ui.TileGrid(tiles...)` | a dashboard stat card (icon, big number, label) and a responsive row of them |
| `ui.Tabs` | a tab bar underlining the active tab; each `ui.Tab` may carry a count |
| `ui.Badge` | a small status pill |
| `ui.Alert` | an inline message box in four tones |
| `ui.Toast` / `ui.ToastContainer` | one notification, and the stack a live view renders them into |
| `ui.Modal` | a dialog over a backdrop — server-owned through `Open`/`OnClose`, or browser-only via `OpenModal` |
| `ui.Form`, `ui.Field`/`ui.Input`/`ui.Password`/`ui.Textarea`/`ui.Select`/`ui.Checkbox` | a live-wired form and its fields, errors given by the host |
| `ui.Table` | rows of type `T` as a table; presentational |
| `ui.DataTable` | a `Table` with a search box, sortable headers and a pager, driven from a live view's events |
| `ui.Image` | a lazy, reserved, off-thread-decoded `<img>` with a placeholder, or a backdrop for an `Overlay` |
| `ui.Icon(name)`, `ui.Spinner()` | an icon by name, and a loading spinner |

The ones taking arguments rather than fields — `Actions`, `Cancel`,
`TileGrid`, `Icon`, `Spinner` — are partials, called in a hole:
`{ui.Icon("plus")}`.

Tone and size are shared constants: `ui.Default`, `ui.Primary`, `ui.Success`,
`ui.Danger`, `ui.Warning`, `ui.Info`, `ui.Ghost`; `ui.SizeSm`, `ui.SizeMd`,
`ui.SizeLg`. Everything is styled with the `--ui-*`
[tokens](/ui/rendering), light and dark.

`Table` and `DataTable` are generic in the row type. Their type argument is
[inferred](/features/jsx#components) from the attributes — `columns` and
`rows` pin it — so a bare tag is enough:

::: vuka
```html
<ui.Table
  columns={cols}
  rows={d.rows}
  rowKey={func(t Task) any { return t.ID }}
  selectable
  rowSelected={func(t Task) bool { return d.picked(t.ID) }}
  onSelect={func(t Task) any { id := t.ID; return func(on bool) { d.pick(id, on) } }}
  onSelectAll={d.pickPage}
  allSelected={d.allPicked()}
/>
```
:::

::: go
```go
ui.Table[Task]{
	Columns:     cols,
	Rows:        d.rows,
	RowKey:      func(t Task) any { return t.ID },
	Selectable:  true,
	RowSelected: func(t Task) bool { return d.picked(t.ID) },
	OnSelect:    func(t Task) any { id := t.ID; return func(on bool) { d.pick(id, on) } },
	OnSelectAll: d.pickPage,
	AllSelected: d.allPicked(),
}
```
:::

A selection checkbox is a live field: it fires `onChange` with its new
checked state, so `OnSelect` returns a handler taking that `bool` and
`OnSelectAll` takes one directly — set the row to what the box now is rather
than toggling a server-side guess, which keeps the box and the server in sync
(see [field sync](/web/live)).

### How the kit is written

The kit is written as an app's components are: a `.go` file declares the
type, the `.vuka.html` beside it is its `View`
([templates](/ui/templates)), and the Go `vuka gen` compiles it to is
committed, so ui builds with plain `go build`.

```go
// card.go
type Card struct {
	Title    Node
	Header   Node
	NoPad    bool
	Class    string
	Children Node
}
```

```html
<!-- card.vuka.html -->
func (c Card) View()
<div class={withClass("vk-card", c.Class)}>
  if c.Title != nil || c.Header != nil {
    <div class="vk-card__header">
      <div class="vk-card__title">{c.Title}</div>
      if c.Header != nil {
        <div class="vk-card__header-actions">{c.Header}</div>
      }
    </div>
  }
  <div class="vk-card__body" class:vk-card__body--flush={c.NoPad}>{c.Children}</div>
</div>
```

Most of its templates name their receiver (`m.Icon`): in package ui, many
field names — `Icon`, `Value`, `Class`, `Ref`, `On`, `Page` — are also the
package's own names, and a template reading both (`m.Icon` and the function
`Icon("x")`) says which. The ones that don't, `badge.vuka.html` and `alert.vuka.html` among them,
read their fields by bare name, as an app's do.

The props types of earlier versions are deprecated aliases of the
components — `ui.CardProps` is `ui.Card`, `ui.TableProps[T]` is
`ui.Table[T]` — so a call of the former function, `ui.Card(ui.CardProps{…})`,
still compiles: it converts the literal to the component, a Node. A generic
one needs its type argument, `ui.Table[Row]{…}`; `vuka fix ui-structs`
writes both as literals. Tags are unchanged.

## Picking ui

A `.vuka` file uses ui by importing it; any name works, a dot import too.
Its components can still take and return other packages' types: a templ
component, a templUI component, a `templ.Component` from anywhere is a
`ui.Node`.
