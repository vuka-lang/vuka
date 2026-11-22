package transpile_test

import (
	"strings"
	"testing"
)

const htmlCard = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Card struct {
	Title    string
	Body     ui.RawHTML
	Footer   ui.Node
	Children ui.Node
}
</script>

<section>
  <h2>{Title}</h2>
  <div class="body" innerHTML={Body} />
  {Children}
  <footer>{Footer}</footer>
</section>
`

const htmlPost = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Post struct {
	ui.Live
	Title string
	Body  ui.RawHTML
	Note  string
	draft ui.State[ui.RawHTML]
}

func (p *Post) Edit(body string) { p.Body = ui.RawHTML(body) }
</script>

<article>
  <h1>{Title}</h1>
  <div class="raw" innerHTML={Body}></div>
  <p>{Note}</p>
  <button onClick={Edit("<b>new</b>")}>edit</button>
  @@
</article>
`

func compileHTML(t *testing.T, markup string) (string, error) {
	t.Helper()
	res, err := compileShapes(map[string]string{
		"post.vuka.html": strings.Replace(htmlPost, "@@", markup, 1),
		"card.vuka.html": htmlCard,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, o := range res.Files {
		b.Write(o.Src)
	}
	return b.String(), nil
}

// TestInnerHTML: innerHTML={x} is the element's one child, on a live page
// and in a component; a RawHTML prop, child or slot is a hole like any
// other, and a string hole is the same hole it always was (ui escapes it).
func TestInnerHTML(t *testing.T) {
	src, err := compileHTML(t, `<Card title={Title} body={Body}>{Body}<slot:footer>{Body}</slot:footer></Card>`)
	if err != nil {
		t.Fatal(codes(err))
	}
	for _, want := range []string{
		`<div class="raw">{p.Body}</div>`,
		`<p>{p.Note}</p>`,
		`<div class="body">{c.Body}</div>`,
		`Card{Title: p.Title, Body: p.Body, Footer: p.Body, Children: p.Body}`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in:\n%s", want, src)
		}
	}
	if strings.Contains(src, "innerHTML") || strings.Contains(src, "innerhtml") {
		t.Errorf("innerHTML is an attribute still:\n%s", src)
	}
}

// TestInnerHTMLErrors: anything but a RawHTML in innerHTML is E0133;
// innerHTML beside children, quoted, or where no markup goes, E0134.
func TestInnerHTMLErrors(t *testing.T) {
	for _, tc := range []struct{ markup, want string }{
		{`<div innerHTML={Note} />`, `E0133 post.vuka.html:22:8: <div innerHTML={Note}>: innerHTML takes a ui.RawHTML, and Note is a string; wrap it in ui.Sanitize(Note) when users write it, or ui.RawHTML(Note) when it is trusted`},
		{`<div innerHTML={"<b>x</b>"} />`, `E0133 post.vuka.html:22:8: <div innerHTML={"<b>x</b>"}>: innerHTML takes a ui.RawHTML, and "<b>x</b>" is a string`},
		{`<div innerHTML={draft} />`, `E0133 post.vuka.html:22:8: <div innerHTML={draft}>: innerHTML takes a ui.RawHTML, and draft is a stateui.State[stateui.RawHTML]; a state changes in the browser, as text: keep the HTML in a field the server renders, ui.RawHTML`},
		{`<div innerHTML={Body}>more</div>`, `E0134 post.vuka.html:22:8: <div> has innerHTML and children`},
		{`<div innerHTML={Body}><b>{Note}</b></div>`, `E0134 post.vuka.html:22:8: <div> has innerHTML and children`},
		{`<div innerHTML="<b>x</b>" />`, `E0134 post.vuka.html:22:8: <div innerHTML> takes a ui.RawHTML: innerHTML={body}`},
		{`<br innerHTML={Body} />`, `E0134 post.vuka.html:22:7: <br> holds no markup`},
		{`<script innerHTML={Body}></script>`, `E0134 post.vuka.html:22:11: <script> holds no markup`},
	} {
		_, err := compileHTML(t, tc.markup)
		if err == nil || !strings.Contains(codes(err), tc.want) {
			t.Errorf("%s: no %q in\n%v", tc.markup, tc.want, err)
		}
	}
}
