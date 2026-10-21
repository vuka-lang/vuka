// Command folder-routes is a vuka app in plain Go whose routes are the
// folders under pages/: vuka gen writes their web.Routes calls and typed
// URLs, and vuka routes lists them. Run it with vuka run, or vuka gen
// -inplace then go run.
package main

import (
	"log"
	"net/http"

	_ "example.com/folder-routes/pages"
	"example.com/folder-routes/pages/app"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/web"
)

func main() {
	a := web.New(web.Provide(store.New), web.WrapRoutes("no-store", noStore))
	a.Get("/", func() web.Redirect { return "/employers" })
	if err := a.Run(""); err != nil {
		log.Fatal(err)
	}
}

// noStore wraps the routes marked app.NoStore: their pages are never
// cached.
func noStore(info web.RouteInfo) web.Middleware {
	for _, x := range info.Attrs {
		if _, ok := x.(app.NoStore); ok {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store")
					next.ServeHTTP(w, r)
				})
			}
		}
	}
	return nil
}
