package employer

import (
	"context"
	"io"

	"example.com/folder-routes/pages/urls"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Edit is the employer's form: GET /employers/{employer}/edit, its Post
// method POST and its Delete method DELETE on the same path. Each request
// builds its own Edit with the employer bound, and the employer's Layout
// loads first, so an unknown employer is not found for all three.
type Edit struct {
	Params
	store *store.Store
}

func NewEdit(s *store.Store) *Edit { return &Edit{store: s} }

// employer is the bound employer; its layout has found it already.
func (p *Edit) employer() store.Employer {
	e, _ := p.store.Employer(p.Employer)
	return e
}

func (p *Edit) Title() string { return "Edit " + p.employer().Name }

func (p *Edit) Post(ctx context.Context, form store.EmployerForm) (web.Redirect, error) {
	p.store.UpdateEmployer(p.Employer, form)
	return web.Redirect(urls.Employers.Employer(p.Employer).Path()), nil
}

func (p *Edit) Delete(ctx context.Context) error {
	p.store.DeleteEmployer(p.Employer)
	return nil
}

func (p *Edit) Render(ctx context.Context, w io.Writer) error {
	e, action := p.employer(), urls.Employers.Employer(p.Employer).Edit.Path()
	return ui.HTML(`<form id="edit" method="post" action={action}>
  <label>Name <input name="name" value={e.Name} required/></label>
  <label>City <input name="city" value={e.City} required/></label>
  <button>Save</button>
</form>`, action, e.Name, e.City).Render(ctx, w)
}
