package live

import (
	"hash/maphash"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vuka-lang/vuka"
)

// A v2 connection holds each instance's render tree (vuka.Tree) and gets
// the changes to it; the encoding is documented in components.md, "Protocol
// v2". The server keeps, per instance, a shadow of the tree the client
// holds: frames and lists as they were, each string as a hash (and markup
// longer than longMarkup whole, for token patches), so a page costs a few
// bytes per dynamic, not a copy of its HTML.

const (
	refMinLen  = 64   // markup kept by the connection, sent once, from this length
	refMax     = 4096 // strings a connection keeps
	longMarkup = 1024 // markup changed by token patches, from this length
	fallbackAt = 256  // changes this long are compared with the instance's HTML
)

type sframe struct {
	key uint64
	d   []any // sstr, *sframe, *slist, vuka.TreeRef
}

type sstr struct {
	h    uint64
	long string
}

type slist struct {
	items []*sframe
	keys  []string
}

var seed = maphash.MakeSeed()

func sig(s string) uint64 { return maphash.String(seed, s) }

var emptyShadow = sstr{h: sig("")}

// shadow is what a connection keeps of t once the client holds it.
func shadow(t *vuka.Tree) *sframe {
	f := &sframe{key: t.Key, d: make([]any, len(t.Dyn))}
	for i, d := range t.Dyn {
		f.d[i] = shadowDyn(d)
	}
	return f
}

func shadowDyn(d any) any {
	switch d := d.(type) {
	case string:
		if d == "" {
			return emptyShadow
		}
		s := sstr{h: sig(d)}
		if len(d) >= longMarkup {
			s.long = d
		}
		return s
	case *vuka.Tree:
		return shadow(d)
	case *vuka.TreeList:
		l := &slist{items: make([]*sframe, len(d.Items)), keys: d.Keys}
		for i, it := range d.Items {
			l.items[i] = shadow(it)
		}
		return l
	}
	return d
}

// conn is a v2 connection's state beyond the shadows: the statics and the
// long markup the client holds, by id.
type conn struct {
	ids     map[uint64]int // statics key → id
	strs    map[uint64]int // markup sig → id
	newIDs  []uint64       // what the encoding under way added, for a rollback
	newStrs []uint64
}

func (c *conn) reset() { *c = conn{ids: map[uint64]int{}, strs: map[uint64]int{}} }

// begin starts an entry's encoding; rollback undoes what it allocated.
func (c *conn) begin() { c.newIDs, c.newStrs = c.newIDs[:0], c.newStrs[:0] }

func (c *conn) rollback() {
	for _, k := range c.newIDs {
		delete(c.ids, k)
	}
	for _, k := range c.newStrs {
		delete(c.strs, k)
	}
}

// enc writes the JSON of trees and changes.
type enc struct {
	c *conn
	b []byte
}

func (e *enc) raw(s string) { e.b = append(e.b, s...) }

func (e *enc) int(n int) { e.b = strconv.AppendInt(e.b, int64(n), 10) }

// quote writes s as a JSON string, markup as markup (no <).
func (e *enc) quote(s string) {
	e.b = append(e.b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c < 0x80 {
			i++
			continue
		}
		if c >= 0x80 {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r != utf8.RuneError || size != 1 {
				if r != ' ' && r != ' ' {
					i += size
					continue
				}
			}
			e.b = append(e.b, s[start:i]...)
			if r == utf8.RuneError {
				e.raw(`�`)
			} else {
				e.raw(`\u` + strconv.FormatInt(int64(r), 16))
			}
			i += size
			start = i
			continue
		}
		e.b = append(e.b, s[start:i]...)
		switch c {
		case '"':
			e.raw(`\"`)
		case '\\':
			e.raw(`\\`)
		case '\n':
			e.raw(`\n`)
		case '\r':
			e.raw(`\r`)
		case '\t':
			e.raw(`\t`)
		default:
			e.raw(`\u00`)
			e.b = append(e.b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&15])
		}
		i++
		start = i
	}
	e.b = append(e.b, s[start:]...)
	e.b = append(e.b, '"')
}

// frame writes a whole frame.
func (e *enc) frame(t *vuka.Tree) {
	id, known := e.c.ids[t.Key]
	if !known {
		id = len(e.c.ids)
		e.c.ids[t.Key] = id
		e.c.newIDs = append(e.c.newIDs, t.Key)
	}
	e.raw(`{"t":`)
	e.int(id)
	if !known {
		e.raw(`,"s":[`)
		for i, s := range t.Statics {
			if i > 0 {
				e.raw(",")
			}
			e.quote(s)
		}
		e.raw("]")
	}
	e.raw(`,"d":[`)
	for i, d := range t.Dyn {
		if i > 0 {
			e.raw(",")
		}
		e.dyn(d)
	}
	e.raw("]}")
}

