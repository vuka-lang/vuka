# Deployment

[[toc]]

## Introduction

`vuka build` makes one binary that serves your whole app. This page covers
how the app runs and shuts down, the environment it reads in production, the
security headers it sends, and what changes behind a proxy or with several
replicas.

It ends with the router seam, for when you want another router than the
standard library's.

## Running the App {#running}

The app serves itself, or hands you an `http.Handler` to serve elsewhere:

| | |
|---|---|
| `app.Run(addr)` | builds, starts the services, serves `addr` until SIGINT or SIGTERM, then shuts down gracefully (10s for requests in flight) and stops the services |
| `app.Run("")` | listens on `:$PORT`, else `:8080` |
| `app.Handler()` | the app as an `http.Handler`, to serve or mount anywhere; panics when Build fails |
| `app.Start(ctx)`, `app.Stop(ctx)` | the services' [lifecycle](/web/dependency-injection#lifecycle) without a server, for a `Handler` served elsewhere |
| `app.Build()` | resolves everything and returns every problem; the others call it |

To serve TLS yourself, start the services, then serve the handler:

<!-- go:fragment -->
```go
app := web.New()
if err := app.Start(ctx); err != nil {
	log.Fatal(err)
}
defer app.Stop(context.Background())
srv := &http.Server{Addr: ":8443", Handler: app.Handler()}
log.Fatal(srv.ListenAndServeTLS("cert.pem", "key.pem"))
```

## The Environment

The app reads these variables:

