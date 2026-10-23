package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// routesModule copies testdata/routes/app into a module requiring the web
// stub beside it (testdata/routes/web, the folder-route API) and the local
// ui, plus files (path → source, "" removes one).
func routesModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	src := filepath.Join("testdata", "routes")
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".golden") {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, _ := os.ReadFile(p)
		writeFile(t, filepath.Join(root, rel), string(b))
		return nil
	})
	app := filepath.Join(root, "app")
	gomod, gosum := testModule(t, "example.com/app")
	gomod = strings.Replace(gomod, "require (", "require (\n\tgithub.com/vuka-lang/web/v2 v2.0.0", 1) + "\nreplace github.com/vuka-lang/web/v2 => ../web\n"
	writeFile(t, filepath.Join(app, "go.mod"), gomod)
	writeFile(t, filepath.Join(app, "go.sum"), gosum)
	for name, src := range files {
		if src == "" {
			os.Remove(filepath.Join(app, name))
			continue
		}
		writeFile(t, filepath.Join(app, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(app)
	return app
}

// checkGolden compares got with testdata/routes/name.golden (-update
// rewrites it).
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(routesTestdata, name+".golden")
	if f := flag.Lookup("update"); f != nil && f.Value.String() == "true" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s differs (go test -run %s -update):\n%s", name, t.Name(), got)
	}
}

var routesTestdata, _ = filepath.Abs(filepath.Join("testdata", "routes"))

