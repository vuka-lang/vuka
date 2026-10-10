package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/live"
)

func a(name string, v any) vuka.Attr { return vuka.Attr{Name: name, Value: v} }

type Counter struct {
	vuka.Live
	Start  int
	Label  string
	n      int
	mounts int
	gone   *[]string
}

func (c *Counter) Mount(ctx context.Context) error { c.n = c.Start; c.mounts++; return nil }
func (c *Counter) Inc()                            { c.n++ }
func (c *Counter) Unmount() {
	if c.gone != nil {
		*c.gone = append(*c.gone, c.Label)
	}
}
func (c *Counter) Render() vuka.Node {
	return vuka.El("div", nil, vuka.El("span", nil, vuka.Text(c.Label), vuka.Child(c.n)),
		vuka.El("button", []vuka.Attr{a("onClick", vuka.On(c.Inc))}, vuka.Text("+")))
}

// client is a reference client: it keeps the page's HTML and applies patches
// the way a browser runtime morphs them.
type client struct {
	t    *testing.T
	s    *live.Session
	html string
	ref  int
}

func join(t *testing.T, render func(context.Context) vuka.Node) *client {
	c := &client{t: t, s: live.NewSession(context.Background(), render)}
	m := c.send(live.ClientMessage{Type: live.Join})
	if m.Type != live.Render {
		t.Fatalf("join: %+v", m)
	}
	c.html = m.HTML
	return c
}

func (c *client) send(m live.ClientMessage) live.ServerMessage {
	c.t.Helper()
	c.ref++
	m.Ref = c.ref
	data, _ := json.Marshal(m)
	var reply live.ServerMessage
	if err := json.Unmarshal(c.s.HandleJSON(data), &reply); err != nil {
		c.t.Fatal(err)
	}
	if reply.Ref != c.ref {
		c.t.Fatalf("reply to %d, want %d", reply.Ref, c.ref)
	}
	return reply
}

// handler finds the n-th handler of event in the page.
func (c *client) handler(event string, n int) string {
	c.t.Helper()
	ms := regexp.MustCompile(`data-vk-on-`+event+`="([^"]+)"`).FindAllStringSubmatch(c.html, -1)
	if n >= len(ms) {
		c.t.Fatalf("no %s handler %d in %s", event, n, c.html)
	}
	return ms[n][1]
}

func (c *client) event(ev vuka.Event) live.ServerMessage {
	c.t.Helper()
	m := c.send(live.ClientMessage{Type: live.EventMsg, Event: ev})
	if m.Type == live.Error {
		c.t.Fatalf("event: %s", m.Error)
	}
	c.apply(m.Updates)
	return m
}

func (c *client) apply(us []live.Update) {
	for _, u := range us {
		if u.ID == live.RootID {
			c.html = u.HTML
			continue
		}
		start := strings.Index(c.html, `data-vk-id="`+u.ID+`"`)
		if start < 0 {
			c.t.Fatalf("no element %s in %s", u.ID, c.html)
		}
		start = strings.LastIndexByte(c.html[:start], '<')
		tag := c.html[start+1 : start+1+strings.IndexAny(c.html[start+1:], " >")]
		end := matchingEnd(c.html, start, tag)
		c.html = c.html[:start] + u.HTML + c.html[end:]
	}
}

func matchingEnd(s string, start int, tag string) int {
	depth, i := 0, start
	for {
		o := strings.Index(s[i:], "<"+tag)
		cl := strings.Index(s[i:], "</"+tag+">")
		if o >= 0 && o < cl {
			depth++
			i += o + 1
			continue
		}
		depth--
		i += cl + len(tag) + 3
		if depth == 0 {
			return i
		}
	}
}

func counters(cs ...*Counter) func(context.Context) vuka.Node {
	return func(context.Context) vuka.Node {
		var kids []vuka.Node
		for _, c := range cs {
			kids = append(kids, vuka.Component("t:1", nil, c))
		}
		return vuka.El("main", nil, kids...)
	}
}

