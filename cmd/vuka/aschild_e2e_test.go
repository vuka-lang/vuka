package main

import (
	"bytes"
	"strings"
	"testing"
)

var asChildModule = map[string]string{
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
	if _, err := ui.RenderPage(context.Background(), views.Index{}, &b); err != nil {
		panic(err)
	}
	fmt.Println("static:", b.String())

	page := &views.Reports{}
	s := live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.Component("r", nil, page) })
	c, err := livetest.Join(s, 2)
	if err != nil {
		panic(err)
	}
	fmt.Println("joined:", c.HTML())
	for range 2 {
		if err := c.Click("a#report"); err != nil {
			panic(err)
		}
	}
	fmt.Println("clicked:", c.HTML())
}
`,
	"design/button.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

type Button struct {
	Variant  string ` + "`vuka:\"default=solid\"`" + `
	Children ui.Node
}
</script>

<button class="btn rounded-md px-4 py-2" type="button" data-variant={Variant}>{Children}</button>

<script lang="ts">
import { component } from 'vuka:component'
export default component<Button>((el, props) => { el.dataset.ready = props.Variant })
</script>
`,
	"views/index.vuka.html": `<script lang="go">
package views

import "example.com/app/design"

type Index struct{}
</script>

<main>
  <design.Button asChild variant="outline" class="mt-4" aria-label="Report 42">
    <a href="/reports/42" class="px-2" target="_blank" download>Report</a>
  </design.Button>
  <design.Button>Plain</design.Button>
</main>
`,
	"views/reports.vuka.html": `<script lang="go">
package views

import (
	"example.com/app/design"
	"github.com/vuka-lang/ui/v2"
)

type Reports struct {
	ui.Live
	n int
}

func (r *Reports) Open() { r.n++ }
</script>

<div>
  <design.Button asChild>
    <a id="report" href="/reports/42" onClick={Open()}>Report {n}</a>
  </design.Button>
  <p>{n}</p>
</div>
`,
}

// TestAsChildBuild: asChild against the ui checkout. The tag's child is
// the component's root, taking the forward element's attributes — its
// class merged with the child's winning, the component's props (and its
// script's data-vk-p, which never carries the Fallthrough), the tag's
// own fallthrough — while the child keeps its own; on a live page the
// child's handler runs and each re-render keeps the element as it was.
func TestAsChildBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range asChildModule {
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
		if j := strings.Index(s, "\n"); j >= 0 {
			s = s[:j]
		}
		return s
	}
	for _, c := range []struct{ section, want string }{
		// the child's own first, then what the button gives it; the
		// child's px-2 replaces the button's px-4, the tag's mt-4 merged
		{"static", `<a href="/reports/42" class="btn rounded-md py-2 mt-4 px-2" target="_blank" download aria-label="Report 42" data-vk-p="{&#34;Variant&#34;:&#34;outline&#34;}"`},
		{"static", `type="button" data-variant="outline"`},
		{"static", `>Report</a><button data-vk-p="{&#34;Variant&#34;:&#34;solid&#34;}"`},
		{"joined", `<a id="report" href="/reports/42" data-vk-js-click=`},
		{"joined", `>Report 0</a><p>0</p>`},
		{"clicked", `>Report 2</a><p>2</p>`},
	} {
		if s := section(c.section); !strings.Contains(s, c.want) {
			t.Errorf("%s: no %s in\n%s", c.section, c.want, s)
		}
	}
	static := section("static")
	if strings.Contains(out, "Fallthrough") || strings.Contains(out, "aschild") || strings.Count(static, "<button") != 1 {
		t.Errorf("the forward element or its Fallthrough reached the page:\n%s", out)
	}
	// the live element is the same element in each render: its attributes
	// as they were, its handler the same
	attrs := func(s string) string { return s[strings.Index(s, "<a "):strings.Index(s, ">Report")] }
	if a, b := attrs(section("joined")), attrs(section("clicked")); a != b {
		t.Errorf("the link changed across renders:\n%s\n%s", a, b)
	}
}
