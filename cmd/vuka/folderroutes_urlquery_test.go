package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// queryPages is a tree of pages keeping fields in the query: pages/ajira
// is the only folder, so AdvertsView drops it; the orgs page has a path
// parameter and a bare @ui.URL beside its query fields.
var queryPages = map[string]string{
	"pages/ajira/about.vuka.html": "package ajira\n\nimport \"example.com/app/pages/urls\"\n\n// About has no query, and links to a page that has.\ntype About struct{}\n<a href={urls.Ajira.Adverts.View.URL(ctx, urls.AdvertsView{Tab: \"x\"})}>view</a>\n",
	"pages/ajira/adverts/index.vuka.html": `package adverts

import "github.com/vuka-lang/ui/v2"

// Index lists the adverts.
type Index struct {
	ui.Live
	Page int      @ui.URL("page").Default(1)
	Tags []string @ui.URL("tag")
	Q    string   @ui.URL("q")
}
<p>{Page}</p>
`,
	"pages/ajira/adverts/view.vuka.html": `package adverts

import (
	"github.com/vuka-lang/ui/v2"

	"example.com/app/codes"
)

// View shows one advert.
type View struct {
	ui.Live
	Tab    string      @ui.URL("tab").Default("overview")
	Status codes.Code  @ui.URL("status").Default(codes.Open)
	Ratio  float64     @ui.URL("ratio")
	Open   bool        @ui.URL("open")
	Owner  *int        @ui.URL("owner")
	hidden int         @ui.URL("hidden")
	Kind   Kind        @ui.URL("kind")
}

// Kind is declared in the page's own package, which urls can't import.
type Kind string
<p>{Tab}</p>
`,
	"pages/ajira/orgs/[org]/edit.vuka.html": `package org

import "github.com/vuka-lang/ui/v2"

// Edit edits an organisation.
type Edit struct {
	ui.Live
	Org int    @ui.URL
	Tab string @ui.URL("tab")
}
<p>{Tab}</p>
`,
}

func queryModule(t *testing.T, files map[string]string) string {
	t.Helper()
	app := routesModuleAt(t, nil)
	os.RemoveAll(filepath.Join(app, "pages"))
	writeFile(t, filepath.Join(app, "codes/codes.go"), `// Package codes is the permits' codes.
package codes

import "strings"

// Code is a permit's code, written in upper case.
type Code string

const Open Code = "open"

func (c Code) MarshalText() ([]byte, error) { return []byte(strings.ToUpper(string(c))), nil }

func (c *Code) UnmarshalText(b []byte) error { *c = Code(strings.ToLower(string(b))); return nil }
`)
	for name, src := range queryPages {
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

func genURLs(t *testing.T, app string) string {
	t.Helper()
	var out bytes.Buffer
	err := vukaIn(app, &out, &out, "gen", "-inplace")
	b, _ := os.ReadFile(filepath.Join(app, "pages/urls/urls_gen.go"))
	if err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out.Bytes())
	}
	return string(b)
}

// TestURLQuery: a page keeping fields in the query gets a struct in urls
// named for its path, and a URL taking it; the file compiles, the pages
// without such fields are as before, and a type urls can't name is left
// out with a warning.
func TestURLQuery(t *testing.T) {
	t.Parallel()
	app := queryModule(t, nil)
	urls := genURLs(t, app)
	checkGolden(t, "urlquery_urls_gen.go", []byte(urls))
	if !strings.Contains(urls, "About   pageURL") || strings.Contains(urls, "Kind") && strings.Contains(urls, "// ?kind=") {
		t.Errorf("a page with no query fields is not a pageURL, or the page-package type is in a query:\n%s", urls)
	}
	var errOut bytes.Buffer
	if err := vukaIn(app, io.Discard, &errOut, "vet", "./..."); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
	errOut.Reset()
	if err := vukaIn(app, io.Discard, &errOut, "routes"); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	if got := errOut.String(); !strings.Contains(got, "W0705") || !strings.Contains(got, "View.Kind") || strings.Count(got, "warning[W0705]") != 1 {
		t.Errorf("no W0705 for the field urls can't name, or more than that:\n%s", got)
	}
}

