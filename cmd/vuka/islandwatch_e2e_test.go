package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestRunWatchIslandFailure: under vuka run -watch an island that stops
// building shows in the toolbar's build state with the build's error, the
// loader knows (an island it doesn't have says why) and keeps the last good
// bundle; building again clears it.
func TestRunWatchIslandFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	web := webDir(t)
	vuka := buildVuka(t)
	t.Setenv("GOFLAGS", "-mod=mod")
	root := localModule(t, web, map[string]string{
		"main.vuka":          islandPage,
		"islands/Counter.ts": counterTS,
		"islands/lib.ts":     "export const twice = (n: number): number => n * 2;\n",
	})
	r := startWatch(t, vuka, root, nil)
	get := func(path string) string {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", r.port, path))
		if err != nil {
			return ""
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	type assets struct {
		Failed  bool   `json:"failed"`
		Islands bool   `json:"islands"`
		Output  string `json:"output"`
	}
	state := func() (st struct {
		State  string  `json:"state"`
		Assets *assets `json:"assets"`
	}) {
		json.Unmarshal([]byte(get("/_vuka/toolbar/build")), &st)
		return st
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(90 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s; output:\n%s", what, r.log.String())
			}
		}
	}
	waitFor("the app's first build", func() bool { return state().State == "ok" })
	loader := get("/_vuka/c/loader.js")
	if st := state(); st.Assets != nil || !strings.Contains(loader, `"Counter":["/_vuka/i/counter.`) || strings.Contains(loader, `"x":true`) {
		t.Fatalf("before: %+v %s", st.Assets, loader)
	}
	src := filepath.Join(root, "islands", "Counter.ts")
	edit := func(s string) {
		os.WriteFile(src, []byte(s), 0o644)
		later := time.Now().Add(time.Second)
		os.Chtimes(src, later, later)
	}
	edit(strings.Replace(counterTS, "return {", "return {{", 1))
	waitFor("no island failure in the toolbar", func() bool { st := state(); return st.Assets != nil && st.Assets.Failed })
	st := state()
	if !st.Assets.Islands || !strings.Contains(st.Assets.Output, "Counter") || strings.Contains(st.Assets.Output, "\x1b[") || st.State != "ok" {
		t.Errorf("the island failure: %+v %+v", st.State, st.Assets)
	}
	if l := get("/_vuka/c/loader.js"); !strings.Contains(l, `"x":true`) || !strings.Contains(l, `"Counter":["/_vuka/i/counter.`) {
		t.Errorf("the loader during the failure: %s", l)
	}
	edit(counterTS)
	waitFor("the failure stayed after a good build", func() bool { return state().Assets == nil })
	if l := get("/_vuka/c/loader.js"); strings.Contains(l, `"x":true`) {
		t.Errorf("the loader after: %s", l)
	}
	r.stop()
}

// TestRunWatchIslandImport: under vuka run -watch an island is bundled again
// when a file it imports changes, not only its own.
func TestRunWatchIslandImport(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	web := webDir(t)
	vuka := buildVuka(t)
	t.Setenv("GOFLAGS", "-mod=mod")
	root := localModule(t, web, map[string]string{
		"main.vuka":          islandPage,
		"islands/Counter.ts": counterTS,
		"islands/lib.ts":     "export const twice = (n: number): number => n * 2;\n",
	})
	r := startWatch(t, vuka, root, nil)
	module := regexp.MustCompile(`"Counter":\["/_vuka/i/(counter\.[0-9a-f]+\.js)`)
	bundle := func() string {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_vuka/c/loader.js", r.port))
		if err != nil {
			return ""
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if m := module.FindSubmatch(b); m != nil {
			return string(m[1])
		}
		return ""
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(30 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s; output:\n%s", what, r.log.String())
			}
		}
	}
	waitFor("the island's first bundle", func() bool { return bundle() != "" })
	first := bundle()
	time.Sleep(2 * time.Second) // past the first builds: the change alone must rebuild it
	lib := filepath.Join(root, "islands", "lib.ts")
	os.WriteFile(lib, []byte("export const twice = (n: number): number => n * 3;\n"), 0o644)
	later := time.Now().Add(time.Second)
	os.Chtimes(lib, later, later)
	waitFor("no new bundle after the imported file changed", func() bool { b := bundle(); return b != "" && b != first })
	r.stop()
}
