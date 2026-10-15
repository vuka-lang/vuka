package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// sessionModule writes a module: main imports the Vuka package lib and the
// Go package plain; lib imports the directory-replaced dependency dep.
func sessionModule(t *testing.T) (root string) {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "dep", "go.mod"), "module example.com/dep\n\ngo 1.23\n")
	write(t, filepath.Join(dir, "dep", "dep.go"), "package dep\n\nfunc N() int { return 1 }\n")
	root = filepath.Join(dir, "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/vuka-lang/vuka v0.0.0\n\texample.com/dep v0.0.0\n)\n\n"+
		"replace github.com/vuka-lang/vuka => "+repo+"\n\nreplace example.com/dep => ../dep\n")
	write(t, filepath.Join(root, "main.vuka"), `package main

import (
	"fmt"

	"example.com/app/lib"
	"example.com/app/plain"
)

func run() Result[int] {
	n := lib.Count()?
	return Ok(n + plain.K)
}

func main() { fmt.Println(run()) }
`)
	write(t, filepath.Join(root, "lib", "lib.vuka"), `package lib

import "example.com/dep"

func Count() (int, error) { return dep.N(), nil }

func helper() int { return 1 }
`)
	write(t, filepath.Join(root, "plain", "plain.go"), "package plain\n\nconst K = 2\n")
	return root
}

// sessionRun transpiles the module at root with s and from scratch, fails
// the test when they differ, and returns main.vuka's generated Go and the
// errors.
func sessionRun(t *testing.T, s *Session, root string) (string, []string) {
	t.Helper()
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _, gotErr := s.Transpile(pkgs, t.TempDir(), Options{})
	want, _, wantErr := Transpile(pkgs, t.TempDir(), Options{})
	if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		t.Fatalf("errors: session\n%v\nTranspile\n%v", gotErr, wantErr)
	}
	if len(got) != len(want) {
		t.Fatalf("session made %d files, Transpile %d", len(got), len(want))
	}
	main := ""
	for i := range got {
		if got[i].Target != want[i].Target || string(got[i].Src) != string(want[i].Src) {
			t.Fatalf("%s: the session's Go differs from Transpile's:\n%s\n---\n%s", got[i].Target, got[i].Src, want[i].Src)
		}
		if filepath.Base(got[i].Target) == "main_vuka.go" {
			main = string(got[i].Src)
		}
	}
	var errs []string
	var list transpile.ErrorList
	if errors.As(gotErr, &list) {
		for _, e := range list {
			errs = append(errs, e.Error())
		}
	}
	return main, errs
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(src), old) {
		t.Fatalf("%s has no %q", path, old)
	}
	write(t, path, strings.Replace(string(src), old, new, 1))
}

