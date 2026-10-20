package org

import (
	"context"
	"io"
)

// Index is an organisation's page.
type Index struct{ Params }

func NewIndex() *Index { return &Index{} }

func (x *Index) Render(ctx context.Context, w io.Writer) error {
	_, err := io.WriteString(w, ResetPasswordURL(x.Org))
	return err
}

func (*Index) Delete(ctx context.Context) error { return nil }
