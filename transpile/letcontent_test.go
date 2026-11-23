package transpile_test

import (
	"strings"
	"testing"
)

const letShow = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Show[T any] struct {
	Value    *T
	Empty    ui.Node
	Children func(*T) ui.Node
}
</script>

<div>{Empty}</div>
`

const letBox = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Box struct{ Children ui.Node }
</script>

<div>{Children}</div>
`

// TestLetOnTag: a let on a component tag is its default slot's — the
// tag's content, beside its other slots — and the places it can't be say
// where it goes.
func TestLetOnTag(t *testing.T) {
	page := func(markup string) map[string]string {
		return map[string]string{"show.vuka.html": letShow, "box.vuka.html": letBox, "page.vuka.html": `<script lang="go">
package main

type Task struct{ Title string }

type Page struct{ Tasks []Task }
</script>

<main>` + markup + `</main>
`}
	}
	res, err := compileShapes(page(`<Show value={&Tasks[0]} let={t}><b>{t.Title}</b><slot:empty>none</slot:empty></Show>`))
	if err != nil {
		t.Fatal(codes(err))
	}
	var src strings.Builder
	for _, o := range res.Files {
		src.Write(o.Src)
	}
	if !strings.Contains(src.String(), `Show[Task]{Value: &p.Tasks[0], Children: func(t *Task) ui.Node {`) {
		t.Errorf("the content isn't Children's function of t:\n%s", src.String())
	}
	for _, c := range []struct{ markup, want string }{
		{`<Show value={&Tasks[0]} let={t}/>`, "<Show/> has none: give it content, or drop the let"},
		{`<Show value={&Tasks[0]} let={t}><slot:children let={u}>{u.Title}</slot:children></Show>`, "<Show>'s content is in <slot:children>: put the let there"},
		{`<Show value={&Tasks[0]} let={t}><slot:empty>none</slot:empty></Show>`, "it has none outside its slots"},
		{`<Show value={&Tasks[0]} let="t">x</Show>`, "let names the values the slot's content takes"},
		{`<Show value={&Tasks[0]} let={t, i}>{t.Title}</Show>`, "<Show>'s content passes one value, *T: let={t}"},
		{`<Box let={t}>x</Box>`, "<Box>'s content fills ui.Node, which takes no values"},
	} {
		_, err := compileShapes(page(c.markup))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.markup, err, c.want)
		}
	}
}
