// What vuka gen writes for a _layout.vuka.html storing the page's title as
// it loads.

package titled

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Layout struct { // want `W0703: titled.Layout, a folder's layout, reads ui.PageTitle in Load: it loads again on every navigation`
	Children ui.Node
	title    string
}

func (l *Layout) Load(ctx context.Context) error {
	l.title = heading(ctx)
	return nil
}

func heading(ctx context.Context) string { return ui.PageTitle(ctx) }
