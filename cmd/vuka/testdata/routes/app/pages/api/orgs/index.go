package orgs

import "context"

// Index is the organisations, as JSON.
type Index struct{}

// Query filters the list.
type Query struct{ Name string }

func (*Index) Get(ctx context.Context, q Query) ([]string, error) { return nil, nil }

// Form is a new organisation.
type Form struct{ Name string }

func (*Index) Post(ctx context.Context, f Form) (string, error) { return f.Name, nil }
