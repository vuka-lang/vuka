// Package employers is /api/employers.
package employers

import (
	"context"
	"strings"

	"example.com/folder-routes/store"
)

// Index is the employers' collection: GET lists them, POST adds one.
type Index struct {
	store *store.Store
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

// ListQuery is GET's query string: /api/employers?city=Arusha&limit=10.
type ListQuery struct {
	City  string `form:"city"`
	Limit int    `form:"limit" validate:"omitempty,max=100"`
}

func (e *Index) Get(ctx context.Context, q ListQuery) ([]store.Employer, error) {
	out := []store.Employer{}
	for _, em := range e.store.Employers() {
		if q.City == "" || strings.EqualFold(em.City, q.City) {
			out = append(out, em)
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (e *Index) Post(ctx context.Context, in store.EmployerForm) (store.Employer, error) {
	return e.store.AddEmployer(in), nil
}
