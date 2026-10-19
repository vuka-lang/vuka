package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/ui/validate"
	"github.com/vuka-lang/web"
)

// Added is what the board broadcasts when a note is added.
type Added struct{ Note Note }

// Board is the notes, filtered by a query the URL keeps; its component is
// board.ts.
type Board struct {
	ui.Live
	Q     string `json:"q"`
	Rows  []Note `json:"rows"`
	Total int    `json:"total"`
	Seen  int    `json:"seen"`
	notes *Notes
}

func NewBoard(notes *Notes) *Board { return &Board{notes: notes} }

func (b *Board) LiveSchema(l *ui.LiveSchema) {
	l.Field(&b.Q, ui.Bind{Debounce: 200 * time.Millisecond}, ui.URL("q"), validate.Max(20))
	l.Field(&b.Rows, ui.Defer("notes"))
	l.Field(&b.Total, ui.Defer("notes"))
	l.Mount(b.mount)
	l.Loads("notes", b.loadNotes)
	l.OnChange(b.changed)
}

// BoardParams is what a patch of the board's URL brings.
type BoardParams struct {
	web.URLParams
	Q string
}

func (b *Board) mount(ctx context.Context) error {
	b.Subscribe("notes")
	return nil
}

func (b *Board) loadNotes(ctx context.Context) error {
	b.Rows, b.Total = b.notes.Matching(b.Q), b.notes.Count()
	return nil
}

func (b *Board) changed(ctx context.Context, c ui.Changed) error {
	b.Rows = b.notes.Matching(b.Q)
	return nil
}

// UpdateBoardParams takes a patch of the URL.
func (b *Board) UpdateBoardParams(p BoardParams) {
	b.Q = p.Q
	b.Rows = b.notes.Matching(b.Q)
}

// UpdateAdded takes a note a page added.
func (b *Board) UpdateAdded(a Added) {
	b.Seen++
	b.Rows = b.notes.Matching(b.Q)
	b.Total = b.notes.Count()
}

// Add adds a note, which every board hears of.
func (b *Board) Add(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return web.Invalid{"text": "required"}
	}
	return web.Broadcast(ctx, "notes", Added{b.notes.Add(text)})
}

// Open moves to a note's page.
func (b *Board) Open(id int) { b.PushNavigate(fmt.Sprint("/notes/", id)) }

func (b *Board) Render() ui.Node { return ui.IslandPage(b) }

// page is the board's layout.
func page(content ui.Node) ui.Node {
	return ui.HTML(`
<html>
  <head><title>Board</title></head>
  <body>
    <h1>Notes</h1>
    {content}
  </body>
</html>`, content)
}
