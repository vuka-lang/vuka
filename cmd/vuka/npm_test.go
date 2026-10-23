package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/diag"
)

func TestNPMMajor(t *testing.T) {
	for _, c := range []struct {
		out   string
		major int
		ok    bool
	}{
		{"10.8.2\n", 10, true},
		{"11.21.1", 11, true},
		{"v9.0.0", 9, true},
		{"  12.0.0-pre.1  \n", 12, true},
		{"npm warn config something\n10.9.9\n", 10, true},
		{"", 0, false},
		{"npm noise 8", 0, false},
		{"eleven", 0, false},
		{"11", 0, false},
	} {
		major, ok := npmMajor(c.out)
		if major != c.major || ok != c.ok {
			t.Errorf("npmMajor(%q) = %d, %v; want %d, %v", c.out, major, ok, c.major, c.ok)
		}
	}
	if !npmTooOld("10.8.2") || npmTooOld("11.0.0") || npmTooOld("") {
		t.Error("npmTooOld")
	}
}

// fakeNPMVersion puts an npm on PATH that prints version for --version and
// fails anything else.
func fakeNPMVersion(t *testing.T, version string) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "npm"), "#!/bin/sh\nif [ \"$1\" = --version ]; then echo '"+version+"'; exit 0; fi\nexit 1\n")
	os.Chmod(filepath.Join(bin, "npm"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCheckNPM(t *testing.T) {
	var w bytes.Buffer
	fakeNPMVersion(t, "10.8.2")
	err := checkNPM([]string{"npm", "install"}, "web/", &w)
	var d *diag.Diagnostic
	if !errors.As(err, &d) || d.Code != "E0610" || !strings.Contains(d.Message, "npm 10.8.2 can't install web/") || !strings.Contains(d.Help, "npm install -g npm@11") {
		t.Errorf("npm 10 without a lockfile: %v", err)
	}
	if err := checkNPM([]string{"npm", "ci"}, "web/", &w); err != nil {
		t.Errorf("npm 10 with a lockfile: %v", err)
	}
	if got := w.String(); !strings.HasPrefix(got, "warning: npm 10.8.2 installs web/ from its lockfile") || !strings.Contains(got, "npm install -g npm@11") {
		t.Errorf("npm 10 with a lockfile warned %q", got)
	}
	w.Reset()
	for _, cmd := range [][]string{{"pnpm", "install"}, {"yarn", "install", "--immutable"}, {"bun", "install"}} {
		if err := checkNPM(cmd, "web/", &w); err != nil || w.Len() > 0 {
			t.Errorf("%v: %v %q", cmd, err, w.String())
		}
	}
	for _, v := range []string{"11.21.1", "12.0.0", "not a version"} {
		fakeNPMVersion(t, v)
		if err := checkNPM([]string{"npm", "install"}, "web/", &w); err != nil || w.Len() > 0 {
			t.Errorf("npm %s: %v %q", v, err, w.String())
		}
	}
}

// prepareFrontend stops before running npm 10 without a lockfile, and runs
// it (warning) with one.
func TestPrepareFrontendOldNPM(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "web/package.json": "{}"})
	dir := filepath.Join(root, "web")
	fakeNPMVersion(t, "10.9.9")
	var out bytes.Buffer
	err := prepareFrontend(root, dir, &out)
	if err == nil || !strings.Contains(err.Error(), "npm 10.9.9 can't install web/") || strings.Contains(out.String(), "installing") {
		t.Errorf("no lockfile: %v\n%s", err, out.String())
	}
	writeFile(t, filepath.Join(dir, "package-lock.json"), "{}")
	out.Reset()
	err = prepareFrontend(root, dir, &out) // the fake fails npm ci
	if err == nil || !strings.Contains(out.String(), "warning: npm 10.9.9 installs web/ from its lockfile") || !strings.Contains(out.String(), "npm ci failed") {
		t.Errorf("a lockfile: %v\n%s", err, out.String())
	}
}

// TestScaffoldLockfiles: every package.json vuka new writes has a
// package-lock.json beside it for the same packages, so npm ci installs it
// (npm 10 can't resolve these sets without one). After changing a
// scaffold package.json, run scripts/lockfiles.sh (npm 11).
func TestScaffoldLockfiles(t *testing.T) {
	type pkg struct {
		Name                          string
		Dependencies, DevDependencies map[string]string
	}
	seen := 0
	for _, a := range []map[string]string{
		{"frontend": "vue"}, {"frontend": "react"}, {"frontend": "svelte"},
		{"frontend": "none", "islands": "yes"},
	} {
		s := &scaffold{Dir: "shop", Module: "example.com/my-shop", Name: "Shop"}
		maps.Copy(a, map[string]string{"ui": "live", "db": "none", "mail": "no", "auth": "no"})
		if err := s.answer(a, nil); err != nil {
			t.Fatal(err)
		}
		files, err := s.render()
		if err != nil {
			t.Fatal(err)
		}
		for name, b := range files {
			if path.Base(name) != "package.json" {
				continue
			}
			seen++
			lockName := path.Join(path.Dir(name), "package-lock.json")
			lb, ok := files[lockName]
			if !ok {
				t.Errorf("%v: %s has no %s", a, name, lockName)
				continue
			}
			var p pkg
			var lock struct {
				Name            string
				LockfileVersion int
				Packages        map[string]pkg
			}
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := json.Unmarshal(lb, &lock); err != nil {
				t.Fatalf("%s: %v", lockName, err)
			}
			top := lock.Packages[""]
			switch {
			case lock.LockfileVersion != 3:
				t.Errorf("%s: lockfileVersion %d, want 3", lockName, lock.LockfileVersion)
			case lock.Name != p.Name || top.Name != p.Name:
				t.Errorf("%s: name %q/%q, package.json's %q", lockName, lock.Name, top.Name, p.Name)
			case !maps.Equal(top.Dependencies, p.Dependencies) || !maps.Equal(top.DevDependencies, p.DevDependencies):
				t.Errorf("%s is out of date with %s (run scripts/lockfiles.sh):\nlock %v %v\npackage.json %v %v",
					lockName, name, top.Dependencies, top.DevDependencies, p.Dependencies, p.DevDependencies)
			}
			for dep := range maps.Keys(p.Dependencies) {
				if _, ok := lock.Packages["node_modules/"+dep]; !ok {
					t.Errorf("%s doesn't lock %s", lockName, dep)
				}
			}
			for dep := range maps.Keys(p.DevDependencies) {
				if _, ok := lock.Packages["node_modules/"+dep]; !ok {
					t.Errorf("%s doesn't lock %s", lockName, dep)
				}
			}
		}
	}
	if seen != 4 {
		t.Errorf("%d package.json files, want 4 (one per frontend and the islands' web/)", seen)
	}
}
