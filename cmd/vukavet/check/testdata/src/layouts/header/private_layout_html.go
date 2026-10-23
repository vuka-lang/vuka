// What vuka gen writes for a _layout.vuka.html reading the page's title and
// breadcrumbs in its markup: it persists across navigations.

package header

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Layout struct {
	Children ui.Node
}

func (l *Layout) Load(ctx context.Context) error { return nil }

func (l *Layout) View() ui.Node {
	return heading(context.Background())
}

func heading(ctx context.Context) ui.Node {
	_, _ = ui.PageTitle(ctx), ui.PageCrumbs(ctx)
	return nil
}
