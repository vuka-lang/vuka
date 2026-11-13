package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/semver"
)

// TestUIRegistryRequiresResolve: each version the registry's items require
// of vuka's libraries is one the go command resolves, as written (a tag,
// or the pseudo-version of a commit not yet in one), and no newer than the
// library's main: an item asking for a tag that doesn't exist yet would
// refuse every app tracking main, whose pseudo-versions sort below it.
func TestUIRegistryRequiresResolve(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("asks the module proxy")
	}
	reg, err := readRegistry(regSource{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/resolve\n\ngo 1.25\n")
	resolve := func(query string) (string, error) {
		c := exec.Command("go", "list", "-m", "-json", query)
		c.Dir = dir
		c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		out, err := c.CombinedOutput()
		if err != nil {
			return "", &exec.ExitError{Stderr: out}
		}
		var m struct{ Version string }
		if err := json.Unmarshal(out, &m); err != nil {
			return "", err
		}
		return m.Version, nil
	}
	mains := map[string]string{}
	checked := map[string]bool{}
	for _, it := range reg.Items {
		for _, lib := range slices.Sorted(mapKeys(it.Requires)) {
			p, v := requireModules[lib], it.Requires[lib]
			if mains[p] == "" {
				m, err := resolve(p + "@main")
				if err != nil {
					needs(t, "can't resolve %s@main: %s", p, err.(*exec.ExitError).Stderr)
				}
				mains[p] = m
			}
			if semver.Compare(v, mains[p]) > 0 {
				t.Errorf("%s requires %s %s, newer than its main (%s): require the pseudo-version of the commit that added what it needs", it.Name, p, v, mains[p])
				continue
			}
			if checked[p+"@"+v] {
				continue
			}
			checked[p+"@"+v] = true
			got, err := resolve(p + "@" + v)
			switch {
			case err != nil:
				t.Errorf("%s requires %s %s, which doesn't resolve: %s", it.Name, p, v, strings.TrimSpace(string(err.(*exec.ExitError).Stderr)))
			case got != v:
				t.Errorf("%s requires %s %s, which resolves as %s: write that", it.Name, p, v, got)
			}
		}
	}
}

// TestUIRequiresPseudo: an app tracking main, its ui a pseudo-version
// newer than an item's pseudo-version, adds it; one older is refused with
// the go get of the exact version the item needs.
func TestUIRequiresPseudo(t *testing.T) {
	dir := uiApp(t)
	mustUI(t, dir, "init")
	mustUI(t, dir, "add", "dialog", "badge")
	old := uiApp(t)
	writeFile(t, filepath.Join(old, "go.mod"), "module example.com/shop\n\ngo 1.25\n\nrequire github.com/vuka-lang/ui/v2 v2.16.2-0.20261106031119-d0334320cb89\n")
	mustUI(t, old, "init")
	mustUI(t, old, "add", "badge")
	_, err := runUI(t, old, "add", "dialog")
	if want := "go get github.com/vuka-lang/ui/v2@v2.17.1-0.20261107182035-d82a0590bd58"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("an older main: %v, want %q", err, want)
	}
}
