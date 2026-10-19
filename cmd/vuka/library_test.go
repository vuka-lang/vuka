package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
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

// TestLibraryExample builds examples/library-users, plain Go, with go build
// and no vuka command, and its twin written in Vuka after vuka eject -rm:
// the code is written alike — the same web.Routes calls in an init, the
// same templates — and the two programs answer the same.
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
	files := map[string]string{}
	for _, name := range []string{"main.go", "users.go", "islands/Profile.ts", "decorated/main.vuka", "decorated/users.vuka"} {
		src, err := os.ReadFile(filepath.Join(repo, "examples/library-users", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(src)
	}
	root := localModule(t, web, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "plain"), ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if err := eject([]string{"-rm", "./decorated"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "-o", filepath.Join(bin, "decorated"), "./decorated"}, {"vet", "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}

	plain := registrations(t, "main.go", "web.Routes.")
	ejected := registrations(t, "decorated/users.go", "web.Routes.")
	if len(plain) == 0 || !slices.Equal(plain, ejected) {
		t.Errorf("registrations differ:\nplain Go:\n\t%s\nejected:\n\t%s", strings.Join(plain, "\n\t"), strings.Join(ejected, "\n\t"))
	}
	plain = registrations(t, "users.go", "ui.HTML(")
	ejected = registrations(t, "decorated/users.go", "ui.HTML(")
	if len(plain) == 0 || !slices.Equal(plain, ejected) {
		t.Errorf("templates differ:\nplain Go:\n\t%s\nejected:\n\t%s", strings.Join(plain, "\n\t"), strings.Join(ejected, "\n\t"))
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
// (web.Routes., ui.HTML(, the prefix cut: Get("/x", X,
// web.Params("id")). app.Run isn't one.
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
