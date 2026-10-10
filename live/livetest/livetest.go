// Package livetest is a reference client of the live protocol, in Go: it
// keeps what a browser runtime keeps — the page's HTML for v1, each
// instance's render tree for v2 — and applies the server's messages the way
// a runtime must, so tests can check that what a client ends up with is what
// the server rendered. It is also the executable form of components.md's
// "Protocol v2" section, for runtimes to port.
package livetest

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/live"
)

// Client is one connected page.
type Client struct {
	V     int // the protocol version it joins with: 1 or 2
	S     *live.Session
	Bytes int // the JSON bytes of every server message received

	html    string            // the page
	statics map[int][]string  // v2: statics by id
	strs    map[int]string    // v2: kept markup by id
	trees   map[string]any    // v2: each instance's *frame, or its HTML (a string)
	ref     int
}

type frame struct {
	t int
	d []any // string, *frame, *list, ref
}

type list struct{ c []*frame }

type ref string

// Join connects a client speaking version v to s.
func Join(s *live.Session, v int) (*Client, error) {
	c := &Client{V: v, S: s}
	m, err := c.Send(live.ClientMessage{Type: live.Join, V: v})
	if err != nil {
		return nil, err
	}
	if m.Type != live.Render {
		return nil, fmt.Errorf("join: %s %s", m.Type, m.Error)
	}
	return c, nil
}

// Send sends m through the session's JSON interface and applies the reply.
func (c *Client) Send(m live.ClientMessage) (live.ServerMessage, error) {
	c.ref++
	m.Ref = c.ref
	data, _ := json.Marshal(m)
	out := c.S.HandleJSON(data)
	c.Bytes += len(out)
	var reply live.ServerMessage
	if err := json.Unmarshal(out, &reply); err != nil {
		return reply, err
	}
	if reply.Ref != c.ref {
		return reply, fmt.Errorf("reply to %d, want %d", reply.Ref, c.ref)
	}
	return reply, c.Apply(out)
}

// Event sends an event.
func (c *Client) Event(ev vuka.Event) (live.ServerMessage, error) {
	return c.Send(live.ClientMessage{Type: live.EventMsg, Event: ev})
}

// HTML is the page as the client holds it.
func (c *Client) HTML() string { return c.html }

var handlerAttr = regexp.MustCompile(`data-vk-on-(\w+)="([^"]+)"`)

