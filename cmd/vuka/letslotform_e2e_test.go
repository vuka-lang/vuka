package main

import (
	"bytes"
	"html"
	"strings"
	"testing"
)

var letSlotFormModule = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"example.com/app/views"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
	"github.com/vuka-lang/ui/v2/live/livetest"
)

func main() {
	page := &views.Page{}
	s := live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("t", nil, page) })
	c, err := livetest.Join(s, 2)
	if err != nil {
		panic(err)
	}
	if err := c.Click("button#pick"); err != nil {
		panic(err)
	}
	fmt.Println("opened:", c.HTML())
}
`,
	"views/transfer.vuka.html": `<script lang="go">
package views

import "github.com/vuka-lang/ui/v2"

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
	Form  Detail
	For   ui.State[*TransferReq]
	saved string
}

@ui.Mount
func (t *Transfer) Mount() { t.Form.Note = "note for orders" }

func (t *Transfer) Save(id int) { t.saved = t.Form.Note }
</script>

<div>
  <ui.ModalOf for={For} let={t} title={ui.Text("Transfer")}>
    <p>Transfer {t.Name}</p>
    <form for={Form} onSubmit={Save(t.ID)}>
      <input model={Form.Note}/>
    </form>
  </ui.ModalOf>
</div>
`,
	"views/page.vuka.html": `<script lang="go">
package views

import "github.com/vuka-lang/ui/v2"

type Page struct {
	ui.Live
	pick ui.State[*TransferReq]
}

func (p *Page) Pick(id int) { p.pick = &TransferReq{ID: id, Name: "orders"} }
</script>

<main>
  <button id="pick" onClick={Pick(7)}>Pick</button>
  <Transfer for={pick}/>
</main>
`,
}

// TestLetSlotFormBuild: a let named as the struct's receiver (func (t
// *Transfer), let={t}) inside <ui.ModalOf>: the form inside binds to the
// receiver's FormState, its model field to the receiver's field, while t
// is the modal's value — the page builds, and opening it renders both.
func TestLetSlotFormBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range letSlotFormModule {
		files[k] = v
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	root := tempModule(t, files)
	t.Setenv("GOFLAGS", "")
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if err := runGo("run", []string{"."}, &stdout, &stderr); err != nil {
		t.Fatalf("vuka run: %v\n%s", err, stderr.String())
	}
	out := html.UnescapeString(stdout.String())
	for _, want := range []string{`<p>Transfer orders</p>`, `data-vk-form="detail"`, `<input name="note" id="detail-note" type="text" value="note for orders">`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in\n%s", want, out)
		}
	}
}
