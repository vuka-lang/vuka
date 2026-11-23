package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

var attrSugarModule = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"
	"strings"

	"example.com/app/views"
	"github.com/vuka-lang/ui/v2"
)

func main() {
	for range 2 {
		var b strings.Builder
		if _, err := ui.RenderPage(context.Background(), views.Index{Rule: "r1", Hint: "Search"}, &b); err != nil {
			panic(err)
		}
		fmt.Println(b.String())
	}
}
`,
	"design/button.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneDanger  Tone = "danger"
)

type Button struct {
	Type     string ` + "`vuka:\"default=button\"`" + `
	Tone     Tone   ` + "`vuka:\"default=ToneNeutral\"`" + `
	Label    string
	Attrs    []ui.Attr
	Children ui.Node
}
</script>

<button type={Type} data-tone={string(Tone)} {Attrs...}>{Children}</button>
`,
	"design/dialog.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Dialog struct {
	ID    ui.AutoID
	Title string
}
</script>

<dialog id={ID} aria-labelledby={ID.Suffix("title")}><h2 id={ID.Suffix("title")}>{Title}</h2></dialog>
`,
	"views/index.vuka.html": `<script lang="go">
package views

import "example.com/app/design"

type Index struct {
	Rule string
	Hint string
	Kind string
	ID   string
}
</script>

<main>
  <design.Button attr:form={"rule-" + Rule} attr:label="html" title?={Kind}>Save</design.Button>
  <design.Button tone="danger" type?={Kind}>Drop</design.Button>
  <design.Button type="">Plain</design.Button>
  <input id?={ID} placeholder?={Hint} />
  <design.Dialog title="one" />
  <design.Dialog title="two" />
  <design.Dialog id="confirm" title="three" />
</main>
`,
}

// TestAttrSugarBuild: attr:name, name?={v}, prop defaults and ui.AutoID
// build against the ui checkout and render: a passed-on attribute, a zero
// one left out, defaults where a tag leaves the prop out, and ids unique on
// the page and the same in each of its renders.
func TestAttrSugarBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range attrSugarModule {
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
	renders := strings.Split(strings.TrimSpace(stdout.String()), "\n<main>")
	out := renders[0]
	for _, want := range []string{
		`<button type="button" data-tone="neutral" form="rule-r1" label="html">Save</button>`,
		`<button type="button" data-tone="danger">Drop</button>`,
		`<button type="" data-tone="neutral">Plain</button>`,
		`<input placeholder="Search">`,
		`<dialog id="confirm" aria-labelledby="confirm-title"><h2 id="confirm-title">three</h2></dialog>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in\n%s", want, out)
		}
	}
	ids := regexp.MustCompile(`<dialog id="([^"]+)" aria-labelledby="([^"]+)"><h2 id="([^"]+)">(one|two)<`).FindAllStringSubmatch(out, -1)
	if len(ids) != 2 || ids[0][1] == ids[1][1] {
		t.Fatalf("ids: %q", ids)
	}
	for _, m := range ids {
		if m[2] != m[1]+"-title" || m[3] != m[1]+"-title" {
			t.Errorf("parts not named by the id: %s", m[0])
		}
	}
	if len(renders) != 2 || "<main>"+renders[1] != out[strings.Index(out, "<main>"):] {
		t.Errorf("the two renders differ:\n%s", stdout.String())
	}
}
