package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addPageAt runs vuka add page path in the app at dir, returning its output.
func addPageAt(t *testing.T, dir, path string) string {
	t.Helper()
	f, err := parseAddFlags("page", []string{path})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := addPage(dir, "example.com/shop", f, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// writeApp writes files (relative path → content) into a new module
// example.com/shop.
func writeApp(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/shop\n\ngo 1.25.0\n"
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The first page under pages/ of an app vuka new made without folder
// pages, templ views or an API, makes main pass pages.Routes(); the next
// page leaves main as it is.
func TestAddPageServesRoutes(t *testing.T) {
	for _, ui := range []string{"templ", "api"} {
		t.Run(ui, func(t *testing.T) {
			dir := scaffolded(t, map[string]string{"ui": ui, "db": "none", "auth": "no", "mail": "no"})
			out := addPageAt(t, dir, "/reports")
			if !strings.Contains(out, "changed main.vuka") {
				t.Errorf("main.vuka unchanged:\n%s", out)
			}
			main := read(t, filepath.Join(dir, "main.vuka"))
			if !strings.Contains(main, "\t\"example.com/shop/pages\"\n") || strings.Contains(main, `_ "example.com/shop/pages"`) ||
				!strings.Contains(main, "\t\tpages.Routes(), // the pages under pages/\n\t}") {
				t.Errorf("main.vuka doesn't pass pages.Routes():\n%s", main)
			}
			out = addPageAt(t, dir, "/other")
			if strings.Contains(out, "main.vuka") || read(t, filepath.Join(dir, "main.vuka")) != main {
				t.Errorf("a second page changed main.vuka:\n%s", out)
			}
		})
	}
}

// A live app vuka new made passes pages.Routes() already.
func TestAddPageLiveAppServed(t *testing.T) {
	dir := scaffolded(t, map[string]string{"db": "none", "auth": "no", "mail": "no"})
	before := read(t, filepath.Join(dir, "main.vuka"))
	if out := addPageAt(t, dir, "/reports"); strings.Contains(out, "main.vuka") || strings.Contains(out, "web.New") {
		t.Errorf("output:\n%s", out)
	}
	if read(t, filepath.Join(dir, "main.vuka")) != before {
		t.Error("main.vuka changed")
	}
}

func TestPagesRoutesEdit(t *testing.T) {
	const webImport = "import (\n\t\"log\"\n\n\t\"github.com/vuka-lang/web/v2\"\n)\n\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		file  string // the file changed
		want  string // in its new source
		hint  string // in the hint, when it can't be changed
	}{
		{name: "main.go call", file: "main.go",
			files: map[string]string{"main.go": "package main\n\n" + webImport + "func main() {\n\t// serve\n\tlog.Fatal(web.New(web.Dev()).Run(\"\")) // :8080\n}\n"},
			want:  "import (\n\t\"log\"\n\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/shop/pages\"\n)\n\nfunc main() {\n\t// serve\n\tlog.Fatal(web.New(pages.Routes(), web.Dev()).Run(\"\")) // :8080\n}\n"},
		{name: "no options", file: "main.go",
			files: map[string]string{"main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n"},
			want:  "import \"github.com/vuka-lang/web/v2\"\nimport \"example.com/shop/pages\"\n\nfunc main() { web.New(pages.Routes()).Run(\"\") }\n"},
		{name: "spread of a variable", file: "main.go",
			files: map[string]string{"main.go": "package main\n\n" + webImport + "func main() {\n\topts := []web.Option{web.Dev()}\n\tlog.Fatal(web.New(opts...).Run(\"\"))\n}\n"},
			want:  "opts := []web.Option{web.Dev(), pages.Routes()}"},
		{name: "main.vuka with sugar", file: "main.vuka",
			files: map[string]string{"main.vuka": "package main\n\n" + webImport + "@web.Get(\"/health\")\nfunc health() Result[string] {\n\tn := count()?\n\treturn Ok(n)\n}\n\nfunc count() Result[string] { return Ok(\"ok\") }\n\nfunc main() {\n\tlog.Fatal(web.New(\n\t\tweb.Dev(),\n\t).Run(\"\"))\n}\n"},
			want:  "@web.Get(\"/health\")\nfunc health() Result[string] {\n\tn := count()?\n\treturn Ok(n)\n}\n\nfunc count() Result[string] { return Ok(\"ok\") }\n\nfunc main() {\n\tlog.Fatal(web.New(\n\t\tpages.Routes(),\n\t\tweb.Dev(),\n\t).Run(\"\"))\n}\n"},
		{name: "aliased import", file: "main.go",
			files: map[string]string{"main.go": "package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\tp \"example.com/shop/pages\"\n)\n\nvar _ = p.X\n\nfunc main() { web.New().Run(\"\") }\n"},
			want:  "web.New(p.Routes())"},
		{name: "the name is taken", file: "main.go",
			files: map[string]string{"main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nvar pages = 1\n\nfunc main() { web.New().Run(\"\") }\n"},
			hint:  "the name pages is taken"},
		{name: "no web.New", file: "main.go",
			files: map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
			hint:  "its main has no web.New call vuka can find): add pages.Routes() to the app's web.New in main.go, importing \"example.com/shop/pages\""},
		{name: "a spread vuka can't follow", file: "main.go",
			files: map[string]string{"main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New(other.Options()...).Run(\"\") }\n"},
			hint:  "a list of options vuka can't follow"},
		{name: "several mains", files: map[string]string{
			"cmd/a/main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n",
			"cmd/b/main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n"},
			hint: "several package mains, cmd/a/main.go, cmd/b/main.go, and none passes pages.Routes()"},
		{name: "a main in a subdirectory", file: "cmd/app/main.go",
			files: map[string]string{"cmd/app/main.go": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n"},
			want:  "web.New(pages.Routes())"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.files["pages/index.vuka.html"] = "<script lang=\"go\">\npackage pages\n\ntype Home struct{}\n</script>\n\n<p>hi</p>\n"
			dir := writeApp(t, c.files)
			e := pagesRoutesEdit(dir, "example.com/shop", filepath.Join(dir, "pages"))
			if c.hint != "" {
				if e.Src != nil || !strings.Contains(e.Hint, c.hint) || !strings.Contains(e.Hint, "web.New(pages.Routes(), …)") {
					t.Fatalf("hint %q, edit:\n%s", e.Hint, e.Src)
				}
				return
			}
			if e.Hint != "" || filepath.ToSlash(e.File) != c.file || !strings.Contains(string(e.Src), c.want) {
				t.Fatalf("hint %q, file %s:\n%s", e.Hint, e.File, e.Src)
			}
			// applied, it is served: nothing more to do
			os.WriteFile(filepath.Join(dir, c.file), e.Src, 0o644)
			if again := pagesRoutesEdit(dir, "example.com/shop", filepath.Join(dir, "pages")); !again.Served || again.Src != nil {
				t.Errorf("not served after the edit: %+v", again)
			}
		})
	}
}

// W0701 names vuka fix routes when it can make the change, and the fixer
// makes it.
func TestFixRoutes(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"main.go":               "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n",
		"pages/index.vuka.html": "<script lang=\"go\">\npackage pages\n\ntype Home struct{}\n</script>\n\n<p>hi</p>\n",
	})
	ds := routesUnserved(dir, "example.com/shop", scanRoutes(dir, "example.com/shop", nil))
	if len(ds) != 1 || ds[0].Code != "W0701" || !strings.Contains(ds[0].Help, "vuka fix routes adds it to web.New in main.go") {
		t.Fatalf("diagnostics: %+v", ds)
	}
	changes, err := fixRoutes(&fixModule{root: realPathOf(dir), modPath: "example.com/shop"})
	if err != nil || len(changes) != 1 || changes[0].what != "pass pages.Routes() to web.New in main.go" {
		t.Fatalf("changes %+v, %v", changes, err)
	}
	if err := changes[0].apply(); err != nil {
		t.Fatal(err)
	}
	if ds := routesUnserved(dir, "example.com/shop", scanRoutes(dir, "example.com/shop", nil)); len(ds) != 0 {
		t.Errorf("still W0701: %+v", ds)
	}
}

