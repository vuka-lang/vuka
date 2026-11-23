package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGoShimInvokedAsGo(t *testing.T) {
	for _, c := range []struct {
		goos, argv0 string
		want        bool
	}{
		{"linux", "go", true},
		{"linux", "/home/u/.vuka/shims/go", true},
		{"linux", "vuka", false},
		{"linux", "/usr/local/bin/vuka", false},
		{"linux", "gofmt", false},
		{"linux", "go.exe", false},
		{"linux", "GO", false},
		{"linux", "gopls", false},
		{"darwin", "./go", true},
		{"windows", `C:\Users\u\.vuka\shims\go.exe`, true},
		{"windows", `C:\Users\u\.vuka\shims\GO.EXE`, true},
		{"windows", `C:/tools/Go.exe`, true},
		{"windows", "go", true},
		{"windows", `C:\bin\vuka.exe`, false},
		{"windows", `C:\bin\gofmt.exe`, false},
		{"windows", `C:\go.exe\vuka.exe`, false},
	} {
		if got := invokedAsGo(c.goos, c.argv0); got != c.want {
			t.Errorf("invokedAsGo(%s, %q) = %v, want %v", c.goos, c.argv0, got, c.want)
		}
	}
}

func TestGoShimVukaModule(t *testing.T) {
	for path, want := range map[string]bool{
		"github.com/vuka-lang/vuka":          true,
		"github.com/vuka-lang/vuka/v2":       true,
		"github.com/vuka-lang/vuka/v3":       true,
		"github.com/vuka-lang/vuka/cmd/vuka": false,
		"github.com/vuka-lang/vuka/vx":       false,
		"github.com/vuka-lang/vuka/v":        false,
		"github.com/vuka-lang/ui/v2":         false,
		"github.com/vuka-lang/vukaish":       false,
	} {
		if got := isVukaModule(path); got != want {
			t.Errorf("isVukaModule(%q) = %v, want %v", path, got, want)
		}
	}
}

// shimFixtures are a module requiring vuka (an indirect requirement, as
// one on web alone has), a plain one, and a directory in neither.
func shimFixtures(t *testing.T) (app, plain, none string) {
	t.Helper()
	base := t.TempDir()
	app, plain, none = filepath.Join(base, "app"), filepath.Join(base, "plain"), filepath.Join(base, "none")
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/app\n\ngo 1.25\n\nrequire github.com/vuka-lang/vuka/v2 v2.24.0 // indirect\n")
	writeFile(t, filepath.Join(app, "pages", "x.go"), "package pages\n")
	writeFile(t, filepath.Join(plain, "go.mod"), "module example.com/plain\n\ngo 1.25\n\nrequire github.com/vuka-lang/ui/v2 v2.24.0\n")
	if err := os.MkdirAll(none, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadModuleRoot(none); err == nil {
		t.Skipf("%s is inside a module", none)
	}
	return app, plain, none
}

func TestGoShimRoutes(t *testing.T) {
	app, plain, none := shimFixtures(t)
	sub := filepath.Join(app, "pages")
	for _, c := range []struct {
		dir  string
		args string
		want bool
	}{
		{app, "build", true},
		{app, "build -o out .", true},
		{app, "test ./...", true},
		{app, "test -run X ./... -args -overlay=x", true},
		{app, "vet ./...", true},
		{app, "run .", true},
		{sub, "test", true},
		{app, "", false},
		{app, "install", false},
		{app, "mod tidy", false},
		{app, "env GOPATH", false},
		{app, "version", false},
		{app, "list ./...", false},
		{app, "tool vukavet", false},
		{app, "generate ./...", false},
		{app, "-C pages build", false},
		{app, "build -h", false},
		{app, "test -help", false},
		{app, "build -overlay=o.json .", false},
		{app, "build --overlay o.json .", false},
		{app, "test -modfile=alt.mod ./...", false},
		{plain, "build", false},
		{plain, "test ./...", false},
		{none, "build", false},
		{none, "run x.go", false},
	} {
		if got := shimRoutes(strings.Fields(c.args), c.dir); got != c.want {
			t.Errorf("in %s: go %s routes = %v, want %v", filepath.Base(c.dir), c.args, got, c.want)
		}
	}
}

func loadModuleRoot(dir string) (string, string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, "", nil
		}
		if filepath.Dir(d) == d {
			return "", "", errors.New("none")
		}
	}
}

