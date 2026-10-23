package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const ctxMain = `package main

import (
	"example.com/app/pages"
	"example.com/app/users"
	"github.com/vuka-lang/web/v2"
)

func main() {
	app := web.New(
		pages.Routes(),
		web.ContextProcessors(web.RequestContext, web.CSRFContext, web.MessagesContext, users.Context),
	)
	app.Service(users.NewService)
	app.Run("")
}
`

const ctxUsers = `package users

import "context"

type User struct{ Name string }

type Service struct{ name string }

func NewService() *Service { return &Service{name: "Ada"} }

// Vars are Context's variables: me.
type Vars struct{ Me *User }

// Context is the app's own processor, its Service from the container.
func Context(ctx context.Context, s *Service) (Vars, error) {
	return Vars{Me: &User{Name: s.name}}, nil
}
`

const ctxBadge = `<script lang="go">
package components

// Badge reads the app's processor's variable by name, in a package of
// components main imports through the pages.
type Badge struct{ Label string }
</script>

<b id="badge">{Label}: {me.Name}</b>
`

const ctxLayout = `<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

// Layout reads the request in its markup.
type Layout struct{ Children ui.Node }
</script>

<html>
  <body>
    <nav id="path">{request.Path}</nav>
    {Children}
  </body>
</html>
`

const ctxIndex = `<script lang="go">
package pages

import "example.com/app/components"

// Index reads web's processors' variables and the app's.
type Index struct{}
</script>

<main>
  <p id="me">{me.Name}</p>
  if csrfToken != "" {
    <p id="csrf">token</p>
  }
  <p id="messages">{len(messages)}</p>
  <components.Badge label="signed in" />
</main>
`

// TestContextProcessorsE2E: an app listing web's processors and one of its
// own builds, and its pages, layout and a component read their variables
// by name.
func TestContextProcessorsE2E(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "ContextProcessors") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no ContextProcessors; set VUKA_WEB (and VUKA_UI)", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":                    ctxMain,
		"users/users.go":             ctxUsers,
		"components/badge.vuka.html": ctxBadge,
		"pages/_layout.vuka.html":    ctxLayout,
		"pages/index.vuka.html":      ctxIndex,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), assetsEnv+"=")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/"); return err == nil && res.Body.Close() == nil })
	res, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{`<nav id="path">/</nav>`, `<p id="me">Ada</p>`, `<p id="csrf">token</p>`, `<p id="messages">0</p>`, `<b id="badge">signed in: Ada</b>`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the page has no %s:\n%s\napp log:\n%s", want, body, logs.String())
		}
	}
}

const ctxFolderMain = `package main

import (
	"example.com/app/nav"
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

func main() {
	app := web.New(
		pages.Routes(),
		web.ContextProcessors(web.RequestContext, auth.Context, nav.Context),
	)
	app.Run("")
}
`

// ctxNav is an app processor whose package imports comp, a package of
// shapes the pages import too.
const ctxNav = `package nav

import (
	"context"

	"example.com/app/comp"
)

// Vars are Context's variables: badges.
type Vars struct{ Badges []comp.Badge }

func Context(ctx context.Context) (Vars, error) {
	return Vars{Badges: []comp.Badge{{Label: "new"}}}, nil
}
`

// ctxCompBadge reads web's processor's variable: nav's would be an import
// cycle.
const ctxCompBadge = `<script lang="go">
package comp

// Badge is a shape of the package nav imports.
type Badge struct{ Label string }
</script>

<b class="badge" data-path={request.Path}>{Label}</b>
`

const ctxHome = `<script lang="go">
package home

import "example.com/app/comp"

// Index is GET /home.
type Index struct{}

var _ comp.Badge
</script>

<main>
  <p id="path">{request.Path}</p>
  if !perms.Has("admin") {
    <p id="perms">no admin</p>
  }
  for _, b := range badges {
    <comp.Badge label={b.Label} />
  }
</main>
`

const ctxGroup = `<script lang="go">
package panel

// Index is GET /panel: the group (admin) is no part of its path.
type Index struct{}
</script>

<p id="group">{request.Path} {len(badges)}</p>
`

const ctxItem = `<script lang="go">
package item

// Index is GET /items/{id}.
type Index struct{ ID string }
</script>

<p id="item">{request.Params["id"]} {len(perms)}</p>
`

// TestContextProcessorsFolderPagesE2E: folder pages — a plain folder, a
// route group's and a [id] folder's — read web's, auth's and the app's
// processors' variables by name, though main reaches them only through
// the generated pages.Routes; the app's processor imports a package of
// shapes the pages use.
func TestContextProcessorsFolderPagesE2E(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "ContextProcessors") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no ContextProcessors; set VUKA_WEB (and VUKA_UI)", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":                             ctxFolderMain,
		"nav/context.go":                      ctxNav,
		"comp/badge.vuka.html":                ctxCompBadge,
		"pages/home/index.vuka.html":          ctxHome,
		"pages/(admin)/panel/index.vuka.html": ctxGroup,
		"pages/items/[id]/index.vuka.html":    ctxItem,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), assetsEnv+"=")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/home"); return err == nil && res.Body.Close() == nil })
	for path, wants := range map[string][]string{
		"/home":     {`<p id="path">/home</p>`, `<p id="perms">no admin</p>`, `data-path="/home"`, `>new</b>`},
		"/panel":    {`<p id="group">/panel 1</p>`},
		"/items/42": {`<p id="item">42 0</p>`},
	} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		for _, want := range wants {
			if !strings.Contains(string(body), want) {
				t.Errorf("%s has no %s:\n%s\napp log:\n%s", path, want, body, logs.String())
			}
		}
	}
}
