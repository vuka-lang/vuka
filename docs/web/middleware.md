# Middleware

Middleware is ordinary Go logic around HTTP. Put subtree middleware in
`pages/middleware.go` or a descendant folder's `middleware.go`.

```go
package pages

import (
    "log"
    "net/http"
)

func Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next.ServeHTTP(w, r)
    })
}
```

A constructor form can receive services and return `web.Middleware`. Root
middleware wraps child middleware. Guards and layout loading then run before
binding, mount and handlers. App-wide middleware can be passed as
`web.Use(mw)` to `web.New(pages.Routes(), …)`.

## A guard or middleware?

Use a folder's `Guard.Load` for typed resource and permission checks that
return a web error. Use middleware for HTTP concerns such as headers,
logging, compression and rate limits. Not calling next short-circuits the request.

## Live sessions

HTTP middleware wraps the initial request and socket join, not every event
on an established socket. Protected event handlers must authorize actions with
the current identity. Do not infer ongoing permission from a successful join.

## Decorators

Language-level call decorators still apply to logic functions and handlers.
They see typed arguments and results; HTTP middleware sees request and response.
Choose the layer that owns the check. See [Decorators](/features/decorators).
