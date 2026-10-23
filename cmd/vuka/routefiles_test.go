package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunWritesRouteFiles: vuka run writes the route files hand-written
// code imports, pages/route.go and pages/urls/urls_gen.go, to disk, and
// nothing else it generates.
func TestRunWritesRouteFiles(t *testing.T) {
	app := routesModule(t, nil)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	checkRouteFilesOnDisk(t, app, "/orgs/{org}")
	noGoDirs(t, app)
}

// TestRunWatchWritesRouteFiles: vuka run -watch writes the route files
// and writes route.go again when a page is added.
func TestRunWatchWritesRouteFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	vuka := buildVuka(t)
	app := routesModule(t, map[string]string{"main.go": "package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/app/pages\"\n)\n\nfunc main() {\n\tweb.New(pages.Routes())\n\tselect {}\n}\n"})
	r := startWatch(t, vuka, app, nil)
	waitFile(t, r, filepath.Join(app, "pages", "route.go"), `"/orgs/{org}"`)
	writeFile(t, filepath.Join(app, "pages", "pets", "index.vuka.html"), "package pets\n\n// Index lists the pets.\ntype Index struct{}\n<p>pets</p>\n")
	waitFile(t, r, filepath.Join(app, "pages", "route.go"), `"/pets"`)
	waitFile(t, r, filepath.Join(app, "pages", "urls", "urls_gen.go"), "Pets")
	r.stop()
	checkRouteFilesOnDisk(t, app, "/pets")
}

func waitFile(t *testing.T, r *watchRun, path, want string) {
	t.Helper()
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return
		}
		if time.Now().After(end) {
			b, _ := os.ReadFile(path)
			t.Fatalf("%s never had %s:\n%s\noutput:\n%s", path, want, b, r.log.String())
		}
	}
}

// checkRouteFilesOnDisk: the route files are on disk, route.go with path,
// and no other generated file is.
func checkRouteFilesOnDisk(t *testing.T, app, path string) {
	t.Helper()
	route, err := os.ReadFile(filepath.Join(app, "pages", "route.go"))
	if err != nil || !strings.Contains(string(route), `"`+path+`"`) {
		t.Errorf("pages/route.go: %v\n%s", err, route)
	}
	if _, err := os.Stat(filepath.Join(app, "pages", "urls", "urls_gen.go")); err != nil {
		t.Errorf("no pages/urls/urls_gen.go: %v", err)
	}
	filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, "_html.go") || strings.HasSuffix(p, "_vuka.go")) {
			t.Errorf("vuka run wrote %s", p)
		}
		return nil
	})
}
