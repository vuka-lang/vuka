package org

import (
	"context"

	"github.com/vuka-lang/ui"
)

// Layout frames an organisation's pages (layout.vuka.html).
type Layout struct {
	Params
	Children ui.Node
}

func (l *Layout) Load(ctx context.Context) error { return nil }
