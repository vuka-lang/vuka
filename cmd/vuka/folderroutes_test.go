package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	gomod = strings.Replace(gomod, "require (", "require (\n\tgithub.com/vuka-lang/web v0.0.0", 1) + "\nreplace github.com/vuka-lang/web => ../web\n"
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
	for _, f := range []string{"pages/pages_gen.go", "pages/orgs/routes_gen.go", "pages/orgs/org/routes_gen.go",
		"pages/orgs/org/permits/permit/routes_gen.go", "pages/files/path/routes_gen.go", "pages/routes_gen.go"} {
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
	writeFile(t, filepath.Join(app, "pages/orgs/org/edit.go"), editPage)
	writeFile(t, filepath.Join(app, "pages/orgs/org/index.go"), strings.Replace(orgIndex, "ResetPasswordURL", "EditURL", 1))
	errOut.Reset()
	if err := runGo("build", []string{"-o", os.DevNull, "."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka build with a new page: %v\n%s", err, errOut.Bytes())
	}
	out.Reset()
	if err := gen([]string{"-inplace", "-check"}, &out); err == nil || !strings.Contains(out.String(), "stale: pages/orgs/org/routes_gen.go") {
		t.Errorf("vuka gen -inplace -check with a new page: %v\n%s", err, out.Bytes())
	}

	// With nothing on disk, the overlay alone builds it.
	for _, f := range []string{"pages/pages_gen.go", "pages/orgs/routes_gen.go", "pages/orgs/org/routes_gen.go",
		"pages/orgs/org/permits/permit/routes_gen.go", "pages/files/path/routes_gen.go", "pages/routes_gen.go"} {
		os.Remove(filepath.Join(app, f))
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet from the overlay: %v\n%s", err, errOut.Bytes())
	}
}

const editPage = `package org

import (
	"context"
	"io"
)

// Edit edits the organisation.
type Edit struct{ Params }

func NewEdit() *Edit { return &Edit{} }

func (*Edit) Render(ctx context.Context, w io.Writer) error { return nil }
`

var orgIndex = func() string {
	b, _ := os.ReadFile(filepath.Join("testdata", "routes", "app", "pages", "orgs", "org", "index.go"))
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
		{"two pages on a path", map[string]string{"pages/orgs/new/index.go": "package new\n\nimport (\n\t\"context\"\n\t\"io\"\n)\n\ntype Index struct{}\n\nfunc NewIndex() *Index { return nil }\n\nfunc (*Index) Render(context.Context, io.Writer) error { return nil }\n"},
			"E0701", "pages/orgs/new/index.go:8"},
		{"parameter folders side by side", map[string]string{"pages/orgs/other/params.go": "package other\n\ntype Params struct{ Other int }\n"},
			"E0702", "pages/orgs/other/params.go:3"},
		{"ambiguous", map[string]string{"pages/orgs/new/x/params.go": "package x\n\ntype Params struct{ X int }\n",
			"pages/orgs/new/x/reset_password.go": "package x\n\nimport (\n\t\"context\"\n\t\"io\"\n)\n\ntype ResetPassword struct{}\n\nfunc NewResetPassword() *ResetPassword { return nil }\n\nfunc (*ResetPassword) Render(context.Context, io.Writer) error { return nil }\n"},
			"E0702", "pages/orgs/new/x/reset_password.go:8"},
		{"below a catch-all", map[string]string{"pages/files/path/more/index.go": "package more\n\nimport (\n\t\"context\"\n\t\"io\"\n)\n\ntype Index struct{}\n\nfunc NewIndex() *Index { return nil }\n\nfunc (*Index) Render(context.Context, io.Writer) error { return nil }\n"},
			"E0703", "pages/files/path/more/index.go:8"},
		{"a page beside a catch-all's index", map[string]string{"pages/files/path/raw.go": "package path\n\nimport (\n\t\"context\"\n\t\"io\"\n)\n\ntype Raw struct{}\n\nfunc NewRaw() *Raw { return nil }\n\nfunc (*Raw) Render(context.Context, io.Writer) error { return nil }\n"},
			"E0703", "pages/files/path/raw.go:8"},
		{"Params named unlike its folder", map[string]string{"pages/orgs/org/params.go": "package org\n\ntype Params struct{ ID int }\n"},
			"E0704", "pages/orgs/org/params.go:3"},
		{"Params not embedding the parent's", map[string]string{"pages/orgs/org/permits/permit/params.go": "package permit\n\ntype Params struct{ Permit int }\n"},
			"E0704", "pages/orgs/org/permits/permit/params.go:3"},
		{"a parameter type", map[string]string{"pages/orgs/org/params.go": "package org\n\ntype Params struct{ Org float64 }\n"},
			"E0705", "pages/orgs/org/params.go:3"},
		{"a URL function declared", map[string]string{"pages/orgs/urls.go": "package orgs\n\nfunc NewURL() string { return \"/x\" }\n"},
			"E0706", "pages/orgs/urls.go:3"},
		{"a template-only page's type taken", map[string]string{"pages/about.go": "package pages\n\nfunc About() string { return \"\" }\n"},
			"E0706", "pages/about.go:3"},
		{"a declared route", map[string]string{"main.go": "package main\n\nimport (\n\t\"github.com/vuka-lang/web\"\n\n\t_ \"example.com/app/pages\"\n)\n\nfunc main() { web.Routes.Get(\"/orgs/{id}\", nil) }\n"},
			"E0707", "pages/orgs/org/index.go:9"},
		{"a layout without Children", map[string]string{"pages/layout.go": "package pages\n\ntype Layout struct{ Body string }\n"},
			"E0708", "pages/layout.go:3"},
		{"a layout's Load", map[string]string{"pages/orgs/org/layout.go": "package org\n\nimport \"github.com/vuka-lang/ui\"\n\ntype Layout struct {\n\tParams\n\tChildren ui.Node\n}\n\nfunc (l *Layout) Load() {}\n"},
			"E0708", "pages/orgs/org/layout.go:10"},
		{"a Get method", map[string]string{"pages/orgs/get.go": "package orgs\n\nimport \"context\"\n\nfunc (*New) Get(ctx context.Context) error { return nil }\n"},
			"E0709", "pages/orgs/get.go:5"},
		{"a scalar handler parameter", map[string]string{"pages/orgs/put.go": "package orgs\n\nimport \"context\"\n\nfunc (*New) Put(ctx context.Context, name string) error { return nil }\n"},
			"E0709", "pages/orgs/put.go:5"},
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
			case "web.Routes", "web.PageRoute", "web.LayoutOf", "web.Receiver", "web.Rest":
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

// TestFolderRoutesRegistered: a type registered as a page another way (its
// constructor given to web.Routes, or a route decorator on it) isn't a
// folder page, so an app of earlier releases keeps the routes it had.
func TestFolderRoutesRegistered(t *testing.T) {
	routesModule(t, map[string]string{
		"main.go": "package main\n\nimport (\n\t\"github.com/vuka-lang/web\"\n\n\t\"example.com/app/pages/orgs\"\n)\n\nfunc main() { web.Routes.Live(\"/elsewhere\", orgs.NewIndex) }\n",
		"pages/orgs/org/permits/permit/index.vuka": "package permit\n\nimport (\n\t\"github.com/vuka-lang/ui\"\n\t\"github.com/vuka-lang/web\"\n)\n\n@web.Live(\"/permit\")\ntype Index struct {\n\tui.Live\n\tParams\n}\n\nfunc NewIndex() *Index { return &Index{} }\n\nfunc (x *Index) Render() ui.Node { return ui.Text(\"\") }\n",
	})
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	for _, page := range []string{"orgs.Index", "permit.Index"} {
		if strings.Contains(out.String(), page) {
			t.Errorf("%s is a folder page:\n%s", page, out.Bytes())
		}
	}
	if !strings.Contains(out.String(), "orgs.New") {
		t.Errorf("the other pages are gone:\n%s", out.Bytes())
	}
}

// TestTemplatePageDeclared: declaring a template-only page's type makes it
// an ordinary page, its type and constructor no longer generated.
func TestTemplatePageDeclared(t *testing.T) {
	app := routesModule(t, map[string]string{"pages/about.go": "package pages\n\n// About is declared by hand.\ntype About struct{ Version string }\n\nfunc NewAbout() *About { return &About{Version: \"1\"} }\n"})
	var out, errOut bytes.Buffer
	if err := routesCmd(nil, &out, &errOut); err != nil {
		t.Fatalf("vuka routes: %v\n%s", err, errOut.Bytes())
	}
	if !strings.Contains(out.String(), "pages.About  ") || strings.Contains(out.String(), "pages.About (template)") {
		t.Errorf("About isn't an ordinary page:\n%s", out.Bytes())
	}
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(app, "pages", "routes_gen.go"))
	if strings.Contains(string(b), "type About") || !strings.Contains(string(b), "func AboutURL() string") {
		t.Errorf("routes_gen.go:\n%s", b)
	}
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, errOut.Bytes())
	}
}
