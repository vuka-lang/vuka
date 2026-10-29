package transpile_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// tryModule writes the Go of the golden cases names into a module of
// their own each, under one directory.
func tryModule(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	mod, sum := goldenModule(t)
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join("testdata/golden", name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
		os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
		os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644)
	}
	return root
}

func goCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	return cmd
}

// TestTryVet: the Go every ? lowers to is what go vet takes as written by
// hand.
func TestTryVet(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet")
	}
	names := []string{"try", "try_names", "try_match", "try_anywhere", "try_context", "try_shapes", "try_main", "try_tests"}
	root := tryModule(t, names...)
	for _, name := range names {
		if out, err := goCmd(filepath.Join(root, name), "vet", ".").CombinedOutput(); err != nil {
			t.Errorf("go vet %s: %v\n%s", name, err, out)
		}
	}
}

// TestTryMainExit: a ? failing in main runs main's defers, then prints the
// error and exits with status 1.
func TestTryMainExit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	root := tryModule(t, "try_main")
	dir := filepath.Join(root, "try_main")
	bin := filepath.Join(root, "prog")
	if out, err := goCmd(dir, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "COUNT=x")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("exit: %v", err)
	}
	if stdout.String() != "deferred\n" {
		t.Errorf("stdout %q, want the deferred line", stdout.String())
	}
	if want := "error: strconv.Atoi: parsing \"x\": invalid syntax\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

// TestTryWarnings: vet's warnings about ?: more than one in an expression,
// one on the right of && or ||, and with WrapErrors a bare one on a call
// into another package.
func TestTryWarnings(t *testing.T) {
	src := `package main

import "strconv"

func local(s string) (int, error) { return len(s), nil }

func total(a, b string) (int, error) {
	return local(a)? * local(b)?, nil
}

func allowed(ok bool, s string) (bool, error) {
	if ok && local(s)? > 1 {
		return true, nil
	}
	return false, nil
}

func header(s string) (bool, error) {
	if local(s)? > 1 {
		return true, nil
	}
	return false, nil
}

func parse(s string) (int, error) {
	n := strconv.Atoi(s)?
	m := strconv.Atoi(s)?("parsing %q", s)
	return n + m, nil
}
`
	dir, _ := filepath.Abs("testdata/golden")
	for _, wrap := range []bool{false, true} {
		res, err := transpile.Package([]transpile.File{{Name: "w.vuka", Src: []byte(src)}},
			transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir, WrapErrors: wrap})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, w := range res.Files[0].Warnings {
			got = append(got, w.Error()+" ["+string(w.Code)+"]")
		}
		want := []string{
			"w.vuka:8:29: more than one ? in one expression: each may return; give each its own statement [W0109]",
			"w.vuka:12:19: ? on the right of && returns only when the left side lets it run; give it a statement of its own [W0110]",
		}
		if wrap {
			want = append(want, `w.vuka:26:22: a bare ? passes on strconv.Atoi's error without saying what failed; add context: ?("…") [W0111]`)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("WrapErrors %v:\n%s\nwant:\n%s", wrap, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}
