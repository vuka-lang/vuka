// Package pages is the app's folder routes: each folder below is a path,
// each page in it a route, and a bracketed name a parameter. vuka writes
// route.go, whose Routes main passes to web.New. _layout.vuka.html is the
// document of every page.
package pages

import (
	"log"
	"net/http"
)

// Middleware stamps and logs every request of the site: the response says
// who served it, and the console shows each request.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Served-By", "hr")
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
