package main

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// List takes a scoped slot and a Node one.
type List struct {
	Items   []string
	Row     func(string) ui.Node
	Actions ui.Node
}

func (l List) View() ui.Node { return nil }

func (l List) Render(ctx context.Context, w io.Writer) error { return l.View().Render(ctx, w) }
