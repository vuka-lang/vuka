package permit

import (
	"context"
	"fmt"

	"example.com/folder-routes/pages/employers/employer"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Index is a permit, a live page: GET
// /employers/{employer}/permits/{permit}, inside the employer's layout.
type Index struct {
	ui.Live
	Params
	store  *store.Store
	permit store.Permit
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

func (p *Index) LiveSchema(l *ui.LiveSchema) { l.Mount(p.mount) }

func (p *Index) mount(ctx context.Context) error {
	pm, ok := p.store.Permit(p.Employer, p.Permit)
	if !ok {
		return web.NotFound
	}
	p.permit = pm
	return nil
}

// Title is read once the parameters are bound, before mount.
func (p *Index) Title() string { return fmt.Sprintf("Permit %d", p.Permit) }

// Approve is the button's event.
func (p *Index) Approve() {
	p.store.Approve(p.permit.ID)
	p.permit.Approved = true
}

func (p *Index) Render() ui.Node {
	status := "Pending"
	if p.permit.Approved {
		status = "Approved"
	}
	approve := ui.If(!p.permit.Approved, ui.HTML(`<button id="approve" onClick={p.Approve}>Approve</button>`, p.Approve))
	return ui.HTML(`<section id="permit">
  <h2>{p.permit.Kind}</h2>
  <p id="status">{status}</p>
  {approve}
  <a id="back" href={employer.IndexURL(p.Employer)}>All permits</a>
</section>`, p.permit.Kind, status, approve, employer.IndexURL(p.Employer))
}