| Variable | |
|---|---|
| `PORT` | the port `Run("")` listens on |
| `VUKA_ENV` | which [env files](/web/configuration#files-and-precedence) are read; `development` shows internal error messages (as `web.Dev()` does) and logs the files read |
| `VUKA_CHROME` | the Chrome binary for browser tests |

In production, set real environment variables and leave `VUKA_ENV` unset
(or `production`): internal errors answer "Internal Server Error", never
their message.

`VUKA_ENV` and the other `VUKA_` variables are read from the real
environment only; an env file can't set them. A binary started outside a Go
module reads env files from its working directory alone, never from a
directory above it.

> [!WARNING]
> Build with **Go 1.27.2 or newer**: earlier releases have standard-library
> security fixes (`net/http`, `crypto/tls` and others) the binary doesn't
> get otherwise.

## Security Headers

Every response carries `X-Content-Type-Options: nosniff`,
`Referrer-Policy: strict-origin-when-cross-origin`, `X-Frame-Options: DENY`
and `Content-Security-Policy: frame-ancestors 'none'`, and HTTPS responses
`Strict-Transport-Security: max-age=31536000`, unless the handler sets its
own.

These options change them:

| Option | |
|---|---|
| `web.Headers(web.SecurityHeaders{…})` | change them: `FrameOptions: "SAMEORIGIN"` (the CSP follows), `ReferrerPolicy`, `ContentTypeOptions`, `CSP`, `HSTS` (a max-age), `HSTSSubdomains`; `"-"` (or a negative `HSTS`) leaves one out |
| `web.NoSecurityHeaders()` | none: a proxy in front sets them |
| `web.MaxRequestBody(n)` | the largest request body, 32 MiB by default ([uploads](/web/parameters#the-body)) |

## Compression and Caching {#compression}

Responses are compressed when the browser takes it: brotli, else gzip, with
`Vary: Accept-Encoding`. On a 200-row list page of a `vuka new` app, 268 KB
of HTML goes out as 4.6 KB of brotli (7.7 KB of gzip), for about a
millisecond of the server's time; `/_vuka/live.js` goes from 79 KB to 23 KB
and the app's stylesheet from 25 KB to 4.5 KB. Live sockets compress their
messages too ([live pages](/web/live#compression)).

What handlers write (pages, JSON, endpoints) is compressed as it goes out.
These are sent as they are:

- bodies under 1 KB;
- types already compressed: images, video, audio, archives, `woff`/`woff2`;
- a `Range` request, a `HEAD`, and `1xx`, `204`, `206` and `304` answers;
- a response the handler encoded itself (it set `Content-Encoding`), or
  marked `Cache-Control: no-transform`;
- event streams and WebSocket upgrades, which the compression never wraps.

A handler that flushes (`http.Flusher`) sends what it wrote so far,
compressed: a streamed page reaches the browser as it is flushed. An
encoded response's `ETag` becomes weak (`W/"…"`) and still matches the
browser's next `If-None-Match`.

The live runtime, the stylesheet, the components' scripts and the island
chunks are sent in an encoding made once: `vuka build` embeds their brotli
and gzip with them, and the app makes one the first time it is asked for
what wasn't. Each encoding has its own `ETag`, so a browser revalidating
gets a `304` for the one it holds.

| Option | |
|---|---|
| `web.Compression(level)` | how hard what handlers write is compressed: `web.BestSpeed` (1) to `web.BestCompression` (11), brotli's quality (gzip's level is the same, up to 9). `web.DefaultCompression` is 5 |
| `web.Compression(web.Off)` | no compression: a proxy in front compresses |

Compression is on under `vuka run` too, so what you measure while you work
is what the binary sends. The [toolbar](/web/toolbar#request) shows each
request's size as written and on the wire.

### Static Files {#static-files}

`app.Static(prefix, fsys)` sends each file with an `ETag` (a hash of its
content) and, when the file system has times (`os.DirFS`), `Last-Modified`;
a browser revalidating gets a `304`. A file whose name carries a content hash
(`app.3f9a1c2b.css`, `index-BkL2x9aQ.js`) is cached for good
(`public, max-age=31536000, immutable`); every other one is kept and
revalidated each time (`public, max-age=0, must-revalidate`), unless the
handler's middleware set its own `Cache-Control`. A compressible file is
sent in the encoding the browser takes: the `.br` or `.gz` beside it in
`fsys` when there is one (`app.css.br`), else one the app makes the first
time and keeps.

## Behind a Proxy

A proxy or load balancer in front of the app makes every request come from
its address. Name it, and only then are its headers believed:


<!-- go:fragment -->
```go
app := web.New(web.TrustedProxies("10.0.0.0/8", "fd00::/8"))
```

From those addresses, `web.ClientIP(r)` is the nearest address in
`X-Forwarded-For` (else `Forwarded`) that isn't a trusted proxy; entries a
client added itself further left are ignored. `X-Forwarded-Proto: https`
makes a request HTTPS (`web.IsHTTPS(r)`) for HSTS.

From any other address, those headers are ignored and `web.ClientIP(r)` is
the connection's peer. [Login throttling](/web/auth#signing-in-and-out) and
the [live connection caps](/web/live) per client address count by it.

## Building One Binary

`vuka build` makes one binary: views and every
[`vuka.File`](/features/decorators#files) are embedded, and so is the live
runtime. Static files are served from an `embed.FS`:


```go
//go:embed static
var static embed.FS

func main() {
	app := web.New()
	assets, _ := fs.Sub(static, "static")
	app.Static("/assets", assets)
	log.Fatal(app.Run(""))
}
```

## Running Several Replicas

Several replicas behind one hostname need a shared `web.LiveKey(key)` for
[live pages](/web/live#security), and a `web.WithRelay(r)` for broadcasts to
reach every replica.

## Using Another Router {#routers}

The default router is `net/http`'s `ServeMux` with Go 1.22 patterns
(`web.NewServeMux()`). Any router plugs in through this interface:

```go
type Router interface {
	http.Handler
	Handle(method, pattern string, h http.Handler) // canonical {name} / {name...} patterns
	Param(r *http.Request, name string) string
}
```

Pass yours with `web.WithRouter`:

<!-- go:context
var myRouter web.Router = web.NewServeMux()
-->

<!-- go:fragment -->
```go
app := web.New(web.WithRouter(myRouter))
```

A chi adapter is a dozen lines: `Handle` → `r.Method(method, pattern, h)`,
`Param` → `chi.URLParam(r, name)`. Paths are always written `{name}` and
`{name...}`; an adapter translates them if its router spells them otherwise.

App-wide middleware (`web.Use`) wraps the router, so it runs for every
request, 404s included.
