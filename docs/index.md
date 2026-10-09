---
layout: home

hero:
  name: Vuka
  text: Go, with a few things more
  tagline: Result and Option, the ? operator, pattern matching, overloading, decorators and statics. Vuka transpiles to plain Go and builds with the go command you already have.
  image:
    src: /logo.svg
    alt: Vuka
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: What Vuka adds
      link: /features/result-option
    - theme: alt
      text: GitHub
      link: https://github.com/vuka-lang/vuka

features:
  - title: It is Go
    details: A .vuka file is Go plus a handful of additions. Everything else is read by Go's own parser and type checker, so a new Go release's syntax works in Vuka the day it ships.
  - title: Errors as values, with less ceremony
    details: Result and Option, ? to pass an error on, and exhaustive match. Go's (T, error) works with all of it.
  - title: Decorators, the Python way
    details: "decorator logged(c) { … } decorates any function, method or type. Retries, caches, auth guards and dependency injection, in a few lines."
  - title: Statics and Self
    details: User.Objects.All(ctx) and u.Save(ctx) — a Django-style model API, compiled to ordinary Go.
  - title: Plain Go comes out
    details: No runtime magic, no reflection where a type will do. vuka explain shows exactly what each line becomes, and build/ holds a plain Go module any Go tool can build.
  - title: Full editor support
    details: vuka lsp puts gopls behind a proxy — completion, hover, go to definition, rename and diagnostics in .vuka files, and Go files that see Vuka code.
---

## A taste

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

## Built on Go

Vuka stands on the work of the Go team. It doesn't have a parser or type checker
of its own for Go: it finds its own additions token by token, rewrites them into
Go, and leaves everything else to `go/parser`, `go/types` and the `go` command of
the toolchain you have installed. The editor support is
[gopls](https://go.dev/gopls) behind a proxy. What Vuka adds is small on purpose;
the language underneath is [Go](https://go.dev), and the credit for it belongs to
the Go Authors. See [Credits](/credits).
