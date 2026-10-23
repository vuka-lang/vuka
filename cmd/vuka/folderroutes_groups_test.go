package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// groupPages is a tree with a route group: pages/admin/(console) holds the
// admin area, its layout and guard over its pages alone, and
// pages/admin/auth/login is a sign-in page beside it under the same /admin
// prefix, outside both; pages/_parts is a private folder, a component
// package the pages import, routing nothing.
var groupPages = map[string]string{
	"pages/_layout.vuka.html":                     "package pages\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Layout is the document.\ntype Layout struct {\n\tChildren ui.Node\n}\n<main>{Children}</main>\n",
	"pages/admin/(console)/_layout.vuka.html":     "package console\n\nimport (\n\t\"example.com/app/pages/urls\"\n\t\"github.com/vuka-lang/ui/v2\"\n)\n\n// Shell is the admin area's shell.\ntype Shell struct {\n\tChildren ui.Node\n}\n<section id=\"console\"><a href={urls.Admin.Users.Path()}>Users</a>{Children}</section>\n",
	"pages/admin/(console)/guard.vuka":            "package console\n\nimport \"context\"\n\n// Guard lets a signed-in user in.\ntype Guard struct{}\n\nfunc (g *Guard) Load(ctx context.Context) error { return nil }\n",
	"pages/admin/(console)/index.vuka.html":       "package console\n\n// Index is the dashboard.\ntype Index struct{}\n<h1>Dashboard</h1>\n",
	"pages/admin/(console)/users/index.vuka.html": "package users\n\nimport \"example.com/app/pages/urls\"\n\n// Index lists the users.\ntype Index struct{}\n<a href={urls.Admin.Users.ID(7).Path()}>7</a>\n",
	"pages/admin/(console)/users/[id].vuka.html":  "package users\n\n// User is one user.\ntype User struct {\n\tID int\n}\n<p>user {ID}</p>\n",
	"pages/admin/auth/login.vuka.html":            "package auth\n\nimport (\n\t\"example.com/app/pages/private_parts\"\n\t\"example.com/app/pages/urls\"\n)\n\n// Login signs a user in.\ntype Login struct{}\n<parts.Card title=\"Sign in\"><form action={urls.Admin.Auth.Login.Path()}></form></parts.Card>\n",
	"pages/_parts/card.vuka.html":                 "package parts\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Card is a part of pages, no page itself.\ntype Card struct {\n\tTitle    string\n\tChildren ui.Node\n}\n<div class=\"card\"><h2>{Title}</h2>{Children}</div>\n",
}

// groupsModule is the routes fixture with groupPages for its pages/, plus
// files.
func groupsModule(t *testing.T, files map[string]string) string {
	t.Helper()
	app := routesModule(t, nil)
	os.RemoveAll(filepath.Join(app, "pages"))
	for name, src := range groupPages {
		writeFile(t, filepath.Join(app, name), src)
	}
	for name, src := range files {
		if src == "" {
			os.Remove(filepath.Join(app, name))
			continue
		}
		writeFile(t, filepath.Join(app, name), src)
	}
	return app
}

