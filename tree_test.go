package vuka_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka"
)

func TestBuildTree(t *testing.T) {
	ctx := context.Background()
	rows := func(n int) vuka.Node {
		return vuka.Nodes(func(add func(vuka.Node)) {
			for i := range n {
				add(vuka.F(2, "Ed(h)", vuka.El("li", []vuka.Attr{{Name: "key", Value: i}}, vuka.Child(i))))
			}
		})
	}
	n := vuka.F(1, "Esd(tbE(h))tE()", vuka.El("ul", []vuka.Attr{{Name: "className", Value: "x"}, {Name: "id", Value: 7}},
		vuka.Text("a<"), rows(3), vuka.El("script", nil, vuka.Child("</script>"))), vuka.Text(" "), vuka.El("br", nil))
	tr, err := vuka.BuildTree(ctx, n, true)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	tr.HTML(&b, nil)
	want, _ := vuka.String(ctx, n)
	if b.String() != want {
		t.Fatalf("tree html\n%s\nwant\n%s", b.String(), want)
	}
	if len(tr.Statics) != len(tr.Dyn)+1 || len(tr.Dyn) != 3 {
		t.Fatalf("statics %q dyn %#v", tr.Statics, tr.Dyn)
	}
	if l := tr.Dyn[1].(*vuka.TreeList); len(l.Items) != 3 || l.Keys[2] != "int=2" {
		t.Fatalf("list %#v", l)
	}
	bad := vuka.F(3, "E(h)", vuka.Text("not an element"))
	tr, err = vuka.BuildTree(ctx, bad, true)
	if err != nil || len(tr.Dyn) != 1 || tr.Dyn[0] != "not an element" {
		t.Fatalf("a frame its shape doesn't fit is opaque: %#v %v", tr, err)
	}
}
