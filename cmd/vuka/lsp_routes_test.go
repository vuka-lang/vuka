package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const linksPage = "package pages\n\nimport \"example.com/app/pages/urls\"\n\ntype Links struct{}\n<nav><a href=\"/orgs/new\">New</a> <a href=\"/orgs/7?tab=x\">Org</a> <a href={urls.Orgs.Org(7).TeamSettings.Path()}>Team</a></nav>\n"

// routesProxy is a proxy holding the route table of the routes fixture.
func routesProxy(t *testing.T) (*proxy, string) {
	t.Helper()
	app := routesModule(t, map[string]string{"pages/links.vuka.html": linksPage})
	_, res, err := routeTable(app, "example.com/app")
	if err != nil || res == nil || len(res.routes) == 0 {
		t.Fatalf("route table: %v", err)
	}
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.setRoutes(app, res, nil)
	return p, app
}

func TestRouteCodeLens(t *testing.T) {
	p, app := routesProxy(t)
	lenses := p.codeLenses(filepath.Join(app, "pages", "orgs", "new.vuka.html"))
	if len(lenses) != 2 {
		t.Fatalf("lenses: %v", lenses)
	}
	title := func(i int) string { return lenses[i].(map[string]any)["command"].(map[string]any)["title"].(string) }
	if title(0) != "GET /orgs/new · page · layout pages.Layout" || title(1) != "POST /orgs/new · handler · layout pages.Layout" {
		t.Errorf("titles %q, %q", title(0), title(1))
	}
	cmd := lenses[0].(map[string]any)["command"].(map[string]any)
	args := cmd["arguments"].([]any)
	entry := args[0].(map[string]any)
	if cmd["command"] != "vuka.showRoute" || entry["file"] != "pages/orgs/new.vuka.html" || entry["line"] != typeLine(t, app, "pages/orgs/new.vuka.html") || entry["page"] != "orgs.New" {
		t.Errorf("command %v", cmd)
	}
	if r := lenses[0].(map[string]any)["range"].(lspRange); int(r.Start.Line) != typeLine(t, app, "pages/orgs/new.vuka.html")-1 {
		t.Errorf("lens on line %d, want the type's", r.Start.Line)
	}
	lenses = p.codeLenses(filepath.Join(app, "pages", "orgs", "[org]", "permits", "[permit].vuka.html"))
	if len(lenses) != 1 || !strings.Contains(lenses[0].(map[string]any)["command"].(map[string]any)["title"].(string), "· live · layout org.Layout · guard org.Guard") {
		t.Errorf("permit lens %v", lenses)
	}
}

func TestRouteLinks(t *testing.T) {
	p, app := routesProxy(t)
	path := filepath.Join(app, "pages", "links.vuka.html")
	vf := &vfile{source: path, from: []byte(linksPage)}
	links, _ := p.withRouteLinks([]any{}, vf).([]any)
	targets := map[string]string{}
	for _, l := range links {
		m := l.(map[string]any)
		r := m["range"].(lspRange)
		text := string(vf.from[offsetOf(vf.from, r.Start):offsetOf(vf.from, r.End)])
		targets[text] = m["target"].(string)
	}
	// A link lands on the line of the page's type (#L is from 1).
	want := map[string]string{
		"/orgs/new":                            "pages/orgs/new.vuka.html#L" + strconv.Itoa(typeLine(t, app, "pages/orgs/new.vuka.html")),
		"/orgs/7?tab=x":                        "pages/orgs/%5Borg%5D/index.vuka.html#L" + strconv.Itoa(typeLine(t, app, "pages/orgs/[org]/index.vuka.html")),
		"urls.Orgs.Org(7).TeamSettings.Path()": "pages/orgs/%5Borg%5D/team-settings.vuka.html#L" + strconv.Itoa(typeLine(t, app, "pages/orgs/[org]/team-settings.vuka.html")),
	}
	for text, suffix := range want {
		got := targets[text]
		if got == "" {
			got = targets[strings.ReplaceAll(text, "%5B", "[")]
		}
		if !strings.HasSuffix(strings.ReplaceAll(got, "%5B", "["), strings.NewReplacer("%5B", "[", "%5D", "]").Replace(suffix)) &&
			!strings.HasSuffix(got, suffix) {
			t.Errorf("link %q → %q, want …%s (all: %v)", text, got, suffix, targets)
		}
	}

	src := []byte(linksPage)
	loc, ok := p.routeTargetAt(path, src, strings.Index(linksPage, "TeamSettings"))
	if !ok || !strings.HasSuffix(loc["uri"].(string), "team-settings.vuka.html") {
		t.Fatalf("definition: %v", loc)
	}
	if r := loc["range"].(lspRange); int(r.Start.Line) != typeLine(t, app, "pages/orgs/[org]/team-settings.vuka.html")-1 || int(r.End.Character-r.Start.Character) != len("TeamSettings") {
		t.Errorf("definition lands on %v, want the type's name", r)
	}
	if _, ok := p.routeTargetAt(path, src, strings.Index(linksPage, "<nav>")); ok {
		t.Error("a definition off any link")
	}
}

