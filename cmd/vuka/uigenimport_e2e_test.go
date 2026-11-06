package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uigenMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { web.New(pages.Routes()).Run("") }
`

const uigenOrders = `package orders

type Order struct {
	ID     int64
	Number string
}
`

// uigenShapes are pages and components reading browser state, a stream or
// both, each with plain states (no options) and with @ui.Keep.
var uigenShapes = map[string]string{
	"pages/index.vuka.html": `<script lang="go">
package pages

import (
	"context"

	"example.com/app/orders"
	"github.com/vuka-lang/ui/v2"
)

// Index is live: a plain state and a stream.
type Index struct {
	ui.Live
	open ui.State[bool]
	rows ui.Stream[orders.Order]
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows.Insert(orders.Order{ID: 1, Number: "A-1"})
	return nil
}
</script>

<main>
  <button onClick={open = !open}>toggle</button>
  <ul hidden={!open}>
    for _, o := range rows.Rows() {
      <li key={o.ID}>{o.Number}</li>
    }
  </ul>
</main>
`,
	"pages/kept/index.vuka.html": `<script lang="go">
package kept

import (
	"context"

	"example.com/app/orders"
	"github.com/vuka-lang/ui/v2"
)

// Index is live: a kept state and a stream.
type Index struct {
	ui.Live
	open ui.State[bool] @ui.Keep("open")
	rows ui.Stream[orders.Order]
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows.Insert(orders.Order{ID: 1, Number: "A-1"})
	return nil
}
</script>

<main>
  <button onClick={open = !open}>toggle</button>
  <ul hidden={!open}>
    for _, o := range rows.Rows() {
      <li key={o.ID}>{o.Number}</li>
    }
  </ul>
</main>
`,
	"pages/plain/index.vuka.html": `<script lang="go">
package plain

import "github.com/vuka-lang/ui/v2"

// Index isn't live: a plain state.
type Index struct {
	open ui.State[bool]
}
</script>

<main><button onClick={open = !open}>toggle</button><p hidden={!open}>shown</p></main>
`,
	"pages/feed/index.vuka.html": `<script lang="go">
package feed

import (
	"context"

	"example.com/app/orders"
	"github.com/vuka-lang/ui/v2"
)

// Index is live with only a stream.
type Index struct {
	ui.Live
	rows ui.Stream[orders.Order]
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows.Insert(orders.Order{ID: 1, Number: "A-1"})
	return nil
}
</script>

<ul>
  for _, o := range rows.Rows() {
    <li key={o.ID}>{o.Number}</li>
  }
</ul>
`,
	"pages/reads/index.vuka.html": `<script lang="go">
package reads

import (
	"context"

	"example.com/app/orders"
	"github.com/vuka-lang/ui/v2"
)

// Index is live: a plain state it only reads, and a stream.
type Index struct {
	ui.Live
	open ui.State[bool]
	name ui.State[string]
	rows ui.Stream[orders.Order]
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.rows.Insert(orders.Order{ID: 1, Number: "A-1"})
	return nil
}
</script>

<main>
  <p class:on={open}>{name}</p>
  <ul>
    for _, o := range rows.Rows() {
      <li key={o.ID} class:on={open}>{o.Number} {name}</li>
    }
  </ul>
</main>
`,
	"components/menu.vuka.html": `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Menu is a component with a plain state.
type Menu struct {
	Label string
	open  ui.State[bool]
}
</script>

<nav><button onClick={open = !open}>{Label}</button><ul hidden={!open}><li>a</li></ul></nav>
`,
	"pages/menu/index.vuka.html": `<script lang="go">
package menu

import "example.com/app/components"

type Index struct{}
</script>

<components.Menu label="Menu" />
`,
}

// TestUigenImportE2E: an app whose pages and components read browser state
// and streams in every combination builds: the generated Go imports ui's
// state package only where it calls it.
func TestUigenImportE2E(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	files := map[string]string{"main.go": uigenMain, "orders/orders.go": uigenOrders}
	for k, v := range uigenShapes {
		files[k] = v
	}
	root := localModule(t, web, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	if strings.Contains(stderr.String(), "uigen") {
		t.Errorf("build output mentions uigen:\n%s", stderr.String())
	}
}