func TestGoShimPathHelpers(t *testing.T) {
	if got := goNames("linux", ".EXE"); len(got) != 1 || got[0] != "go" {
		t.Errorf("goNames(linux) = %q", got)
	}
	if got := strings.Join(goNames("windows", ""), " "); got != "go.com go.exe go.bat go.cmd" {
		t.Errorf("goNames(windows, default PATHEXT) = %s", got)
	}
	if got := strings.Join(goNames("windows", ".EXE;;.CMD;bad"), " "); got != "go.exe go.cmd" {
		t.Errorf("goNames(windows, .EXE;;.CMD;bad) = %s", got)
	}
	if got := splitPathList(`C:\Go\bin;"C:\Program Files\x";;D:\y`, "windows"); strings.Join(got, "|") != `C:\Go\bin|C:\Program Files\x||D:\y` {
		t.Errorf("splitPathList(windows) = %q", got)
	}
	if got := splitPathList("/a:/b c::/d", "linux"); strings.Join(got, "|") != "/a|/b c||/d" {
		t.Errorf("splitPathList(linux) = %q", got)
	}
	if !samePathDir("windows", `C:\Users\U\.vuka\Shims\`, `c:/users/u/.vuka/shims`) {
		t.Error("windows PATH entries compare ignoring case, separators and a trailing one")
	}
	if samePathDir("linux", "/nowhere/A", "/nowhere/a") {
		t.Error("linux PATH entries compare with case")
	}
}

// TestGoShimMakeEntry: a symlink where one can be made; on Windows a hard
// link when it can't, then a copy; elsewhere a failed symlink is an error.
func TestGoShimMakeEntry(t *testing.T) {
	no := func(string, string) error { return errors.New("no") }
	ok := func(string, string) error { return nil }
	for _, c := range []struct {
		goos                string
		symlink, link, copy func(string, string) error
		want                string
	}{
		{"linux", ok, no, no, "symlink"},
		{"linux", no, ok, ok, ""},
		{"darwin", no, ok, ok, ""},
		{"windows", ok, ok, ok, "symlink"},
		{"windows", no, ok, ok, "hard link"},
		{"windows", no, no, ok, "copy"},
		{"windows", no, no, no, ""},
	} {
		got, err := makeEntry(c.goos, "vuka", "go", linker{symlink: c.symlink, link: c.link, copy: c.copy})
		if got != c.want || (err == nil) != (c.want != "") {
			t.Errorf("%s: makeEntry = %q, %v; want %q", c.goos, got, err, c.want)
		}
		if c.goos == "windows" && c.want == "" && (err == nil || !strings.Contains(err.Error(), "hard link") || !strings.Contains(err.Error(), "copy")) {
			t.Errorf("windows: the error names each way tried: %v", err)
		}
	}
}

// testLinker makes a symlink, else a copy: never a hard link to the
// running test binary, which Windows can't remove while it runs.
var testLinker = linker{
	symlink: os.Symlink,
	link:    func(string, string) error { return errors.New("not in the tests") },
	copy:    copyExecutable,
}

// selfShim puts the test binary in a new directory as go (go.exe): a
// symlink, else (Windows without Developer Mode) a copy. TestMain runs
// the test binary called go as the shim.
func selfShim(t *testing.T) (dir, entry string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	entry = shimEntry(dir, runtime.GOOS)
	if _, err := makeEntry(runtime.GOOS, self, entry, testLinker); err != nil {
		t.Fatal(err)
	}
	return dir, entry
}

// realGoDir is the directory of the go the tests run.
func realGoDir(t *testing.T) (dir, gobin string) {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(gobin); err == nil {
		gobin = real
	}
	if isVukaBinary(gobin, "") {
		t.Skip("the go on PATH is a go shim")
	}
	return filepath.Dir(gobin), gobin
}

func TestGoShimRealGo(t *testing.T) {
	shimDir, entry := selfShim(t)
	goDir, gobin := realGoDir(t)
	self, _ := os.Executable()
	l := shimLookup{self: self, selfPath: shimEnv().selfPath}
	env := map[string]string{}
	l.getenv = func(k string) string { return env[k] }
	sep := string(os.PathListSeparator)

	// The shim's own directory, twice, and through a link to it.
	dirs := []string{shimDir, shimDir}
	link := filepath.Join(t.TempDir(), "shims")
	if os.Symlink(shimDir, link) == nil {
		dirs = append(dirs, link)
	}
	// A copy of the shim in a directory of its own, not the running file.
	other := t.TempDir()
	if err := copyExecutable(self, shimEntry(other, runtime.GOOS)); err != nil {
		t.Fatal(err)
	}
	dirs = append(dirs, other, "relative", goDir)
	env["PATH"] = strings.Join(dirs, sep)
	if runtime.GOOS == "windows" {
		env["PATHEXT"] = ".COM;.EXE;.BAT;.CMD"
	}
	got, err := realGo(l, runtime.GOOS)
	if err != nil || !sameInode(got, gobin) {
		t.Errorf("realGo past the shims = %q, %v; want %s", got, err, gobin)
	}

	// The shim directory, named, is skipped even when its go is no vuka.
	fake := t.TempDir()
	writeExecutable(t, shimEntry(fake, runtime.GOOS))
	env["PATH"] = strings.Join([]string{fake, goDir}, sep)
	l.dirs = []string{fake}
	if got, err := realGo(l, runtime.GOOS); err != nil || !sameInode(got, gobin) {
		t.Errorf("realGo past the shim directory = %q, %v; want %s", got, err, gobin)
	}
	l.dirs = nil

	// VUKA_REAL_GO wins, and isn't a shim.
	env[realGoEnv] = shimEntry(fake, runtime.GOOS)
	if got, err := realGo(l, runtime.GOOS); err != nil || got != env[realGoEnv] {
		t.Errorf("realGo with %s = %q, %v", realGoEnv, got, err)
	}
	env[realGoEnv] = entry
	if _, err := realGo(l, runtime.GOOS); err == nil || !strings.Contains(err.Error(), "is the go shim") {
		t.Errorf("%s naming the shim: %v", realGoEnv, err)
	}
	delete(env, realGoEnv)

	// Only shims on PATH.
	env["PATH"] = strings.Join([]string{shimDir, other}, sep)
	if got, err := realGo(l, runtime.GOOS); err == nil {
		t.Errorf("realGo with only shims on PATH = %q", got)
	}
}

// writeExecutable writes a file that stands for a go and isn't one.
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// shimCommand is the shim run with args in dir, PATH having the shim
// first, then the real go; bounded, so that a loop fails the test.
func shimCommand(t *testing.T, ctx context.Context, entry, dir string, path []string, args ...string) *exec.Cmd {
	t.Helper()
	c := exec.CommandContext(ctx, entry, args...)
	c.Dir = dir
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "PATH", shimOffEnv, shimDepthEnv, realGoEnv, childEnv:
			continue
		}
		c.Env = append(c.Env, kv)
	}
	c.Env = append(c.Env, "PATH="+strings.Join(path, string(os.PathListSeparator)))
	return c
}

// TestGoShimPassthrough: go env and a failing go build in a plain module
// through the shim are the real go's: output, error output and status.
func TestGoShimPassthrough(t *testing.T) {
	shimDir, entry := selfShim(t)
	goDir, gobin := realGoDir(t)
	path := append([]string{shimDir, goDir}, filepath.SplitList(os.Getenv("PATH"))...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, plain, _ := shimFixtures(t)
	writeFile(t, filepath.Join(plain, "go.mod"), "module example.com/plain\n\ngo 1.21\n")
	writeFile(t, filepath.Join(plain, "main.go"), "package main\n\nfunc main() { undefinedName() }\n")
	for _, args := range [][]string{
		{"env", "GOPATH"},
		{"version"},
		{"build", "-o", os.DevNull, "."},
		{"vet", "."},
		{"list", "-f", "{{.ImportPath}}", "."},
	} {
		var sOut, sErr, rOut, rErr bytes.Buffer
		s := shimCommand(t, ctx, entry, plain, path, args...)
		s.Stdout, s.Stderr = &sOut, &sErr
		sCode := exitCode(s.Run())
		r := shimCommand(t, ctx, gobin, plain, path, args...)
		r.Stdout, r.Stderr = &rOut, &rErr
		rCode := exitCode(r.Run())
		if sCode != rCode || sOut.String() != rOut.String() || sErr.String() != rErr.String() {
			t.Errorf("go %s through the shim: status %d, %q, %q; the real go: %d, %q, %q",
				strings.Join(args, " "), sCode, sOut.Bytes(), sErr.Bytes(), rCode, rOut.Bytes(), rErr.Bytes())
		}
		if args[0] == "build" && rCode == 0 {
			t.Errorf("go build of a broken module succeeded")
		}
	}
	if ctx.Err() != nil {
		t.Fatal("the shim didn't finish: a loop?")
	}
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// TestGoShimLoop: a go on PATH that hands the command back to go (a
// wrapper script) would loop forever through the shim; the shim stops it.
func TestGoShimLoop(t *testing.T) {
	shimDir, entry := selfShim(t)
	wrap := t.TempDir()
	var wrapper string
	if runtime.GOOS == "windows" {
		wrapper = filepath.Join(wrap, "go.bat")
		writeFile(t, wrapper, "@go %*\r\n")
	} else {
		wrapper = filepath.Join(wrap, "go")
		if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec go \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out bytes.Buffer
	c := shimCommand(t, ctx, entry, t.TempDir(), []string{shimDir, wrap}, "env", "GOPATH")
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	if ctx.Err() != nil {
		t.Fatalf("go env through a looping PATH didn't stop:\n%s", out.Bytes())
	}
	if err == nil || !strings.Contains(out.String(), "a loop") {
		t.Fatalf("go env through a looping PATH: %v\n%s", err, out.Bytes())
	}
}

// shimAppSrc runs go run . itself: through a shim that took it for vuka's
// again, each run would start another (stopped at 3 here).
const shimAppSrc = `package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func name() Result[string] { return Ok("ada") }

func main() {
	n, _ := strconv.Atoi(os.Getenv("SHIM_LOOP"))
	if n >= 3 {
		fmt.Println("looped")
		return
	}
	c := exec.Command("go", "run", ".")
	c.Env = append(os.Environ(), "SHIM_LOOP="+strconv.Itoa(n+1))
	out, err := c.CombinedOutput()
	fmt.Printf("%s; the inner go run: %v\n%s", name().Unwrap(), err, out)
}
`

const shimAppTest = `package main

import "testing"

func TestName(t *testing.T) {
	if got := name().Unwrap(); got != "ada" {
		t.Fatalf("name() = %q", got)
	}
}
`

// TestGoShimTestApp: in a vuka app plain go test fails, the .vuka files
// being nothing to it; through the shim it is vuka test, and passes. vuka
// test itself, with the shim first on PATH, runs the real go and doesn't
// reach itself again.
func TestGoShimTestApp(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{"main.vuka": shimAppSrc, "main_test.go": shimAppTest})
	shimDir, entry := selfShim(t)
	goDir, gobin := realGoDir(t)
	path := append([]string{shimDir, goDir}, filepath.SplitList(os.Getenv("PATH"))...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var out bytes.Buffer
	r := shimCommand(t, ctx, gobin, dir, path, "test", "-count=1", "./...")
	r.Stdout, r.Stderr = &out, &out
	if err := r.Run(); err == nil {
		t.Fatalf("the real go test passed without the generated Go:\n%s", out.Bytes())
	}

	out.Reset()
	s := shimCommand(t, ctx, entry, dir, path, "test", "-count=1", "./...")
	s.Stdout, s.Stderr = &out, &out
	if err := s.Run(); err != nil {
		t.Fatalf("go test through the shim: %v\n%s", err, out.Bytes())
	}
	if !strings.Contains(out.String(), "ok  \tm") {
		t.Errorf("go test through the shim ran no test:\n%s", out.Bytes())
	}

	// go run through the shim is vuka run; the program's own go run is the
	// real go's (go run puts its GOROOT/bin first on the program's PATH),
	// which has no generated Go.
	out.Reset()
	s = shimCommand(t, ctx, entry, dir, path, "run", ".")
	s.Stdout, s.Stderr = &out, &out
	if err := s.Run(); err != nil {
		t.Fatalf("go run through the shim: %v\n%s", err, out.Bytes())
	}
	if !strings.Contains(out.String(), "ada; the inner go run: exit status 1") || strings.Contains(out.String(), "looped") {
		t.Errorf("go run through the shim, its program running go run:\n%s", out.Bytes())
	}

	// vuka test (the test binary as vuka), the shim first on PATH: its go
	// commands reach the shim, which passes them on.
	out.Reset()
	v := shimCommand(t, ctx, mustExecutable(t), dir, path, "test", "-count=1", "./...")
	vukavet, err := checkoutVukavet()
	if err != nil {
		t.Fatal(err)
	}
	v.Env = append(v.Env, childEnv+"="+vukavet)
	v.Stdout, v.Stderr = &out, &out
	if err := v.Run(); err != nil {
		t.Fatalf("vuka test with the shim on PATH: %v\n%s", err, out.Bytes())
	}
	if ctx.Err() != nil {
		t.Fatal("the shim didn't finish: a loop?")
	}
}

// shimInternalEnv makes the test binary, run as a go shim that takes the
// command for vuka's, first run go build itself, as vuka's commands run
// go commands of their own (the orm tool's build while transpiling): one
// the shim took for vuka's again would run another, for ever (stopped at 3).
const shimInternalEnv = "VUKA_TEST_SHIM_INTERNAL"

func shimInternalGo() int {
	n, _ := strconv.Atoi(os.Getenv(shimInternalEnv))
	if n >= 3 {
		fmt.Println("looped")
		return 3
	}
	c := exec.Command("go", "build", "-o", os.DevNull, ".")
	c.Env = append(os.Environ(), shimInternalEnv+"="+strconv.Itoa(n+1))
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return exitCode(c.Run())
}

// TestGoShimInternalGo: a go command vuka runs, reaching the shim first on
// PATH, is the real go's, never vuka's again: no loop.
func TestGoShimInternalGo(t *testing.T) {
	app, _, _ := shimFixtures(t)
	shimDir, entry := selfShim(t)
	goDir, _ := realGoDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out bytes.Buffer
	c := shimCommand(t, ctx, entry, app, []string{shimDir, goDir}, "build", ".")
	c.Env = append(c.Env, shimInternalEnv+"=0")
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	if ctx.Err() != nil || strings.Contains(out.String(), "looped") {
		t.Fatalf("vuka's own go build reached vuka again:\n%s", out.Bytes())
	}
	// The real go's: the module's root has no Go files.
	if err == nil || !strings.Contains(out.String(), "no Go files") {
		t.Errorf("vuka's own go build: %v\n%s", err, out.Bytes())
	}
}

const shimBuildSrc = `package main

import "fmt"

var released bool

var mark = "none"

func name() Result[string] { return Ok("ada") }

func main() { fmt.Println(name().Unwrap(), released, mark) }
`

const shimBuildRelease = `//go:build vuka_release

package main

func init() { released = true }
`

// TestGoShimBuildPlain: go build through the shim is the overlay alone:
// the binary plain go build makes with the generated Go on disk — debug
// information kept, no vuka_release tag, no build/ module — and the
// caller's -tags and -ldflags as given. vuka build adds its own.
func TestGoShimBuildPlain(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := module(t, map[string]string{"main.vuka": shimBuildSrc, "release.go": shimBuildRelease})
	shimDir, entry := selfShim(t)
	goDir, _ := realGoDir(t)
	path := append([]string{shimDir, goDir}, filepath.SplitList(os.Getenv("PATH"))...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	bins := t.TempDir()
	build := func(bin string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		c := shimCommand(t, ctx, entry, dir, path, append(append([]string{"build", "-o", bin}, args...), ".")...)
		c.Stdout, c.Stderr = &out, &out
		if err := c.Run(); err != nil {
			t.Fatalf("go build %s through the shim: %v\n%s", strings.Join(args, " "), err, out.Bytes())
		}
		run, err := exec.CommandContext(ctx, bin).Output()
		if err != nil {
			t.Fatalf("%s: %v", bin, err)
		}
		return strings.TrimSpace(string(run))
	}

	bin := filepath.Join(bins, exe("plain"))
	if got := build(bin); got != "ada false none" {
		t.Errorf("go build through the shim: the binary prints %q, want %q (no vuka_release tag)", got, "ada false none")
	}
	if has, err := hasDWARF(bin); err != nil {
		t.Log(err)
	} else if !has {
		t.Error("go build through the shim left out the debug information")
	}
	if _, err := os.Stat(filepath.Join(dir, "build")); err == nil {
		t.Error("go build through the shim wrote the build/ module")
	}

	bin = filepath.Join(bins, exe("flags"))
	if got := build(bin, "-tags=vuka_release", "-ldflags=-X=main.mark=mine"); got != "ada true mine" {
		t.Errorf("go build -tags -ldflags through the shim: the binary prints %q, want %q", got, "ada true mine")
	}
	if has, err := hasDWARF(bin); err == nil && !has {
		t.Error("go build -ldflags through the shim: -w was added to the caller's flag")
	}

	// An app on web: the stylesheet is built and embedded, as the
	// generated Go is.
	t.Run("tailwind", func(t *testing.T) {
		root := webModule(t, map[string]string{"main.go": twMain, "pages/index.vuka.html": twPage,
			"styles/app.css": "@import \"tailwindcss\";\n@theme { --color-brand: #123456; }\n"})
		// As vuka finds it: tailwindcss, or the standalone binary under
		// its released name (tailwindcss-windows-x64.exe on the Windows
		// job), or the npm package's.
		if _, err := tailwindBinary(root); err != nil {
			needs(t, "no Tailwind CLI: %v", err)
		}
		t.Setenv(twEnv, "")
		app := filepath.Join(t.TempDir(), exe("app"))
		var out bytes.Buffer
		c := shimCommand(t, ctx, entry, root, path, "build", "-o", app, ".")
		c.Stdout, c.Stderr = &out, &out
		if err := c.Run(); err != nil {
			t.Fatalf("go build through the shim: %v\n%s", err, out.Bytes())
		}
		if has, err := hasDWARF(app); err == nil && !has {
			t.Error("go build through the shim left out the debug information")
		}
		for _, p := range []string{"build", twGoName} {
			if _, err := os.Stat(filepath.Join(root, p)); err == nil {
				t.Errorf("go build through the shim wrote %s into the module", p)
			}
		}
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		srv := exec.Command(app)
		srv.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=")
		if err := srv.Start(); err != nil {
			t.Fatal(err)
		}
		defer srv.Process.Kill()
		get := func(p string) string {
			var res *http.Response
			waitFor(t, func() bool {
				var err error
				res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, p))
				return err == nil
			})
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			return string(b)
		}
		page := get("/")
		i := strings.Index(page, `<link rel="stylesheet" href="/_vuka/app.css?v=`)
		if i < 0 {
			t.Fatalf("the page links no stylesheet: %s", page)
		}
		href := page[i+len(`<link rel="stylesheet" href="`):]
		href = href[:strings.IndexByte(href, '"')]
		css := get(href)
		for _, want := range []string{".text-brand", "#123456"} {
			if !strings.Contains(css, want) {
				t.Errorf("the embedded stylesheet lacks %s:\n%.300s", want, css)
			}
		}
	})

	// vuka build itself keeps its release build.
	bin = filepath.Join(bins, exe("vuka"))
	var out bytes.Buffer
	if err := vukaIn(dir, &out, &out, "build", "-o", bin, "."); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, out.Bytes())
	}
	if run, err := exec.CommandContext(ctx, bin).Output(); err != nil || strings.TrimSpace(string(run)) != "ada true none" {
		t.Errorf("vuka build: the binary prints %q (%v), want the vuka_release build", run, err)
	}
	if has, err := hasDWARF(bin); err == nil && has {
		t.Error("vuka build kept the debug information")
	}
	if _, err := os.Stat(filepath.Join(dir, "build")); err != nil {
		t.Errorf("vuka build wrote no build/ module: %v", err)
	}
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// TestGoShimInstall: install links go to vuka in the directory and says
// where PATH has it; status reads it back; remove deletes it and the
// directory; a go that isn't a vuka is never replaced or removed.
func TestGoShimInstall(t *testing.T) {
	self := mustExecutable(t)
	dir := filepath.Join(t.TempDir(), "shims")
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if err := shimInstall(dir, self, runtime.GOOS, testLinker, &out); err != nil {
		t.Fatal(err)
	}
	entry := shimEntry(dir, runtime.GOOS)
	if !sameInode(entry, self) && !isOurEntry(entry) {
		t.Fatalf("%s doesn't run vuka:\n%s", entry, out.Bytes())
	}
	if !strings.Contains(out.String(), "isn't on PATH") {
		t.Errorf("install with the directory off PATH:\n%s", out.Bytes())
	}
	if runtime.GOOS == "windows" && !strings.Contains(out.String(), "$env:Path") {
		t.Errorf("no PowerShell PATH line:\n%s", out.Bytes())
	}
	if runtime.GOOS != "windows" && !strings.Contains(out.String(), "export PATH=") {
		t.Errorf("no POSIX PATH line:\n%s", out.Bytes())
	}

	// Again: replaced, the shim directory first on PATH.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out.Reset()
	if err := shimInstall(dir, self, runtime.GOOS, testLinker, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is first on PATH") {
		t.Errorf("install with the directory first on PATH:\n%s", out.Bytes())
	}

	// Behind a directory with a go.
	before := t.TempDir()
	writeExecutable(t, shimEntry(before, runtime.GOOS))
	t.Setenv("PATH", before+string(os.PathListSeparator)+dir)
	out.Reset()
	if err := shimStatus(dir, self, runtime.GOOS, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "go shim: "+entry) || !strings.Contains(out.String(), "after "+before) {
		t.Errorf("status:\n%s", out.Bytes())
	}

	out.Reset()
	if err := shimRemove(dir, runtime.GOOS, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("remove left %s:\n%s", dir, out.Bytes())
	}

	// A real go in the directory stays.
	writeExecutable(t, entry)
	if err := shimInstall(dir, self, runtime.GOOS, testLinker, &out); err == nil {
		t.Error("install replaced a go that isn't a vuka")
	}
	if err := shimRemove(dir, runtime.GOOS, &out); err == nil {
		t.Error("remove deleted a go that isn't a vuka")
	}
	if _, err := os.Stat(entry); err != nil {
		t.Errorf("the go that isn't a vuka is gone: %v", err)
	}
}

func TestGoShimPathAdvice(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	dir := filepath.Join(home, ".vuka", "shims")
	for _, c := range []struct{ goos, shell, want string }{
		{"windows", "", `$env:Path = "` + dir + `;" + $env:Path`},
		{"windows", "", `[Environment]::SetEnvironmentVariable("Path"`},
		{"linux", "/bin/bash", `export PATH="$HOME` + string(filepath.Separator) + filepath.Join(".vuka", "shims") + `:$PATH"`},
		{"darwin", "/bin/zsh", "~/.zshrc"},
		{"linux", "/usr/bin/fish", "fish_add_path --move ~"},
	} {
		if got := pathAdvice(dir, c.goos, c.shell); !strings.Contains(got, c.want) {
			t.Errorf("pathAdvice(%s, %s) = %s, want it to have %s", c.goos, c.shell, got, c.want)
		}
	}
}
