// What vuka gen writes for a _layout.vuka.html reading the request's path.

package crumbs

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

type Layout struct { // want `W0703: crumbs.Layout, a folder's layout, reads web.Request: it loads and renders again on every navigation`
	Children ui.Node
	path     string
}

func (l *Layout) Load(ctx context.Context) error {
	l.path = here(ctx)
	return nil
}

func here(ctx context.Context) string { return web.Request(ctx).URL.Path }
