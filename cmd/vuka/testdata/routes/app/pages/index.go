package pages

import (
	"context"
	"io"

	"example.com/app/pages/urls"
)

// Index is the home page.
type Index struct{}

func NewIndex() *Index { return &Index{} }

func (*Index) Render(ctx context.Context, w io.Writer) error {
	_, err := io.WriteString(w, urls.Orgs.Path() + urls.About.Path() + urls.Files.File("a b/c").Path())
	return err
}
