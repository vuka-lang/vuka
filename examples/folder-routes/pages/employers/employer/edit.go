package employer

import (
	"context"
	"io"

	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Edit is the employer's form: GET /employers/{employer}/edit, its Post
// method POST and its Delete method DELETE on the same path. Each request
// builds its own Edit with the employer bound. The layouts' Load runs for
// the page, not for Post and Delete, so they check the employer themselves.
type Edit struct {
	Params
	store    *store.Store
	employer store.Employer
}

func NewEdit(s *store.Store) *Edit { return &Edit{store: s} }

func (p *Edit) Mount() { p.employer, _ = p.store.Employer(p.Employer) }

func (p *Edit) Title() string { return "Edit " + p.employer.Name }

func (p *Edit) Post(ctx context.Context, form store.EmployerForm) (web.Redirect, error) {
	if !p.store.UpdateEmployer(p.Employer, form) {
		return "", web.NotFound
	}
	return web.Redirect(IndexURL(p.Employer)), nil
}

func (p *Edit) Delete(ctx context.Context) error {
	if !p.store.DeleteEmployer(p.Employer) {
		return web.NotFound
	}
	return nil
}

func (p *Edit) Render(ctx context.Context, w io.Writer) error {
	e := p.employer
	return ui.HTML(`<form id="edit" method="post" action={EditURL(p.Employer)}>
  <label>Name <input name="name" value={e.Name} required/></label>
  <label>City <input name="city" value={e.City} required/></label>
  <button>Save</button>
</form>`, EditURL(p.Employer), e.Name, e.City).Render(ctx, w)
}
