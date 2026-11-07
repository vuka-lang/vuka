package main

import (
	"bytes"
	"io"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	vukaOld = "v2.17.1-0.20261101120000-9156bdf7ad26"
	vukaNew = "v2.17.1-0.20261107135620-b616b7547f57"
)

func shopApp(t *testing.T, gomod string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.25.0\n\n"+gomod)
	return dir
}

func TestCompilerAge(t *testing.T) {
	for _, c := range []struct {
		name, linked, gomod, want string
	}{
		{"older", vukaOld, "require github.com/vuka-lang/vuka/v2 " + vukaNew + "\n",
			"warning: this vuka command compiles with vuka 9156bdf7ad26, older than the b616b7547f57 example.com/shop requires;\n" +
				"         its fixes since aren't applied: vuka upgrade -self main (or go tool vuka, which builds against the app's go.mod)\n"},
		{"equal", vukaNew, "require github.com/vuka-lang/vuka/v2 " + vukaNew + "\n", ""},
		{"newer", vukaNew, "require github.com/vuka-lang/vuka/v2 " + vukaOld + "\n", ""},
		{"release app, same release", "v2.17.0", "require github.com/vuka-lang/vuka/v2 v2.17.0\n", ""},
		{"release app, older release", "v2.16.0", "require github.com/vuka-lang/vuka/v2 v2.17.0\n", "vuka upgrade -self v2.17.0 "},
		{"main command, release app", vukaNew, "require github.com/vuka-lang/vuka/v2 v2.17.0\n", ""},
		{"release command, app on main", "v2.17.0", "require github.com/vuka-lang/vuka/v2 " + vukaNew + "\n", "compiles with vuka v2.17.0, older than the b616b7547f57"},
		{"local command", "", "require github.com/vuka-lang/vuka/v2 " + vukaNew + "\n", ""},
		{"no vuka", vukaOld, "", ""},
		{"directory replace", vukaOld, "require github.com/vuka-lang/vuka/v2 " + vukaNew + "\n\nreplace github.com/vuka-lang/vuka/v2 => ../vuka\n", ""},
		{"version replace", vukaOld, "require github.com/vuka-lang/vuka/v2 v2.17.0\n\nreplace github.com/vuka-lang/vuka/v2 => github.com/vuka-lang/vuka/v2 " + vukaNew + "\n", "older than the b616b7547f57"},
	} {
		got := compilerAge(shopApp(t, c.gomod), c.linked)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWarnCompilerAgeOnce(t *testing.T) {
	oldLinked := linkedVuka
	t.Cleanup(func() { linkedVuka, compilerAgeOnce = oldLinked, sync.Once{} })
	linkedVuka = func() string { return vukaOld }
	compilerAgeOnce = sync.Once{}
	t.Chdir(shopApp(t, "require github.com/vuka-lang/vuka/v2 "+vukaNew+"\n"))
	var errb bytes.Buffer
	warnCompilerAge(&errb)
	warnCompilerAge(&errb)
	if n := strings.Count(errb.String(), "warning: "); n != 1 {
		t.Errorf("%d warnings:\n%s", n, errb.String())
	}
}

func TestDepVersion(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Path: "vuka-main", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: cmdModPath, Version: vukaNew},
			{Path: vukaModPath, Version: vukaOld, Replace: &debug.Module{Path: "../vuka"}},
			{Path: uiModPath, Version: "v2.17.0"},
		},
	}
	if got := depVersion(bi, vukaModPath); got != "" {
		t.Errorf("a directory replace: %q", got)
	}
	if got := depVersion(bi, uiModPath); got != "v2.17.0" {
		t.Errorf("ui %q", got)
	}
	if got := commandVersion(bi); got != vukaNew || !unreleased(got) {
		t.Errorf("command built in a temporary module: %q", got)
	}
	bi.Main = debug.Module{Path: cmdModPath, Version: "v2.17.0"}
	if got := commandVersion(bi); got != "v2.17.0" {
		t.Errorf("installed command: %q", got)
	}
}

