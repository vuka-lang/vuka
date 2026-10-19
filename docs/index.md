---
layout: home

hero:
  name: Vuka
  text: Go, with a few things more
  tagline: Result and Option, the ? operator, pattern matching, enums, overloading, decorators, statics and JSX components. Vuka transpiles to plain Go and builds with the go command you already have.
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
  - title: Enums
    details: "enum Status { Draft, Sent, Paid } — variants with String, Parse, JSON and database methods, or variants carrying data, and a match that must handle every one."
    link: /features/enums
    linkText: Enums
  - title: Decorators, the Python way
    details: "decorator logged(c) { … } decorates any function, method or type. Retries, caches, auth guards and dependency injection, in a few lines."
  - title: Statics and Self
    details: "User.New(\"ada\"), User.Max — statics that belong to a type, and generic bases whose Self is the type embedding them, compiled to ordinary Go."
    link: /features/statics
    linkText: Statics and Self
  - title: Components with JSX or templates
    details: "Markup is JSX in a .vuka file, a .vuka.html template beside the Go, or ui.HTML in plain Go — all compile to the same calls. The UI library renders it with templ, so .templ files and templ libraries work side by side — and keeps stateful components live over a connection."
    link: /ui/
    linkText: UI
  - title: Plain Go comes out
    details: No runtime magic, no reflection where a type will do. vuka explain shows exactly what each line becomes, and build/ holds a plain Go module any Go tool can build.
  - title: Full editor support
    details: vuka lsp puts gopls behind a proxy — completion, hover, go to definition, rename and diagnostics in .vuka files, and Go files that see Vuka code.
  - title: A web framework
    details: "A library in Vuka: routes declared with decorators, parameters bound by name, services injected, results as JSON or a templ / JSX page — and live components that update the page over a WebSocket."
    link: /web/
    linkText: Web
  - title: A Django-style ORM
    details: A library in Vuka. Models declared with field attributes — a column's name, type, default and checks in one — custom value types, queries written with field references the compiler checks — Post.Objects.Filter(Post.Title.Contains("Go")) — Result and Option from every terminal, and Go migrations.
    link: /orm/
    linkText: ORM
---

## Try it

With Go 1.25 or newer:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
vuka new hello && cd hello
vuka run
```

## A taste

::: vuka
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
:::

<!-- go:context
type User struct{ Name string }
type Receipt struct{ ID string }
type NotFound struct{ ID int }

func (e *NotFound) Error() string { return fmt.Sprint("no user ", e.ID) }

func find(id int) (User, error) { return User{}, &NotFound{ID: id} }
func logged(c *vuka.Call)       { c.Next() }
func retry(n int) vuka.Decorator { return func(c *vuka.Call) { c.Next() } }
-->

::: go
```go
func lookup(arg string) (User, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return User{}, err
	}
	return find(id)
}

// Decorators are functions; vuka.Wrap puts them around a call.
var charge = vuka.Wrap(chargeOnce, vuka.FuncInfo{Name: "charge"}, retry(3), logged)

func chargeOnce(ctx context.Context, id int) (Receipt, error) {
	return Receipt{ID: fmt.Sprint("r-", id)}, nil
}

func show(arg string) {
	u, err := lookup(arg)
	var missing *NotFound
	switch {
	case err == nil:
		fmt.Println(u.Name)
	case errors.As(err, &missing):
		fmt.Println("missing user", missing.ID)
	case errors.Is(err, strconv.ErrSyntax):
		fmt.Println("not a number:", arg)
	default:
		fmt.Println("error:", err)
	}
}
```
:::

## Built on Go

Vuka stands on the work of the Go team. It doesn't have a parser or type checker
of its own for Go: it finds its own additions token by token, rewrites them into
Go, and leaves everything else to `go/parser`, `go/types` and the `go` command of
the toolchain you have installed. The editor support is
[gopls](https://go.dev/gopls) behind a proxy. What Vuka adds is small on purpose;
the language underneath is [Go](https://go.dev), and the credit for it belongs to
the Go Authors. See [Credits](/credits).