// TestRouteGroups: a route group adds no segment to the URL, and its
// layout and guard wrap the pages inside it alone, inside the layouts
// above it; the urls chain leaves it out; the go command sees it as
// group_console and the private folder as private_parts, made on disk,
// where vuka gen -inplace writes their Go.
func TestRouteGroups(t *testing.T) {
	app := groupsModule(t, nil)
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	checkGolden(t, "groups_routes.txt", out.Bytes())
	for _, want := range []string{
		`GET +/admin +console\.Index +pages/admin/\(console\)/index\.vuka\.html:\d+ +console\.Guard +pages\.Layout, console\.Shell`,
		`GET +/admin/users/\{id\} +users\.User .*console\.Guard +pages\.Layout, console\.Shell`,
		`GET +/admin/auth/login +auth\.Login +pages/admin/auth/login\.vuka\.html:\d+ +pages\.Layout\n`,
	} {
		if !regexp.MustCompile(want).MatchString(out.String()) {
			t.Errorf("vuka routes has no %s:\n%s", want, out.Bytes())
		}
	}
	if strings.Contains(out.String(), "Card") || regexp.MustCompile(`(?m)^\w+ +\S*[()_]`).MatchString(out.String()) {
		t.Errorf("vuka routes routes the private folder or puts the group in a path:\n%s", out.Bytes())
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet from the overlay: %v\n%s", err, errOut.Bytes())
	}
	noGoDirs(t, app)
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	for _, f := range []string{"pages/admin/group_console/index_html.go", "pages/admin/group_console/users/param_id_html.go", "pages/private_parts/card_html.go"} {
		if _, err := os.Stat(filepath.Join(app, f)); err != nil {
			t.Errorf("vuka gen -inplace wrote no %s: %v", f, err)
		}
	}
	urls, _ := os.ReadFile(filepath.Join(app, "pages/urls/urls_gen.go"))
	checkGolden(t, "groups_urls_gen.go", urls)
	route, _ := os.ReadFile(filepath.Join(app, "pages/route.go"))
	checkGolden(t, "groups_route.go", route)
	if err := gen([]string{"-inplace", "-check"}, io.Discard); err != nil {
		t.Errorf("vuka gen -inplace -check: %v", err)
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet with the files on disk: %v\n%s", err, errOut.Bytes())
	}
}

// TestRouteGroupErrors: each mistake a route group or a private folder
// makes, its code and where it is reported.
func TestRouteGroupErrors(t *testing.T) {
	page := func(pkg, typ string) string { return "package " + pkg + "\n\ntype " + typ + " struct{}\n<p>x</p>\n" }
	shell := func(pkg string) string {
		return "package " + pkg + "\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Shell struct {\n\tChildren ui.Node\n}\n<div>{Children}</div>\n"
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		code  string
		at    []string
	}{
		{"a page in a group and one outside it on one path", map[string]string{"pages/admin/(console)/auth/login.vuka.html": page("auth", "Login")},
			"E0722", []string{"pages/admin/auth/login.vuka.html", "pages/admin/(console)/auth/login.vuka.html"}},
		{"pages in two groups on one path", map[string]string{"pages/admin/(other)/index.vuka.html": page("other", "Index")},
			"E0722", []string{"pages/admin/(console)/index.vuka.html", "pages/admin/(other)/index.vuka.html"}},
		{"a group's name", map[string]string{"pages/(admin-area)/index.vuka.html": page("area", "Home")},
			"E0723", []string{"pages/(admin-area)"}},
		{"a file named as a group", map[string]string{"pages/admin/(help).vuka.html": page("admin", "Help")},
			"E0723", []string{"pages/admin/(help).vuka.html:1"}},
		{"Go in a group of another package", map[string]string{"pages/admin/(console)/settings.go": "package settings\n"},
			"E0716", []string{"pages/admin/(console)/settings.go:1"}},
		{"the home page outside the root layouts", map[string]string{"pages/_layout.vuka.html": "", "pages/(site)/_layout.vuka.html": shell("site"),
			"pages/(site)/about.vuka.html": page("site", "About"), "pages/index.vuka.html": page("pages", "Home")},
			"E0724", []string{"pages/index.vuka.html:3"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			groupsModule(t, c.files)
			var out, errOut bytes.Buffer
			err := routesCmd([]string{"-json"}, &out, &errOut)
			if err == nil {
				t.Fatalf("no error:\n%s", out.Bytes())
			}
			got := errOut.String()
			if !strings.Contains(got, c.code) {
				t.Errorf("want %s:\n%s", c.code, got)
			}
			for _, at := range c.at {
				if !strings.Contains(got, at) {
					t.Errorf("want %s at %s:\n%s", c.code, at, got)
				}
			}
		})
	}
}

