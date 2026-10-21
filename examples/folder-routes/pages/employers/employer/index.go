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
	store    *store.Store
	employer store.Employer
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

// Mount runs once the employer is bound, before Title and Render.
func (p *Index) Mount() { p.employer, _ = p.store.Employer(p.Employer) }

func (p *Index) Title() string { return p.employer.Name }

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	permits := ui.Each(p.store.Permits(p.Employer), func(pm store.Permit) ui.Node {
		href := urls.Employers.Employer(p.Employer).Permits.Permit(pm.ID).Path()
		return ui.HTML(`<li><a href={href}>{pm.Kind}</a></li>`, href, pm.Kind)
	})
	contract := urls.Files.File(web.Rest(fmt.Sprintf("contracts/%d.txt", p.Employer))).Path()
	return ui.HTML(`<section id="overview">
  <h2>Permits</h2>
  <ul id="permits">{permits}</ul>
  <a id="contract" href={contract}>Contract</a>
</section>`, permits, contract).Render(ctx, w)
}
