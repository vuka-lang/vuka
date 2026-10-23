// What vuka gen writes for a _layout.vuka.html that reads no request: it
// persists across navigations.

package shell

import (
	"context"
	"net/http"

	"github.com/vuka-lang/ui/v2"
)

type Layout struct {
	Children ui.Node
	name     string
}

func (l *Layout) Load(ctx context.Context) error {
	l.name = "shop"
	return nil
}

// Handler reads a request, but isn't the layout's.
func Handler(r *http.Request) string { return r.URL.Path }
