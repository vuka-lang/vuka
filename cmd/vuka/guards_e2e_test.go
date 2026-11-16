package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// gTable is a title over a table: its loop reads only tracked state, so the
// compiler guards it; the summary's branch reads it through a method, the
// note a field that isn't tracked.
const gTable = `<script lang="go">
package main

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

type Table struct {
	ui.Live
	N     int
	Sink  *Sink
	title int
	cells []Cell
	open  bool
}

func (t *Table) Title()      { t.title++ }
func (t *Table) Bump(i int)  { t.cells[i]++ }
func (t *Table) Toggle()     { t.open = !t.open }
func (t *Table) total() int {
	n := 0
	for _, c := range t.cells {
		n += int(c)
	}
	return n
}
func (t *Table) Notes() string { return t.Sink.Note }
</script>
<main>
  <h1 onClick={Title}>Table {title}</h1>
  <button onClick={Toggle}>toggle</button>
  <table>
    for i, v := range cells {
      <tr key={i}><td>row {strconv.Itoa(i)}</td><td>{v}</td><td><button onClick={func() { t.Bump(i) }}>+</button></td></tr>
    }
  </table>
  if open {
    <p>total {total()} of {N}</p>
  }
  if Sink != nil {
    <p>{Notes()}</p>
  }
</main>
`

const gTypes = `package main

import "strconv"

// Cell counts the times a render formats it.
type Cell int

var formatted int

func (c Cell) String() string { formatted++; return strconv.Itoa(int(c)) }

type Sink struct{ Note string }
`

const gMain = `package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
)

func event(s *live.Session, target string) {
	if _, err := s.Event(ui.Event{Target: target, Type: "click"}); err != nil {
		panic(err)
	}
}

func main() {
	for _, verify := range []bool{false, true} {
		live.Verify = verify
		fmt.Println("verify", verify)
		t := &Table{N: 3, Sink: &Sink{Note: "a"}}
		t.cells.Set(make([]Cell, 3))
		s := live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("t", nil, t) })
		if _, err := s.RenderTrees(); err != nil {
			panic(err)
		}
		step := func(what, target string) {
			formatted = 0
			event(s, target)
			h := s.HTML()
			fmt.Println(what, "formatted", formatted, strings.Contains(h, "Table "), strings.Count(h, "<tr"), strings.Contains(h, "total 1 of 3"))
		}
		step("title", "c1:0")
		step("title", "c1:0")
		step("cell", "c1:1:0")
		step("toggle", "c1:1")
		step("title", "c1:0")
		step("kept row", "c1:1:0")
	}
}
`

const gWant = `verify false
title formatted 0 true 3 false
title formatted 0 true 3 false
cell formatted 3 true 3 false
toggle formatted 0 true 3 true
title formatted 0 true 3 true
kept row formatted 3 true 3 false
verify true
title formatted 3 true 3 false
title formatted 3 true 3 false
cell formatted 3 true 3 false
toggle formatted 3 true 3 true
title formatted 3 true 3 true
kept row formatted 3 true 3 false
`

// TestGuardsE2E compiles a table whose loop reads only tracked state and runs
// it under a session: an event that changes the title leaves the loop's rows
// unformatted, one that changes a cell formats them again, the kept rows'
// handlers still work, and verification renders them every time without a
// difference.
func TestGuardsE2E(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if b, err := os.ReadFile(filepath.Join(ui, "guard.go")); err != nil || !strings.Contains(string(b), "func Guard(") {
		needs(t, "the ui checkout (%s) has no Guard", ui)
	}
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n\tgithub.com/vuka-lang/vuka/v2 " + transpile.RuntimeVersion +
			"\n)\n\nreplace github.com/vuka-lang/ui/v2 => " + ui + "\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"go.sum":          string(uiSum) + string(ownSum),
		"main.go":         gMain,
		"types.go":        gTypes,
		"table.vuka.html": gTable,
	})
	cmd := exec.Command(buildVuka(t), "run", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "VUKA_TAILWIND=off", "VUKA_FRONTEND=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out = withoutStaleToolchain(out)
	if string(out) != gWant {
		t.Errorf("got:\n%s\nwant:\n%s", out, gWant)
	}
}
