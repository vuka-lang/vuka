package file

import (
	"context"
	"io"

	"example.com/folder-routes/store"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Layout answers a path naming no file not found.
type Layout struct {
	Params
	Children ui.Node
}

func (l *Layout) Load(ctx context.Context, s *store.Store) error {
	if _, ok := s.File(string(l.File)); !ok {
		return web.NotFound
	}
	return nil
}

func (l Layout) Render(ctx context.Context, w io.Writer) error {
	name := string(l.File)
	return ui.HTML(`<article id="file"><h1>{name}</h1>{l.Children}</article>`, name, l.Children).Render(ctx, w)
}
