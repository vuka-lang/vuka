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

```sh
vuka run .
```

The import is what makes `<section>` a `ui.El`: a file renders its JSX with
the [JSX target](/features/jsx#jsx-targets) it imports, and ui is one.

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
