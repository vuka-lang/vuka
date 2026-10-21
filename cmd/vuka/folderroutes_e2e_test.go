package main

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestFolderRoutesE2E generates examples/folder-routes, plain Go whose
// routes are its pages/ folders, with vuka gen -inplace, builds it with go
// build and requests its pages: the layouts nest, titles come from the
// pages, parameters bind typed (a bad one is not found), the catch-all
// takes the rest of the path (a client app's shell answers every URL under
// it but the pages beside it), a template is a page with no type of its
// own, method handlers answer on their page's path, and links are the
// typed URLs.
func TestFolderRoutesE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and an app")
	}
	if _, err := os.Stat("folderroutes.go"); err != nil {
		t.Skip("this vuka command generates no folder routes")
	}
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "PageRoute") || !webDeclares(web, "func LayoutOf[") {
		t.Skipf("github.com/vuka-lang/web at %s has no folder routes (PageRoute, LayoutOf); set VUKA_WEB", web)
	}
	src := filepath.Join(repo, "examples/folder-routes")
	files := map[string]string{}
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".vuka.html") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(src, p)
		files[filepath.ToSlash(rel)] = strings.ReplaceAll(string(b), "example.com/folder-routes", "example.com/app")
		return nil
	})
	vuka := buildVuka(t)
	root := localModule(t, web, files)

	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "pages", "pages_gen.go")); err != nil {
		t.Errorf("no pages/pages_gen.go: %v", err)
	}
	urls, _ := os.ReadFile(filepath.Join(root, "pages/employers/employer/routes_gen.go"))
	if !strings.Contains(string(urls), "func EditURL(employer int) string") {
		t.Errorf("employer/routes_gen.go has no EditURL(employer int):\n%s", urls)
	}
	if out, err := runVuka(t, vuka, root, "gen", "-inplace", "-check"); err != nil {
		t.Errorf("vuka gen -inplace -check right after gen: %v\n%s", err, out)
	}
	out, err := runVuka(t, vuka, root, "routes")
	if err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, out)
	}
	for _, want := range []string{"/employers/new", "/employers/{employer}/edit", "/employers/{employer}/permits/{permit}", "/files/{file...}", "/app/{route...}", "/about", "DELETE", "employer.Layout"} {
		if !strings.Contains(out, want) {
			t.Errorf("vuka routes has no %q:\n%s", want, out)
		}
	}

	app := filepath.Join(t.TempDir(), "app")
	build := exec.Command("go", "build", "-o", app, ".")
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build after gen -inplace: %v\n%s", err, out)
	}
	base, logs := runApp(t, app, root)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("app log:\n%s", logs)
		}
	})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path string, form url.Values) (int, string, string) {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, _ := http.NewRequest(method, base+path, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Location"), string(b)
	}
	title := regexp.MustCompile(`<title>([^<]*)</title>`)
	page := func(path, wantTitle string, want ...string) string {
		t.Helper()
		code, _, body := do("GET", path, nil)
		if code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200\n%s", path, code, body)
			return body
		}
		if m := title.FindStringSubmatch(body); m == nil || m[1] != wantTitle {
			t.Errorf("GET %s: title %q, want %q", path, m, wantTitle)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("GET %s has no %s:\n%s", path, w, body)
			}
		}
		return body
	}
	notFound := func(method, path string) {
		t.Helper()
		if code, _, _ := do(method, path, nil); code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", method, path, code)
		}
	}

	page("/employers", "Employers - HR", `<nav id="site"><a href="/employers">Employers</a> <a href="/files/handbook/leave.txt">Handbook</a> <a href="/about">About</a> <a href="/app/settings">App</a></nav>`,
		`<a href="/employers/1">Acme Logistics</a>`, `<a id="new" href="/employers/new">`, "data-vk-token")
	page("/employers/new", "New employer - HR", `action="/employers/new"`)
	body := page("/employers/1", "Acme Logistics - HR", `<a href="/employers/1/edit">Edit</a>`,
		`<a href="/employers/1/permits/2">Residence permit</a>`, `<a id="contract" href="/files/contracts/1.txt">`)
	doc, article, overview := strings.Index(body, "<main>"), strings.Index(body, `<article id="employer">`), strings.Index(body, `<section id="overview">`)
	if doc < 0 || article < doc || overview < article || strings.Index(body, "</article>") < overview || strings.Index(body, "</main>") < overview {
		t.Errorf("GET /employers/1: the page isn't inside the employer's layout inside the document:\n%s", body)
	}
	page("/employers/1/edit", "Edit Acme Logistics - HR", `<article id="employer">`, `action="/employers/1/edit"`, `value="Acme Logistics"`)
	page("/employers/1/permits/2", "Permit 2 - HR", `<article id="employer">`, `<p id="status">Approved</p>`, `<a id="back" href="/employers/1">`, "data-vk-token")
	page("/files/handbook/leave.txt", "handbook/leave.txt - HR", "<h1>handbook/leave.txt</h1>", "28 days")
	page("/about", "HR", `<section id="about">`, "<main>")
	page("/app/settings", "App settings - HR", `<section id="settings">`, `<a id="open" href="/app/">`)
	for _, p := range []string{"/app/", "/app/employers/7", "/app/a/b/c"} {
		page(p, "App - HR", `<div id="app"></div>`)
	}
	notFound("GET", "/employers/x")
	notFound("GET", "/employers/99")
	notFound("GET", "/employers/99/edit")
	notFound("GET", "/employers/2/permits/1")
	notFound("GET", "/employers/1/permits/x")
	notFound("GET", "/files/nope.txt")
	if code, loc, _ := do("GET", "/", nil); code != http.StatusFound || loc != "/employers" {
		t.Errorf("GET /: %d %q, want a redirect to /employers", code, loc)
	}

	if code, loc, body := do("POST", "/employers/new", url.Values{"name": {"Pwani Tea"}, "city": {"Tanga"}}); code != http.StatusSeeOther || loc != "/employers/3" {
		t.Errorf("POST /employers/new: %d %q\n%s", code, loc, body)
	}
	if code, _, _ := do("POST", "/employers/new", url.Values{"name": {""}}); code != http.StatusUnprocessableEntity {
		t.Errorf("POST /employers/new without a name: %d, want 422", code)
	}
	if code, loc, body := do("POST", "/employers/3/edit", url.Values{"name": {"Pwani Tea Estates"}, "city": {"Tanga"}}); code != http.StatusSeeOther || loc != "/employers/3" {
		t.Errorf("POST /employers/3/edit: %d %q\n%s", code, loc, body)
	}
	page("/employers/3", "Pwani Tea Estates - HR")
	if code, _, body := do("DELETE", "/employers/3/edit", nil); code/100 != 2 {
		t.Errorf("DELETE /employers/3/edit: %d\n%s", code, body)
	}
	notFound("GET", "/employers/3")

	// The employer's Layout loads before the handlers too: an unknown
	// employer is not found, and nothing is written.
	notFound("DELETE", "/employers/3/edit")
	notFound("POST", "/employers/x/edit")
	if code, _, _ := do("POST", "/employers/99/edit", url.Values{"name": {"Ghost"}, "city": {"Nowhere"}}); code != http.StatusNotFound {
		t.Errorf("POST /employers/99/edit: %d, want 404", code)
	}
	notFound("GET", "/employers/99")
	if _, _, body := do("GET", "/employers", nil); strings.Contains(body, "Ghost") || strings.Contains(body, "Pwani") {
		t.Errorf("GET /employers after writes to unknown employers:\n%s", body)
	}
}

// webDeclares reports whether a Go file of web, in its module directory,
// holds decl.
func webDeclares(dir, decl string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if src, err := os.ReadFile(f); err == nil && bytes.Contains(src, []byte(decl)) {
			return true
		}
	}
	return false
}
