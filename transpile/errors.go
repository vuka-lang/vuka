package transpile

import (
	"fmt"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// Error is a problem in a source file, positioned in the file as written,
// with its code and what to do about it.
type Error struct {
	Pos     token.Position
	Msg     string
	Code    diag.Code
	Help    string // the fix, when the message doesn't say it
	Suggest string // did you mean …
	Len     int    // bytes the problem spans from Pos, 0 for one token
	// Fix is the source text replacing the Len bytes at Pos, for a
	// warning with one rewrite (W0402: perms.Has("x") for auth.Can(ctx, "x")).
	Fix string
	// Related are the other places the error is about (the other uses of
	// a package a file doesn't import).
	Related []token.Position
	// Edits fix it, as the action FixTitle: offsets in Pos's file.
	Edits    []Edit
	FixTitle string

	knockOn bool // a knock-on of an import that didn't load (knockon.go)
}

func (e *Error) Error() string { return e.Pos.String() + ": " + e.Text() }

// Text is the message with its help and suggestion, as one line.
func (e *Error) Text() string {
	s := e.Msg
	if e.Help != "" {
		s += "; " + e.Help
	}
	if e.Suggest != "" {
		s += "; did you mean " + e.Suggest + "?"
	}
	return s
}

// Diagnostic is e as a diagnostic, its file relative to root ("" keeps it).
func (e *Error) Diagnostic(root string) diag.Diagnostic {
	file := e.Pos.Filename
	if root != "" && filepath.IsAbs(file) {
		if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
	}
	sp := diag.Span{File: file, Line: e.Pos.Line, Col: e.Pos.Column}
	if e.Len > 0 && sp.Col > 0 {
		sp.EndCol = sp.Col + e.Len
	}
	d := diag.Diagnostic{Code: e.Code, Message: e.Msg, Span: sp, Help: e.Help, Suggest: e.Suggest}
	for _, r := range e.Related {
		rf := r.Filename
		if root != "" && filepath.IsAbs(rf) {
			if rel, err := filepath.Rel(root, rf); err == nil && !strings.HasPrefix(rel, "..") {
				rf = filepath.ToSlash(rel)
			}
		}
		d.Related = append(d.Related, diag.Span{File: rf, Line: r.Line, Col: r.Column})
	}
	return d
}

// Edit replaces the bytes from Off to End of a file with Text.
type Edit struct {
	Off, End int
	Text     string
}

func (e *Error) help(format string, args ...any) *Error {
	e.Help = fmt.Sprintf(format, args...)
	return e
}

func (e *Error) suggest(s string) *Error {
	e.Suggest = s
	return e
}

// ErrorList is every problem found in a package, sorted by position.
type ErrorList []*Error

func (l ErrorList) Error() string {
	var b strings.Builder
	for i, e := range l {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Error())
	}
	return b.String()
}

func (l *ErrorList) add(code diag.Code, pos token.Position, format string, args ...any) *Error {
	e := &Error{Pos: pos, Msg: fmt.Sprintf(format, args...), Code: code}
	*l = append(*l, e)
	return e
}

// Diagnostics is the list as diagnostics, files as written.
func (l ErrorList) Diagnostics() []diag.Diagnostic {
	out := make([]diag.Diagnostic, len(l))
	for i, e := range l {
		out[i] = e.Diagnostic("")
	}
	return out
}

func (l ErrorList) err() error {
	if len(l) == 0 {
		return nil
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i].Pos, l[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return l
}
