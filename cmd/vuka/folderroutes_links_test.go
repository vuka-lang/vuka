package main

import (
	"bytes"
	"strings"
	"testing"
)

// aboutLinking is pages/about.vuka.html linking with markup.
func aboutLinking(markup string) map[string]string {
	return map[string]string{"pages/about.vuka.html": "<script lang=\"go\">\npackage pages\n\n// About links.\ntype About struct {\n\tID int64\n}\n</script>\n\n" + markup + "\n"}
}

// TestRouteNames: every route is named by its folders, parameters and
// groups adding nothing unless two paths would share a name.
func TestRouteNames(t *testing.T) {
	t.Parallel()
	got := nameRoutes([]string{"/", "/about", "/orgs", "/orgs/new", "/orgs/{org}", "/orgs/{org}/permits/{permit}",
		"/orgs/{org}/team-settings", "/files/{file...}", "/api/pets", "/api/pets/{id}", "/{slug}"})
	want := map[string]string{"/": "index", "/about": "about", "/orgs": "orgs", "/orgs/new": "orgs.new", "/orgs/{org}": "orgs.org",
		"/orgs/{org}/permits/{permit}": "orgs.permits", "/orgs/{org}/team-settings": "orgs.team-settings", "/files/{file...}": "files",
		"/api/pets": "api.pets", "/api/pets/{id}": "api.pets.id", "/{slug}": "slug"}
	for p, n := range want {
		if got[p] != n {
			t.Errorf("%s is named %q, want %q", p, got[p], n)
		}
	}
}

// TestNamedRouteLinks: route() and routePath() in markup and Go are checked
// against the route names when the page transpiles.
func TestNamedRouteLinks(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, markup string
		want         []string // in vuka routes' errors; none when empty
		more         map[string]string
	}{
		{"good links", `<a href={route("orgs.org", ID)}>org</a> <a href={routePath("orgs.permits", 1, "p7")}>p</a> <a href={route("index")}>home</a>`, nil, nil},
		{"an unknown name", `<p><a href={route("org.org", ID)}>org</a></p>`,
			[]string{"E0725", `route("org.org"): no route is named org.org`, "did you mean orgs.org?", "pages/about.vuka.html:10:19"}, nil},
		{"too few parameters", `<a href={route("orgs.permits", ID)}>p</a>`,
			[]string{"E0726", "gives 1 path argument: orgs.permits (/orgs/[org]/permits/[permit]) takes org int, permit codes.Code"}, nil},
		{"a string for a number", `<a href={route("orgs.org", "x")}>org</a>`,
			[]string{"E0726", "its first argument is a string, and orgs.org (/orgs/[org]) takes org int"}, nil},
		{"a query", `<a href={route("orders", query{"page": 2, "q": "x"})}>o</a>`, nil, ordersPage},
		{"a query key the page doesn't keep", `<a href={route("orders", query{"pag": 2})}>o</a>`,
			[]string{"E0727", `route("orders", query{"pag": …}): pages.Orders keeps no ?pag in its query`, "did you mean page?", "the page's @ui.URL fields keep page, q", "pages/about.vuka.html:10:32"}, ordersPage},
		{"a link in a component", `<p>x</p>`, []string{"E0725", "components/nav.vuka.html:7"},
			map[string]string{"components/nav.vuka.html": "<script lang=\"go\">\npackage components\n\ntype Nav struct{}\n</script>\n\n<a href={route(\"orgs.nope\", 1)}>x</a>\n",
				"pages/about.vuka.html": "<script lang=\"go\">\npackage pages\n\nimport \"example.com/app/components\"\n\n// About links.\ntype About struct{}\n</script>\n\n<components.Nav/>\n"}},
		{"a link in Go", `<p>x</p>`, []string{"E0725", "pages/go.vuka:6"},
			map[string]string{"pages/go.vuka": "package pages\n\nimport \"context\"\n\nfunc next(ctx context.Context) string {\n\treturn route(\"abuot\")\n}\n"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			files := aboutLinking(c.markup)
			for k, v := range c.more {
				files[k] = v
			}
			app := routesModuleAt(t, files)
			var out, errOut bytes.Buffer
			err := vukaIn(app, &out, &errOut, "routes")
			if len(c.want) == 0 {
				if err != nil {
					t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
				}
				return
			}
			if err == nil {
				t.Fatalf("no error:\n%s", out.Bytes())
			}
			for _, w := range c.want {
				if !strings.Contains(errOut.String(), w) {
					t.Errorf("want %q in:\n%s", w, errOut.Bytes())
				}
			}
		})
	}
}

var ordersPage = map[string]string{"pages/orders.vuka.html": "<script lang=\"go\">\npackage pages\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Orders lists orders.\ntype Orders struct {\n\tui.Live\n\tpage int    @ui.URL(\"page\")\n\tq    string @ui.URL(\"q\")\n}\n</script>\n\n<p>{page} {q}</p>\n"}
