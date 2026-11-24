package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteRenameTarget: renaming a route name moves the folder, or the
// page file, of the one part that changes.
func TestRouteRenameTarget(t *testing.T) {
	p, app := routesProxy(t)
	routes, _ := p.routes()
	byName := func(name string) *folderRoute {
		var r *folderRoute
		for _, x := range routes {
			if x.Name == name && (r == nil || rank(x) < rank(r)) {
				r = x
			}
		}
		if r == nil {
			t.Fatalf("no route %s", name)
		}
		return r
	}
	for _, c := range []struct{ name, to, from, want string }{
		{"orgs.org", "teams.org", "pages/orgs", "pages/teams"},
		{"orgs.org", "orgs.team", "pages/orgs/[org]", "pages/orgs/[team]"},
		{"orgs.new", "orgs.create", "pages/orgs/new.vuka.html", "pages/orgs/create.vuka.html"},
		{"orgs.permits", "orgs.licences", "pages/orgs/[org]/permits", "pages/orgs/[org]/licences"},
		{"api.pets.id", "api.pets.pet", "pages/api/pets/[id].vuka", "pages/api/pets/[pet].vuka"},
		{"files", "docs", "pages/files", "pages/docs"},
	} {
		from, to, err := renameTarget(byName(c.name), c.to)
		if err != nil || from != filepath.Join(app, c.from) || to != filepath.Join(app, c.want) {
			t.Errorf("rename %s to %s: %s → %s, %v; want %s → %s", c.name, c.to, relPath(app, from), relPath(app, to), err, c.from, c.want)
		}
	}
	for _, c := range []struct{ name, to, want string }{
		{"orgs.org", "orgs", "a rename changes one"},
		{"orgs.permits", "teams.licences", "more than one folder"},
		{"orgs.new", "orgs.new", "already"},
		{"orgs.new", "orgs.a/b", "can't be a folder's name"},
	} {
		if _, _, err := renameTarget(byName(c.name), c.to); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("rename %s to %s: %v, want %q", c.name, c.to, err, c.want)
		}
	}
}

