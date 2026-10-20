package file

import (
	"context"
	"io"

	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
)

// Index shows a file: GET /files/{file...}.
type Index struct {
	Params
	store *store.Store
}

func NewIndex(s *store.Store) *Index { return &Index{store: s} }

func (p *Index) Title() string { return string(p.File) }

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	text, _ := p.store.File(string(p.File))
	return ui.HTML(`<pre>{text}</pre>`, text).Render(ctx, w)
}
