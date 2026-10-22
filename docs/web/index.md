# Web

Build pages as `.vuka.html` shape files under `pages/`. The folder tree
supplies URLs and layouts; the compiler writes `pages/route.go`. Logic,
services and API endpoints remain ordinary `.go` or `.vuka` files.

## A first app

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
routes with `vuka gen`, or let `vuka run` build the overlay. Do not edit
`pages/route.go`.

```html
package pages

import "github.com/vuka-lang/ui/v2"

type Layout struct { Children ui.Node }
<html lang="en">
  <head><title>{ui.PageTitle(ctx)}</title></head>
  <body>{Children}</body>
</html>
```

Save as `pages/_layout.vuka.html`; it wraps every page below that folder.

```html
package pages

type Index struct { Name string }
<main><h1>Hello, {Name}!</h1></main>
```

Save as `pages/index.vuka.html` and open `/?name=Ada`.

## Services

Register constructors with `app.Service(NewX)`. Hook and handler parameters
are filled by type: context, form struct, request, signed-in user and services.
See [Dependency injection](/web/dependency-injection).

## What's in it

[Folder routes](/web/folder-routes), [Live pages](/web/live),
[Authentication](/web/auth), [Styles](/web/assets), [Islands](/web/islands),
[Mail](/web/mail), [Crons](/web/crons), [Testing](/web/testing) and
[Deployment](/web/deployment) share the same app and container.
