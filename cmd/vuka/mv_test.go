package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// moveLinks links to the routes the moves below rename, by name and by
// urls chain, in markup, a page's Go and a hand-written Go file.
var moveLinks = map[string]string{
	"pages/links.vuka.html": "<script lang=\"go\">\npackage pages\n\nimport \"example.com/app/pages/urls\"\n\n// Links links.\ntype Links struct{ ID int }\n</script>\n\n" +
		"<a href={route(\"orgs.org\", ID)}>org</a> <a href={route(\"orgs\")}>orgs</a> <a href={routePath(\"orgs.new\")}>new</a>\n" +
		"<a href={urls.Orgs.Org(ID).Path()}>chain</a> <a href={urls.Orgs.New.Path()}>new</a> <a href={route(\"about\")}>about</a>\n",
	"handlers.go": "package main\n\nimport (\n\t\"context\"\n\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/app/pages/urls\"\n)\n\n" +
		"func next(ctx context.Context, id int) string {\n\treturn web.Route(ctx, \"orgs.org\", id) + urls.Orgs.Org(id).URL(ctx) + web.RoutePath(ctx, \"api.pets.id\", id)\n}\n",
	"forms.go": "package main\n\nimport \"example.com/app/pages/orgs\"\n\nvar _ orgs.Form\n",
}

// planIn works out a move in app, from and to relative to it.
func planIn(t *testing.T, app, from, to string) (*movePlan, error) {
	t.Helper()
	return planMove(app, "example.com/app", filepath.Join(app, from), filepath.Join(app, to))
}

// planRoutes are the plan's route changes, "old /path → new /path".
func planRoutes(p *movePlan) []string {
	var out []string
	for _, r := range p.routes {
		out = append(out, r.oldName+" "+r.oldPath+" → "+r.newName+" "+r.newPath)
	}
	slices.Sort(out)
	return out
}

// planText is what the plan prints.
func planText(p *movePlan) string {
	var b bytes.Buffer
	p.print(&b, true)
	return b.String()
}

func wantAll(t *testing.T, what, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, got)
		}
	}
}

// TestMovePlan: the route names before and after a move, worked out from
// the route tables, and the edits of the links naming them.
func TestMovePlan(t *testing.T) {
	t.Run("folder", func(t *testing.T) {
		t.Parallel()
		app := routesModuleAt(t, moveLinks)
		p, err := planIn(t, app, "pages/orgs", "pages/teams")
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(planRoutes(p), "\n")
		wantAll(t, "routes", got, "orgs /orgs → teams /teams", "orgs.org /orgs/{org} → teams.org /teams/{org}", "orgs.new /orgs/new → teams.new /teams/new",
			"orgs.permits /orgs/{org}/permits/{permit} → teams.permits /teams/{org}/permits/{permit}")
		wantAll(t, "plan", planText(p),
			`pages/links.vuka.html:10: "orgs.org" → "teams.org"`, `pages/links.vuka.html:10: "orgs" → "teams"`, `pages/links.vuka.html:10: "orgs.new" → "teams.new"`,
			"pages/links.vuka.html:11: Orgs.Org(ID) → Teams.Org(ID)", "pages/links.vuka.html:11: Orgs.New → Teams.New",
			`handlers.go:12: "orgs.org" → "teams.org"`, "handlers.go:12: Orgs.Org(id) → Teams.Org(id)",
			"forms.go:3: example.com/app/pages/orgs → example.com/app/pages/teams",
			"pages/orgs/index.vuka.html:16: Orgs.Org(1) → Teams.Org(1)", "pages/orgs/index.vuka.html:19: Orgs.New → Teams.New")
		if strings.Contains(planText(p), `"about"`) || strings.Contains(planText(p), "api.pets") {
			t.Errorf("the plan rewrites a link the move leaves:\n%s", planText(p))
		}
		if len(p.wraps) != 0 {
			t.Errorf("wraps change: %q", p.wraps)
		}
	})
	t.Run("parameter folder", func(t *testing.T) {
		t.Parallel()
		app := routesModuleAt(t, moveLinks)
		p, err := planIn(t, app, "pages/orgs/[org]", "pages/teams/[org]")
		if err != nil {
			t.Fatal(err)
		}
		// /teams/{org} alone under teams: its base name.
		wantAll(t, "routes", strings.Join(planRoutes(p), "\n"), "orgs.org /orgs/{org} → teams /teams/{org}", "orgs.permits /orgs/{org}/permits/{permit} → teams.permits /teams/{org}/permits/{permit}")
		wantAll(t, "plan", planText(p), `pages/links.vuka.html:10: "orgs.org" → "teams"`, "pages/links.vuka.html:11: Orgs.Org(ID) → Teams.Org(ID)",
			"pages/orgs/[org]/permits/[permit].vuka.html:19: Orgs.Org(Org).Permits.Permit(Permit) → Teams.Org(Org).Permits.Permit(Permit)")
		if strings.Contains(planText(p), `"orgs" →`) || strings.Contains(planText(p), `"orgs.new" →`) {
			t.Errorf("the plan renames /orgs or /orgs/new:\n%s", planText(p))
		}
	})
	t.Run("one name freed, one taken", func(t *testing.T) {
		t.Parallel()
		// /orgs moves to /teams: /orgs/{org} is alone in orgs, and takes
		// the name orgs.
		app := routesModuleAt(t, moveLinks)
		p, err := planIn(t, app, "pages/orgs/index.vuka.html", "pages/teams/index.vuka.html")
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(planRoutes(p), "\n")
		wantAll(t, "routes", got, "orgs /orgs → teams /teams", "orgs.org /orgs/{org} → orgs /orgs/{org}")
		wantAll(t, "plan", planText(p), `pages/links.vuka.html:10: "orgs.org" → "orgs"`, `pages/links.vuka.html:10: "orgs" → "teams"`)
	})
	t.Run("file into another package", func(t *testing.T) {
		t.Parallel()
		app := routesModuleAt(t, moveLinks)
		p, err := planIn(t, app, "pages/about.vuka.html", "pages/orgs/about.vuka.html")
		if err != nil {
			t.Fatal(err)
		}
		wantAll(t, "routes", strings.Join(planRoutes(p), "\n"), "about /about → orgs.about /orgs/about")
		wantAll(t, "plan", planText(p), "pages/about.vuka.html:2: pages → orgs", `pages/links.vuka.html:11: "about" → "orgs.about"`)
	})
	t.Run("endpoints", func(t *testing.T) {
		t.Parallel()
		app := routesModuleAt(t, moveLinks)
		p, err := planIn(t, app, "pages/api/pets", "pages/api/animals")
		if err != nil {
			t.Fatal(err)
		}
		wantAll(t, "routes", strings.Join(planRoutes(p), "\n"), "api.pets /api/pets → api.animals /api/animals", "api.pets.id /api/pets/{id} → api.animals.id /api/animals/{id}")
		wantAll(t, "plan", planText(p), `handlers.go:12: "api.pets.id" → "api.animals.id"`)
	})
	t.Run("into a group", func(t *testing.T) {
		t.Parallel()
		app := groupsModule(t, nil)
		p, err := planIn(t, app, "pages/admin/auth", "pages/admin/(console)/auth")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.routes) != 0 {
			t.Errorf("a group renames routes: %q", planRoutes(p))
		}
		wantAll(t, "plan", planText(p), "GET /admin/auth/login: guards none → console.Guard; layouts pages.Layout → pages.Layout, console.Shell")
	})
}

