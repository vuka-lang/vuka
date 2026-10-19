package main

import (
	"context"
	"strings"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// NotePage is a note's page: its links patch it to the next note, its
// form edits the reply, its button navigates back to the board. Its markup
// is note_page.vuka.html.
type NotePage struct {
	ui.Live
	ID      int
	Reply   string
	note    Note
	replies []string
	patches int
	notes   *Notes
}

func NewNotePage(notes *Notes) *NotePage { return &NotePage{notes: notes} }

func (p *NotePage) LiveSchema(l *ui.LiveSchema) {
	l.Field(&p.Reply, ui.Bind{})
	l.Mount(p.mount)
}

// NoteParams is a patch's.
type NoteParams struct {
	web.URLParams
	ID int
}

// ReplyForm is the reply form's.
type ReplyForm struct {
	Text string `form:"text" validate:"required,max=20"`
}

func (p *NotePage) mount(ctx context.Context) error { return p.show(p.ID) }

func (p *NotePage) show(id int) error {
	n, ok := p.notes.Get(id)
	if !ok {
		return web.NotFound
	}
	p.ID, p.note = id, n
	return nil
}

// UpdateNoteParams takes a patch to another note.
func (p *NotePage) UpdateNoteParams(q NoteParams) error {
	p.patches++
	return p.show(q.ID)
}

// Send adds a reply.
func (p *NotePage) Send(ctx context.Context, f ReplyForm) error {
	if strings.Contains(f.Text, "!") {
		return web.Invalid{"text": "no shouting"}
	}
	p.replies = append(p.replies, f.Text)
	p.Reply = ""
	return nil
}

// Back navigates to the board.
func (p *NotePage) Back() { p.PushNavigate("/board?q=" + p.note.Text[:1]) }
