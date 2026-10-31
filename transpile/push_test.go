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
		"W0113 feed.vuka:29:38: Seen assigns p.seen, but a page that isn't live pushes it over HTTP to a fresh copy of the page, which is thrown away: the change is lost",
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
