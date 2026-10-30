# CORS and Token APIs

[[toc]]

## Introduction

An app's pages sign visitors in with a session cookie, and the app refuses
cross-origin requests that change state, so another site can't use that
cookie (see [CSRF](/web/auth#csrf)). A JSON API for a mobile app, a shop on
another domain or a partner's script is different: its callers live on
other origins and sign in with a bearer token. Two declarations serve it:

```go
package main

import (
	"log"
	"time"

	"github.com/vuka-lang/web/v2"
)

func main() {
	app := web.New(
		web.TokenOnly("/api/"),
		web.CORS("/api/", web.CORSConfig{
			Origins: []string{"https://shop.example"},
			Methods: []string{"GET", "POST", "DELETE"},
			MaxAge:  time.Hour,
		}),
	)
	log.Fatal(app.Run(""))
}
```

- `web.TokenOnly` makes `/api/` a token API: cookies are ignored there and
  the cross-origin check is off.
- `web.CORS` lets the shop's pages read the API's answers, and answers the
  browser's preflights.

Both take a path prefix: `"/api/"` covers `/api` and every path under it.

## Token-Only Routes {#token-only}

`web.TokenOnly(prefix)` takes the `Cookie` header off every request under
the prefix before any middleware runs, and drops the `Set-Cookie` headers of
its answers. A request there is signed in by its `Authorization: Bearer …`
header (the auth module's tokens, or [JWTs](/web/auth)) or not at all:

| Request to `/api/orders` | Signed in as |
|---|---|
| `Authorization: Bearer <token>` | the token's user |
| the browser's session cookie only | nobody: `401` from a gate |
| a cross-site `POST` with a cookie | nobody, and the cookie never reaches the handler |

With no cookie on the request, there is nothing for another site to abuse,
so the [cross-origin check](/web/auth#csrf) is off under the prefix: another
origin's script may `POST` a sign-in (no token yet) or any call with its
token. The two go together and can't be had apart: the check is never off
for a request that still carries cookies. Everywhere else the check stands,
and a path that only looks like it is under the prefix (`/api/../admin`,
`/api%2F..%2Fadmin`) is judged by where the router sends it.

The auth module's token endpoint can be token-only too, so a script on
another origin gets its token there:

<!-- go:fragment -->
```go
_ = web.New(
	web.TokenOnly("/api/"),
	web.TokenOnly("/auth/token"),
	web.CORS("/api/", web.CORSConfig{Origins: []string{"https://shop.example"}}),
	web.CORS("/auth/token", web.CORSConfig{Origins: []string{"https://shop.example"}}),
	// auth.Module(…)
)
```

> [!WARNING]
> Keep pages, forms and live views off a token-only prefix: they need the
> session cookie, and their forms the CSRF cookie.

## CORS {#cors}

A browser lets a page read an answer from another origin only when the
answer says so. `web.CORS(prefix, config)` says so for the routes under the
prefix:

- A **preflight** (an `OPTIONS` request with
  `Access-Control-Request-Method`, which a browser sends before a request
  with a JSON body or an `Authorization` header) is answered `204` at once,
  before routing, middleware, gates and the cross-origin check. For a
  listed origin it names the methods, headers and how long to keep the
  answer; for any other it says nothing, and the browser stops there.
- **Every answer** under the prefix carries `Vary: Origin`, and for a listed
  origin `Access-Control-Allow-Origin` (the origin itself, or `*` with
  `web.AnyOrigin`) and the exposed headers.

| `web.CORSConfig` | | Default |
|---|---|---|
| `Origins` | the origins allowed, exactly as a browser sends them (`https://shop.example`, `http://localhost:5173`, `capacitor://localhost`), or `web.AnyOrigin` alone | required |
| `Methods` | the methods a request may use | `GET`, `HEAD`, `POST` |
| `Headers` | the request headers allowed besides the safelisted ones; `web.AnyHeader` for any | `Content-Type`, and `Authorization` under a token-only prefix |
| `ExposeHeaders` | the response headers a script may read besides the safelisted ones | none |
| `Credentials` | let the origins send cookies | `false` |
| `MaxAge` | how long a browser keeps a preflight's answer; negative for never | 5 minutes |

Origins match exactly, without case: `https://shop.example` doesn't let
`https://shop.example.evil.test` or `http://shop.example` in. When several
prefixes cover a path, the most specific wins.

CORS is not access control. An origin not listed still reaches the route;
its browser only keeps the answer from the page. Gate the routes as any
others, and keep the cross-origin check, or `web.TokenOnly`, for what
changes state.

### Authorization and Wildcard Headers {#wildcard-headers}

`Access-Control-Allow-Headers: *` doesn't cover `Authorization`: browsers
read the `*` as every header but that one, and on a request with
credentials as a header named `*`. So `Authorization` is listed by name: by
default under a token-only prefix, or in `Headers`. `web.AnyHeader` answers
a preflight with the headers it asked for, `Authorization` included, rather
than with a literal `*`.

<!-- go:fragment -->
```go
_ = web.CORS("/api/", web.CORSConfig{
	Origins: []string{"https://shop.example"},
	Headers: []string{"Authorization", "Content-Type", "X-Request-Id"},
})
```

### Credentials {#credentials}

`Credentials: true` lets the listed origins send the visitor's cookies, for
an app's own frontend on another domain that shares its session. The
browser then needs `Access-Control-Allow-Credentials: true`, which CORS
sends, and the app's cross-origin check lets these origins' requests
through under the prefix, as [`web.CSRF`](/web/auth#csrf)'s trusted
origins do everywhere. List only origins you control.

Build refuses `Credentials` with `web.AnyOrigin` (browsers would refuse it
too), and under a token-only prefix, where cookies are taken off. See
[E0430](/errors/E0430).

## CORS on Its Own {#cors-alone}

Without `web.TokenOnly`, a cross-origin `GET` is answered with the CORS
headers, but a cross-origin `POST` is still refused by the cross-origin
check, which can't tell a script's token call from a cookie riding along.
A request carrying `Authorization: Bearer` is let through (a browser never
adds one by itself), but a sign-in has no token yet. Declare the prefix
token-only, or list the origins with `Credentials` when they share the
session.
