package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIslandsGoE2E builds examples/islands-go, Go with a Vue island placed
// in a live page and a page backed by its component, as its go:generate
// lines do (vuka gen -inplace, vuka build-islands) and then with go build:
// the binary embeds the islands, which mount in Chrome. Under VUKA_DEV=1,
// web.Dev runs vuka build-islands -watch and the islands mount from there.
func TestIslandsGoE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs an app")
	}
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	if _, err := exec.LookPath("node"); err != nil {
		needs(t, "no node")
	}
	repo, _ := filepath.Abs("../..")
	web, ui := os.Getenv("VUKA_WEB"), os.Getenv("VUKA_UI")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if !hasWebFunc(web, "distAssets") {
		t.Skipf("github.com/vuka-lang/web/v2 at %s serves no islands from a frontend's build; set VUKA_WEB", web)
	}
	if src, _ := os.ReadFile(filepath.Join(ui, "island.go")); !bytes.Contains(src, []byte("func RegisterIsland[T any](args ...any)")) {
		t.Skipf("github.com/vuka-lang/ui/v2 at %s has no bare RegisterIsland; set VUKA_UI", ui)
	}
	nm := islandModules(t)
	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "vuka"), ".").CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	files := map[string]string{}
	for _, name := range []string{"main.go", "pages/chart.go", "pages/index.vuka.html", "pages/users/_layout.vuka.html", "pages/users/[id].vuka.html",
		"web/islands/Chart.vue", "web/islands/Profile.vue", "web/dist/.gitkeep"} {
		src, err := os.ReadFile(filepath.Join(repo, "examples/islands-go", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = strings.ReplaceAll(string(src), "example.com/islands-go/", "example.com/app/")
	}
	root := localModule(t, web, files)
	if err := os.Symlink(nm, filepath.Join(root, "web", "node_modules")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	if err := gen([]string{"-inplace", "./..."}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	if err := buildIslands(nil, io.Discard, os.Stderr); err != nil {
		t.Fatalf("vuka build-islands: %v", err)
	}
	app := filepath.Join(bin, "app")
	if out, err := exec.Command("go", "build", "-o", app, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ch := launchChrome(t)
	check := func(base string, logs *bytes.Buffer) {
		t.Helper()
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("app log:\n%s", logs)
			}
		})
		p := ch.openChecked(base+"/", 900, 700)
		p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
		p.wait(`(document.querySelector("#chart .title") || {}).textContent === "Sales"`)
		p.click("#add")
		p.wait(`document.querySelector("#chart .points").textContent === "3,1,4,3"`)
		p = ch.openChecked(base+"/users/2", 900, 700)
		p.wait(`(document.querySelector("#profile .name") || {}).textContent === "Grace"`)
		if id := p.text("#profile .id"); id != "2" {
			t.Errorf("profile id %q", id)
		}
	}

	// Embedded: the islands come from the binary; a build of islands only
	// answers other paths 404.
	base, logs := runApp(t, app, root, "VUKA_DEV=", "VUKA_ENV=")
	check(base, logs)
	if res, err := http.Get(base + "/nowhere"); err == nil {
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("GET /nowhere: %d, want 404", res.StatusCode)
		}
	}

	// web.Dev: vuka build-islands -watch keeps them in .vuka/assets.
	if err := os.RemoveAll(filepath.Join(root, "web/dist/.vuka")); err != nil {
		t.Fatal(err)
	}
	base, logs = runApp(t, app, root, "VUKA_DEV=1", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	check(base, logs)
	if _, err := os.Stat(filepath.Join(root, ".vuka/assets/manifest.json")); err != nil {
		t.Errorf("web.Dev built no islands in .vuka/assets: %v", err)
	}
}

// runApp runs bin in dir on a free port, with env, until the test ends.
func runApp(t *testing.T, bin, dir string, env ...string) (string, *bytes.Buffer) {
	t.Helper()
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "PORT="+port, assetsEnv+"="), env...)
	logs := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
	base := fmt.Sprintf("http://127.0.0.1:%s", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/"); return err == nil && res.Body.Close() == nil })
	return base, logs
}
