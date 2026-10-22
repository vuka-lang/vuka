package main

import (
	"bytes"
	"io"
	"io/fs"
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

// TestLibraryExample builds examples/conformance/library-users/vuka, an
// app written the one way, with vuka build, then ejects it (vuka eject -rm)
// and builds it again with go build and no vuka command: the ejected Go
// passes go vet and vukavet, and the two programs answer the same.
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
	src := filepath.Join(repo, "examples/conformance/library-users/vuka")
	files := map[string]string{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(src, p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
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
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(bin, "vuka"), "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	if err := eject([]string{"-rm", "./..."}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "-o", filepath.Join(bin, "ejected"), "."}, {"vet", "./..."}, {"vet", "-vettool=" + filepath.Join(bin, "vukavet"), "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}
	filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".vuka") {
			t.Errorf("vuka eject -rm left %s", p)
		}
		return nil
	})
	if _, err := os.Stat("pages/index_html.go"); err != nil {
		t.Errorf("vuka eject -rm keeps pages/index.vuka.html compiled to pages/index_html.go: %v", err)
	}

	vukaURL, _ := serve(t, root, filepath.Join(bin, "vuka"))
	ejectedURL, _ := serve(t, root, filepath.Join(bin, "ejected"))
	// A live page's join token and a page's signature are the process's.
	volatile := regexp.MustCompile(`data-vk-(token|sig|session)="[^"]*"`)
	for _, path := range []string{"/api/users", "/api/users/2", "/api/users/9", "/", "/users/2", "/users/9"} {
		var bodies [2]string
		var codes [2]int
		for i, base := range []string{vukaURL, ejectedURL} {
			res, err := http.Get(base + path)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			codes[i], bodies[i] = res.StatusCode, volatile.ReplaceAllString(string(b), "")
		}
		if codes[0] != codes[1] || bodies[0] != bodies[1] {
			t.Errorf("GET %s: vuka build %d\n%s\nejected %d\n%s", path, codes[0], bodies[0], codes[1], bodies[1])
		}
	}
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
