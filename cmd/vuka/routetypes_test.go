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
	// lastFacts are the packages whose facts the last build recorded.
	lastFacts := func() []string {
		log := r.log.String()
		log = log[:strings.LastIndex(log, "page packages assembled")]
		log = log[strings.LastIndex(log, "prepare routes"):]
		var out []string
		for _, m := range regexp.MustCompile(`route facts of example\.com/app/(\S+) `).FindAllStringSubmatch(log, -1) {
			out = append(out, m[1])
		}
		return out
	}
	get("/a", "a one")
	a := filepath.Join(root, "pages", "a", "index.vuka.html")
	for i, s := range []string{"a two", "a three"} {
		edit(a, []string{"a one", "a two"}[i], s)
		get("/a", s)
		get("/b", "b one")
		get("/", "home one")
		r.waitLog("● rebuilt in ", i+1)
		if got := lastFacts(); !slices.Equal(got, []string{"pages/a"}) {
			t.Errorf("a body edit recorded the facts of %v:\n%s", got, r.log.String())
		}
	}
	edit(filepath.Join(root, "pages", "b", "index.vuka.html"), "func (p *Index) Text()", "func (p *Index) More() int { return 1 }\n\nfunc (p *Index) Text()")
	edit(filepath.Join(root, "pages", "b", "index.vuka.html"), "b one", "b two")
	get("/b", "b two")
	get("/a", "a three")
	r.waitLog("● rebuilt in ", 3)
	if got := lastFacts(); !slices.Contains(got, "pages/b") || slices.Contains(got, "pages/a") {
		t.Errorf("a declaration in b recorded the facts of %v:\n%s", got, r.log.String())
	}
	if strings.Contains(r.log.String(), "page packages typed") {
		t.Errorf("the route check type-checked page packages:\n%s", r.log.String())
	}
}
