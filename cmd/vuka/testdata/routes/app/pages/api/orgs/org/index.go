package org

import "context"

// Index is an organisation, as JSON.
type Index struct{ Params }

func NewIndex() (*Index, error) { return &Index{}, nil }

func (x *Index) Get(ctx context.Context) (int, error) { return x.Org, nil }

// Form is an organisation's new name.
type Form struct{ Name string }

func (x *Index) Put(ctx context.Context, f Form) error { return nil }

func (x *Index) Delete(ctx context.Context) error { return nil }
