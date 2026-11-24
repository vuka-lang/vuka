package load

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// fanModule writes a module shaped like an app's page tree: n independent
// Vuka packages that all import one shared Vuka package, two of them with
// an error, and returns its root.
func fanModule(t *testing.T, n int) (root string) {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	root = filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\n"+
		"replace github.com/vuka-lang/vuka/v2 => "+repo+"\n")
	write(t, filepath.Join(root, "lib", "lib.vuka"), "package lib\n\nfunc Title() Result[string] { return Ok(\"t\") }\n")
	var uses strings.Builder
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("package p%02d\n\nimport \"example.com/app/lib\"\n\nfunc V() Result[string] { s := lib.Title()?\n\treturn Ok(s + %q) }\n", i, fmt.Sprint(i))
		if i == 7 || i == 21 {
			body = fmt.Sprintf("package p%02d\n\nfunc V() int { return 1? }\n", i)
		}
		write(t, filepath.Join(root, fmt.Sprintf("p%02d", i), "page.vuka"), body)
		fmt.Fprintf(&uses, "\t_ \"example.com/app/p%02d\"\n", i)
	}
	write(t, filepath.Join(root, "main.vuka"), "package main\n\nimport (\n\t\"fmt\"\n\n"+uses.String()+")\n\nfunc main() { fmt.Println(\"ok\") }\n")
	return root
}

// TestTranspileDeterministic transpiles the fan module twice from scratch:
// the packages run concurrently, so two runs must still generate the same
// bytes and report the same errors in the same order.
func TestTranspileDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := fanModule(t, 24)
	run := func() ([]Generated, []string) {
		pkgs, err := Discover(root, "example.com/app", root, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		gens, _, err := Transpile(pkgs, t.TempDir(), Options{})
		var list transpile.ErrorList
		if !errors.As(err, &list) {
			t.Fatalf("want the two packages' errors, got %v", err)
		}
		var errs []string
		for _, e := range list {
			errs = append(errs, e.Error())
		}
		return gens, errs
	}
	gens1, errs1 := run()
	gens2, errs2 := run()
	if len(gens1) != len(gens2) {
		t.Fatalf("one run generated %d files, the other %d", len(gens1), len(gens2))
	}
	for i := range gens1 {
		if gens1[i].Target != gens2[i].Target {
			t.Fatalf("file %d: %s, then %s", i, gens1[i].Target, gens2[i].Target)
		}
		if string(gens1[i].Src) != string(gens2[i].Src) {
			t.Fatalf("%s: the runs generated different bytes", gens1[i].Target)
		}
	}
	if fmt.Sprint(errs1) != fmt.Sprint(errs2) {
		t.Fatalf("the runs' errors differ:\n%v\n%v", errs1, errs2)
	}
	var at []int
	for i, e := range errs1 {
		if strings.Contains(e, "p07") || strings.Contains(e, "p21") {
			at = append(at, i)
		}
	}
	if len(at) < 2 || !strings.Contains(errs1[at[0]], "p07") {
		t.Fatalf("errors aren't in import order: %v", errs1)
	}
}

// TestRunDeps checks the run's schedule: the fan module's pages depend
// only on lib, never on each other, so they transpile concurrently; main,
// importing every page, comes last.
func TestRunDeps(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := fanModule(t, 6) // below the broken two: every page is whole
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pkgs, err = Order(pkgs); err != nil {
		t.Fatal(err)
	}
	deps := runDeps(pkgs)
	lib, main := -1, -1
	for i, p := range pkgs {
		switch p.ImportPath {
		case "example.com/app/lib":
			lib = i
		case "example.com/app":
			main = i
		}
	}
	for i, p := range pkgs {
		switch {
		case i == lib:
			if len(deps[i]) != 0 {
				t.Errorf("lib depends on %v", deps[i])
			}
		case i == main:
			if len(deps[i]) != 6 {
				t.Errorf("main depends on %d packages, want its 6 pages", len(deps[i]))
			}
		default:
			if len(deps[i]) != 1 || deps[i][0] != lib {
				t.Errorf("%s depends on %v, want lib alone", p.ImportPath, deps[i])
			}
		}
	}
}

// factsRun is what one run gave: its files, its errors, and the facts Got
// was given, in the order it was.
type factsRun struct {
	gens  []Generated
	errs  []string
	got   []string
	facts map[string]string
}

