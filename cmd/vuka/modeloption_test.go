package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// model={…} and for={…} on an Option field, in each kind of form: the
// control, its label and an app's control component take the field as
// declared, and show the value Load put in it.
var modelOption = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
)

func main() {
	p := &Edit{Model: ui.FormOf[Product]("Name", "Notes")}
	p.Form.Load(context.Background(), Product{Name: "Kettle", Notes: vuka.Some("fragile")})
	p.Order.Note = vuka.Some("gift wrap")
	p.Model.Load(Product{Name: "Lamp", Notes: vuka.Some("brass")})
	s, err := ui.String(context.Background(), p)
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}
`,
	"edit.vuka.html": `<script lang="go">
package main

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
)

type Product struct {
	Name  string
	Notes vuka.Option[string]
}

type Order struct {
	ui.FormState
	Note vuka.Option[string]
}

type Edit struct {
	Form  ui.FormFor[Product]
	Order Order
	Model *ui.ModelForm[Product]
}
</script>

<main>
  <ui.Form for={Form}>
    <label for={Form.Notes}>Notes</label>
    <input model={Form.Notes} class="input"/>
    <ui.Error for={Form.Notes}/>
  </ui.Form>
  <ui.Form for={Order}>
    <textarea model.lazy={Order.Note}></textarea>
    <Input model={Order.Note}/>
  </ui.Form>
  <ui.Form for={Model}>
    <input model={Model.Value.Notes}/>
  </ui.Form>
</main>
`,
	"input.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Input struct {
	Model ui.FieldRef
}
</script>

<input model={Model} class="app-input"/>
`,
}

func TestModelOption(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range modelOption {
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
		`<label for="product-notes">Notes</label>`,
		`name="notes" id="product-notes" type="text" value="fragile" class="input"`,
		`<textarea name="note" id="order-note" data-vk-lazy>gift wrap</textarea>`,
		`name="note" id="order-note-2" type="text" value="gift wrap" class="app-input"`,
		`name="notes" id="product-notes" type="text" value="brass">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}
