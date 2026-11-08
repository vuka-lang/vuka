package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestHiddenModelWarning: model on an <input type="hidden"> compiles, with
// the warning W0115 once.
func TestHiddenModelWarning(t *testing.T) {
	src := `package main

import ui "vukatest.local/stateui"

type Signup struct {
	ui.FormState
	Email string
	Staged string
}

type Join struct {
	Form Signup
}

func (j *Join) Render() ui.Node {
	return <form for={j.Form}><input model={j.Form.Email}/><input type="hidden" model={j.Form.Staged}/></form>
}

func main() {}
`
	res, err := transpile.Package([]transpile.File{{Name: "join.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range res.Files {
		for _, w := range o.Warnings {
			got = append(got, string(w.Code)+" "+w.Error())
		}
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "W0115 join.vuka:16:") || !strings.Contains(got[0], "hidden") {
		t.Fatalf("warnings = %q, want one W0115 on the hidden input", got)
	}
}