// TestRootLayouts: with no pages/_layout.vuka.html, a layout with none
// above it is a root layout, the document of the pages below it: a
// top-level route group's, a plain folder's, one under a parameter folder;
// the home page lives in one of the groups. vuka routes -json gives each
// route its root.
func TestRootLayouts(t *testing.T) {
	shell := func(pkg, typ, id string) string {
		return "package " + pkg + "\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype " + typ + " struct {\n\tChildren ui.Node\n}\n<html><body id=\"" + id + "\">{Children}</body></html>\n"
	}
	page := func(pkg, typ string) string { return "package " + pkg + "\n\ntype " + typ + " struct{}\n<p>x</p>\n" }
	app := routesModule(t, nil)
	os.RemoveAll(filepath.Join(app, "pages"))
	for name, src := range map[string]string{
		"pages/(site)/_layout.vuka.html": shell("site", "Site", "site"),
		"pages/(site)/index.vuka.html":   page("site", "Home"),
		"pages/(shop)/_layout.vuka.html": shell("shop", "Shop", "shop"),
		"pages/(shop)/cart.vuka.html":    page("shop", "Cart"),
		"pages/blog/_layout.vuka.html":   shell("blog", "Blog", "blog"),
		"pages/blog/index.vuka.html":     page("blog", "Index"),
		"pages/[lang]/_layout.vuka.html": "package lang\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Doc struct {\n\tLang     string\n\tChildren ui.Node\n}\n<html lang={Lang}><body>{Children}</body></html>\n",
		"pages/[lang]/docs.vuka.html":    "package lang\n\ntype Docs struct {\n\tLang string\n}\n<p>{Lang}</p>\n",
	} {
		writeFile(t, filepath.Join(app, name), src)
	}
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	for _, want := range []string{`GET +/ +site\.Home .* site\.Site\n`, `GET +/cart +shop\.Cart .* shop\.Shop\n`} {
		if !regexp.MustCompile(want).MatchString(out.String()) {
			t.Errorf("vuka routes has no %s:\n%s", want, out.Bytes())
		}
	}
	out.Reset()
	if err := routesCmd([]string{"-json"}, &out, &errOut); err != nil {
		t.Fatalf("vuka routes -json: %v\n%s", err, errOut.Bytes())
	}
	var list []folderRoute
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range list {
		got[r.Path] = r.Root
	}
	if want := map[string]string{"/": "site.Site", "/cart": "shop.Shop", "/blog": "blog.Blog", "/{lang}/docs": "lang.Doc"}; !maps.Equal(got, want) {
		t.Errorf("roots: %v, want %v", got, want)
	}
}