// TestRouteRename: prepareRename and rename on a link's route name give
// vuka mv's plan as a workspace edit, its links' edits then the folder's
// rename, to an editor that renames files; another is told to run vuka mv.
func TestRouteRename(t *testing.T) {
	app := routesModule(t, moveLinks)
	_, res, err := routeTable(app, "example.com/app")
	if err != nil || res == nil {
		t.Fatalf("route table: %v", err)
	}
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.setRoutes(app, res, nil)
	path := filepath.Join(app, "pages", "links.vuka.html")
	src, _ := os.ReadFile(path)
	at := strings.Index(string(src), `"orgs.org"`) + 3
	rc, ok := routeCallAt(src, at, false)
	if !ok || !rc.inName(at) {
		t.Fatal("no link at the name")
	}
	defer editorRenamesFiles.Store(editorRenamesFiles.Load())

	editorRenamesFiles.Store(false)
	for _, m := range []string{"textDocument/prepareRename", "textDocument/rename"} {
		if _, err := p.routeRename(m, src, rc, "teams.org"); err == nil || !strings.Contains(err.Error(), "run vuka mv") {
			t.Errorf("%s without file renames: %v", m, err)
		}
	}

	editorRenamesFiles.Store(true)
	got, err := p.routeRename("textDocument/prepareRename", src, rc, "")
	if b, _ := json.Marshal(got); err != nil || !strings.Contains(string(b), `"placeholder":"orgs.org"`) || !strings.Contains(string(b), `"start":{"line":9,"character":16}`) {
		t.Errorf("prepareRename: %s, %v", b, err)
	}
	got, err = p.routeRename("textDocument/rename", src, rc, "teams.org")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	b, _ := json.Marshal(got)
	var we struct {
		DocumentChanges []struct {
			Kind         string `json:"kind"`
			OldURI       string `json:"oldUri"`
			NewURI       string `json:"newUri"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []struct {
				NewText string `json:"newText"`
			} `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(b, &we); err != nil || len(we.DocumentChanges) < 2 {
		t.Fatalf("rename's edit: %s", b)
	}
	last := we.DocumentChanges[len(we.DocumentChanges)-1]
	if last.Kind != "rename" || uriToPath(last.OldURI) != filepath.Join(app, "pages", "orgs") || uriToPath(last.NewURI) != filepath.Join(app, "pages", "teams") {
		t.Errorf("the rename isn't last, of pages/orgs to pages/teams: %s", b)
	}
	var links []string
	for _, c := range we.DocumentChanges {
		if c.Kind != "" {
			continue
		}
		if uriToPath(c.TextDocument.URI) == path {
			for _, e := range c.Edits {
				links = append(links, e.NewText)
			}
		}
	}
	if j := strings.Join(links, " "); !strings.Contains(j, `"teams.org"`) || !strings.Contains(j, `"teams"`) || !strings.Contains(j, "Teams.Org(ID)") {
		t.Errorf("the links' edits: %q", links)
	}
	if fileExists(filepath.Join(app, "pages", "teams")) {
		t.Error("the rename moved the folder itself")
	}

	// Unsaved changes in a file to edit: no edit worked out on the disk's text.
	p.bufs[path] = append(src, '\n')
	if _, err := p.routeRename("textDocument/rename", src, rc, "teams.org"); err == nil || !strings.Contains(err.Error(), "not saved") {
		t.Errorf("rename over unsaved changes: %v", err)
	}
}

// TestEditorRenamesFiles: initialize tells whether the editor applies a
// workspace edit's file renames.
func TestEditorRenamesFiles(t *testing.T) {
	defer editorRenamesFiles.Store(editorRenamesFiles.Load())
	p := &proxy{}
	for caps, want := range map[string]bool{
		`{"workspace":{"workspaceEdit":{"documentChanges":true,"resourceOperations":["create","rename","delete"]}}}`: true,
		`{"workspace":{"workspaceEdit":{"documentChanges":true,"resourceOperations":["create"]}}}`:                   false,
		`{"workspace":{"workspaceEdit":{"resourceOperations":["rename"]}}}`:                                          false,
		`{}`: false,
	} {
		p.refactorRequest(&rpcMsg{ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"capabilities":` + caps + `}`)}, "")
		if editorRenamesFiles.Load() != want {
			t.Errorf("%s: renames files %v, want %v", caps, !want, want)
		}
	}
}

// TestWillRenameRouteNames: a folder the editor's explorer moves takes vuka
// mv's edits, so the links by name follow it as the urls chains do.
func TestWillRenameRouteNames(t *testing.T) {
	app := routesModule(t, moveLinks)
	_, res, err := routeTable(app, "example.com/app")
	if err != nil || res == nil {
		t.Fatalf("route table: %v", err)
	}
	p := &proxy{bufs: map[string][]byte{}, shown: map[string]string{}, virtual: map[string]*vfile{}, bySource: map[string]string{}}
	p.setRoutes(app, res, nil)
	raw, _ := json.Marshal(map[string]any{"files": []any{map[string]any{
		"oldUri": pathToURI(filepath.Join(app, "pages", "orgs")), "newUri": pathToURI(filepath.Join(app, "pages", "teams"))}}})
	v, _ := p.renameEdits(raw).(map[string]any)
	path := filepath.Join(app, "pages", "links.vuka.html")
	edits, _ := v["changes"].(map[string]any)[pathToURI(path)].([]any)
	got := applyEdits(string(p.sourceText(path)), edits)
	for _, want := range []string{`route("teams.org", ID)`, `route("teams")`, `routePath("teams.new")`, "urls.Teams.Org(ID).Path()", `route("about")`} {
		if !strings.Contains(got, want) {
			t.Errorf("links after the explorer's move lack %s:\n%s", want, got)
		}
	}
}
