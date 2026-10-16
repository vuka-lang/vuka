package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The line prompter reads numbers, keys, labels and empty lines (the
// default), asks again after an answer it doesn't know, and takes the
// default once the input runs out.
func TestPromptLines(t *testing.T) {
	var out bytes.Buffer
	p := newPrompter(strings.NewReader("2\n\nmysql\nnope\n4\n"), &out, false)
	for _, want := range []int{1, 0, 2, 3, 0} { // the last: no input left
		got, err := p.choose("Database", dbChoices, 0)
		if err != nil || got != want {
			t.Fatalf("choose = %d, %v; want %d", got, err, want)
		}
	}
	for _, s := range []string{"? Database", "* 1. SQLite (a file, no server to run)", "  2. PostgreSQL", "Choose 1-4 [1]: ", `"nope" isn't one of the choices.`} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("the prompt lacks %q:\n%s", s, out.String())
		}
	}
}

// With the terminal raw, arrows (and j/k, Tab, a digit) move the mark and
// Enter takes it; Ctrl-C stops.
func TestPromptKeys(t *testing.T) {
	for keys, want := range map[string]int{
		"\r":                         0,
		"\x1b[B\x1b[B\r":             2,
		"\x1b[B\x1b[B\x1b[B\x1b[B\r": 0, // wraps around
		"\x1b[A\r":                   3,
		"jjk\r":                      1,
		"\t\r":                       1,
		"3\r":                        2,
		"\x1bOB\r":                   1, // application cursor mode
	} {
		var out bytes.Buffer
		p := newPrompter(strings.NewReader(keys), &out, true)
		got, err := p.choose("Database", dbChoices, 0)
		if err != nil || got != want {
			t.Errorf("%q: %d, %v; want %d", keys, got, err, want)
		}
		if !strings.Contains(out.String(), "› ") {
			t.Errorf("%q: no mark drawn:\n%q", keys, out.String())
		}
	}
	for _, keys := range []string{"\x03", "\x04", ""} {
		p := newPrompter(strings.NewReader(keys), &bytes.Buffer{}, true)
		if _, err := p.choose("UI", uiChoices, 0); err != errInterrupted {
			t.Errorf("%q: %v, want interrupted", keys, err)
		}
	}
}

// Questions answered in order by scripted lines.
func TestAnswerScripted(t *testing.T) {
	s := &scaffold{}
	p := newPrompter(strings.NewReader("2\n3\n2\n\n4\n"), &bytes.Buffer{}, false)
	if err := s.answer(map[string]string{}, p); err != nil {
		t.Fatal(err)
	}
	if s.UI != "templ" || s.DB != "mysql" || s.Mail || !s.Auth || s.Frontend != "svelte" {
		t.Errorf("answers: %+v", s)
	}
	// A flag answers its question: it isn't asked.
	s = &scaffold{}
	p = newPrompter(strings.NewReader("2\n"), &bytes.Buffer{}, false)
	if err := s.answer(map[string]string{"ui": "api", "db": "postgres", "mail": "no", "auth": "yes"}, p); err != nil {
		t.Fatal(err)
	}
	if s.UI != "api" || s.DB != "postgres" || s.Mail || !s.Auth || s.Frontend != "react" {
		t.Errorf("flags and an answer: %+v", s)
	}
	// No database: sign-in isn't asked, and is off.
	var out bytes.Buffer
	s = &scaffold{}
	p = newPrompter(strings.NewReader("1\n4\n1\n1\n"), &out, false)
	if err := s.answer(map[string]string{}, p); err != nil {
		t.Fatal(err)
	}
	if s.DB != "none" || s.Auth || s.Frontend != "none" || !strings.Contains(out.String(), "Authentication: no: it keeps its users in the database") {
		t.Errorf("without a database: %+v\n%s", s, out.String())
	}
}

