package live_test

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
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
	defer func(v bool) { live.Verify = v }(live.Verify)
	seen := map[string]int{}
	for name, page := range pages {
		for seed := range uint64(12) {
			live.Verify = seed%2 == 0 // odd seeds take Assign skipping's own path
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
				for _, u := range m2.Trees {
					for _, k := range []string{`"m":`, `"c":`, `"r":`, `"p":`, `"k":`, `"id":`} {
						seen[k] += strings.Count(string(u.Tree), k)
					}
					if u.HTML != "" {
						seen["html"]++
					}
				}
				if err1 != nil || err2 != nil || m1.Type != m2.Type || m1.Error != m2.Error {
					t.Fatalf("%s seed %d step %d %+v:\nv1 %+v %v\nv2 %+v %v", name, seed, i, ev, m1, err1, m2, err2)
				}
			}
		}
	}
	t.Log(seen)
	for _, k := range []string{`"m":`, `"c":`, `"r":`, `"k":`, `"id":`} {
		if seen[k] == 0 {
			t.Errorf("no patch had %s", k)
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

type longText struct {
	vuka.Live
	n int
}

func (l *longText) Inc() { l.n++ }
func (l *longText) Render() vuka.Node {
	long := strings.Repeat("<i>static-ish text</i>", 60) + strconv.Itoa(l.n) + strings.Repeat("<b>more</b>", 60)
	return vuka.F(91, "Ed(h)", vuka.El("div", []vuka.Attr{a("onClick", vuka.On(l.Inc))}, vuka.Safe(long)))
}

type manyCells struct {
	vuka.Live
	n int
}

func (m *manyCells) Inc() { m.n++ }
func (m *manyCells) Render() vuka.Node {
	kids, shape := []vuka.Node{}, "Ed("
	for i := range 100 {
		kids, shape = append(kids, vuka.Child((m.n+i)%10)), shape+"h"
	}
	return vuka.F(92, shape+")", vuka.El("p", []vuka.Attr{a("onClick", vuka.On(m.Inc))}, kids...))
}

// TestTreeLongAndFallback: long markup changes by token patch; a change
// bigger than its instance's HTML comes as the HTML.
func TestTreeLongAndFallback(t *testing.T) {
	s := live.NewSession(context.Background(), func(context.Context) vuka.Node {
		return vuka.Fragment(vuka.Component("l#1", nil, &longText{}), vuka.Component("l#2", nil, &manyCells{}))
	})
	c, err := livetest.Join(s, 2)
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Event(vuka.Event{Target: "c1:0", Type: "click"})
	if err != nil || len(m.Trees) != 1 || !strings.Contains(string(m.Trees[0].Tree), `"p":`) {
		t.Fatalf("token patch: %+v %v", m.Trees, err)
	}
	m, err = c.Event(vuka.Event{Target: "c2:0", Type: "click"})
	if err != nil || len(m.Trees) != 1 || m.Trees[0].HTML == "" {
		t.Fatalf("html fallback: %+v %v", m.Trees, err)
	}
	m, err = c.Event(vuka.Event{Target: "c2:0", Type: "click"})
	if err != nil || len(m.Trees) != 1 || !m.Trees[0].Full {
		t.Fatalf("after html, a full tree: %+v %v", m.Trees, err)
	}
	if c.HTML() != s.HTML() {
		t.Fatalf("client %s\nserver %s", c.HTML(), s.HTML())
	}
}