// vuka add page in apps vuka new made without folder pages: the app
// builds and serves the page.
func TestAddPageRoutesEndToEnd(t *testing.T) {
	e := newE2E(t)
	off := []string{"VUKA_TAILWIND=off", "VUKA_FRONTEND=off"}
	for _, ui := range []string{"templ", "api"} {
		dir := e.new("app"+ui, "--ui="+ui, "--db=none", "--auth=no", "--mail=no")
		if out := e.run(dir, off, "vuka", "add", "page", "/reports"); !strings.Contains(out, "changed main.vuka") {
			t.Errorf("%s: %s", ui, out)
		}
		bin := filepath.Join(dir, "app")
		e.run(dir, off, "vuka", "build", "-o", bin, ".")
		base, stop := serve(t, dir, bin)
		res, err := http.Get(base + "/reports")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		stop()
		if res.StatusCode != 200 || !strings.Contains(string(b), "Clicked 0 times") {
			t.Errorf("%s: GET /reports: %d\n%s", ui, res.StatusCode, b)
		}
		os.Remove(bin)
	}
}

// vuka add module writing the module's page passes pages.Routes() when
// main doesn't.
func TestAddModuleServesRoutes(t *testing.T) {
	dir := scaffolded(t, map[string]string{"auth": "no", "mail": "no"})
	main := filepath.Join(dir, "main.vuka")
	src := read(t, main)
	unserved := strings.Replace(strings.Replace(src, "\t\tpages.Routes(), // the pages under pages/\n", "", 1), "\t\"example.com/shop/pages\"\n", "", 1)
	if unserved == src || os.WriteFile(main, []byte(unserved), 0o644) != nil {
		t.Fatal("main.vuka didn't pass pages.Routes()")
	}
	t.Chdir(dir)
	var out bytes.Buffer
	if err := add([]string{"module", "notes"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := read(t, main); !strings.Contains(out.String(), "changed main.vuka") || !passesRoutes([]byte(got), "example.com/shop/pages", "pages") {
		t.Errorf("main.vuka doesn't pass pages.Routes():\n%s\n%s", out.String(), got)
	}
}
