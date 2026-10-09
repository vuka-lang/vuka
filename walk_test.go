package vuka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/vuka-lang/vuka"
)

// events records what a Walk sends.
type events []string

func (e *events) Open(el *vuka.Element) error  { *e = append(*e, "<"+el.Tag); return nil }
func (e *events) Close(el *vuka.Element) error { *e = append(*e, "/"+el.Tag); return nil }
func (e *events) Text(s string) error          { *e = append(*e, "t:"+s); return nil }
func (e *events) Raw(s string) error           { *e = append(*e, "r:"+s); return nil }
func (e *events) Opaque(ctx context.Context, n vuka.Node) error {
	s, err := vuka.String(ctx, n)
	*e = append(*e, "o:"+s)
	return err
}

func TestWalk(t *testing.T) {
	opaque := templ.Raw("<i>t</i>")
	tests := []struct {
		name string
		n    vuka.Node
		want string
	}{
		{"element", vuka.El("p", nil, vuka.Text("a"), vuka.El("br", nil)), "<p t:a <br /p"},
		{"flattened", vuka.Fragment(vuka.Child([]any{"a", 1}), vuka.Nodes(func(add func(vuka.Node)) { add(vuka.Text("b")) }), nil), "t:a t:1 t:b"},
		{"raw", vuka.Safe("<b>"), "r:<b>"},
		{"opaque", vuka.El("div", nil, opaque), "<div o:<i>t</i> /div"},
		{"try", vuka.Try(vuka.Text("x"), nil), "t:x"},
		{"boundary replays", vuka.ErrorBoundary(nil, vuka.El("p", nil, vuka.Text("a")), opaque), "<p t:a /p o:<i>t</i>"},
		{"boundary fallback", vuka.Fragment(vuka.Text("before"), vuka.ErrorBoundary(
			func(err error) vuka.Node { return vuka.Text("fb:" + err.Error()) },
			vuka.El("p", nil, vuka.Text("lost")), vuka.Try(nil, errBoom))), "t:before t:fb:boom"},
		{"boundary opaque error", vuka.ErrorBoundary(func(error) vuka.Node { return vuka.Text("fb") }, failing()), "t:fb"},
		{"nested boundaries", vuka.ErrorBoundary(func(error) vuka.Node { return vuka.Text("outer") },
			vuka.ErrorBoundary(func(error) vuka.Node { return vuka.Text("inner") }, failing()), vuka.Text("x")), "t:inner t:x"},
	}
	for _, tt := range tests {
		var e events
		if err := vuka.Walk(context.Background(), tt.n, &e); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got := strings.Join(e, " "); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
	var e events
	if err := vuka.Walk(context.Background(), vuka.El("div", nil, vuka.Text("a"), vuka.Try(nil, errBoom), vuka.Text("b")), &e); !errors.Is(err, errBoom) || len(e) != 2 {
		t.Errorf("error walk: %v %v", err, e)
	}
	if err := vuka.Walk(context.Background(), vuka.El("img", nil, vuka.Text("x")), &e); err == nil {
		t.Error("void children walked")
	}
}

// The HTML a Node renders is a Walk into an HTMLRenderer.
func TestWalkHTMLMatchesRender(t *testing.T) {
	trees := []vuka.Node{
		table(20),
		vuka.El("html", nil, vuka.El("body", nil,
			vuka.El("script", nil, vuka.Text("</script>")), vuka.El("style", nil, vuka.Text("a</b")),
			vuka.ErrorBoundary(func(err error) vuka.Node { return vuka.Child(err) }, vuka.Text("<ok>"), templ.Raw("<i>t</i>")),
			vuka.El("a", []vuka.Attr{{Name: "href", Value: "javascript:x"}, {Name: "style", Value: vuka.Style{"color": "red"}}}, vuka.Safe("&amp;")))),
	}
	for i, n := range trees {
		want, err := vuka.String(context.Background(), n)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		if err := vuka.Walk(context.Background(), n, vuka.NewHTMLRenderer(context.Background(), &b)); err != nil {
			t.Fatal(err)
		}
		if b.String() != want {
			t.Errorf("tree %d:\nwalk   %s\nrender %s", i, b.String(), want)
		}
	}
}

// A plain io.Writer (no WriteString) works too.
type plainWriter struct{ w io.Writer }

func (p plainWriter) Write(b []byte) (int, error) { return p.w.Write(b) }

func TestHTMLRendererPlainWriter(t *testing.T) {
	var b strings.Builder
	n := vuka.El("p", nil, vuka.Text("<x>"), templ.Raw("<i></i>"))
	if err := vuka.Walk(context.Background(), n, vuka.NewHTMLRenderer(context.Background(), plainWriter{&b})); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "<p>&lt;x&gt;<i></i></p>" {
		t.Fatal(got)
	}
}

func TestExportedTypes(t *testing.T) {
	el, ok := vuka.El("div", []vuka.Attr{{Name: "id", Value: "x"}}, vuka.Text("a")).(*vuka.Element)
	if !ok || el.Tag != "div" || len(el.Attrs) != 1 || el.Children[0] != vuka.TextNode("a") {
		t.Fatalf("%#v", el)
	}
	for _, c := range []struct {
		n    vuka.Node
		want string
	}{
		{vuka.Safe("x"), "vuka.RawHTML"}, {vuka.Fragment(), "vuka.Group"}, {vuka.Nodes(nil), "vuka.Builder"},
		{vuka.Try(nil, nil), "vuka.TryNode"}, {vuka.ErrorBoundary(nil), "vuka.Boundary"}, {vuka.Child(nil), "vuka.Group"},
	} {
		if got := fmt.Sprintf("%T", c.n); got != c.want {
			t.Errorf("%s, want %s", got, c.want)
		}
	}
}
