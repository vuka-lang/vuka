package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestPushWarnings: a page that isn't live pushing a method that assigns
// its fields, whose copy of the page is thrown away (W0113), and a read
// naming a method that writes through the orm (W0114). A live page's
// pushes may assign theirs.
func TestPushWarnings(t *testing.T) {
	src := `package main

import (
	"context"

	"vukatest.local/opttarget"
	"vukatest.local/orm"
)

type Post struct{ ID int }

type Feed struct{ seen int }

func (p *Feed) Seen(id int) error { p.seen = id; return nil }
func (p *Feed) Fine(id int) error { return nil }
func (p *Feed) Publish(ctx context.Context, id int) (string, error) {
	return "", (&orm.Base[Post]{}).Save(&Post{ID: id}, ctx)
}

type Live struct {
	opttarget.Live
	n int
}

func (p *Live) Bump(by int) error { p.n += by; return nil }

func (p *Feed) Render() opttarget.Node {
	return <div>
		<button onClick={opttarget.JS.Push(p.Seen, 1)} />
		<button onClick={opttarget.JS.Push(p.Fine, 1, opttarget.Read())} />
		<button onClick={opttarget.JS.Push(p.Publish, 1, opttarget.Read())} />
		<button onClick={opttarget.JS.Push(p.Publish, 1)} />
	</div>
}

func (p *Live) Render() opttarget.Node {
	return <button onClick={opttarget.JS.Push(p.Bump, 1)} />
}

func main() {}
`
	res, err := transpile.Package([]transpile.File{{Name: "feed.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range res.Files {
		for _, w := range o.Warnings {
			got = append(got, string(w.Code)+" "+w.Error())
		}
	}
	joined := strings.Join(got, "\n")
	for _, w := range []string{
		"W0113 feed.vuka:29:38: Seen assigns p.seen, but on a page that isn't live a push runs over HTTP on a fresh copy of the page, which is thrown away: the change is lost",
		"W0114 feed.vuka:31:38: Publish calls Save, but the push is a read (ui.Read)",
	} {
		if !strings.Contains(joined, w) {
			t.Errorf("no warning %q in:\n%s", w, joined)
		}
	}
	if len(got) != 2 {
		t.Errorf("%d warnings, want 2:\n%s", len(got), joined)
	}
}

// A method of state a live type embeds runs on the live page's state, however
// a component reaches it: no W0113. State nothing live embeds still warns.
func TestPushEmbeddedState(t *testing.T) {
	const src = `package main

import (
	"context"
	"vukatest.local/opttarget"
)

type orderState struct{ off map[string]bool }

func (s *orderState) Toggle(ctx context.Context, key string) error {
	s.off[key] = !s.off[key]
	return nil
}

type loose struct{ n int }

func (l *loose) Bump(ctx context.Context) error {
	l.n++
	return nil
}

type Tab struct {
	opttarget.Live
	orderState
}

func (p *Tab) Render() opttarget.Node { return <div /> }

type Row struct {
	State *orderState
	L     *loose
}

func (x Row) Render() opttarget.Node {
	return <div>
		<input onClick={opttarget.JS.Push(x.State.Toggle, "a")} />
		<input onClick={opttarget.JS.Push(x.L.Bump)} />
	</div>
}

func main() {}
`
	res, err := transpile.Package([]transpile.File{{Name: "row.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range res.Files {
		for _, w := range o.Warnings {
			got = append(got, string(w.Code)+" "+w.Error())
		}
	}
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "Toggle") {
		t.Errorf("W0113 for state a live type embeds:\n%s", joined)
	}
	if !strings.Contains(joined, "Bump assigns l.n") {
		t.Errorf("no W0113 for state nothing live embeds:\n%s", joined)
	}
}
