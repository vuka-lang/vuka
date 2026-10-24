package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const tbMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

func main() {
	web.New(
		pages.Routes(),
		web.ContextProcessors(web.RequestContext, ui.PageContext, auth.Context),
	).Run("")
}
`

const tbDocument = `<script lang="go">
package layouts

import "github.com/vuka-lang/ui/v2"

// Document is the page's document.
type Document struct {
	Title    string
	Children ui.Node
}
</script>

<html><head><title>{Title}</title></head><body>{Children}</body></html>
`

const tbLayout = `<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

// Layout is the site's layout.
type Layout struct{ Children ui.Node }
</script>

<main>{Children}</main>
`

const tbAdminLayout = `<script lang="go">
package admin

import (
	"example.com/app/layouts"
	"github.com/vuka-lang/ui/v2"
)

// Shell is the admin area's layout.
type Shell struct{ Children ui.Node }
</script>

<layouts.Document title={page.Title}>{Children}</layouts.Document>
`

// tbAdminIndex reads the request by name, and the permissions directly
// in its mount and an event: two W0402 sites.
const tbAdminIndex = `<script lang="go">
package admin

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

// Index is the admin dashboard.
type Index struct {
	ui.Live
	reports bool
}

@ui.Mount
func (d *Index) mount(ctx context.Context) error {
	if auth.Can(ctx, "view_reports") {
		d.reports = true
	}
	return nil
}

func (d *Index) Approve(ctx context.Context) error {
	if !auth.Can(ctx, "approve") {
		return web.Forbidden
	}
	return nil
}
</script>

<main>
  <p id="path">{request.Path}</p>
  <button onClick={Approve}>Approve</button>
</main>
`

// tbAdminTest declares the names of processor variables the package's
// pages read: page, request, user.
const tbAdminTest = `package admin

import "testing"

func page(s string) string    { return s }
func request(s string) string { return s }
func user(s string) string    { return s }

func TestNames(t *testing.T) {
	if page("a")+request("b")+user("c") != "abc" {
		t.Fatal("names")
	}
}
`

const tbItem = `<script lang="go">
package item

// Index is one item.
type Index struct{ ID int }
</script>

<p>{Label(ID)} {request.Path}</p>
`

const tbItemLabel = `package item

import "strconv"

// Label is an item's label.
func Label(id int) string { return "item " + strconv.Itoa(id) }
`

const tbItemTest = `package item

import "testing"

