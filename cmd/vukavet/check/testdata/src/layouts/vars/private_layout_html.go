// What vuka gen writes for a _layout.vuka.html storing a context
// processor's variable as it loads, and reading another in its markup.

package vars

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

type Layout struct { // want `W0703: vars.Layout, a folder's layout, stores a context processor's variable in Load \(web.Var\)`
	Children ui.Node
	path     string
}

func (l *Layout) Load(ctx context.Context) error {
	l.path = web.Var[web.RequestInfo](ctx, "request").Path
	return nil
}

func (l *Layout) View(ctx context.Context) ui.Node {
	_ = web.Var[string](ctx, "csrfToken")
	return nil
}
