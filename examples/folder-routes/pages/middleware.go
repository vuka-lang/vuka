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
