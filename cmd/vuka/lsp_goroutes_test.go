package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The pages in Go the route hovers and lenses are tried on: a page with a
// handler in a [id] folder, an endpoint in a route group, an endpoint with
// a urls chain, and a live page whose event method is in a .go file.
var goRouteFiles = map[string]string{
	"pages/users/[id]/index.go":       "package user\n\nimport \"context\"\n\n// Index is a user, as JSON.\ntype Index struct{ ID int }\n\nfunc (*Index) Get(ctx context.Context) (int, error) { return 0, nil }\n\nfunc (*Index) Delete(ctx context.Context) error { return nil }\n",
	"pages/users/[id]/edit.vuka.html": "package user\n\n// Edit edits a user.\ntype Edit struct{ ID int }\n<p>{ID}</p>\n",
	"pages/users/[id]/edit.go":        "package user\n\nimport \"context\"\n\nfunc (*Edit) Post(ctx context.Context) error { return nil }\n",
	"pages/(admin)/stats/index.go":    "package stats\n\nimport \"context\"\n\ntype Index struct{}\n\nfunc (p *Index) Post(ctx context.Context) error { return nil }\n",
	"pages/auth/logout/index.go":      "package logout\n\nimport (\n\t\"context\"\n\n\t\"example.com/app/pages/urls\"\n)\n\ntype Index struct{}\n\nfunc (p *Index) Post(ctx context.Context) (string, error) {\n\treturn urls.Orgs.New.Path(), nil\n}\n",
	"pages/counter.vuka.html":         "package pages\n\n// Counter is live: its markup sends Inc.\ntype Counter struct {\n\tn int\n}\n<button onClick={Inc}>{n}</button>\n",
	"pages/counter.go":                "package pages\n\nfunc (p *Counter) Inc() {}\n\nfunc (p *Counter) helper() {}\n",
}

func goRoutesProxy(t *testing.T) (*proxy, string) {
	t.Helper()
	app := routesModule(t, goRouteFiles)
	_, res, err := routeTable(app, "example.com/app")
	if err != nil || res == nil || len(res.routes) == 0 {
		t.Fatalf("route table: %v %+v", err, res)
	}
	if err := res.err(); err != nil {
		t.Fatalf("route table: %v", err)
	}
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.setRoutes(app, res, nil)
	return p, app
}

