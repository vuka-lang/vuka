package main

import (
	"bytes"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
)

// withoutStaleToolchain is out without the stale-toolchain warning: a test's
// vuka built with GOWORK=off links the published compiler while its app
// replaces the compiler with this repository, so the warning is right there.
func withoutStaleToolchain(out []byte) []byte {
	var kept [][]byte
	for _, line := range bytes.SplitAfter(out, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("warning: this vuka command was built with the published ")) {
			kept = append(kept, line)
		}
	}
	return bytes.Join(kept, nil)
}

// installedBI is the build info of an installed vuka command: published
// versions, no local copies.
func installedBI() *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Path: cmdModPath, Version: "v2.19.0"},
		Deps: []*debug.Module{
			{Path: vukaModPath, Version: "v2.19.0"},
			{Path: uiModPath, Version: "v2.19.0"},
			{Path: webModPath, Version: "v2.19.0"},
		},
	}
}

// workspaceBI is the build info of a command built inside the workspace:
// the compiler replaced by a directory, the command at (devel).
func workspaceBI() *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Path: cmdModPath, Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: vukaModPath, Version: "v2.19.0", Replace: &debug.Module{Path: "../vuka"}},
		},
	}
}

func TestStaleToolchain(t *testing.T) {
	appMod := "module example.com/shop\n\ngo 1.25.0\n"
	for _, c := range []struct {
		name  string
		bi    *debug.BuildInfo
		files map[string]string // "" values are directories
		work  string            // go.work path relative to the root dir, "" for none
		mod   string            // the module root relative to the root dir, "" for none
		want  string
	}{
		{"workspace uses local vuka, installed command",
			installedBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse (\n\t./shop\n\t./vuka\n)\n",
				"shop/go.mod": appMod,
				"vuka/go.mod": "module " + vukaModPath + "\n\ngo 1.25.0\n",
			},
			"go.work", "shop",
			vukaModPath + " v2.19.0"},
		{"workspace uses local cmd/vuka, installed command",
			installedBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse (\n\t./shop\n\t./cmd\n)\n",
				"shop/go.mod": appMod,
				"cmd/go.mod":  "module " + cmdModPath + "\n\ngo 1.25.0\n",
			},
			"go.work", "shop",
			cmdModPath + " v2.19.0"},
		{"workspace uses local vuka, command built from it",
			workspaceBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse (\n\t./shop\n\t./vuka\n)\n",
				"shop/go.mod": appMod,
				"vuka/go.mod": "module " + vukaModPath + "\n\ngo 1.25.0\n",
			},
			"go.work", "shop",
			""},
		{"workspace of app modules only",
			installedBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse ./shop\n",
				"shop/go.mod": appMod,
			},
			"go.work", "shop",
			""},
		{"go.work replace to a directory",
			installedBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse ./shop\n\nreplace " + vukaModPath + " => ../vuka\n",
				"shop/go.mod": appMod,
			},
			"go.work", "shop",
			vukaModPath + " v2.19.0"},
		{"go.mod directory replace, no workspace",
			installedBI(),
			map[string]string{
				"shop/go.mod": appMod + "\nrequire " + vukaModPath + " v2.19.0\n\nreplace " + vukaModPath + " => ../vuka\n",
			},
			"", "shop",
			vukaModPath + " v2.19.0"},
		{"go.mod version replace",
			installedBI(),
			map[string]string{
				"shop/go.mod": appMod + "\nrequire " + vukaModPath + " v2.18.0\n\nreplace " + vukaModPath + " => " + vukaModPath + " v2.19.0\n",
			},
			"", "shop",
			""},
		{"local web is the app's business, not the command's",
			installedBI(),
			map[string]string{
				"go.work":     "go 1.25.0\n\nuse (\n\t./shop\n\t./web\n)\n",
				"shop/go.mod": appMod + "\nrequire " + webModPath + " v2.19.0\n\nreplace " + webModPath + " => ../web\n",
				"web/go.mod":  "module " + webModPath + "\n\ngo 1.25.0\n",
			},
			"go.work", "shop",
			""},
		{"no workspace, no replaces",
			installedBI(),
			map[string]string{"shop/go.mod": appMod},
			"", "shop",
			""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, src := range c.files {
				writeFile(t, filepath.Join(dir, name), src)
			}
			work, root := "", ""
			if c.work != "" {
				work = filepath.Join(dir, c.work)
			}
			if c.mod != "" {
				root = filepath.Join(dir, c.mod)
			}
			got := staleToolchain(c.bi, work, root)
			if c.want == "" {
				if got != "" {
					t.Fatalf("unexpected warning: %q", got)
				}
				return
			}
			if !strings.Contains(got, c.want) || !strings.Contains(got, "go run "+cmdModPath) {
				t.Fatalf("warning %q, want it naming %q and the fix", got, c.want)
			}
			if strings.Count(strings.TrimSuffix(got, "\n"), "\n") != 0 {
				t.Fatalf("warning is not one line: %q", got)
			}
		})
	}
}

func TestWarnStaleToolchainOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.work"), "go 1.25.0\n\nuse .\n")
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+vukaModPath+"\n\ngo 1.25.0\n")
	t.Chdir(dir)
	t.Setenv("GOWORK", "")
	t.Cleanup(func() { staleToolchainOnce = sync.Once{} })
	staleToolchainOnce = sync.Once{}
	var errb bytes.Buffer
	warnStaleToolchain(&errb)
	warnStaleToolchain(&errb)
	if n := strings.Count(errb.String(), "warning: "); n > 1 {
		t.Errorf("%d warnings:\n%s", n, errb.String())
	}
}
