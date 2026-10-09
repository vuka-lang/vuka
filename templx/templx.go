// Package templx passes children to templ components. vuka.Node is
// templ.Component, so components need no adapter either way; children do,
// because templ passes them on the context.
package templx

import (
	"context"
	"io"

	"github.com/a-h/templ"
	"github.com/vuka-lang/vuka"
)

// WithChildren renders c with children as its templ children (templ.WithChildren),
// so a templ component's { children... } renders them: <Card>{…}</Card> on a templ Card.
func WithChildren(c vuka.Node, children vuka.Node) vuka.Node {
	if children == nil {
		children = templ.NopComponent
	}
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if c == nil {
			return nil
		}
		return c.Render(templ.WithChildren(ctx, children), w)
	})
}