func TestGoRouteHover(t *testing.T) {
	p, app := goRoutesProxy(t)
	note := func(rel, needle string) string {
		t.Helper()
		path := filepath.Join(app, filepath.FromSlash(rel))
		src := p.sourceText(path)
		i := strings.Index(string(src), needle)
		if i < 0 {
			t.Fatalf("%s has no %q", rel, needle)
		}
		return p.routeNote(path, src, i+1)
	}
	for _, c := range []struct{ rel, at, want string }{
		{"pages/auth/logout/index.go", "Index struct", "**route** `POST /auth/logout` · endpoint `logout.Index.Post`\n\nlayout `pages.Layout` · middleware `pages.Middleware`"},
		{"pages/auth/logout/index.go", "Post(", "**route** `POST /auth/logout` · endpoint `logout.Index.Post`"},
		{"pages/users/[id]/index.go", "Index struct", "**routes**\n\n- `GET /users/{id}` · endpoint `user.Index.Get`\n- `DELETE /users/{id}` · endpoint `user.Index.Delete`"},
		{"pages/users/[id]/index.go", "Delete(", "**route** `DELETE /users/{id}` · endpoint `user.Index.Delete`"},
		{"pages/users/[id]/edit.go", "Post(", "**route** `POST /users/{id}/edit` · handler `user.Edit.Post`"},
		{"pages/users/[id]/edit.vuka.html", "Edit struct", "**routes**\n\n- `GET /users/{id}/edit` · page `user.Edit`\n- `POST /users/{id}/edit` · handler `user.Edit.Post`"},
		{"pages/(admin)/stats/index.go", "Index struct", "**route** `POST /stats` · endpoint `stats.Index.Post`"},
		{"pages/api/orgs/index.go", "Index struct", "**routes**\n\n- `GET /api/orgs` · endpoint `orgs.Index.Get`\n- `POST /api/orgs` · endpoint `orgs.Index.Post`\n\nguard `api.Guard` · layout `pages.Layout` · middleware `pages.Middleware`, `api.Middleware`"},
		{"pages/api/orgs/index.go", "Get(", "**route** `GET /api/orgs` · endpoint `orgs.Index.Get`"},
		{"pages/counter.go", "Inc(", "**event** `Inc` of the page `pages.Counter` · `GET /counter` · runs on the server · patches the page"},
		{"pages/api/pets/index.vuka", "POST(", "**route** `POST /api/pets` · endpoint `pets.POST`"},
		{"pages/orgs/[org]/index.vuka.html", "Delete(", "**route** `DELETE /orgs/{org}` · handler `org.Index.Delete`"},
	} {
		if got := note(c.rel, c.at); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s, on %q:\n%s\nwant it to start\n%s", c.rel, c.at, got, c.want)
		}
	}
	for _, c := range []struct{ rel, at string }{
		{"pages/counter.go", "helper"},
		{"pages/api/orgs/index.go", "Query struct"},
		{"pages/api/orgs/index.go", "ctx context"},
		{"pages/auth/logout/index.go", "urls.Orgs"},
		{"pages/orgs/form.go", "Form struct"},
		{"pages/api/pets/index.vuka", "Pet struct"},
		{"codes/codes.go", "Code string"},
	} {
		if got := note(c.rel, c.at); got != "" {
			t.Errorf("%s, on %q: %q, want no note", c.rel, c.at, got)
		}
	}
}

func TestGoRouteLens(t *testing.T) {
	p, app := goRoutesProxy(t)
	titles := func(rel string) []string {
		t.Helper()
		var out []string
		for _, l := range p.codeLenses(filepath.Join(app, filepath.FromSlash(rel))) {
			m := l.(map[string]any)
			out = append(out, itoaTest(m["range"].(lspRange).Start.Line+1)+": "+m["command"].(map[string]any)["title"].(string))
		}
		return out
	}
	for rel, want := range map[string]string{
		"pages/auth/logout/index.go":   "9: POST /auth/logout · endpoint · layout pages.Layout|11: POST /auth/logout · endpoint · layout pages.Layout",
		"pages/users/[id]/index.go":    "6: GET, DELETE /users/{id} · endpoint · layout pages.Layout|8: GET /users/{id} · endpoint · layout pages.Layout|10: DELETE /users/{id} · endpoint · layout pages.Layout",
		"pages/users/[id]/edit.go":     "5: POST /users/{id}/edit · handler · layout pages.Layout",
		"pages/(admin)/stats/index.go": "5: POST /stats · endpoint · layout pages.Layout|7: POST /stats · endpoint · layout pages.Layout",
		"pages/api/orgs/index.go":      "6: GET, POST /api/orgs · endpoint · layout pages.Layout · guard api.Guard|11: GET /api/orgs · endpoint · layout pages.Layout · guard api.Guard|16: POST /api/orgs · endpoint · layout pages.Layout · guard api.Guard",
		"pages/orgs/form.go":           "",
		"codes/codes.go":               "",
	} {
		if got := strings.Join(titles(rel), "|"); got != want {
			t.Errorf("%s lenses:\n%s\nwant\n%s", rel, got, want)
		}
	}
	path := filepath.Join(app, "pages", "auth", "logout", "index.go")
	src := p.sourceText(path)
	loc, ok := p.routeTargetAt(path, src, strings.Index(string(src), "New.Path"))
	if !ok || !strings.HasSuffix(loc["uri"].(string), "new.vuka.html") {
		t.Errorf("definition on urls.Orgs.New.Path(): %v", loc)
	}
}

