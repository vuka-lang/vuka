package main

import (
	"context"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// ShellProps is the layout's (shell.vuka.html).
type ShellProps struct {
	Title    string
	Aside    ui.Node
	Children ui.Node
}

// requestPath is the path of the request rendering ctx, "live" in a live
// session's renders.
func requestPath(ctx context.Context) string {
	if r := web.Request(ctx); r != nil {
		return r.URL.Path
	}
	return "live"
}
