package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// Widgets: an app's widget written as a .vuka.html component, filled by
// the form as its markup places it; built-in widgets in the markup and by
// @ui.Spec on the model's field; the form's components from package widget.
var widgetApp = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	p := &Edit{}
	p.Form.Load(context.Background(), Product{Name: "Lamp", Size: "l"})
	s, err := ui.String(context.Background(), p.Render())
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}
`,
	"sizes.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2/widget"

// SizePicker shows each size as a button-like radio.
type SizePicker struct {
	widget.Props
	Choices []widget.Choice[any]
}
</script>

<div class="sizes">
  <span class="sizes-label">{Label}</span>
  for _, c := range Choices {
    <label class:on={c.Selected}><input type="radio" model={Model} value={c.Value}/>{c.Label}</label>
  }
</div>
`,
	"edit.vuka.html": `<script lang="go">
package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/widget"
)

type Product struct {
	Name  string
	Notes string @ui.Spec{Widget: widget.Textarea{Rows: 5}}
	Size  string
}

func (Product) SizeChoices() []ui.Option {
	return []ui.Option{{Value: "s", Label: "Small"}, {Value: "l", Label: "Large"}}
}

type Edit struct {
	ui.Live
	Form ui.FormFor[Product]
}

func (e *Edit) Save(ctx context.Context) error { return nil }
</script>

<main>
<form for={Form} onSubmit={Save}>
  <widget.Summary/>
  <widget.Text model={Form.Name} class="wide"/>
  <widget.Field for={Form.Notes}/>
  <SizePicker model={Form.Size}/>
</form>
</main>
`,
}

func TestWidgets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range widgetApp {
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
		`<input class="vk-input wide" name="name" id="product-name" type="text" value="Lamp"`,
		`<textarea class="vk-textarea" rows="5" name="notes" id="product-notes"`,
		`<span class="sizes-label">Size</span>`,
		`<div id="product-summary-slot" hidden></div>`,
		`<label class="on"><input type="radio" name="size" id="product-size-2" checked value="l">Large</label>`,
		`value="name,notes,size.`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}
