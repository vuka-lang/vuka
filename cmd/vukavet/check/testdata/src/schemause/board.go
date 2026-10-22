package schemause

import (
	"context"

	"github.com/vuka-lang/ui"
)

// Board is declared in board.vuka.html (board_html.go): its hooks are
// attributes there.
func (b *Board) LiveSchema(l *ui.LiveSchema) { // want `W0306: LiveSchema is what the compiler writes`
	l.Field(&b.Q, ui.Bind{})
	l.Mount(b.mount)
}

func (b *Board) mount(ctx context.Context) error { return nil }

// Plain is declared in Go: its LiveSchema is the only form it has.
type Plain struct{ ui.Live }

func (p *Plain) LiveSchema(l *ui.LiveSchema)     { l.Mount(p.mount) }
func (p *Plain) mount(ctx context.Context) error { return nil }
func (p *Plain) Render() ui.Node                 { return nil }
