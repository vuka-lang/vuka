// Package app is /app: a client-side app under app/route, with Settings, a
// server page, beside it.
package app

import (
	"context"
	"io"

	"example.com/folder-routes/pages/urls"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/vuka"
)

// NoStore marks a page main's web.WrapRoutes("no-store") never caches —
// an attribute a gate like auth's would be.
type NoStore struct{}

func (NoStore) RouteWrapper() string { return "no-store" }

// Settings is GET /app/settings: a fixed segment, so it wins over the
// client app's catch-all. Its attributes are the type's, as a live
// page's would be: this page alone is never cached.
type Settings struct{}

func init() { vuka.RegisterType[Settings](NoStore{}) }

func (p *Settings) Title() string { return "App settings" }

func (p *Settings) Render(ctx context.Context, w io.Writer) error {
	open := urls.App.Route("").Path()
	return ui.HTML(`<section id="settings"><h1>Settings</h1><a id="open" href={open}>Open the app</a></section>`, open).Render(ctx, w)
}
