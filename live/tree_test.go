package live_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/live"
	"github.com/vuka-lang/vuka/live/internal/demo"
	"github.com/vuka-lang/vuka/live/livetest"
)

// TestTreeProperty sends random events to the same page over v1 and v2: after
// each, both clients hold the page the server rendered, byte for byte.
func TestTreeProperty(t *testing.T) {
	pages := map[string]func(context.Context) vuka.Node{
		"page":     demo.Page,
		"counters": demo.Counters(20),
		"table": func(context.Context) vuka.Node {
			return vuka.Fragment(vuka.Component("t#1", nil, &demo.Table{N: 30}), vuka.Component("t#2", nil, &demo.Table{N: 5, Plain: true}),
				vuka.Component("t#3", nil, &demo.CellTable{N: 20}))
		},
	}
	for name, page := range pages {
		for seed := range uint64(12) {
			r := rand.New(rand.NewPCG(seed, 7))
			s1, s2 := live.NewSession(context.Background(), page), live.NewSession(context.Background(), page)
			c1, err := livetest.Join(s1, 1)
			if err != nil {
				t.Fatal(err)
			}
			c2, err := livetest.Join(s2, 2)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 150 {
				if c1.HTML() != c2.HTML() || c2.HTML() != s2.HTML() || c1.HTML() != s1.HTML() {
					t.Fatalf("%s seed %d step %d:\nv1     %s\nv2     %s\nserver %s", name, seed, i, c1.HTML(), c2.HTML(), s2.HTML())
				}
				if r.IntN(40) == 0 { // reconnect
					if c2, err = livetest.Join(s2, 2); err != nil {
						t.Fatal(err)
					}
					continue
				}
				ev, ok := c2.RandomEvent(r, demo.Words)
				if !ok {
					break
				}
				m1, err1 := c1.Event(ev)
				m2, err2 := c2.Event(ev)
				if err1 != nil || err2 != nil || m1.Type != m2.Type || m1.Error != m2.Error {
					t.Fatalf("%s seed %d step %d %+v:\nv1 %+v %v\nv2 %+v %v", name, seed, i, ev, m1, err1, m2, err2)
				}
			}
		}
	}
}

// TestTreeOneCell: a click in one row of a table sends one small change.
func TestTreeOneCell(t *testing.T) {
	s := live.NewSession(context.Background(), func(context.Context) vuka.Node {
		return vuka.Component("t#1", nil, &demo.CellTable{N: 100})
	})
	c, err := livetest.Join(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Event(vuka.Event{Target: c.Handlers()[42][1], Type: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Trees) != 1 || string(m.Trees[0].Tree) != `{"u":{"1":{"k":[42,{"u":{"2":"1"}},57]}}}` {
		t.Fatalf("patch: %+v %s", m.Trees, m.Trees[0].Tree)
	}
}
