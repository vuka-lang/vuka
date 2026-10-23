package load

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// contextModule writes an app whose main lists web's and auth's context
// processors and users.Context, a Vuka package of the app's: views reads
// their variables by name; lib, a module of its own, reads one with
// web.Var.
func contextModule(t *testing.T) string {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	web := filepath.Join(repo, "transpile", "testdata", "golden", "webretired")
	root := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/vuka-lang/vuka/v2 v2.0.0\n\tgithub.com/vuka-lang/web/v2 v2.0.0\n)\n\n"+
		"replace github.com/vuka-lang/vuka/v2 => "+repo+"\n\nreplace github.com/vuka-lang/web/v2 => "+web+"\n")
	write(t, filepath.Join(root, "main.go"), `package main

import (
	"example.com/app/users"
	"example.com/app/views"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

func main() {
	web.New(views.Routes(), web.ContextProcessors(web.RequestContext, auth.Context, users.Context))
}
`)
	write(t, filepath.Join(root, "users", "context.vuka"), `package users

import "context"

type User struct{ Name string }

type Vars struct{ Me *User }

func Context(ctx context.Context) (Vars, error) { return Vars{Me: &User{Name: "Ada"}}, nil }
`)
	write(t, filepath.Join(root, "views", "views.vuka"), `package views

import "context"

func Routes() int { return 0 }

func Greeting(ctx context.Context) string {
	if user == nil {
		return request.Path
	}
	return me.Name
}
`)
	write(t, filepath.Join(root, "lib", "go.mod"), "module example.com/lib\n\ngo 1.23\n")
	write(t, filepath.Join(root, "lib", "lib.vuka"), "package lib\n\nimport \"context\"\n\nfunc Name(ctx context.Context) string { return me.Name }\n")
	return root
}

func TestContexts(t *testing.T) {
	root := contextModule(t)
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := Contexts(root, "example.com/app", pkgs, nil)
	views := cs["example.com/app/views"]
	if len(views) != 1 || views[0].Main != "example.com/app" || len(views[0].Processors) != 3 {
		t.Fatalf("views' contexts: %+v", views)
	}
	p := views[0].Processors[2]
	if p.Path != "example.com/app/users" || p.Func != "Context" || p.Pos.Line != 11 || filepath.Base(p.Pos.Filename) != "main.go" {
		t.Errorf("users.Context: %+v", p)
	}
	sess, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Session{nil, sess} {
		var gens []Generated
		if s == nil {
			gens, _, err = Transpile(pkgs, t.TempDir(), Options{})
		} else {
			gens, _, err = s.Transpile(pkgs, t.TempDir(), Options{})
		}
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, g := range gens {
			if filepath.Base(g.Target) == "views_vuka.go" {
				got = string(g.Src)
			}
		}
		for _, want := range []string{`web.Var[*users.User](ctx, "me").Name`, `web.Var[*auth.Identity](ctx, "user") == nil`, `web.Var[web.RequestInfo](ctx, "request").Path`} {
			if !strings.Contains(got, want) {
				t.Errorf("views' Go has no %s:\n%s", want, got)
			}
		}
	}
}

// TestContextsMains: a package two mains link in reads a variable both
// list; one only the first lists is an error naming the other.
func TestContextsMains(t *testing.T) {
	root := contextModule(t)
	write(t, filepath.Join(root, "cmd", "admin", "main.go"), `package main

import (
	"example.com/app/users"
	_ "example.com/app/views"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(web.ContextProcessors(web.RequestContext, users.Context)) }
`)
	errs := transpileErrors(t, root, "example.com/app")
	if len(errs) != 1 || !strings.Contains(errs[0], "views.vuka:8:5: E0422") && !strings.Contains(errs[0], "views.vuka:8:5:") ||
		!strings.Contains(errs[0], "example.com/app/cmd/admin doesn't list auth.Context") {
		t.Fatalf("errors: %q", errs)
	}
	// A main listing none renders no page reading them.
	os.WriteFile(filepath.Join(root, "cmd", "admin", "main.go"), []byte("package main\n\nimport _ \"example.com/app/views\"\n\nfunc main() {}\n"), 0o644)
	pkgs, _ := Discover(root, "example.com/app", root, true, nil)
	if _, _, err := Transpile(pkgs, t.TempDir(), Options{}); err != nil {
		t.Fatal(err)
	}
}

// TestContextsLibrary: a package of another module isn't linked in by
// the app's mains: a bare name there is undefined; it reads web.Var.
func TestContextsLibrary(t *testing.T) {
	root := contextModule(t)
	lib := filepath.Join(root, "lib")
	pkgs, err := Discover(lib, "example.com/lib", lib, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cs := Contexts(lib, "example.com/lib", pkgs, nil); len(cs) != 0 {
		t.Errorf("the library's contexts: %+v", cs)
	}
	if cs := Contexts(root, "example.com/app", pkgs, nil); len(cs) != 0 {
		t.Errorf("the library's contexts in the app: %+v", cs)
	}
}

// procModule writes an app of files beside a go.mod requiring the web
// stand-in.
func procModule(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	web := filepath.Join(repo, "transpile", "testdata", "golden", "webretired")
	root := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/vuka-lang/vuka/v2 v2.0.0\n\tgithub.com/vuka-lang/web/v2 v2.0.0\n)\n\n"+
		"replace github.com/vuka-lang/vuka/v2 => "+repo+"\n\nreplace github.com/vuka-lang/web/v2 => "+web+"\n")
	for name, src := range files {
		write(t, filepath.Join(root, filepath.FromSlash(name)), src)
	}
	return root
}

