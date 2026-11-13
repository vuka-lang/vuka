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

const resultStore = `package orders

import (
	"context"
	"errors"

	"github.com/vuka-lang/vuka/v2"
)

type Order struct{ Number string }

func List(ctx context.Context, down bool) vuka.Result[[]Order] {
	if down {
		return vuka.Err[[]Order](errors.New("orders: store down"))
	}
	return vuka.Ok([]Order{{"A-1"}, {"A-2"}})
}
`

const resultIndex = `<script lang="go">
package pages

import "example.com/app/orders"

type Index struct{}

func numbers(os []orders.Order) []string {
	var out []string
	for _, o := range os {
		out = append(out, o.Number)
	}
	return out
}
</script>

<main>
  <p id="count">{len(orders.List(ctx, false))}</p>
  for _, o := range orders.List(ctx, false) {
    <b>{o.Number}</b>
  }
</main>
`

const resultBroken = `<script lang="go">
package broken

import "example.com/app/orders"

type Index struct{}
</script>

<main>
  for _, o := range orders.List(ctx, true) {
    <b>{o.Number}</b>
  }
</main>
`

// TestMarkupResultE2E: a page ranges over a vuka.Result as its value, and
// an Err takes the page's error path: a 500, the error in the log.
func TestMarkupResultE2E(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	root := localModule(t, web, map[string]string{
		"main.go":                      genInferMain,
		"orders/orders.go":             resultStore,
		"pages/index.vuka.html":        resultIndex,
		"pages/broken/index.vuka.html": resultBroken,
	})
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	base, _ := serve(t, root, app)
	get := func(path string) (int, string) {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res.StatusCode, string(body)
	}
	if code, body := get("/"); code != 200 || !strings.Contains(body, `<p id="count">2</p>`) || !strings.Contains(body, "<b>A-1</b><b>A-2</b>") {
		t.Errorf("GET /: %d\n%s", code, body)
	}
	if code, body := get("/broken"); code != 500 || strings.Contains(body, "<b>") {
		t.Errorf("GET /broken: %d, want 500 and nothing rendered\n%s", code, body)
	}
}
