package load

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
