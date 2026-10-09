package load

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestStaticsAcrossPackages is the Django shape: an orm package declares the
// base model once; models embed it without type arguments; another package
// calls models.User.Objects.All(ctx) and u.Save(ctx).
func TestStaticsAcrossPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	repo, _ := filepath.Abs("../..")
	root := filepath.Join(t.TempDir(), "shop")
	write(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => "+repo+"\n")
	write(t, filepath.Join(root, "orm", "orm.vuka"), `package orm

import "context"

type Manager[T any] struct{ rows []*T }

func (m *Manager[T]) All(ctx context.Context) []*T { return m.rows }

type Model[Self any] struct {
	static Objects = Manager[Self]{}
}

func (m *Model[Self]) Save(self *Self, ctx context.Context) error {
	objs := &Model[Self].Objects
	objs.rows = append(objs.rows, self)
	return nil
}
`)
	write(t, filepath.Join(root, "models", "models.vuka"), `package models

import "example.com/shop/orm"

type User struct {
	orm.Model
	Name string

	static Table = "users"
}

func User.New(name string) *User { return &User{Name: name} }
`)
	write(t, filepath.Join(root, "main.vuka"), `package main

import (
	"context"
	"fmt"

	"example.com/shop/models"
)

func main() {
	ctx := context.Background()
	for _, n := range []string{"ada", "linus"} {
		u := models.User.New(n)
		if err := u.Save(ctx); err != nil {
			panic(err)
		}
	}
	for _, u := range models.User.Objects.All(ctx) {
		fmt.Println(models.User.Table, u.Name)
	}
}
`)
	pkgs, err := Discover(root, "example.com/shop", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	_, overlay, err := Transpile(pkgs, tmp, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command("go", "run", "-overlay="+overlay, ".")
	c.Dir = root
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if got, want := string(out), "users ada\nusers linus\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