// TestMoveRefused: a move that would lose data, leave pages/ or break the
// routes writes nothing.
func TestMoveRefused(t *testing.T) {
	t.Parallel()
	app := routesModuleAt(t, moveLinks)
	before := treeSum(t, app)
	for _, c := range []struct{ from, to, want string }{
		{"pages/orgs", "pages/about.vuka.html", "exists already"},
		{"pages/orgs", "teams", "below a pages/ folder"},
		{"pages/nothing", "pages/x", "no such file"},
		{"pages/orgs", "pages/orgs/x", "into itself"},
		{"pages/about.vuka.html", "pages/about.vuka", "keeps its kind"},
		// /orgs twice
		{"pages/about.vuka.html", "pages/(g)/orgs.vuka.html", "the moved tree's routes fail their checks"},
		// the page's chain gives the parameter org, which its route no longer takes
		{"pages/orgs/[org]/permits", "pages/permits", "can't follow the move"},
	} {
		var out, errOut bytes.Buffer
		err := vukaIn(app, &out, &errOut, "mv", c.from, c.to)
		if err == nil || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("vuka mv %s %s: %v, want %q:\n%s%s", c.from, c.to, err, c.want, out.Bytes(), errOut.Bytes())
		}
	}
	if treeSum(t, app) != before {
		t.Error("a refused move wrote to the module")
	}
	// git has a file at the target, deleted from the disk but not from the index.
	gitIn(t, app, "init", "-q")
	writeFile(t, filepath.Join(app, "pages", "teams", "x.vuka.html"), "package teams\n")
	gitIn(t, app, "add", ".")
	os.RemoveAll(filepath.Join(app, "pages", "teams"))
	var errOut bytes.Buffer
	if err := vukaIn(app, io.Discard, &errOut, "mv", "pages/orgs", "pages/teams"); err == nil || !strings.Contains(errOut.String(), "git has files at pages/teams") {
		t.Errorf("vuka mv onto files in git's index: %v\n%s", err, errOut.Bytes())
	}
	if !fileExists(filepath.Join(app, "pages", "orgs")) {
		t.Error("the refused move moved")
	}
}

