package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A form on plain tags: <form for> is ui's form on the page's own element,
// labels and legends fill themselves, a fieldset scopes its fields, and a
// button outside the form names it.
var formTags = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	p := &Checkout{}
	p.Form.Load(context.Background(), Order{Name: "Ada", Plan: "pro"})
	s, err := ui.String(context.Background(), p.Render())
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
)

type Address struct {
	Street string
	City   string
}

type Order struct {
	Name    string
	Plan    string
	Address Address
}

func (Order) PlanChoices() []ui.Option {
	return []ui.Option{{Value: "free", Label: "Free"}, {Value: "pro", Label: "Pro"}}
}

type Checkout struct {
	ui.Live
	Form ui.FormFor[Order]
}

func (c *Checkout) Save(ctx context.Context, o Order) error { return nil }
</script>

<main>
  <form for={Form} onSubmit={Save} onSuccess={ui.JS.Hide(ui.Sel("#dialog"))} class="stack" autocomplete="off">
    <label for={Form.Name}/>
    <input model={Form.Name} class="input"/>
    <fieldset for={Form.Plan}>
      <legend/>
      <ui.Choices for={Form.Plan}/>
    </fieldset>
    <fieldset for={Form.Address}>
      <legend/>
      <label for={Street}/>
      <input model={Street}/>
      <ui.Field for={City}/>
    </fieldset>
  </form>
  <button form={Form}>Save</button>
</main>
`,
}

func TestFormTags(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range formTags {
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
		`<form class="stack" method="post" id="order" novalidate data-vk-form="order"`,
		`autocomplete="off"`,
		`<label for="order-name">Name</label>`,
		`name="name" id="order-name" type="text" value="Ada"`,
		`data-vk-name="plan"`,
		`<legend>Plan</legend>`,
		`<legend>Address</legend>`,
		`<label for="order-address-street">Street</label>`,
		`name="address.street" id="order-address-street"`,
		`for="order-address-city"`,
		`<button form="order">Save</button>`,
		`data-vk-js-vksuccess=`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}
