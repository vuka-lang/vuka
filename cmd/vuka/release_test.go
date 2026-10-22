package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeToolchain = `package main

import "fmt"

const (
	version     = "v1.5.0"
	vukaVersion = "v1.5.0"
	uiVersion   = "v1.5.0"
	webVersion  = "v1.5.0"
)

func main() {
	if vukaVersion == version && uiVersion == version && webVersion == version {
		fmt.Printf("vuka %s (vuka, ui, web %s)\n", version, vukaVersion)
		return
	}
	fmt.Printf("vuka %s (vuka %s, ui %s, web %s)\n", version, vukaVersion, uiVersion, webVersion)
}
`

// releaseFixture is the set's three repositories as clones of bare
// remotes, which git reaches in place of https://github.com/vuka-lang/…, and
// a module cache of their own, so a release runs without the network.
type releaseFixture struct {
	t             *testing.T
	remotes       string
	vuka, ui, web string
}

func newReleaseFixture(t *testing.T, notes string) *releaseFixture {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	t.Cleanup(func() { // the module cache is read-only
		filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err == nil {
				os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	f := &releaseFixture{t: t, remotes: filepath.Join(base, "remotes")}
	gitconfig := filepath.Join(base, "gitconfig")
	writeFile(t, gitconfig, "[user]\n\tname = Test\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n"+
		"[url \"file://"+f.remotes+"/\"]\n\tinsteadOf = https://github.com/vuka-lang/\n")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GOMODCACHE", filepath.Join(base, "modcache"))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPRIVATE", "github.com/vuka-lang")

	work := filepath.Join(base, "work")
	f.vuka = f.repo(work, "vuka", "vuka", map[string]string{
		"go.mod":                "module github.com/vuka-lang/vuka/v2\n\ngo 1.23\n",
		"vuka.go":               "package vuka\n",
		"docs/releases.md":      "# Releases\n\n" + notes,
		"cmd/vuka/go.mod":       "module github.com/vuka-lang/vuka/cmd/vuka/v2\n\ngo 1.23\n",
		"cmd/vuka/toolchain.go": fakeToolchain,
		"cmd/vukavet/go.mod":    "module github.com/vuka-lang/vuka/cmd/vukavet/v2\n\ngo 1.23\n",
		"cmd/vukavet/main.go":   "package main\n\nfunc main() {}\n",
	}, "v1.5.0", "cmd/vuka/v1.5.0")
	f.ui = f.repo(work, "vuka-ui", "ui", map[string]string{
		"go.mod": "module github.com/vuka-lang/ui/v2\n\ngo 1.23\n",
		"ui.go":  "package ui\n",
	}, "v1.5.0")
	f.web = f.repo(work, "vuka-web", "web", map[string]string{
		"go.mod": "module github.com/vuka-lang/web/v2\n\ngo 1.23\n",
		"web.go": "package web\n",
	}, "v1.5.0")
	return f
}

func (f *releaseFixture) git(dir string, args ...string) string {
	f.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo makes the remote name and its clone work/dir with files, tagged.
func (f *releaseFixture) repo(work, dir, name string, files map[string]string, tags ...string) string {
	f.t.Helper()
	remote := filepath.Join(f.remotes, name)
	os.MkdirAll(remote, 0o755)
	f.git(remote, "init", "-q", "--bare")
	clone := filepath.Join(work, dir)
	os.MkdirAll(clone, 0o755)
	f.git(clone, "init", "-q")
	for p, src := range files {
		writeFile(f.t, filepath.Join(clone, p), src)
	}
	f.git(clone, "add", "-A")
	f.git(clone, "commit", "-q", "-m", "start")
	f.git(clone, "remote", "add", "origin", remote)
	for _, tag := range tags {
		f.git(clone, "tag", "-a", tag, "-m", tag)
	}
	f.git(clone, "push", "-q", "origin", "main", "--tags")
	f.git(clone, "fetch", "-q", "origin")
	return clone
}

func (f *releaseFixture) release(args ...string) (string, error) {
	f.t.Helper()
	f.t.Chdir(filepath.Join(f.vuka, "cmd", "vuka"))
	var out bytes.Buffer
	err := release(append([]string{"-no-test", "-proxy-wait=0"}, args...), &out, &out)
	return out.String(), err
}

const notes50 = "## v2.0.0 — 2026-10-26\n\nOne version for the toolchain.\n"

func TestReleaseDryRun(t *testing.T) {
	f := newReleaseFixture(t, notes50)
	out, err := f.release("v2.0.0")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"ok   v2.0.0 follows every tag of the set (v1.5.0)",
		"ok   docs/releases.md has a section for v2.0.0",
		"1. tag vuka v2.0.0",
		"ui: go get github.com/vuka-lang/vuka/v2@v2.0.0; go build; commit",
		"web: go get github.com/vuka-lang/vuka/v2@v2.0.0 github.com/vuka-lang/ui/v2@v2.0.0",
		"cmd/vuka: go get github.com/vuka-lang/vuka/v2@v2.0.0; the toolchain constants to v2.0.0",
		"tag cmd/vuka cmd/vuka/v2.0.0",
		"tag cmd/vukavet cmd/vukavet/v2.0.0 on cmd/vuka/v2.0.0",
		"push cmd/vukavet/v2.0.0",
		"verify: GOPROXY=direct go install github.com/vuka-lang/vuka/cmd/vuka/v2@v2.0.0 and github.com/vuka-lang/vuka/cmd/vukavet/v2@v2.0.0",
		"dry run: nothing changed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if tags := f.git(f.vuka, "tag", "-l", "*v2.0.0"); tags != "" {
		t.Errorf("a dry run tagged %s", tags)
	}
}

func TestReleaseChecks(t *testing.T) {
	f := newReleaseFixture(t, "## v2.0.0 — unreleased\n")
	writeFile(t, filepath.Join(f.web, "stray.go"), "package web\n")
	f.git(f.ui, "checkout", "-q", "-b", "topic")
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"v0.40.0"}, []string{"set versions start at v0.50.0"}},
		{[]string{"v0.50"}, []string{"not a release version"}},
		{[]string{"v3.0.0"}, []string{"v3.0.0 needs /v3 module paths"}},
		{[]string{"v1.6.0"}, []string{"v1.6.0 is a v1 version, but the set's modules are v2"}},
		{[]string{"v2.0.0"}, []string{
			"FAIL ui is clean, on main, level with origin: on topic, not main",
			"FAIL web is clean, on main, level with origin: uncommitted changes",
			"warn the v2.0.0 section is marked unreleased",
		}},
		{[]string{"-yes", "v2.0.0"}, []string{"FAIL docs/releases.md has a section for v2.0.0: the v2.0.0 section is marked unreleased"}},
		{[]string{"v2.1.0"}, []string{"FAIL docs/releases.md has a section for v2.1.0: no \"## v2.1.0"}},
	} {
		out, err := f.release(c.args...)
		if err == nil {
			t.Errorf("release %v: no error\n%s", c.args, out)
		}
		for _, w := range c.want {
			if !strings.Contains(out+err.Error(), w) {
				t.Errorf("release %v: missing %q in:\n%s\n%v", c.args, w, out, err)
			}
		}
	}
}

func TestRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("runs git and go install")
	}
	f := newReleaseFixture(t, notes50)
	out, err := f.release("-yes", "v2.0.0")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for dir, want := range map[string]string{
		f.vuka: "cmd/vuka/v2.0.0\ncmd/vukavet/v2.0.0\nv2.0.0",
		f.ui:   "v2.0.0",
		f.web:  "v2.0.0",
	} {
		if got := f.git(filepath.Join(f.remotes, filepath.Base(remoteName(dir))), "tag", "-l", "*v2.0.0"); got != want {
			t.Errorf("%s remote tags %q, want %q", dir, got, want)
		}
	}
	webMod := readTest(t, filepath.Join(f.web, "go.mod"))
	if !strings.Contains(webMod, "github.com/vuka-lang/ui/v2 v2.0.0") || !strings.Contains(webMod, "github.com/vuka-lang/vuka/v2 v2.0.0") {
		t.Errorf("web's go.mod:\n%s", webMod)
	}
	if vet, cmd := f.git(f.vuka, "rev-list", "-n1", "cmd/vukavet/v2.0.0"), f.git(f.vuka, "rev-list", "-n1", "cmd/vuka/v2.0.0"); vet != cmd {
		t.Errorf("cmd/vukavet/v2.0.0 is on %s, cmd/vuka/v2.0.0 on %s", vet, cmd)
	}
	tc := readTest(t, filepath.Join(f.vuka, "cmd", "vuka", "toolchain.go"))
	if strings.Count(tc, `"v2.0.0"`) != 4 {
		t.Errorf("toolchain.go:\n%s", tc)
	}
	if !strings.Contains(out, "released the v2.0.0 set") {
		t.Errorf("output:\n%s", out)
	}
	if msg := f.git(f.web, "log", "-1", "--format=%s"); msg != "web v2.0.0: require vuka, ui v2.0.0" {
		t.Errorf("web's commit %q", msg)
	}

	// The next patch within the week is refused unless it is a security fix.
	writeFile(t, filepath.Join(f.vuka, "docs", "releases.md"), "# Releases\n\n## v2.0.1 — 2026-10-27\n\n"+notes50)
	f.git(f.vuka, "commit", "-qam", "notes")
	f.git(f.vuka, "push", "-q", "origin", "main")
	if out, err := f.release("v2.0.1"); err == nil || !strings.Contains(out, "patches ship weekly") {
		t.Errorf("a second patch within the week: %v\n%s", err, out)
	}
	if out, err := f.release("-security", "v2.0.1"); err != nil {
		t.Errorf("a security patch: %v\n%s", err, out)
	}
}

func remoteName(clone string) string {
	switch filepath.Base(clone) {
	case "vuka-ui":
		return "ui"
	case "vuka-web":
		return "web"
	}
	return "vuka"
}

func TestSetToolchain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "toolchain.go")
	writeFile(t, p, fakeToolchain)
	if err := setToolchain(p, "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := readTest(t, p); strings.Count(got, `"v2.0.0"`) != 4 || strings.Contains(got, "v1.5.0") {
		t.Errorf("toolchain.go:\n%s", got)
	}
	writeFile(t, p, "package main\n\nconst version = \"v1\"\n")
	if err := setToolchain(p, "v2.0.0"); err == nil {
		t.Error("no error for a file missing constants")
	}
	real, _ := os.ReadFile("toolchain.go")
	writeFile(t, p, string(real))
	if err := setToolchain(p, "v2.0.0"); err != nil {
		t.Errorf("this command's toolchain.go: %v", err)
	}
}
