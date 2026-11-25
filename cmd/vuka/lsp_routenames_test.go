package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const routeLinksPage = "<script lang=\"go\">\npackage pages\n\n// Links links.\ntype Links struct{ ID int }\n</script>\n\n<a href={route(\"orgs.org\", ID)}>org</a> <a href={route(\"orgs.permits\", ID, \"x\")}>permit</a>\n"

func routeItemLabels(v any) []string {
	var out []string
	b, _ := json.Marshal(v)
	var r struct {
		Items []struct {
			Label  string `json:"label"`
			Detail string `json:"detail"`
		} `json:"items"`
	}
	json.Unmarshal(b, &r)
	for _, it := range r.Items {
		out = append(out, it.Label+" | "+it.Detail)
	}
	return out
}

func TestRouteNameEditor(t *testing.T) {
	p, app := routesProxy(t)
	path := filepath.Join(app, "pages", "links.vuka.html")
	src := []byte(routeLinksPage)
	ask := func(method string, off int) any {
		h := p.routeNameRequest(method, path, src, off)
		if h == nil {
			return nil
		}
		return h()
	}
	in := strings.Index(routeLinksPage, "orgs.org") + 2
	items := strings.Join(routeItemLabels(ask("textDocument/completion", in)), "\n")
	for _, want := range []string{"orgs.org | orgs.org (/orgs/[org]) takes org int", "index | index (/) takes no path parameters", "api.pets.id | "} {
		if !strings.Contains(items, want) {
			t.Errorf("completion lacks %q:\n%s", want, items)
		}
	}
	b, _ := json.Marshal(ask("textDocument/hover", in))
	if !strings.Contains(string(b), "**orgs.org** `/orgs/[org]`") || !strings.Contains(string(b), "takes `org int`") || !strings.Contains(string(b), "org.Index") {
		t.Errorf("hover: %s", b)
	}
	if !strings.Contains(string(b), `route(\"orgs.org\", org)`) {
		t.Errorf("hover lacks an example: %s", b)
	}
	b, _ = json.Marshal(ask("textDocument/hover", strings.Index(routeLinksPage, "route(")+2))
	if !strings.Contains(string(b), "**route**: the URL of a route") || !strings.Contains(string(b), `route(\"orgs.org\", org)`) {
		t.Errorf("hover on route: %s", b)
	}
	b, _ = json.Marshal(ask("textDocument/definition", in))
	if !strings.Contains(string(b), "index.vuka.html") || !strings.Contains(string(b), "orgs/") {
		t.Errorf("definition: %s", b)
	}
	at := strings.Index(routeLinksPage, `ID, "x"`) + len("ID, ")
	b, _ = json.Marshal(ask("textDocument/signatureHelp", at))
	if !strings.Contains(string(b), `"label":"route(\"orgs.permits\", org int, permit codes.Code)"`) || !strings.Contains(string(b), `"activeParameter":1`) {
		t.Errorf("signature help: %s", b)
	}
	if ask("textDocument/completion", strings.Index(routeLinksPage, "<a")) != nil {
		t.Error("a completion off any link")
	}
}

func TestRouteNamesBeforeABuild(t *testing.T) {
	app := routesModule(t, nil)
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.modRoot = app
	src := []byte(`<a href={route("or")}>x</a>`)
	h := p.routeNameRequest("textDocument/completion", filepath.Join(app, "pages", "x.vuka.html"), src, strings.Index(string(src), "or")+2)
	if h == nil {
		t.Fatal("no completion")
	}
	items := strings.Join(routeItemLabels(h()), "\n")
	for _, want := range []string{"orgs.org | orgs.org (/orgs/[org]) takes org", "orgs.new |", "about |"} {
		if !strings.Contains(items, want) {
			t.Errorf("completion before a build lacks %q:\n%s", want, items)
		}
	}
}

func TestRouteLinkDiags(t *testing.T) {
	p, app := routesProxy(t)
	path := filepath.Join(app, "pages", "links.vuka.html")
	src := []byte(`<a href={route("orgs.ogr", 1)}>x</a> <a href={route("orgs.org")}>y</a>`)
	ds := p.routeLinkDiags(path, src)
	b, _ := json.Marshal(ds)
	if len(ds) != 2 || !strings.Contains(string(b), `"code":"E0725"`) || !strings.Contains(string(b), `"code":"E0726"`) ||
		!strings.Contains(string(b), `"title":"Change to \"orgs.org\""`) {
		t.Errorf("diagnostics: %s", b)
	}
}

func TestWebRouteInGo(t *testing.T) {
	p, app := routesProxy(t)
	path := filepath.Join(app, "handlers.go")
	src := []byte("package main\n\nfunc f(ctx context.Context) string { return web.Route(ctx, \"orgs.n\") }\n")
	h := p.routeNameRequest("textDocument/completion", path, src, strings.Index(string(src), "orgs.n")+6)
	if h == nil || !strings.Contains(strings.Join(routeItemLabels(h()), "\n"), "orgs.new |") {
		t.Errorf("web.Route completion in Go")
	}
	if p.routeNameRequest("textDocument/completion", path, []byte(`x := route("orgs")`), 14) != nil {
		t.Error("a route builtin completes in a .go file")
	}
}

func TestRouteQueryKeys(t *testing.T) {
	app := routesModule(t, ordersPage)
	_, res, err := routeTable(app, "example.com/app")
	if err != nil || res == nil {
		t.Fatalf("route table: %v", err)
	}
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.setRoutes(app, res, nil)
	src := []byte(`<a href={route("orders", query{"p": 2})}>x</a>`)
	h := p.routeNameRequest("textDocument/completion", filepath.Join(app, "pages", "x.vuka.html"), src, strings.Index(string(src), `"p"`)+2)
	if h == nil {
		t.Fatal("no completion of a query key")
	}
	if got := strings.Join(routeItemLabels(h()), "\n"); got != "page | page int\nq | q string" {
		t.Errorf("query keys:\n%s", got)
	}
	h = p.routeNameRequest("textDocument/signatureHelp", filepath.Join(app, "pages", "x.vuka.html"), src, strings.Index(string(src), "query"))
	if b, _ := json.Marshal(h()); !strings.Contains(string(b), `route(\"orders\", query{…})`) {
		t.Errorf("signature help: %s", b)
	}
}
