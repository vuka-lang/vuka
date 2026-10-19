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

::: go
```go
package main

import (
	"net/http"

	"github.com/vuka-lang/ui"
)

var greeting = ui.Template(`
<section>
	<h1>Hello, {{0}}!</h1>
	{{1}}
</section>`)

func Greeting(name string, children ui.Node) ui.Node {
	return greeting.Render(name, children)
}

var welcome = ui.Template(`<p>Welcome.</p>`)

func main() {
	http.Handle("/", ui.Handler(func(r *http.Request) (ui.Node, error) {
		return Greeting(r.URL.Query().Get("name"), welcome.Render()), nil
	}))
	http.ListenAndServe(":8080", nil)
}
```
:::

::: vuka
```sh
vuka run .
```
:::

::: go
```sh
go run .
```
:::

::: vuka
The import is what makes `<section>` a `ui.El`: a file renders its JSX with
the [JSX target](/features/jsx#jsx-targets) it imports, and ui is one.
:::

::: go
A template is parsed once, when the package initializes, and each `Render`
fills its holes. It renders the same nodes JSX does, so a page built from
templates is live, diffed and patched the same way.
:::

## Templates

JSX is how a `.vuka` file writes markup. Go code writes it with
`ui.Template`: HTML with numbered holes, `{{0}}`, `{{1}}`, …, that `Render`
fills in order. Declare templates at package level:

```go
var orderRow = ui.Template(`
<tr key="{{0}}" class="{{1}}">
	<td>{{2}}</td>
	<td class="tabular-nums">{{3}}</td>
	<td><button onClick="{{4}}">Cancel</button></td>
</tr>`)

func (p *Orders) row(o Order) ui.Node {
	return orderRow.Render(o.ID, ui.Class{"row": true, "late": o.Late}, o.Ref, money(o.Total),
		func() { p.Cancel(o.ID) })
}
```

What a hole takes depends on where it is:

| Place | Takes |
|---|---|
| a child, between tags | a `ui.Node`, `[]ui.Node`, a string or number (escaped), an error (its message), `nil`, a `ui.State`, a stateful component (`&Counter{…}`, `ui.Key(k, &Counter{…})`), an island (`ui.Mount(Chart{…}, ui.Idle())`) |
| text, in `title`, `textarea` or `script` | a string or number |
| an attribute's value, `class="{{0}}"` | a string, number, bool (false leaves it out), `nil`, a `ui.Class` or `ui.Style`, a `ui.State`; an `on…` attribute takes a handler: a func, `ui.On(…)`, JS commands |
| part of a value, `href="/orders/{{0}}"` | a string or number |
| an attribute set, `<input {{0}}>` | a `ui.Attr`, `[]ui.Attr`, `templ.Attributes` |

A value of the wrong kind fails the render with the template's line and the
hole, and [`vukavet`](/tools/vukavet) reports it from the source; a
malformed template panics when the package initializes. Loops and
conditions are Go:

```go
var list = ui.Template(`<table><tbody>{{0}}</tbody></table>{{1}}`)
var empty = ui.Template(`<p>No orders yet.</p>`)

func (p *Orders) Render() ui.Node {
	return list.Render(ui.Each(p.rows, p.row), ui.If(len(p.rows) == 0, empty.Render()))
}
```

`ui.Each(items, fn)` is a list, its rows diffed by their `key` under a live
session; `ui.If(cond, node)` is the node or nothing; a `[]ui.Node` works
too. Client state is declared where it is used: `open := ui.UseState(false)`.

## Next

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
