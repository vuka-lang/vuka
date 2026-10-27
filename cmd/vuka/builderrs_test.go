package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// webCheckout is the web checkout the tests build against ($VUKA_WEB, else
// ../vuka-web), the test failing under CI and skipping otherwise without it.
func webCheckout(t *testing.T) string {
	t.Helper()
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		repo, _ := filepath.Abs("../..")
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if _, err := os.Stat(filepath.Join(web, "go.mod")); err != nil {
		needs(t, "the build reports need github.com/vuka-lang/web/v2 checked out at %s (or $VUKA_WEB)", web)
	}
	return web
}

// brokenApp is an app whose layout calls a method without the context it
// takes, in its markup, and whose accounts module names typed fields that
// aren't there; the guards of three folders import both.
func brokenApp(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{
		"main.go":          "package main\n\nimport (\n\t\"example.com/app/pages\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc main() { web.New(pages.Routes()).Run(\"\") }\n",
		"entities/user.go": "package entities\n\ntype User struct {\n\tID    int64\n\tEmail string\n}\n",
		"modules/accounts/service.vuka": `package accounts

import (
	"context"

	"example.com/app/entities"
)

type Service struct{}

var Default = &Service{}

func (s *Service) Find(ctx context.Context, email string) (*entities.User, error) {
	_ = entities.UserFields
	return &entities.User{Email: email}, nil
}
`,
		"layouts/links.go": "package layouts\n\nimport \"context\"\n\ntype Links struct{}\n\nfunc (Links) HomePath(ctx context.Context) string { return \"/\" }\n\nvar a Links\n\nfunc Home() string { return a.HomePath(context.Background()) }\n",
		"layouts/main.vuka.html": `<script lang="go">
package layouts

import "github.com/vuka-lang/ui/v2"

// Main is the document of every page.
type Main struct {
	Children ui.Node
}
</script>

<html lang="en">
<head><meta charset="utf-8"/><title>App</title></head>
<body>
  <nav>
    <a href={a.HomePath()}>Home</a>
  </nav>
  <main>{Children}</main>
</body>
</html>
`,
	}
	for _, d := range []string{"account", "admin", "users"} {
		files["pages/"+d+"/guard.vuka"] = "package " + d + `

import (
	"context"

	"example.com/app/layouts"
	"example.com/app/modules/accounts"
)

func show(s *accounts.Service) string { return "" }
func show(n int) string { return "" }

type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	_ = show(accounts.Default) + layouts.Home()
	return nil
}
`
		files["pages/"+d+"/index.vuka.html"] = "<script lang=\"go\">\npackage " + d + "\n\ntype Index struct{}\n</script>\n\n<section>" + d + "</section>\n"
	}
	root := localModule(t, webCheckout(t), files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	return root
}

// testDir is the package's directory, which tests may leave for a module's.
var testDir, _ = os.Getwd()

// golden compares got with testdata/builderrs/name, which -update writes.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testDir, "testdata", "builderrs", name)
	if *updateFixUI {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run %s -update writes it)", err, t.Name())
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
	}
}

