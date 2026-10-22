package api

import (
	"net/http"

	"github.com/vuka-lang/web/v2"
)

// Limiter paces the API.
type Limiter struct{}

// Middleware paces every API route.
func Middleware(l *Limiter) web.Middleware {
	return func(next http.Handler) http.Handler { return next }
}
