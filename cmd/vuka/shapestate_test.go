package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

const shapeStateMain = `package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vuka-lang/ui"
)

func main() {
	ctx := context.Background()
	b := &Board{}
	if err := ui.MountLive(ctx, b); err != nil {
		panic(err)
	}
	tracked, changed := ui.TrackedState(b)
	fmt.Println("tracked", tracked, "changed", changed)
	b.Add("a")
	b.Add("b")
	b.Bump()
	_, changed = ui.TrackedState(b)
	fmt.Println("after add, changed", changed)
	html, err := ui.String(ctx, b.Render())
	if err != nil {
		panic(err)
	}
	fmt.Println("rows", strings.Contains(html, "2 rows"))
	fmt.Println("toggle", strings.Contains(html, "state_toggle"), "set then push", strings.Contains(html, "state_set") && strings.Contains(html, "push"))
	m, err := ui.String(ctx, &Menu{Label: "M"})
	if err != nil {
		panic(err)
	}
	fmt.Println("inc", strings.Contains(m, "state_inc"), "read", strings.Contains(m, "data-vk-b"))
}
`

const shapeStateBoard = `package main

// Board is live: rows is tracked server state, open and tab the browser's.
type Board struct {
	ui.Live
	rows []string
	open ui.State[bool]
	tab  ui.State[string]
}

func (b *Board) Add(name string) { b.rows = append(b.rows, name) }

func (b *Board) Bump() { b.open = !b.open }
<section>
  <p>{len(rows)} rows</p>
  <button onClick={open = !open} aria-pressed={open}>open</button>
  <button onClick={tab = "x"; Bump()}>{tab}</button>
</section>
`

const shapeStateMenu = `package main

// Menu is static: its state is the browser's alone.
type Menu struct {
	Label string
	n     ui.State[int]
}
<div>
  <button onClick={n++}>{Label}</button>
  <span>{n + 1}</span>
</div>
`

// TestShapeStateRuntime builds a .vuka.html struct with client state and
// tracked server state against ui and runs it: a live one's states exist
// before it mounts, its server state is tracked (a ui.State field doesn't
// stop that) and marked when a method writes it, a method's state write
// pushes rather than panics, and on… attributes' writes are chains of
// state commands — a static struct's too.
func TestShapeStateRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shapes")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for name, src := range map[string]string{
		"go.mod":          gomod,
		"go.sum":          gosum,
		"main.go":         shapeStateMain,
		"board.vuka.html": shapeStateBoard,
		"menu.vuka.html":  shapeStateMenu,
	} {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	want := "tracked true changed false\nafter add, changed true\nrows true\ntoggle true set then push true\ninc true read true\n"
	if got := out.String(); !strings.HasSuffix(got, want) {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}
