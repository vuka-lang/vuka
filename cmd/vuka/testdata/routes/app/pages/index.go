package pages

import (
	"context"
	"io"

	"example.com/app/pages/orgs"
)

// Index is the home page.
type Index struct{}

func NewIndex() *Index { return &Index{} }

func (*Index) Render(ctx context.Context, w io.Writer) error {
	_, err := io.WriteString(w, orgs.IndexURL())
	return err
}
