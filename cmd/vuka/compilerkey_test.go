package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func releaseInfo() *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.27.3",
		Main:      debug.Module{Path: cmdModPath, Version: "v2.23.1-0.20261120021808-f874507aea22"},
		Deps: []*debug.Module{
			{Path: vukaModPath, Version: "v2.23.1-0.20261120021808-f874507aea22", Sum: "h1:aaa"},
			{Path: uiModPath, Version: "v2.23.0", Sum: "h1:bbb"},
			{Path: "github.com/a-h/templ", Version: "v0.3.1020", Sum: "h1:ccc"},
		},
	}
}

func TestCompilerKey(t *testing.T) {
	exe := func(s string) func() string { return func() string { return " " + s } }

	a, b := compilerKey(releaseInfo(), exe("/bin/vuka 1 1")), compilerKey(releaseInfo(), exe("/other/vuka 2 2"))
	if a != b {
		t.Errorf("the same source built twice: %q and %q", a, b)
	}
	if strings.Contains(a, "vuka 1") {
		t.Errorf("a build's key names its executable: %q", a)
	}

	reordered := releaseInfo()
	reordered.Deps[0], reordered.Deps[2] = reordered.Deps[2], reordered.Deps[0]
	if got := compilerKey(reordered, exe("x")); got != a {
		t.Errorf("the order of the dependencies changes the key: %q", got)
	}

	changed := func(name string, edit func(*debug.BuildInfo)) {
		t.Helper()
		bi := releaseInfo()
		edit(bi)
		if got := compilerKey(bi, exe("x")); got == a {
			t.Errorf("%s: the key is the same", name)
		}
	}
	changed("the compiler's version", func(bi *debug.BuildInfo) { bi.Deps[0].Version = "v2.23.2" })
	changed("the compiler's checksum", func(bi *debug.BuildInfo) { bi.Deps[0].Sum = "h1:zzz" })
	changed("a dependency", func(bi *debug.BuildInfo) { bi.Deps[2].Version = "v0.3.1021" })
	changed("the command's version", func(bi *debug.BuildInfo) { bi.Main.Version = "v2.23.1-0.20261121000000-aaaaaaaaaaaa" })
	changed("the Go toolchain", func(bi *debug.BuildInfo) { bi.GoVersion = "go1.27.4" })

	local := func(name string, edit func(*debug.BuildInfo)) {
		t.Helper()
		bi := releaseInfo()
		edit(bi)
		x, y := compilerKey(bi, exe("/bin/vuka 1 1")), compilerKey(bi, exe("/bin/vuka 2 2"))
		if x == y || x == a {
			t.Errorf("%s: a local build's key %q, %q must name its executable and not a release's %q", name, x, y, a)
		}
	}
	local("a workspace module", func(bi *debug.BuildInfo) { bi.Deps[0].Version = "(devel)"; bi.Deps[0].Sum = "" })
	local("a directory replace", func(bi *debug.BuildInfo) { bi.Deps[0].Replace = &debug.Module{Path: "../.."} })
	local("a dirty checkout", func(bi *debug.BuildInfo) { bi.Main.Version = "v2.23.1-0.20261120021808-f874507aea22+dirty" })
	local("an unversioned main", func(bi *debug.BuildInfo) { bi.Main.Version = "(devel)" })
	local("a dependency without a checksum", func(bi *debug.BuildInfo) { bi.Deps[1].Sum = "" })

	if x, y := compilerKey(nil, exe("a")), compilerKey(nil, exe("b")); x == y {
		t.Errorf("without build info the key is the executable's: %q", x)
	}

	replaced := releaseInfo()
	replaced.Deps[0].Replace = &debug.Module{Path: "example.com/fork", Version: "v1.0.0", Sum: "h1:fork"}
	if got := compilerKey(replaced, exe("x")); got == a || strings.Contains(got, " x") {
		t.Errorf("a module replacement: %q", got)
	}
}