// TestLSPGoRoutes: through gopls, a hover on a page package's .go file
// keeps gopls's hover with the route below it, and its code lenses come
// with the route lenses; a .go file outside the pages has gopls's alone.
func TestLSPGoRoutes(t *testing.T) {
	app := routesModule(t, goRouteFiles)
	c := startLSPAt(t, app, lspOptions{renameCommands: true})
	open := func(rel string) (string, string) {
		path := filepath.Join(app, filepath.FromSlash(rel))
		src := string(p0(path))
		uri := pathToURI(path)
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": src}})
		return uri, src
	}
	hover := func(uri, src, needle string) string {
		t.Helper()
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), strings.Index(src, needle)+1)}).(map[string]any)
		if h == nil {
			return ""
		}
		v, _ := h["contents"].(map[string]any)["value"].(string)
		return v
	}
	lensTitles := func(uri string) string {
		lenses, _ := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": uri}}).([]any)
		var out []string
		for _, l := range lenses {
			if cmd, ok := l.(map[string]any)["command"].(map[string]any); ok && cmd["command"] == "vuka.showRoute" {
				out = append(out, cmd["title"].(string))
			}
		}
		return strings.Join(out, "|")
	}

	logout, logoutSrc := open("pages/auth/logout/index.go")
	user, userSrc := open("pages/users/[id]/index.go")
	stats, statsSrc := open("pages/(admin)/stats/index.go")
	codes, codesSrc := open("codes/codes.go")
	// The route table is the first build's.
	var titles string
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if titles = lensTitles(logout); titles != "" {
			break
		}
	}
	if titles != "POST /auth/logout · endpoint · layout pages.Layout|POST /auth/logout · endpoint · layout pages.Layout" {
		t.Errorf("logout lenses: %q", titles)
	}
	for _, c := range []struct{ uri, src, at, gopls, note string }{
		{logout, logoutSrc, "Index struct", "type Index struct", "**route** `POST /auth/logout` · endpoint `logout.Index.Post`"},
		{logout, logoutSrc, "Post(", "func (p *Index) Post", "**route** `POST /auth/logout` · endpoint `logout.Index.Post`"},
		{user, userSrc, "Index struct", "type Index struct", "- `DELETE /users/{id}` · endpoint `user.Index.Delete`"},
		{user, userSrc, "Delete(", "Delete(ctx context.Context) error", "**route** `DELETE /users/{id}` · endpoint `user.Index.Delete`"},
		{stats, statsSrc, "Post(", "func (p *Index) Post", "**route** `POST /stats` · endpoint `stats.Index.Post`"},
	} {
		got := hover(c.uri, c.src, c.at)
		g, n := strings.Index(got, c.gopls), strings.Index(got, c.note)
		if g < 0 || n < 0 || n < g {
			t.Errorf("hover on %q of %s:\n%s\nwant gopls's %q, then %q", c.at, c.uri, got, c.gopls, c.note)
		}
	}
	if got := lensTitles(user); !strings.Contains(got, "GET, DELETE /users/{id} · endpoint") {
		t.Errorf("[id] lenses: %q", got)
	}
	if got := lensTitles(stats); !strings.Contains(got, "POST /stats · endpoint") {
		t.Errorf("(admin) lenses: %q", got)
	}
	if got := hover(codes, codesSrc, "Code string"); !strings.Contains(got, "type Code string") || strings.Contains(got, "route") {
		t.Errorf("hover outside the pages: %q", got)
	}
	if got := lensTitles(codes); got != "" {
		t.Errorf("lenses outside the pages: %q", got)
	}
	locs, _ := c.call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": logout},
		"position": positionOf([]byte(logoutSrc), strings.Index(logoutSrc, "New.Path"))}).([]any)
	if len(locs) != 1 || !strings.HasSuffix(locs[0].(map[string]any)["uri"].(string), "new.vuka.html") {
		t.Errorf("definition on urls.Orgs.New.Path(): %v", locs)
	}
}
