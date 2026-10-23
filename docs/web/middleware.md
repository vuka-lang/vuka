# Middleware

[[toc]]

## Introduction

Middleware is ordinary Go logic around HTTP. You reach for it for concerns
that belong to the request and response rather than to a page: headers,
logging, compression and rate limits.

It lives in the page tree beside the pages it wraps, or is passed to the app
for every request.

## Writing Middleware

Put subtree middleware in `pages/middleware.go`, or in a descendant folder's
`middleware.go`. This one logs each request before passing it on:

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

Not calling `next` short-circuits the request.

### Middleware With Services

A constructor form can receive services and return `web.Middleware`. See
[Dependency Injection](/web/dependency-injection).

### The Order It Runs In

Root middleware wraps child middleware. Guards and layout loading then run
before binding, mount and handlers.

### App-Wide Middleware

App-wide middleware can be passed as `web.Use(mw)` to
`web.New(pages.Routes(), …)`.

## Choosing a Guard or Middleware {#a-guard-or-middleware}

Use a folder's `Guard.Load` for typed resource and permission checks that
return a web error; see [Guards](/web/folder-routes#guards). Use middleware
for HTTP concerns such as headers, logging, compression and rate limits.

## Middleware and Live Sessions {#live-sessions}

HTTP middleware wraps the initial request and the socket join, not every
event on an established socket. Protected event handlers must authorize
actions with the current identity.

> [!WARNING]
> Do not infer ongoing permission from a successful join.

## Middleware and Decorators {#decorators}

Language-level call decorators still apply to logic functions and handlers.
They see typed arguments and results; HTTP middleware sees the request and
response. Choose the layer that owns the check. See
[Decorators](/features/decorators).
