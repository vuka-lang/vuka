package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestWatchRouteTypes: under watch, a page package's route facts are
// recorded when it is transpiled, and no page package is type-checked
// again for the route table: a body edit records the edited package's
// facts alone, a new declaration its package's (and those importing it);
// every page is served as edited.
func TestWatchRouteTypes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the vuka command and an app")
	}
	vuka := buildVuka(t)
	page := func(pkg, typ, text string) string {
		return fmt.Sprintf("<script lang=\"go\">\npackage %s\n\ntype %s struct{}\n\nfunc (p *%s) Text() string { return %q }\n</script>\n\n<main><span>{p.Text()}</span></main>\n", pkg, typ, typ, text)
	}
	root := localModule(t, webCheckout(t), map[string]string{
		"main.go":                 e2eMain,
		"pages/index.vuka.html":   page("pages", "Index", "home one"),
		"pages/a/index.vuka.html": page("a", "Index", "a one"),
		"pages/b/index.vuka.html": page("b", "Index", "b one"),
	})
	r := startWatch(t, vuka, root, []string{"GOFLAGS=-mod=mod", "VUKA_WATCH_TRACE=1"})
	get := func(path, want string) {
		t.Helper()
		var body string
		for end := time.Now().Add(3 * time.Minute); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", r.port, path))
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if body = string(b); strings.Contains(body, want) {
				return
			}
		}
		t.Fatalf("no %q at %s: %q; output:\n%s", want, path, body, r.log.String())
	}
	edit := func(path, old, new string) {
		t.Helper()
		src, _ := os.ReadFile(path)
		if !strings.Contains(string(src), old) {
			t.Fatalf("%s has no %q", path, old)
		}
		writeFile(t, path, strings.Replace(string(src), old, new, 1))
	}
	// settle waits until the watch has started no build for a while: it
	// never builds again on what it writes itself.
	settle := func() {
		t.Helper()
		n := -1
		for end := time.Now().Add(time.Minute); time.Now().Before(end); time.Sleep(1500 * time.Millisecond) {
			m := strings.Count(r.log.String(), "prepare routes")
			if m == n {
				return
			}
			n = m
		}
		t.Fatalf("the watch kept building:\n%s", r.log.String())
	}
	// since are the packages whose facts the builds after the log's offset
	// recorded, and how many builds those were.
	since := func(offset int) (pkgs []string, builds int) {
		log := r.log.String()[offset:]
		for _, m := range regexp.MustCompile(`route facts of example\.com/app/(\S+) `).FindAllStringSubmatch(log, -1) {
			pkgs = append(pkgs, m[1])
		}
		slices.Sort(pkgs)
		return slices.Compact(pkgs), strings.Count(log, "prepare routes")
	}
	a := filepath.Join(root, "pages", "a", "index.vuka.html")
	// The watch builds again soon after it starts, the go command having
	// completed the module's go.sum, which keys its transpile: an edit then
	// may land in that build. One edit first, and the watch settled.
	get("/a", "a one")
	edit(a, "a one", "a two")
	get("/a", "a two")
	settle()
	for i, s := range []string{"a three", "a four"} {
		offset := len(r.log.String())
		edit(a, []string{"a two", "a three"}[i], s)
		get("/a", s)
		get("/b", "b one")
		get("/", "home one")
		settle()
		if got, builds := since(offset); !slices.Equal(got, []string{"pages/a"}) || builds != 1 {
			t.Errorf("a body edit ran %d builds recording the facts of %v:\n%s", builds, got, r.log.String())
		}
	}
	offset := len(r.log.String())
	edit(filepath.Join(root, "pages", "b", "index.vuka.html"), "func (p *Index) Text()", "func (p *Index) More() int { return 1 }\n\nfunc (p *Index) Text()")
	edit(filepath.Join(root, "pages", "b", "index.vuka.html"), "b one", "b two")
	get("/b", "b two")
	get("/a", "a four")
	settle()
	if got, builds := since(offset); !slices.Contains(got, "pages/b") || slices.Contains(got, "pages/a") || builds > 2 {
		t.Errorf("a declaration in b ran %d builds recording the facts of %v:\n%s", builds, got, r.log.String())
	}
	if strings.Contains(r.log.String(), "page packages typed") {
		t.Errorf("the route check type-checked page packages:\n%s", r.log.String())
	}
}