func TestClick(t *testing.T) {
	c := join(t, counters(&Counter{Label: "a", Start: 5}, &Counter{Label: "b"}))
	want := `<main><div data-vk-id="c1"><span>a5</span><button data-vk-on-click="c1:0">+</button></div>` +
		`<div data-vk-id="c2"><span>b0</span><button data-vk-on-click="c2:0">+</button></div></main>`
	if c.html != want {
		t.Fatalf("render:\n%s\nwant\n%s", c.html, want)
	}
	m := c.event(vuka.Event{Target: c.handler("click", 1), Type: "click"})
	if len(m.Updates) != 1 || m.Updates[0].ID != "c2" {
		t.Fatalf("patch: %+v", m.Updates)
	}
	c.event(vuka.Event{Target: c.handler("click", 1), Type: "click"})
	c.event(vuka.Event{Target: c.handler("click", 0), Type: "click"})
	if !strings.Contains(c.html, "<span>a6</span>") || !strings.Contains(c.html, "<span>b2</span>") {
		t.Fatalf("after clicks: %s", c.html)
	}
	if m := c.send(live.ClientMessage{Type: live.EventMsg, Event: vuka.Event{Target: "c9:0"}}); m.Type != live.Error {
		t.Fatalf("unknown handler: %+v", m)
	}
}

