package employers

import (
	"context"
	"io"

	"example.com/folder-routes/pages/employers/employer"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// New is the form adding an employer: GET /employers/new, and its Post
// method POST /employers/new.
type New struct {
	store *store.Store
}

func NewNew(s *store.Store) *New { return &New{store: s} }

func (p *New) Title() string { return "New employer" }

func (p *New) Post(ctx context.Context, form store.EmployerForm) (web.Redirect, error) {
	e := p.store.AddEmployer(form)
	return web.Redirect(employer.IndexURL(e.ID)), nil
}

func (p *New) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<section id="new">
  <h1>New employer</h1>
  <form method="post" action={NewURL()}>
    <label>Name <input name="name" required/></label>
    <label>City <input name="city" required/></label>
    <button>Add</button>
  </form>
</section>`, NewURL()).Render(ctx, w)
}
