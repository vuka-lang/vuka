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
	has(t, filepath.Join(dir, "modules/users/mailer.vuka"), "package users", "@web.Service\ntype Mailer struct{}", "func Mailer.New() *Mailer")
	has(t, filepath.Join(dir, "modules/users/mailer_test.vuka"), "func TestMailer(t *testing.T)", "Mailer.New()")

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
	has(t, filepath.Join(dir, "main.vuka"), `/services"`)
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
	if !strings.HasPrefix(out, "would write components/panel.vuka\n") {
		t.Errorf("output:\n%s", out)
	}
	if exists(dir, "components", "panel.vuka") {
		t.Error("--dry-run wrote")
	}
	out = mustAdd(t, dir, "page", "/reports", "--dry-run")
	if !strings.Contains(out, "would write pages/reports_page.vuka\n") {
		t.Errorf("page output:\n%s", out)
	}
	if exists(dir, "pages", "reports_page.vuka") {
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
	has(t, filepath.Join(dir, "components/stat_card.vuka"), "package components", "type StatCardProps struct", "func StatCard(p StatCardProps) ui.Node")
	if _, err := runAdd(t, dir, "component", "Card"); err == nil || !strings.Contains(err.Error(), "Card is declared already in components/card.vuka") {
		t.Errorf("a clash: %v", err)
	}
	mustAdd(t, dir, "component", "Avatar", "--module", "users")
	has(t, filepath.Join(dir, "modules/users/avatar.vuka"), "package users", "func Avatar(p AvatarProps) ui.Node")
}

func TestAddPage(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	writeGoMod(t, dir)
	out := mustAdd(t, dir, "page", "/reports/{id}")
	has(t, filepath.Join(dir, "pages/reports_page.vuka"), `@web.Get("/reports/{id}")`, "func ShowReports(ctx context.Context, id string) ui.Node", "ID    string", `layouts.Page(p.Title)(`)
	if !strings.Contains(out, "open http://localhost:8080/reports/1") {
		t.Errorf("output:\n%s", out)
	}
	mustAdd(t, dir, "page", "/charts")
	has(t, filepath.Join(dir, "pages/charts_page.vuka"), `@web.Live("/charts"`, "type ChartsPage struct {\n\tui.Live")
	mustAdd(t, dir, "page", "/about", "--stateless")
	has(t, filepath.Join(dir, "pages/about_page.vuka"), `@web.Get("/about")`)
	mustAdd(t, dir, "page", "/stats", "--live", "--module", "users")
	has(t, filepath.Join(dir, "modules/users/stats_page.vuka"), `@web.Live("/stats", web.Layout(layouts.Page("Stats")))`, "type StatsPage struct {\n\tui.Live", "func (p *StatsPage) Inc()")
	for args, msg := range map[string]string{
		"reports":               "starts with /",
		"/Reports":              "isn't a path segment",
		"/stats":                "StatsPage is declared already",
		"/login":                "is a route already, in pages/login.vuka",
		"/x/{id} --live":        "{parameters}",
		"/y --live --stateless": "not both",
		"/x --vue":              "go with --island",
		"/x --name lower":       "upper-case",
		"/x --module nobody":    "no module nobody",
	} {
		a := append([]string{"page"}, strings.Fields(args)...)
		if args == "/stats" {
			a = append(a, "--module", "users")
		}
		if _, err := runAdd(t, dir, a...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", args, err, msg)
		}
	}
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
	has(t, filepath.Join(dir, "components/chart.vuka"), "package components", `ChartIsland "../web/islands/Chart.vue"`,
		"func Chart(p ChartProps) ui.Node", "<ChartIsland title={p.Title} count={p.Count} client:load />")
	if !strings.Contains(out, "<components.Chart Title=\"Hello\" Count={3} />") || !strings.Contains(out, "Place it in components/ as") {
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
	has(t, filepath.Join(dir, "modules/notes/board.vuka"), "package notes", `BoardIsland "../../web/islands/notes/Board.vue"`)

	for args, msg := range map[string]string{
		"Chart":             "Chart is declared already in components/chart.vuka",
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
	has(t, filepath.Join(dir, "pages/reports.vuka"), `@web.Get("/reports")`, `@ui.Island("islands/Reports.vue")`,
		"type Reports struct {\n\tui.Live", "Title string `json:\"title\"`", "`json:\"q\" url:\"q\"`", "func (p *Reports) Search(ctx context.Context, q string) error")
	has(t, filepath.Join(dir, "web/islands/Reports.vue"), "import { useLive } from 'vuka:live'", "defineProps<Reports>()", "live.search(")

	mustAdd(t, dir, "page", "/board", "--island", "Board", "--react", "--stateless", "--no-install")
	page := read(t, filepath.Join(dir, "pages/board.vuka"))
	if strings.Contains(page, "ui.Live") || !strings.Contains(page, `@ui.Island("islands/Board.tsx")`) {
		t.Errorf("a stateless page:\n%s", page)
	}
	has(t, filepath.Join(dir, "web/islands/Board.tsx"), "export default function Board(props: Board)", "useLive<Board>()")

	mustAdd(t, dir, "page", "/tags", "--island", "Tags", "--svelte", "--no-install")
	has(t, filepath.Join(dir, "web/islands/Tags.svelte"), "const l = live<Tags>()", "l.search(")
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