// TestSession transpiles a module again after edits, checking each run
// against Transpile from scratch: a stale package or type would make them
// differ.
func TestSession(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := sessionModule(t)
	s, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 2 {
		t.Fatalf("first run: %+v", s.ran)
	}

	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 0 || s.ran.reused != 2 || s.ran.checked != 0 {
		t.Errorf("an unchanged module: %+v", s.ran)
	}

	// A body edit in lib: lib is transpiled and checked again, main isn't
	// transpiled, its imports' API being as it was.
	lib := filepath.Join(root, "lib", "lib.vuka")
	edit(t, lib, "return 1 }", "return 2 }")
	if main, _ := sessionRun(t, s, root); main != first || s.ran.transpiled != 1 || s.ran.reused != 1 {
		t.Errorf("a body edit in lib: %+v", s.ran)
	}

	// An exported signature main uses: (int, error) becomes a Result, and
	// main's ? reads it differently.
	edit(t, lib, "func Count() (int, error) { return dep.N(), nil }", "func Count() Result[int] { return Ok(dep.N()) }")
	main, _ := sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 2 {
		t.Errorf("an exported signature: %+v", s.ran)
	}
	if main == first {
		t.Errorf("main's Go didn't follow lib.Count's new signature:\n%s", main)
	}

	// A constant's value in a Go package main imports.
	edit(t, filepath.Join(root, "plain", "plain.go"), "K = 2", "K = 3")
	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 1 {
		t.Errorf("a constant: %+v", s.ran)
	}

	// The dependency, replaced by a directory, changes its API.
	edit(t, filepath.Join(filepath.Dir(root), "dep", "dep.go"), "func N() int { return 1 }", "func N() int64 { return 1 }")
	_, errs := sessionRun(t, s, root)
	if len(errs) == 0 {
		t.Errorf("dep.N's new type isn't seen")
	}
	edit(t, filepath.Join(filepath.Dir(root), "dep", "dep.go"), "func N() int64 { return 1 }", "func N() int { return 1 }")
	sessionRun(t, s, root)
	if s.ran.fellBack {
		t.Errorf("after the dependency is mended: %+v", s.ran)
	}

	// A Go package of the module that doesn't compile: Transpile does the
	// run (go build reports it).
	edit(t, filepath.Join(root, "plain", "plain.go"), "K = 3", "K = missing")
	if sessionRun(t, s, root); !s.ran.fellBack {
		t.Errorf("a Go package that doesn't compile: %+v", s.ran)
	}
	edit(t, filepath.Join(root, "plain", "plain.go"), "K = missing", "K = 3")

	// An error in lib's Vuka source: reported as Transpile reports it, and
	// main, importing lib, says lib has errors; no go command runs for it.
	edit(t, lib, "func helper() int { return 2 }", "func helper() int { return 1? }")
	if _, errs := sessionRun(t, s, root); s.ran.fellBack || len(errs) == 0 {
		t.Errorf("an error in lib: %+v %v", s.ran, errs)
	}
	// Go's type error in lib's Vuka source, which only the Go compiler
	// reports: main's import of lib fails as go build says, so Transpile
	// does the run.
	edit(t, lib, "func helper() int { return 1? }", "func helper() int { return \"x\" }")
	if _, errs := sessionRun(t, s, root); !s.ran.fellBack || len(errs) == 0 {
		t.Errorf("a type error in lib: %+v %v", s.ran, errs)
	}
	edit(t, lib, "func helper() int { return \"x\" }", "func helper() int { return 2 }")
	if _, errs := sessionRun(t, s, root); s.ran.fellBack || len(errs) != 0 {
		t.Errorf("lib mended: %+v %v", s.ran, errs)
	}

	// An import nothing provides, as it is typed.
	mainFile := filepath.Join(root, "main.vuka")
	edit(t, mainFile, `"fmt"`, `"fmt"
	"example.com/nowhere"`)
	edit(t, mainFile, "func main() {", "func far() Result[int] {\n\tn := nowhere.F()?\n\treturn Ok(n)\n}\n\nfunc main() {")
	if _, errs := sessionRun(t, s, root); s.ran.fellBack || len(errs) == 0 {
		t.Errorf("an unresolved import: %+v %v", s.ran, errs)
	}
	edit(t, mainFile, "func far() Result[int] {\n\tn := nowhere.F()?\n\treturn Ok(n)\n}\n\n", "")
	edit(t, mainFile, `
	"example.com/nowhere"`, "")
	if _, errs := sessionRun(t, s, root); s.ran.fellBack || len(errs) != 0 {
		t.Errorf("the import removed: %+v %v", s.ran, errs)
	}

	// Stopped between packages, a run leaves the session as it was: the next
	// run reuses what the stopped one transpiled.
	edit(t, lib, "return 2 }", "return 3 }")
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if _, _, err := s.Transpile(pkgs, t.TempDir(), Options{Stop: func() bool { n++; return n > 1 }}); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped run: %v", err)
	}
	if sessionRun(t, s, root); s.ran.fellBack || s.ran.transpiled != 0 || s.ran.reused != 2 {
		t.Errorf("after a stopped run: %+v", s.ran)
	}

	// Some of the packages: those, and the packages they import, as a run
	// of all of them makes them.
	edit(t, lib, "return 3 }", "return 4 }")
	pkgs, _ = Discover(root, "example.com/app", root, true, nil)
	var libPkg *Package
	for _, p := range pkgs {
		if p.ImportPath == "example.com/app/lib" {
			libPkg = p
		}
	}
	if part := Closure(pkgs, []*Package{libPkg}); len(part) != 1 || part[0] != libPkg {
		t.Fatalf("lib's closure: %v", part)
	}
	if _, _, err := s.Transpile([]*Package{libPkg}, t.TempDir(), Options{}); err != nil || s.ran.transpiled != 1 {
		t.Fatalf("lib alone: %v %+v", err, s.ran)
	}
	if sessionRun(t, s, root); s.ran.fellBack || s.ran.transpiled != 0 || s.ran.reused != 2 {
		t.Errorf("after lib alone: %+v", s.ran)
	}
	if part := Closure(pkgs, []*Package{pkgs[0]}); len(part) != 2 {
		t.Errorf("main's closure: %d packages", len(part))
	}

	// A new file, and one removed.
	write(t, filepath.Join(root, "lib", "more.vuka"), "package lib\n\nfunc More() Option[int] { return Some(1) }\n")
	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 2 {
		t.Errorf("a new file: %+v", s.ran)
	}
	os.Remove(filepath.Join(root, "lib", "more.vuka"))
	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 2 {
		t.Errorf("a file removed: %+v", s.ran)
	}

	// go.mod starts the session over.
	edit(t, filepath.Join(root, "go.mod"), "go 1.23", "go 1.24")
	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 2 {
		t.Errorf("after go.mod changed: %+v", s.ran)
	}
}