func (e *enc) dyn(d any) {
	switch d := d.(type) {
	case string:
		e.str(d)
	case *vuka.Tree:
		e.frame(d)
	case *vuka.TreeList:
		e.list(d)
	case vuka.TreeRef:
		e.raw(`{"id":`)
		e.quote(string(d))
		e.raw("}")
	default:
		e.raw(`""`)
	}
}

func (e *enc) list(l *vuka.TreeList) {
	e.raw(`{"c":[`)
	for i, it := range l.Items {
		if i > 0 {
			e.raw(",")
		}
		e.frame(it)
	}
	e.raw("]}")
}

// str writes markup: long markup by reference once the client keeps it.
func (e *enc) str(s string) {
	if len(s) < refMinLen {
		e.quote(s)
		return
	}
	k := sig(s)
	if id, ok := e.c.strs[k]; ok {
		e.raw(`{"r":`)
		e.int(id)
		e.raw("}")
		return
	}
	if len(e.c.strs) >= refMax {
		e.quote(s)
		return
	}
	id := len(e.c.strs)
	e.c.strs[k] = id
	e.c.newStrs = append(e.c.newStrs, k)
	e.raw(`{"r":`)
	e.int(id)
	e.raw(`,"v":`)
	e.quote(s)
	e.raw("}")
}

// frameChange writes {"u":{…}}, the change from a to b (frames with the same
// statics), and reports whether there is one; if not it writes nothing.
func (e *enc) frameChange(a *sframe, b *vuka.Tree) bool {
	start := len(e.b)
	e.raw(`{"u":`)
	if !e.changes(a, b) {
		e.b = e.b[:start]
		return false
	}
	e.raw("}")
	return true
}

// changes writes the map of b's changed dynamics, {"i":change,…}.
func (e *enc) changes(a *sframe, b *vuka.Tree) bool {
	start := len(e.b)
	e.raw("{")
	n := 0
	for i, d := range b.Dyn {
		at := len(e.b)
		if n > 0 {
			e.raw(",")
		}
		e.raw(`"`)
		e.int(i)
		e.raw(`":`)
		if e.dynChange(a.d[i], d) {
			n++
		} else {
			e.b = e.b[:at]
		}
	}
	if n == 0 {
		e.b = e.b[:start]
		return false
	}
	e.raw("}")
	return true
}

// dynChange writes the change from a dynamic's shadow a to b, if any.
func (e *enc) dynChange(a, b any) bool {
	switch b := b.(type) {
	case string:
		old, ok := a.(sstr)
		if ok && old.h == sig(b) {
			return false
		}
		if ok && old.long != "" && len(b) >= longMarkup {
			if e.tokenPatch(old.long, b) {
				return true
			}
		}
		e.str(b)
	case *vuka.Tree:
		if old, ok := a.(*sframe); ok && old.key == b.Key {
			return e.frameChange(old, b)
		}
		e.frame(b)
	case *vuka.TreeList:
		if old, ok := a.(*slist); ok {
			return e.listChange(old, b)
		}
		e.list(b)
	case vuka.TreeRef:
		if old, ok := a.(vuka.TreeRef); ok && old == b {
			return false
		}
		e.dyn(b)
	default:
		e.dyn(b)
	}
	return true
}

// A list change is planned first: which old item each new one comes from.
type step struct {
	op       byte // '=' an old item at the cursor, 'm' an old item elsewhere, '+' a new one, '-' skip old items
	old, new int
}

// listChange writes {"k":[…]} (or the whole list, when no item stays), the
// change from a to b, if any.
func (e *enc) listChange(a *slist, b *vuka.TreeList) bool {
	plan := keyedPlan(a, b)
	if plan == nil {
		plan = positionalPlan(a, b)
	}
	taken := 0
	for _, s := range plan {
		if s.op == '=' || s.op == 'm' {
			taken++
		}
	}
	if taken == 0 {
		if len(a.items) == 0 && len(b.Items) == 0 {
			return false
		}
		e.list(b)
		return true
	}
	start := len(e.b)
	e.raw(`{"k":[`)
	n, keep, changed := 0, 0, taken < len(a.items)
	sep := func() {
		if n > 0 {
			e.raw(",")
		}
		n++
	}
	flush := func() {
		if keep > 0 {
			sep()
			e.int(keep)
			keep = 0
		}
	}
	for _, s := range plan {
		switch s.op {
		case '=':
			at := len(e.b)
			if e.frameChange(a.items[s.old], b.Items[s.new]) {
				ch := append([]byte(nil), e.b[at:]...)
				e.b = e.b[:at]
				flush()
				sep()
				e.b = append(e.b, ch...)
				changed = true
			} else {
				keep++
			}
		case 'm':
			flush()
			sep()
			e.raw(`{"m":`)
			e.int(s.old)
			if e.b = append(e.b, `,"u":`...); !e.changes(a.items[s.old], b.Items[s.new]) {
				e.b = e.b[:len(e.b)-len(`,"u":`)]
			}
			e.raw("}")
			changed = true
		case '+':
			flush()
			sep()
			e.frame(b.Items[s.new])
			changed = true
		case '-':
			flush()
			sep()
			e.int(-s.old)
			changed = true
		}
	}
	if !changed {
		e.b = e.b[:start]
		return false
	}
	flush()
	e.raw("]}")
	return true
}

