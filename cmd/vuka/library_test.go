package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// TestLibraryExample builds examples/conformance/library-users/go, plain Go,
// with go build and no vuka command, and its twin written in Vuka
// (library-users/vuka) after vuka eject -rm
// (its markup then templates, users.vuka.html, compiled to users_html.go):
// the code is written alike — the same web.Routes calls in an init, the
// same templates, the same model and its Schema, the same LiveSchema
// methods — vukavet finds nothing in either, and the two programs answer
// the same.
func TestLibraryExample(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if bi, ok := debug.ReadBuildInfo(); !ok || !slices.ContainsFunc(bi.Deps, func(m *debug.Module) bool {
		return m.Path == "github.com/vuka-lang/vuka" && m.Replace != nil
	}) {
		t.Skip("needs this checkout's transpiler: go mod edit -replace github.com/vuka-lang/vuka=../..")
	}
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if src, _ := os.ReadFile(filepath.Join(web, "registry.go")); !bytes.Contains(src, []byte("var Routes = ")) {
		t.Skipf("github.com/vuka-lang/web at %s has no Routes registry; set VUKA_WEB", web)
	}
	orm := os.Getenv("VUKA_ORM")
	if orm == "" {
		orm = filepath.Join(filepath.Dir(repo), "vuka-orm")
	}
	if src, _ := os.ReadFile(filepath.Join(orm, "schema.go")); !bytes.Contains(src, []byte("func (s *Schema) Field(")) {
		t.Skipf("github.com/vuka-lang/orm at %s has no Schema; set VUKA_ORM", orm)
	}
	files := map[string]string{}
	for _, name := range []string{"main.go", "users.go", "users_gen.go", "orm_scanners_gen.go", "islands/Profile.ts", "decorated/main.vuka", "decorated/users.vuka"} {
		form, file := "go", name
		if rest, ok := strings.CutPrefix(name, "decorated/"); ok {
			form, file = "vuka", rest
		}
		src, err := os.ReadFile(filepath.Join(repo, "examples/conformance/library-users", form, file))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(src)
	}
	root := localModule(t, web, files)
	withModule(t, root, "github.com/vuka-lang/orm", orm)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	bin := t.TempDir()
	vet := exec.Command("go", "build", "-o", filepath.Join(bin, "vukavet"), ".")
	vet.Dir = filepath.Join(repo, "cmd/vukavet")
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go build vukavet: %v\n%s", err, out)
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "plain"), ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := eject([]string{"-rm", "./decorated"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "-o", filepath.Join(bin, "decorated"), "./decorated"}, {"vet", "./..."}, {"vet", "-vettool=" + filepath.Join(bin, "vukavet"), "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}

	if html, err := os.ReadFile("decorated/users.vuka.html"); err != nil || !bytes.Contains(html, []byte("func (d *Dashboard) Render()\n<main>")) {
		t.Errorf("vuka eject -rm writes the markup as templates, decorated/users.vuka.html: %s %v", html, err)
	}
	plain := registrations(t, "main.go", "web.Routes.")
	ejected := registrations(t, "decorated/users.go", "web.Routes.")
	if len(plain) == 0 || !slices.Equal(plain, ejected) {
		t.Errorf("registrations differ:\nplain Go:\n\t%s\nejected:\n\t%s", strings.Join(plain, "\n\t"), strings.Join(ejected, "\n\t"))
	}
	plain = registrations(t, "users.go", "ui.HTML(")
	ejected = registrations(t, "decorated/users_html.go", "ui.HTML(")
	if len(plain) == 0 || !slices.Equal(plain, ejected) {
		t.Errorf("templates differ:\nplain Go:\n\t%s\nejected:\n\t%s", strings.Join(plain, "\n\t"), strings.Join(ejected, "\n\t"))
	}

	if p, e := typeSrc(t, "users.go", "User"), typeSrc(t, "decorated/users.go", "User"); p == "" || p != e {
		t.Errorf("the model User differs:\nplain Go:\n%s\nejected:\n%s", p, e)
	}
	for _, m := range []string{"User.Schema", "Dashboard.LiveSchema", "Profile.LiveSchema"} {
		if p, e := methodSrc(t, "users.go", m), methodSrc(t, "decorated/users.go", m); p == "" || p != e {
			t.Errorf("%s differs:\nplain Go:\n%s\nejected:\n%s", m, p, e)
		}
	}

	plainURL, _ := serve(t, root, filepath.Join(bin, "plain"))
	ejectedURL, _ := serve(t, root, filepath.Join(bin, "decorated"))
	// A live page's join token and a page's signature are the process's;
	// the island bundle and its stylesheet are what the vuka command builds
	// (vuka build, or go tool vuka build-islands), which neither binary has.
	volatile := regexp.MustCompile(`data-vk-(token|sig|session)="[^"]*"|<link rel="stylesheet" href="/_vuka/app\.css\?v=[^"]*">|<script src="/_vuka/c/loader\.js\?v=[^"]*" defer></script>`)
	for _, path := range []string{"/api/users", "/api/users/2", "/api/users/9", "/", "/users/2", "/users/9"} {
		var bodies [2]string
		var codes [2]int
		for i, base := range []string{plainURL, ejectedURL} {
			res, err := http.Get(base + path)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			codes[i], bodies[i] = res.StatusCode, volatile.ReplaceAllString(string(b), "")
		}
		if codes[0] != codes[1] || bodies[0] != bodies[1] {
			t.Errorf("GET %s: plain Go %d\n%s\nejected %d\n%s", path, codes[0], bodies[0], codes[1], bodies[1])
		}
	}
}

// registrations are the calls of file whose text starts with prefix
// (web.Routes., ui.HTML(, the prefix cut: Get("/x/{id}", X, web.Use(mw))).
// app.Run isn't one.
func registrations(t *testing.T, file, prefix string) []string {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		text := string(src[fset.Position(call.Pos()).Offset:fset.Position(call.End()).Offset])
		if rest, ok := strings.CutPrefix(text, prefix); ok && !strings.HasPrefix(rest, "Run(") {
			out = append(out, rest)
			return false
		}
		return true
	})
	return out
}

// methodSrc is the source of file's method Type.Name, without comments.
func methodSrc(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	typ, method, _ := strings.Cut(name, ".")
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == method {
			if star, ok := fd.Recv.List[0].Type.(*ast.StarExpr); ok && types.ExprString(star.X) == typ {
				var b bytes.Buffer
				format.Node(&b, fset, fd)
				return b.String()
			}
		}
	}
	return ""
}

// typeSrc is the declaration of the type name in file, formatted.
func typeSrc(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, s := range gd.Specs {
				if ts := s.(*ast.TypeSpec); ts.Name.Name == name {
					var b bytes.Buffer
					format.Node(&b, fset, ts)
					return b.String()
				}
			}
		}
	}
	return ""
}

// withModule adds the module path, checked out at dir, to the module at
// root: required, replaced and its sums.
func withModule(t *testing.T, root, path, dir string) {
	t.Helper()
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod = append(mod, "\nrequire "+path+" v0.0.0\n\nreplace "+path+" => "+dir+"\n"...)
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	more, _ := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err := os.WriteFile(filepath.Join(root, "go.mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), append(sum, more...), 0o644); err != nil {
		t.Fatal(err)
	}
}