// TestSessionDiskCache: a new session (a new vuka command) reads what an
// earlier one cached: nothing unchanged is transpiled or type-checked again;
// a change reaches the packages it should and no others; another compiler
// reads nothing; a removed file is gone from the output.
func TestSessionDiskCache(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := sessionModule(t)
	cache := t.TempDir()
	fresh := func() *Session {
		s, err := NewSession(root)
		if err != nil {
			t.Fatal(err)
		}
		s.UseCache(cache)
		return s
	}
	defer func(v string) { CompilerVersion = v }(CompilerVersion)
	CompilerVersion = "test 1"

	s := fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 2 {
		t.Fatalf("first run: %+v", s.ran)
	}
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.fellBack || s.ran.transpiled != 0 || s.ran.reused != 2 || s.ran.checked != 0 {
		t.Errorf("a new session, nothing changed: %+v", s.ran)
	}

	// A body: lib again, main (its API is the same) not.
	edit(t, filepath.Join(root, "lib", "lib.vuka"), "func helper() int { return 1 }", "func helper() int { return 2 }")
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 1 || s.ran.reused != 1 {
		t.Errorf("lib's body: %+v", s.ran)
	}
	// A type main uses: both.
	edit(t, filepath.Join(root, "lib", "lib.vuka"), "func Count() (int, error) { return dep.N(), nil }", "func Count() (int64, error) { return int64(dep.N()), nil }")
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 2 {
		t.Errorf("lib's API: %+v", s.ran)
	}
	// A Go package main imports: main again.
	edit(t, filepath.Join(root, "plain", "plain.go"), "const K = 2", "const K = 3")
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 1 || s.ran.reused != 1 {
		t.Errorf("plain's constant: %+v", s.ran)
	}

	// Another compiler: everything.
	CompilerVersion = "test 2"
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 2 || s.ran.reused != 0 {
		t.Errorf("another compiler: %+v", s.ran)
	}

	// A file added, then removed (lib's API changes: main is transpiled too).
	write(t, filepath.Join(root, "lib", "more.vuka"), "package lib\n\nfunc More() int { return 3 }\n")
	s = fresh()
	gens := sessionGens(t, s, root)
	if !hasTarget(gens, "more_vuka.go") || s.ran.transpiled != 2 {
		t.Errorf("a file added: %+v", s.ran)
	}
	os.Remove(filepath.Join(root, "lib", "more.vuka"))
	s = fresh()
	gens = sessionGens(t, s, root)
	if hasTarget(gens, "more_vuka.go") || s.ran.transpiled != 2 {
		t.Errorf("a file removed: %+v", s.ran)
	}

	// A broken cache entry is a miss.
	filepath.Walk(cache, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			os.WriteFile(p, []byte("junk"), 0o644)
		}
		return nil
	})
	s = fresh()
	sessionRun(t, s, root)
	if s.ran.transpiled != 2 {
		t.Errorf("a broken cache: %+v", s.ran)
	}
}

func sessionGens(t *testing.T, s *Session, root string) []Generated {
	t.Helper()
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	gens, _, err := s.Transpile(pkgs, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return gens
}

func hasTarget(gens []Generated, base string) bool {
	for _, g := range gens {
		if filepath.Base(g.Target) == base {
			return true
		}
	}
	return false
}
