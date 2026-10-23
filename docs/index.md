---
layout: home

hero:
  name: Vuka
  text: Go, with a few things more
  tagline: Result, the ? operator, match, enums and decorators, plus a web framework where pages are files and the UI stays live. It compiles to plain Go.
  image:
    src: /logo.svg
    alt: Vuka
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: Your first web app
      link: /web/
    - theme: alt
      text: GitHub
      link: https://github.com/vuka-lang/vuka

features:
  - title: It is Go
    details: A .vuka file is Go plus a handful of additions. Every Go function, type and library works as it is, from the standard library, go get, or your own .go files.
    link: /guide/using-go
    linkText: Using Go code
  - title: Errors as values
    details: Result and Option, ? to pass an error on, enums with exhaustive match. Go's (T, error) works with all of it.
    link: /features/result-option
    linkText: Result and Option
  - title: Decorators and statics
    details: "@retry(3) on a function, User.New(\"ada\") on a type: retries, caches, guards and constructors in a few lines, compiled to ordinary Go."
    link: /features/decorators
    linkText: Decorators
  - title: Pages are files
    details: Folders under pages/ are your routes, with [id] parameters, (group) folders and layouts that stay put while you navigate. Links between pages are typed.
    link: /web/folder-routes
    linkText: Folder routes
  - title: Live UI without a JS build
    details: A .vuka.html shape is one struct and its markup. Server state, browser state and Vue or React islands share one form, and events patch the page in place.
    link: /ui/
    linkText: Shape files
  - title: An ORM
    details: Models declared with field attributes, queries the compiler checks, such as Post.Objects.Filter(Post.Title.Contains("Go")), and migrations written in Go.
    link: /orm/
    linkText: Database
---

## Try it

With Go 1.25 or newer:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
vuka new hello && cd hello
vuka run
```

For your editor, install the Vuka extension from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=vuka-lang.vuka) or [Open VSX](https://open-vsx.org/extension/vuka-lang/vuka). Other editors use `vuka lsp`; see [Editors](/tools/editors).

## A taste

Errors as values, decorators and `match`, in a `.vuka` file:

```vuka
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?          // Go's (T, error) works directly
	u := find(id)?
	return Ok(u)
}

@retry(3)
@logged
func charge(ctx context.Context, id int) (Receipt, error) { … }

match lookup(arg) {
case Ok(u):
	fmt.Println(u.Name)
case Err(&NotFound{ID: id}):
	fmt.Println("missing user", id)
case Err(e) if errors.Is(e, strconv.ErrSyntax):
	fmt.Println("not a number:", arg)
case Err(e):
	fmt.Println("error:", e)
}
```

A live page, `pages/counter/index.vuka.html`, served at `/counter`. Each click runs `Add` on the server and patches the count in place:

```html
package counter

import "github.com/vuka-lang/ui/v2"

type Index struct {
	ui.Live
	count int
}

func (p *Index) Add() { p.count++ }
<main>
  <h1>{count}</h1>
  <button onClick={Add}>Add one</button>
</main>
```

## Built on Go

Vuka stands on the work of the Go team. It doesn't have a parser or type checker
of its own for Go: it finds its own additions token by token, rewrites them into
Go, and leaves everything else to `go/parser`, `go/types` and the `go` command of
the toolchain you have installed. The editor support is
[gopls](https://go.dev/gopls) behind a proxy. What Vuka adds is small on purpose;
the language underneath is [Go](https://go.dev), and the credit for it belongs to
the Go Authors. See [Credits](/credits).
