// Package pages is the app's folder routes: each folder below is a path
// and each page type in it a route. main imports it once; vuka gen adds
// the registrations of the folders. Its Layout is the document of every
// page; about.vuka.html is a page with no Go type of its own.
package pages

import (
	"context"
	"io"

	"example.com/folder-routes/pages/urls"
	"github.com/vuka-lang/ui"
)

type Layout struct {
	Children ui.Node
}

func (l Layout) Render(ctx context.Context, w io.Writer) error {
	title := "HR"
	if t := ui.PageTitle(ctx); t != "" {
		title = t + " - HR"
	}
	employers, handbook := urls.Employers.Path(), urls.Files.File("handbook/leave.txt").Path()
	about, settings := urls.About.Path(), urls.App.Settings.Path()
	return ui.HTML(`<html lang="en">
<head><meta charset="utf-8"/><title>{title}</title></head>
<body>
  <nav id="site"><a href={employers}>Employers</a> <a href={handbook}>Handbook</a> <a href={about}>About</a> <a href={settings}>App</a></nav>
  <main>{l.Children}</main>
</body>
</html>`, title, employers, handbook, about, settings, l.Children).Render(ctx, w)
}
