# Getting started

Create a web project and run it:

```sh
vuka new shop
cd shop
vuka run -watch .
```

## What's where

- `main.go` constructs the app with `web.New(pages.Routes(), …)`.
- `pages/index.vuka.html` renders `/`.
- `pages/_layout.vuka.html` supplies the document and wraps descendants.
- `pages/pets/[id].vuka.html` renders `/pets/{id}`.
- `pages/route.go` is generated; edit source files, not this table.
- Logic and services live in `.go` or `.vuka`, with no markup.

A page is a Go header with exactly one struct followed by markup:

```html
package pages

type Index struct { Name string }
<main>
  <h1>Hello, {Name}!</h1>
  <a href="/pets/7">A pet</a>
</main>
```

Exported fields are props, populated from matching path segments or query
parameters. Unexported fields hold tracked server state; provided types are
dependencies. Browser state uses `ui.State[T]` and assignments in `on…`.

## Wire the app

```go
// main.go
package main

import (
    "log"
    "example.com/app/pages"
    "github.com/vuka-lang/web"
)

func main() {
    app := web.New(pages.Routes())
    log.Fatal(app.Run(""))
}
```

Use your actual module path in place of `example.com/app`. Register services
with `app.Service(NewX)` before building or running the app. See
[typed parameters](/web/parameters) for form and service arguments.

## Build and test

```sh
vuka test ./...
vuka build
```

`vuka gen` writes the generated Go when you need an on-disk Go build.
[Deploy](/web/deployment) the resulting binary with production configuration.
