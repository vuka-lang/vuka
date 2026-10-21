package employer

import (
	"context"

	"example.com/folder-routes/store"
	"github.com/vuka-lang/web"
)

// Index is one employer: GET reads it, PUT replaces it, DELETE removes it.
type Index struct {
	Params
	store *store.Store
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

func (e *Index) Get(ctx context.Context) (store.Employer, error) {
	em, ok := e.store.Employer(e.Employer)
	if !ok {
		return store.Employer{}, web.NotFound
	}
	return em, nil
}

func (e *Index) Put(ctx context.Context, in store.EmployerForm) (store.Employer, error) {
	if _, err := e.Get(ctx); err != nil {
		return store.Employer{}, err
	}
	e.store.UpdateEmployer(e.Employer, in)
	return e.Get(ctx)
}

func (e *Index) Delete(ctx context.Context) error {
	if _, err := e.Get(ctx); err != nil {
		return err
	}
	e.store.DeleteEmployer(e.Employer)
	return nil
}
