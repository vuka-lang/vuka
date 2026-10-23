package main

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
)

// Col is a Grid's column: its cell is required.
type Col struct {
	Title string
	Cell  func(int) ui.Node `vuka:"required"`
}

// Grid takes its columns as repeated slots.
type Grid struct{ Cols []Col }

func (g Grid) View() ui.Node { return nil }

func (g Grid) Render(ctx context.Context, w io.Writer) error { return g.View().Render(ctx, w) }