func TestStatic(t *testing.T) {
	got, err := vuka.String(context.Background(), counters(&Counter{Label: "a", Start: 2})(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if want := `<main><div><span>a2</span><button>+</button></div></main>`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

type Search struct {
	vuka.Live
	q, key string
	on     bool
	n      int
	err    error
}

type Signup struct {
	Name  string `form:"user_name"`
	Age   int
	Tags  []string `json:"tags"`
	Agree bool
}

func (s *Search) Render() vuka.Node {
	return vuka.El("form", []vuka.Attr{a("onSubmit", vuka.On(func(ctx context.Context, f Signup) error {
		if f.Name == "" {
			return errors.New("name is required")
		}
		s.q = f.Name + "/" + strings.Join(f.Tags, ",")
		s.n, s.on = f.Age, f.Agree
		return nil
	}))},
		vuka.El("input", []vuka.Attr{a("onInput", vuka.On(func(v string) { s.q = v }))}),
		vuka.El("input", []vuka.Attr{a("onKeyDown", vuka.On(func(k string) { s.key = k }))}),
		vuka.El("input", []vuka.Attr{a("onChange", vuka.On(func(b bool) { s.on = b }))}),
		vuka.El("input", []vuka.Attr{a("onChange", vuka.On(func(n int) { s.n = n }))}),
		vuka.El("input", []vuka.Attr{a("onChange", vuka.On(func(f url.Values) { s.q = f.Get("raw") }))}),
		vuka.El("p", nil, vuka.Text(s.q), vuka.Text("|"), vuka.Text(s.key), vuka.Text("|"), vuka.Child(s.on), vuka.Text("|"), vuka.Child(s.n)))
}

func TestPayloads(t *testing.T) {
	c := join(t, func(context.Context) vuka.Node { return vuka.Component("t:1", nil, &Search{}) })
	p := func() string { return c.html[strings.Index(c.html, "<p>"):] }
	c.event(vuka.Event{Target: c.handler("input", 0), Type: "input", Value: "go"})
	c.event(vuka.Event{Target: c.handler("keydown", 0), Type: "keydown", Value: "go", Key: "Enter"})
	c.event(vuka.Event{Target: c.handler("change", 0), Type: "change", Value: "true"})
	c.event(vuka.Event{Target: c.handler("change", 1), Type: "change", Value: "42"})
	if got := p(); got != "<p>go|Enter|true|42</p></form></vk-c>" && !strings.HasPrefix(got, "<p>go|Enter|true|42</p>") {
		t.Fatalf("got %s", got)
	}
	m := c.event(vuka.Event{Target: c.handler("change", 1), Type: "change", Value: "x"})
	if !strings.Contains(m.Error, "isn't a whole number") || len(m.Updates) != 0 {
		t.Fatalf("bad number: %+v", m)
	}
	m = c.event(vuka.Event{Target: c.handler("submit", 0), Type: "submit", Form: url.Values{}})
	if m.Error != "name is required" {
		t.Fatalf("submit error: %+v", m)
	}
	c.event(vuka.Event{Target: c.handler("submit", 0), Type: "submit",
		Form: url.Values{"user_name": {"ann"}, "AGE": {"7"}, "tags": {"x", "y"}, "agree": {"on"}}})
	if got := p(); !strings.HasPrefix(got, "<p>ann/x,y||true|7</p>") && !strings.HasPrefix(got, "<p>ann/x,y|Enter|true|7</p>") {
		t.Fatalf("got %s", got)
	}
	c.event(vuka.Event{Target: c.handler("change", 2), Type: "change", Form: url.Values{"raw": {"r"}}})
	if got := p(); !strings.HasPrefix(got, "<p>r|") {
		t.Fatalf("got %s", got)
	}
}

type List struct {
	vuka.Live
	items []string
	gone  *[]string
}

func (l *List) Remove(s string) {
	for i, x := range l.items {
		if x == s {
			l.items = append(l.items[:i:i], l.items[i+1:]...)
		}
	}
}

func (l *List) Reverse() {
	for i, j := 0, len(l.items)-1; i < j; i, j = i+1, j-1 {
		l.items[i], l.items[j] = l.items[j], l.items[i]
	}
}

func (l *List) Render() vuka.Node {
	return vuka.El("ul", nil, vuka.Nodes(func(add func(vuka.Node)) {
		for _, it := range l.items {
			add(vuka.El("li", []vuka.Attr{a("key", it)},
				vuka.Component("t:2", nil, &Counter{Label: it, gone: l.gone}),
				vuka.El("button", []vuka.Attr{a("onClick", vuka.On(func() { l.Remove(it) }))}, vuka.Text("x"))))
		}
	}), vuka.El("button", []vuka.Attr{a("onClick", vuka.On(l.Reverse))}))
}

func TestKeyedList(t *testing.T) {
	var gone []string
	l := &List{items: []string{"a", "b", "c"}, gone: &gone}
	c := join(t, func(context.Context) vuka.Node { return vuka.Component("t:1", "list", l) })
	idOf := func(label string) string {
		m := regexp.MustCompile(`data-vk-id="(c\d+)"><span>` + label).FindStringSubmatch(c.html)
		if m == nil {
			t.Fatalf("no %s in %s", label, c.html)
		}
		return m[1]
	}
	a, b := idOf("a"), idOf("b")
	// increment b, then reverse: b keeps its instance and its count
	c.event(vuka.Event{Target: c.handler("click", 2), Type: "click"})
	if !strings.Contains(c.html, "<span>b1</span>") {
		t.Fatalf("b not incremented: %s", c.html)
	}
	last := strings.Count(c.html, "data-vk-on-click") - 1
	c.event(vuka.Event{Target: c.handler("click", last), Type: "click"})
	if idOf("a") != a || idOf("b") != b || !strings.Contains(c.html, "<span>b1</span>") || strings.Index(c.html, ">c0<") > strings.Index(c.html, ">a0<") {
		t.Fatalf("after reverse: %s", c.html)
	}
	// remove b (now the middle item): its instance is dropped and unmounted
	c.event(vuka.Event{Target: c.handler("click", 3), Type: "click"})
	if strings.Contains(c.html, "<span>b") {
		t.Fatalf("b still there: %s", c.html)
	}
	if len(gone) != 1 || gone[0] != "b" {
		t.Fatalf("unmounted %v", gone)
	}
	if got := c.html; strings.Contains(got, `"`+b+`"`) {
		t.Fatalf("b's id still rendered: %s", got)
	}
}

type Chat struct {
	vuka.Live
	Room string
	log  []string
}

type NewMessage struct{ Text string }
type Typing struct{ Who string }

func (c *Chat) Mount() { c.Subscribe("room:" + c.Room) }

// Overloads, as Vuka names them.
func (c *Chat) Update__NewMessage(m NewMessage) { c.log = append(c.log, "msg "+m.Text) }
func (c *Chat) Update__Typing(ctx context.Context, m Typing) error {
	c.log = append(c.log, "typing "+m.Who)
	return nil
}
func (c *Chat) Update__any(m any) { c.log = append(c.log, "other") }

func (c *Chat) Render() vuka.Node {
	return vuka.El("ul", nil, vuka.Nodes(func(add func(vuka.Node)) {
		for _, l := range c.log {
			add(vuka.El("li", nil, vuka.Text(l)))
		}
	}))
}

func TestInfo(t *testing.T) {
	s := live.NewSession(context.Background(), func(context.Context) vuka.Node {
		return vuka.Fragment(vuka.Component("t:1", nil, &Chat{Room: "go"}), vuka.Component("t:1", nil, &Chat{Room: "rust"}))
	})
	if _, err := s.Render(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Topics(), ","); got != "room:go,room:rust" {
		t.Fatalf("topics %s", got)
	}
	p, err := s.Info("room:go", NewMessage{"hi"})
	if err != nil || len(p.Updates) != 1 || p.Updates[0].ID != "c1" || !strings.Contains(p.Updates[0].HTML, "<li>msg hi</li>") {
		t.Fatalf("info: %+v %v", p, err)
	}
	p, _ = s.Info("room:go", Typing{"ann"})
	if !strings.Contains(p.Updates[0].HTML, "<li>typing ann</li>") {
		t.Fatalf("typing: %+v", p)
	}
	p, _ = s.Info("", 42)
	if len(p.Updates) != 2 || !strings.Contains(p.Updates[1].HTML, "<li>other</li>") {
		t.Fatalf("broadcast: %+v", p)
	}
	if p, _ := s.Info("room:none", NewMessage{"x"}); len(p.Updates) != 0 {
		t.Fatalf("nobody subscribed: %+v", p)
	}
}

func TestMountOnceAndProps(t *testing.T) {
	label := "a"
	var gone []string
	c := &Counter{Start: 3}
	s := live.NewSession(context.Background(), func(context.Context) vuka.Node {
		if label == "" {
			return vuka.El("main", nil)
		}
		cp := *c
		cp.Label, cp.gone = label, &gone
		return vuka.El("main", nil, vuka.Component("t:1", nil, &cp))
	})
	html, _ := s.Render()
	target := regexp.MustCompile(`data-vk-on-click="([^"]+)"`).FindStringSubmatch(html)[1]
	label = "b"
	p, err := s.Event(vuka.Event{Target: target})
	if err != nil || len(p.Updates) != 1 || p.Updates[0].ID != "c1" || !strings.Contains(p.Updates[0].HTML, "<span>b4</span>") {
		t.Fatalf("props set again, state kept: %+v %v", p, err)
	}
	if c.mounts != 0 {
		t.Fatal("the tag's props were mounted")
	}
	label = ""
	p, _ = s.Event(vuka.Event{Target: target})
	if len(p.Updates) != 1 || p.Updates[0].ID != live.RootID || len(gone) != 1 {
		t.Fatalf("removed: %+v %v", p, gone)
	}
}

func TestKeysRendered(t *testing.T) {
	c := join(t, func(context.Context) vuka.Node {
		return vuka.El("ul", nil,
			vuka.El("li", []vuka.Attr{a("key", 7)}, vuka.Text("x")),
			vuka.El("li", []vuka.Attr{a("key", "q\"")}, vuka.Text("y")),
			vuka.Component("t:1", "row-1", &Counter{Label: "a"}),
			vuka.El("li", []vuka.Attr{a("key", nil)}))
	})
	for _, want := range []string{`<li data-vk-key="7">x</li>`, `<li data-vk-key="q&#34;">y</li>`, `data-vk-key="row-1"`, `<li></li>`} {
		if !strings.Contains(c.html, want) {
			t.Fatalf("no %s in %s", want, c.html)
		}
	}
	if !regexp.MustCompile(`data-vk-id="c\d+" data-vk-key="row-1"`).MatchString(c.html) {
		t.Fatalf("component root lacks its key: %s", c.html)
	}
}