// TestMoveCLI: vuka mv -n writes nothing; vuka mv moves, the module builds
// with every link resolving, vuka routes names the routes as the plan
// said, and moving back gives the tree as it was, byte for byte; in a git
// work tree, by git mv.
func TestMoveCLI(t *testing.T) {
	for _, git := range []bool{false, true} {
		t.Run(map[bool]string{false: "files", true: "git"}[git], func(t *testing.T) {
			t.Parallel()
			app := routesModuleAt(t, moveLinks)
			if git {
				gitIn(t, app, "init", "-q")
				gitIn(t, app, "add", ".")
				gitIn(t, app, "-c", "user.email=a@example.com", "-c", "user.name=a", "commit", "-qm", "x")
			}
			before := treeSum(t, app)
			var out, errOut bytes.Buffer
			if err := vukaIn(app, &out, &errOut, "mv", "-n", "pages/orgs", "pages/teams"); err != nil {
				t.Fatalf("vuka mv -n: %v\n%s", err, errOut.Bytes())
			}
			wantAll(t, "vuka mv -n", out.String(), "would move pages/orgs → pages/teams", `pages/links.vuka.html:10: "orgs.org" → "teams.org"`)
			if treeSum(t, app) != before {
				t.Fatalf("vuka mv -n wrote to the module:\n%s", strings.Join(moduleFiles(t, app), "\n"))
			}
			out.Reset()
			if err := vukaIn(app, &out, &errOut, "mv", "pages/orgs", "pages/teams"); err != nil {
				t.Fatalf("vuka mv: %v\n%s", err, errOut.Bytes())
			}
			how := map[bool]string{false: "(move)", true: "(git mv)"}[git]
			wantAll(t, "vuka mv", out.String(), "moving pages/orgs → pages/teams "+how)
			if fileExists(filepath.Join(app, "pages", "orgs")) || !fileExists(filepath.Join(app, "pages", "teams", "[org]", "index.vuka.html")) {
				t.Fatal("the folder didn't move")
			}
			links, _ := os.ReadFile(filepath.Join(app, "pages", "links.vuka.html"))
			wantAll(t, "links", string(links), `route("teams.org", ID)`, `route("teams")`, `routePath("teams.new")`, "urls.Teams.Org(ID).Path()", `route("about")`)
			var js bytes.Buffer
			errOut.Reset()
			if err := vukaIn(app, &js, &errOut, "routes", "-json"); err != nil {
				t.Fatalf("vuka routes after the move: %v\n%s", err, errOut.Bytes())
			}
			var routes []struct{ Path, Name string }
			if err := json.Unmarshal(js.Bytes(), &routes); err != nil {
				t.Fatal(err)
			}
			names := map[string]string{}
			for _, r := range routes {
				names[r.Path] = r.Name
			}
			for path, name := range map[string]string{"/teams": "teams", "/teams/{org}": "teams.org", "/teams/new": "teams.new", "/about": "about", "/api/pets/{id}": "api.pets.id"} {
				if names[path] != name {
					t.Errorf("vuka routes: %s is named %q, want %q", path, names[path], name)
				}
			}
			errOut.Reset()
			if err := vukaIn(app, io.Discard, &errOut, "vet", "./..."); err != nil {
				t.Errorf("vuka vet after the move: %v\n%s", err, errOut.Bytes())
			}
			if git {
				st := gitIn(t, app, "status", "--short")
				if !strings.Contains(st, "R  pages/orgs/new.vuka.html -> pages/teams/new.vuka.html") {
					t.Errorf("git status after vuka mv:\n%s", st)
				}
			}
			removeGenerated(t, app)
			out.Reset()
			if err := vukaIn(app, &out, &errOut, "mv", "pages/teams", "pages/orgs"); err != nil {
				t.Fatalf("vuka mv back: %v\n%s", err, errOut.Bytes())
			}
			removeGenerated(t, app)
			if treeSum(t, app) != before {
				t.Errorf("moving back doesn't give the tree as it was:\n%s", strings.Join(moduleFiles(t, app), "\n"))
			}
		})
	}
}

// removeGenerated removes what a build writes beside the sources.
func removeGenerated(t *testing.T, app string) {
	t.Helper()
	filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "route.go" || d.Name() == "urls_gen.go" || d.Name() == "vuka_route_gen.go") {
			os.Remove(p)
		}
		return nil
	})
	os.Remove(filepath.Join(app, "pages", "urls"))
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// treeSum is a digest of the module's files, the version control's aside.
func treeSum(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	for _, f := range moduleFiles(t, root) {
		b, _ := os.ReadFile(filepath.Join(root, f))
		fmt.Fprintf(h, "%s %x\n", f, sha256.Sum256(b))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func moduleFiles(t *testing.T, root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// TestMoveApplyFails: a move whose writing fails halfway puts back what it
// had done.
func TestMoveApplyFails(t *testing.T) {
	t.Parallel()
	app := routesModuleAt(t, moveLinks)
	before := treeSum(t, app)
	p, err := planIn(t, app, "pages/orgs", "pages/teams")
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(app, "pages", "links.vuka.html")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o644)
	if err := p.apply(); err == nil || !strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("apply with a file it can't write: %v", err)
	}
	if treeSum(t, app) != before || fileExists(filepath.Join(app, "pages", "teams")) {
		t.Errorf("a failed move left the module changed:\n%s", strings.Join(moduleFiles(t, app), "\n"))
	}
}