// TestFolderRoutes: the route table of the fixture, the files generated for
// it, and the module building with them in the overlay.
func TestFolderRoutes(t *testing.T) {
	app := routesModule(t, nil)
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	checkGolden(t, "routes.txt", out.Bytes())
	out.Reset()
	if err := routesCmd([]string{"-json"}, &out, &errOut); err != nil {
		t.Fatalf("vuka routes -json: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) == 0 {
		t.Fatalf("vuka routes -json: %v\n%s", err, out.Bytes())
	}

	out.Reset()
	if err := gen([]string{"-inplace"}, &out); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	for _, f := range routeFiles {
		b, err := os.ReadFile(filepath.Join(app, f))
		if err != nil {
			t.Fatalf("vuka gen -inplace wrote no %s:\n%s", f, out.Bytes())
		}
		if problems := generatedProblems(b); len(problems) > 0 {
			t.Errorf("%s: %s", f, strings.Join(problems, "; "))
		}
		checkGolden(t, strings.NewReplacer("/", "_").Replace(strings.TrimPrefix(f, "pages/")), b)
	}
	out.Reset()
	if err := gen([]string{"-inplace", "-check"}, &out); err != nil {
		t.Errorf("vuka gen -inplace -check after gen: %v\n%s", err, out.Bytes())
	}
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet with the files on disk: %v\n%s", err, errOut.Bytes())
	}

	// A new page, and the files on disk stale: the overlay has the fresh
	// ones, and gen -check says they are stale.
	writeFile(t, filepath.Join(app, "pages/orgs/[org]/edit.vuka.html"), editPage)
	writeFile(t, filepath.Join(app, "pages/orgs/[org]/index.vuka.html"), strings.Replace(orgIndex, "ResetPassword.Path()", "Edit.Path()", 1))
	errOut.Reset()
	if err := runGo("build", []string{"-o", os.DevNull, "."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka build with a new page: %v\n%s", err, errOut.Bytes())
	}
	out.Reset()
	if err := gen([]string{"-inplace", "-check"}, &out); err == nil || !strings.Contains(out.String(), "stale: pages/urls/urls_gen.go") {
		t.Errorf("vuka gen -inplace -check with a new page: %v\n%s", err, out.Bytes())
	}

	// With nothing on disk, the overlay alone builds it.
	for _, f := range routeFiles {
		os.Remove(filepath.Join(app, f))
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet from the overlay: %v\n%s", err, errOut.Bytes())
	}
}

// routeFiles are the files generated for the fixture.
var routeFiles = []string{"pages/route.go", "pages/urls/urls_gen.go"}

const editPage = `package org

// Edit edits the organisation.
type Edit struct {
	Org int
}

func NewEdit() *Edit { return &Edit{} }
<form method="post"><input name="name"/></form>
`

var orgIndex = func() string {
	b, _ := os.ReadFile(filepath.Join("testdata", "routes", "app", "pages", "orgs", "[org]", "index.vuka.html"))
	return string(b)
}()

// TestFolderRouteErrors: each mistake of the route table, its code and
// where it is reported.
func TestFolderRouteErrors(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		code  string
		at    string
	}{
		{"two pages on a path", map[string]string{"pages/orgs/new/index.vuka.html": "package new\n\ntype Index struct{}\n<p>x</p>\n"},
			"E0701", "pages/orgs/new/index.vuka.html:3"},
		{"a method func and a method on one path", map[string]string{"pages/api/orgs/index.vuka": "package orgs\n\nimport \"context\"\n\nfunc GET(ctx context.Context) error { return nil }\n"},
			"E0701", "pages/api/orgs/index.vuka:5"},
		{"parameter folders side by side", map[string]string{"pages/orgs/[other]/index.vuka.html": "package other\n\ntype Index struct {\n\tOther int\n}\n<p>x</p>\n"},
			"E0702", "pages/orgs/[other]"},
		{"parameter files side by side", map[string]string{"pages/api/pets/[name].vuka": "package pets\n\nimport \"context\"\n\nfunc PUT(ctx context.Context, name string) error { return nil }\n"},
			"E0702", "pages/api/pets/[name].vuka"},
		{"ambiguous", map[string]string{"pages/orgs/new/[x]/reset_password.vuka.html": "package x\n\ntype ResetPassword struct {\n\tX int\n}\n<p>x</p>\n"},
			"E0702", "pages/orgs/new/[x]/reset_password.vuka.html:3"},
		{"below a catch-all", map[string]string{"pages/files/[rest...]/more/index.go": "package more\n"},
			"E0703", "pages/files/[rest...]/more"},
		{"a folder below a catch-all", map[string]string{"pages/app/[route...]/more/index.vuka.html": "package more\n\ntype Index struct {\n\tRoute string\n}\n<p>x</p>\n"},
			"E0703", "pages/app/[route...]/more/index.vuka.html:3"},
		{"a page beside a catch-all's index", map[string]string{"pages/app/[route...]/raw.vuka.html": "package route\n\ntype Raw struct {\n\tRoute string\n}\n<p>x</p>\n"},
			"E0703", "pages/app/[route...]/raw.vuka.html:3"},
		{"a bracket that names nothing", map[string]string{"pages/orgs/[1x]/index.vuka": "package x\n"},
			"E0704", "pages/orgs/[1x]"},
		{"a bracketed file that names nothing", map[string]string{"pages/orgs/[a-b].vuka": "package orgs\n"},
			"E0704", "pages/orgs/[a-b].vuka:1"},
		{"a parameter bound twice", map[string]string{"pages/orgs/[org]/members/[org].vuka": "package members\n"},
			"E0704", "pages/orgs/[org]/members/[org]"},
		{"a parameter type", map[string]string{"pages/orgs/[org]/guard.vuka": "package org\n\nimport \"context\"\n\ntype Guard struct{ Org float64 }\n\nfunc (g *Guard) Load(ctx context.Context) error { return nil }\n"},
			"E0712", "pages/orgs/[org]/guard.vuka:5"},
		{"a parameter type a URL can't hold", map[string]string{"pages/files/[file...].vuka.html": "package files\n\n// File serves a file.\ntype File struct {\n\tFile float64\n}\n<pre></pre>\n"},
			"E0705", "pages/files/[file...].vuka.html:5"},
		{"a parameter type of a page package", map[string]string{"pages/files/[file...].vuka.html": "package files\n\n// File serves a file.\ntype File struct {\n\tFile Name\n}\n<pre></pre>\n", "pages/files/name.go": "package files\n\ntype Name string\n"},
			"E0705", "pages/files/[file...].vuka.html:5"},
		{"web.Rest on one segment", map[string]string{"pages/api/pets/[id].vuka": "package pets\n\nimport (\n\t\"context\"\n\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc GET(ctx context.Context, id web.Rest) (string, error) { return \"\", nil }\n"},
			"E0705", "pages/api/pets/[id].vuka:9"},
		{"a folder and a page named alike", map[string]string{"pages/orgs/new/draft.vuka.html": "package new\n\ntype Draft struct{}\n<p>x</p>\n"},
			"E0706", "pages/orgs/new.vuka.html:7"},
		{"a Routes of package pages", map[string]string{"pages/routes.go": "package pages\n\nfunc Routes() {}\n"},
			"E0706", "pages/routes.go:3"},
		{"a declared route", map[string]string{"main.go": "package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/app/pages\"\n)\n\nfunc main() { web.New(pages.Routes()).Get(\"/orgs/{id}\", nil) }\n"},
			"E0707", "pages/orgs/[org]/index.vuka.html:11"},
		{"a layout without Children", map[string]string{"pages/_layout.vuka.html": "package pages\n\n// Layout has no Children.\ntype Layout struct {\n\tBody string\n}\n<main></main>\n"},
			"E0708", "pages/_layout.vuka.html:4"},
		{"a layout's Load", map[string]string{"pages/orgs/[org]/_layout.vuka.html": "package org\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Layout struct {\n\tOrg      int\n\tChildren ui.Node\n}\n\nfunc (l *Layout) Load() {}\n<section>{Children}</section>\n"},
			"E0708", "pages/orgs/[org]/_layout.vuka.html:10"},
		{"a guard without Load", map[string]string{"pages/api/guard.go": "package api\n\ntype Guard struct{}\n"},
			"E0708", "pages/api/guard.go:3"},
		{"a guard's Load", map[string]string{"pages/api/guard.go": "package api\n\ntype Guard struct{}\n\nfunc (g *Guard) Load() {}\n"},
			"E0708", "pages/api/guard.go:5"},
		{"a page and an endpoint on one path", map[string]string{"pages/orgs/new/index.go": "package new\n\nimport \"context\"\n\ntype Index struct{}\n\nfunc NewIndex() *Index { return nil }\n\nfunc (*Index) Get(ctx context.Context) (string, error) { return \"\", nil }\n"},
			"E0701", "pages/orgs/new/index.go:9"},
		{"an endpoint's scalar parameter", map[string]string{"pages/api/orgs/get.go": "package orgs\n\nimport \"context\"\n\nfunc (*Index) Put(ctx context.Context, id int) error { return nil }\n"},
			"E0709", "pages/api/orgs/get.go:5"},
		{"a method func's unnamed scalar", map[string]string{"pages/api/pets/[id].vuka": "package pets\n\nimport \"context\"\n\nfunc GET(ctx context.Context, _ int64) (int64, error) { return 0, nil }\n"},
			"E0709", "pages/api/pets/[id].vuka:5"},
		{"a middleware's signature", map[string]string{"pages/orgs/middleware.go": "package orgs\n\nimport \"net/http\"\n\nfunc Middleware(next http.Handler) error { return nil }\n"},
			"E0710", "pages/orgs/middleware.go:5"},
		{"a middleware constructor's dependency", map[string]string{"pages/api/middleware.go": "package api\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc Middleware(n int) web.Middleware {\n\treturn func(next http.Handler) http.Handler { return next }\n}\n"},
			"E0710", "pages/api/middleware.go:9"},
		{"a Get method", map[string]string{"pages/orgs/get.go": "package orgs\n\nimport \"context\"\n\nfunc (*New) Get(ctx context.Context) error { return nil }\n"},
			"E0709", "pages/orgs/get.go:5"},
		{"a scalar handler parameter", map[string]string{"pages/orgs/put.go": "package orgs\n\nimport \"context\"\n\nfunc (*New) Put(ctx context.Context, name string) error { return nil }\n"},
			"E0709", "pages/orgs/put.go:5"},
		{"a parameter nothing binds", map[string]string{"pages/orgs/[org]/team-settings.vuka.html": "", "pages/orgs/[org]/_layout.vuka.html": "package org\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Layout struct {\n\tChildren ui.Node\n}\n<section>{Children}</section>\n",
			"pages/orgs/[org]/guard.vuka": "", "pages/orgs/[org]/index.vuka.html": "", "pages/orgs/[org]/reset_password.vuka.html": "", "pages/orgs/[org]/store.vuka": "", "pages/orgs/[org]/permits/[permit].vuka.html": ""},
			"E0711", "pages/orgs/[org]"},
		{"a parameter page without a struct", map[string]string{"pages/api/[x].vuka.html": "package api\n\n<p>x</p>\n"},
			"E0711", "pages/api/[x].vuka.html:1"},
		{"a parameter folder and file", map[string]string{"pages/orgs/[org].vuka": "package orgs\n\nimport \"context\"\n\nfunc PATCH(ctx context.Context, org int) error { return nil }\n"},
			"E0713", "pages/orgs/[org].vuka"},
		{"a 1.x Params", map[string]string{"pages/orgs/params.go": "package orgs\n\n// Params binds nothing now.\ntype Params struct{ Org int }\n"},
			"E0714", "pages/orgs/params.go:4"},
		{"a 1.x Layout", map[string]string{"pages/orgs/layout.go": "package orgs\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Layout wraps nothing now.\ntype Layout struct{ Children ui.Node }\n"},
			"E0715", "pages/orgs/layout.go:6"},
		{"Go of another package in a parameter folder", map[string]string{"pages/orgs/[org]/edit.go": "package edit\n"},
			"E0716", "pages/orgs/[org]/edit.go:1"},
		{"a method func in two Go files", map[string]string{"pages/api/a.go": "package api\n\nfunc GET() {}\n", "pages/api/b.go": "package api\n\nfunc GET() {}\n"},
			"E0717", "pages/api/b.go:1"},
		{"a 1.x generated file", map[string]string{"pages/orgs/routes_gen.go": "package orgs\n"},
			"E0718", "pages/orgs/routes_gen.go:1"},
		{"a route.go by hand", map[string]string{"pages/route.go": "package pages\n"},
			"E0718", "pages/route.go:1"},
		{"a page written in Go", map[string]string{"pages/contact.go": "package pages\n\nimport (\n\t\"context\"\n\t\"io\"\n)\n\ntype Contact struct{}\n\nfunc NewContact() *Contact { return &Contact{} }\n\nfunc (*Contact) Render(ctx context.Context, w io.Writer) error { return nil }\n"},
			"E0721", "pages/contact.go:12"},
		{"a template-only page", map[string]string{"pages/terms.vuka.html": "package pages\n\n<p>terms</p>\n"},
			"E0719", "pages/terms.vuka.html:1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			routesModule(t, c.files)
			var out, errOut bytes.Buffer
			err := routesCmd([]string{"-json"}, &out, &errOut)
			if err == nil {
				t.Fatalf("no error:\n%s", out.Bytes())
			}
			got := errOut.String()
			if !strings.Contains(got, c.code) || !strings.Contains(got, c.at) {
				t.Errorf("want %s at %s:\n%s", c.code, c.at, got)
			}
			if c.code == "E0721" && !strings.Contains(got, "Contact is a page whose markup is written in Go: a page is a .vuka.html file") {
				t.Errorf("E0721's message:\n%s", got)
			}
		})
	}
}

