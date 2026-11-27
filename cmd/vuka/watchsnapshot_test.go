package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestWatchSnapshot: each watch build reads the module through a snapshot
// of its own, and sees what changed since the last: a markup edit still
// skips go build; an edited Go file, an added one and a removed one are
// each served.
func TestWatchSnapshot(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the vuka command and an app")
	}
	vuka := buildVuka(t)
	root := localModule(t, webCheckout(t), map[string]string{
		"main.go": e2eMain,
		"pages/index.vuka.html": `<script lang="go">
package pages

import "example.com/app/components"

type Index struct{}
</script>

<main><components.Card Title="Hello" /><span id="label">{components.Label()}</span></main>
`,
		"components/label.go": "package components\n\nfunc Label() string { return \"label one\" }\n",
		"components/card.vuka.html": `<script lang="go">
package components

type Card struct {
	Title string
}
</script>

<section class="card">
  <h2>{Title}</h2>
  <p>The card says one.</p>
</section>
`,
	})
	env := []string{"GOFLAGS=-mod=mod", "VUKA_WATCH_TRACE=1"}
	var r *watchRun
	page := func(want ...string) {
		t.Helper()
		var body string
		for end := time.Now().Add(3 * time.Minute); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", r.port))
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			body = string(b)
			if !slices.ContainsFunc(want, func(w string) bool { return !strings.Contains(body, w) }) {
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
	skips := func() int { return strings.Count(r.log.String(), "go build skipped") }
	card := filepath.Join(root, "components", "card.vuka.html")
	label := filepath.Join(root, "components", "label.go")
	index := filepath.Join(root, "pages", "index.vuka.html")
	// The first run's go build tidies go.mod (-mod=mod); a run after it has
	// nothing to tidy.
	r = startWatch(t, vuka, root, env)
	page("The card says one.", "label one")
	r.stop()
	r = startWatch(t, vuka, root, env)
	page("The card says one.", "label one")

	edit(card, "says one", "says two")
	page("The card says two.", "label one")
	r.waitLog("go build skipped", 1)

	edit(label, "label one", "label two")
	page("label two", "The card says two.")
	if n := skips(); n != 1 {
		t.Fatalf("go build skipped for an edit of Go (%d skips):\n%s", n, r.log.String())
	}

	writeFile(t, filepath.Join(root, "components", "extra.go"), "package components\n\nfunc Extra() string { return \"extra one\" }\n")
	edit(index, `{components.Label()}</span>`, `{components.Label()}</span><b>{components.Extra()}</b>`)
	page("<b>extra one</b>", "label two")

	// Were extra.go still read, Extra would be declared twice.
	if err := os.Remove(filepath.Join(root, "components", "extra.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "components", "more.go"), "package components\n\nfunc Extra() string { return \"extra two\" }\n")
	page("<b>extra two</b>", "label two")

	edit(card, "says two", "says three")
	page("The card says three.", "<b>extra two</b>", "label two")
	r.waitLog("go build skipped", 2)
	if strings.Contains(r.log.String(), "warning: go build was skipped") {
		t.Fatalf("a skipped go build came out different:\n%s", r.log.String())
	}
}
