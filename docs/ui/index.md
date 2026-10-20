# UI

[`github.com/vuka-lang/ui`](https://github.com/vuka-lang/ui) is the UI
library for Vuka: what [JSX](/features/jsx) in a `.vuka` file renders with.
The language reads the markup; ui gives it meaning — a node type, an HTML
renderer built on [templ](https://templ.guide), stateful components and the
live sessions that keep them running for a connected page.

| Package | |
|---|---|
| `github.com/vuka-lang/ui` | `ui.Node` (which is `templ.Component`), elements and text, rendering (`String`, `Write`, `Handler`, `Walk`), stateful components (`ui.Live`, `ui.On`, `ui.Event`, `ui.Assign`) |
| `github.com/vuka-lang/ui/live` | sessions: stateful components alive for one connected page, and the wire protocol a transport speaks |
| `github.com/vuka-lang/ui/live/livetest` | a reference client in Go, for tests |
| `github.com/vuka-lang/ui/term` | the same components as terminal text |
| `github.com/vuka-lang/ui/templx` | templ interop: children for templ components, the components of `.templ` files |

## Install

```sh
go get github.com/vuka-lang/ui
```

ui requires templ, so a module using it builds with Go 1.25 or later. The
language itself doesn't: a module without JSX doesn't need ui at all.

## A first component

::: vuka
```vuka
package main

import (
	"net/http"

	"github.com/vuka-lang/ui"
)

func Greeting(name string, children ui.Node) ui.Node {
	return <section>
		<h1>Hello, {name}!</h1>
		{children}
	</section>
}

func main() {
	http.Handle("/", ui.Handler(func(r *http.Request) (ui.Node, error) {
		return <Greeting name={r.URL.Query().Get("name")}><p>Welcome.</p></Greeting>, nil
	}))
	http.ListenAndServe(":8080", nil)
}
```
:::

<!-- go:context
func (g Greeting) Render(ctx context.Context, w io.Writer) error { return g.View().Render(ctx, w) }

func (g Greeting) View() ui.Node {
	return ui.HTML(`<section><h1>Hello, {g.Name}!</h1>{g.Children}</section>`, g.Name, g.Children)
}
-->

::: go
A component is a struct, its fields its props, and its markup a
[template](/ui/templates) beside it:

```go
// greeting.go
package main

import (
	"net/http"

	"github.com/vuka-lang/ui"
)

type Greeting struct {
	Name     string
	Children ui.Node
}

func main() {
	http.Handle("/", ui.Handler(func(r *http.Request) (ui.Node, error) {
		return Greeting{Name: r.URL.Query().Get("name"), Children: ui.HTML(`<p>Welcome.</p>`)}, nil
	}))
	http.ListenAndServe(":8080", nil)
}
```

```html
<!-- greeting.vuka.html -->
package main

<section>
  <h1>Hello, {Name}!</h1>
  {Children}
</section>
```
:::

::: vuka
```sh
vuka run .
```
:::

::: go
```sh
go tool vuka run .   # or: go tool vuka gen && go run .
```
:::

::: vuka
The import is what makes `<section>` a `ui.El`: a file renders its JSX with
the [JSX target](/features/jsx#jsx-targets) it imports, and ui is one.
:::

::: go
The struct is itself a `ui.Node`: `greeting.vuka.html` is its `View`, its
fields read by bare name, compiled to Go by the `vuka` command (`vuka gen`
writes it as `greeting_html.go`, for a plain `go build`). Without the
command, markup is written inline with `ui.HTML` (below).
:::

## Markup in Go

<!-- go:context
type Order struct {
	ID    int
	Ref   string
	Total int
	Late  bool
}

type Orders struct {
	ui.Live
	rows []Order
}

func (p *Orders) Cancel(id int) {}

func money(cents int) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }
-->

JSX is how a `.vuka` file writes markup, and a [`.vuka.html`
template](/ui/templates) is how an app built with the `vuka` command keeps
it beside its Go. Plain Go writes it with `ui.HTML`: HTML whose holes are
labelled with the Go expression of their value, the values after it in the
order the labels first appear. A label used twice is one value.

```go
func (p *Orders) row(o Order) ui.Node {
	cancel := func() { p.Cancel(o.ID) }
	return ui.HTML(`
<tr key={o.ID} class="row" class:late={o.Late}>
	<td>{o.Ref}</td>
	<td class="tabular-nums">{money(o.Total)}</td>
	<td><button onClick={cancel}>Cancel</button></td>
</tr>`, o.ID, o.Late, o.Ref, money(o.Total), cancel)
}
```

What a hole takes depends on where it is:

| Place | Takes |
|---|---|
| a child, `<td>{x}</td>` | a `ui.Node`, `[]ui.Node`, a string or number (escaped), an error (its message), `nil`, a `ui.State` (kept current), a stateful component (`&Counter{…}`, `ui.Key(k, &Counter{…})`), an island (`Chart{…}`, `ui.Mount(Chart{…}, ui.Idle())`) |
| text, `<title>{x}</title>` (`title`, `textarea`) | a string or number |
| an attribute's value, `class={x}` or `class="{x}"` | a string, number, bool (false leaves it out, true writes it bare), `nil`, a `ui.Class`, `ui.Classes` or `ui.Style` on `class` and `style`, a `ui.State`; an `on…` attribute takes a handler: a func, `ui.On(…)`, JS commands |
| part of a value, `href="/orders/{id}"` | a string or number |
| a toggled class, `class:late={on}` | a bool or a `ui.State[bool]` |
| an attribute set, `<input {attrs...}>` | a `ui.Attr`, `[]ui.Attr`, `templ.Attributes` (`ui.BindAttr` gives one) |

<span v-pre>`{{`</span> is a literal brace and `{/* … */}` a comment; inside `script` and
`style`, braces are text. The markup is strict — every element closed, void
elements and `<x/>` aside — and text follows JSX's whitespace rule, so
indentation adds nothing.

A value of the wrong kind, or malformed markup, fails the render with the
call's line and the hole. [`vukavet`](/tools/vukavet) checks the values
from the source: one per label ([E0127](/errors/E0127)), each written as
its label ([E0132](/errors/E0132)), each of a kind its place takes
([E0128](/errors/E0128)). A value too long to be a readable label goes in
a variable first, as `cancel` does above.

Loops and conditions are Go:

```go
func (p *Orders) Render() ui.Node {
	rows := ui.Each(p.rows, p.row)
	empty := ui.If(len(p.rows) == 0, ui.HTML(`<p>No orders yet.</p>`))
	return ui.HTML(`<table><tbody>{rows}</tbody></table>{empty}`, rows, empty)
}
```

`ui.Each(items, fn)` is a list, its rows diffed by their `key` under a live
session; `ui.If(cond, node)` is the node or nothing; a `[]ui.Node` built in
a loop works too. Client state is declared where it is used:
`open := ui.UseState(false)`.

## Next

- [Templates](/ui/templates): `.vuka.html` files — markup beside the Go.
- [Components](/ui/components): elements, attributes, children, errors.
- [Stateful components](/ui/stateful): props, state, events, messages.
- [Rendering](/ui/rendering): HTML, HTTP, walking a tree, the terminal.
- [templ interop](/ui/templ): `.templ` files beside `.vuka` files.
- [Live sessions](/ui/live): a page's components kept alive over a connection.
- [Testing](/ui/testing): the reference client.
- [Live protocol](/reference/live-protocol): the session API and the wire, for transport authors.
- [Live components on the web](/web/live): the web framework serves live pages.

## Coming from v0.9

The UI layer lived in the language's runtime until Vuka v0.10.0. `vuka fix
ui` moves a module over: `vuka.Node` → `ui.Node`, `vuka.Live` → `ui.Live`
(and every other UI name), `github.com/vuka-lang/vuka/live` →
`github.com/vuka-lang/ui/live` (`term`, `templx` too), `vuka.TemplComponents`
→ `templx.Components`, adds the ui import where JSX is used, and requires ui.
