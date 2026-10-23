# Your First Web App

[[toc]]

## Introduction

You build pages as `.vuka.html` shape files under `pages/`. The folder tree
supplies the URLs and layouts, and the compiler writes `pages/route.go` for
you.

Logic, services and API endpoints remain ordinary `.go` or `.vuka` files.
This page walks through the smallest app: a layout, a page and the `main.go`
that serves them.

## The Project Layout

A first app has a `main.go` beside a `pages/` tree:

```text
main.go
pages/
  _layout.vuka.html
  index.vuka.html
  pets/
    [id].vuka.html
  api/
    pets.go
```

## Starting the App {#a-first-app}

In `main.go`, construct the app from the generated routes and run it:

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

The `pages` import is your app's package, not a library package. Generate
the routes with `vuka gen`, or let `vuka run` build the overlay.

> [!WARNING]
> Do not edit `pages/route.go`.

## Adding a Layout

Save this as `pages/_layout.vuka.html`; it wraps every page below that
folder:

```html
<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

type Layout struct { Children ui.Node }
</script>

<html lang="en">
  <head><title>{ui.PageTitle(ctx)}</title></head>
  <body>{Children}</body>
</html>
```

## Adding a Page

Save this as `pages/index.vuka.html`, then open `/?name=Ada`:

```html
<script lang="go">
package pages

type Index struct { Name string }
</script>

<main><h1>Hello, {Name}!</h1></main>
```

## Registering Services {#services}

You register constructors with `app.Service(NewX)`. Hook and handler
parameters are filled by type: the context, a form struct, the request, the
signed-in user and services. See [Dependency Injection](/web/dependency-injection).

## Where to Go Next {#what-s-in-it}

[Folder Routes](/web/folder-routes), [Live Pages](/web/live),
[Authentication](/web/auth), [Styles & Scripts](/web/assets),
[Islands](/web/islands), [Mail](/web/mail), [Crons](/web/crons),
[Testing Web Apps](/web/testing) and [Deployment](/web/deployment) all share
the same app and container.
