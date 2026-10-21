package org

import (
	"context"
	"io"
)

// ResetPassword resets the organisation's password.
type ResetPassword struct{ Params }

func NewResetPassword() (*ResetPassword, error) { return &ResetPassword{}, nil }

func (*ResetPassword) Render(ctx context.Context, w io.Writer) error { return nil }

// Store is a service, not a page: it renders nothing.
type Store struct{}

func NewStore() *Store { return &Store{} }
