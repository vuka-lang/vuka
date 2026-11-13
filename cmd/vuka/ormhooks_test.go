package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignatureText(t *testing.T) {
	t.Parallel()
	paths := map[string]string{"orm": ormModule, "db": ormModule, "context": "context"}
	for _, c := range []struct{ sig, want string }{
		{"() []orm.Index", "() []" + ormModule + ".Index"},
		{"() []db.Index", "() []" + ormModule + ".Index"},
		{"(ctx context.Context) error", "(context.Context) error"},
		{"(a, b int, rest ...orm.Expr) (map[string]orm.Expr, error)", "(int, int, ..." + ormModule + ".Expr) (map[string]" + ormModule + ".Expr, error)"},
	} {
		if got, ok := signatureText(c.sig, func(q string) string { return paths[q] }); !ok || got != c.want {
			t.Errorf("signatureText(%q) = %q, %v, want %q", c.sig, got, ok, c.want)
		}
	}
	if _, ok := signatureText("not a signature", func(string) string { return "" }); ok {
		t.Error("a non-signature parsed")
	}
}

// TestOrmHookDiags: a model's method named like a hook, with another
// signature than the orm the module requires declares, is reported; the
// hooks' own signatures, with either receiver, and methods of types that
// aren't models are not.
func TestOrmHookDiags(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	orm := ormDir(t)
	if src, _ := os.ReadFile(filepath.Join(orm, "meta.go")); !bytes.Contains(src, []byte("IndexesDeclarer")) {
		needs(t, "the hook checks need github.com/vuka-lang/orm/v2 with orm.IndexesDeclarer at %s (or $VUKA_ORM)", orm)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n\nrequire "+ormModule+" v2.0.0\n\nreplace "+ormModule+" => "+orm+"\n"), 0o644)
	user := `package models

import (
	"context"

	db "github.com/vuka-lang/orm/v2"
)

type User struct {
	db.Base @db.Meta{Table: "users"}
	ID    int64
	Email string
}

func (User) Indexes() []db.IndexKey { return nil }

func (u *User) Checks() []db.RowCheck { return nil }

func (u *User) BeforeSave() {}

func (User) Validate(ctx context.Context) error { return nil }

type Stamps struct{ Created int64 }

func (Stamps) Indexes() []string { return nil }
`
	post := `package models

import "github.com/vuka-lang/orm/v2"

func (p Post) Generated() map[string]orm.Expr { return nil }

func (p *Post) AfterSave() error { return nil }
`
	other := `package models

import "github.com/vuka-lang/orm/v2"

type Post struct {
	orm.Model[Post]
	ID int64
}
`
	var got []string
	for _, d := range ormHookDiags(root, "user.vuka", [][]byte{[]byte(user), []byte(other)}) {
		got = append(got, d.Message)
	}
	for _, d := range ormHookDiags(root, "post.vuka", [][]byte{[]byte(post), []byte(other)}) {
		got = append(got, d.Message)
	}
	want := []string{
		"User.Indexes looks like an ORM hook, but its signature is func (User) Indexes() []db.IndexKey; it must be func (User) Indexes() []db.Index: the ORM ignores it otherwise",
		"User.BeforeSave looks like an ORM hook, but its signature is func (*User) BeforeSave(); it must be func (*User) BeforeSave(context.Context) error: the ORM ignores it otherwise",
		"Post.AfterSave looks like an ORM hook, but its signature is func (*Post) AfterSave() error; it must be func (*Post) AfterSave(context.Context) error: the ORM ignores it otherwise",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