// Without a terminal nothing is asked: the defaults, and the flags.
func TestAnswerDefaults(t *testing.T) {
	if interactive(strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("a reader is a terminal")
	}
	s := &scaffold{}
	if err := s.answer(map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	if s.UI != "live" || s.DB != "sqlite" || !s.Mail || !s.Auth || s.Frontend != "none" {
		t.Errorf("defaults: %+v", s)
	}
	for given, want := range map[string]string{
		"db=none":                         "",
		"db=none auth=no":                 "",
		"db=none auth=yes":                "--auth=yes needs a database",
		"db=oracle":                       "--db=oracle: one of sqlite, postgres, mysql, none",
		"frontend=angular":                "--frontend=angular: one of none, react, vue, svelte",
		"ui=API db=PG auth=true mail=off": "",
		"islands=yes":                     "", // ui=live, frontend=none by default: valid
		"islands=yes ui=templ":            "--islands=yes needs --ui=live",
		"islands=yes frontend=vue":        "--islands=yes needs --ui=live",
		"islands=maybe":                   "--islands=maybe:",
	} {
		flags := map[string]string{}
		for kv := range strings.FieldsSeq(given) {
			k, v, _ := strings.Cut(kv, "=")
			flags[k] = v
		}
		s := &scaffold{}
		err := s.answer(flags, nil)
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: %v, want %q", given, err, want)
		}
		if given == "db=none" && s.Auth {
			t.Error("no database, yet sign-in")
		}
		if strings.HasPrefix(given, "ui=API") && (s.UI != "api" || s.DB != "postgres" || !s.Auth || s.Mail) {
			t.Errorf("synonyms: %+v", s)
		}
	}
}