func TestLabel(t *testing.T) {
	if Label(7) != "item 7" {
		t.Fatal(Label(7))
	}
}
`

// testBuildsModule is an app whose pages are in a route group and a [id]
// folder, each with a _test.go; files are added, or removed when empty.
func testBuildsModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "ContextProcessors") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no ContextProcessors; set VUKA_WEB (and VUKA_UI)", web)
	}
	all := map[string]string{
		"main.go":                          tbMain,
		"layouts/document.vuka.html":       tbDocument,
		"pages/_layout.vuka.html":          tbLayout,
		"pages/(admin)/_layout.vuka.html":  tbAdminLayout,
		"pages/(admin)/index.vuka.html":    tbAdminIndex,
		"pages/(admin)/names_test.go":      tbAdminTest,
		"pages/items/[id]/index.vuka.html": tbItem,
		"pages/items/[id]/label.go":        tbItemLabel,
		"pages/items/[id]/label_test.go":   tbItemTest,
	}
	for name, src := range files {
		if src == "" {
			delete(all, name)
		} else {
			all[name] = src
		}
	}
	root := localModule(t, web, all)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	return root
}

// TestTestAndVetRouteFolders: vuka test ./... and vuka vet ./... build a
// route group's and a [id] folder's packages with their tests, though a
// test file declares page, request and user, names of processor variables
// the pages read (#101, #102); the folders' paths are patterns too.
func TestTestAndVetRouteFolders(t *testing.T) {
	root := testBuildsModule(t, nil)
	var out, errOut bytes.Buffer
	if err := runGo("test", []string{"-count=1", "./..."}, &out, &errOut); err != nil {
		t.Fatalf("vuka test ./...: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{"ok  \texample.com/app/pages/group_admin", "ok  \texample.com/app/pages/items/param_id"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("vuka test ./... ran no %s:\n%s", want, out.Bytes())
		}
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka vet ./...: %v\n%s", err, errOut.Bytes())
	}
	if strings.Contains(errOut.String(), "undefined") || strings.Contains(errOut.String(), "chdir") {
		t.Errorf("vuka vet ./...:\n%s", errOut.Bytes())
	}
	out.Reset()
	errOut.Reset()
	if err := runGo("test", []string{"-count=1", "./pages/(admin)", "./pages/items/[id]/..."}, &out, &errOut); err != nil {
		t.Fatalf("vuka test of the folders' paths: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	if c := strings.Count(out.String(), "ok  \t"); c != 2 {
		t.Errorf("vuka test of the folders' paths ran %d packages:\n%s", c, out.Bytes())
	}
	errOut.Reset()
	if err := runGo("build", []string{"-o", os.DevNull, "."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, errOut.Bytes())
	}
	noGoDirs(t, root)
}

var w0402At = regexp.MustCompile(`warning\[W0402\][^\n]*\n\s*--> (\S+:\d+:\d+)`)

// TestFixCtxVarsFolderPage: vuka fix ctx-vars rewrites exactly the W0402
// sites vet reports in a route group's page — its mount and an event —
// with or without a test file that shadows processor variables or doesn't
// compile (#103); a page that doesn't compile is named as skipped.
func TestFixCtxVarsFolderPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		test string
	}{
		{"no test file", ""},
		{"a test file declaring page, request, user", tbAdminTest},
		{"a test file that doesn't compile", "package admin\n\nvar broken int = \"s\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testBuildsModule(t, map[string]string{"pages/(admin)/names_test.go": tc.test})
			var errOut bytes.Buffer
			runGo("vet", []string{"./..."}, io.Discard, &errOut) // the broken test file fails vet
			var sites []string
			for _, m := range w0402At.FindAllStringSubmatch(errOut.String(), -1) {
				sites = append(sites, m[1])
			}
			if want := []string{"pages/(admin)/index.vuka.html:20:5", "pages/(admin)/index.vuka.html:27:6"}; !slices.Equal(sites, want) {
				t.Fatalf("vuka vet reports W0402 at %v, want %v:\n%s", sites, want, errOut.Bytes())
			}
			var out bytes.Buffer
			if err := fix([]string{"-n", "ctx-vars"}, &out); err != nil {
				t.Fatal(err)
			}
			if want := "ctx-vars (not applied): pages/(admin)/index.vuka.html: 2 read(s) through perms, user or request\n"; out.String() != want {
				t.Fatalf("vuka fix -n ctx-vars:\n%s\nwant:\n%s", out.Bytes(), want)
			}
			out.Reset()
			if err := fix([]string{"ctx-vars"}, &out); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(root, "pages/(admin)/index.vuka.html"))
			src := string(b)
			if !strings.Contains(src, `if perms.Has("view_reports") {`) || !strings.Contains(src, `if !perms.Has("approve") {`) ||
				strings.Contains(src, "auth.") || !strings.Contains(src, "web.Forbidden") {
				t.Errorf("vuka fix ctx-vars wrote:\n%s", src)
			}
			errOut.Reset()
			runGo("vet", []string{"./..."}, io.Discard, &errOut)
			if strings.Contains(errOut.String(), "W0402") || strings.Contains(errOut.String(), "index.vuka.html") {
				t.Errorf("vuka vet after the fix:\n%s", errOut.Bytes())
			}
		})
	}
	t.Run("a page that doesn't compile", func(t *testing.T) {
		testBuildsModule(t, map[string]string{"pages/(admin)/index.vuka.html": strings.Replace(tbAdminIndex, "\treturn nil\n}\n</script>", "\treturn nil +\n}\n</script>", 1)})
		var out bytes.Buffer
		if err := fix([]string{"-n", "ctx-vars"}, &out); err != nil {
			t.Fatal(err)
		}
		if want := "ctx-vars: skipped pages/(admin)/index.vuka.html: it doesn't compile (vuka vet shows why), so its reads aren't known\n"; out.String() != want {
			t.Errorf("vuka fix -n ctx-vars:\n%s\nwant:\n%s", out.Bytes(), want)
		}
	})
}

// TestOverlayDirsShared: two runs using a Go-name directory each mark it:
// one ending leaves it to the other, and another vuka tidying the route
// folders' directories (routeTree.goDirs) leaves a marked one alone, but
// removes the mark of a process that has exited, and then the directory
// (#102).
func TestOverlayDirsShared(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	pages := filepath.Join(root, "pages")
	for _, d := range []string{"(admin)", "items/[id]"} {
		if err := os.MkdirAll(filepath.Join(pages, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(root, "x.go")
	writeFile(t, src, "package x\n")
	group := filepath.Join(pages, "group_admin")
	param := filepath.Join(pages, "items", "param_id")
	ov, _ := json.Marshal(map[string]any{"Replace": map[string]string{
		filepath.Join(group, "a.go"): src,
		filepath.Join(param, "b.go"): src,
	}})
	overlay := filepath.Join(root, "overlay.json")
	writeFile(t, overlay, string(ov))
	tree := &routeTree{pages: pages}

	done1, err := overlayDirs(overlay)
	if err != nil {
		t.Fatal(err)
	}
	done2, err := overlayDirs(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.goDirs(); err != nil {
		t.Fatal(err)
	}
	if !fileExists(group) || !fileExists(param) {
		t.Fatal("goDirs removed a directory a run is using")
	}
	done1()
	if !fileExists(group) || !fileExists(param) {
		t.Fatal("one run's end removed a directory another is using")
	}
	done2()
	if fileExists(group) || fileExists(param) {
		t.Fatal("the runs' ends left their directories")
	}

	// A mark left by a process that has exited is stale.
	exited := exec.Command(os.Args[0], "-test.run=^$")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(group, inUseMark+strconv.Itoa(exited.Process.Pid)+"-1"), "")
	if err := tree.goDirs(); err != nil {
		t.Fatal(err)
	}
	if fileExists(group) {
		t.Error("goDirs kept a directory marked by a process that has exited")
	}
}

// TestGoPatterns: a pattern naming a route folder by its path is written
// with its Go name; others are left alone.
func TestGoPatterns(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{"pages/(admin)/users", "pages/items/[id]", "pages/_parts", "other"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	got := goPatterns(root, []string{"-run", "./x(y)", "./pages/(admin)", "./pages/(admin)/users/...", "./pages/items/[id]/...", "./pages/_parts", "./other", "./..."})
	want := []string{"-run", "./x(y)", "./pages/group_admin", "./pages/group_admin/users/...", "./pages/items/param_id/...", "./pages/private_parts", "./other", "./..."}
	if !slices.Equal(got, want) {
		t.Errorf("goPatterns:\n%q\nwant\n%q", got, want)
	}
	t.Chdir(filepath.Join(root, "pages"))
	if got := goPatterns(root, []string{"./(admin)", filepath.Join(root, "pages", "items", "[id]")}); !slices.Equal(got, []string{"./group_admin", filepath.ToSlash(filepath.Join(root, "pages", "items", "param_id"))}) {
		t.Errorf("goPatterns from pages/: %q", got)
	}
}
