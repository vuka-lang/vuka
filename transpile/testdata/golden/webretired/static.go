package web

import (
	"context"
	"io/fs"
)

// Static serves fsys's files under prefix.
func (a *App) Static(prefix string, fsys fs.FS) *App { return a }

// Asset is the URL path of a static file.
func Asset(ctx context.Context, path string) string { return path }
