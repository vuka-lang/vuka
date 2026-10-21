package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/vuka-lang/web"
)

// Middleware rate-limits the API: API_RATE requests at once (100), one
// more each second; over it is 429 before the guard sees the key.
func Middleware() web.Middleware {
	limit := web.Env("API_RATE", 100.0)
	var mu sync.Mutex
	tokens, last := limit, time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			now := time.Now()
			tokens = min(limit, tokens+now.Sub(last).Seconds())
			last = now
			ok := tokens >= 1
			if ok {
				tokens--
			}
			mu.Unlock()
			if !ok {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			w.Header().Add("X-Served-By", "hr-api")
			next.ServeHTTP(w, r)
		})
	}
}
