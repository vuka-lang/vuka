// Package employers is /employers.
package employers

import (
	"slices"
	"strings"

	"example.com/folder-routes/pages/urls"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
)

// Index is the list of employers, a live page: GET /employers.
type Index struct {
	ui.Live
	store  *store.Store
	byCity bool
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

func (p *Index) Title() string { return "Employers" }

// Sort orders the list by city, or back by number.
func (p *Index) Sort() { p.byCity = !p.byCity }

func (p *Index) Render() ui.Node {
	list := p.store.Employers()
	label := "Sort by city"
	if p.byCity {
		slices.SortStableFunc(list, func(a, b store.Employer) int { return strings.Compare(a.City, b.City) })
		label = "Sort by number"
	}
	rows := ui.Each(list, func(e store.Employer) ui.Node {
		href := urls.Employers.Employer(e.ID).Path()
		return ui.HTML(`<li><a href={href}>{e.Name}</a>, {e.City}</li>`, href, e.Name, e.City)
	})
	return ui.HTML(`<section id="employers">
  <h1>Employers</h1>
  <button id="sort" onClick={p.Sort}>{label}</button>
  <ul>{rows}</ul>
  <a id="new" href={urls.Employers.New.Path()}>New employer</a>
</section>`, p.Sort, label, rows, urls.Employers.New.Path())
}
