package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixRouteLinks: vuka fix routes writes the urls chains it can name by
// route name, leaves the others with W0706, and changes nothing run again.
func TestFixRouteLinks(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("testdata", "fixroutes", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	app := routesModuleAt(t, map[string]string{"pages/links.vuka.html": read("links.vuka.html"), "handlers.go": read("handlers.go")})
	var out bytes.Buffer
	if err := vukaIn(app, &out, &out, "fix", "routes"); err != nil {
		t.Fatalf("vuka fix routes: %v\n%s", err, out.Bytes())
	}
	for _, w := range []string{"pages/links.vuka.html: 4 urls chains written by route name", "handlers.go: 1 urls chain written by route name",
		"pages/links.vuka.html:17: W0706 urls.Orgs.New.Path(): Path() in a template's Go", "handlers.go:10: W0706 urls.Orgs: the chain is kept or passed along"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("want %q in:\n%s", w, out.Bytes())
		}
	}
	for _, f := range []string{"pages/links.vuka.html", "handlers.go"} {
		b, _ := os.ReadFile(filepath.Join(app, f))
		checkGolden(t, "../fixroutes/"+filepath.Base(f)+".fixed", b)
	}
	out.Reset()
	if err := vukaIn(app, &out, &out, "fix", "routes"); err != nil || strings.Contains(out.String(), "written by route name") {
		t.Errorf("vuka fix routes again: %v\n%s", err, out.Bytes())
	}
}

// TestFixRouteLinksShadowed: where a template's Go names a variable route,
// the builtin is hidden, so its links are written as web.RouteURL.
func TestFixRouteLinksShadowed(t *testing.T) {
	t.Parallel()
	links, err := os.ReadFile(filepath.Join("testdata", "fixroutes", "links.vuka.html"))
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(string(links), "func (l *Links) Kept()", "func (l *Links) Doc(ctx context.Context, route string) string { return route + urls.Orgs.Org(l.ID).URL(ctx) }\n\nfunc (l *Links) Kept()", 1)
	app := routesModuleAt(t, map[string]string{"pages/links.vuka.html": src})
	var out bytes.Buffer
	if err := vukaIn(app, &out, &out, "fix", "routes"); err != nil {
		t.Fatalf("vuka fix routes: %v\n%s", err, out.Bytes())
	}
	b, _ := os.ReadFile(filepath.Join(app, "pages/links.vuka.html"))
	got := string(b)
	for _, w := range []string{`return route + web.RouteURL(ctx, "orgs.org", l.ID)`, `"github.com/vuka-lang/web/v2"`, "pages/links.vuka.html:25: W0706"} {
		if !strings.Contains(got+out.String(), w) {
			t.Errorf("want %q in:\n%s\n%s", w, got, out.Bytes())
		}
	}
	if err := vukaIn(app, &out, &out, "vet"); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, out.Bytes())
	}
}
