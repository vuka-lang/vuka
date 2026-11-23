package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

var fallthroughModule = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"
	"strings"

	"example.com/app/views"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
	"github.com/vuka-lang/ui/v2/live/livetest"
)

func main() {
	var b strings.Builder
	if _, err := ui.RenderPage(context.Background(), views.Index{ID: "billing"}, &b); err != nil {
		panic(err)
	}
	fmt.Println("static:", b.String())

	page := &views.Search{Q: "go"}
	s := live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("s", nil, page) })
	c, err := livetest.Join(s, 2)
	if err != nil {
		panic(err)
	}
	fmt.Println("joined:", c.HTML())
	if err := c.Fire("input", ui.Event{Type: "input", Value: "orders"}); err != nil {
		panic(err)
	}
	fmt.Println("typed:", page.Q, c.HTML())
}
`,
	"design/card.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Card struct {
	Title    string
	Children ui.Node
}
</script>

<section class="rounded-xl border bg-card p-4" role="region"><h2>{Title}</h2>{Children}</section>
`,
	"design/input.vuka.html": `<script lang="go">
package design

type Input struct {
	Label string
}
</script>

<label class="field">{Label} <input class="h-10 rounded-md border px-3" forward/></label>
`,
	"design/chip.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Chip struct {
	Children ui.Node
}
</script>

<button class="chip rounded-full px-2">{Children}</button>
`,
	"views/index.vuka.html": `<script lang="go">
package views

import "example.com/app/design"

type Index struct {
	ID   string
	Hint string
}
</script>

<main>
  <design.Card title="Billing" class="bg-muted p-6 mt-4" id={ID} role="note" aria-label="Billing" data-tester="card" aria-describedby?={Hint}>body</design.Card>
  <design.Input label="Email" placeholder="you@example.com" disabled aria-describedby?={Hint}/>
</main>
`,
	"views/search.vuka.html": `<script lang="go">
package views

import (
	"example.com/app/design"
	"github.com/vuka-lang/ui/v2"
)

type Search struct {
	ui.Live
	Q     string
	Picks int
	open  ui.State[bool]
}

func (s *Search) LiveSchema(l *ui.LiveSchema) { l.Field(&s.Q, ui.Bind{}) }

func (s *Search) Pick() {
	s.Q = "tags"
	s.Picks++
}

func (s *Search) Plain() { s.Picks += 10 }
</script>

<div>
  <design.Input label="Search" bind:value={Q} class="w-64" aria-label="Search"/>
  <design.Chip onClick={Pick()} data-q="tags">tags</design.Chip>
  <design.Chip onClick={open = !open} data-q="toggle">toggle</design.Chip>
  <p>{Q}</p>
  <button class="plain" onClick={Plain()}>plain</button>
</div>
`,
}

// TestFallthroughBuild: what a component's tag passes on reaches its
// forward element against the ui checkout — class merged, attributes
// replacing the element's own, a zero ?= left out — and, on a live page,
// bind:value binds the forward control (typing sets the page's field, the
// re-render keeping the control's merged class and attributes) and an
// onClick is the forward button's, as written on a button.
func TestFallthroughBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range fallthroughModule {
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
	out := stdout.String()
	section := func(name string) string {
		i := strings.Index(out, name+":")
		if i < 0 {
			t.Fatalf("no %s in\n%s", name, out)
		}
		s := out[i:]
		if j := strings.Index(s[1:], "\n"); j >= 0 {
			s = s[:j+1]
		}
		return s
	}
	// the chip's click is its forward button's, as the plain button's is
	push := regexp.MustCompile(`<button data-vk-js-click="\[\[&#34;push&#34;,\{&#34;h&#34;:&#34;c1:\d+&#34;\}\]\]" data-q="tags" class="chip rounded-full px-2">tags</button>`)
	plain := regexp.MustCompile(`<button class="plain" data-vk-js-click="\[\[&#34;push&#34;,\{&#34;h&#34;:&#34;c1:\d+&#34;\}\]\]">plain</button>`)
	if j := section("joined"); !push.MatchString(j) || !plain.MatchString(j) {
		t.Errorf("joined: the chip's onClick isn't its button's:\n%s", j)
	}
	for _, c := range []struct{ section, want string }{
		{"static", `<section id="billing" role="note" aria-label="Billing" data-tester="card" class="rounded-xl border bg-muted p-6 mt-4"><h2>Billing</h2>body</section>`},
		{"static", `<label class="field">Email <input placeholder="you@example.com" disabled class="h-10 rounded-md border px-3"></label>`},
		{"joined", `class="h-10 rounded-md border px-3 w-64"`},
		{"joined", `aria-label="Search"`},
		{"joined", `value="go"`},
		{"joined", `data-q="tags"`},
		{"typed", `typed: orders`},
		{"typed", `value="orders"`},
		{"typed", `class="h-10 rounded-md border px-3 w-64"`},
		{"typed", `<p>orders</p>`},
		{"typed", `data-q="tags"`},
		{"joined", `data-q="toggle" class="chip rounded-full px-2">toggle</button>`},
		{"joined", `<button data-vk-js-click="[[&#34;state_toggle&#34;`},
	} {
		if s := section(c.section); !strings.Contains(s, c.want) {
			t.Errorf("%s: no %s in\n%s", c.section, c.want, s)
		}
	}
}