// positionalPlan pairs items by position.
func positionalPlan(a *slist, b *vuka.TreeList) []step {
	var plan []step
	for j, it := range b.Items {
		switch {
		case j >= len(a.items):
			plan = append(plan, step{op: '+', new: j})
		case a.items[j].key == it.Key:
			plan = append(plan, step{op: '=', old: j, new: j})
		default:
			plan = append(plan, step{op: '+', new: j}, step{op: '-', old: 1})
		}
	}
	return plan
}

// keyedPlan pairs items by key, keeping the longest run of items in order
// and moving the others; nil when the lists aren't both keyed uniquely.
func keyedPlan(a *slist, b *vuka.TreeList) []step {
	if a.keys == nil || b.Keys == nil || len(a.keys) != len(a.items) {
		return nil
	}
	oldAt := make(map[string]int, len(a.keys))
	for i, k := range a.keys {
		if _, dup := oldAt[k]; dup {
			return nil
		}
		oldAt[k] = i
	}
	from := make([]int, len(b.Keys)) // each new item's old index, or -1
	seen := make(map[string]bool, len(b.Keys))
	for j, k := range b.Keys {
		if seen[k] {
			return nil
		}
		seen[k] = true
		from[j] = -1
		if i, ok := oldAt[k]; ok && a.items[i].key == b.Items[j].Key {
			from[j] = i
		}
	}
	inOrder := lis(from)
	var plan []step
	cur := 0
	for j, i := range from {
		switch {
		case i < 0:
			plan = append(plan, step{op: '+', new: j})
		case inOrder[j]:
			if i > cur {
				plan = append(plan, step{op: '-', old: i - cur})
			}
			plan = append(plan, step{op: '=', old: i, new: j})
			cur = i + 1
		default:
			plan = append(plan, step{op: 'm', old: i, new: j})
		}
	}
	return plan
}

// lis marks a longest increasing subsequence of the non-negative values of xs.
func lis(xs []int) []bool {
	var tails []int // index into xs of the smallest tail of each length
	prev := make([]int, len(xs))
	for j, x := range xs {
		if x < 0 {
			continue
		}
		lo, hi := 0, len(tails)
		for lo < hi {
			m := (lo + hi) / 2
			if xs[tails[m]] < x {
				lo = m + 1
			} else {
				hi = m
			}
		}
		prev[j] = -1
		if lo > 0 {
			prev[j] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, j)
		} else {
			tails[lo] = j
		}
	}
	out := make([]bool, len(xs))
	if len(tails) > 0 {
		for j := tails[len(tails)-1]; j >= 0; j = prev[j] {
			out[j] = true
		}
	}
	return out
}

// tokenPatch writes {"p":[…]}, a token patch from old to new markup, when it
// is clearly smaller than new: the tokens they start and end with kept, the
// middle replaced.
func (e *enc) tokenPatch(old, new string) bool {
	a, b := tokenize(old), tokenize(new)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	mid := strings.Join(b[pre:len(b)-suf], "")
	if len(mid)+32 > len(new)*3/4 {
		return false
	}
	e.raw(`{"p":[`)
	n := 0
	put := func(f func()) {
		if n > 0 {
			e.raw(",")
		}
		n++
		f()
	}
	if pre > 0 {
		put(func() { e.int(pre) })
	}
	if del := len(a) - pre - suf; del > 0 {
		put(func() { e.int(-del) })
	}
	if mid != "" {
		put(func() { e.quote(mid) })
	}
	if suf > 0 {
		put(func() { e.int(suf) })
	}
	e.raw("]}")
	return true
}

// tokenize cuts markup before every '<' and after every '>', '"' and
// "&#34;", as nexus's token patches do.
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

// hasRefs reports whether t renders another instance.
func hasRefs(t *vuka.Tree) bool {
	for _, d := range t.Dyn {
		switch d := d.(type) {
		case vuka.TreeRef:
			return true
		case *vuka.Tree:
			if hasRefs(d) {
				return true
			}
		case *vuka.TreeList:
			for _, it := range d.Items {
				if hasRefs(it) {
					return true
				}
			}
		}
	}
	return false
}

// sameTree reports whether a and b render alike (instances by id).
func sameTree(a, b *vuka.Tree) bool {
	if a == nil || b == nil {
		return a == b
	}
	c := conn{}
	c.reset()
	e := enc{c: &c}
	return !e.changesOrKey(shadow(a), b)
}

func (e *enc) changesOrKey(a *sframe, b *vuka.Tree) bool {
	return a.key != b.Key || e.changes(a, b)
}