// TestGoInRouteFolders: a route group's and a parameter folder's own .go
// files — logic and tests — are the folder's package, copied where the go
// command sees it: vuka build, test and vet take them, an error in one is
// reported at it, and after vuka gen -inplace a plain go build does too.
func TestGoInRouteFolders(t *testing.T) {
	app := groupsModule(t, map[string]string{
		"pages/admin/(console)/stats/index.go":       "package stats\n\nimport (\n\t\"context\"\n\t\"strings\"\n)\n\n// Index is GET /admin/stats.\ntype Index struct{}\n\nfunc (*Index) Get(ctx context.Context) (string, error) { return Count(3), nil }\n\n// Count names n orders.\nfunc Count(n int) string { return strings.Repeat(\"o\", n) }\n",
		"pages/admin/(console)/stats/index_test.go":  "package stats\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n\tif Count(2) != \"oo\" {\n\t\tt.Fatal(Count(2))\n\t}\n}\n",
		"pages/admin/(console)/users/label.go":       "package users\n\nimport \"strconv\"\n\n// Label is a user's label.\nfunc Label(id int) string { return \"user \" + strconv.Itoa(id) }\n",
		"pages/admin/(console)/users/label_test.go":  "package users\n\nimport \"testing\"\n\nfunc TestLabel(t *testing.T) {\n\tif Label(7) != \"user 7\" {\n\t\tt.Fatal(Label(7))\n\t}\n}\n",
		"pages/admin/(console)/users/[id].vuka.html": "package users\n\n// User is one user.\ntype User struct {\n\tID int\n}\n<p>{Label(ID)}</p>\n",
		"pages/orders/[order]/index.vuka.html":       "package order\n\n// Index is one order.\ntype Index struct {\n\tOrder int\n}\n<p>{Total(Order)}</p>\n",
		"pages/orders/[order]/total.go":              "package order\n\nimport \"strconv\"\n\n// Total is an order's total.\nfunc Total(id int) string { return strconv.Itoa(id * 10) }\n",
		"pages/orders/[order]/total_test.go":         "package order_test\n\nimport (\n\t\"testing\"\n\n\torder \"example.com/app/pages/orders/param_order\"\n)\n\nfunc TestTotal(t *testing.T) {\n\tif order.Total(2) != \"20\" {\n\t\tt.Fatal(order.Total(2))\n\t}\n}\n",
		"pages/_parts/util.go":                       "package parts\n\n// Upper is s in capitals.\nfunc Upper(s string) string { return s }\n",
	})
	var errOut bytes.Buffer
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
	var out bytes.Buffer
	errOut.Reset()
	if err := runGo("test", []string{"./pages/..."}, &out, &errOut); err != nil {
		t.Fatalf("vuka test: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{"ok  \texample.com/app/pages/admin/group_console/stats", "ok  \texample.com/app/pages/admin/group_console/users", "ok  \texample.com/app/pages/orders/param_order"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("vuka test ran no %s:\n%s", want, out.Bytes())
		}
	}
	out.Reset()
	if err := routesCmd(nil, &out, &errOut); err != nil || !regexp.MustCompile(`GET +/admin/stats +stats\.Index\.Get \(endpoint\) +pages/admin/\(console\)/stats/index\.go:11`).Match(out.Bytes()) {
		t.Errorf("vuka routes: %v\n%s", err, out.Bytes())
	}

	errOut.Reset()
	if err := runGo("build", []string{"-o", os.DevNull, "."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, errOut.Bytes())
	}
	noGoDirs(t, app)

	// An error in one is reported at the file, by its own path.
	total := filepath.Join(app, "pages/orders/[order]/total.go")
	writeFile(t, total, "package order\n\n// Total is an order's total.\nfunc Total(id int) string { return id * 10 }\n")
	errOut.Reset()
	if err := runGo("build", []string{"-o", os.DevNull, "."}, io.Discard, &errOut); err == nil || !strings.Contains(errOut.String(), "pages/orders/[order]/total.go:4:") {
		t.Errorf("vuka build with an error in total.go: %v\n%s", err, errOut.Bytes())
	}
	writeFile(t, total, "package order\n\nimport \"strconv\"\n\n// Total is an order's total.\nfunc Total(id int) string { return strconv.Itoa(id * 10) }\n")

	// gen -inplace writes the copies, and a plain go build builds them.
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	for _, f := range []string{"pages/admin/group_console/stats/index.go", "pages/admin/group_console/users/label_test.go", "pages/orders/param_order/total.go", "pages/private_parts/util.go"} {
		b, err := os.ReadFile(filepath.Join(app, f))
		if err != nil || !bytes.HasPrefix(b, []byte("//line ")) {
			t.Errorf("vuka gen -inplace wrote no %s: %v\n%s", f, err, b)
		}
	}
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = app
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go build after vuka gen -inplace: %v\n%s", err, b)
	}
	cmd = exec.Command("go", "test", "./pages/orders/param_order", "./pages/admin/group_console/...")
	cmd.Dir = app
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go test after vuka gen -inplace: %v\n%s", err, b)
	}
}

// noGoDirs: the directories the go command sees the route folders in are
// in the overlay alone: none is under pages/.
func noGoDirs(t *testing.T, app string) {
	t.Helper()
	filepath.WalkDir(filepath.Join(app, "pages"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && regexp.MustCompile(`^(param|group|private)_`).MatchString(d.Name()) {
			t.Errorf("%s is on disk", p)
		}
		return nil
	})
}
