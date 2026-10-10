package load

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// transpileErrors transpiles the module at root and returns its errors, with
// paths relative to the module's parent directory.
func transpileErrors(t *testing.T, root, modPath string) []string {
	t.Helper()
	pkgs, err := Discover(root, modPath, root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Transpile(pkgs, t.TempDir(), Options{})
	var list transpile.ErrorList
	if !errors.As(err, &list) {
		t.Fatalf("want an error list, got %v", err)
	}
	parent := filepath.Dir(realPath(root)) + string(filepath.Separator)
	var out []string
	for _, e := range list {
		out = append(out, strings.ReplaceAll(e.Error(), parent, ""))
	}
	return out
}

// depModule writes a module whose dependency example.com/dep, a directory
// replace, has a file dep.go with src, and returns its root. The app imports
// dep from main.vuka; a healthy Vuka package (views), a healthy Go package
// (ok) and a Go package only importing dep (mid) sit beside it.
func depModule(t *testing.T, depMod, src string) string {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "dep", "go.mod"), depMod)
	write(t, filepath.Join(dir, "dep", "dep.go"), src)
	root := filepath.Join(dir, "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/vuka-lang/vuka v0.0.0\n\texample.com/dep v0.0.0\n)\n\n"+
		"replace github.com/vuka-lang/vuka => "+repo+"\n\nreplace example.com/dep => ../dep\n")
	write(t, filepath.Join(root, "main.vuka"), `package main

import (
	"fmt"

	"example.com/app/mid"
	"example.com/app/ok"
	"example.com/app/views"
	"example.com/dep"
)

func run() Result[int] {
	n := dep.Load()?
	t := views.Title()?
	mid.M()
	fmt.Println(t, ok.K())
	return Ok(n)
}

func main() { fmt.Println(run()) }
`)
	write(t, filepath.Join(root, "views", "views.vuka"), "package views\n\nfunc Title() Result[string] { return Ok(\"t\") }\n")
	write(t, filepath.Join(root, "ok", "ok.go"), "package ok\n\nfunc K() int { return 1 }\n")
	write(t, filepath.Join(root, "mid", "mid.go"), "package mid\n\nimport \"example.com/dep\"\n\nfunc M() { _, _ = dep.Load() }\n")
	return root
}

func TestDependencyDoesNotCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := depModule(t, "module example.com/dep\n\ngo 1.23\n",
		"package dep\n\nfunc Load() (int, error) { return missing, nil }\n")
	got := transpileErrors(t, root, "example.com/app")
	want := []string{
		"app/main.vuka:6:2: example.com/app/mid depends on example.com/dep, which doesn't compile",
		"app/main.vuka:9:2: example.com/dep doesn't compile: dep/dep.go:3:35: undefined: missing",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestDependencyForOlderVuka(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	repo, _ := filepath.Abs("../..")
	root := depModule(t, "module example.com/dep\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => "+repo+"\n",
		"package dep\n\nimport \"github.com/vuka-lang/vuka\"\n\nvar root vuka.Node\n\nfunc Load() (int, error) { return 1, nil }\n")
	got := transpileErrors(t, root, "example.com/app")
	want := "app/main.vuka:9:2: example.com/dep doesn't compile: dep/dep.go:5:15: undefined: vuka.Node; " +
		"dep v0.0.0 was built for an older Vuka; upgrade it (go get example.com/dep@latest)"
	if len(got) != 2 || got[1] != want {
		t.Errorf("got\n%s\nwant (second)\n%s", strings.Join(got, "\n"), want)
	}
}

func TestFailureMessages(t *testing.T) {
	im := NewImporter("/w/app", "")
	im.mods["example.com/web"] = &goModule{Path: "example.com/web", Version: "v0.3.0"}
	im.mods["example.com/app/views"] = &goModule{Path: "example.com/app", Main: true}
	compile := &goError{Err: "# example.com/web\n../web/app.go:21:10: undefined: vuka.Node\n../web/app.go:30:2: undefined: vuka.Text\n"}
	for _, c := range []struct {
		path string
		own  *goError
		deps []*goError
		want string
	}{
		{"example.com/web", compile, nil, "example.com/web doesn't compile: /w/web/app.go:21:10: undefined: vuka.Node (and 1 more); web v0.3.0 was built for an older Vuka; upgrade it (go get example.com/web@latest)"},
		{"example.com/app/data", nil, []*goError{compile}, "example.com/app/data depends on example.com/web, which doesn't compile: /w/web/app.go:21:10: undefined: vuka.Node (and 1 more); web v0.3.0 was built for an older Vuka; upgrade it (go get example.com/web@latest)"},
		{"example.com/app/views", &goError{Err: "# example.com/app/views\nv.go:1:2: undefined: vuka.Node\n"}, nil, "example.com/app/views doesn't compile: /w/app/v.go:1:2: undefined: vuka.Node; it was written for an older Vuka; run vuka fix ui"},
		{"example.com/web", nil, []*goError{{ImportStack: []string{"example.com/web"}, Pos: "/m/web@v0.3.0/live.go:20:2", Err: "no required module provides package github.com/vuka-lang/vuka/live"}},
			"example.com/web doesn't compile: /m/web@v0.3.0/live.go:20:2: no required module provides package github.com/vuka-lang/vuka/live; web v0.3.0 was built for an older Vuka; upgrade it (go get example.com/web@latest)"},
		{"nope", &goError{Err: `no required module provides package "nope"`}, nil, `no required module provides package "nope"`},
	} {
		if got := im.failure(c.path, c.own, c.deps); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.path, got, c.want)
		}
	}
}
