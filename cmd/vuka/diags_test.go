package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/docs"
	"github.com/vuka-lang/vuka/v2/transpile"
)

func TestExplainCode(t *testing.T) {
	var b bytes.Buffer
	if err := explain([]string{"E0203"}, &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"E0203: An attribute must start its own line", "Wrong", "Right", "    @doc(\"Lists the users\")", "See " + diag.DocsURL + "E0203"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain E0203 lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "```") || strings.Contains(out, "\n# ") {
		t.Errorf("Markdown left in:\n%s", out)
	}
	if err := explain([]string{"e0203"}, &b); err != nil {
		t.Errorf("lower case: %v", err)
	}
	if err := explain([]string{"E9999"}, &b); err == nil || !strings.Contains(err.Error(), "no code E9999") {
		t.Errorf("unknown code: %v", err)
	}
}

// TestCatalogueCovers: every code the CLI's diagnostics use has a page.
func TestCatalogueCovers(t *testing.T) {
	for _, d := range []*diag.Diagnostic{
		(&islandComp{name: "X", label: "a.vuka:1", problem: "p", problemCode: "E0310"}).diagnostic(),
	} {
		if _, ok := docs.Lookup(string(d.Code)); !ok {
			t.Errorf("%s has no page", d.Code)
		}
	}
}

func TestPrintDiagnostics(t *testing.T) {
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	src := "package main\n\nfunc helper() {} @doc(\"x\")\nfunc main() {}\n"
	os.WriteFile(filepath.Join(dir, "main.vuka"), []byte(src), 0o644)
	t.Chdir(dir)
	list := transpile.ErrorList{{Pos: tokenPos(filepath.Join(dir, "main.vuka"), 3, 18), Msg: "an attribute must start its own line", Code: "E0203"}}
	err := errors.Join(list, errors.New("plain trouble"))

	var b bytes.Buffer
	writeDiags(&b, diagsOf(err), false)
	want := `error[E0203]: an attribute must start its own line
 --> main.vuka:3:18
  |
3 | func helper() {} @doc("x")
  |                  ^^^^
  |
  = help: put the attribute on its own line, above the declaration

For more information about this error, try ` + "`vuka explain E0203`" + `.
`
	if b.String() != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", b.String(), want)
	}

	diagJSON = true
	defer func() { diagJSON = false }()
	b.Reset()
	writeDiags(&b, diagsOf(err), false)
	var out []map[string]any
	if err := json.Unmarshal(b.Bytes(), &out); err != nil || len(out) != 1 {
		t.Fatalf("JSON: %v %s", err, b.String())
	}
	if d := out[0]; d["code"] != "E0203" || d["help"] == nil || d["url"] != diag.DocsURL+"E0203" ||
		d["span"].(map[string]any)["file"] != "main.vuka" {
		t.Errorf("JSON: %v", d)
	}
}

func tokenPos(file string, line, col int) token.Position {
	return token.Position{Filename: file, Line: line, Column: col}
}

func diagsOf(err error) []diag.Diagnostic {
	ds, _ := diag.From(err)
	return ds
}

func TestCutDiagFlag(t *testing.T) {
	defer func() { diagJSON = false }()
	got := cutDiagFlag([]string{"-v", "-diag=json", "./..."})
	if strings.Join(got, " ") != "-v ./..." || !diagJSON {
		t.Errorf("cutDiagFlag = %q, %v", got, diagJSON)
	}
	diagJSON = false
	if got := cutDiagFlag([]string{"./...", "-diag=json"}); len(got) != 2 || diagJSON {
		t.Errorf("a package's argument is taken: %q", got)
	}
}

const vetPage = `package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

@web.Live("/applicants/{id}")
@ui.Island("islands/Detail.vue")
type Detail struct {
	ui.Live
	Name   string
	CV     string ` + "`defer:\"profile\"`" + `
	Notes  string ` + "`defer:\"the notes\"`" + `
	Q      string ` + "`json:\"q\" bind:\"query\"`" + `
	Status string ` + "`bind:\"status,wait=1s\"`" + `
	Stats  string ` + "`defer:\"\"`" + `
	Open   bool
	Mode   string ` + "`json:\"open\"`" + `
	Flag   string ` + "`json:\"cancel\"`" + `
	Years  int    ` + "`validate:\"min,max=40\"`" + `
}

func (d *Detail) LoadStats(ctx context.Context, n int) error { return nil }

func (d *Detail) Cancel(ctx context.Context) error { return nil }
`

// TestVetIslands: what ui.CheckIsland refuses at boot that the source shows,
// vuka vet says first, at the field.
func TestVetIslands(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod, "detail.vuka": vetPage, "islands/Detail.vue": "<template><p/></template>"})
	pa := &pageAssets{root: root}
	if errs := pa.readIslands(filepath.Join(root, "detail.vuka"), []byte(vetPage)); len(errs) > 0 {
		t.Fatal(errs)
	}
	var got []string
	for _, d := range vetIslands(root, "example.com/app", pa) {
		got = append(got, string(d.Code)+" "+d.Span.String()+" "+d.Message)
		if _, ok := docs.Lookup(string(d.Code)); !ok {
			t.Errorf("%s has no page", d.Code)
		}
	}
	want := []string{
		`E0323 detail.vuka:16:2 Detail.Notes: defer:"the notes": a group is a name`,
		`E0322 detail.vuka:17:2 Detail.Q: bind:"query" names the field live.q`,
		`E0322 detail.vuka:18:2 Detail.Status: bind:"status,wait=1s": unknown option "wait"`,
		`E0326 detail.vuka:21:2 Detail: fields Open and Mode are both live.open`,
		`E0328 detail.vuka:23:2 Detail.Years: validate:"min,max=40": min takes a number`,
		`E0324 detail.vuka:15:2 Detail.CV is deferred, and the group Profile has no loader`,
		`E0324 detail.vuka:19:2 Detail.Stats is deferred, and the group Stats has no loader`,
		`E0326 detail.vuka:28 Detail: the method Cancel and the field Flag are both live.cancel`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("vet:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
