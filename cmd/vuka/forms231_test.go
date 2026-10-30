package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The forms gaps of vuka#231, each a shape of a small program built and
// run: attributes on <ui.Form> (and any component with an Attrs []ui.Attr),
// a form declared beside its page's struct, model={…} given to an app's
// own control component.
var forms231 = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	s, err := ui.String(context.Background(), &Join{})
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}
`,
	"join.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Join struct {
	Form Signup
}

type Signup struct {
	ui.FormState
	Email string ` + "`validate:\"required,email\"`" + `
	Name  string
}
</script>

<ui.Form for={Form} autocomplete="off" data-shop="join" aria-label="Join">
  <Input model={Form.Email} label="Email"/>
  <Input model={Form.Name} label="Name" data-x="1"/>
  <ui.Button type="submit" data-kind="primary">Join</ui.Button>
</ui.Form>
`,
	"input.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

// Input is the app's own text field: a label and a styled control.
type Input struct {
	Model ui.FieldRef
	Label string
	Attrs []ui.Attr
}
</script>

<div class="app-field" {Attrs...}>
  <label class="app-label" for={ui.ModelID(Model)}>{Label}</label>
  <ui.Error for={Model}/>
  <input model={Model} class="app-input"/>
</div>
`,
}

func TestForms231(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/forms")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range forms231 {
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
		`<form class="vk-form" method="post" id="signup" novalidate data-vk-form="signup"`, `autocomplete="off"`, `data-shop="join"`, `aria-label="Join"`,
		`<div class="app-field"><label class="app-label" for="signup-email">Email</label>`,
		`<input name="email" id="signup-email" type="email" value="" autocomplete="email" class="app-input">`,
		`<div class="app-field" data-x="1"><label class="app-label" for="signup-name">Name</label>`,
		`name="name" id="signup-name"`, `data-kind="primary"`, `value="email,name.`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}

// TestForms231Errors: what the compiler says of model on a component that
// takes none, of a modifier given to one, and of a second struct that
// isn't a form.
func TestForms231Errors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	for _, c := range []struct{ name, file, src, want string }{
		{"no Model field", "join.vuka.html", strings.Replace(forms231["join.vuka.html"], `<Input model={Form.Name} label="Name" data-x="1"/>`, `<ui.Badge model={Form.Name}/>`, 1),
			"<ui.Badge> takes no model: a component takes model={Form.Email} when its props have a field Model ui.FieldRef"},
		{"a modifier", "join.vuka.html", strings.Replace(forms231["join.vuka.html"], `<Input model={Form.Email}`, `<Input model.lazy={Form.Email}`, 1),
			"model.lazy on a component"},
		{"a second struct", "join.vuka.html", strings.Replace(forms231["join.vuka.html"], "type Signup struct {", "type Row struct{ ID int }\n\ntype Signup struct {", 1),
			"the template file declares Join and Row: a .vuka.html file renders one struct"},
	} {
		t.Run(c.name, func(t *testing.T) {
			gomod, gosum := testModule(t, "example.com/forms")
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			writeFile(t, filepath.Join(dir, "go.mod"), gomod)
			writeFile(t, filepath.Join(dir, "go.sum"), gosum)
			for name, src := range forms231 {
				if name == c.file {
					src = c.src
				}
				writeFile(t, filepath.Join(dir, name), src)
			}
			t.Setenv("GOWORK", "off")
			t.Chdir(dir)
			var out, errOut bytes.Buffer
			err := runGo("build", []string{"-o", filepath.Join(dir, "app"), "."}, &out, &errOut)
			if got := out.String() + errOut.String(); err == nil || !strings.Contains(got, c.want) {
				t.Errorf("%v: no %q in\n%s", err, c.want, got)
			}
		})
	}
}
