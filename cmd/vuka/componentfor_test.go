package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

var componentFor = map[string]string{
	"main.go": forms231["main.go"],
	"join.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Join struct {
	Form Signup
	Slug string
}

type Signup struct {
	ui.FormState
	Email string
}
</script>

<ui.Form for={Form}>
  <Label for={Form.Email}>Email</Label>
  <Label for={Slug}>Slug</Label>
  <input model={Form.Email}/>
</ui.Form>
`,
	"label.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Label struct {
	For      string
	Children ui.Node
}
</script>

<label for={For}>{Children}</label>
`,
}

func TestComponentStringFor(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/compfor")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range componentFor {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOFLAGS", "")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	got := out.String()
	for _, want := range []string{`<label for="signup-email">Email</label>`, `<label for="">Slug</label>`, `<input name="email" id="signup-email"`} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}
