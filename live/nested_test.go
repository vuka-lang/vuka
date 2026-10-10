package live_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka"
)

type Item struct {
	vuka.Live
	Name   string
	OnPick func(string)
	n      int
}

func (c *Item) Inc() { c.n++ }
func (c *Item) Pick() {
	if c.OnPick != nil {
		c.OnPick(c.Name)
	}
}
func (c *Item) Render() vuka.Node {
	return vuka.El("li", nil, vuka.Text(c.Name), vuka.Child(c.n),
		vuka.El("button", []vuka.Attr{a("onClick", vuka.On(c.Inc))}, vuka.Text("+")),
		vuka.El("button", []vuka.Attr{a("onClick", vuka.On(c.Pick))}, vuka.Text("pick")))
}

type Board struct {
	vuka.Live
	picked string
	title  int
}

func (b *Board) Retitle() { b.title++ }
func (b *Board) Render() vuka.Node {
	return vuka.El("section", nil,
		vuka.El("h1", nil, vuka.Text("board "), vuka.Child(b.title), vuka.Text(" picked:"), vuka.Text(b.picked)),
		vuka.El("button", []vuka.Attr{a("onClick", vuka.On(b.Retitle))}, vuka.Text("retitle")),
		vuka.El("ul", nil,
			vuka.Component("n:item", "x", &Item{Name: "x", OnPick: func(s string) { b.picked = s }}),
			vuka.Component("n:item", "y", &Item{Name: "y", OnPick: func(s string) { b.picked = s }})))
}

func TestNested(t *testing.T) {
	c := join(t, func(context.Context) vuka.Node { return vuka.El("main", nil, vuka.Component("n:board", nil, &Board{})) })
	// handlers in page order: retitle, x+, xpick, y+, ypick
	m := c.event(vuka.Event{Target: c.handler("click", 3), Type: "click"})
	if len(m.Updates) != 1 || strings.Contains(m.Updates[0].HTML, "board") || !strings.Contains(m.Updates[0].HTML, "y1") {
		t.Fatalf("a child's own event patches only the child: %+v", m.Updates)
	}
	m = c.event(vuka.Event{Target: c.handler("click", 0), Type: "click"})
	if len(m.Updates) != 1 || !strings.Contains(m.Updates[0].HTML, "board 1") {
		t.Fatalf("the parent's event patches the parent: %+v", m.Updates)
	}
	if !strings.Contains(c.html, "y1") {
		t.Fatalf("the child keeps its state when the parent re-renders: %s", c.html)
	}
	m = c.event(vuka.Event{Target: c.handler("click", 2), Type: "click"})
	if len(m.Updates) != 1 || !strings.Contains(m.Updates[0].HTML, "picked:x") {
		t.Fatalf("a child changes the parent through a func prop: %+v", m.Updates)
	}
	if !strings.Contains(c.html, "picked:x") || !strings.Contains(c.html, "y1") || !strings.Contains(c.html, "board 1") {
		t.Fatalf("page after nested events: %s", c.html)
	}
}