const queryRoundTrip = `package adverts

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/vuka-lang/ui/v2"

	"example.com/app/codes"
	"example.com/app/pages/urls"
)

func fill(t *testing.T, c ui.Stateful, u string) {
	t.Helper()
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	var f ui.URLFields
	f.Fill(context.Background(), c, nil, "", false, parsed.Query())
}

func TestURLs(t *testing.T) {
	ctx := context.Background()
	seven := 7
	for _, tc := range []struct {
		q    urls.AdvertsView
		want string
	}{
		{urls.AdvertsView{}, "/ajira/adverts/view"},
		{urls.AdvertsView{Tab: "overview", Status: "open"}, "/ajira/adverts/view"},
		{urls.AdvertsView{Tab: "a b&c", Status: "closed", Ratio: 1.5, Open: true, Owner: &seven, Hidden: 3},
			"/ajira/adverts/view?hidden=3&open=true&owner=7&ratio=1.5&status=CLOSED&tab=a+b%26c"},
	} {
		got := urls.Ajira.Adverts.View.URL(ctx, tc.q)
		if got != tc.want {
			t.Errorf("%+v: %s, want %s", tc.q, got, tc.want)
		}
		v := &View{}
		fill(t, v, got)
		want := tc.q
		if want.Tab == "" {
			want.Tab = "overview"
		}
		if want.Status == "" {
			want.Status = codes.Open
		}
		have := urls.AdvertsView{Tab: v.Tab, Status: v.Status, Ratio: v.Ratio, Open: v.Open, Owner: v.Owner, Hidden: v.hidden.Get()}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("%s filled %+v, want %+v", got, have, want)
		}
	}
	if got := urls.Ajira.Adverts.View.URL(ctx); got != "/ajira/adverts/view" {
		t.Errorf("no query: %s", got)
	}
	if got := urls.Ajira.Adverts.View.URL(ctx, urls.AdvertsView{Tab: "x"}, urls.AdvertsView{Tab: "y"}); got != "/ajira/adverts/view?tab=x" {
		t.Errorf("two queries: %s", got)
	}
	if got := urls.Ajira.Adverts.View.Path(); got != "/ajira/adverts/view" {
		t.Errorf("Path: %s", got)
	}

	q := urls.Adverts{Page: 2, Tags: []string{"b", "a c"}, Q: "x"}
	got := urls.Ajira.Adverts.URL(ctx, q)
	if want := "/ajira/adverts?page=2&q=x&tag=b&tag=a+c"; got != want {
		t.Errorf("%s, want %s", got, want)
	}
	idx := &Index{}
	fill(t, idx, got)
	if idx.Page != 2 || idx.Q != "x" || !reflect.DeepEqual(idx.Tags, q.Tags) {
		t.Errorf("%s filled %+v", got, idx)
	}
	if got := urls.Ajira.Adverts.URL(ctx, urls.Adverts{Page: 1}); got != "/ajira/adverts" {
		t.Errorf("the default page: %s", got)
	}
	if got := urls.Ajira.Orgs.Org(7).Edit.URL(ctx, urls.OrgsOrgEdit{Tab: "x"}); got != "/ajira/orgs/7/edit?tab=x" {
		t.Errorf("a path parameter and a query: %s", got)
	}
	if got := urls.Ajira.About.URL(ctx); got != "/ajira/about" {
		t.Errorf("a page without a query: %s", got)
	}
}
`

// TestURLQueryRoundTrip: a URL written from a query struct, read back by
// the page's @ui.URL fields, gives the values written, defaults and zero
// values left out.
func TestURLQueryRoundTrip(t *testing.T) {
	t.Parallel()
	app := queryModule(t, map[string]string{"pages/ajira/adverts/urls_test.go": queryRoundTrip})
	genURLs(t, app)
	var out bytes.Buffer
	if err := vukaIn(app, &out, &out, "test", "./pages/ajira/adverts"); err != nil {
		t.Fatalf("vuka test: %v\n%s", err, out.Bytes())
	}
}

// TestURLQueryNames: a struct is named for its page's path less the
// folders every page is in; a name the root value has gets Query, and a
// repeat a number.
func TestURLQueryNames(t *testing.T) {
	build := func(pages ...string) map[string]string {
		root := &urlNode{}
		for _, p := range pages {
			dir, leaf := "", p
			if i := strings.LastIndex(p, "/"); i >= 0 {
				dir, leaf = p[:i], p[i+1:]
			}
			n := root.at(dir)
			if leaf == "" {
				n.index = true
				n.queries = map[string]*urlQuery{"": {segs: n.nameSegs("")}}
				continue
			}
			n.pages = append(n.pages, leaf)
			if n.queries == nil {
				n.queries = map[string]*urlQuery{}
			}
			n.queries[leaf] = &urlQuery{segs: n.nameSegs(leaf)}
		}
		root.mark()
		root.nameQueries()
		got := map[string]string{}
		var walk func(n *urlNode)
		walk = func(n *urlNode) {
			for leaf, q := range n.queries {
				got[joinRel(n.rel, leaf)] = q.name
			}
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(root)
		return got
	}
	for _, tc := range []struct {
		name  string
		pages []string
		want  map[string]string
	}{
		{"shared folders dropped", []string{"ajira/adverts/View", "ajira/orgs/Edit", "ajira/About"},
			map[string]string{"ajira/adverts/View": "AdvertsView", "ajira/orgs/Edit": "OrgsEdit"}},
		{"a lone page keeps its last name", []string{"ajira/adverts/View"},
			map[string]string{"ajira/adverts/View": "View"}},
		{"a parameter folder", []string{"orgs/[org]/Edit", "orgs/New"},
			map[string]string{"orgs/[org]/Edit": "OrgEdit"}},
		{"a repeat", []string{"a-b/C", "a/b-c/", "x/Y"},
			map[string]string{"a/b-c": "ABC", "a-b/C": "ABC2", "x/Y": "XY"}},
		{"a name of the root value", []string{"Adverts", "Orgs/Edit"},
			map[string]string{"Adverts": "AdvertsQuery", "Orgs/Edit": "OrgsEdit"}},
		{"the root index", []string{"", "ajira/adverts/View"},
			map[string]string{"": "Index", "ajira/adverts/View": "View"}},
	} {
		got := build(tc.pages...)
		for page, name := range tc.want {
			if got[page] != name {
				t.Errorf("%s: %s is %q, want %q (all: %v)", tc.name, page, got[page], name, got)
			}
		}
	}
}