func TestParseNewArgs(t *testing.T) {
	a, err := parseNewArgs([]string{"apps/my-shop", "--db=postgres", "example.com/shop", "--frontend", "react", "-y", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if a.s.Dir != "apps/my-shop" || a.s.Module != "example.com/shop" || a.s.Name != "My Shop" || !a.yes || !a.force ||
		a.given["db"] != "postgres" || a.given["frontend"] != "react" || a.given["ui"] != "" {
		t.Errorf("parsed: %+v %+v", a, a.s)
	}
	if a, _ := parseNewArgs([]string{"shop", "--name=Corner Shop"}); a.s.Module != "shop" || a.s.Name != "Corner Shop" {
		t.Errorf("--name: %+v", a.s)
	}
	for _, bad := range [][]string{nil, {"a", "b", "c"}, {"a", "--colour=red"}} {
		if _, err := parseNewArgs(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestTagged(t *testing.T) {
	tags := map[string]bool{"live": true, "auth": true, "db": true}
	for in, want := range map[string]string{
		"main.vuka":                    "main.vuka",
		"layouts@live/app.vuka":        "layouts/app.vuka",
		"modules@auth/users/x.vuka@db": "modules/users/x.vuka",
		"pages@templ/home.vuka":        "",
		"a@live+auth/b@live+mail":      "",
	} {
		got, ok := tagged(in, tags)
		if want == "" && ok || want != "" && got != want {
			t.Errorf("tagged(%q) = %q, %v", in, got, ok)
		}
	}
}

// The files each combination makes.
func TestRenderFileSets(t *testing.T) {
	render := func(ui, db, mail, auth, frontend string) map[string][]byte {
		t.Helper()
		s := &scaffold{Dir: "shop", Module: "example.com/shop", Name: "Shop"}
		if err := s.answer(map[string]string{"ui": ui, "db": db, "mail": mail, "auth": auth, "frontend": frontend}, nil); err != nil {
			t.Fatal(err)
		}
		s.AdminEmail, s.AdminPassword = "admin@example.com", "pw"
		s.VukaVersion, s.UIVersion, s.WebVersion, s.ORMVersion = "v1", "v2", "v3", "v4"
		files, err := s.render()
		if err != nil {
			t.Fatal(err)
		}
		for name, b := range files {
			if bytes.Contains(b, []byte("{%")) || bytes.Contains(b, []byte("<no value>")) {
				t.Errorf("%s/%s/%s: %s isn't fully rendered", ui, db, auth, name)
			}
		}
		return files
	}

	// The default app.
	files := render("", "", "", "", "")
	want := []string{
		".env", ".env.example", ".gitignore", "README.md", "go.mod", "main.vuka", "main_test.vuka",
		"components/button.vuka", "components/card.vuka", "components/class.vuka", "components/flash.vuka",
		"components/icon.vuka", "components/input.vuka", "components/logo.vuka",
		"layouts/app.vuka", "layouts/auth.vuka", "layouts/document.vuka", "layouts/nav.vuka",
		"migrations/0001_initial.go", "migrations/doc.go",
		"modules/users/model.vuka", "modules/users/pages.vuka", "modules/users/service.vuka",
		"modules/users/stores.vuka", "modules/users/users_test.vuka",
		"pages/home.vuka", "pages/login.vuka", "static/logo.svg", "styles/app.css",
	}
	slices.Sort(want)
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
		t.Errorf("the default app's files:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for name, s := range map[string]string{
		"go.mod":       "github.com/vuka-lang/orm v4",
		"main.vuka":    `_ "github.com/vuka-lang/orm/sqlite"`,
		".env":         "ADMIN_PASSWORD=pw",
		".env.example": "ADMIN_PASSWORD=change-me",
		".gitignore":   ".env\n",
		"README.md":    "vuka add module notes",
	} {
		if !strings.Contains(string(files[name]), s) {
			t.Errorf("%s lacks %q:\n%s", name, s, files[name])
		}
	}
	if strings.Contains(string(files[".env.example"]), "ADMIN_PASSWORD=pw") {
		t.Error(".env.example has the admin's password")
	}
	if !strings.Contains(string(files["main.vuka"]), "import (\n\t\"embed\"\n\t\"io/fs\"\n\t\"log\"\n\n\t\"github.com/vuka-lang/orm\"") {
		t.Errorf("main.vuka's imports aren't grouped:\n%s", files["main.vuka"])
	}

	has := func(files map[string][]byte, yes []string, no []string) {
		t.Helper()
		for _, f := range yes {
			if _, ok := files[f]; !ok {
				t.Errorf("no %s", f)
			}
		}
		for _, f := range no {
			if _, ok := files[f]; ok {
				t.Errorf("%s, unwanted", f)
			}
		}
	}

	pg := render("live", "postgres", "no", "yes", "none")
	has(pg, []string{"migrations/0001_initial.go"}, nil)
	if m := string(pg["main.vuka"]); !strings.Contains(m, "orm/postgres") || strings.Contains(m, "mail.Provide") {
		t.Errorf("postgres without mail: %s", m)
	}
	if e := string(pg[".env"]); !strings.Contains(e, "DATABASE_URL=postgres://postgres:postgres@localhost:5432/shop?sslmode=disable") || strings.Contains(e, "MAIL_") {
		t.Errorf("postgres .env: %s", e)
	}
	if !strings.Contains(string(render("live", "mysql", "", "", "")[".env"]), "DATABASE_URL=mysql://root:password@tcp(localhost:3306)/shop") {
		t.Error("mysql's DATABASE_URL")
	}

	noauth := render("live", "sqlite", "yes", "no", "none")
	has(noauth, []string{"pages/home.vuka", "migrations/doc.go"}, []string{"pages/login.vuka", "layouts/auth.vuka", "modules/users/model.vuka", "migrations/0001_initial.go"})
	if strings.Contains(string(noauth["layouts/app.vuka"]), "auth") {
		t.Error("the shell of an app without sign-in mentions auth")
	}

	nodb := render("live", "none", "no", "", "none")
	has(nodb, []string{"pages/home.vuka"}, []string{"migrations/doc.go", "modules/users/model.vuka"})
	if m := string(nodb["main.vuka"]) + string(nodb["go.mod"]); strings.Contains(m, "orm") {
		t.Errorf("an app without a database has the ORM:\n%s", m)
	}

	api := render("api", "sqlite", "yes", "yes", "none")
	has(api, []string{"api/home.vuka", "modules/users/api.vuka", "modules/users/service.vuka"},
		[]string{"pages/home.vuka", "layouts/app.vuka", "components/button.vuka", "static/logo.svg", "styles/app.css", "modules/users/pages.vuka"})
	if !strings.Contains(string(api["main.vuka"]), "web.NoTailwind()") {
		t.Error("an API has Tailwind")
	}

	templ := render("templ", "sqlite", "yes", "yes", "none")
	has(templ, []string{"layouts/layouts.templ", "layouts/layouts.vuka", "pages/home.templ", "pages/login.templ", "modules/users/users.templ", "components/button.vuka"},
		[]string{"layouts/app.vuka", "pages/login.vuka@auth"})
	if strings.Contains(string(templ["main.vuka"]), "LiveLayout") {
		t.Error("a templ app has a live layout")
	}

	for _, fw := range []string{"react", "vue", "svelte"} {
		f := render("live", "sqlite", "yes", "yes", fw)
		entry := map[string]string{"react": "frontend/src/App.jsx", "vue": "frontend/src/App.vue", "svelte": "frontend/src/App.svelte"}[fw]
		has(f, []string{"frontend/package.json", "frontend/vite.config.js", "frontend/index.html", "frontend/dist/.gitkeep", "frontend/public/logo.svg", "frontend/src/app.css", entry}, nil)
		if !strings.Contains(string(f["main.vuka"]), `web.FrontendAt("/app")`) || !strings.Contains(string(f["frontend/vite.config.js"]), "base: '/app/'") {
			t.Errorf("%s: not served at /app", fw)
		}
		if !strings.Contains(string(f[entry]), "APP_NAME") || !strings.Contains(string(f[".gitignore"]), "node_modules") {
			t.Errorf("%s: the app's name, or .gitignore", fw)
		}
	}
}

// A Vue island app scaffolds its web/ frontend root, the sample component
// and the page that hosts it, and wires the island frontend in main.vuka;
// islands=no makes none of them.
func TestRenderIslands(t *testing.T) {
	render := func(islands string) map[string][]byte {
		t.Helper()
		s := &scaffold{Dir: "shop", Module: "example.com/shop", Name: "Shop"}
		if err := s.answer(map[string]string{"ui": "live", "db": "none", "mail": "no", "auth": "no", "frontend": "none", "islands": islands}, nil); err != nil {
			t.Fatal(err)
		}
		s.AdminEmail, s.AdminPassword = "admin@example.com", "pw"
		s.VukaVersion, s.UIVersion, s.WebVersion, s.ORMVersion = "v1", "v2", "v3", "v4"
		files, err := s.render()
		if err != nil {
			t.Fatal(err)
		}
		for name, b := range files {
			if bytes.Contains(b, []byte("{%")) || bytes.Contains(b, []byte("<no value>")) {
				t.Errorf("%s isn't fully rendered", name)
			}
		}
		return files
	}

	yes := render("yes")
	for _, f := range []string{"web/package.json", "web/vite.config.mts", "web/app.css", "web/dist/index.html", "web/islands/Counter.vue", "pages/island.vuka"} {
		if _, ok := yes[f]; !ok {
			t.Errorf("islands: no %s", f)
		}
	}
	if pkg := string(yes["web/package.json"]); !strings.Contains(pkg, "@nuxt/ui") || !strings.Contains(pkg, `"vite"`) || !strings.Contains(pkg, "vue-router") {
		t.Errorf("web/package.json lacks the Vite island build deps:\n%s", pkg)
	}
	if cfg := string(yes["web/vite.config.mts"]); !strings.Contains(cfg, "@nuxt/ui/vite") || !strings.Contains(cfg, "vuka()") {
		t.Errorf("web/vite.config.mts doesn't wire the UI + vuka plugins:\n%s", cfg)
	}
	if vue := string(yes["web/islands/Counter.vue"]); !strings.Contains(vue, "<UButton") {
		t.Errorf("web/islands/Counter.vue doesn't use a Nuxt UI component:\n%s", vue)
	}
	if m := string(yes["main.vuka"]); !strings.Contains(m, `web.FrontendRoot("web")`) || !strings.Contains(m, "all:web/dist") {
		t.Errorf("main.vuka doesn't wire the island frontend:\n%s", m)
	}
	if p := string(yes["pages/island.vuka"]); !strings.Contains(p, `@ui.Island("islands/Counter.vue")`) || !strings.Contains(p, "<ui.Island>") {
		t.Errorf("pages/island.vuka doesn't declare and place the island:\n%s", p)
	}
	if g := string(yes[".gitignore"]); !strings.Contains(g, "/web/node_modules/") {
		t.Errorf(".gitignore doesn't ignore the island deps:\n%s", g)
	}

	no := render("no")
	for _, f := range []string{"web/package.json", "web/dist/index.html", "web/islands/Counter.vue", "pages/island.vuka"} {
		if _, ok := no[f]; ok {
			t.Errorf("islands=no, yet %s", f)
		}
	}
	if m := string(no["main.vuka"]); strings.Contains(m, "web/dist") || strings.Contains(m, "FrontendRoot") {
		t.Errorf("islands=no, yet main.vuka wires an island frontend:\n%s", m)
	}
}

// A directory with files in it is refused unless forced; forced, the
// files in it are kept.
func TestTargetAndForce(t *testing.T) {
	dir := t.TempDir()
	if err := checkTarget(filepath.Join(dir, "new"), false); err != nil {
		t.Errorf("a new directory: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("mine"), 0o644)
	if err := checkTarget(dir, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("a directory with files: %v", err)
	}
	if err := checkTarget(dir, true); err != nil {
		t.Error(err)
	}
	kept, err := writeFiles(dir, map[string][]byte{"README.md": []byte("theirs"), "a/b.txt": []byte("new"), ".env": []byte("SECRET=1")})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "mine" || !slices.Equal(kept, []string{"README.md"}) {
		t.Errorf("kept %v; README.md is %q", kept, b)
	}
	if fi, err := os.Stat(filepath.Join(dir, ".env")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf(".env: %v %v", fi, err)
	}
}

func TestNewPassword(t *testing.T) {
	a, b := newPassword(), newPassword()
	if len(a) != 20 || a == b || strings.ContainsAny(a, "0O1lI") {
		t.Errorf("passwords %q %q", a, b)
	}
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{"shop": "Shop", "my-app": "My App", "corner_shop.io": "Corner Shop Io", "---": "App"} {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q", in, got)
		}
	}
}
