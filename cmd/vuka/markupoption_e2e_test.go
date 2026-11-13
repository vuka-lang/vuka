package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const optionUsers = `package users

import "github.com/vuka-lang/vuka/v2"

type User struct {
	Name     string
	Nick     vuka.Option[string]
	Verified vuka.Option[bool]
}

func All() []User {
	return []User{{Name: "ann", Nick: vuka.Some("a"), Verified: vuka.Some(true)}, {Name: "bob"}}
}
`

const optionBadge = `<script lang="go">
package components

// Badge shows a label.
type Badge struct{ Label string }
</script>

<b class="badge">{Label}</b>
`

const optionIndex = `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/users"
)

type Index struct{}
</script>

<main>
  for _, u := range users.All() {
    <p id={u.Name}>{u.Nick}<components.Badge label={u.Nick} />
      if u.Verified {
        <i>verified</i>
      }
    </p>
  }
</main>
`

// TestMarkupOptionE2E: a served page reads vuka.Options as their values:
// Some shows the value, None nothing, and never fails the page.
func TestMarkupOptionE2E(t *testing.T) {
	t.Parallel()
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	root := localModule(t, web, map[string]string{
		"main.go":                    genInferMain,
		"users/users.go":             optionUsers,
		"components/badge.vuka.html": optionBadge,
		"pages/index.vuka.html":      optionIndex,
	})

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := vukaIn(root, io.Discard, &stderr, "build", "-o", app, "."); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	base, _ := serve(t, root, app)
	res, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /: %d\n%s", res.StatusCode, body)
	}
	for _, want := range []string{`<p id="ann">a<b class="badge">a</b><i>verified</i></p>`, `<p id="bob"><b class="badge"></b></p>`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the page has no %s:\n%s", want, body)
		}
	}
}
