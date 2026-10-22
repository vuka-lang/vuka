# Dependency injection

Services are ordinary Go types with constructors in logic files. Register
them explicitly on the app. Constructor parameters describe dependencies.

```go
// services/store.go
package services

type Store struct { names []string }

func NewStore() *Store { return &Store{} }

func (s *Store) All() []string { return append([]string(nil), s.names...) }
```

In `main.go`, construct `web.New(pages.Routes(), …)` and call
`app.Service(services.NewStore)` before running the app. A constructor may
return `T` or `(T, error)`; construction errors stop startup.

## Parameters, by type

| Parameter | Source |
|---|---|
| `context.Context` | Request or live-session context |
| Unprovided form struct or pointer | Submitted form or JSON, validated |
| `url.Values` | Raw submitted fields |
| Scalar | Matching path segment or event value |
| `*http.Request`, `http.ResponseWriter` | Current request |
| `auth.User` | Signed-in visitor |
| Registered service type | App container |

The same rule applies to hooks, page handlers and endpoint functions. A type
that cannot be supplied is a startup error; it is not silently zero-filled.

## A page using a service

```html
package pages

import (
    "github.com/vuka-lang/ui"
    "context"
    "example.com/app/services"
)

type Index struct {
    store *services.Store
    names []string
}

@ui.Mount
func (p *Index) mount(ctx context.Context, store *services.Store) error {
    p.names = store.All()
    return nil
}
<main>
  for _, name := range names {
    <p>{name}</p>
  }
</main>
```

The provided field is injected, not tracked state. The `names` field is tracked
server state. The example's hook can instead use `p.store` when no service
parameter is needed.

## Lifecycle

Services are app-scoped singletons. Keep request data in parameters, not shared
service fields. Lifecycle Start and Stop methods manage external resources;
context cancellation should stop background work. Tests should create a fresh app
and register fake constructors in place of real services.

## Build errors

Build validates the dependency graph and reports missing types, cycles and
unfillable hook or handler parameters before serving requests. Avoid hidden
global registration: the app's startup should show the services it owns.
