package transpile_test

import (
	"strings"
	"testing"
)

// letSheet is a generic component over a state, its content a function
// of the state's value: ui.ModalOf's shape.
const letSheet = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Sheet[T any] struct {
	For      ui.State[*T]
	Children func(*T) ui.Node
}
</script>

<div>{Children(For.Get())}</div>
`

// TestLetSlotFormRefs: a let (or a loop's variable) named as the
// receiver its struct's methods use — func (t *Transfer), let={t} — names
// the value where it is declared, and the bare names inside it are still
// the receiver's: a form reference (for={Form}, model={Form.Note}), a
// handler, a field. The markup's method takes a receiver of its own, the
// markup's t a local of it.
func TestLetSlotFormRefs(t *testing.T) {
	res, err := compileShapes(map[string]string{"sheet.vuka.html": letSheet, "transfer.vuka.html": `<script lang="go">
package main

import ui "vukatest.local/stateui"

type TransferReq struct {
	ID   int
	Name string
}

type Detail struct {
	ui.FormState
	Note string
}

type Transfer struct {
	ui.Live
	Form Detail
	For  ui.State[*TransferReq]
	Rows []TransferReq
}

func (t *Transfer) Save(id int) {}
</script>

<Sheet for={For} let={t}>
  <p>{t.Name}</p>
  <form for={Form} onSubmit={Save(t.ID)}>
    <input model={Form.Note}/>
  </form>
</Sheet>
<ul>
  for _, t := range Rows {
    <li>{Form.Note} {t.Name}</li>
  }
</ul>
`})
	if err != nil {
		t.Fatal(codes(err))
	}
	var src strings.Builder
	for _, o := range res.Files {
		src.Write(o.Src)
	}
	for _, want := range []string{
		"func (tr *Transfer) Render() ui.Node {",
		"t := tr",
		"Children: func(t *TransferReq) ui.Node {",
		"ui.For(&tr.Form, tr), OnSubmit: tr.Save(t.ID)",
		"&tr.Form.Note, &tr.Form, tr)",
		"<li>{tr.Form.Note} {t.Name}</li>",
	} {
		if !strings.Contains(src.String(), want) {
			t.Errorf("no %s in\n%s", want, src.String())
		}
	}
}
