package file

import (
	"context"
	"io"
)

// Index serves a file.
type Index struct{ Params }

func (x *Index) Render(ctx context.Context, w io.Writer) error { return nil }
