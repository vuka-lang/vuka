package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prefixMain serves examples/folder-routes under /gateway, as
// http.StripPrefix mounts it, and at the root behind a proxy that strips
// the prefix and says so with X-Forwarded-Prefix.
const prefixMain = `package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"example.com/app/pages"
	"example.com/app/pages/app"
	"example.com/app/store"
	"github.com/vuka-lang/web/v2"
)

func main() {
	a := web.New(pages.Routes(), web.Provide(store.New), web.WrapRoutes("no-store", noStore),
		web.ContextProcessors(web.RequestContext), web.TrustedProxies("127.0.0.1"))
	a.Get("/", func() web.Redirect { return "/employers" })
	a.Get("/where", func(ctx context.Context) string {
		return web.Route(ctx, "employers.edit", 1) + " " + web.RoutePath(ctx, "employers.edit", 1)
	})
	if err := a.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	h := a.Handler()
	mux := http.NewServeMux()
	mux.Handle("/gateway/", http.StripPrefix("/gateway", h))
	mux.Handle("/", h)
	log.Fatal(http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), mux))
}

func noStore(info web.RouteInfo) web.Middleware {
	for _, x := range info.Attrs {
		if _, ok := x.(app.NoStore); ok {
			return func(next http.Handler) http.Handler { return next }
		}
	}
	return nil
}
`

// prefixLinks links through the urls chain both ways: URL(ctx) carries the
// mount prefix, Path() is the path alone.
const prefixLinks = `<script lang="go">
package links

import (
	"context"

	"example.com/app/pages/urls"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

// Index links to an employer's edit page.
type Index struct{}

func (p *Index) Edit() string { return urls.Employers.Employer(1).Edit.Path() }

func (p *Index) Post(ctx context.Context) web.Redirect {
	return web.Redirect(route("employers.employer", 1))
}

var _ = ui.JS
</script>

<section id="links">
  <a id="url" href={urls.Employers.Employer(1).Edit.URL(ctx)}>Edit</a>
  <a id="path" href={p.Edit()}>Edit</a>
  <a id="employers" href={urls.Employers.URL(ctx)}>Employers</a>
  <a id="route" href={route("employers.edit", 1)}>Edit</a>
  <a id="routepath" href={routePath("employers.edit", 1)}>Edit</a>
  <form id="form" method="post" action={route("employers.employer", 1)}></form>
  <button id="nav" onClick={ui.JS.Navigate(route("employers.employer", 1))}>Open</button>
  <button id="patch" onClick={ui.JS.Patch(route("employers"))}>All</button>
</section>
`