// TestBuildReport: a failed build shows the errors of the two packages that
// fail, at their source, and the pages that fail only for importing them
// in one line (listed with -v); run -watch shows the same, and so does the
// toolbar.
func TestBuildReport(t *testing.T) {
	root := brokenApp(t)
	var stderr bytes.Buffer
	err := runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr)
	if err == nil {
		t.Fatal("the build passed")
	}
	out := stderr.String()
	golden(t, "importers.golden", out)
	if n := strings.Count(out, "error["); n != 2 {
		t.Errorf("%d error blocks, want 2:\n%s", n, out)
	}
	for _, bad := range []string{"(and more)", "doesn't compile", "main_html.go", ":22:"} {
		if strings.Contains(out, bad) {
			t.Errorf("the report says %q:\n%s", bad, out)
		}
	}

	stderr.Reset()
	runGo("build", []string{"-v", "-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr)
	golden(t, "importers-v.golden", stderr.String()[strings.Index(stderr.String(), "error["):])

	// vuka run -watch, and the toolbar's build panel.
	slow := slowPhase
	slowPhase = time.Hour
	defer func() { slowPhase = slow }()
	s := &supervisor{root: root, dir: root, tmp: t.TempDir(), pkg: []string{"."}, flags: []string{"-buildvcs=false"}, env: os.Environ(), stderr: &stderr}
	stderr.Reset()
	r := s.build(context.Background(), 1)
	s.finish(r, time.Time{})
	if want := out + "● build failed; waiting for a change\n"; !strings.HasSuffix(stderr.String(), want) {
		t.Errorf("run -watch says:\n%s\nwant:\n%s", stderr.String(), want)
	}
	var st buildState
	b, _ := os.ReadFile(filepath.Join(s.tmp, buildStateFile))
	if json.Unmarshal(b, &st); st.State != "failed" || st.Output != out {
		t.Errorf("the toolbar shows %q:\n%s\nwant:\n%s", st.State, st.Output, out)
	}
}

// TestBuildReportGo: the errors of a build the go command fails, at their
// source, ten a package (vuka vet shows them all), with hints.
func TestBuildReportGo(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	var many strings.Builder
	for i := range 12 {
		many.WriteString("\t_ = missing" + string(rune('A'+i)) + "\n")
	}
	files := map[string]string{
		"main.go":        "package main\n\nimport _ \"example.com/app/lots\"\n\nfunc main() {}\n",
		"lots/lots.vuka": "package lots\n\nfunc f() {\n" + many.String() + "}\n",
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	root := tempModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	var stderr bytes.Buffer
	if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
		t.Fatal("the build passed")
	}
	golden(t, "capped.golden", stderr.String())
	stderr.Reset()
	runGo("vet", []string{"./..."}, io.Discard, &stderr)
	if out := stderr.String(); strings.Count(out, "error[E0002]") != 12 || !strings.Contains(out, "12 errors in 1 package") || strings.Contains(out, "vuka vet lists all") {
		t.Errorf("vuka vet:\n%s", out)
	}
}

// TestBlockWriter: the go command's lines pass on as they come, but its
// packages' blocks of errors, kept for the report.
func TestBlockWriter(t *testing.T) {
	var out bytes.Buffer
	b := &blockWriter{next: &out, respell: func(l string) string { return l }}
	io.WriteString(b, "go: downloading example.com/x v1.0.0\n# example.com/app/a\na/a.vuka:3: undefined: x\n\thave ()\n")
	io.WriteString(b, "\twant (int)\nsomething else\n# [example.com/app/b]\nvet: b/b.go:1:2: undefined: y\n")
	b.flush()
	if out.String() != "go: downloading example.com/x v1.0.0\nsomething else\n" {
		t.Errorf("passed on:\n%s", out.String())
	}
	if len(b.blocks) != 2 || b.blocks[0].path != "example.com/app/a" || len(b.blocks[0].lines) != 4 ||
		b.blocks[1].path != "example.com/app/b" || len(b.blocks[1].lines) != 2 {
		t.Errorf("blocks: %+v", b.blocks)
	}
	rep := goReport(buildInput{}, []byte("# example.com/x\nx/x.go:3: undefined: x\nlink: trouble\n"))
	if len(rep.groups) != 0 || strings.Join(rep.raw, "") != "link: trouble\n# example.com/x\nx/x.go:3: undefined: x\n" {
		t.Errorf("a package outside the module: %+v", rep)
	}
}

// TestDepOrder: a package's errors come after those of the packages of
// the module it imports.
func TestDepOrder(t *testing.T) {
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25.0\n",
		"a/a.go": "package a\n\nimport _ \"example.com/app/b\"\n",
		"b/b.go": "package b\n\nimport _ \"example.com/app/c\"\n",
		"c/c.go": "package c\n",
		"d/d.go": "package d\n",
	})
	in := buildInput{root: root, modPath: "example.com/app"}
	if got := strings.Join(in.depOrder([]string{"example.com/app/a", "example.com/app/d", "example.com/app/c"}), " "); got != "example.com/app/c example.com/app/a example.com/app/d" {
		t.Errorf("order: %s", got)
	}
}