// generatedProblems is how a generated file breaks the rules generated
// code keeps (transpile's TestContract): gofmt'd, one import declaration,
// only web's public API.
func generatedProblems(src []byte) []string {
	var out []string
	if b := gofmt(string(src)); !bytes.Equal(b, src) {
		out = append(out, "not gofmt'd")
	}
	if strings.Count(string(src), "\nimport ") > 1 {
		out = append(out, "two import declarations")
	}
	for _, sel := range []string{"web.", "ui."} {
		for _, part := range strings.Split(string(src), sel)[1:] {
			name := part[:strings.IndexFunc(part, func(r rune) bool { return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') })]
			switch sel + name {
			case "web.PageRoute", "web.LayoutOf", "web.Receiver", "web.Rest", "web.GuardOf", "web.ReloadLayout", "web.UseOf", "web.Option", "web.OptionFunc", "web.App", "web.New":
			default:
				out = append(out, "names "+sel+name)
			}
		}
	}
	return out
}

func TestKebab(t *testing.T) {
	for in, want := range map[string]string{"New": "new", "ResetPassword": "reset-password", "HTMLPage": "html-page", "Step2": "step2", "APIKeys": "api-keys"} {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestPatterns(t *testing.T) {
	for _, c := range []struct {
		a, b         string
		overlap, a2b bool
	}{
		{"/orgs/new", "/orgs/{}", true, true},
		{"/orgs/{}", "/orgs/new", true, false},
		{"/orgs/{}/edit", "/orgs/new/{}", true, false},
		{"/files/{...}", "/files/a/b", true, false},
		{"/files/a/b", "/files/{...}", true, true},
		{"/files", "/files/{...}", false, false},
		{"/a/{}", "/b/{}", false, false},
	} {
		a, b := splitPattern(c.a), splitPattern(c.b)
		if got := patternsOverlap(a, b); got != c.overlap {
			t.Errorf("overlap(%s, %s) = %v", c.a, c.b, got)
		}
		if got := patternWithin(a, b); got != c.a2b {
			t.Errorf("within(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// TestFolderRoutesRegistered: a route decorator on a page's type, the way
// earlier releases declared a page, is E0720: the page is its file under
// pages/, its path its URL.
func TestFolderRoutesRegistered(t *testing.T) {
	routesModule(t, map[string]string{
		"pages/orgs/[org]/permits/[permit].vuka.html": "",
		"pages/orgs/[org]/permits/[permit].vuka":      "package permits\n\nimport (\n\t\"github.com/vuka-lang/ui/v2\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\n@web.Live(\"/permit\")\ntype Permit struct {\n\tui.Live\n\tOrg    int\n\tPermit string\n}\n\nfunc NewPermit() *Permit { return &Permit{} }\n\nfunc (x *Permit) Render() ui.Node { return ui.Text(\"\") }\n",
	})
	var out bytes.Buffer
	err := gen([]string{"-inplace"}, &out)
	if err == nil || !strings.Contains(err.Error(), "@web.Live declares a page outside pages/") {
		t.Fatalf("vuka gen -inplace: want E0720, got %v\n%s", err, out.Bytes())
	}
}

// TestShapePages: a .vuka.html file in the 2.0 shape declares its page's
// type, so it is an ordinary page — static, or live by an event its markup
// sends — named by its file; one without a constructor is built in
// route.go, and nothing is written into its folder.
func TestShapePages(t *testing.T) {
	app := routesModule(t, map[string]string{
		"pages/about.vuka.html":   "package pages\n\n// Info is declared above its markup.\ntype Info struct {\n\tVersion string\n}\n\nfunc (p *Info) Title() string { return \"About\" }\n<article><h1>About {Version}</h1></article>\n",
		"pages/counter.vuka.html": "package pages\n\n// Counter is live: its markup sends Inc.\ntype Counter struct {\n\tn int\n}\n\nfunc (p *Counter) Inc() { p.n++ }\n<button onClick={Inc}>{n}</button>\n",
	})
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	if !regexp.MustCompile(`GET +/about +pages\.Info `).MatchString(out.String()) || !strings.Contains(out.String(), "pages.Counter (live)") {
		t.Errorf("vuka routes:\n%s", out.Bytes())
	}
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(app, "pages", "route.go"))
	if !strings.Contains(string(b), `app.Live("/counter", func() *Counter { return &Counter{} }`) || !strings.Contains(string(b), `app.Get("/about", func() *Info { return &Info{} }`) {
		t.Errorf("route.go:\n%s", b)
	}
	if u, _ := os.ReadFile(filepath.Join(app, "pages", "urls", "urls_gen.go")); !regexp.MustCompile(`About += Root\.About`).Match(u) {
		t.Errorf("urls_gen.go names the page by its type, not its file:\n%s", u)
	}
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
}

// TestGeneratedConstructors: a page or endpoint without New<T> is built in
// route.go; one declared by hand is used, and a type registered another way
// gets none.
func TestGeneratedConstructors(t *testing.T) {
	app := routesModule(t, map[string]string{
		"pages/files/new.go": "package files\n\n// NewFile is declared by hand.\nfunc NewFile() *File { return &File{} }\n",
	})
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(app, "pages/route.go"))
	for _, want := range []string{`app.Get("/files/{file...}", files.NewFile,`, `web.Receiver(func() *api_orgs.Index { return &api_orgs.Index{} })`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("route.go has no %s:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "Card") || strings.Contains(string(b), "Store") {
		t.Errorf("route.go builds a type that is no page:\n%s", b)
	}
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
}

// TestBracketFoldersOnDisk: vuka gen -inplace writes a [id] folder's Go
// where the go command sees it, its Go name _id, with line directives back
// to the sources, and a plain go build builds the module.
func TestBracketFoldersOnDisk(t *testing.T) {
	app := routesModule(t, nil)
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(app, "pages/orgs/param_org/index_html.go"))
	if err != nil || !strings.Contains(string(b), "//line ../[org]/index.vuka.html:") {
		t.Fatalf("pages/orgs/param_org/index_html.go: %v\n%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(app, "pages/orgs/[org]/index_html.go")); err == nil {
		t.Error("Go written into the [org] folder")
	}
	build := exec.Command("go", "vet", "./...", "./pages/orgs/param_org/...")
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Errorf("go vet after gen -inplace: %v\n%s", err, out)
	}
}

// TestRoutesUnserved: a main that never passes pages.Routes() to its app
// serves none of the folder routes, which vuka routes and vuka vet say
// (W0701); one that does, under any import name, is quiet.
func TestRoutesUnserved(t *testing.T) {
	for _, c := range []struct{ main, want string }{
		{"package main\n\nimport _ \"example.com/app/pages\"\n\nfunc main() {}\n", "W0701"},
		{"package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\n\tp \"example.com/app/pages\"\n)\n\nfunc main() { web.New(p.Routes()) }\n", ""},
	} {
		t.Run(c.want, func(t *testing.T) {
			routesModule(t, map[string]string{"main.go": c.main})
			var out, errOut bytes.Buffer
			if err := routesCmd(nil, &out, &errOut); err != nil {
				t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
			}
			if got := strings.Contains(errOut.String(), "W0701"); got != (c.want != "") {
				t.Errorf("main %q: W0701 %v:\n%s", c.main, got, errOut.Bytes())
			}
		})
	}
}

// TestMainPages: a pages/ directory beside a package main is that main's
// folder routes, beside the module's own: each tree gets its route.go and
// urls, with paths from its own pages/, and W0701 checks the main beside
// it alone. A pages/ directory beside no main is no tree. The module's
// pages/ here is the fixture's, one page in place of its tree.
func TestMainPages(t *testing.T) {
	page := func(pkg, typ, text string) string {
		return "package " + pkg + "\n\ntype " + typ + " struct{}\n<p>" + text + "</p>\n"
	}
	app := routesModule(t, map[string]string{
		"cmd/a/main.go":                   "package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/app/cmd/a/pages\"\n)\n\nfunc main() { web.New(pages.Routes()) }\n",
		"cmd/a/pages/index.vuka.html":     page("pages", "Home", "a"),
		"cmd/a/pages/pets/[id].vuka.html": "package pets\n\nimport \"example.com/app/cmd/a/pages/urls\"\n\ntype Pet struct {\n\tID int\n}\n<a href={urls.Pets.ID(ID).Path()}>{ID}</a>\n",
		"cmd/b/main.go":                   "package main\n\nimport _ \"example.com/app/cmd/b/pages\"\n\nfunc main() {}\n",
		"cmd/b/pages/about.vuka.html":     page("pages", "About", "b"),
		"lib/pages/index.vuka.html":       page("pages", "Lib", "no main"),
	})
	os.RemoveAll(filepath.Join(app, "pages"))
	writeFile(t, filepath.Join(app, "pages", "about.vuka.html"), page("pages", "Info", "the module's"))
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	for _, want := range []string{`GET +/ +pages\.Home `, `GET +/pets/\{id\} +pets\.Pet `, `GET +/about +pages\.About `, `GET +/about +pages\.Info `} {
		if !regexp.MustCompile(want).MatchString(out.String()) {
			t.Errorf("vuka routes has no %s:\n%s", want, out.Bytes())
		}
	}
	if strings.Contains(out.String(), "pages.Lib") {
		t.Errorf("a pages/ beside no main is routed:\n%s", out.Bytes())
	}
	if w := regexp.MustCompile(`warning\[W0701\].*`).FindAllString(errOut.String(), -1); len(w) != 1 || !strings.Contains(w[0], "cmd/b/pages/") {
		t.Errorf("W0701 is for cmd/b alone: %q\n%s", w, errOut.Bytes())
	}
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	for f, want := range map[string]string{
		"pages/route.go":               "package pages",
		"cmd/a/pages/route.go":         `app.Get("/pets/{id}"`,
		"cmd/a/pages/urls/urls_gen.go": "package urls",
		"cmd/b/pages/route.go":         `app.Get("/about"`,
	} {
		if b, _ := os.ReadFile(filepath.Join(app, f)); !strings.Contains(string(b), want) {
			t.Errorf("%s has no %s:\n%s", f, want, b)
		}
	}
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
}

// TestBracketFolderGoDirs: a [id] folder's package is seen by the go
// command in param_id, its Go in the overlay; vuka test and vet, whose
// tools run in a package's directory, make it for the while and remove it,
// so none is left on disk. One whose [id] folder is gone, holding
// generated Go alone, is removed.
func TestBracketFolderGoDirs(t *testing.T) {
	app := routesModule(t, map[string]string{
		"main_test.go": "package main\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/pages\"\n)\n\nfunc TestRoutes(t *testing.T) { _ = pages.Routes() }\n",
	})
	os.RemoveAll(filepath.Join(app, "pages"))
	for name, src := range map[string]string{
		"pages/index.vuka.html":              "package pages\n\ntype Home struct{}\n<p>home</p>\n",
		"pages/pets/[id]/_layout.vuka.html":  "package pet\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Layout struct {\n\tID       int\n\tChildren ui.Node\n}\n<article data-id={ID}>{Children}</article>\n",
		"pages/pets/[id]/index.vuka.html":    "package pet\n\ntype Index struct {\n\tID int\n}\n<p>pet {ID}</p>\n",
		"pages/pets/[id]/edit.vuka.html":     "package pet\n\nimport \"example.com/app/pages/urls\"\n\ntype Edit struct {\n\tID int\n}\n<a href={urls.Pets.ID(ID).Path()}>back</a>\n",
		"pages/old/param_gone/index_vuka.go": "// Code generated by vuka from index.vuka. DO NOT EDIT.\n\npackage gone\n",
	} {
		writeFile(t, filepath.Join(app, name), src)
	}
	var errOut bytes.Buffer
	for _, cmd := range []string{"test", "vet"} {
		errOut.Reset()
		if err := runGo(cmd, []string{"./..."}, io.Discard, &errOut); err != nil {
			t.Errorf("vuka %s ./...: %v\n%s", cmd, err, errOut.Bytes())
		}
	}
	noGoDirs(t, app)
	if _, err := os.Stat(filepath.Join(app, "pages/old/param_gone")); err == nil {
		t.Error("the stale pages/old/param_gone is still there")
	}
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "pages/pets/param_id/index_html.go")); err != nil {
		t.Errorf("vuka gen -inplace: %v", err)
	}
	if err := gen([]string{"-inplace", "-check"}, io.Discard); err != nil {
		t.Errorf("vuka gen -inplace -check: %v", err)
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet with the files on disk: %v\n%s", err, errOut.Bytes())
	}
}

// TestPageHandlerDeps: a page's method handler takes a provided dependency
// as a pointer beside the struct its body binds; an event its markup sends
// can't be named for an HTTP method.
func TestPageHandlerDeps(t *testing.T) {
	routesModule(t, map[string]string{
		"pages/pets/store.go":        "package pets\n\n// Store keeps the pets.\ntype Store struct{ Names []string }\n\n// PetForm is a new pet.\ntype PetForm struct {\n\tName string `form:\"name\"`\n}\n",
		"pages/pets/index.vuka.html": "package pets\n\nimport \"context\"\n\n// Index lists the pets.\ntype Index struct{}\n\nfunc (p *Index) Post(ctx context.Context, s *Store, form PetForm) error { return nil }\n<form method=\"post\"><input name=\"name\"/></form>\n",
	})
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil || !regexp.MustCompile(`POST +/pets +pets\.Index\.Post`).Match(out.Bytes()) {
		t.Fatalf("vuka routes: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	writeFile(t, "pages/pets/index.vuka.html", "package pets\n\n// Index lists the pets.\ntype Index struct {\n\tn int\n}\n\nfunc (p *Index) Delete() { p.n-- }\n<button onClick={Delete}>-</button>\n")
	out.Reset()
	errOut.Reset()
	if err := routesCmd(nil, &out, &errOut); err == nil || !strings.Contains(errOut.String(), "E0709") || !strings.Contains(errOut.String(), "Index.Delete is the page's DELETE handler, and its markup sends it as an event") {
		t.Errorf("vuka routes with an event named Delete: %v\n%s", err, errOut.Bytes())
	}
}

// TestLayoutPageReads: a layout reading the page's title in its Load loads
// on every navigation (web.ReloadLayout); one reading it in its markup,
// which renders again for each page, stays.
func TestLayoutPageReads(t *testing.T) {
	app := routesModule(t, map[string]string{
		"pages/admin/_layout.vuka.html": "package admin\n\nimport (\n\t\"context\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n)\n\n// Shell stores the title as it loads.\ntype Shell struct {\n\tChildren ui.Node\n\ttitle    string\n}\n\nfunc (l *Shell) Load(ctx context.Context) error {\n\tl.title = heading(ctx)\n\treturn nil\n}\n\nfunc heading(ctx context.Context) string { return ui.PageTitle(ctx) }\n<section><h1>{title}</h1>{Children}</section>\n",
		"pages/admin/index.vuka.html":   "package admin\n\n// Index is the admin area.\ntype Index struct{}\n<p>admin</p>\n",
		"pages/shop/_layout.vuka.html":  "package shop\n\nimport (\n\t\"context\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n)\n\n// Shell reads the title as it renders.\ntype Shell struct {\n\tChildren ui.Node\n}\n\nfunc (l *Shell) Load(ctx context.Context) error { return nil }\n\nfunc (l *Shell) Heading(ctx context.Context) string { return ui.PageTitle(ctx) }\n<section><h1>{Heading(ctx)}</h1>{Children}</section>\n",
		"pages/shop/index.vuka.html":    "package shop\n\n// Index is the shop.\ntype Index struct{}\n<p>shop</p>\n",
	})
	var out bytes.Buffer
	if err := gen([]string{"-inplace"}, &out); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out.Bytes())
	}
	b, _ := os.ReadFile(filepath.Join(app, "pages/route.go"))
	if !strings.Contains(string(b), "web.ReloadLayout[admin.Shell]()") {
		t.Errorf("the layout reading the title in Load doesn't reload:\n%s", b)
	}
	if strings.Contains(string(b), "web.ReloadLayout[shop.Shell]()") {
		t.Errorf("the layout reading the title in its markup reloads:\n%s", b)
	}
}
