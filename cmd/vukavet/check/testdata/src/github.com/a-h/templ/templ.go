package templ

import (
	"context"
	"io"
)

type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

type Attributes map[string]any

type ComponentScript struct{ Name string }

type SafeURL string
