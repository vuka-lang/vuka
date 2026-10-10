---
layout: home

hero:
  name: Vuka
  text: Go, with a few things more
  tagline: Result and Option, the ? operator, pattern matching, overloading, decorators, statics and JSX components. Vuka transpiles to plain Go and builds with the go command you already have.
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
    details: A .vuka file is Go plus a handful of additions. Every Go function, type and library works as it is — from the standard library, from go get, or from your own .go files.
    link: /guide/using-go
    linkText: Using Go code
  - title: Errors as values, with less ceremony
    details: Result and Option, ? to pass an error on, and exhaustive match. Go's (T, error) works with all of it.
  - title: Decorators, the Python way
    details: "decorator logged(c) { … } decorates any function, method or type. Retries, caches, auth guards and dependency injection, in a few lines."
  - title: Statics and Self
    details: "User.New(\"ada\"), User.Max — statics that belong to a type, and generic bases whose Self is the type embedding them, compiled to ordinary Go."
    link: /features/statics
    linkText: Statics and Self
  - title: Components with JSX
    details: "Markup is an expression and a component is a function. Rendered by templ, so .templ files and templ libraries work side by side with no generate step."
    link: /features/components
    linkText: Components and JSX
  - title: Plain Go comes out
    details: No runtime magic, no reflection where a type will do. vuka explain shows exactly what each line becomes, and build/ holds a plain Go module any Go tool can build.
  - title: Full editor support
    details: vuka lsp puts gopls behind a proxy — completion, hover, go to definition, rename and diagnostics in .vuka files, and Go files that see Vuka code.
  - title: A web framework
    details: "A library in Vuka: routes declared with decorators, parameters bound by name, services injected, results as JSON or a templ / JSX page — and live components that update the page over a WebSocket."
    link: /web/
    linkText: Web
  - title: A Django-style ORM
    details: A library in Vuka. Models declared with field attributes, queries written with field references the compiler checks — Post.Objects.Filter(Post.Title.Contains("Go")) — Result and Option from every terminal, and Go migrations.
    link: /orm/
    linkText: ORM
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
