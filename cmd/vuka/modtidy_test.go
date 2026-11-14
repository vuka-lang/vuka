package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// tidyEnv is the environment of a test's vuka mod tidy: offline, the
// module's own go.mod, no workspace.
func tidyEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	chdir(t, dir)
}

// treeFiles lists the files under dir, relative to it.
func treeFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func readMod(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func appendMod(t *testing.T, dir, more string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"), readMod(t, dir)+more)
}

// requires reports whether go.mod has a require directive for path.
func requires(mod, path string) bool {
	return slices.ContainsFunc(strings.Split(mod, "\n"), func(l string) bool {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "require "))
		return len(f) >= 2 && f[0] == path
	})
}

// sameTree fails when running changed the files of the tree, other than
// go.mod, go.sum and the keep-file (modkeep_test.go tests that).
func sameTree(t *testing.T, dir string, before []string) {
	t.Helper()
	after := slices.DeleteFunc(treeFiles(t, dir), func(f string) bool { return f == filepath.Join(keepDir, keepName) })
	if !slices.Equal(before, after) {
		t.Errorf("the module's files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// localLib writes a module example.com/<name> beside dir, a package of one
// function, and returns its directory.
func localLib(t *testing.T, dir, name, src string) string {
	t.Helper()
	lib := filepath.Join(filepath.Dir(dir), name)
	writeFile(t, filepath.Join(lib, "go.mod"), "module example.com/"+name+"\n\ngo 1.24\n")
	writeFile(t, filepath.Join(lib, name+".go"), src)
	return lib
}

// TestModTidyCopy: what only a .vuka file imports is kept and, required
// nowhere, added — from a module a relative replace directive names; what
// nothing imports is removed; a tool's requirement stays. go.mod is the
// only file written, the module's tree is as it was, and vuka mod tidy
// -check then passes.
func TestModTidyCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{
		"main.vuka": `package main

import (
	"fmt"

	"example.com/greet"
)

func hello() Result[string] { return Ok(greet.Hello()) }

func main() { fmt.Println(hello().Unwrap()) }
`,
		"helper.go": "package main\n\nfunc helper() {}\n",
		// A nested module: its imports aren't this module's.
		"tools/go.mod": "module example.com/tools\n\ngo 1.24\n",
		"tools/x.go":   "package tools\n\nimport _ \"example.com/unused\"\n",
	})
	localLib(t, dir, "greet", "package greet\n\nfunc Hello() string { return \"hi\" }\n")
	localLib(t, dir, "unused", "package unused\n")
	localLib(t, dir, "tool", "package main\n\nfunc main() {}\n")
	appendMod(t, dir, "\nrequire (\n\texample.com/unused v0.0.0\n\texample.com/tool v0.0.0\n)\n\ntool example.com/tool\n\n"+
		"replace example.com/greet => ../greet\n\nreplace example.com/unused => ../unused\n\nreplace example.com/tool => ../tool\n")
	tidyEnv(t, dir)
	before := treeFiles(t, dir)

	var stdout, stderr bytes.Buffer
	if err := modTidy(nil, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	got := readMod(t, dir)
	for _, want := range []string{"example.com/greet", "example.com/tool", "github.com/vuka-lang/vuka/v2"} {
		if !requires(got, want) {
			t.Errorf("go.mod doesn't require %s:\n%s", want, got)
		}
	}
	if requires(got, "example.com/unused") {
		t.Errorf("go.mod still requires example.com/unused:\n%s", got)
	}
	if !strings.Contains(got, "replace example.com/greet => ../greet") {
		t.Errorf("the replace directive changed:\n%s", got)
	}
	for _, want := range []string{"added   example.com/greet ", "removed example.com/unused v0.0.0"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the output doesn't say %q:\n%s", want, stdout.String())
		}
	}
	sameTree(t, dir, before)

	stdout.Reset()
	stderr.Reset()
	if err := modTidy([]string{"-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("-check of a tidy module: %v\n%s", err, stderr.String())
	}
	var out bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &out); err != nil {
		t.Fatalf("vuka build after vuka mod tidy: %v\n%s", err, out.String())
	}
}

// TestModTidyCheck: -check fails on an untidy module, saying what would
// change, and writes nothing.
func TestModTidyCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{
		"main.vuka": "package main\n\nfunc main() {}\n",
	})
	localLib(t, dir, "unused", "package unused\n")
	appendMod(t, dir, "\nrequire example.com/unused v0.0.0\n\nreplace example.com/unused => ../unused\n")
	tidyEnv(t, dir)
	mod := readMod(t, dir)
	sum, _ := os.ReadFile(filepath.Join(dir, "go.sum"))
	before := treeFiles(t, dir)

	var stdout, stderr bytes.Buffer
	err := modTidy([]string{"-check"}, &stdout, &stderr)
	if st, ok := err.(exitStatus); !ok || st != 1 {
		t.Fatalf("-check of an untidy module: %v, want exit status 1\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "isn't tidy") && !strings.Contains(stderr.String(), "aren't tidy") ||
		!strings.Contains(stderr.String(), "removed example.com/unused v0.0.0") {
		t.Errorf("-check says:\n%s", stderr.String())
	}
	if got := readMod(t, dir); got != mod {
		t.Errorf("-check wrote go.mod:\n%s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "go.sum")); !bytes.Equal(got, sum) {
		t.Error("-check wrote go.sum")
	}
	sameTree(t, dir, before)
}

// TestModTidyORM: the orm stays required in a module whose models and
// their readers are .vuka files, and whose generated typed fields the
// build needs: after vuka mod tidy, vuka build passes. It fetches through
// GOPROXY what tidy reads and no build does.
func TestModTidyORM(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	orm := ormDir(t)
	fs := ormColumnFixtures(orm, map[string]string{
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/accounts\"\n)\n\nfunc main() { fmt.Println(accounts.Field()) }\n",
		"entities/user.vuka": `package entities

import "github.com/vuka-lang/orm/v2"

type User struct {
	orm.Base @orm.Meta{Table: "users"}
	ID    int64  @orm.PK
	Email string @orm.Char{Max: 120}
}
`,
		"accounts/accounts.vuka": `package accounts

import "example.com/app/entities"

func Field() string { return entities.UserFields.Email.Name() }
`,
	})
	root := localModule(t, webCheckout(t), fs)
	withModule(t, root, ormModule, orm)
	// tidy resolves the imports of the orm's tests too, its database
	// drivers, which no build here fetches: the job's GOPROXY is kept.
	chdir(t, root)
	before := treeFiles(t, root)
	var copied []string // the copy's Go files under entities/
	tidyCopyHook = func(dir string) {
		for _, f := range treeFiles(t, filepath.Join(dir, "entities")) {
			if src, _ := os.ReadFile(filepath.Join(dir, "entities", f)); bytes.Contains(src, []byte("type UserFields struct")) {
				copied = append(copied, f)
			}
		}
	}
	t.Cleanup(func() { tidyCopyHook = nil })

	var stdout, stderr bytes.Buffer
	if err := modTidy(nil, &stdout, &stderr); err != nil {
		if strings.Contains(err.Error(), offlineHint) {
			needs(t, "the orm's modules can't all be fetched: %v\n%s", err, stderr.String())
		}
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if len(copied) == 0 {
		t.Error("the copy go mod tidy ran in has no Go declaring the typed fields")
	}
	if !strings.Contains(stderr.String(), "go get -tool "+ormModule+"/cmd/orm") {
		t.Errorf("no note on the orm tool's go run:\n%s", stderr.String())
	}
	got := readMod(t, root)
	for _, want := range []string{ormModule, "github.com/vuka-lang/vuka/v2"} {
		if !requires(got, want) {
			t.Errorf("go.mod doesn't require %s:\n%s", want, got)
		}
	}
	sameTree(t, root, before)
	var out bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &out); err != nil {
		t.Fatalf("vuka build after vuka mod tidy: %v\n%s\ngo.mod:\n%s", err, out.String(), got)
	}
}

// TestModTidyNeeded: a requirement tidy would drop that the generated Go
// imports from is refused, go.mod left as it was.
func TestModTidyNeeded(t *testing.T) {
	t.Parallel()
	mod := []byte("module example.com/app\n\ngo 1.24\n\nrequire example.com/lib v1.0.0\n")
	r := &tidyResult{root: "/app", origMod: mod, newMod: []byte("module example.com/app\n\ngo 1.24\n"),
		gens: []load.Generated{{Target: "/app/x_vuka.go", Source: "/app/x.vuka", Src: []byte("package app\n\nimport \"example.com/lib/sub\"\n")}}}
	if err := r.needed(); err == nil || !strings.Contains(err.Error(), "example.com/lib, which x.vuka still needs") {
		t.Errorf("needed: %v", err)
	}
	r.gens[0].Src = []byte("package app\n\nimport \"example.com/app/sub\"\n")
	if err := r.needed(); err != nil {
		t.Errorf("needed, importing the module's own package: %v", err)
	}
	r.origMod = append(mod, "\ntool example.com/lib/cmd/x\n"...)
	if err := r.needed(); err == nil || !strings.Contains(err.Error(), "a tool directive") {
		t.Errorf("needed, a tool's module dropped: %v", err)
	}
}

// TestModTidyBroken: a .vuka file that doesn't transpile still has its
// imports required, with a warning naming it.
func TestModTidyBroken(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{
		"main.vuka": "package main\n\nimport \"example.com/greet\"\n\nfunc main() { greet.Hello( }\n",
	})
	localLib(t, dir, "greet", "package greet\n\nfunc Hello() string { return \"hi\" }\n")
	appendMod(t, dir, "\nreplace example.com/greet => ../greet\n")
	tidyEnv(t, dir)
	before := treeFiles(t, dir)
	var stdout, stderr bytes.Buffer
	if err := modTidy(nil, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if got := readMod(t, dir); !requires(got, "example.com/greet") {
		t.Errorf("go.mod doesn't require example.com/greet:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "don't transpile") || !strings.Contains(stderr.String(), "main.vuka") {
		t.Errorf("no warning naming main.vuka:\n%s", stderr.String())
	}
	sameTree(t, dir, before)
}

// TestModTidyTestDepsHint: tidy failing on a module it can't fetch, here
// one only a dependency's tests import, says how to get it.
func TestModTidyTestDepsHint(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{"main.vuka": "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.F() }\n"})
	dep := localLib(t, dir, "dep", "package dep\n\nfunc F() {}\n")
	writeFile(t, filepath.Join(dep, "dep_test.go"), "package dep\n\nimport _ \"example.com/nowhere/pkg\"\n")
	writeFile(t, filepath.Join(dep, "go.mod"), "module example.com/dep\n\ngo 1.24\n\nrequire example.com/nowhere v1.0.0\n")
	appendMod(t, dir, "\nrequire example.com/dep v0.0.0\n\nreplace example.com/dep => ../dep\n")
	tidyEnv(t, dir)
	mod := readMod(t, dir)
	var stdout, stderr bytes.Buffer
	err := modTidy(nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "go mod download all") {
		t.Fatalf("want the hint, got %v\n%s", err, stderr.String())
	}
	if got := readMod(t, dir); got != mod {
		t.Errorf("a failed tidy wrote go.mod:\n%s", got)
	}
}

// TestDeclaresTool: a tool directive counts on its own line or in a block.
func TestDeclaresTool(t *testing.T) {
	t.Parallel()
	const tool = "github.com/vuka-lang/orm/v2/cmd/orm"
	for name, mod := range map[string]string{
		"line":  "module example.com/app\n\ngo 1.24\n\ntool " + tool + "\n",
		"block": "module example.com/app\n\ngo 1.24\n\ntool (\n\t" + tool + "\n\tgithub.com/vuka-lang/vuka/cmd/vuka/v2\n)\n",
	} {
		if !declaresTool([]byte(mod), tool) {
			t.Errorf("%s: the tool isn't seen", name)
		}
	}
	if declaresTool([]byte("module example.com/app\n\ngo 1.24\n"), tool) {
		t.Error("a go.mod with no tool declares one")
	}
}

// TestModTidyChangesReport: requirements moving to or from // indirect, and
// go.sum's lines, are reported, so -check says why a module isn't tidy.
func TestModTidyChangesReport(t *testing.T) {
	t.Parallel()
	r := tidyResult{
		origMod: []byte("module example.com/app\n\ngo 1.24\n\nrequire (\n\texample.com/a v1.0.0 // indirect\n\texample.com/b v1.0.0\n)\n"),
		newMod:  []byte("module example.com/app\n\ngo 1.24\n\nrequire (\n\texample.com/a v1.0.0\n\texample.com/b v1.0.0 // indirect\n)\n"),
		origSum: []byte("example.com/a v1.0.0 h1:x=\nexample.com/old v1.0.0 h1:y=\nexample.com/old v1.0.0/go.mod h1:z=\n"),
		newSum:  []byte("example.com/a v1.0.0 h1:x=\nexample.com/b v1.0.0 h1:w=\n"),
	}
	got := strings.Join(r.changes(), "\n")
	for _, want := range []string{"direct  example.com/a", "indirect example.com/b", "go.sum  1 lines added, 2 removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}
