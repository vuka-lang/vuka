package employer

import (
	"context"
	"fmt"
	"io"

	"example.com/folder-routes/pages/urls"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Index is an employer's overview, a stateless page: GET
// /employers/{employer}.
type Index struct {
	Params
	store *store.Store
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

func (p *Index) Title() string {
	e, _ := p.store.Employer(p.Employer)
	return e.Name
}

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	permits := ui.Each(p.store.Permits(p.Employer), func(pm store.Permit) ui.Node {
		href := urls.Employers.Employer(p.Employer).Permits.Permit(pm.ID).Path()
		return ui.HTML(`<li><a href={href}>{pm.Kind}</a></li>`, href, pm.Kind)
	})
	contract := urls.Files.File(web.Rest(fmt.Sprintf("contracts/%d.txt", p.Employer))).Path()
	api := urls.Api.Employers.Employer(p.Employer).Path()
	return ui.HTML(`<section id="overview">
  <h2>Permits</h2>
  <ul id="permits">{permits}</ul>
  <a id="contract" href={contract}>Contract</a>
  <a id="api" href={api}>As JSON</a>
</section>`, permits, contract, api).Render(ctx, w)
}
