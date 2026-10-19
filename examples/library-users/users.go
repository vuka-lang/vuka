package main

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// User is a member of the directory.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Directory holds the users: a service, built once.
type Directory struct{ users []User }

func NewDirectory() *Directory {
	return &Directory{users: []User{{1, "Ada"}, {2, "Grace"}, {3, "Linus"}}}
}

func (d *Directory) find(id int) (User, bool) {
	for _, u := range d.users {
		if u.ID == id {
			return u, true
		}
	}
	return User{}, false
}

// ListUsers answers GET /api/users.
func ListUsers(dir *Directory) []User { return dir.users }

// ShowUser answers GET /api/users/{id}.
func ShowUser(id int, dir *Directory) (User, error) {
	if u, ok := dir.find(id); ok {
		return u, nil
	}
	return User{}, web.NotFound
}

// Dashboard is a live page.
type Dashboard struct {
	ui.Live
	dir   *Directory
	likes int
}

func NewDashboard(dir *Directory) *Dashboard { return &Dashboard{dir: dir} }

// Like is the button's event.
func (d *Dashboard) Like() { d.likes++ }

func (d *Dashboard) Render() ui.Node {
	return ui.HTML(`
<main>
  <h1>Users</h1>
  <p>{len(d.dir.users)} users, {d.likes} likes</p>
  <button onClick={d.Like}>Like</button>
</main>`, len(d.dir.users), d.likes, d.Like)
}

// Profile is a live page backed by its island, islands/Profile.ts: its
// exported fields are the component's props.
type Profile struct {
	ui.Live
	ID   int    `json:"id"`
	Name string `json:"name"`
	dir  *Directory
}

func NewProfile(dir *Directory) *Profile { return &Profile{dir: dir} }

func (p *Profile) Mount() error {
	u, ok := p.dir.find(p.ID)
	if !ok {
		return web.NotFound
	}
	p.Name = u.Name
	return nil
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
