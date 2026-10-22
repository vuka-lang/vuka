// Package api is /api, a JSON API: endpoint types with Get, Post, Put or
// Delete methods and no markup, and files of method funcs, GET, PUT and
// DELETE in employers/[employer].vuka.
package api

import (
	"context"
	"net/http"

	"github.com/vuka-lang/web/v2"
)

// Guard checks the API key of every request below /api: the read key may
// read, the write key may also change.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	r := web.Request(ctx)
	switch r.Header.Get("X-API-Key") {
	case web.Env("API_WRITE_KEY", "write-key"):
		return nil
	case web.Env("API_READ_KEY", "read-key"):
		if r.Method == http.MethodGet {
			return nil
		}
		return web.Forbidden
	}
	return web.Unauthorized
}
