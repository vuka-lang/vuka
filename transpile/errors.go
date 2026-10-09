package transpile

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
)

// Error is a problem in a source file, positioned in the file as written.
type Error struct {
	Pos token.Position
	Msg string
}

func (e *Error) Error() string { return e.Pos.String() + ": " + e.Msg }

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

func (l *ErrorList) add(pos token.Position, format string, args ...any) {
	*l = append(*l, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
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
