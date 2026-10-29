package transpile

import (
	"strings"
	"testing"
)

// TestDeclared: a file's top-level names come from its tokens, whether or
// not it transpiles.
func TestDeclared(t *testing.T) {
	src := `package users

import "context"

// Account is a user's sign-in.
type Account struct {
	ID int64
}

type (
	// Role is what a user may do.
	Role  string
	Posts []string
)

var Default, other = &Account{}, 1

const (
	Limit = 3
	a, b  = 1, 2
)

enum Status {
	// Active is signed in.
	Active
	Blocked{Reason string}
}

@graphql.Query
func Next(ctx context.Context, id int64) int64 {
	type inner int
	var x = 1
	return id + int64(x)
}

func (a *Account) Domain() string { return "" }

func (l List[T]) Len() int { return 0 }

func status(r Result[int]) int {
	match r {
	case Ok(n):
		return n
	}
	return 0
}
`
	want := "Account Role Posts Default other Limit a b Status StatusValues ParseStatus StatusActive StatusBlocked Next Account.Domain List.Len status"
	if got := strings.Join(Declared("users.vuka", []byte(src)), " "); got != want {
		t.Errorf("declared:\n%s\nwant:\n%s", got, want)
	}
	tmpl := "<script lang=\"go\">\npackage users\n\ntype Index struct{ n int }\n\nfunc (p *Index) Add() { p.n++ }\n</script>\n\n<main>{n}</main>\n"
	if got := strings.Join(Declared("index.vuka.html", []byte(tmpl)), " "); got != "Index Index.Add" {
		t.Errorf("template declares %s", got)
	}
}
