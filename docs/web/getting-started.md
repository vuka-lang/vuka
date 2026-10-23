# Directory Structure

[[toc]]

## Introduction

A web project keeps its pages in a `pages/` tree, its logic in ordinary Go
or `.vuka` files, and one `main.go` that wires them together. This page
shows what each file is for, then how to create, run, test and build a
project.

## Creating a Project

Create a web project and run it:

```sh
vuka new shop
cd shop
vuka run -watch .
```

## What's Where

Each file in a new project has one job:

- `main.go` constructs the app with `web.New(pages.Routes(), …)`.
- `pages/index.vuka.html` renders `/`.
- `pages/_layout.vuka.html` supplies the document and wraps descendants.
- `pages/pets/[id].vuka.html` renders `/pets/{id}`.
- `pages/route.go` is generated; edit source files, not this table.
- Logic and services live in `.go` or `.vuka`, with no markup.

## The Shape of a Page

A page is a Go header with exactly one struct, followed by markup:

```html
package pages

type Index struct { Name string }
<main>
  <h1>Hello, {Name}!</h1>
  <a href="/pets/7">A pet</a>
</main>
```

Exported fields are props, populated from matching path segments or query
parameters. Unexported fields hold tracked server state, and provided types
are dependencies. Browser state uses `ui.State[T]` and assignments in `on…`.

## Wiring the App {#wire-the-app}

In `main.go`, construct the app from the generated routes:

```go
// main.go
package main

import (
    "log"
    "example.com/app/pages"
    "github.com/vuka-lang/web/v2"
)

func main() {
    app := web.New(pages.Routes())
    log.Fatal(app.Run(""))
}
```

Use your actual module path in place of `example.com/app`. Register services
with `app.Service(NewX)` before building or running the app. See
[Parameters & Validation](/web/parameters) for form and service arguments.

## Testing and Building {#build-and-test}

Run the tests and build the binary:

```sh
vuka test ./...
vuka build
```

`vuka gen` writes the generated Go when you need an on-disk Go build.
[Deploy](/web/deployment) the resulting binary with production configuration.
