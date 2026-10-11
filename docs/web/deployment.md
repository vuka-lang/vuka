# Routers and deployment

## Running

| | |
|---|---|
| `app.Run(addr)` | builds, starts the services, serves `addr` until SIGINT or SIGTERM, then shuts down gracefully (10s for requests in flight) and stops the services |
| `app.Run("")` | listens on `:$PORT`, else `:8080` |
| `app.Handler()` | the app as an `http.Handler`, to serve or mount anywhere; panics when Build fails |
| `app.Start(ctx)`, `app.Stop(ctx)` | the services' [lifecycle](/web/dependency-injection#lifecycle) without a server, for a `Handler` served elsewhere |
| `app.Build()` | resolves everything and returns every problem; the others call it |

```go
app := web.New()
if err := app.Start(ctx); err != nil {
	log.Fatal(err)
}
defer app.Stop(context.Background())
srv := &http.Server{Addr: ":8443", Handler: app.Handler()}
log.Fatal(srv.ListenAndServeTLS("cert.pem", "key.pem"))
```

## The environment

| Variable | |
|---|---|
| `PORT` | the port `Run("")` listens on |
| `VUKA_ENV` | which [env files](/web/configuration#files-and-precedence) are read; `development` shows internal error messages (as `web.Dev()` does) and logs the files read |
| `VUKA_CHROME` | the Chrome binary for browser tests |

In production, set real environment variables and leave `VUKA_ENV` unset (or
`production`): internal errors answer "Internal Server Error", never their
message. `VUKA_ENV` and the other `VUKA_` variables are read from the real
environment only — an env file can't set them — and a binary started
outside a Go module reads env files from its working directory alone, never
from a directory above it.

Build with **Go 1.27.2 or newer**: earlier releases have standard-library
security fixes (`net/http`, `crypto/tls` and others) the binary doesn't get
otherwise.

## Security headers

Every response carries `X-Content-Type-Options: nosniff`,
`Referrer-Policy: strict-origin-when-cross-origin`, `X-Frame-Options: DENY`
and `Content-Security-Policy: frame-ancestors 'none'`, and HTTPS responses
`Strict-Transport-Security: max-age=31536000` — unless the handler sets
its own.

| Option | |
|---|---|
| `web.Headers(web.SecurityHeaders{…})` | change them: `FrameOptions: "SAMEORIGIN"` (the CSP follows), `ReferrerPolicy`, `ContentTypeOptions`, `CSP`, `HSTS` (a max-age), `HSTSSubdomains`; `"-"` (or a negative `HSTS`) leaves one out |
| `web.NoSecurityHeaders()` | none: a proxy in front sets them |
| `web.MaxRequestBody(n)` | the largest request body, 32 MiB by default ([uploads](/web/parameters#the-body)) |

## Behind a proxy

A proxy or load balancer in front of the app makes every request come from
its address. Name it, and only then are its headers believed:

```vuka
app := web.New(web.TrustedProxies("10.0.0.0/8", "fd00::/8"))
```

From those addresses, `web.ClientIP(r)` is the nearest address in
`X-Forwarded-For` (else `Forwarded`) that isn't a trusted proxy — entries a
client added itself further left are ignored — and `X-Forwarded-Proto: https`
makes a request HTTPS (`web.IsHTTPS(r)`) for HSTS. From any other address,
those headers are ignored and `web.ClientIP(r)` is the connection's peer.
[Login throttling](/web/auth#signing-in-and-out) counts by it.

`vuka build` makes one binary: views and every
[`vuka.File`](/features/decorators#files) are embedded, and so is the live
runtime. Static files are served from an `embed.FS`:

```vuka
//go:embed static
var static embed.FS

func main() {
	app := web.New()
	assets, _ := fs.Sub(static, "static")
	app.Static("/assets", assets)
	app.Run("")
}
```

Several replicas behind one hostname need a shared `web.LiveKey(key)` for
[live pages](/web/live#security), and a `web.WithRelay(r)` for broadcasts to
reach every replica.

## Routers

The default router is `net/http`'s `ServeMux` with Go 1.22 patterns
(`web.NewServeMux()`). Any router plugs in through:

```go
type Router interface {
	http.Handler
	Handle(method, pattern string, h http.Handler) // canonical {name} / {name...} patterns
	Param(r *http.Request, name string) string
}
```

```go
app := web.New(web.WithRouter(myRouter))
```

A chi adapter is a dozen lines: `Handle` → `r.Method(method, pattern, h)`,
`Param` → `chi.URLParam(r, name)`. Paths are always written `{name}` and
`{name...}`; an adapter translates them if its router spells them otherwise.

App-wide middleware (`web.Use`) wraps the router, so it runs for every
request, 404s included.
