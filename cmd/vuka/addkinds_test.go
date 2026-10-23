package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAdd runs vuka add kind args in the app at dir.
func runAdd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	var out bytes.Buffer
	err := add(args, &out)
	return out.String(), err
}

func mustAdd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runAdd(t, dir, args...)
	if err != nil {
		t.Fatalf("vuka add %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/shop\n\ngo 1.25\n"), 0o644)
	}
}

func has(t *testing.T, path string, wants ...string) {
	t.Helper()
	got := read(t, path)
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", filepath.Base(path), w, got)
		}
	}
}

func TestAddService(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	out := mustAdd(t, dir, "service", "users/Mailer")
	if !strings.Contains(out, "wrote modules/users/mailer.vuka\nwrote modules/users/mailer_test.vuka\n") {
		t.Errorf("output:\n%s", out)
	}
	has(t, filepath.Join(dir, "modules/users/mailer.vuka"), "package users", "\ntype Mailer struct{}", "func NewMailer() *Mailer")
	has(t, filepath.Join(dir, "modules/users/mailer_test.vuka"), "func TestMailer(t *testing.T)", "NewMailer()")
	has(t, filepath.Join(dir, "main.vuka"), "\tapp.Service(users.NewUsers)\n\tapp.Service(users.NewMailer)\n}")

	// no overwrite; --force; clashes and bad names
	if _, err := runAdd(t, dir, "service", "users/Mailer"); err == nil || !strings.Contains(err.Error(), "declared already") {
		t.Errorf("again: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "modules/users/mailer.vuka"), []byte("package users\n"), 0o644)
	if _, err := runAdd(t, dir, "service", "users/Mailer"); err == nil || !strings.Contains(err.Error(), "exists already (--force overwrites it)") {
		t.Errorf("a file in the way: %v", err)
	}
	mustAdd(t, dir, "service", "users/Mailer", "--force")
	has(t, filepath.Join(dir, "modules/users/mailer.vuka"), "type Mailer struct{}")
	mustAdd(t, dir, "service", "Notifier", "--module", "users")
	has(t, filepath.Join(dir, "modules/users/notifier.vuka"), "package users", "type Notifier struct{}")
	out = mustAdd(t, dir, "service", "Clock")
	has(t, filepath.Join(dir, "services/clock.vuka"), "package services", "type Clock struct{}")
	has(t, filepath.Join(dir, "main.vuka"), "\t\"example.com/shop/services\"", "app.Service(services.NewClock)")
	if _, err := runAdd(t, dir, "service", "users/Pager", "--module", "posts"); err == nil || !strings.Contains(err.Error(), "--module posts names another module") {
		t.Errorf("two modules: %v", err)
	}
	for arg, msg := range map[string]string{
		"users/mailer":  "upper-case: Invoice, UserCard (Mailer?)",
		"/Mailer":       "<Name> [--module m], or <module>/<Name>",
		"userz/Mailer":  "no module userz (did you mean users?)",
		"users/Mail-er": "(MailEr?)",
		"users/Users":   "declared already in modules/users/service.vuka",
	} {
		if _, err := runAdd(t, dir, "service", arg); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", arg, err, msg)
		}
	}
}

func TestAddDryRun(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	out := mustAdd(t, dir, "component", "Panel", "--dry-run")
	if !strings.HasPrefix(out, "would write components/panel.vuka.html\n") {
		t.Errorf("output:\n%s", out)
	}
	if exists(dir, "components", "panel.vuka.html") {
		t.Error("--dry-run wrote")
	}
	out = mustAdd(t, dir, "page", "/reports", "--dry-run")
	if !strings.Contains(out, "would write pages/reports.vuka.html\n") {
		t.Errorf("page output:\n%s", out)
	}
	if exists(dir, "pages", "reports.vuka.html") {
		t.Error("--dry-run wrote the page")
	}
	if _, err := runAdd(t, dir, "component", "Panel", "--dryrun"); err == nil || !strings.Contains(err.Error(), "did you mean --dry-run?") {
		t.Errorf("a misspelt flag: %v", err)
	}
	if _, err := runAdd(t, dir, "compnent", "Panel"); err == nil || !strings.Contains(err.Error(), "did you mean component?") {
		t.Errorf("a misspelt kind: %v", err)
	}
}

func TestAddComponent(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	mustAdd(t, dir, "component", "StatCard")
	has(t, filepath.Join(dir, "components/stat_card.vuka.html"), "package components", "type StatCard struct", "Children ui.Node", "<section class=", "{Title}", "{Children}")
	if _, err := runAdd(t, dir, "component", "Card"); err == nil || !strings.Contains(err.Error(), "Card is declared already in components/card.vuka.html") {
		t.Errorf("a clash: %v", err)
	}
	mustAdd(t, dir, "component", "Avatar", "--module", "users")
	has(t, filepath.Join(dir, "modules/users/avatar.vuka.html"), "package users", "type Avatar struct", "{Children}")
}

func TestAddPage(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	out := mustAdd(t, dir, "page", "/reports/{id}")
	has(t, filepath.Join(dir, "pages/reports/[id].vuka.html"), "package reports", "type ReportsPage struct {\n\tID string\n}", `func (p *ReportsPage) Title() string { return "Reports" }`,
		"<h1 class=\"text-2xl font-semibold tracking-tight\">{Title()}</h1>")
	if strings.Contains(read(t, filepath.Join(dir, "pages/reports/[id].vuka.html")), "<html") {
		t.Error("a page under pages/_layout.vuka.html writes its own document")
	}
	if !strings.Contains(out, "open http://localhost:8080/reports/1") {
		t.Errorf("output:\n%s", out)
	}
	mustAdd(t, dir, "page", "/charts")
	has(t, filepath.Join(dir, "pages/charts.vuka.html"), "package pages", "type ChartsPage struct {\n\tui.Live", "func (p *ChartsPage) Inc()", "onClick={Inc}", "Clicked {count} times")
	mustAdd(t, dir, "page", "/about", "--stateless")
	if body := read(t, filepath.Join(dir, "pages/about.vuka.html")); strings.Contains(body, "ui.Live") || !strings.Contains(body, "type AboutPage struct {\n}") {
		t.Errorf("a stateless page:\n%s", body)
	}
	mustAdd(t, dir, "page", "/users/stats", "--live")
	has(t, filepath.Join(dir, "pages/users/stats.vuka.html"), "package users", "type UsersStatsPage struct {\n\tui.Live")
	mustAdd(t, dir, "page", "/orgs/{org}/edit", "--live")
	has(t, filepath.Join(dir, "pages/orgs/[org]/edit.vuka.html"), "package org", "type OrgsEditPage struct {\n\tui.Live\n\tOrg   string")
	// A (group) is a folder of the page's, no part of its URL.
	if out := mustAdd(t, dir, "page", "/admin/(console)/members", "--stateless"); !strings.Contains(out, "open http://localhost:8080/admin/members") {
		t.Errorf("a page in a route group: output:\n%s", out)
	}
	has(t, filepath.Join(dir, "pages/admin/(console)/members.vuka.html"), "package console", "type AdminMembersPage struct")
	mustAdd(t, dir, "page", "/(site)", "--stateless", "--name", "Home")
	has(t, filepath.Join(dir, "pages/(site)/index.vuka.html"), "package site", "type Home struct")
	for args, msg := range map[string]string{
		"reports":               "starts with /",
		"/Reports":              "isn't a path segment",
		"/charts --name Charts": "pages/charts.vuka.html exists already",
		"/login":                "pages/login/index.vuka.html exists already",
		"/y --live --stateless": "not both",
		"/x --vue":              "go with --island",
		"/x --name lower":       "upper-case",
		"/x --module users":     "--module goes with --island",
		"/x --layout none":      "no flag --layout",
	} {
		if _, err := runAdd(t, dir, append([]string{"page"}, strings.Fields(args)...)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", args, err, msg)
		}
	}

	// An app without pages/_layout.vuka.html: the page is a document.
	os.Remove(filepath.Join(dir, "pages/_layout.vuka.html"))
	mustAdd(t, dir, "page", "/plain", "--stateless")
	has(t, filepath.Join(dir, "pages/plain.vuka.html"), "<html lang=\"en\">", "<title>{Title()}</title>")
}

func islandsApp(t *testing.T) string {
	dir := scaffolded(t, map[string]string{"islands": "yes", "frontend": "none", "auth": "no"})
	writeGoMod(t, dir)
	return dir
}

func TestAddIsland(t *testing.T) {
	dir := islandsApp(t)
	os.Remove(filepath.Join(dir, "web/tsconfig.json"))
	out := mustAdd(t, dir, "island", "Chart", "--no-install")
	has(t, filepath.Join(dir, "web/islands/Chart.vue"), "defineProps<{ title: string; count?: number }>()")
	has(t, filepath.Join(dir, "web/tsconfig.json"), `"vuka:live": ["./vuka-live"]`)
	has(t, filepath.Join(dir, "components/chart.vuka.html"), "package components", "type Chart struct", `import ChartIsland "../web/islands/Chart.vue"`, "<ChartIsland title={Title} count={Count} client:load />")
	if !strings.Contains(out, "<components.Chart title=\"Hello\" count={3} />") || !strings.Contains(out, "Place it in components/ as") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(out, "package.json") {
		t.Errorf("Vue is there already, yet package.json changed:\n%s", out)
	}

	out = mustAdd(t, dir, "island", "Clock", "--react", "--no-install")
	has(t, filepath.Join(dir, "web/islands/Clock.tsx"), "interface Props {", "export default function Clock({ title, count = 0 }: Props)")
	has(t, filepath.Join(dir, "web/package.json"), `"react": "^19.3.0"`, `"@vitejs/plugin-react"`, `"vue": "^3.5.44"`)
	has(t, filepath.Join(dir, "web/vite.config.mts"), "import react from '@vitejs/plugin-react'\n", "plugins: [react(), vue(),")
	if !strings.Contains(out, "changed web/package.json") || !strings.Contains(out, "Install the new packages: (cd web && npm install)") {
		t.Errorf("output:\n%s", out)
	}
	// a second React island changes nothing more
	out = mustAdd(t, dir, "island", "Gauge", "--react", "--no-install")
	if strings.Contains(out, "changed") {
		t.Errorf("a second React island:\n%s", out)
	}
	mustAdd(t, dir, "island", "Tag", "--svelte", "--no-install", "--dir", "web/islands/small")
	has(t, filepath.Join(dir, "web/islands/small/Tag.svelte"), "let { title, count = 0 }: Props = $props()")
	has(t, filepath.Join(dir, "web/vite.config.mts"), "import { svelte } from '@sveltejs/vite-plugin-svelte'")

	os.WriteFile(filepath.Join(dir, "web/islands/Pie.vue"), []byte("<template/>"), 0o644)
	mustAdd(t, dir, "module", "notes")
	mustAdd(t, dir, "island", "Board", "--module", "notes", "--no-install")
	has(t, filepath.Join(dir, "web/islands/notes/Board.vue"), "defineProps")
	has(t, filepath.Join(dir, "modules/notes/board.vuka.html"), "package notes", "type Board struct", `import BoardIsland "../../web/islands/notes/Board.vue"`)

	for args, msg := range map[string]string{
		"Chart":             "Chart is declared already in components/chart.vuka.html",
		"Pye --module nots": "did you mean",
		"Pie --react":       "an island Pie",
		"chart":             "upper-case",
		"Pye --vue --react": "one framework",
		"Pye --dir islands": "outside the island frontend web/",
		"Pye --no-instal":   "did you mean --no-install?",
	} {
		if _, err := runAdd(t, dir, append([]string{"island"}, strings.Fields(args)...)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", args, err, msg)
		}
	}

	plain := scaffolded(t, map[string]string{})
	writeGoMod(t, plain)
	if _, err := runAdd(t, plain, "island", "Chart"); err == nil || !strings.Contains(err.Error(), "no island frontend") {
		t.Errorf("an app without one: %v", err)
	}
}

// installs with a fake npm, showing it is under way
func TestAddIslandInstalls(t *testing.T) {
	dir := islandsApp(t)
	fakeNPM(t, false)
	out := mustAdd(t, dir, "island", "Clock", "--react")
	if !strings.Contains(out, "installing npm packages in web/ …\n✓ npm packages installed in web/") {
		t.Errorf("output:\n%s", out)
	}
}

func TestAddIslandPage(t *testing.T) {
	dir := islandsApp(t)
	mustAdd(t, dir, "page", "/reports", "--island", "Reports", "--live", "--no-install")
	has(t, filepath.Join(dir, "pages/reports.vuka"), "@ui.Island\ntype Reports struct {\n\tui.Live", "Title string   `json:\"title\"`", "`json:\"q\"` @ui.URL(\"q\") @ui.Bind",
		"@ui.OnChange\nfunc (p *Reports) changed(ctx context.Context, c ui.Changed) error")
	if body := read(t, filepath.Join(dir, "pages/reports.vuka")); strings.Contains(body, "@web.") || strings.Contains(body, "layouts") {
		t.Errorf("a page under pages/ takes no route decorator, and its folder's layout:\n%s", body)
	}
	has(t, filepath.Join(dir, "web/islands/Reports.vue"), "import { useLive } from 'vuka:live'", "const live = useLive<Reports>()", `<input v-model="live.q.value" placeholder="Search"`)
	if body := read(t, filepath.Join(dir, "web/islands/Reports.vue")); strings.Contains(body, "defineProps") {
		t.Errorf("the component reads its props through live:\n%s", body)
	}

	mustAdd(t, dir, "page", "/board", "--island", "Board", "--react", "--stateless", "--no-install")
	page := read(t, filepath.Join(dir, "pages/board.vuka"))
	if strings.Contains(page, "\tui.Live\n") || strings.Contains(page, "@ui.Mount") || !strings.Contains(page, "@ui.Island\ntype Board struct") || !strings.Contains(page, "func NewBoard() *Board") {
		t.Errorf("a stateless page:\n%s", page)
	}
	has(t, filepath.Join(dir, "web/islands/Board.tsx"), "export default function Board()", "useLive<Page>()", "onChange={(e) => live.q.set(e.target.value)}")

	mustAdd(t, dir, "page", "/tags", "--island", "Tags", "--svelte", "--no-install")
	has(t, filepath.Join(dir, "web/islands/Tags.svelte"), "const l = live<Tags>()", "bind:value={l.q.value}")

	// --module puts the component under the module's islands, as vuka add
	// island does, and names it; the page is its path under pages/.
	mustAdd(t, dir, "module", "orders")
	mustAdd(t, dir, "page", "/orders/find", "--island", "FindOrders", "--module", "orders", "--no-install")
	has(t, filepath.Join(dir, "pages/orders/find.vuka"), "package orders", "@ui.Island(\"islands/orders/FindOrders.vue\")\ntype FindOrders struct")
	has(t, filepath.Join(dir, "web/islands/orders/FindOrders.vue"), "useLive<FindOrders>()")

	// A component elsewhere (--dir) is named.
	mustAdd(t, dir, "page", "/gauge", "--island", "Gauge", "--dir", "web/widgets", "--no-install")
	has(t, filepath.Join(dir, "pages/gauge.vuka"), `@ui.Island("widgets/Gauge.vue")`)
}

func TestAddEntityFields(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	mustAdd(t, dir, "module", "posts")
	out, err := runAdd(t, dir, "entity", "posts/Comment", "--dry-run", "body:text", "post:ref", "score:int:index", "email:string:unique",
		"approved_at:time?", "price:decimal", "active:bool", "meta:json", "user:ref?", "--table", "post_comments")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would write modules/posts/comment.vuka") || !strings.Contains(out, "would write migrations/NNNN_post_comments.go") {
		t.Errorf("output:\n%s", out)
	}
	out = mustAdd(t, dir, "entity", "Tag", "--module", "posts", "--dry-run", "name:string")
	if !strings.Contains(out, "would write modules/posts/tag.vuka") {
		t.Errorf("--module:\n%s", out)
	}
	out = mustAdd(t, dir, "entity", "Country", "--dry-run", "name:string")
	if !strings.Contains(out, "would write entities/country.vuka") {
		t.Errorf("the app's own:\n%s", out)
	}
	out = mustAdd(t, dir, "entity", "Region", "--unmanaged", "--table", "regions", "name:string")
	has(t, filepath.Join(dir, "entities/region.vuka"), `@orm.Meta{Table: "regions", Unmanaged: true}`)
	if body := read(t, filepath.Join(dir, "entities/region.vuka")); strings.Contains(body, "CreatedAt") || exists(dir, "entities", "region_test.vuka") || !strings.Contains(out, "no migration") {
		t.Errorf("--unmanaged:\n%s\n%s", body, out)
	}
	for args, msg := range map[string]string{
		"posts/Comment body:txt":        `no type "txt" (did you mean text?)`,
		"posts/Comment body":            "a field is name:type",
		"posts/Comment Body:text":       "snake_case: body",
		"posts/Comment id:int":          "has ID already",
		"posts/Comment body:text:uniq":  `"uniq" isn't a field option`,
		"posts/Comment owner:ref":       "no model Owner in the app",
		"posts/Comment a:text a:string": "two fields are A",
		"posts/Post":                    "Post is declared already",
		"nobody/Comment":                "no module nobody",
		"posts/Comment --table Bad":     "snake_case",
	} {
		if _, err := runAdd(t, dir, append([]string{"entity"}, strings.Fields(args)...)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", args, err, msg)
		}
	}
}
