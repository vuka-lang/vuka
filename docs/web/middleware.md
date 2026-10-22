# Middleware

Middleware is Go's: `func(http.Handler) http.Handler` (`web.Middleware`). Any
library's works as it is.

| | |
|---|---|
| <span class="vuka-only">`@web.Use(mw…)`</span><span class="go-only">the option `web.Use(mw…)`</span> | on a route (or a live page): that route's middleware |
| `web.New(web.Use(mw…))`, `app.Use(mw…)` | every request of the app, 404s included |
| `web.Wrap(mw…)` | a plain-Go route's middleware |

::: vuka
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
:::

::: go
<!-- go:context
type PetStore struct{}

func (s *PetStore) Remove(id int) error { return nil }

func logRequests(next http.Handler) http.Handler { return next }
-->

```go
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

func RemovePet(id int, store *PetStore) error { return store.Remove(id) }

func init() {
	web.Routes.Delete("/pets/{id}", RemovePet, web.Use(requireToken))
}

func main() {
	app := web.New(web.Use(logRequests))
	log.Fatal(app.Run(""))
}
```
:::

The first given is outermost, and the app's run before the route's:
`web.Use(a)`, `app.Use(b)` and <span class="vuka-only">`@web.Use(c, d)`</span><span class="go-only">a route's `web.Use(c, d)`</span> run as `a b c d handler`.
The app wraps its routes after reading the [env files](/web/configuration),
so middleware built from `web.Env` sees them.

## Decorators as middleware

A [call decorator](/features/decorators) on a handler is middleware
too, at the function's level: it sees the handler's arguments and results,
can change them, retry, or answer without running it.<span class="go-only"> In
Go it is a `func(c *vuka.Call)`, put around the handler with `vuka.Wrap`.</span>

::: vuka
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
:::

::: go
```go
func audited(c *vuka.Call) {
	r := web.Request(c.Context())
	c.Next()
	log.Println("audit:", r.Method, r.URL.Path, "err:", c.Err())
}

type PetAdmin struct{ store *PetStore }

func (a *PetAdmin) remove(ctx context.Context, id int) error { return a.store.Remove(id) }

var petAdminRemove vuka.Decorated[func(*PetAdmin, context.Context, int) error]

func (a *PetAdmin) Remove(ctx context.Context, id int) error {
	return petAdminRemove.Get(func() func(*PetAdmin, context.Context, int) error {
		return vuka.Wrap((*PetAdmin).remove, vuka.FuncInfo{Name: "PetAdmin.Remove", Method: true}, audited)
	})(a, ctx, id)
}

func NewPetAdmin(store *PetStore) *PetAdmin { return &PetAdmin{store: store} }

func init() {
	web.Routes.Controller("/admin/pets", NewPetAdmin)
	web.Routes.Delete("/{id}", (*PetAdmin).Remove, web.Use(requireToken))
}
```
:::

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

For signing users in and guarding routes, use [Authentication](/web/auth)
rather than a token check of your own; it also protects cookie-signed-in
forms from other sites (`web.CSRF`).
