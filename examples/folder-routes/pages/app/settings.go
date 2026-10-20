// Package app is /app: a client-side app under app/route, with Settings, a
// server page, beside it.
package app

import (
	"context"
	"io"

	"example.com/folder-routes/pages/app/route"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Settings is GET /app/settings: a fixed segment, so it wins over the
// client app's catch-all.
type Settings struct{}

func NewSettings() *Settings { return &Settings{} }

func (p *Settings) Title() string { return "App settings" }

func (p *Settings) Render(ctx context.Context, w io.Writer) error {
	open := route.IndexURL(web.Rest(""))
	return ui.HTML(`<section id="settings"><h1>Settings</h1><a id="open" href={open}>Open the app</a></section>`, open).Render(ctx, w)
}
