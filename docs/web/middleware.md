# Middleware

Middleware is Go's: `func(http.Handler) http.Handler` (`web.Middleware`). Any
library's works as it is.

| | |
|---|---|
| `@web.Use(mw…)` | on a route (or a `@web.Live` page): that route's middleware |
| `web.New(web.Use(mw…))`, `app.Use(mw…)` | every request of the app, 404s included |
| `web.Wrap(mw…)` | a plain-Go route's middleware |

```vuka
func requireToken(next http.Handler) http.Handler {
	want := "Bearer " + web.Env("ADMIN_TOKEN", "letmein")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			http.Error(w, `{"error":"unauthorized","status":401}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

@web.Delete("/pets/{id}")
@web.Use(requireToken)
func RemovePet(id int, store *PetStore) error { return store.Remove(id) }

func main() {
	app := web.New(web.Use(logRequests))
	app.Run("")
}
```

The first given is outermost, and the app's run before the route's:
`web.Use(a)`, `app.Use(b)` and `@web.Use(c, d)` run as `a b c d handler`.
The app wraps its routes after reading the [env files](/web/configuration),
so middleware built from `web.Env` sees them.

## Decorators as middleware

A Vuka [call decorator](/features/decorators) on a handler is middleware
too, at the function's level: it sees the handler's arguments and results,
can change them, retry, or answer without running it.

```vuka
decorator audited(c) {
	r := web.Request(c.Context())
	c.Next()
	log.Println("audit:", r.Method, r.URL.Path, "err:", c.Err())
}

@web.Delete("/{id}")
@web.Use(requireToken)
@audited
func (a *PetAdmin) Remove(ctx context.Context, id int) error { return a.store.Remove(id) }
```

A decorator reaches the request through the handler's `context.Context`
argument — declare one on the handler:

| | |
|---|---|
| `web.Request(ctx)` | the `*http.Request` being served |
| `web.ResponseWriter(ctx)` | its `http.ResponseWriter` |

A decorator's `c.SetErr(web.Forbidden)` (without `c.Next()`) answers 403 the
way the handler's own error would, as JSON or an HTML page.

Which to use: HTTP middleware for what concerns requests (auth headers,
CORS, logging, compression) and runs before parameters bind; a decorator for
what concerns the call (auditing, retries, caching a result, a permission
read from a [typed attribute](/features/attributes)).
