package employer

import (
	"context"
	"io"

	"example.com/folder-routes/pages/urls"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Layout wraps every page of an employer, inside hr's document. Load runs
// first, so an unknown employer is not found on each of them.
type Layout struct {
	Params
	Children ui.Node
	employer store.Employer
}

func (l *Layout) Load(ctx context.Context, s *store.Store) error {
	e, ok := s.Employer(l.Employer)
	if !ok {
		return web.NotFound
	}
	l.employer = e
	return nil
}

func (l Layout) Render(ctx context.Context, w io.Writer) error {
	at := urls.Employers.Employer(l.Employer)
	overview, edit := at.Path(), at.Edit.Path()
	return ui.HTML(`<article id="employer">
  <header>
    <h1>{l.employer.Name}</h1>
    <p>{l.employer.City}</p>
    <nav><a href={overview}>Overview</a> <a href={edit}>Edit</a></nav>
  </header>
  {l.Children}
</article>`, l.employer.Name, l.employer.City, overview, edit, l.Children).Render(ctx, w)
}
