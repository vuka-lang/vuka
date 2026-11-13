package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const tagPage = `package app

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Orders struct {
	ui.Live
	Q     string   ` + "`json:\"q\" bind:\"q,debounce=500\" url:\"q\" validate:\"required,max=50\"`" + `
	Size  int      ` + "`bind:\",debounce=0\"`" + `
	Rows  []string ` + "`defer:\"lists\"`" + `
	Kind  string   ` + "`validate:\"oneof=a b\"`" + `
	total int
}

func (o *Orders) Mount(ctx context.Context) error { return nil }

func (o *Orders) Changed(ctx context.Context, c ui.Changed) error { return nil }

func (o *Orders) LoadLists(ctx context.Context) error { return nil }

func (o *Orders) LoadMore() {}
`

func TestFixAttributes(t *testing.T) {
	t.Parallel()
	out, n, err := attributeTags([]byte(tagPage))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("%d fields changed, want 4", n)
	}
	for _, want := range []string{
		"Q     string   `json:\"q\"` @ui.Bind{Debounce: 500 * time.Millisecond} @ui.URL(\"q\") @validate.Required @validate.Max(50)",
		"Size  int      @ui.Bind{Debounce: ui.NoDebounce}",
		"Rows  []string @ui.Defer(\"lists\")",
		"Kind  string   @validate.OneOf{\"a\", \"b\"}",
		"\t\"context\"\n\t\"time\"\n",
		"\t\"github.com/vuka-lang/ui/v2\"\n\t\"github.com/vuka-lang/ui/v2/validate\"\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, n, _ := attributeTags(out); n != 0 {
		t.Errorf("a second run changed %d fields", n)
	}
}

func TestFixHooks(t *testing.T) {
	t.Parallel()
	live, deferred := deferredGroups([][]byte{[]byte(tagPage)})
	out, marked := markHooks([]byte(tagPage), live, deferred)
	if got := strings.Join(marked, " "); got != "Orders.Mount Orders.Changed Orders.LoadLists" {
		t.Errorf("marked %s (LoadMore isn't a loader)", got)
	}
	for _, want := range []string{"@ui.Mount\nfunc (o *Orders) Mount(", "@ui.OnChange\nfunc (o *Orders) Changed(", "@ui.Loads(\"lists\")\nfunc (o *Orders) LoadLists("} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, again := markHooks(out, live, deferred); len(again) != 0 {
		t.Errorf("a second run marked %v", again)
	}
}

func TestVetLivePages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders.vuka"), []byte(tagPage), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := &load.Package{Dir: dir, Files: []transpile.File{{Name: "orders.vuka", Src: []byte(tagPage)}}}
	var got []string
	for _, d := range vetLivePages(dir, []*load.Package{pkg}) {
		got = append(got, string(d.Code)+" "+d.Message)
	}
	want := []string{
		"W0302 Orders.Q is configured by the tag bind:, url:, validate:, the form of earlier releases",
		"W0302 Orders.Size is configured by the tag bind:, the form of earlier releases",
		"W0302 Orders.Rows is configured by the tag defer:, the form of earlier releases",
		"W0302 Orders.Kind is configured by the tag validate:, the form of earlier releases",
		"E0337 Orders.LoadLists isn't a hook: a loader is declared with @ui.Loads(\"lists\"), never found by its name",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	fixed, _, _ := attributeTags([]byte(tagPage))
	live, deferred := deferredGroups([][]byte{fixed})
	fixed, _ = markHooks(fixed, live, deferred)
	pkg.Files[0].Src = fixed
	os.WriteFile(filepath.Join(dir, "orders.vuka"), fixed, 0o644)
	for _, d := range vetLivePages(dir, []*load.Package{pkg}) {
		t.Errorf("after the fixes: %s", d.Message)
	}
}

func TestIsUpdateMethod(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, params string
		want         bool
	}{
		{"UpdateNewMessage", "m NewMessage", true},
		{"UpdateTyping", "ctx context.Context, t *chat.Typing", true},
		{"UpdateOrders", "o []Order", true},
		{"Update__Typing", "t Typing", true},
		{"UpdateTopic", "topic string", false},
		{"Update", "m any", false},
		{"Save", "o Order", false},
	} {
		if got := isUpdateMethod(c.name, c.params); got != c.want {
			t.Errorf("isUpdateMethod(%s, %q) = %v", c.name, c.params, got)
		}
	}
}

// deferPage is a live page rendered by its own markup whose deferred
// fields, in two files, ui would refuse.
const deferPage = `<script lang="go">
package app

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Dashboard struct {
	ui.Live
	stats map[string]int @ui.Defer("stats")
	Users []string @ui.Defer("users")
	rows  []string @ui.Defer("rows")
	q     string @ui.URL("q") @ui.Defer("q")
	sel   string @ui.Bind @ui.Defer("sel")
	bad   int @ui.Defer("two words")
}

@ui.Loads("stats")
func (d *Dashboard) loadStats(ctx context.Context) error { return nil }
</script>

<p>{len(stats)}</p>
`

const deferLogic = `package app

import "context"

@ui.Loads("rows")
func (d *Dashboard) loadRows(ctx context.Context) error { return nil }
`

func TestDeferDiags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "dashboard.vuka"), []byte(deferLogic), 0o644)
	var got []string
	for _, d := range deferDiags("dashboard.vuka.html", []byte(deferPage), loadersOf([][]byte{[]byte(deferPage), []byte(deferLogic)})) {
		got = append(got, string(d.Code)+" "+d.Message+" ("+d.Help+")")
	}
	want := []string{
		`E0324 Dashboard.Users is deferred, and the group Users has no loader (mark its loader @ui.Loads("users"))`,
		"E0323 Dashboard.q: a deferred field is the server's to load (drop @ui.URL or @ui.Defer)",
		"E0323 Dashboard.sel: a deferred field is the server's to load (drop @ui.Bind or @ui.Defer)",
		`E0323 Dashboard.bad: @ui.Defer("two words"): a group is a name (name the group: @ui.Defer("stats"), loaded by @ui.Loads("stats"))`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The editor reports the file's own, with the loaders of the
	// package's other files as on disk.
	if ds := islandDiags(dir, filepath.Join(dir, "dashboard.vuka.html"), []byte(deferPage)); len(ds) != len(want) {
		t.Errorf("the editor: %d diagnostics, want %d: %v", len(ds), len(want), ds)
	}
}