func TestRouteReferences(t *testing.T) {
	p, app := routesProxy(t)
	path := filepath.Join(app, "pages", "orgs", "new.vuka.html")
	src := p.sourceText(path)
	pages := p.pageRoutesAt(path, src, strings.Index(string(src), "New struct"))
	if len(pages) != 1 || pages[0].Method != "GET" {
		t.Fatalf("routes of New: %d", len(pages))
	}
	params, _ := json.Marshal(map[string]any{"position": positionOf(src, strings.Index(string(src), "New struct"))})
	refs := p.pageReferences(params, path, pages)
	var files []string
	for _, r := range refs {
		rel, _ := filepath.Rel(app, uriToPath(r.(map[string]any)["uri"].(string)))
		files = append(files, rel)
	}
	got := strings.Join(files, " ")
	if !strings.Contains(got, filepath.Join("pages", "orgs", "index.vuka.html")) || !strings.Contains(got, "links.vuka.html") {
		t.Errorf("references of orgs.New: %v", files)
	}
	if len(p.pageRoutesAt(path, src, strings.Index(string(src), "Post("))) != 0 {
		t.Error("references off the page's type name")
	}
}

func TestWillRenamePages(t *testing.T) {
	p, app := routesProxy(t)
	rename := func(from, to string) map[string]string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"files": []any{map[string]any{
			"oldUri": pathToURI(filepath.Join(app, from)), "newUri": pathToURI(filepath.Join(app, to))}}})
		v := p.renameEdits(raw)
		out := map[string]string{}
		if v == nil {
			return out
		}
		for uri, edits := range v.(map[string]any)["changes"].(map[string]any) {
			rel, _ := filepath.Rel(app, uriToPath(uri))
			src := string(p.sourceText(uriToPath(uri)))
			b, _ := json.Marshal(edits)
			var list []any
			json.Unmarshal(b, &list)
			out[filepath.ToSlash(rel)] = applyEdits(src, list)
		}
		return out
	}

	got := rename("pages/orgs/new.vuka.html", "pages/orgs/create.vuka.html")
	if !strings.Contains(got["pages/orgs/index.vuka.html"], "urls.Orgs.Create.Path()") {
		t.Errorf("file rename: %v", got)
	}

	got = rename("pages/orgs", "pages/teams")
	if s := got["pages/orgs/index.vuka.html"]; !strings.Contains(s, "urls.Teams.Org(1).Path()") || !strings.Contains(s, "urls.Teams.New.Path()") {
		t.Errorf("folder rename, orgs/index: %q", s)
	}
	if s := got["pages/links.vuka.html"]; !strings.Contains(s, "urls.Teams.Org(7).TeamSettings.Path()") {
		t.Errorf("folder rename, links: %q", s)
	}
	if s := got["pages/index.vuka.html"]; !strings.Contains(s, "urls.Teams.Path()") || !strings.Contains(s, "urls.Api.Orgs.Path()") {
		t.Errorf("folder rename, index (api/orgs is another folder): %q", s)
	}

	got = rename("pages/orgs/[org]/team-settings.vuka.html", "pages/orgs/team.vuka.html")
	if s := got["pages/links.vuka.html"]; !strings.Contains(s, "urls.Orgs.Team.Path()") {
		t.Errorf("a page moved up a folder: %q", s)
	}

	if got := rename("pages/about.vuka.html", "pages/about2.txt"); len(got) != 0 {
		t.Errorf("a rename out of the pages: %v", got)
	}
}

func TestPathMatches(t *testing.T) {
	for _, c := range []struct {
		pattern, lit string
		want         bool
	}{
		{"/orgs/{org}", "/orgs/7", true},
		{"/orgs/{org}", "/orgs/7/", true},
		{"/orgs/{org}", "/orgs", false},
		{"/files/{file...}", "/files/a/b", true},
		{"/", "/", true},
		{"/about", "/about?x=1#y", true},
		{"/about", "/abouts", false},
	} {
		if got := pathMatches(c.pattern, c.lit); got != c.want {
			t.Errorf("pathMatches(%q, %q) = %v", c.pattern, c.lit, got)
		}
	}
}

// typeLine is the line, from 1, the page type of the routes app's file
// rel is declared on.
func typeLine(t *testing.T, app, rel string) int {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(app, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(l, "type ") {
			return i + 1
		}
	}
	t.Fatalf("%s declares no type", rel)
	return 0
}
