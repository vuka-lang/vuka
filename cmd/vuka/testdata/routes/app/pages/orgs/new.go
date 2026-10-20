package orgs

import (
	"context"
	"io"
)

// New is the form for a new organisation.
type New struct{}

func NewNew() *New { return &New{} }

func (*New) Render(ctx context.Context, w io.Writer) error { return nil }

// Form is what New posts.
type Form struct{ Name string }

func (*New) Post(ctx context.Context, f Form) error { return nil }
