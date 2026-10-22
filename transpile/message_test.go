package transpile_test

import (
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestMessage checks that compiler messages quote rewritten code as the
// source spells it, telling apart references that read alike by their line.
func TestMessage(t *testing.T) {
	src := `package main

type User struct {
	Name, Email string
	static Table = "users"
}

var a = User.Name
var b = User.Email
var c = User.Table
`
	res, err := transpile.Package([]transpile.File{{Name: "m.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter(".", ""), Dir: "."})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Files[0].Map
	ref := "vuka.StringRefTo(func(x *User) *string {…})"
	for _, c := range []struct {
		line     int
		msg, out string
	}{
		{8, "cannot use " + ref + " as int", "cannot use User.Name as int"},
		{9, "cannot use " + ref + " as int", "cannot use User.Email as int"},
		{3, "cannot use " + ref + " as int", "cannot use " + ref + " as int"},
		{10, "UserTable + 1 (mismatched types)", "User.Table + 1 (mismatched types)"},
		{10, "UserTables, xUserTable", "UserTables, xUserTable"},
	} {
		if got := m.Message(c.line, c.msg); got != c.out {
			t.Errorf("line %d: %q\nwant %q", c.line, got, c.out)
		}
	}
}
