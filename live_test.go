package vuka_test

import (
	"context"
	"testing"

	"github.com/a-h/templ"
	"github.com/vuka-lang/vuka"
)

type tally struct {
	vuka.Live
	Start int
	n     int
}

func (t *tally) Mount() { t.n += t.Start }
func (t *tally) Inc()   { t.n++ }
func (t *tally) Render() vuka.Node {
	return vuka.El("b", []vuka.Attr{{Name: "onClick", Value: vuka.On(t.Inc)}}, vuka.Child(t.n))
}

func TestStaticStateful(t *testing.T) {
	n := vuka.El("li", []vuka.Attr{{Name: "key", Value: 7}}, vuka.Component("x#1", nil, &tally{Start: 2}))
	for range 2 { // each render mounts a copy: the node is unchanged
		if got := render(t, n); got != `<li><b>2</b></li>` {
			t.Fatalf("got %s", got)
		}
	}
	script := vuka.El("button", []vuka.Attr{{Name: "onClick", Value: vuka.On(templ.ComponentScript{Call: "go()"})}})
	if got := render(t, script); got != `<button onClick="go()"></button>` {
		t.Fatalf("a script stays a script: %s", got)
	}
	var w recorder
	if err := vuka.Walk(context.Background(), n, &w); err != nil || w.String() != "<li><b>2</b></li>" {
		t.Fatalf("walk: %q %v", w.String(), err)
	}
}

// recorder writes what a walk sends as bare tags and text.
type recorder struct{ b []byte }

func (r *recorder) Open(e *vuka.Element) error  { r.b = append(r.b, "<"+e.Tag+">"...); return nil }
func (r *recorder) Close(e *vuka.Element) error { r.b = append(r.b, "</"+e.Tag+">"...); return nil }
func (r *recorder) Text(s string) error         { r.b = append(r.b, s...); return nil }
func (r *recorder) Raw(s string) error          { r.b = append(r.b, s...); return nil }
func (r *recorder) Opaque(ctx context.Context, n vuka.Node) error {
	s, err := vuka.String(ctx, n)
	r.b = append(r.b, s...)
	return err
}
func (r *recorder) String() string { return string(r.b) }
