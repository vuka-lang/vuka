package main

import (
	"context"
	"errors"

	"github.com/vuka-lang/orm"
	_ "github.com/vuka-lang/orm/sqlite"
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/ui/validate"
	"github.com/vuka-lang/web"
)

//go:generate go run github.com/vuka-lang/orm/cmd/orm generate

// User is a member of the directory, a row of its table.
type User struct {
	orm.Model[User]
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (u *User) Schema(s *orm.Schema) {
	s.Field(&u.Name, orm.Char{Max: 50})
}

// Directory holds the users: a service, built once, on a database in
// memory.
type Directory struct{ users *orm.Manager[User] }

func NewDirectory() (*Directory, error) {
	ctx := context.Background()
	db, err := orm.ConnectURL("sqlite::memory:")
	if err != nil {
		return nil, err
	}
	orm.AddDatabase("main", db, orm.AsDefault())
	d := &Directory{users: orm.Objects[User]()}
	if err := orm.CreateTables(ctx, d.users); err != nil {
		return nil, err
	}
	for _, name := range []string{"Ada", "Grace", "Linus"} {
		u := User{Name: name}
		if err := u.Save(ctx); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *Directory) find(ctx context.Context, id int64) (User, error) {
	u, err := d.users.Get(ctx, UserFields.ID.Eq(id))
	if errors.Is(err, orm.ErrNotFound) {
		return u, web.NotFound
	}
	return u, err
}

// ListUsers answers GET /api/users.
func ListUsers(ctx context.Context, dir *Directory) ([]User, error) {
	return dir.users.OrderBy(UserFields.ID.Asc()).All(ctx)
}

// ShowUser answers GET /api/users/{id}.
func ShowUser(ctx context.Context, id int64, dir *Directory) (User, error) { return dir.find(ctx, id) }

// Dashboard is a live page.
type Dashboard struct {
	ui.Live
	dir   *Directory
	users int64
	likes int
}

func NewDashboard(dir *Directory) *Dashboard { return &Dashboard{dir: dir} }

func (d *Dashboard) LiveSchema(l *ui.LiveSchema) {
	l.Mount(d.mount)
}

func (d *Dashboard) mount(ctx context.Context) (err error) {
	d.users, err = d.dir.users.Count(ctx)
	return err
}

// Like is the button's event.
func (d *Dashboard) Like() { d.likes++ }

func (d *Dashboard) Render() ui.Node {
	return ui.HTML(`
<main>
  <h1>Users</h1>
  <p>{d.users} users, {d.likes} likes</p>
  <button onClick={d.Like}>Like</button>
</main>`, d.users, d.likes, d.Like)
}

// Profile is a live page backed by its island, islands/Profile.ts: its
// exported fields are the component's props, Name one it may set.
type Profile struct {
	ui.Live
	ID   int64  `json:"id"`
	Name string `json:"name"`
	dir  *Directory
}

func NewProfile(dir *Directory) *Profile { return &Profile{dir: dir} }

func (p *Profile) LiveSchema(l *ui.LiveSchema) {
	l.Field(&p.Name, ui.Bind{}, validate.Required{}, validate.Max(50))
	l.Mount(p.mount)
}

func (p *Profile) mount(ctx context.Context) error {
	u, err := p.dir.find(ctx, p.ID)
	p.Name = u.Name
	return err
}

func (p *Profile) Render() ui.Node { return ui.IslandPage(p) }

// page is the profile's layout.
func page(content ui.Node) ui.Node {
	return ui.HTML(`
<html><body>
  <h1>Profile</h1>
  {content}
</body></html>`, content)
}
