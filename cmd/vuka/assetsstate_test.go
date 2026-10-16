package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFailedAssets(t *testing.T) {
	vite := islandError{&viteError{msg: "vite islands (islands/Levels.vue): vite build: exit status 1\n", tail: []string{"\x1b[31merror\x1b[39m [vite:vue] Unexpected token"}}}
	f := failedAssets(errors.Join(errors.New("app.css: bad"), vite))
	want := "app.css: bad\nvite islands (islands/Levels.vue): vite build: exit status 1\nerror [vite:vue] Unexpected token\n"
	if !f.Failed || !f.Islands || f.Output != want {
		t.Errorf("got %+v\nwant output %q", f, want)
	}
	if f := failedAssets(errors.Join(errors.New("app.css: bad"))); f.Islands {
		t.Errorf("no island failed: %+v", f)
	}
}

// Under vuka run an island that stops building leaves the last good build
// served and says why beside the manifest; building again clears it.
func TestDevAssetsIslandFailure(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "islands", "Counter.ts")
	os.MkdirAll(filepath.Dir(src), 0o755)
	good := "export function mount(el: HTMLElement) { el.textContent = 'n'; return {} }\n"
	os.WriteFile(src, []byte(good), 0o644)
	pa := &pageAssets{root: root, global: &assetComp{}, islands: []*islandComp{{
		name: "Counter", src: src, rel: "islands/Counter.ts", label: "page.vuka:3", framework: "vanilla",
	}}}
	var stderr bytes.Buffer
	d := newDevAssets(root, &stderr)
	defer d.close()
	d.set(pa)
	state := filepath.Join(d.dir(), assetsState)
	module := func() string {
		var mf manifestFile
		b, _ := os.ReadFile(filepath.Join(d.dir(), assetsManifest))
		json.Unmarshal(b, &mf)
		if mf.Islands["Counter"] == nil {
			return ""
		}
		return mf.Islands["Counter"].Module
	}
	first := module()
	if first == "" || fileExists(state) {
		t.Fatalf("the first build: module %q, state %v\n%s", first, fileExists(state), stderr.String())
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s\n%s", what, stderr.String())
			}
		}
	}
	touch := func(s string) {
		os.WriteFile(src, []byte(s), 0o644)
		later := time.Now().Add(time.Second)
		os.Chtimes(src, later, later)
	}
	touch("export function mount(el: HTMLElement) { el.textContent = ; }\n")
	waitFor("no state after a failed build", func() bool { return fileExists(state) })
	var f assetsFailure
	b, _ := os.ReadFile(state)
	json.Unmarshal(b, &f)
	if !f.Failed || !f.Islands || !strings.Contains(f.Output, "Counter") {
		t.Errorf("the state: %s", b)
	}
	if module() != first || !fileExists(filepath.Join(d.dir(), "i", first)) {
		t.Errorf("the last good build isn't served: %q", module())
	}
	touch(good + "// again\n")
	waitFor("the state stayed after a good build", func() bool { return !fileExists(state) })
	if module() == "" || module() == first {
		t.Errorf("the new build: %q", module())
	}
}
