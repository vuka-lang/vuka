package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestFolderRoutesE2E generates examples/folder-routes, whose routes are
// its pages/ folders, with vuka gen -inplace (pages/route.go, pages/urls,
// and its bracketed folders' Go under their Go names), builds it with go
// build and requests its pages: the layouts (_layout.vuka.html) nest,
// titles come from the pages, parameters bind typed into the pages'
// fields (a bad one is not found), the catch-all takes the rest of the path
// (a client app's shell answers every URL under it but the pages beside
// it), method handlers answer on their page's path, and every link, parent
// to child included, comes from pages/urls. Its JSON API under pages/api is
// an endpoint type and a file of method funcs behind a guard: each method
// answers JSON or a status, and the guard refuses a request before its
// body is read. Pages without a constructor of their own are built in
// route.go. Folder middleware wraps everything below its folder, the
// root's outermost, and short-circuits before the guards; a type's
// attributes (vuka.RegisterType) reach a stateless page's route. The
// admin area is a route group, pages/admin/(console): its shell and guard
// wrap its pages alone, at /admin with no segment of the group's, and the
// sign-in page beside it, /admin/auth/login, has the site's document only,
// framing itself in a component of the private folder pages/_parts.
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
		t.Skipf("github.com/vuka-lang/web/v2 at %s has no folder routes (PageRoute, LayoutOf); set VUKA_WEB", web)
	}
	if !webDeclares(web, "func GuardOf[") {
		t.Skipf("github.com/vuka-lang/web/v2 at %s has no guards (GuardOf, 1.2); set VUKA_WEB", web)
	}
	if gen, _ := os.ReadFile("folderroutes_gen.go"); !bytes.Contains(gen, []byte("GuardOf[")) {
		t.Skip("this vuka command generates no endpoints or guards (1.2)")
	} else if !bytes.Contains(gen, []byte("Middleware")) {
		t.Skip("this vuka command generates no folder middleware (1.3)")
	}
	if !webDeclares(web, "func UseOf(") {
		t.Skipf("github.com/vuka-lang/web/v2 at %s has no folder middleware (UseOf, 1.3); set VUKA_WEB", web)
	}
	src := filepath.Join(repo, "examples/folder-routes")
	files := map[string]string{}
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".vuka.html") && !strings.HasSuffix(p, ".vuka") {
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
	if b, err := os.ReadFile(filepath.Join(root, "pages", "route.go")); err != nil || !bytes.Contains(b, []byte("func Routes() web.Option")) {
		t.Errorf("pages/route.go: %v\n%s", err, b)
	}
	urls, _ := os.ReadFile(filepath.Join(root, "pages/urls/urls_gen.go"))
	for _, want := range []string{"package urls", "Employer(employer int)", "Permit(permit int)", "File(file web.Rest)", "Route(route string)"} {
		if !strings.Contains(string(urls), want) {
			t.Errorf("pages/urls/urls_gen.go has no %s:\n%s", want, urls)
		}
	}
	if out, err := runVuka(t, vuka, root, "gen", "-inplace", "-check"); err != nil {
		t.Errorf("vuka gen -inplace -check right after gen: %v\n%s", err, out)
	}
	out, err := runVuka(t, vuka, root, "routes")
	if err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, out)
	}
	for _, want := range []string{"/admin/auth/login", "console.Shell", "console.Guard", "/employers/new", "/employers/{employer}/edit", "/employers/{employer}/permits/{permit}", "/files/{file...}", "/app/{route...}", "/about", "DELETE", "employer.Layout",
		"/api/employers/{employer}", "employers.GET (endpoint)", "api.Guard", "app.Settings", "permits.Permit (live)", "app.Shell",
		"MIDDLEWARE", "pages.Middleware", "api.Middleware"} {
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
	base, logs := runApp(t, app, root, "API_RATE=60")
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
	// The overview links down to its permits, through pages/urls.
	body := page("/employers/1", "Acme Logistics - HR", `<a href="/employers/1/edit">Edit</a>`,
		`<a href="/employers/1/permits/2">Residence permit</a>`, `<a id="contract" href="/files/contracts/1.txt">`,
		`<a id="api" href="/api/employers/1">`)
	doc, article, overview := strings.Index(body, "<main>"), strings.Index(body, `<article id="employer">`), strings.Index(body, `<section id="overview">`)
	if doc < 0 || article < doc || overview < article || strings.Index(body, "</article>") < overview || strings.Index(body, "</main>") < overview {
		t.Errorf("GET /employers/1: the page isn't inside the employer's layout inside the document:\n%s", body)
	}
	page("/employers/1/edit", "Edit Acme Logistics - HR", `<article id="employer">`, `action="/employers/1/edit"`, `value="Acme Logistics"`)
	page("/employers/1/permits/2", "Permit 2 - HR", `<article id="employer">`, `<p id="status">Approved</p>`, `<a id="back" href="/employers/1">`, "data-vk-token")
	page("/files/handbook/leave.txt", "handbook/leave.txt - HR", "<h1>handbook/leave.txt</h1>", "28 days")
	page("/about", "HR", `<section id="about">`, "<main>")
	// Settings and the client app's Index have no New<T>: vuka gen writes
	// their constructors.
	page("/app/settings", "App settings - HR", `<section id="settings">`, `<a id="open" href="/app/">`)
	for _, p := range []string{"/app/", "/app/employers/7", "/app/a/b/c"} {
		page(p, "App - HR", `<div id="app"></div>`)
	}
	// The route group: its guard sends a visitor without the cookie to the
	// sign-in page, which is outside the group and has no shell; with the
	// cookie, the dashboard is inside the shell inside the document.
	if code, loc, _ := do("GET", "/admin", nil); code != http.StatusSeeOther && code != http.StatusFound || loc != "/admin/auth/login" {
		t.Errorf("GET /admin without the cookie: %d %q, want a redirect to /admin/auth/login", code, loc)
	}
	if login := page("/admin/auth/login", "Sign in - HR", `<nav id="site">`, `<div class="card"><h2>Sign in</h2>`, `<form id="login"`); strings.Contains(login, `id="console"`) {
		t.Errorf("GET /admin/auth/login is inside the group's shell:\n%s", login)
	}
	{
		req, _ := http.NewRequest("GET", base+"/admin", nil)
		req.AddCookie(&http.Cookie{Name: "admin", Value: "1"})
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		body := string(b)
		doc, shell, h1 := strings.Index(body, "<main>"), strings.Index(body, `<section id="console">`), strings.Index(body, "<h1>Dashboard</h1>")
		if res.StatusCode != http.StatusOK || doc < 0 || shell < doc || h1 < shell || !strings.Contains(body, `<a href="/admin">Dashboard</a>`) {
			t.Errorf("GET /admin with the cookie: %d, want the dashboard inside the shell inside the document:\n%s", res.StatusCode, body)
		}
	}
	notFound("GET", "/admin/console")
	notFound("GET", "/(console)")
	notFound("GET", "/_parts/card")
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

	// The JSON API: the guard wants a key, the read key reads, the write
	// key also writes.
	const readKey, writeKey = "read-key", "write-key"
	api := func(method, path, key, body string) (int, string, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, base+path, rd)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Content-Type"), string(b)
	}
	type employer struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		City string `json:"city"`
	}
	list := func(path string, want ...string) {
		t.Helper()
		code, ct, body := api("GET", path, readKey, "")
		var got []employer
		if code != http.StatusOK || !strings.HasPrefix(ct, "application/json") || json.Unmarshal([]byte(body), &got) != nil {
			t.Errorf("GET %s: %d %s, want 200 and a JSON list\n%s", path, code, ct, body)
			return
		}
		var names []string
		for _, e := range got {
			names = append(names, e.Name)
		}
		if strings.Join(names, ", ") != strings.Join(want, ", ") {
			t.Errorf("GET %s: %v, want %v", path, names, want)
		}
	}
	one := func(method, path, body string, want employer) {
		t.Helper()
		code, _, b := api(method, path, writeKey, body)
		var got employer
		if code != http.StatusOK || json.Unmarshal([]byte(b), &got) != nil || got != want {
			t.Errorf("%s %s: %d %s, want 200 and %+v", method, path, code, b, want)
		}
	}
	status := func(method, path, key, body string, want int) {
		t.Helper()
		code, ct, b := api(method, path, key, body)
		if code != want {
			t.Errorf("%s %s with key %q: %d, want %d\n%s", method, path, key, code, want, b)
		}
		if want >= 400 && !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s %s: the %d answers %s, want JSON\n%s", method, path, code, ct, b)
		}
	}

	list("/api/employers", "Acme Logistics", "Kilima Foods")
	list("/api/employers?city=arusha", "Kilima Foods")
	list("/api/employers?limit=1", "Acme Logistics")
	status("GET", "/api/employers?limit=500", readKey, "", http.StatusUnprocessableEntity)
	one("GET", "/api/employers/1", "", employer{1, "Acme Logistics", "Dar es Salaam"})
	status("GET", "/api/employers/99", readKey, "", http.StatusNotFound)
	status("GET", "/api/employers/x", readKey, "", http.StatusNotFound)

	// The guard stops a request before its handler, and before its body
	// is bound: the read key's invalid POST is 403, not 422.
	status("GET", "/api/employers", "", "", http.StatusUnauthorized)
	status("GET", "/api/employers/1", "wrong", "", http.StatusUnauthorized)
	status("POST", "/api/employers", "", `{"name":"Ghost","city":"Nowhere"}`, http.StatusUnauthorized)
	status("POST", "/api/employers", readKey, `{"name":"Ghost","city":"Nowhere"}`, http.StatusForbidden)
	status("POST", "/api/employers", readKey, `{}`, http.StatusForbidden)
	status("PUT", "/api/employers/1", readKey, `{"name":"Ghost","city":"Nowhere"}`, http.StatusForbidden)
	status("DELETE", "/api/employers/1", readKey, "", http.StatusForbidden)
	list("/api/employers", "Acme Logistics", "Kilima Foods")

	one("POST", "/api/employers", `{"name":"Pwani Tea","city":"Tanga"}`, employer{4, "Pwani Tea", "Tanga"})
	status("POST", "/api/employers", writeKey, `{"city":"Tanga"}`, http.StatusUnprocessableEntity)
	one("PUT", "/api/employers/4", `{"name":"Pwani Tea Estates","city":"Tanga"}`, employer{4, "Pwani Tea Estates", "Tanga"})
	status("PUT", "/api/employers/99", writeKey, `{"name":"Ghost","city":"Nowhere"}`, http.StatusNotFound)
	page("/employers/4", "Pwani Tea Estates - HR")
	if code, _, body := api("DELETE", "/api/employers/4", writeKey, ""); code != http.StatusNoContent || body != "" {
		t.Errorf("DELETE /api/employers/4: %d %q, want 204 and no body", code, body)
	}
	status("GET", "/api/employers/4", readKey, "", http.StatusNotFound)
	status("DELETE", "/api/employers/4", writeKey, "", http.StatusNotFound)
	list("/api/employers", "Acme Logistics", "Kilima Foods")

	// The middleware stamps pages and endpoints alike, the root's header
	// before the api folder's; Settings' no-store comes from its type's
	// attribute, not its folder.
	head := func(method, path, key string) http.Header {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, nil)
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header
	}
	if h := head("GET", "/employers", ""); !slices.Equal(h.Values("X-Served-By"), []string{"hr"}) {
		t.Errorf("GET /employers: X-Served-By %v, want [hr]", h.Values("X-Served-By"))
	}
	if h := head("GET", "/api/employers/1", readKey); !slices.Equal(h.Values("X-Served-By"), []string{"hr", "hr-api"}) {
		t.Errorf("GET /api/employers/1: X-Served-By %v, want [hr hr-api]: the root's middleware outermost", h.Values("X-Served-By"))
	}
	if h := head("GET", "/app/settings", ""); h.Get("Cache-Control") != "no-store" {
		t.Errorf("GET /app/settings: Cache-Control %q, want no-store from RegisterType[Settings]", h.Get("Cache-Control"))
	}
	if h := head("GET", "/employers/1", ""); h.Get("Cache-Control") == "no-store" {
		t.Error("GET /employers/1: no-store leaked off the Settings type")
	}

	// The api middleware's rate limit short-circuits: hammered without a
	// key, the 401s turn 429 once the bucket (API_RATE=60) is empty —
	// before the guard, which never sees those requests.
	saw401, saw429 := false, false
	for i := 0; i < 200 && !saw429; i++ {
		switch code, _, _ := api("GET", "/api/employers", "", ""); code {
		case http.StatusUnauthorized:
			saw401 = true
		case http.StatusTooManyRequests:
			saw429 = true
		default:
			t.Fatalf("GET /api/employers while hammering: %d", code)
		}
	}
	if !saw401 || !saw429 {
		t.Errorf("hammering /api/employers: 401 then 429, got 401=%v 429=%v", saw401, saw429)
	}
	if h := head("GET", "/employers", ""); !slices.Equal(h.Values("X-Served-By"), []string{"hr"}) {
		t.Error("the api folder's rate limit reached /employers")
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