// TestContextsLinks: a package main reaches only through another's
// generated Go (Links: pages/route.go's page packages) reads the
// variables, though the linking package has no file on disk.
func TestContextsLinks(t *testing.T) {
	root := procModule(t, map[string]string{
		"main.go": `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes(), web.ContextProcessors(web.RequestContext)) }
`,
		"pages/home/home.vuka": "package home\n\nimport \"context\"\n\nfunc Path(ctx context.Context) string { return request.Path }\n",
	})
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cs := Contexts(root, "example.com/app", pkgs, nil); len(cs["example.com/app/pages/home"]) != 0 {
		t.Fatalf("home is linked in: %+v", cs)
	}
	routes := &Package{Dir: filepath.Join(root, "pages"), ImportPath: "example.com/app/pages", Name: "pages",
		Files: []transpile.File{{Name: "route.vuka", Src: []byte("package pages\n\nfunc Routes() int { return 0 }\n")}},
		Links: []string{"example.com/app/pages/home"}}
	pkgs = append(pkgs, routes)
	cs := Contexts(root, "example.com/app", pkgs, nil)
	if home := cs["example.com/app/pages/home"]; len(home) != 1 || len(home[0].Processors) != 1 {
		t.Fatalf("home's contexts: %+v", cs)
	}
	gens, _, err := Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := genOf(gens, "home_vuka.go"); !strings.Contains(got, `web.Var[web.RequestInfo](ctx, "request").Path`) {
		t.Errorf("home's Go:\n%s", got)
	}
}

// genOf is the generated file named base.
func genOf(gens []Generated, base string) string {
	for _, g := range gens {
		if filepath.Base(g.Target) == base {
			return string(g.Src)
		}
	}
	return ""
}

// TestContextsProcessorImports: an app processor's package imports comp,
// a Vuka package others import too: comp doesn't get that processor (it
// would be an import cycle), still reads web's, and transpiles before the
// processor's package; views reads both.
func TestContextsProcessorImports(t *testing.T) {
	root := procModule(t, map[string]string{
		"main.go": `package main

import (
	"example.com/app/nav"
	_ "example.com/app/views"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(web.ContextProcessors(web.RequestContext, nav.Context)) }
`,
		"nav/context.go": `package nav

import (
	"context"

	"example.com/app/comp"
)

type Vars struct{ Badges []comp.Badge }

func Context(ctx context.Context) (Vars, error) { return Vars{}, nil }
`,
		"comp/comp.vuka": "package comp\n\nimport \"context\"\n\ntype Badge struct{ Label string }\n\nfunc Here(ctx context.Context) string { return request.Path }\n",
		"views/views.vuka": `package views

import (
	"context"

	"example.com/app/comp"
)

func Count(ctx context.Context) int { return len(badges) + len(comp.Here(ctx)) }
`,
	})
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := Contexts(root, "example.com/app", pkgs, nil)
	if comp := cs["example.com/app/comp"]; len(comp) != 1 || len(comp[0].Processors) != 1 || comp[0].Processors[0].Func != "RequestContext" {
		t.Errorf("comp's contexts: %+v", comp)
	}
	if views := cs["example.com/app/views"]; len(views) != 1 || len(views[0].Processors) != 2 {
		t.Errorf("views' contexts: %+v", views)
	}
	sess, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Session{nil, sess} {
		var gens []Generated
		if s == nil {
			gens, _, err = Transpile(pkgs, t.TempDir(), Options{})
		} else {
			gens, _, err = s.Transpile(pkgs, t.TempDir(), Options{})
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := genOf(gens, "views_vuka.go"); !strings.Contains(got, `web.Var[[]comp.Badge](ctx, "badges")`) {
			t.Errorf("views' Go:\n%s", got)
		}
		if got := genOf(gens, "comp_vuka.go"); !strings.Contains(got, `web.Var[web.RequestInfo](ctx, "request").Path`) {
			t.Errorf("comp's Go:\n%s", got)
		}
	}
}

// TestContextsProcessorCycle: a reads q's variables, q imports r, r reads
// s's, and s imports a: no order transpiles each package after the
// processors it reads, E0428 at the entry in main.
func TestContextsProcessorCycle(t *testing.T) {
	root := procModule(t, map[string]string{
		"main.go": `package main

import (
	_ "example.com/app/a"
	"example.com/app/q"
	_ "example.com/app/r"
	"example.com/app/s"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(web.ContextProcessors(q.Context, s.Context)) }
`,
		"q/q.vuka": "package q\n\nimport (\n\t\"context\"\n\n\t\"example.com/app/r\"\n)\n\ntype Vars struct{ QV int }\n\nfunc Context(ctx context.Context) (Vars, error) { return Vars{QV: r.X}, nil }\n",
		"r/r.vuka": "package r\n\nimport \"context\"\n\nvar X = 1\n\nfunc R(ctx context.Context) int { return sv }\n",
		"s/s.vuka": "package s\n\nimport (\n\t\"context\"\n\n\t\"example.com/app/a\"\n)\n\ntype Vars struct{ SV int }\n\nfunc Context(ctx context.Context) (Vars, error) { return Vars{SV: a.N}, nil }\n",
		"a/a.vuka": "package a\n\nimport \"context\"\n\nvar N = 1\n\nfunc A(ctx context.Context) int { return qv }\n",
	})
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Transpile(pkgs, t.TempDir(), Options{})
	var list transpile.ErrorList
	if !errors.As(err, &list) || len(list) != 1 {
		t.Fatalf("want one error, got %v", err)
	}
	e := list[0]
	if e.Code != "E0428" || filepath.Base(e.Pos.Filename) != "main.go" || e.Pos.Line != 11 ||
		!strings.Contains(e.Msg, "example.com/app/a reads the variables of q.Context by name") || !strings.Contains(e.Help, "example.com/app/s -> example.com/app/a") {
		t.Fatalf("error: %+v", e)
	}
}
