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

const genInferMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes()).Run("") }
`

const genInferOrders = `package entities

type Order struct {
	Number string
	Total  int
}
`

const genInferTable = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type TableData[T any] struct {
	Rows  []T
	Count int
}

// DataTable renders each of its data's rows through the caller's Row.
type DataTable[T any] struct {
	Data TableData[T]
	Row  func(T) ui.Node
}
</script>

<table data-count={Data.Count}>
  for _, r := range Data.Rows {
    <tr>{Row(r)}</tr>
  }
</table>
`

const genInferIndex = `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/entities"
)

type Index struct{}

func orders() components.TableData[entities.Order] {
	rows := []entities.Order{{Number: "A-1", Total: 30}, {Number: "A-2", Total: 12}}
	return components.TableData[entities.Order]{Rows: rows, Count: len(rows)}
}
</script>

<main>
  <components.DataTable data={orders()}>
    <slot:row let={o}><td class="n">{o.Number}</td><td>{o.Total}</td></slot:row>
  </components.DataTable>
</main>
`

// TestGenericInferenceE2E: a page uses a generic component of another
// package without type arguments; T comes from its data, and its scoped
// slot's value is that T.
func TestGenericInferenceE2E(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	root := localModule(t, web, map[string]string{
		"main.go":                        genInferMain,
		"entities/order.go":              genInferOrders,
		"components/datatable.vuka.html": genInferTable,
		"pages/index.vuka.html":          genInferIndex,
	})
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	base, _ := serve(t, root, app)
	res, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{`<table data-count="2">`, `<td class="n">A-1</td><td>30</td>`, `<td class="n">A-2</td><td>12</td>`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the page has no %s:\n%s", want, body)
		}
	}
}