func TestVersionCmdCompiler(t *testing.T) {
	oldLinked := linkedVuka
	t.Cleanup(func() { linkedVuka = oldLinked })
	linkedVuka = func() string { return vukaOld }
	t.Chdir(shopApp(t, "require github.com/vuka-lang/vuka/v2 "+vukaNew+"\n"))
	var out, errb bytes.Buffer
	if err := versionCmd(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\ncompiles with vuka "+vukaOld+"\n") || !strings.Contains(errb.String(), "older than the b616b7547f57 example.com/shop requires") {
		t.Errorf("version:\n%s%s", out.String(), errb.String())
	}
}

func TestMainModuleCmds(t *testing.T) {
	cmds := mainModuleCmds("/tmp/m", "build", "-o", "/tmp/m/vuka")
	if len(cmds) != 2 {
		t.Fatalf("%d commands", len(cmds))
	}
	if want := []string{"go", "get", cmdModPath + "@main", vukaModPath + "@main"}; !slices.Equal(cmds[0].Args, want) {
		t.Errorf("get %v", cmds[0].Args)
	}
	if want := []string{"go", "build", "-o", "/tmp/m/vuka", cmdModPath}; !slices.Equal(cmds[1].Args, want) {
		t.Errorf("build %v", cmds[1].Args)
	}
	for _, c := range cmds {
		if c.Dir != "/tmp/m" || !slices.Contains(c.Env, "GOPROXY=direct") || !slices.Contains(c.Env, "GOWORK=off") {
			t.Errorf("%v in %q", c.Args, c.Dir)
		}
	}
	dir := t.TempDir()
	if err := writeMainModule(dir); err != nil {
		t.Fatal(err)
	}
	if got := readTest(t, filepath.Join(dir, "go.mod")); got != "module vuka-main\n" {
		t.Errorf("go.mod %q", got)
	}
}

func fakeSelf(t *testing.T, bi *debug.BuildInfo) *[][]string {
	t.Helper()
	oldGo, oldInfo, oldLatest := goCmd, installedInfo, latestSet
	t.Cleanup(func() { goCmd, installedInfo, latestSet = oldGo, oldInfo, oldLatest })
	var ran [][]string
	goCmd = func(c *exec.Cmd, stdout, stderr io.Writer) error {
		ran = append(ran, c.Args)
		return nil
	}
	installedInfo = func(string) (*debug.BuildInfo, error) { return bi, nil }
	latestSet = func(dir string, mods []string) (string, error) {
		if !slices.Equal(mods, []string{cmdModPath}) {
			t.Errorf("latest of %v", mods)
		}
		return "v2.17.0", nil
	}
	t.Setenv("GOBIN", t.TempDir())
	t.Chdir(t.TempDir())
	return &ran
}

func TestUpgradeSelfRelease(t *testing.T) {
	ran := fakeSelf(t, &debug.BuildInfo{
		Main: debug.Module{Path: cmdModPath, Version: "v2.17.0"},
		Deps: []*debug.Module{{Path: vukaModPath, Version: "v2.17.0"}},
	})
	var out bytes.Buffer
	if err := upgrade([]string{"-self"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"go", "install", cmdModPath + "@v2.17.0"}}; !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Errorf("ran %v", *ran)
	}
	if !strings.Contains(out.String(), "cmd/vuka v2.17.0, compiling with vuka v2.17.0\n") {
		t.Errorf("output %q", out.String())
	}
}

func TestUpgradeSelfMain(t *testing.T) {
	ran := fakeSelf(t, &debug.BuildInfo{
		Main: debug.Module{Path: "vuka-main", Version: "(devel)"},
		Deps: []*debug.Module{{Path: cmdModPath, Version: vukaNew}, {Path: vukaModPath, Version: vukaNew}},
	})
	var out bytes.Buffer
	if err := upgrade([]string{"-self", "main"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"go", "get", cmdModPath + "@main", vukaModPath + "@main"}, {"go", "install", cmdModPath}}
	if !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Errorf("ran %v", *ran)
	}
	if !strings.Contains(out.String(), "cmd/vuka "+vukaNew+", compiling with vuka "+vukaNew+"\n") {
		t.Errorf("output %q", out.String())
	}

	*ran = nil
	out.Reset()
	if err := upgrade([]string{"-self", "-n", "main"}, &out, &out); err != nil || *ran != nil || !strings.Contains(out.String(), "dry run") {
		t.Errorf("dry run ran %v (err %v):\n%s", *ran, err, out.String())
	}
	if err := upgrade([]string{"-self", "latest"}, &out, &out); err == nil {
		t.Error("-self latest: no error")
	}
}