// factsOptions are Options asking each package for its scope: every name it
// declares and its type, as the run's Go type-checks.
func factsOptions(r *factsRun) Options {
	var mu sync.Mutex
	r.facts = map[string]string{}
	return Options{
		Facts: func(p *Package, check func(skip ...string) *Typed) []byte {
			ty := check()
			if ty == nil {
				return nil
			}
			var b strings.Builder
			for _, name := range ty.Pkg.Scope().Names() {
				fmt.Fprintf(&b, "%s %s\n", name, ty.Pkg.Scope().Lookup(name).Type())
			}
			return []byte(b.String())
		},
		Got: func(p *Package, facts []byte) {
			mu.Lock()
			defer mu.Unlock()
			r.got = append(r.got, p.ImportPath)
			r.facts[p.ImportPath] = string(facts)
		},
	}
}

// TestTranspileSequentialMatches transpiles the fan module one package at a
// time (one P, so one worker) and concurrently: the two must generate the
// same bytes, report the same errors in the same order, and give Got the
// same facts in import order. A session, taking every whole package from
// a run that asked for no facts, asks them on its workers and agrees too.
func TestTranspileSequentialMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("VUKA_CACHE", t.TempDir())
	root := fanModule(t, 24)
	discover := func() []*Package {
		pkgs, err := Discover(root, "example.com/app", root, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		return pkgs
	}
	errsOf := func(err error) []string {
		var list transpile.ErrorList
		if !errors.As(err, &list) {
			t.Fatalf("want the two packages' errors, got %v", err)
		}
		var errs []string
		for _, e := range list {
			errs = append(errs, e.Error())
		}
		return errs
	}
	run := func(procs int, sess *Session) *factsRun {
		defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(procs))
		r := &factsRun{}
		opts := factsOptions(r)
		var err error
		if sess != nil {
			r.gens, _, err = sess.Transpile(discover(), t.TempDir(), opts)
		} else {
			r.gens, _, err = Transpile(discover(), t.TempDir(), opts)
		}
		r.errs = errsOf(err)
		return r
	}
	same := func(name string, a, b *factsRun) {
		t.Helper()
		if len(a.gens) != len(b.gens) {
			t.Fatalf("%s: %d files, then %d", name, len(a.gens), len(b.gens))
		}
		for i := range a.gens {
			if a.gens[i].Target != b.gens[i].Target || string(a.gens[i].Src) != string(b.gens[i].Src) {
				t.Fatalf("%s: %s: the runs generated different bytes", name, a.gens[i].Target)
			}
		}
		if !slices.Equal(a.errs, b.errs) {
			t.Fatalf("%s: the runs' errors differ:\n%v\n%v", name, a.errs, b.errs)
		}
		if !slices.Equal(a.got, b.got) {
			t.Fatalf("%s: Got was given\n%v\nthen\n%v", name, a.got, b.got)
		}
		for path, f := range a.facts {
			if b.facts[path] != f {
				t.Fatalf("%s: %s's facts differ:\n%s\n---\n%s", name, path, f, b.facts[path])
			}
		}
	}
	seq := run(1, nil)
	par := run(max(4, runtime.NumCPU()), nil)
	same("sequential, concurrent", seq, par)
	if len(seq.got) < 20 || !strings.Contains(seq.facts["example.com/app/p03"], "V func()") {
		t.Fatalf("Got was given %v, p03's facts %q", seq.got, seq.facts["example.com/app/p03"])
	}
	for i := 1; i < len(seq.got); i++ {
		if a, b := seq.got[i-1], seq.got[i]; strings.HasPrefix(a, "example.com/app/p") && strings.HasPrefix(b, "example.com/app/p") && a > b {
			t.Fatalf("Got isn't in import order: %v", seq.got)
		}
	}

	s, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Transpile(discover(), t.TempDir(), Options{}); err == nil {
		t.Fatal("want the two packages' errors")
	}
	kept := run(max(4, runtime.NumCPU()), s)
	if s.ran.fellBack || s.ran.reused == 0 {
		t.Fatalf("the session took nothing from its last run: %+v", s.ran)
	}
	same("sequential, a session asking kept output", seq, kept)
	same("sequential, a session with the facts kept", seq, run(max(4, runtime.NumCPU()), s))
}