// TestWatchFailureOnTerminal: on a terminal each failed build's errors
// replace the last one's, and the last line says when it failed; written
// elsewhere, the output is plain.
func TestWatchFailureOnTerminal(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.25.0\n"})
	for _, tty := range []bool{true, false} {
		var stderr bytes.Buffer
		s := &supervisor{root: root, dir: root, tmp: t.TempDir(), stderr: &stderr, tty: tty}
		for range 2 {
			s.finish(&buildResult{dir: t.TempDir(), err: errors.New("go: trouble")}, time.Time{})
		}
		out := stderr.String()
		failed := regexp.MustCompile(`● build failed at \d\d:\d\d:\d\d; waiting for a change\n`)
		switch {
		case tty && (strings.Count(out, clearScreen) != 2 || len(failed.FindAllString(out, -1)) != 2):
			t.Errorf("on a terminal:\n%q", out)
		case !tty && out != strings.Repeat("go: trouble\n● build failed; waiting for a change\n", 2):
			t.Errorf("not on a terminal:\n%q", out)
		}
	}
}

// importApp is an app whose entities package has the file user, which
// the store package embeds a type of and the api package returns in a
// GraphQL query.
func importApp(t *testing.T, user string) string {
	t.Helper()
	files := map[string]string{
		"main.go":            "package main\n\nimport (\n\t_ \"example.com/app/api\"\n\t_ \"example.com/app/store\"\n)\n\nfunc main() {}\n",
		"entities/user.vuka": user,
		"entities/post.vuka": "package entities\n\nimport \"time\"\n\ntype Post struct {\n\tTitle string\n\tAt    time.Time\n}\n",
		"store/customer.vuka": `package store

import "example.com/app/entities"

type Customer struct {
	entities.User
	Active bool
}

func Email(c *Customer) string { return c.Email }

func Domain(c *Customer) (string, error) {
	d := c.Domain()?
	return d, nil
}
`,
		"api/users.vuka": `package api

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"

	"example.com/app/entities"
)

@graphql.Query
func Users(ctx context.Context) ([]entities.User, error) { return nil, nil }
`,
	}
	root := localModule(t, webCheckout(t), files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	return root
}

// TestBuildReportImports: a broken or missing import is one error, and
// the packages importing its package, which can't see its types, are
// counted in the summary rather than reporting what they don't find.
func TestBuildReportImports(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	const body = `
type User struct {
	ID      int64
	Email   string
	Created time.Time
}

func (u *User) Domain() (string, error) { return strings.Split(u.Email, "@")[1], nil }
`
	for _, c := range []struct{ name, user string }{
		{"bare", "package entities\n\nimport\n" + body},
		{"block", "package entities\n\nimport (\n\t\"strings\"\n" + body},
		{"none", "package entities\n\nimport (\n\t\"strings\"\n\t\"time\"\n)\n" + body + `
@graphql.Query
func Recent() []User { return nil }

@graphql.Mutation
func Touch(id int64) error { return nil }

@graphql.Query
func Count() int { return 0 }
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			importApp(t, c.user)
			var stderr bytes.Buffer
			if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
				t.Fatal("the build passed")
			}
			out := stderr.String()
			golden(t, "imports-"+c.name+".golden", out)
			if n := strings.Count(out, "error["); n != 1 {
				t.Errorf("%d error blocks, want 1:\n%s", n, out)
			}
			if !strings.HasSuffix(out, "1 error in 1 package · 2 files couldn't build because of it\n") {
				t.Errorf("the summary:\n%s", out)
			}
			if c.name == "bare" {
				stderr.Reset()
				runGo("build", []string{"-v", "-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr)
				golden(t, "imports-bare-v.golden", stderr.String()[strings.Index(stderr.String(), "error["):])
			}
		})
	}
}

// ormDir is the orm checkout the tests build against ($VUKA_ORM, else
// ../vuka-orm), skipping without one that has orm.Base.
func ormDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VUKA_ORM")
	if dir == "" {
		repo, _ := filepath.Abs("../..")
		dir = filepath.Join(filepath.Dir(repo), "vuka-orm")
	}
	if src, _ := os.ReadFile(filepath.Join(dir, "vuka_base.go")); !bytes.Contains(src, []byte("type Base[")) {
		needs(t, "the build reports need github.com/vuka-lang/orm/v2 with orm.Base at %s (or $VUKA_ORM)", dir)
	}
	return dir
}

// TestBuildReportTypedFields: a package failing doesn't keep the orm from
// generating the typed fields of another package's models, so the package
// reading them builds and the one error is shown; when the models' own
// package fails, the package reading their fields is counted with the
// files that couldn't build.
func TestBuildReportTypedFields(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	orm := ormDir(t)
	files := func() map[string]string {
		return map[string]string{
			"main.go": "package main\n\nimport (\n\t\"example.com/app/pages\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc main() { web.New(pages.Routes()).Run(\"\") }\n",
			"entities/user.vuka": `package entities

import (
	"time"

	"github.com/vuka-lang/orm/v2"
)

type User struct {
	orm.Base @orm.Meta{Table: "users"}
	ID      int64  @orm.PK
	Email   string @orm.Char{Max: 120}
	Active  bool
	Expires time.Time
}
`,
			"modules/accounts/service.vuka": `package accounts

import (
	"context"
	"time"

	"github.com/vuka-lang/orm/v2"

	"example.com/app/entities"
)

func Find(ctx context.Context, email string) (entities.User, error) {
	return orm.Objects[entities.User]().Filter(entities.UserFields.Email.IExact(email)).Get(ctx)
}

func Expire(ctx context.Context, email string) error {
	expires := time.Now()
	_, err := orm.Objects[entities.User]().Filter(entities.UserFields.Email.IExact(email)).
		Update(ctx, orm.Set{entities.UserFields.Active.Name(): false, entities.UserFields.Expires.Name(): expires})
	return err
}
`,
			"components/card.vuka.html": "<script lang=\"go\">\npackage components\n\ntype Card struct {\n\tTitle string\n}\n</script>\n\n<div>{Title}</div>\n",
			"pages/index.vuka.html": `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/modules/accounts"
)

var _ = accounts.Find

type Index struct{}
</script>

<section><components.Card Title="x"/></section>
`,
		}
	}
	for _, c := range []struct{ name, file, src string }{
		{"other", "components/card.vuka.html", "<script lang=\"go\">\npackage components\n\ntype Card struct {\n\tTitle string\n}\n\ntype Row struct {\n\tID int\n}\n</script>\n\n<div>{Title}</div>\n"},
		{"models", "entities/broken.vuka", "package entities\n\nfunc broken( {\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs := files()
			fs[c.file] = c.src
			root := localModule(t, webCheckout(t), fs)
			withModule(t, root, "github.com/vuka-lang/orm/v2", orm)
			t.Setenv("GOWORK", "off")
			t.Setenv("GOFLAGS", "-mod=mod")
			t.Chdir(root)
			var stderr bytes.Buffer
			if runGo("build", []string{"-v", "-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
				t.Fatal("the build passed")
			}
			out := stderr.String()
			out = out[strings.Index(out, "error["):]
			golden(t, "typedfields-"+c.name+".golden", out)
			if n := strings.Count(out, "error["); n != 1 {
				t.Errorf("%d error blocks, want 1:\n%s", n, out)
			}
			for _, bad := range []string{"UserFields", "declared and not used", "isn't a model"} {
				if strings.Contains(out, bad) {
					t.Errorf("the report says %q:\n%s", bad, out)
				}
			}
		})
	}
}