// Handlers is every event handler on the page: its event and target.
func (c *Client) Handlers() [][2]string {
	var out [][2]string
	for _, m := range handlerAttr.FindAllStringSubmatch(c.html, -1) {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

// RandomEvent is an event of a random handler on the page, with a random
// value and form; ok is false when the page has none.
func (c *Client) RandomEvent(r *rand.Rand, words []string) (vuka.Event, bool) {
	hs := c.Handlers()
	if len(hs) == 0 {
		return vuka.Event{}, false
	}
	h := hs[r.IntN(len(hs))]
	w := words[r.IntN(len(words))]
	return vuka.Event{Target: h[1], Type: h[0], Value: w, Form: url.Values{"name": {w}}}, true
}

// Apply applies a server message's JSON.
func (c *Client) Apply(data []byte) error {
	var m struct {
		Type    string        `json:"type"`
		V       int           `json:"v"`
		HTML    string        `json:"html"`
		Updates []live.Update `json:"updates"`
		Trees   []struct {
			ID   string `json:"id"`
			Full bool   `json:"full"`
			Tree any    `json:"tree"`
			HTML string `json:"html"`
		} `json:"trees"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	switch m.Type {
	case live.Render:
		if m.V < 2 {
			c.html = m.HTML
			return nil
		}
		c.statics, c.strs, c.trees = map[int][]string{}, map[int]string{}, map[string]any{}
	case live.PatchMsg:
		if c.trees == nil {
			for _, u := range m.Updates {
				if err := c.morph(u.ID, u.HTML); err != nil {
					return err
				}
			}
			return nil
		}
	default:
		return nil
	}
	// v2: apply every entry, then morph the updated instances outermost
	// first, as a browser does, and check the result against the page the
	// trees render.
	for _, e := range m.Trees {
		var err error
		switch {
		case e.Full:
			c.trees[e.ID], err = c.frame(e.Tree)
		case e.Tree == nil:
			c.trees[e.ID] = e.HTML
		default:
			f, ok := c.trees[e.ID].(*frame)
			ch, _ := e.Tree.(map[string]any)
			if !ok || ch["u"] == nil {
				return fmt.Errorf("a change for %s, which holds no tree", e.ID)
			}
			err = c.update(f, ch["u"])
		}
		if err != nil {
			return fmt.Errorf("%s: %w", e.ID, err)
		}
	}
	page, err := c.instHTML(live.RootID)
	if err != nil {
		return err
	}
	if m.Type == live.PatchMsg {
		for _, e := range m.Trees {
			h, err := c.instHTML(e.ID)
			if err != nil {
				return err
			}
			if err := c.morph(e.ID, h); err != nil {
				return err
			}
		}
		if c.html != page {
			return fmt.Errorf("the page morphed instance by instance:\n%s\nisn't the page the trees render:\n%s", c.html, page)
		}
	}
	c.html = page
	return nil
}

// morph replaces the element whose data-vk-id is id with html (the page's
// content for the root); an element no longer on the page is skipped.
func (c *Client) morph(id, html string) error {
	if id == live.RootID {
		c.html = html
		return nil
	}
	at := strings.Index(c.html, `data-vk-id="`+id+`"`)
	if at < 0 {
		return nil
	}
	start := strings.LastIndexByte(c.html[:at], '<')
	tag := c.html[start+1 : start+1+strings.IndexAny(c.html[start+1:], " >")]
	end := elementEnd(c.html, start, tag)
	if end < 0 {
		return fmt.Errorf("no end of <%s> for %s", tag, id)
	}
	c.html = c.html[:start] + html + c.html[end:]
	return nil
}

func elementEnd(s string, start int, tag string) int {
	if vuka.Void(tag) {
		return start + strings.IndexByte(s[start:], '>') + 1
	}
	depth, i := 0, start
	for {
		o := indexTag(s[i:], "<"+tag)
		cl := strings.Index(s[i:], "</"+tag+">")
		if cl < 0 {
			return -1
		}
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

// indexTag finds an opening tag named exactly so (<li, not <link).
func indexTag(s, open string) int {
	for off := 0; ; {
		j := strings.Index(s[off:], open)
		if j < 0 {
			return -1
		}
		k := off + j + len(open)
		if k < len(s) && (s[k] == ' ' || s[k] == '>' || s[k] == '/') {
			return off + j
		}
		off = k
	}
}

func (c *Client) frame(v any) (*frame, error) {
	o, ok := v.(map[string]any)
	if !ok || o["t"] == nil {
		return nil, fmt.Errorf("not a frame: %v", v)
	}
	f := &frame{t: int(o["t"].(float64))}
	if s, ok := o["s"].([]any); ok {
		st := make([]string, len(s))
		for i, x := range s {
			st[i] = x.(string)
		}
		c.statics[f.t] = st
	}
	ds, _ := o["d"].([]any)
	for _, d := range ds {
		x, err := c.dyn(d)
		if err != nil {
			return nil, err
		}
		f.d = append(f.d, x)
	}
	if st, ok := c.statics[f.t]; !ok || len(st) != len(f.d)+1 {
		return nil, fmt.Errorf("frame %d: %d dynamics for statics %q", f.t, len(f.d), st)
	}
	return f, nil
}

func (c *Client) dyn(v any) (any, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case map[string]any:
		switch {
		case v["r"] != nil:
			id := int(v["r"].(float64))
			if s, ok := v["v"].(string); ok {
				c.strs[id] = s
			}
			s, ok := c.strs[id]
			if !ok {
				return nil, fmt.Errorf("no string %d", id)
			}
			return s, nil
		case v["c"] != nil:
			l := &list{}
			for _, it := range v["c"].([]any) {
				f, err := c.frame(it)
				if err != nil {
					return nil, err
				}
				l.c = append(l.c, f)
			}
			return l, nil
		case v["id"] != nil:
			return ref(v["id"].(string)), nil
		}
		return c.frame(v)
	}
	return nil, fmt.Errorf("not a dynamic: %v", v)
}

// update applies a frame change's map, {"i": change, …}.
func (c *Client) update(f *frame, u any) error {
	m, ok := u.(map[string]any)
	if !ok {
		return fmt.Errorf("not a change: %v", u)
	}
	for k, ch := range m {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(f.d) {
			return fmt.Errorf("no dynamic %s", k)
		}
		if f.d[i], err = c.change(f.d[i], ch); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) change(old, ch any) (any, error) {
	o, ok := ch.(map[string]any)
	if !ok {
		return c.dyn(ch)
	}
	switch {
	case o["u"] != nil:
		f, ok := old.(*frame)
		if !ok {
			return nil, fmt.Errorf("an update of a non-frame")
		}
		return f, c.update(f, o["u"])
	case o["k"] != nil:
		l, ok := old.(*list)
		if !ok {
			return nil, fmt.Errorf("item steps on a non-loop")
		}
		out := &list{}
		i := 0
		for _, st := range o["k"].([]any) {
			switch st := st.(type) {
			case float64:
				n := int(st)
				if n < 0 {
					i -= n
					continue
				}
				if i+n > len(l.c) {
					return nil, fmt.Errorf("past the items")
				}
				out.c = append(out.c, l.c[i:i+n]...)
				i += n
			case map[string]any:
				switch {
				case st["m"] != nil:
					j := int(st["m"].(float64))
					if j < 0 || j >= len(l.c) {
						return nil, fmt.Errorf("no item %d to move", j)
					}
					if st["u"] != nil {
						if err := c.update(l.c[j], st["u"]); err != nil {
							return nil, err
						}
					}
					out.c = append(out.c, l.c[j])
				case st["u"] != nil:
					if i >= len(l.c) {
						return nil, fmt.Errorf("past the items")
					}
					if err := c.update(l.c[i], st["u"]); err != nil {
						return nil, err
					}
					out.c = append(out.c, l.c[i])
					i++
				default:
					f, err := c.frame(st)
					if err != nil {
						return nil, err
					}
					out.c = append(out.c, f)
				}
			}
		}
		return out, nil
	case o["p"] != nil:
		s, ok := old.(string)
		if !ok {
			return nil, fmt.Errorf("a token patch of a non-string")
		}
		return tokenPatch(s, o["p"].([]any))
	}
	return c.dyn(ch)
}

func tokenPatch(old string, steps []any) (string, error) {
	toks := tokenize(old)
	var b strings.Builder
	i := 0
	for _, st := range steps {
		switch st := st.(type) {
		case string:
			b.WriteString(st)
		case float64:
			n := int(st)
			if n < 0 {
				i -= n
				continue
			}
			if i+n > len(toks) {
				return "", fmt.Errorf("token patch past the end")
			}
			b.WriteString(strings.Join(toks[i:i+n], ""))
			i += n
		case []any:
			if len(st) != 2 {
				return "", fmt.Errorf("a dictionary step in a tree's token patch")
			}
			p, n := int(st[0].(float64)), int(st[1].(float64))
			b.WriteString(strings.Join(toks[p:p+n], ""))
		}
	}
	return b.String(), nil
}

func tokenize(html string) []string {
	var out []string
	start := 0
	for i := 0; i < len(html); i++ {
		switch html[i] {
		case '<':
			if i > start {
				out = append(out, html[start:i])
				start = i
			}
		case '>', '"':
			out = append(out, html[start:i+1])
			start = i + 1
		case ';':
			if i >= 4 && html[i-4:i+1] == "&#34;" {
				out = append(out, html[start:i+1])
				start = i + 1
			}
		}
	}
	if start < len(html) {
		out = append(out, html[start:])
	}
	return out
}

// instHTML renders an instance from what the client holds.
func (c *Client) instHTML(id string) (string, error) {
	var b strings.Builder
	err := c.write(&b, id, 0)
	return b.String(), err
}

func (c *Client) write(b *strings.Builder, id string, depth int) error {
	if depth > 1000 {
		return fmt.Errorf("instances reference each other")
	}
	switch t := c.trees[id].(type) {
	case string:
		b.WriteString(t)
		return nil
	case *frame:
		return c.writeFrame(b, t, depth)
	}
	return fmt.Errorf("no instance %s", id)
}

func (c *Client) writeFrame(b *strings.Builder, f *frame, depth int) error {
	st := c.statics[f.t]
	b.WriteString(st[0])
	for i, d := range f.d {
		switch d := d.(type) {
		case string:
			b.WriteString(d)
		case *frame:
			if err := c.writeFrame(b, d, depth); err != nil {
				return err
			}
		case *list:
			for _, it := range d.c {
				if err := c.writeFrame(b, it, depth); err != nil {
					return err
				}
			}
		case ref:
			if err := c.write(b, string(d), depth+1); err != nil {
				return err
			}
		}
		b.WriteString(st[i+1])
	}
	return nil
}
