package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// Formsets: a form type's ui.Rows, its rows laid out by the markup with
// model={r.X} and <widget.Error for={r.X}/> inside Form.Lines.Each(),
// and the Add and Remove events.
var formsetApp = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	ctx := context.Background()
	p := &Checkout{}
	ui.Attach(&p.Form).Load(ctx, Order{Customer: "Ada"})
	p.Form.Lines.Load(ctx, Line{Product: "Lamp", Quantity: 2}, Line{Product: "Mug", Quantity: 1})
	s, err := ui.String(ctx, p.Render())
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}
`,
	"checkout.vuka.html": `<script lang="go">
package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/widget"
)

type Order struct {
	Customer string
}

type Line struct {
	Product  string
	Quantity int
}

type OrderForm struct {
	ui.FormFor[Order]
	Lines ui.Rows[Line] @ui.Spec{Min: 1, Max: 20}
}

type Checkout struct {
	ui.Live
	Form OrderForm
}

func (c *Checkout) Save(ctx context.Context) error { return nil }
</script>

<main>
  <form for={Form} onSubmit={Save}>
    <widget.Field for={Form.Customer}/>
    for _, r := range Form.Lines.Each() {
      <div class="line">
        <input model={r.Product}/>
        <widget.Number model={r.Quantity}/>
        <widget.Error for={r.Quantity}/>
        <button type="button" onClick={Form.Lines.Remove(r)}>Remove</button>
      </div>
    }
    <button type="button" onClick={Form.Lines.Add()}>Add line</button>
  </form>
</main>
`,
}

func TestFormsets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range formsetApp {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	got := out.String()
	for _, want := range []string{
		`name="customer" id="order-customer" type="text" value="Ada"`,
		`name="lines.r1.product" id="line-lines-r1-product" type="text" value="Lamp"`,
		`name="lines.r2.quantity" id="line-lines-r2-quantity" type="number" value="1"`,
		`<input class="vk-input" name="lines.r1.quantity" id="line-lines-r1-quantity" type="number" value="2"`,
		`>Add line</button>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}