// TestFolderRoutesUnderPrefix: examples/folder-routes served under a
// prefix, by http.StripPrefix and by a proxy's X-Forwarded-Prefix: its
// folder routes match below the prefix, the urls chain's URL(ctx), route(…)
// (the example's own links) and the redirects carry it once, and Path() and
// routePath(…) are the path alone.
func TestFolderRoutesUnderPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and an app")
	}
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !webDeclares(web, "func basePrefix(") {
		t.Skipf("github.com/vuka-lang/web/v2 at %s has no mount prefix; set VUKA_WEB", web)
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
	files["main.go"] = prefixMain
	files["pages/links/index.vuka.html"] = prefixLinks
	vuka := buildVuka(t)
	root := localModule(t, web, files)
	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	bin := filepath.Join(t.TempDir(), "app")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build after gen -inplace: %v\n%s", err, out)
	}
	base, logs := runApp(t, bin, root)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("app log:\n%s", logs)
		}
	})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, mount := range []struct {
		name   string
		at     string // where the requests go
		header string // X-Forwarded-Prefix
	}{{"StripPrefix", "/gateway", ""}, {"X-Forwarded-Prefix", "", "/gateway"}} {
		t.Run(mount.name, func(t *testing.T) {
			do := func(method, path string, form url.Values, cookie bool) (int, string, string) {
				t.Helper()
				var body io.Reader
				if form != nil {
					body = strings.NewReader(form.Encode())
				}
				req, _ := http.NewRequest(method, base+mount.at+path, body)
				if form != nil {
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if mount.header != "" {
					req.Header.Set("X-Forwarded-Prefix", mount.header)
				}
				if cookie {
					req.AddCookie(&http.Cookie{Name: "admin", Value: "1"})
				}
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				b, _ := io.ReadAll(res.Body)
				return res.StatusCode, res.Header.Get("Location"), string(b)
			}
			page := func(path string, want ...string) {
				t.Helper()
				code, _, body := do("GET", path, nil, false)
				if code != http.StatusOK {
					t.Errorf("GET %s: %d, want 200\n%s", path, code, body)
					return
				}
				for _, w := range want {
					if !strings.Contains(body, w) {
						t.Errorf("GET %s has no %s:\n%s", path, w, body)
					}
				}
				if strings.Contains(body, "/gateway/gateway") {
					t.Errorf("GET %s carries the prefix twice:\n%s", path, body)
				}
			}
			// URL(ctx) carries the prefix once, Path() never; the runtime's
			// script is the app's, under the prefix.
			page("/links", `<a id="url" href="/gateway/employers/1/edit">`, `<a id="path" href="/employers/1/edit">`, `<a id="employers" href="/gateway/employers">`,
				`<a id="route" href="/gateway/employers/1/edit">`, `<a id="routepath" href="/employers/1/edit">`, `action="/gateway/employers/1"`,
				`/gateway/employers/1&#34;`, `/gateway/employers&#34;`)
			if code, _, body := do("GET", "/where", nil, false); code != http.StatusOK || !strings.Contains(body, "/gateway/employers/1/edit /employers/1/edit") {
				t.Errorf("web.Route in Go: %d %q", code, body)
			}
			if code, loc, _ := do("POST", "/links", url.Values{}, false); code != http.StatusSeeOther || loc != "/gateway/employers/1" {
				t.Errorf("a redirect to route(…): %d %q, want /gateway/employers/1", code, loc)
			}
			page("/employers", `<a href="/gateway/employers/1">Acme Logistics</a>`, `<script src="/gateway/_vuka/live.js`)
			page("/employers/1", `<a href="/gateway/employers/1/edit">Edit</a>`, `<a href="/gateway/employers/1/permits/2">Residence permit</a>`, `<a id="api" href="/gateway/api/employers/1">`)
			page("/employers/1/permits/2", `<a id="back" href="/gateway/employers/1">`, `<script src="/gateway/_vuka/live.js`)
			page("/files/handbook/leave.txt", "<h1>handbook/leave.txt</h1>")
			page("/app/settings", `<a id="open" href="/gateway/app/">`)
			page("/app/a/b/c", `<div id="app"></div>`)
			if code, loc, _ := do("GET", "/admin", nil, false); code != http.StatusSeeOther && code != http.StatusFound || loc != "/gateway/admin/auth/login" {
				t.Errorf("GET /admin without the cookie: %d %q, want a redirect to /gateway/admin/auth/login", code, loc)
			}
			if code, _, body := do("GET", "/admin", nil, true); code != http.StatusOK || !strings.Contains(body, `<a href="/gateway/admin">Dashboard</a>`) {
				t.Errorf("GET /admin with the cookie: %d\n%s", code, body)
			}
			if code, _, _ := do("GET", "/employers/x", nil, false); code != http.StatusNotFound {
				t.Errorf("GET /employers/x: %d, want 404", code)
			}
			if code, _, _ := do("GET", "/admin/console", nil, false); code != http.StatusNotFound {
				t.Errorf("GET /admin/console: %d, want 404", code)
			}
			if code, loc, body := do("POST", "/employers/1/edit", url.Values{"name": {"Acme Logistics"}, "city": {"Dar es Salaam"}}, false); code != http.StatusSeeOther || loc != "/gateway/employers/1" {
				t.Errorf("POST /employers/1/edit: %d %q, want a redirect to /gateway/employers/1\n%s", code, loc, body)
			}
		})
	}
}
