package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWatchLocalReplace: with a dependency replaced by a directory, a watch
// keeps its caches (the shapes hash the dependency's sources), and an edit
// in the dependency reaches the page with the next build (the watch looks
// at the module only: a save in it starts the build): a body, then a new
// function the page calls, then a changed signature.
func TestWatchLocalReplace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and an app")
	}
	t.Setenv("GOWORK", "off")
	vuka := buildVuka(t)
	root := localModule(t, webCheckout(t), map[string]string{
		"main.go": e2eMain,
		"pages/index.vuka.html": `<script lang="go">
package pages

import "example.com/dep"

type Index struct{}
</script>

<main><span id="dep">{dep.Label()}</span></main>
`,
	})
	dep := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-dep")
	writeFile(t, filepath.Join(dep, "go.mod"), "module example.com/dep\n\ngo 1.25.0\n")
	label := filepath.Join(dep, "dep.go")
	writeFile(t, label, "package dep\n\nfunc Label() string { return \"dep one\" }\n")
	mod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	writeFile(t, filepath.Join(root, "go.mod"), strings.Replace(string(mod), "require (", "require (\n\texample.com/dep v0.0.0", 1)+
		"\nreplace example.com/dep => ../"+filepath.Base(dep)+"\n")

	r := startWatch(t, vuka, root, []string{"GOFLAGS=-mod=mod", "VUKA_WATCH_TRACE=1"})
	page := func(want string) {
		t.Helper()
		var body string
		for end := time.Now().Add(3 * time.Minute); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", r.port))
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if body = string(b); strings.Contains(body, want) {
				return
			}
		}
		t.Fatalf("no %q in the page %q; output:\n%s", want, body, r.log.String())
	}
	edit := func(path, old, new string) {
		t.Helper()
		src, _ := os.ReadFile(path)
		if !strings.Contains(string(src), old) {
			t.Fatalf("%s has no %q", path, old)
		}
		writeFile(t, path, strings.Replace(string(src), old, new, 1))
	}
	page("dep one")
	index := filepath.Join(root, "pages", "index.vuka.html")
	edit(index, `<main>`, `<main><p>text</p>`)
	page("<p>text</p>")
	r.waitLog("● rebuilt in ", 1)
	edit(index, `<p>text</p>`, `<p>text two</p>`)
	page("<p>text two</p>")
	r.waitLog("● rebuilt in ", 2)
	if !strings.Contains(r.log.String(), "transpiled 0 of") && !strings.Contains(r.log.String(), "transpiled 1 of") {
		t.Errorf("the session transpiled the module again:\n%s", r.log.String())
	}

	edit(label, `"dep one"`, `"dep two"`)
	edit(index, `<p>text two</p>`, `<p>text three</p>`) // the build
	page("dep two")
	edit(label, "func Label()", "func Other() string { return \"other\" }\n\nfunc Label()")
	edit(index, "{dep.Label()}", "{dep.Other()}")
	page(">other<")
	edit(label, "func Other() string { return \"other\" }", "func Other(n int) string { return fmt.Sprint(\"other \", n) }")
	edit(label, "package dep\n", "package dep\n\nimport \"fmt\"\n")
	edit(index, "{dep.Other()}", "{dep.Other(3)}")
	page(">other 3<")
}
