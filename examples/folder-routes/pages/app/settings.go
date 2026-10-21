// Package app is /app: a client-side app under app/route, with Settings, a
// server page, beside it.
package app

import (
	"context"
	"io"
	"net/http"

	"example.com/folder-routes/pages/urls"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/web"
)

// Settings is GET /app/settings: a fixed segment, so it wins over the
// client app's catch-all. Its attributes are the type's, as a live
// page's would be: this page alone is never cached.
type Settings struct{}

func init() { vuka.RegisterType[Settings](web.Use(noStore)) }

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (p *Settings) Title() string { return "App settings" }

func (p *Settings) Render(ctx context.Context, w io.Writer) error {
	open := urls.App.Route("").Path()
	return ui.HTML(`<section id="settings"><h1>Settings</h1><a id="open" href={open}>Open the app</a></section>`, open).Render(ctx, w)
}
