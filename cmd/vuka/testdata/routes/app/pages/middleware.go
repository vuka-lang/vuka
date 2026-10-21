package pages

import "net/http"

// Middleware stamps every route.
func Middleware(next http.Handler) http.Handler { return next }
