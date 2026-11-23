package main

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"testing"
)

var modalForModule = map[string]string{
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
	page := &views.Rows{}
	s := live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("r", nil, page) })
	c, err := livetest.Join(s, 2)
	if err != nil {
		panic(err)
	}
	fmt.Println("joined:", c.HTML())
	if err := c.Click("button#pick"); err != nil {
		panic(err)
	}
	fmt.Println("opened:", c.HTML())
}
`,
	"views/rows.vuka.html": `<script lang="go">
package views

import "github.com/vuka-lang/ui/v2"

type SyncTarget struct {
	ID   int
	Name string
}

type Rows struct {
	ui.Live
	sync ui.State[*SyncTarget]
	ran  int
}

func (r *Rows) Pick(id int) { r.sync = &SyncTarget{ID: id, Name: "orders"} }

func (r *Rows) RunSync(id int) { r.ran = id }
</script>

<main>
  <button id="pick" onClick={Pick(7)}>Pick</button>
  <button id="mark" onClick={sync = &SyncTarget{ID: 8, Name: "users"}}>Mark</button>
  <ui.ModalOf for={sync} let={t} title={ui.Text("Sync")}>
    <p>Sync {t.Name}</p>
    <input name="name" value={t.Name}/>
    <slot:footer let={t}>
      <button id="run" onClick={RunSync(t.ID)}>Sync {t.ID}</button>
    </slot:footer>
  </ui.ModalOf>
</main>
`,
}

// TestModalForBuild: <ui.ModalOf for={state} let={t}> against the ui
// checkout — the type argument inferred from the state, the tag's content
// and its footer slot functions of the value, the state handed over as
// itself (synced: the modal's Go reads it) so a browser write (Mark) and a
// handler's (Pick) both open it, and its ids the tag's.
func TestModalForBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range modalForModule {
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
	section := func(name string) string {
		i := strings.Index(out, name+":")
		if i < 0 {
			t.Fatalf("no %s in\n%s", name, out)
		}
		s := out[i:]
		if j := strings.Index(s, "\n"); j >= 0 {
			s = s[:j]
		}
		return s
	}
	joined, opened := section("joined"), section("opened")
	for _, c := range []struct{ section, want string }{
		{joined, `<button id="mark" data-vk-js-click="[["state_set",{"s":"c1:`},
		{joined, `"v":{"ID":8,"Name":"users"}}]]">Mark</button>`},
		{joined, `<div class="vk-modal-root" hidden data-vk-v="["!=",{"i":null,`},
		{joined, `"sy":1},null]"`},
		{joined, `aria-labelledby="vk-c1-`},
		{opened, `<div class="vk-modal__body"><p>Sync orders</p><input name="name" value="orders"></div><div class="vk-modal__footer"><button id="run"`},
		{opened, `>Sync 7</button></div>`},
	} {
		if !strings.Contains(c.section, c.want) {
			t.Errorf("no %s in\n%s", c.want, c.section)
		}
	}
	if strings.Contains(joined, "vk-modal__body\"><p>") || strings.Contains(joined, "data-vk-p") {
		t.Errorf("a closed modal carries its content or props:\n%s", joined)
	}
	if regexp.MustCompile(`<div class="vk-modal-root"[^>]* hidden`).MatchString(opened) {
		t.Errorf("Pick didn't open it:\n%s", opened)
	}
}
