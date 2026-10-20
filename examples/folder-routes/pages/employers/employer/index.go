package employer

import (
	"context"
	"fmt"
	"io"

	"example.com/folder-routes/pages/files/file"
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

// Render links to the permits by path: the permit folder embeds this
// package's Params, so it imports this package and this one can't import
// it back for its typed URLs.
func (p *Index) Render(ctx context.Context, w io.Writer) error {
	permits := ui.Each(p.store.Permits(p.Employer), func(pm store.Permit) ui.Node {
		href := fmt.Sprintf("/employers/%d/permits/%d", p.Employer, pm.ID)
		return ui.HTML(`<li><a href={href}>{pm.Kind}</a></li>`, href, pm.Kind)
	})
	contract := file.IndexURL(web.Rest(fmt.Sprintf("contracts/%d.txt", p.Employer)))
	return ui.HTML(`<section id="overview">
  <h2>Permits</h2>
  <ul id="permits">{permits}</ul>
  <a id="contract" href={contract}>Contract</a>
</section>`, permits, contract).Render(ctx, w)
}
